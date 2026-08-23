package agent

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// collides has two states called "idle" in different branches, a nested state,
// and a state carrying an authored id — the three ways StateNode.ID fails to
// name a node.
const collides = `{
  "id": "m",
  "initial": "idle",
  "states": {
    "idle": { "entry": ["noSuchAction"] },
    "combat": {
      "initial": "attacking",
      "states": {
        "attacking": { "entry": ["noSuchAction"] },
        "idle": { "entry": ["noSuchAction"] }
      }
    },
    "named": { "id": "hand-written", "entry": ["noSuchAction"] }
  }
}`

func parseFor(t *testing.T, src string) *MachineDefinition {
	t.Helper()
	def, err := ParseMachine([]byte(src))
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	return def
}

// StateID cannot name a node: it is machineID + the state's *leaf* name, so a
// nested state's id is not its path, two states in different branches share one
// id, and an authored id replaces it outright. An editor attaching a message to
// the thing that caused it needs the path, and the walk that makes the error
// already knows it.
func TestValidateMachine_ErrorsCarryTheStatePath(t *testing.T) {
	errs := ValidateMachine(parseFor(t, collides), NewRegistry(), schema.DatabaseSchema{})

	got := map[string]string{}
	for _, e := range errs {
		if !strings.Contains(e.Message, "not registered") {
			continue
		}
		got[e.StatePath] = e.StateID
	}
	for path, wantID := range map[string]string{
		"idle":             "m.idle",
		"combat.attacking": "m.attacking",
		"combat.idle":      "m.idle",
		"named":            "hand-written",
	} {
		id, ok := got[path]
		if !ok {
			t.Errorf("no error carries the path %q; got %v", path, got)
			continue
		}
		if id != wantID {
			t.Errorf("path %q carries StateID %q, want %q", path, id, wantID)
		}
	}
	// The two "idle" states are one StateID and two paths, which is the whole
	// reason the path is carried.
	if got["idle"] != got["combat.idle"] {
		t.Errorf("the fixture no longer collides: %q vs %q", got["idle"], got["combat.idle"])
	}
}

// A machine-level error belongs to no state and says so.
func TestValidateMachine_MachineLevelErrorsCarryNoPath(t *testing.T) {
	def := parseFor(t, `{"id": "m", "states": {"a": {}}}`)
	errs := ValidateMachine(def, NewRegistry(), schema.DatabaseSchema{})

	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "machine has child states") {
			found = true
			if e.StatePath != "" {
				t.Errorf("a machine-level error is hung on state %q", e.StatePath)
			}
		}
	}
	if !found {
		t.Fatal("the fixture produces no machine-level error")
	}
}

// An initial naming something that is not a child is the machine's fault when
// the machine declares it, and a state's when a state does. The first belongs
// to no node.
func TestValidateMachine_AMachinesOwnInitialErrorCarriesNoPath(t *testing.T) {
	def := parseFor(t, `{"id": "m", "initial": "nowhere", "states": {"a": {}}}`)

	found := false
	for _, e := range ValidateMachine(def, NewRegistry(), schema.DatabaseSchema{}) {
		if !strings.Contains(e.Message, "is not a child state") {
			continue
		}
		found = true
		if e.StatePath != "" {
			t.Errorf("the machine's own initial error is hung on state %q", e.StatePath)
		}
	}
	if !found {
		t.Fatal("the fixture produces no initial error")
	}
}

// A compound state's children are walked in the file's order too — the same
// reason its parent's siblings are, and a separate loop that could forget.
func TestValidateMachine_ReportsAStatesChildrenInTheFilesOrder(t *testing.T) {
	src := `{
	  "id": "m",
	  "initial": "outer",
	  "states": {
	    "outer": { "initial": "zeta", "states": {
	      "zeta":  { "entry": ["noSuchAction"] },
	      "alpha": { "entry": ["noSuchAction"] },
	      "mid":   { "entry": ["noSuchAction"] }
	    } }
	  }
	}`
	want := "outer.zeta,outer.alpha,outer.mid"

	for run := 0; run < 8; run++ {
		var got []string
		for _, e := range ValidateMachine(parseFor(t, src), NewRegistry(), schema.DatabaseSchema{}) {
			if strings.Contains(e.Message, "not registered") {
				got = append(got, e.StatePath)
			}
		}
		if strings.Join(got, ",") != want {
			t.Fatalf("run %d: %v, want %s", run, got, want)
		}
	}
}

// A transition's error belongs to the state that declares it, not to the state
// it fails to reach.
func TestValidateMachine_ATransitionsErrorCarriesItsSource(t *testing.T) {
	def := parseFor(t, `{
	  "id": "m",
	  "initial": "a",
	  "states": {
	    "a": { "initial": "b", "states": { "b": { "on": { "GO": [{ "target": "nowhere" }] } } } }
	  }
	}`)

	found := false
	for _, e := range ValidateMachine(def, NewRegistry(), schema.DatabaseSchema{}) {
		if !strings.Contains(e.Message, "not a known state") {
			continue
		}
		found = true
		if e.StatePath != "a.b" {
			t.Errorf("the target error is hung on %q, want a.b", e.StatePath)
		}
	}
	// Without this the test passes by finding nothing, which is what reworded
	// the message would do to it.
	if !found {
		t.Fatal("the fixture produces no target error")
	}
}

// Two runs must agree, and in the file's order: this list is rendered on a
// two-second stream that suppresses a patch when the markup is unchanged, so a
// list that reshuffles by itself both flickers and defeats the suppression.
func TestValidateMachine_ReportsInTheFilesOrder(t *testing.T) {
	src := `{
	  "id": "m",
	  "initial": "zeta",
	  "states": {
	    "zeta":  { "entry": ["noSuchAction"] },
	    "alpha": { "entry": ["noSuchAction"] },
	    "mid":   { "initial": "inner", "states": { "inner": { "entry": ["noSuchAction"] } } }
	  }
	}`
	want := []string{"zeta", "alpha", "mid.inner"}

	for run := 0; run < 8; run++ {
		var got []string
		for _, e := range ValidateMachine(parseFor(t, src), NewRegistry(), schema.DatabaseSchema{}) {
			if strings.Contains(e.Message, "not registered") {
				got = append(got, e.StatePath)
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("run %d: %v, want %v", run, got, want)
		}
	}
}

// The events on one state are in the file's order too, for the same reason.
func TestValidateMachine_ReportsAStatesEventsInTheFilesOrder(t *testing.T) {
	src := `{
	  "id": "m",
	  "initial": "a",
	  "states": {
	    "a": { "on": {
	      "ZED":   [{ "target": "nowhere1" }],
	      "ALPHA": [{ "target": "nowhere2" }],
	      "MID":   [{ "target": "nowhere3" }]
	    } },
	    "b": {}
	  }
	}`
	want := "nowhere1,nowhere2,nowhere3"

	for run := 0; run < 8; run++ {
		var got []string
		for _, e := range ValidateMachine(parseFor(t, src), NewRegistry(), schema.DatabaseSchema{}) {
			if strings.Contains(e.Message, "not a known state") {
				got = append(got, e.Field)
			}
		}
		if strings.Join(got, ",") != want {
			t.Fatalf("run %d: %v, want %s", run, got, want)
		}
	}
}

// Context keys are the fifth map the walk ranges, and the one it forgot. Their
// errors belong to the machine and are rendered as a list beside it, on a
// stream that patches an element only when its markup changed — so an order
// that differs between two runs over one machine re-patches the whole mode
// forever, taking the inputs someone is typing in with it.
func TestValidateMachine_ReportsContextKeysInTheFilesOrder(t *testing.T) {
	src := `{
	  "id": "m",
	  "initial": "a",
	  "context": { "zed": 0, "alpha": 0, "mid": 0, "beta": 0, "gamma": 0 },
	  "states": { "a": {} }
	}`
	want := "zed,alpha,mid,beta,gamma"

	for run := 0; run < 8; run++ {
		var got []string
		for _, e := range ValidateMachine(parseFor(t, src), NewRegistry(), schema.DatabaseSchema{}) {
			if strings.Contains(e.Message, "context key") {
				got = append(got, e.Field)
			}
		}
		if strings.Join(got, ",") != want {
			t.Fatalf("run %d: %v, want %s", run, got, want)
		}
	}
}

// What an error is about, structurally, so an editor can place it without
// reading the message. Field alone cannot say: an entry action and a
// transition action are both an action type on the same state.
func TestValidateMachine_ErrorsSayWhatTheyAreAbout(t *testing.T) {
	def := parseFor(t, `{
	  "id": "m",
	  "initial": "a",
	  "context": { "stray": 0 },
	  "states": {
	    "a": {
	      "entry": ["boom"],
	      "on": { "GO": [{ "target": "b", "actions": ["boom"], "cond": "noSuchGuard" }] },
	      "after": { "soon": [{ "target": "b" }] }
	    },
	    "b": { "initial": "nope", "states": { "c": {} } }
	  }
	}`)

	got := map[string]ErrorOrigin{}
	for _, e := range ValidateMachine(def, NewRegistry(), schema.DatabaseSchema{}) {
		got[e.Message] = e.Origin
	}
	for message, want := range map[string]ErrorOrigin{
		`context key "stray" does not match any component field`: OriginMachine,
		`entry action "boom" is not registered`:                  OriginState,
		`initial state "nope" is not a child state`:              OriginState,
		`transition action "boom" is not registered`:             OriginTransition,
		`guard "noSuchGuard" is not registered`:                  OriginTransition,
	} {
		if origin, ok := got[message]; !ok {
			t.Errorf("no error saying %q; got %v", message, got)
		} else if origin != want {
			t.Errorf("%q is about %v, want %v", message, origin, want)
		}
	}
	// The two "boom" errors are the same Field on the same state and belong to
	// different things, which is the whole reason Origin exists.
	if got[`entry action "boom" is not registered`] == got[`transition action "boom" is not registered`] {
		t.Error("an entry action and a transition action are indistinguishable")
	}
	// An after duration keys a transition, so it belongs to the edge drawn for
	// it rather than to the state that declares the map.
	for _, e := range ValidateMachine(def, NewRegistry(), schema.DatabaseSchema{}) {
		if strings.Contains(e.Message, "after duration") && e.Origin != OriginTransition {
			t.Errorf("an after duration is about %v, want a transition", e.Origin)
		}
	}
}
