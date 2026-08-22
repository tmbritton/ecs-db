package agent_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// roundTrip parses and re-emits, failing the test rather than returning errors
// that every caller would have to check identically.
func roundTrip(t *testing.T, in []byte) []byte {
	t.Helper()
	def, err := agent.ParseMachine(in)
	if err != nil {
		t.Fatalf("parse: %v\ninput:\n%s", err, in)
	}
	out, err := agent.EmitMachine(def)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return out
}

// corpus is every machine file the repo has, plus the awkward ones written for
// this story. Driven from files rather than Go literals: the thing under test
// is what happens to a file someone authored, and a literal is a file nobody
// authored.
func corpus(t *testing.T) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, dir := range []string{
		"testdata/behaviors",
		"testdata/roundtrip",
		"../../behaviors",
		"../../e2e/fixtures/project/behaviors",
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("reading %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			path := filepath.Join(dir, e.Name())
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			out[path] = raw
		}
	}
	// Named, not counted. A count is satisfied by testdata/roundtrip on its own,
	// so the two files Forge actually edits could stop being read and every
	// assertion below would still pass.
	for _, required := range []string{
		"../../behaviors/goblin.json",
		"../../e2e/fixtures/project/behaviors/e2e-wander.json",
		"testdata/behaviors/wandering_goblin.json",
		"testdata/roundtrip/stately-export.json",
		"testdata/roundtrip/root-level.json",
		"testdata/roundtrip/inline-extras.json",
	} {
		if _, ok := out[required]; !ok {
			t.Fatalf("the corpus is missing %s; the walk is not finding it", required)
		}
	}
	return out
}

// The property this story exists for. Not "description survives" — that is a
// test about description. Comparing whole decoded values is a test about the
// property, and it catches the field nobody thought of.
func TestRoundTrip_LosesNothing(t *testing.T) {
	for path, raw := range corpus(t) {
		t.Run(path, func(t *testing.T) {
			before := decode(t, raw)
			out := roundTrip(t, raw)
			if !json.Valid(out) {
				t.Fatalf("the emitted machine is not valid JSON:\n%s", out)
			}
			after := decode(t, out)
			if !sameValue(before, after) {
				t.Errorf("the round trip changed the machine.\n--- lost or altered ---\n%s\n--- emitted ---\n%s",
					diffKeys(before, after, ""), out)
			}
		})
	}
}

// Idempotence is what editable.Dirty needs: it compares marshalled bytes
// against the file on disk, so a machine that does not settle after one pass is
// permanently dirty for no visible reason.
func TestRoundTrip_Settles(t *testing.T) {
	for path, raw := range corpus(t) {
		t.Run(path, func(t *testing.T) {
			once := roundTrip(t, raw)
			twice := roundTrip(t, once)
			if string(once) != string(twice) {
				t.Errorf("a second round trip changed the output again:\n--- first ---\n%s\n--- second ---\n%s",
					once, twice)
			}
		})
	}
}

// The two files Forge will actually edit are already in the emitter's canonical
// style, and must stay that way: opening one and saving it must not produce a
// diff.
func TestRoundTrip_LeavesTheLiveFilesAlone(t *testing.T) {
	for _, path := range []string{
		"../../behaviors/goblin.json",
		"../../e2e/fixtures/project/behaviors/e2e-wander.json",
	} {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading: %v", err)
			}
			if got := roundTrip(t, raw); string(got) != string(raw) {
				t.Errorf("saving this file would rewrite it:\n--- on disk ---\n%s\n--- emitted ---\n%s",
					raw, got)
			}
		})
	}
}

// decode reads JSON the way this test needs to compare it.
//
// UseNumber matters more than it looks. Without it every number becomes a
// float64, so "big": 12345678901234567890 and the 12345678901234567000 that
// comes back out of a round trip compare equal and a real precision loss reads
// as no change at all. With it, numbers stay their authored text and the
// comparison sees what the file actually says.
//
// This replaced a normalise() helper that recursively rebuilt maps and slices
// and returned everything unchanged — nineteen lines that looked careful and
// did nothing, in a test whose entire job is to be careful.
func decode(t *testing.T, raw []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return v
}

// sameValue compares two decoded machines by what they mean rather than by how
// they are written.
//
// Numbers are compared as exact rationals. That draws the line in the right
// place: "speed": 2.0 emitted as 2 is a formatting change and is inventoried in
// the golden files, while 12345678901234567890 emerging as
// 12345678901234567000 is a real loss and has to fail here. A plain string
// comparison would reject the first; a float64 comparison would accept the
// second, because both sides would already have been rounded to the same wrong
// number before anything looked.
func sameValue(a, b any) bool {
	switch av := a.(type) {
	case json.Number:
		bv, ok := b.(json.Number)
		if !ok {
			return false
		}
		ar, aok := new(big.Rat).SetString(av.String())
		br, bok := new(big.Rat).SetString(bv.String())
		if !aok || !bok {
			return av.String() == bv.String()
		}
		return ar.Cmp(br) == 0
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			other, present := bv[k]
			if !present || !sameValue(v, other) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !sameValue(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

// diffKeys reports the paths at which two decoded machines differ, so a failure
// names the field rather than printing two documents and leaving the reader to
// find it.
func diffKeys(a, b any, path string) string {
	if sameValue(a, b) {
		return ""
	}
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if !aok || !bok {
		return path + ": " + describe(a) + " → " + describe(b) + "\n"
	}
	out := ""
	for k, av := range am {
		bv, present := bm[k]
		if !present {
			out += path + "." + k + ": dropped (" + describe(av) + ")\n"
			continue
		}
		out += diffKeys(av, bv, path+"."+k)
	}
	for k, bv := range bm {
		if _, present := am[k]; !present {
			out += path + "." + k + ": added (" + describe(bv) + ")\n"
		}
	}
	return out
}

func describe(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "?"
	}
	if len(b) > 120 {
		return string(b[:120]) + "…"
	}
	return string(b)
}

// The normalizations that remain are accepted, but they must not grow. This
// writes what the round trip actually produces to a golden file, so a new
// normalization changes a checked-in file and has to be explained in the same
// commit as the change that caused it.
//
// Re-record with `go test ./internal/agent -update` after a deliberate change.
// The flag is declared in this package only, so `go test ./... -update` fails
// with "flag provided but not defined" from every other one.
func TestRoundTrip_NormalisationsAreUnchanged(t *testing.T) {
	recorded := map[string]bool{}
	for path, raw := range corpus(t) {
		t.Run(path, func(t *testing.T) {
			golden := filepath.Join("testdata/golden", strings.ReplaceAll(
				strings.TrimPrefix(filepath.ToSlash(path), "../../"), "/", "_"))
			got := roundTrip(t, raw)

			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, got, 0o600); err != nil {
					t.Fatal(err)
				}
				return
			}
			recorded[golden] = true
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("no golden file: %v — run `go test ./internal/agent -update`", err)
			}
			if string(got) != string(want) {
				t.Errorf("the round trip changed. If that was deliberate, re-record with\n"+
					"`go test ./internal/agent -update` and explain it in the commit.\n"+
					"--- recorded ---\n%s\n--- now ---\n%s", want, got)
			}
		})
	}

	// A golden left behind by a deleted fixture pins nothing and reads as though
	// it does. Checked after the loop so the map is complete.
	if !*update {
		entries, err := os.ReadDir("testdata/golden")
		if err != nil {
			t.Fatalf("reading the golden directory: %v", err)
		}
		for _, e := range entries {
			path := filepath.Join("testdata/golden", e.Name())
			if !recorded[path] {
				t.Errorf("%s has no fixture behind it any more; delete it", path)
			}
		}
	}
}

var update = flag.Bool("update", false, "re-record the round-trip golden files")

// ── what the golden files pin ────────────────────────────────────────────────
//
// After this story the round trip loses nothing, and three formatting
// normalizations remain. All three are accepted; the golden files exist so a
// fourth cannot appear without someone noticing.
//
//  1. Fields this package does not model move to the end of their object.
//     Deliberate: writeStateNode emits known fields in a fixed order rather than
//     the authored one, so putting unknown ones back in their original slots
//     among reordered known ones would produce a jumble rather than the file.
//  2. A context value's number formatting is canonicalised — "speed": 2.0
//     becomes 2. Context decodes to map[string]any, so the authored text is gone
//     by the time anything could preserve it. An *extra's* formatting does
//     survive, because extras keep their bytes.
//  3. Array layout is canonicalised: a single-element entry list written inline
//     is expanded, a single transition written across lines is inlined. Which
//     spelling was used — array, object or bare string — is preserved; how it
//     was wrapped is not.
//
// Only (2) and (3) can make a file dirty the moment it is opened, and neither
// affects the two files Forge actually edits. Story 2 has to say "reformatting"
// rather than a bare "unsaved changes" when it happens.

// The engine has to ignore what Forge preserves, or preserving it would change
// how a machine runs. Asserted rather than claimed.
func TestExtras_ChangeNothingTheEngineReads(t *testing.T) {
	plain := []byte(`{
  "id": "m",
  "initial": "idle",
  "context": { "hp": 0 },
  "states": { "idle": { "entry": [{ "type": "log", "params": { "message": "x" } }] } }
}`)
	decorated := []byte(`{
  "id": "m",
  "initial": "idle",
  "description": "a machine with things Forge does not model",
  "context": { "hp": 0 },
  "states": {
    "idle": {
      "meta": { "forge": { "x": 10, "y": 20 } },
      "tags": ["decorated"],
      "entry": [{ "type": "log", "params": { "message": "x" } }]
    }
  }
}`)

	s := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Health": {Type: "object", Properties: map[string]schema.Property{"hp": {Type: "integer"}}},
		},
		EntityTypes: map[string]schema.EntityType{"T": {ValidationLevel: schema.ValidationStrict}},
	}
	registry := builtins.NewRegistry()

	a, err := agent.ParseMachine(plain)
	if err != nil {
		t.Fatalf("parsing the plain machine: %v", err)
	}
	b, err := agent.ParseMachine(decorated)
	if err != nil {
		t.Fatalf("parsing the decorated machine: %v", err)
	}

	if errs := agent.ValidateMachine(a, registry, s); len(errs) != 0 {
		t.Fatalf("the plain machine does not validate: %v", errs)
	}
	if errs := agent.ValidateMachine(b, registry, s); len(errs) != 0 {
		t.Errorf("decorating a machine made it invalid: %v", errs)
	}
	if !reflect.DeepEqual(a.ContextManifest, b.ContextManifest) {
		t.Errorf("the manifest differs: %v vs %v", a.ContextManifest, b.ContextManifest)
	}
	// And the parts the interpreter actually executes are untouched.
	if !reflect.DeepEqual(a.States["idle"].Entry, b.States["idle"].Entry) {
		t.Errorf("entry actions differ:\n  %+v\n  %+v", a.States["idle"].Entry, b.States["idle"].Entry)
	}
	if a.Initial != b.Initial || !reflect.DeepEqual(a.Context, b.Context) {
		t.Error("initial state or context differs")
	}
}

// The layout decision in this story is that canvas coordinates live in each
// state's own meta, under a forge key. That is only viable if writing them
// leaves a user's own meta alone, so prove it rather than assume it — Story 4
// builds the accessor, this fixes the mechanism it has to use.
func TestExtras_CanCarryLayoutBesideSomeoneElsesMeta(t *testing.T) {
	in := []byte(`{
  "id": "m",
  "initial": "idle",
  "states": {
    "idle": { "meta": { "notes": "mine", "author": "someone" } }
  }
}`)
	def, err := agent.ParseMachine(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	node := def.States["idle"]
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(node.Extra["meta"], &meta); err != nil {
		t.Fatalf("meta did not survive as an object: %v", err)
	}
	meta["forge"] = json.RawMessage(`{ "x": 40, "y": 80 }`)
	merged, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("re-marshalling meta: %v", err)
	}
	node.Extra["meta"] = merged

	out, err := agent.EmitMachine(def)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	again, err := agent.ParseMachine(out)
	if err != nil {
		t.Fatalf("re-parsing what we wrote: %v\n%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal(again.States["idle"].Extra["meta"], &got); err != nil {
		t.Fatalf("meta is not an object after the round trip: %v", err)
	}
	for _, key := range []string{"notes", "author", "forge"} {
		if _, ok := got[key]; !ok {
			t.Errorf("meta.%s did not survive: %s", key, out)
		}
	}
}

// encoding/json matches field names case-insensitively, so a machine authored
// with "Initial" decodes into Initial and the parser has already consumed it.
// Capturing it as an unknown field too would emit it twice and produce a file
// with two initial keys — which still parses, ambiguously, and would be found
// by whoever read the diff rather than by anything here.
func TestExtras_DoNotDuplicateAModelledFieldWrittenInAnotherCase(t *testing.T) {
	for _, spelling := range []string{"Initial", "INITIAL", "States", "Context", "Invoke"} {
		t.Run(spelling, func(t *testing.T) {
			in := []byte(`{
  "id": "m",
  "` + spelling + `": ` + placeholderFor(spelling) + `,
  "initial": "idle",
  "states": { "idle": {}, "other": {} }
}`)
			def, err := agent.ParseMachine(in)
			if err != nil {
				// Invoke is rejected outright, which is the right answer and
				// also proves the case-folding reaches it.
				if spelling == "Invoke" {
					return
				}
				t.Fatalf("parse: %v", err)
			}
			if _, captured := def.Extra[spelling]; captured {
				t.Fatalf("%q was captured as an unknown field although the parser models it;\n"+
					"emitting it would write the same field twice", spelling)
			}
			out, err := agent.EmitMachine(def)
			if err != nil {
				t.Fatalf("emit: %v", err)
			}
			if n := strings.Count(string(out), `"initial"`) + strings.Count(string(out), `"`+spelling+`"`); n > 1 {
				t.Errorf("the emitted machine names the same field more than once:\n%s", out)
			}
		})
	}
}

func placeholderFor(name string) string {
	switch strings.ToLower(name) {
	case "states":
		// Deliberately different from the lowercase key beside it: with both
		// spellings carrying the same value, whichever one wins looks correct
		// and the test cannot see a drop.
		return `{ "only": {} }`
	case "context":
		return `{ "hp": 0 }`
	case "invoke":
		return `{ "src": "x" }`
	default:
		return `"idle"`
	}
}

// A spelling is preserved only while it can still say what the value says.
// These are the editing paths — a round trip alone never produces a bare
// transition that also has a guard, so nothing above can reach them.
func TestForms_ExpandWhenTheValueOutgrowsThem(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		edit    func(*agent.MachineDefinition)
		wantNot string
		want    string
	}{
		{
			name: "a bare transition that gains a guard",
			in:   `{"id":"m","initial":"a","states":{"a":{"on":{"GO":"a"}}}}`,
			edit: func(d *agent.MachineDefinition) {
				t := d.States["a"].On["GO"]
				t[0].Cond = &agent.CondSpec{Type: "timerExpired"}
			},
			wantNot: `"GO": "a"`,
			want:    `"cond"`,
		},
		{
			name: "a bare action that gains params",
			in:   `{"id":"m","initial":"a","states":{"a":{"exit":"log"}}}`,
			edit: func(d *agent.MachineDefinition) {
				d.States["a"].Exit[0].Params = map[string]any{"message": "x"}
			},
			wantNot: `"exit": "log"`,
			want:    `"params"`,
		},
		{
			name: "a single transition that gains a second",
			in:   `{"id":"m","initial":"a","states":{"a":{"on":{"GO":{"target":"a"}}}}}`,
			edit: func(d *agent.MachineDefinition) {
				d.States["a"].On["GO"] = append(d.States["a"].On["GO"], agent.Transition{Target: "a"})
			},
			wantNot: `"GO": { "target": "a" }`,
			want:    `[`,
		},
		{
			name: "a bare action that gains an empty params map",
			in:   `{"id":"m","initial":"a","states":{"a":{"exit":"log"}}}`,
			edit: func(d *agent.MachineDefinition) {
				// Present but empty is not absent — specObject draws that
				// distinction deliberately, and collapsing to "log" erases it.
				d.States["a"].Exit[0].Params = map[string]any{}
			},
			wantNot: `"exit": "log"`,
			want:    `"params": {}`,
		},
		{
			name: "a bare action that gains an unmodelled field",
			in:   `{"id":"m","initial":"a","states":{"a":{"exit":"log"}}}`,
			edit: func(d *agent.MachineDefinition) {
				d.States["a"].Exit[0].Extra = map[string]json.RawMessage{"description": json.RawMessage(`"d"`)}
			},
			wantNot: `"exit": "log"`,
			want:    `"description"`,
		},
		{
			name: "a bare guard that gains an unmodelled field",
			in:   `{"id":"m","initial":"a","states":{"a":{"on":{"GO":[{"target":"a","cond":"g"}]}}}}`,
			edit: func(d *agent.MachineDefinition) {
				d.States["a"].On["GO"][0].Cond.Extra = map[string]json.RawMessage{"description": json.RawMessage(`"d"`)}
			},
			wantNot: `"cond": "g"`,
			want:    `"description"`,
		},
		{
			name: "a bare transition that gains an unmodelled field",
			in:   `{"id":"m","initial":"a","states":{"a":{"on":{"GO":"a"}}}}`,
			edit: func(d *agent.MachineDefinition) {
				d.States["a"].On["GO"][0].Extra = map[string]json.RawMessage{"internal": json.RawMessage(`false`)}
			},
			wantNot: `"GO": "a"`,
			want:    `"internal"`,
		},
		{
			name: "a single action list that gains a second",
			in:   `{"id":"m","initial":"a","states":{"a":{"entry":{"type":"log"}}}}`,
			edit: func(d *agent.MachineDefinition) {
				d.States["a"].Entry = append(d.States["a"].Entry, agent.ActionSpec{Type: "log"})
			},
			wantNot: `"entry": { "type": "log" }`,
			want:    `[`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			def, err := agent.ParseMachine([]byte(tc.in))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			// The spelling survives an untouched round trip …
			before, err := agent.EmitMachine(def)
			if err != nil {
				t.Fatalf("emit: %v", err)
			}
			if !strings.Contains(string(before), tc.wantNot) {
				t.Fatalf("the authored spelling was not preserved to begin with, so this\n"+
					"test cannot show that editing expands it:\n%s", before)
			}

			// … and gives way the moment the value needs more than it can say.
			tc.edit(def)
			after, err := agent.EmitMachine(def)
			if err != nil {
				t.Fatalf("emit after editing: %v", err)
			}
			if strings.Contains(string(after), tc.wantNot) {
				t.Errorf("the short spelling was kept for a value it cannot express:\n%s", after)
			}
			if !strings.Contains(string(after), tc.want) {
				t.Errorf("the expanded form is missing %q:\n%s", tc.want, after)
			}
			if _, err := agent.ParseMachine(after); err != nil {
				t.Errorf("what we wrote no longer parses: %v\n%s", err, after)
			}
		})
	}
}

// A known limit, pinned so it is visible rather than discovered.
//
// Context decodes to map[string]any, so its numbers become float64 and an
// integer past float64's exact range comes back changed. That is real loss, not
// formatting — the corpus deliberately contains no such file, because the
// property test would rightly fail on it. Fixing it means keeping raw bytes
// beside the decoded values and a rule for when an edit invalidates them, which
// belongs with the story that lets you edit a context value.
//
// Extras are unaffected: they keep their bytes, so an unmodelled field holds a
// number of any size exactly.
func TestRoundTrip_ContextNumbersAreLimitedToFloat64(t *testing.T) {
	in := []byte(`{
  "id": "m",
  "initial": "a",
  "context": { "big": 12345678901234567890 },
  "huge": 12345678901234567890,
  "states": { "a": {} }
}`)
	out := roundTrip(t, in)

	got := decode(t, out).(map[string]any)
	ctx := got["context"].(map[string]any)
	if ctx["big"].(json.Number).String() == "12345678901234567890" {
		t.Errorf("context now preserves large integers exactly — good, but the\n" +
			"inventory in this file still says it does not. Update both.")
	}
	if got := got["huge"].(json.Number).String(); got != "12345678901234567890" {
		t.Errorf("an extra lost precision, which it should not: extras keep their\n"+
			"bytes. Got %s", got)
	}
}

// Story 4 writes canvas layout into a state's meta. The first state it does
// that to will not have had a meta, so nothing recorded an order for the key —
// and an extra present in the map but missing from the order slice used to be
// silently dropped, layout and all.
func TestExtras_AreWrittenEvenWhenNothingRecordedTheirOrder(t *testing.T) {
	def, err := agent.ParseMachine([]byte(`{"id":"m","initial":"a","states":{"a":{}}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	node := def.States["a"]
	if node.ExtraOrder != nil {
		t.Fatalf("this fixture is meant to have no recorded order, got %v", node.ExtraOrder)
	}
	node.Extra = map[string]json.RawMessage{"meta": json.RawMessage(`{ "forge": { "x": 1, "y": 2 } }`)}

	out, err := agent.EmitMachine(def)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(string(out), `"forge"`) {
		t.Fatalf("the layout was dropped:\n%s", out)
	}
	again, err := agent.ParseMachine(out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if _, ok := again.States["a"].Extra["meta"]; !ok {
		t.Errorf("meta did not survive the round trip:\n%s", out)
	}
}

// The same path can put a field into Extra that the emitter writes itself. That
// used to produce an object with the key twice — valid JSON, ambiguous meaning,
// and found by whoever read the diff.
func TestExtras_RefuseToShadowAFieldTheEmitterWrites(t *testing.T) {
	tests := []struct {
		name string
		set  func(*agent.MachineDefinition)
	}{
		{"on the machine", func(d *agent.MachineDefinition) {
			d.Extra = map[string]json.RawMessage{"initial": json.RawMessage(`"zzz"`)}
			d.ExtraOrder = []string{"initial"}
		}},
		{"on a state", func(d *agent.MachineDefinition) {
			d.States["a"].Extra = map[string]json.RawMessage{"entry": json.RawMessage(`["log"]`)}
			d.States["a"].ExtraOrder = []string{"entry"}
		}},
		{"in a different case", func(d *agent.MachineDefinition) {
			d.Extra = map[string]json.RawMessage{"Initial": json.RawMessage(`"zzz"`)}
			d.ExtraOrder = []string{"Initial"}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			def, err := agent.ParseMachine([]byte(`{"id":"m","initial":"a","states":{"a":{}}}`))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			tc.set(def)
			out, err := agent.EmitMachine(def)
			if err == nil {
				t.Fatalf("emitting produced a file rather than refusing:\n%s", out)
			}
		})
	}
}

// A duplicated key in the source. encoding/json keeps the last, and so must
// this — but ExtraOrder is exported state that Story 4 reads and writes, so it
// must not carry the key twice either. (The emitter would survive it anyway:
// jsonorder.Apply deduplicates. This is about the value handed out.)
func TestExtras_RecordADuplicatedKeyOnce(t *testing.T) {
	raw, err := os.ReadFile("testdata/roundtrip/duplicate-key.json")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	def, err := agent.ParseMachine(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	seen := map[string]int{}
	for _, key := range def.ExtraOrder {
		seen[key]++
	}
	for key, n := range seen {
		if n != 1 {
			t.Errorf("ExtraOrder lists %q %d times: %v", key, n, def.ExtraOrder)
		}
	}
	if got := string(def.Extra["note"]); got != `"second"` {
		t.Errorf("want the last value to win as encoding/json does, got %s", got)
	}
}
