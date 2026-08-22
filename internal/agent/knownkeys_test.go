package agent

import (
	"encoding/json"
	"reflect"
	"testing"
)

// knownKeys is the definition of "modelled", and everything not in it is
// preserved verbatim. Get it wrong in one direction and a field is parsed *and*
// captured, so the emitter writes it twice; wrong in the other and a field is
// swallowed by the decoder and deleted on save.
//
// Both failures are silent, and neither shows up until someone adds a field to
// a raw struct — which is exactly when nobody is looking at this function. So
// the test is about the shapes a future field might have rather than about the
// three structs that exist today.
func TestKnownKeys_CoversEveryFieldEncodingJSONBinds(t *testing.T) {
	type embedded struct {
		Description string `json:"description"`
	}
	type future struct {
		embedded
		Tagged     string          `json:"tagged"`
		Omit       string          `json:"omit,omitempty"`
		NoName     json.RawMessage `json:",omitempty"` // the easy typo: binds as "NoName"
		Untagged   string          // binds as "Untagged"
		Skipped    string          `json:"-"`
		unexported string          //nolint:unused // encoding/json ignores it
	}

	got := knownKeys(reflect.TypeOf(future{}))

	// What encoding/json actually binds, established by round-tripping rather
	// than by reading the documentation.
	var probe future
	if err := json.Unmarshal([]byte(`{
		"description": "d", "tagged": "t", "omit": "o",
		"NoName": "n", "Untagged": "u", "-": "s"
	}`), &probe); err != nil {
		t.Fatalf("probe: %v", err)
	}
	bound := map[string]string{
		"description": probe.Description,
		"tagged":      probe.Tagged,
		"omit":        probe.Omit,
		"noname":      string(probe.NoName),
		"untagged":    probe.Untagged,
	}
	for key, value := range bound {
		if value == "" {
			t.Fatalf("the probe did not bind %q, so this test is not testing what it thinks", key)
		}
		if !got[key] {
			t.Errorf("knownKeys is missing %q, which encoding/json binds — it would be\n"+
				"parsed and captured as an extra, and emitted twice", key)
		}
	}
	if got["-"] || got["skipped"] {
		t.Error(`a json:"-" field must not be in the set: nothing binds it, so an input` +
			"\nkey by that name is genuinely unknown and has to be preserved")
	}
	if got["unexported"] {
		t.Error("an unexported field is not bound by encoding/json and must not be claimed")
	}
}

// The three sets the parser actually uses, checked against the fields they are
// derived from, so a rename shows up here rather than as a mangled file.
func TestKnownKeys_MatchTheParsersOwnStructs(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  map[string]bool
		want []string
	}{
		{"machine", machineKeys, []string{"id", "initial", "context", "states", "invoke"}},
		{"state", stateKeys, []string{"id", "type", "initial", "history", "target", "on", "entry", "exit", "after", "states", "invoke"}},
		{"transition", transitionKeys, []string{"target", "cond", "actions"}},
		{"spec", specKeys, []string{"type", "params"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.got) != len(tc.want) {
				t.Errorf("want %d keys %v, got %d %v", len(tc.want), tc.want, len(tc.got), tc.got)
			}
			for _, key := range tc.want {
				if !tc.got[key] {
					t.Errorf("missing %q", key)
				}
			}
		})
	}
}
