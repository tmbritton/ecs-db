package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

func wiringSchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {Type: "object", Properties: map[string]schema.Property{"x": {Type: "number"}}},
		},
		EntityTypes: map[string]schema.EntityType{
			"Thing": {RequiredComponents: []string{"Position"}, ValidationLevel: schema.ValidationStrict},
		},
	}
}

// The scenario the reconciler exists for, end to end through the hot-reload
// path: an entity is sitting in a state, the machine is reloaded without that
// state, and the entity must not be left pointing at something that no longer
// exists.
//
// It is reachable through the UI for the first time in this epic — deleting a
// state is one click on Epic 13's canvas — which is why the callback that had
// been nil since Epic 4 is finally wired.
func TestReconcileOnReload_ResetsAnEntityWhoseStateWasDeleted(t *testing.T) {
	db := setupReconcileDB(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "walker.json")

	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("writing machine: %v", err)
		}
	}
	write(`{"id":"walker","initial":"idle","states":{"idle":{"on":{"GO":"chasing"}},"chasing":{}}}`)

	loader := NewLoader(NewRegistry(), wiringSchema())
	if _, err := loader.ScanDir(dir, "core"); err != nil {
		t.Fatalf("ScanDir: %v", err)
	}

	// An entity mid-chase.
	if _, err := db.Exec(`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`); err != nil {
		t.Fatalf("seeding entity: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO behavior_components (entity_id, machine_id, current_states, updated_at) VALUES (1, 'walker', ?, 1)`,
		`["chasing"]`,
	); err != nil {
		t.Fatalf("seeding behavior: %v", err)
	}

	reconcile := ReconcileOnReload(context.Background(), db, loader, &Reconciler{})

	// The state the entity is in is deleted and the file saved.
	write(`{"id":"walker","initial":"idle","states":{"idle":{}}}`)
	if err := loader.ReloadFile(path, "core", reconcile); err != nil {
		t.Fatalf("ReloadFile: %v", err)
	}

	var raw string
	if err := db.QueryRow(
		`SELECT current_states FROM behavior_components WHERE entity_id = 1 AND machine_id = 'walker'`,
	).Scan(&raw); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	var states []string
	if err := json.Unmarshal([]byte(raw), &states); err != nil {
		t.Fatalf("parsing current_states: %v", err)
	}
	if len(states) != 1 || states[0] != "idle" {
		t.Errorf("current_states = %v, want the machine's initial state — the entity was left pointing at a state that no longer exists", states)
	}
}

// An entity whose state survives the reload must be left alone: resetting it
// would throw away whatever it was in the middle of.
func TestReconcileOnReload_LeavesAValidEntityAlone(t *testing.T) {
	db := setupReconcileDB(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "walker.json")

	body := `{"id":"walker","initial":"idle","states":{"idle":{},"chasing":{}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	loader := NewLoader(NewRegistry(), wiringSchema())
	if _, err := loader.ScanDir(dir, "core"); err != nil {
		t.Fatalf("ScanDir: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`); err != nil {
		t.Fatalf("seeding entity: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO behavior_components (entity_id, machine_id, current_states, updated_at) VALUES (1, 'walker', ?, 1)`,
		`["chasing"]`,
	); err != nil {
		t.Fatalf("seeding behavior: %v", err)
	}

	reconcile := ReconcileOnReload(context.Background(), db, loader, &Reconciler{})
	if err := loader.ReloadFile(path, "core", reconcile); err != nil {
		t.Fatalf("ReloadFile: %v", err)
	}

	var raw string
	if err := db.QueryRow(
		`SELECT current_states FROM behavior_components WHERE entity_id = 1 AND machine_id = 'walker'`,
	).Scan(&raw); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if raw != `["chasing"]` {
		t.Errorf("current_states = %s, want the entity left in chasing", raw)
	}
}

// The adapter must not panic on a machine the loader does not know, and must
// survive a database error — a hot-reload callback that panicked would take the
// watcher goroutine with it.
func TestReconcileOnReload_SurvivesTheAwkwardCases(t *testing.T) {
	db := setupReconcileDB(t)
	loader := NewLoader(NewRegistry(), wiringSchema())
	reconcile := ReconcileOnReload(context.Background(), db, loader, &Reconciler{})

	t.Run("unknown machine", func(t *testing.T) {
		reconcile("never-loaded", map[string]bool{"idle": true})
	})

	t.Run("closed database", func(t *testing.T) {
		closed := setupReconcileDB(t)
		if err := closed.Close(); err != nil {
			t.Fatalf("closing: %v", err)
		}
		fn := ReconcileOnReload(context.Background(), closed, loader, &Reconciler{})
		fn("anything", map[string]bool{"idle": true})
	})

	t.Run("nil reconciler is not wired", func(t *testing.T) {
		if fn := ReconcileOnReload(context.Background(), db, loader, nil); fn != nil {
			t.Error("a nil Reconciler should yield no callback rather than one that panics")
		}
	})
}

// A machine can be saved with every state deleted: ValidateMachine only demands
// an initial when there are children, so `{"states":{}}` passes and Initial is
// empty. Resetting entities to it would write [""] into current_states —
// leaving them pointing at a state that does not exist, which is exactly the
// corruption this callback exists to prevent.
func TestReconcileOnReload_RefusesToResetToNowhere(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "every state deleted",
			body: `{"id":"walker","states":{}}`,
		},
		{
			// Belt and braces: an initial naming something not in the valid set
			// is rejected by validation today, but the guard must not depend on
			// that staying true.
			name: "initial is not among the valid states",
			body: `{"id":"walker","initial":"idle","states":{"idle":{}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupReconcileDB(t)
			dir := t.TempDir()
			path := filepath.Join(dir, "walker.json")

			if err := os.WriteFile(path, []byte(
				`{"id":"walker","initial":"idle","states":{"idle":{},"chasing":{}}}`), 0o600); err != nil {
				t.Fatalf("writing: %v", err)
			}
			loader := NewLoader(NewRegistry(), wiringSchema())
			if _, err := loader.ScanDir(dir, "core"); err != nil {
				t.Fatalf("ScanDir: %v", err)
			}
			if _, err := db.Exec(`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`); err != nil {
				t.Fatalf("seeding entity: %v", err)
			}
			if _, err := db.Exec(
				`INSERT INTO behavior_components (entity_id, machine_id, current_states, updated_at) VALUES (1, 'walker', ?, 1)`,
				`["chasing"]`,
			); err != nil {
				t.Fatalf("seeding behavior: %v", err)
			}

			reconcile := ReconcileOnReload(context.Background(), db, loader, &Reconciler{})

			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatalf("rewriting: %v", err)
			}
			// The reload may fail validation for the second case; either way the
			// callback must not corrupt anything.
			_ = loader.ReloadFile(path, "core", reconcile)
			// And drive the callback directly, so the guard is exercised even
			// when the reload never reaches it.
			reconcile("walker", map[string]bool{})

			var raw string
			if err := db.QueryRow(
				`SELECT current_states FROM behavior_components WHERE entity_id = 1 AND machine_id = 'walker'`,
			).Scan(&raw); err != nil {
				t.Fatalf("reading back: %v", err)
			}
			var states []string
			if err := json.Unmarshal([]byte(raw), &states); err != nil {
				t.Fatalf("parsing: %v", err)
			}
			for _, st := range states {
				if st == "" {
					t.Errorf("current_states = %v — the entity was reset to a state that does not exist", states)
				}
			}
		})
	}
}
