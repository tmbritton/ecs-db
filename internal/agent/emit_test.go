package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// normalise clears the fields ParseMachine derives rather than reads, so a
// round-trip comparison fails for emitter reasons rather than for these.
// Parent is a back-pointer (comparing it recurses forever); ContextManifest is
// filled in by ValidateMachine.
func normalise(def *MachineDefinition) {
	def.ContextManifest = nil
	var walk func(map[string]*StateNode)
	walk = func(states map[string]*StateNode) {
		for _, n := range states {
			n.Parent = nil
			walk(n.Children)
		}
	}
	walk(def.States)
}

// The property the whole story rests on: whatever EmitMachine writes,
// ParseMachine must read back as the same machine.
//
// It compares the definitions, not just that the output re-parses. Story 2's
// equivalent test compared two serialised outputs instead, and passed against
// an emitter that dropped fields entirely.
func TestEmitMachine_RoundTripsEveryShape(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "flat machine",
			src:  `{"id":"m","initial":"idle","states":{"idle":{},"busy":{}}}`,
		},
		{
			name: "context",
			src:  `{"id":"m","initial":"idle","context":{"hp":0,"name":"x"},"states":{"idle":{}}}`,
		},
		{
			name: "nested compound states",
			src: `{"id":"m","initial":"outer","states":{
				"outer":{"initial":"inner","states":{"inner":{},"other":{}}}
			}}`,
		},
		{
			name: "parallel regions",
			src: `{"id":"m","initial":"par","states":{
				"par":{"type":"parallel","states":{
					"a":{"initial":"a1","states":{"a1":{}}},
					"b":{"initial":"b1","states":{"b1":{}}}
				}}
			}}`,
		},
		{
			name: "final state",
			src: `{"id":"m","initial":"idle","states":{
				"idle":{"on":{"GO":"done"}},"done":{"type":"final"}
			}}`,
		},
		{
			name: "history node with a default target",
			src: `{"id":"m","initial":"outer","states":{
				"outer":{"initial":"a","states":{
					"a":{},"hist":{"type":"history","history":"deep","target":"a"}
				}}
			}}`,
		},
		{
			name: "entry and exit actions",
			src: `{"id":"m","initial":"idle","states":{
				"idle":{"entry":[{"type":"a"}],"exit":[{"type":"b"},{"type":"c"}]}
			}}`,
		},
		{
			name: "action with params",
			src: `{"id":"m","initial":"idle","states":{
				"idle":{"entry":[{"type":"a","params":{"radius":5,"name":"x"}}]}
			}}`,
		},
		{
			// An empty-but-present params map is not the same as an absent one,
			// and collapsing them is the case that silently breaks equality.
			name: "action with empty params",
			src: `{"id":"m","initial":"idle","states":{
				"idle":{"entry":[{"type":"a","params":{}}]}
			}}`,
		},
		{
			name: "guard with params",
			src: `{"id":"m","initial":"idle","states":{
				"idle":{"on":{"GO":[{"target":"idle","cond":{"type":"g","params":{"n":1}}}]}}
			}}`,
		},
		{
			name: "transition with actions and no target",
			src: `{"id":"m","initial":"idle","states":{
				"idle":{"on":{"GO":[{"actions":[{"type":"a"}]}]}}
			}}`,
		},
		{
			name: "multiple transitions for one event",
			src: `{"id":"m","initial":"idle","states":{
				"idle":{"on":{"GO":[{"target":"idle","cond":{"type":"g"}},{"actions":[{"type":"a"}]}]}}
			}}`,
		},
		{
			// Durations are kept as written: "500" and "500ms" mean the same
			// thing to the engine but are different keys in the file.
			name: "after with mixed duration formats",
			src: `{"id":"m","initial":"idle","states":{
				"idle":{"after":{"500":[{"target":"idle"}],"1000ms":[{"target":"idle"}]}}
			}}`,
		},
		{
			name: "shorthand string forms on input",
			src: `{"id":"m","initial":"idle","states":{
				"idle":{"entry":"a","on":{"GO":"idle"}}
			}}`,
		},
		{
			// The commonest XState transition shape, and absent from both repo
			// machines — goblin.json writes target+cond and actions as two
			// separate array elements, never combined. Discarding the target
			// whenever actions were present survived the whole suite.
			name: "transition with target, cond and actions together",
			src: `{"id":"m","initial":"a","states":{
				"a":{"on":{"E":[{"target":"a","cond":{"type":"g"},"actions":[{"type":"x"}]}]}}
			}}`,
		},
		{
			// Legal XState: type history with no history key defaults to
			// shallow. Dropping this branch emitted {} and re-parsed as an
			// atomic state — a history node silently demoted.
			name: "bare history state without a history key",
			src: `{"id":"m","initial":"o","states":{
				"o":{"initial":"x","states":{"x":{},"h":{"type":"history"}}}
			}}`,
		},
		{
			// Empty-but-present collections. The round-trip property was
			// simply false for these until ParseMachine normalised them.
			name: "empty context, entry, exit and actions",
			src: `{"id":"m","initial":"a","context":{},"states":{
				"a":{"entry":[],"exit":[],"on":{"E":[{"target":"a","actions":[]}]}}
			}}`,
		},
		{
			name: "explicit state id",
			src:  `{"id":"m","initial":"idle","states":{"idle":{"id":"custom.idle"}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := ParseMachine([]byte(tt.src))
			if err != nil {
				t.Fatalf("ParseMachine: %v", err)
			}
			out, err := EmitMachine(first)
			if err != nil {
				t.Fatalf("EmitMachine: %v", err)
			}

			// Valid JSON before anything else.
			var any map[string]any
			if err := json.Unmarshal(out, &any); err != nil {
				t.Fatalf("output is not valid JSON: %v\n%s", err, out)
			}

			second, err := ParseMachine(out)
			if err != nil {
				t.Fatalf("re-parsing emitted output failed: %v\n%s", err, out)
			}

			normalise(first)
			normalise(second)
			if !reflect.DeepEqual(first, second) {
				t.Errorf("the machine did not survive the round trip\n want: %s\n got:  %s\n--- output ---\n%s",
					dump(first), dump(second), out)
			}

			// Emitting twice must be a fixed point, or saving an unchanged
			// machine produces a diff.
			again, err := EmitMachine(second)
			if err != nil {
				t.Fatalf("EmitMachine (second pass): %v", err)
			}
			if !bytes.Equal(out, again) {
				t.Errorf("emit is not idempotent\n--- first ---\n%s\n--- second ---\n%s", out, again)
			}
		})
	}
}

func dump(def *MachineDefinition) string {
	b, err := json.Marshal(struct {
		ID      string
		Initial string
		Context map[string]any
		States  map[string]*StateNode
	}{def.ID, def.Initial, def.Context, def.States})
	if err != nil {
		return "(undumpable: " + err.Error() + ")"
	}
	return string(b)
}

// The files in this repo must survive a save untouched.
func TestEmitMachine_ByteStableAgainstTheRealMachines(t *testing.T) {
	for _, path := range []string{
		"../../behaviors/goblin.json",
		"../../e2e/fixtures/project/behaviors/e2e-wander.json",
	} {
		t.Run(path, func(t *testing.T) {
			original, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("not readable: %v", err)
			}
			def, err := ParseMachine(original)
			if err != nil {
				t.Fatalf("ParseMachine: %v", err)
			}
			got, err := EmitMachine(def)
			if err != nil {
				t.Fatalf("EmitMachine: %v", err)
			}
			if !bytes.Equal(got, original) {
				t.Errorf("round trip is not byte-stable\n--- want ---\n%s\n--- got ---\n%s", original, got)
			}
		})
	}
}

// The back-pointer is why json.Marshal cannot be the emitter: Go detects the
// cycle and refuses. The emitter must simply never write it.
func TestEmitMachine_NeverEmitsTheParentBackPointer(t *testing.T) {
	def, err := ParseMachine([]byte(`{"id":"m","initial":"a","states":{
		"a":{"initial":"a1","states":{"a1":{}}}
	}}`))
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	if def.States["a"].Children["a1"].Parent == nil {
		t.Fatal("the fixture does not exercise the back-pointer")
	}

	// json.Marshal on this same value fails; the emitter must not.
	if _, err := json.Marshal(def); err == nil {
		t.Error("expected json.Marshal to fail on the cycle; the fixture may no longer be nested")
	}

	out, err := EmitMachine(def)
	if err != nil {
		t.Fatalf("EmitMachine: %v", err)
	}
	for _, forbidden := range []string{`"Parent"`, `"parent"`} {
		if strings.Contains(string(out), forbidden) {
			t.Errorf("output contains %s:\n%s", forbidden, out)
		}
	}
}

// Order carries intent — states are usually written in the order they run, and
// e2e-wander.json opens with idle then chasing, which alphabetising reverses.
func TestEmitMachine_PreservesAuthoredOrder(t *testing.T) {
	src := []byte(`{
  "id": "m",
  "initial": "zulu",
  "context": {
    "zz": 0,
    "aa": 0
  },
  "states": {
    "zulu": {
      "on": {
        "ZED": [{ "target": "alpha" }],
        "ABLE": [{ "target": "alpha" }]
      }
    },
    "alpha": {}
  }
}
`)
	def, err := ParseMachine(src)
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	out, err := EmitMachine(def)
	if err != nil {
		t.Fatalf("EmitMachine: %v", err)
	}
	if !bytes.Equal(out, src) {
		t.Errorf("authored order was not preserved\n--- want ---\n%s\n--- got ---\n%s", src, out)
	}

	got := string(out)
	if strings.Index(got, `"zulu"`) > strings.Index(got, `"alpha"`) {
		t.Error("states were sorted alphabetically")
	}
	if strings.Index(got, `"zz"`) > strings.Index(got, `"aa"`) {
		t.Error("context keys were sorted alphabetically")
	}
	if strings.Index(got, `"ZED"`) > strings.Index(got, `"ABLE"`) {
		t.Error("event keys were sorted alphabetically")
	}
}

// A machine built in code has no recorded order and must still emit the same
// bytes every time, or every save is a spurious diff.
func TestEmitMachine_DeterministicWithoutRecordedOrder(t *testing.T) {
	def := &MachineDefinition{
		ID:      "m",
		Initial: "a",
		Context: map[string]any{"zz": 1.0, "aa": 2.0, "mm": 3.0},
		States: map[string]*StateNode{
			"zebra": {ID: "m.zebra", Type: StateTypeAtomic},
			"apple": {ID: "m.apple", Type: StateTypeAtomic},
			"mango": {ID: "m.mango", Type: StateTypeAtomic},
			"a":     {ID: "m.a", Type: StateTypeAtomic},
		},
	}
	first, err := EmitMachine(def)
	if err != nil {
		t.Fatalf("EmitMachine: %v", err)
	}
	for range 20 {
		again, err := EmitMachine(def)
		if err != nil {
			t.Fatalf("EmitMachine: %v", err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("not deterministic\n--- first ---\n%s\n--- again ---\n%s", first, again)
		}
	}
	got := string(first)
	if strings.Index(got, `"apple"`) > strings.Index(got, `"mango"`) {
		t.Error("unrecorded states are not sorted")
	}
}

// invoke is rejected at parse time, so emitting it would produce a file the
// engine cannot read back. ContextManifest is derived by ValidateMachine, not
// authored, and must not leak into the file either.
func TestEmitMachine_OmitsThingsTheFormatDoesNotHave(t *testing.T) {
	def, err := ParseMachine([]byte(`{"id":"m","initial":"idle","context":{"hp":0},"states":{"idle":{}}}`))
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	def.ContextManifest = map[string]string{"hp": "Health"}

	out, err := EmitMachine(def)
	if err != nil {
		t.Fatalf("EmitMachine: %v", err)
	}
	for _, forbidden := range []string{"invoke", "contextManifest", "ContextManifest", "Health"} {
		if strings.Contains(string(out), forbidden) {
			t.Errorf("output contains %q:\n%s", forbidden, out)
		}
	}
}

// Machine and state ids are written into the file by hand, so they need the
// same escaping any JSON string does.
func TestEmitMachine_EscapesStrings(t *testing.T) {
	def := &MachineDefinition{
		ID:      `m"with\escapes`,
		Initial: "we<ird",
		Context: map[string]any{`key"q`: "val<ue"},
		States: map[string]*StateNode{
			"we<ird": {ID: `m.we<ird`, Type: StateTypeAtomic, Entry: []ActionSpec{{Type: `act"ion`}}},
		},
	}
	out, err := EmitMachine(def)
	if err != nil {
		t.Fatalf("EmitMachine: %v", err)
	}
	var any map[string]any
	if err := json.Unmarshal(out, &any); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if strings.Contains(string(out), `\u003c`) {
		t.Errorf("HTML escaping is on:\n%s", out)
	}
}

// spaceInsideBraces walks the encoder's output by hand to turn {"a":1} into
// { "a": 1 }. Its whole justification is that it knows when it is inside a
// string — and nothing tested that. Three mutations survived the suite,
// including one that rewrote data: a context value of "a:b" became "a: b".
func TestSpaceInsideBraces(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "single pair", in: `{"a":1}`, want: `{ "a": 1 }`},
		{name: "two pairs", in: `{"a":1,"b":2}`, want: `{ "a": 1, "b": 2 }`},
		{name: "empty object stays tight", in: `{}`, want: `{}`},
		{name: "nested empty object", in: `{"o":{}}`, want: `{ "o": {} }`},
		{name: "nested object", in: `{"o":{"x":1}}`, want: `{ "o": { "x": 1 } }`},
		{
			// The one that corrupts data: punctuation inside a string value
			// must not be spaced.
			name: "punctuation inside a string is untouched",
			in:   `{"s":"a:b,c{d}e"}`,
			want: `{ "s": "a:b,c{d}e" }`,
		},
		{
			name: "escaped quote does not end the string",
			in:   `{"s":"he said \"a:b\" once"}`,
			want: `{ "s": "he said \"a:b\" once" }`,
		},
		{
			name: "a key containing punctuation",
			in:   `{"a:b":1}`,
			want: `{ "a:b": 1 }`,
		},
		{
			name: "escaped backslash before a quote",
			in:   `{"s":"back\\"}`,
			want: `{ "s": "back\\" }`,
		},
		{name: "array of objects", in: `{"a":[{"x":1},{"y":2}]}`, want: `{ "a": [{ "x": 1 }, { "y": 2 }] }`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := spaceInsideBraces(tt.in); got != tt.want {
				t.Errorf("spaceInsideBraces(%s)\n got:  %s\n want: %s", tt.in, got, tt.want)
			}
			// Whatever it does, the result must still be JSON that means the
			// same thing.
			var before, after any
			if err := json.Unmarshal([]byte(tt.in), &before); err != nil {
				t.Fatalf("fixture is not valid JSON: %v", err)
			}
			if err := json.Unmarshal([]byte(spaceInsideBraces(tt.in)), &after); err != nil {
				t.Fatalf("output is not valid JSON: %v", err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Errorf("the value changed:\n before: %#v\n after:  %#v", before, after)
			}
		})
	}
}

// A state carrying every field at once, asserted byte-for-byte. Nothing else
// pins the order fields are emitted in: swapping entry and exit, or type and
// id, survived the suite, and either would rewrite every behaviour file on the
// next save.
func TestEmitMachine_FieldOrderIsFixed(t *testing.T) {
	src := []byte(`{
  "id": "m",
  "initial": "everything",
  "states": {
    "everything": {
      "id": "custom",
      "initial": "child",
      "entry": [
        { "type": "onEnter" }
      ],
      "exit": [
        { "type": "onExit" }
      ],
      "on": {
        "GO": [{ "target": "everything" }]
      },
      "after": {
        "500": [{ "target": "everything" }]
      },
      "states": {
        "child": {}
      }
    }
  }
}
`)
	def, err := ParseMachine(src)
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	got, err := EmitMachine(def)
	if err != nil {
		t.Fatalf("EmitMachine: %v", err)
	}
	if !bytes.Equal(got, src) {
		t.Errorf("field order or layout changed\n--- want ---\n%s\n--- got ---\n%s", src, got)
	}
}

// Nested and parallel states, asserted byte-for-byte. Neither repo machine has
// nested states, so the recursion in writeStates was unverified at the byte
// level — changing the child indent survived the suite.
func TestEmitMachine_NestedLayoutIsFixed(t *testing.T) {
	src := []byte(`{
  "id": "m",
  "initial": "par",
  "states": {
    "par": {
      "type": "parallel",
      "states": {
        "left": {
          "initial": "l1",
          "states": {
            "l1": {},
            "l2": {}
          }
        },
        "right": {
          "initial": "r1",
          "states": {
            "r1": {}
          }
        }
      }
    },
    "done": {
      "type": "final"
    }
  }
}
`)
	def, err := ParseMachine(src)
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	got, err := EmitMachine(def)
	if err != nil {
		t.Fatalf("EmitMachine: %v", err)
	}
	if !bytes.Equal(got, src) {
		t.Errorf("nested layout changed\n--- want ---\n%s\n--- got ---\n%s", src, got)
	}
}

// Order is read from the raw bytes by key. encoding/json matches struct fields
// case-insensitively, so a machine authored with "States" parses fine — and an
// exact-match lookup would record no order and silently alphabetise it.
func TestParseMachine_RecordsOrderRegardlessOfKeyCase(t *testing.T) {
	def, err := ParseMachine([]byte(`{"id":"m","initial":"zulu","States":{"zulu":{},"alpha":{}}}`))
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	want := []string{"zulu", "alpha"}
	if len(def.StateOrder) != len(want) {
		t.Fatalf("StateOrder = %v, want %v", def.StateOrder, want)
	}
	for i := range want {
		if def.StateOrder[i] != want[i] {
			t.Fatalf("StateOrder = %v, want %v", def.StateOrder, want)
		}
	}
}
