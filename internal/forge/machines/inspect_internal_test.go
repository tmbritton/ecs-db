package machines

// White-box, and it has to be.
//
// agent.ValidateMachine writes ContextManifest onto whatever it validates, so
// Inspect validates a clone rather than the session's own working value. From
// outside the package that guard is invisible: every way out of the session —
// Working, Read, Dirty — goes through EmitMachine, which does not serialise
// ContextManifest, so the mutation cannot be observed and a test written from
// outside passes whether the clone is there or not. The only place the
// difference exists is s.files[path].Current, which is what this reaches.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/schema"
)

const inspectWander = `{
  "id": "wander",
  "initial": "idle",
  "context": {
    "hp": 0
  },
  "states": {
    "idle": {}
  }
}
`

func TestInspect_LeavesTheSessionsOwnValueAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "behaviors")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "wander.json")
	if err := os.WriteFile(path, []byte(inspectWander), 0o600); err != nil {
		t.Fatal(err)
	}
	current := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Health": {
				Type:          schema.ComponentTypeObject,
				Properties:    map[string]schema.Property{"hp": {Type: schema.PropertyTypeInteger}},
				PropertyOrder: []string{"hp"},
			},
		},
		ComponentOrder: []string{"Health"},
	}
	s, err := Open(Config{
		Mods:   []project.Mod{{Name: "core", Behaviors: dir}},
		Schema: func() schema.DatabaseSchema { return current },
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	got, err := s.Inspect(path)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !got.Computed || got.Manifest["hp"] != "Health" {
		t.Fatalf("the fixture does not exercise the manifest: %+v", got)
	}

	// The value the session holds is untouched: validating wrote the manifest
	// onto the copy Inspect made, not onto what is being edited.
	if held := s.files[path].Current; held.ContextManifest != nil {
		t.Errorf("Inspect wrote a manifest onto the session's working value: %v", held.ContextManifest)
	}
	if s.files[path].Current == got.Definition {
		t.Error("Inspect handed back the session's own value rather than a copy")
	}
}
