package agent

import (
	"fmt"
	"strings"
	"testing"
)

const wanderingGoblinJSON = `{
  "id": "wandering_goblin",
  "initial": "idle",
  "context": {
    "speed": 2.0,
    "aggroRange": 80,
    "target_x": null,
    "target_y": null
  },
  "states": {
    "idle": {
      "entry": [{ "type": "setTimer", "params": { "key": "patience", "ticks": 40 } }],
      "on": {
        "TICK": [
          {
            "target": "wandering",
            "cond": { "type": "timerExpired", "params": { "key": "patience" } }
          }
        ],
        "PLAYER_NEARBY": "pursuing"
      }
    },
    "wandering": {
      "entry": [{ "type": "pickRandomTarget", "params": { "radius": 100 } }],
      "on": {
        "TICK": [
          { "target": "idle", "cond": "atTarget" },
          { "actions": [{ "type": "moveTowardTarget" }] }
        ],
        "PLAYER_NEARBY": "pursuing"
      }
    },
    "pursuing": {
      "entry": [{ "type": "setPursueTarget" }],
      "on": {
        "TICK": [
          {
            "target": "attacking",
            "cond": { "type": "inRange", "params": { "distance": 16 } }
          },
          { "actions": [{ "type": "moveTowardTarget", "params": { "speed_mult": 1.5 } }] }
        ],
        "PLAYER_FAR": "idle"
      }
    },
    "attacking": {
      "entry": [
        { "type": "dealDamage", "params": { "amount": 5, "target": "$player" } },
        "playAttackSound"
      ],
      "after": { "500": "pursuing" }
    }
  }
}`

func mustParse(t *testing.T, data string) *MachineDefinition {
	t.Helper()
	md, err := ParseMachine([]byte(data))
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	return md
}

// ── ParseMachine: top-level behaviour ────────────────────────────────────────

func TestParseMachine_MalformedJSON(t *testing.T) {
	_, err := ParseMachine([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestParseMachine_MinimalMachine(t *testing.T) {
	md := mustParse(t, `{"id":"m","initial":"a","states":{"a":{}}}`)
	if md.ID != "m" {
		t.Errorf("ID = %q, want %q", md.ID, "m")
	}
	if md.Initial != "a" {
		t.Errorf("Initial = %q, want %q", md.Initial, "a")
	}
	a, ok := md.States["a"]
	if !ok {
		t.Fatal("state 'a' not found")
	}
	if a.ID != "m.a" {
		t.Errorf("state ID = %q, want %q", a.ID, "m.a")
	}
}

func TestParseMachine_RootInvokeRejected(t *testing.T) {
	_, err := ParseMachine([]byte(`{"id":"m","initial":"a","states":{"a":{}},"invoke":{"src":"service"}}`))
	if err == nil {
		t.Fatal("expected error for invoke at root, got nil")
	}
	if !strings.Contains(err.Error(), "invoke is not supported") {
		t.Errorf("error %q does not mention invoke", err.Error())
	}
}

func TestParseMachine_StateInvokeRejected(t *testing.T) {
	_, err := ParseMachine([]byte(`{"id":"m","initial":"a","states":{"a":{"invoke":{"src":"svc"}}}}`))
	if err == nil {
		t.Fatal("expected error for invoke in state, got nil")
	}
	if !strings.Contains(err.Error(), "invoke is not supported") {
		t.Errorf("error %q does not mention invoke", err.Error())
	}
	if !strings.Contains(err.Error(), `"m"`) {
		t.Errorf("error %q does not mention machine ID", err.Error())
	}
	if !strings.Contains(err.Error(), `"a"`) {
		t.Errorf("error %q does not mention state name", err.Error())
	}
}

func TestParseMachine_ParentPointers(t *testing.T) {
	md := mustParse(t, `{"id":"m","initial":"p","states":{"p":{"initial":"c","states":{"c":{"initial":"g","states":{"g":{}}}}}}}`)
	p := md.States["p"]
	c := p.Children["c"]
	if c == nil {
		t.Fatal("child 'c' not found")
	}
	g := c.Children["g"]
	if g == nil {
		t.Fatal("grandchild 'g' not found")
	}

	if p.Parent != nil {
		t.Errorf("top-level state parent should be nil, got %v", p.Parent.ID)
	}
	if c.Parent != p {
		t.Errorf("c.Parent should be p (%s), got %v", p.ID, c.Parent)
	}
	if g.Parent != c {
		t.Errorf("g.Parent should be c (%s), got %v", c.ID, g.Parent)
	}
}

func TestParseMachine_ExplicitStateID(t *testing.T) {
	md := mustParse(t, `{"id":"m","initial":"a","states":{"a":{"id":"customID"}}}`)
	if md.States["a"].ID != "customID" {
		t.Errorf("ID = %q, want %q", md.States["a"].ID, "customID")
	}
}

func TestParseMachine_UnknownFieldsIgnored(t *testing.T) {
	_, err := ParseMachine([]byte(`{
		"id": "m", "initial": "a", "version": "1.0", "description": "test",
		"states": {"a": {"meta": {"color": "red"}, "tags": ["active"], "description": "a state"}}
	}`))
	if err != nil {
		t.Fatalf("unexpected error for unknown fields: %v", err)
	}
}

// ── State type inference (table-driven) ───────────────────────────────────────

func TestParseStateType(t *testing.T) {
	tests := []struct {
		name         string
		stateJSON    string
		wantType     StateType
		wantHistory  string
		wantTarget   string
		wantInitial  string
		wantChildren []string // expected child keys; nil = don't check
	}{
		{
			name:      "atomic (no children, no type field)",
			stateJSON: `{}`,
			wantType:  StateTypeAtomic,
		},
		{
			name:         "compound (nested states + initial)",
			stateJSON:    `{"initial":"c","states":{"c":{}}}`,
			wantType:     StateTypeCompound,
			wantInitial:  "c",
			wantChildren: []string{"c"},
		},
		{
			name:         "parallel (type:parallel)",
			stateJSON:    `{"type":"parallel","states":{"a":{},"b":{}}}`,
			wantType:     StateTypeParallel,
			wantChildren: []string{"a", "b"},
		},
		{
			name:      "final (type:final)",
			stateJSON: `{"type":"final"}`,
			wantType:  StateTypeFinal,
		},
		{
			name:      "history shallow (type:history)",
			stateJSON: `{"type":"history"}`,
			wantType:  StateTypeHistory,
		},
		{
			name:        "history deep (type:history + history:deep)",
			stateJSON:   `{"type":"history","history":"deep"}`,
			wantType:    StateTypeHistory,
			wantHistory: "deep",
		},
		{
			name:      "history via type:deep (Stately variant)",
			stateJSON: `{"type":"deep"}`,
			wantType:  StateTypeHistory,
		},
		{
			name:      "history from history field only (no type)",
			stateJSON: `{"history":"shallow"}`,
			wantType:  StateTypeHistory,
		},
		{
			name:       "history with default target",
			stateJSON:  `{"type":"history","target":"idle"}`,
			wantType:   StateTypeHistory,
			wantTarget: "idle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			machineJSON := fmt.Sprintf(`{"id":"m","initial":"s","states":{"s":%s}}`, tt.stateJSON)
			md := mustParse(t, machineJSON)
			s := md.States["s"]
			if s.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", s.Type, tt.wantType)
			}
			if tt.wantHistory != "" && s.History != tt.wantHistory {
				t.Errorf("History = %q, want %q", s.History, tt.wantHistory)
			}
			if tt.wantTarget != "" && s.Target != tt.wantTarget {
				t.Errorf("Target = %q, want %q", s.Target, tt.wantTarget)
			}
			if tt.wantInitial != "" && s.Initial != tt.wantInitial {
				t.Errorf("Initial = %q, want %q", s.Initial, tt.wantInitial)
			}
			for _, k := range tt.wantChildren {
				if _, ok := s.Children[k]; !ok {
					t.Errorf("child %q not found", k)
				}
			}
		})
	}
}

// ── Action parsing (table-driven) ─────────────────────────────────────────────

func TestParseActionSpecs(t *testing.T) {
	tests := []struct {
		name        string
		field       string // "entry" or "exit"
		stateJSON   string
		wantTypes   []string
		wantParams0 map[string]any // checked on first spec when non-nil
	}{
		{
			name:      "entry string shorthand",
			field:     "entry",
			stateJSON: `{"entry":"myAction"}`,
			wantTypes: []string{"myAction"},
		},
		{
			name:        "entry object with params",
			field:       "entry",
			stateJSON:   `{"entry":{"type":"myAction","params":{"k":1}}}`,
			wantTypes:   []string{"myAction"},
			wantParams0: map[string]any{"k": float64(1)},
		},
		{
			name:      "entry array of string and object",
			field:     "entry",
			stateJSON: `{"entry":["first",{"type":"second","params":{}}]}`,
			wantTypes: []string{"first", "second"},
		},
		{
			name:      "exit string shorthand",
			field:     "exit",
			stateJSON: `{"exit":"cleanup"}`,
			wantTypes: []string{"cleanup"},
		},
		{
			name:      "exit array of string and object",
			field:     "exit",
			stateJSON: `{"exit":["cleanup",{"type":"log","params":{}}]}`,
			wantTypes: []string{"cleanup", "log"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			machineJSON := fmt.Sprintf(`{"id":"m","initial":"a","states":{"a":%s}}`, tt.stateJSON)
			md := mustParse(t, machineJSON)
			a := md.States["a"]
			var specs []ActionSpec
			if tt.field == "entry" {
				specs = a.Entry
			} else {
				specs = a.Exit
			}
			if len(specs) != len(tt.wantTypes) {
				t.Fatalf("%s len = %d, want %d", tt.field, len(specs), len(tt.wantTypes))
			}
			for i, wt := range tt.wantTypes {
				if specs[i].Type != wt {
					t.Errorf("%s[%d].Type = %q, want %q", tt.field, i, specs[i].Type, wt)
				}
			}
			if tt.wantParams0 != nil {
				for k, want := range tt.wantParams0 {
					if got := specs[0].Params[k]; got != want {
						t.Errorf("%s[0].Params[%q] = %v, want %v", tt.field, k, got, want)
					}
				}
			}
		})
	}
}

// ── Transition parsing (table-driven) ─────────────────────────────────────────

func TestParseTransitions(t *testing.T) {
	tests := []struct {
		name            string
		stateJSON       string
		event           string
		useAfter        bool
		wantLen         int
		wantTarget      string
		wantCond        bool // whether ts[0].Cond != nil
		wantCondType    string
		wantCondParam   [2]string // [key, expected string value]; empty = skip
		wantAction0Type string
		wantAfterKey    string // when useAfter, key to look up in After map
	}{
		{
			name:       "string target is unconditional",
			stateJSON:  `{"on":{"EVENT":"b"}}`,
			event:      "EVENT",
			wantLen:    1,
			wantTarget: "b",
			wantCond:   false,
		},
		{
			name:         "object with cond string shorthand",
			stateJSON:    `{"on":{"E":{"target":"b","cond":"guardName"}}}`,
			event:        "E",
			wantLen:      1,
			wantTarget:   "b",
			wantCond:     true,
			wantCondType: "guardName",
		},
		{
			name:          "object with cond object form",
			stateJSON:     `{"on":{"E":{"target":"b","cond":{"type":"timerExpired","params":{"key":"patience"}}}}}`,
			event:         "E",
			wantLen:       1,
			wantCond:      true,
			wantCondType:  "timerExpired",
			wantCondParam: [2]string{"key", "patience"},
		},
		{
			name:      "array with string and object elements",
			stateJSON: `{"on":{"E":["b",{"target":"c","cond":"g"}]}}`,
			event:     "E",
			wantLen:   2,
		},
		{
			name:            "object with actions",
			stateJSON:       `{"on":{"E":{"actions":["doThing"]}}}`,
			event:           "E",
			wantLen:         1,
			wantAction0Type: "doThing",
		},
		{
			name:         "after key preserved verbatim",
			stateJSON:    `{"after":{"500":"b"}}`,
			event:        "500",
			useAfter:     true,
			wantLen:      1,
			wantTarget:   "b",
			wantAfterKey: "500",
		},
		{
			name:      "null transition value produces empty list",
			stateJSON: `{"on":{"E":null}}`,
			event:     "E",
			wantLen:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			machineJSON := fmt.Sprintf(`{"id":"m","initial":"a","states":{"a":%s,"b":{},"c":{}}}`, tt.stateJSON)
			md := mustParse(t, machineJSON)
			a := md.States["a"]
			var ts []Transition
			if tt.useAfter {
				ts = a.After[tt.wantAfterKey]
			} else {
				ts = a.On[tt.event]
			}
			if len(ts) != tt.wantLen {
				t.Fatalf("transitions len = %d, want %d", len(ts), tt.wantLen)
			}
			if tt.wantLen == 0 {
				return
			}
			if tt.wantTarget != "" && ts[0].Target != tt.wantTarget {
				t.Errorf("ts[0].Target = %q, want %q", ts[0].Target, tt.wantTarget)
			}
			if !tt.wantCond && ts[0].Cond != nil {
				t.Errorf("ts[0].Cond should be nil")
			}
			if tt.wantCond && ts[0].Cond == nil {
				t.Fatalf("ts[0].Cond should not be nil")
			}
			if tt.wantCondType != "" && ts[0].Cond.Type != tt.wantCondType {
				t.Errorf("ts[0].Cond.Type = %q, want %q", ts[0].Cond.Type, tt.wantCondType)
			}
			if tt.wantCondParam != [2]string{} {
				k, want := tt.wantCondParam[0], tt.wantCondParam[1]
				if got, _ := ts[0].Cond.Params[k].(string); got != want {
					t.Errorf("ts[0].Cond.Params[%q] = %v, want %q", k, ts[0].Cond.Params[k], want)
				}
			}
			if tt.wantAction0Type != "" {
				if len(ts[0].Actions) == 0 {
					t.Fatalf("ts[0].Actions is empty")
				}
				if ts[0].Actions[0].Type != tt.wantAction0Type {
					t.Errorf("ts[0].Actions[0].Type = %q, want %q", ts[0].Actions[0].Type, tt.wantAction0Type)
				}
			}
		})
	}
}

// ── Error paths (table-driven) ────────────────────────────────────────────────

func TestParseMachine_ErrorPaths(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		wantContain string // substring the error must contain; empty = just check non-nil
	}{
		{
			name:        "state value is not an object",
			json:        `{"id":"m","initial":"a","states":{"a":true}}`,
			wantContain: `"m"`,
		},
		{
			name:        "entry action bad params (not an object)",
			json:        `{"id":"m","initial":"a","states":{"a":{"entry":{"type":"x","params":123}}}}`,
			wantContain: "entry",
		},
		{
			name:        "exit action bad params",
			json:        `{"id":"m","initial":"a","states":{"a":{"exit":{"type":"x","params":123}}}}`,
			wantContain: "exit",
		},
		{
			name:        "entry array element bad params",
			json:        `{"id":"m","initial":"a","states":{"a":{"entry":[{"type":"x","params":123}]}}}`,
			wantContain: "entry",
		},
		{
			name: "on transition value is a number",
			json: `{"id":"m","initial":"a","states":{"a":{"on":{"E":123}}}}`,
		},
		{
			name:        "on cond params not an object",
			json:        `{"id":"m","initial":"a","states":{"a":{"on":{"E":{"target":"b","cond":{"type":"g","params":123}}}}}}`,
			wantContain: "cond",
		},
		{
			name:        "on transition action params not an object",
			json:        `{"id":"m","initial":"a","states":{"a":{"on":{"E":{"actions":{"type":"x","params":123}}}}}}`,
			wantContain: "actions",
		},
		{
			name:        "after transition value is a number",
			json:        `{"id":"m","initial":"a","states":{"a":{"after":{"500":123}}}}`,
			wantContain: "after",
		},
		{
			name: "child state value is not an object",
			json: `{"id":"m","initial":"a","states":{"a":{"initial":"b","states":{"b":true}}}}`,
		},
		{
			name: "on array element has bad cond params",
			json: `{"id":"m","initial":"a","states":{"a":{"on":{"E":[{"target":"b","cond":{"type":"g","params":123}}]}}}}`,
		},
		{
			name:        "action spec null input",
			json:        `{"id":"m","initial":"a","states":{"a":{"entry":[null]}}}`,
			wantContain: "null or empty",
		},
		{
			name:        "null element in transition array",
			json:        `{"id":"m","initial":"a","states":{"a":{"on":{"E":[null]}}}}`,
			wantContain: "null or empty",
		},
		{
			name:        "nested transition arrays rejected",
			json:        `{"id":"m","initial":"a","states":{"a":{"on":{"E":[["b"]]}}}}`,
			wantContain: "nested transition arrays",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseMachine([]byte(tt.json))
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if tt.wantContain != "" && !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantContain)
			}
		})
	}
}

// ── Round-trip integration test ───────────────────────────────────────────────

func TestParseMachine_WanderingGoblinRoundTrip(t *testing.T) {
	md, err := ParseMachine([]byte(wanderingGoblinJSON))
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}

	if md.ID != "wandering_goblin" {
		t.Errorf("ID = %q, want %q", md.ID, "wandering_goblin")
	}
	if md.Initial != "idle" {
		t.Errorf("Initial = %q, want %q", md.Initial, "idle")
	}
	if len(md.States) != 4 {
		t.Errorf("States count = %d, want 4", len(md.States))
	}

	// attacking.after["500"] → pursuing
	attacking := md.States["attacking"]
	if attacking == nil {
		t.Fatal("state 'attacking' not found")
	}
	afterTs, ok := attacking.After["500"]
	if !ok {
		t.Fatal("attacking.After['500'] not found")
	}
	if len(afterTs) != 1 || afterTs[0].Target != "pursuing" {
		t.Errorf("attacking.After['500'] = %v, want [{Target:pursuing}]", afterTs)
	}

	// idle.on["TICK"] — one conditional transition
	idle := md.States["idle"]
	if idle == nil {
		t.Fatal("state 'idle' not found")
	}
	tickTs := idle.On["TICK"]
	if len(tickTs) != 1 {
		t.Fatalf("idle.On[TICK] len = %d, want 1", len(tickTs))
	}
	if tickTs[0].Cond == nil {
		t.Error("idle.On[TICK][0] should be conditional")
	}

	// idle.on["PLAYER_NEARBY"] — one unconditional transition
	nearbyTs := idle.On["PLAYER_NEARBY"]
	if len(nearbyTs) != 1 {
		t.Fatalf("idle.On[PLAYER_NEARBY] len = %d, want 1", len(nearbyTs))
	}
	if nearbyTs[0].Cond != nil {
		t.Error("idle.On[PLAYER_NEARBY][0] should be unconditional")
	}
	if nearbyTs[0].Target != "pursuing" {
		t.Errorf("idle.On[PLAYER_NEARBY][0].Target = %q, want %q", nearbyTs[0].Target, "pursuing")
	}

	// context values preserved including null
	if md.Context["speed"] != float64(2.0) {
		t.Errorf("Context[speed] = %v, want 2.0", md.Context["speed"])
	}
	if _, hasKey := md.Context["target_x"]; !hasKey {
		t.Error("Context should contain 'target_x' key (null value)")
	}
	if md.Context["target_x"] != nil {
		t.Errorf("Context[target_x] = %v, want nil (JSON null)", md.Context["target_x"])
	}
}
