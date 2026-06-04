package renderer

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbSchema := schema.DatabaseSchema{SchemaVersion: 1}
	store, err := storage.NewSQLiteStore(":memory:", dbSchema, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	return store.DB()
}

func insertEntity(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res, err := db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Goblin', 0)")
	if err != nil {
		t.Fatalf("insertEntity: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func insertBehaviorComponent(t *testing.T, db *sql.DB, entityID int64, machineID string, states []string) {
	t.Helper()
	raw, _ := json.Marshal(states)
	_, err := db.Exec(
		`INSERT INTO behavior_components (entity_id, machine_id, current_states, updated_at) VALUES (?, ?, ?, 0)`,
		entityID, machineID, string(raw),
	)
	if err != nil {
		t.Fatalf("insertBehaviorComponent: %v", err)
	}
}

func loadTestMachine(t *testing.T, loader *agent.Loader) {
	t.Helper()
	dir := t.TempDir()
	data := `{"id":"test-machine","initial":"idle","states":{"idle":{}}}`
	path := filepath.Join(dir, "test-machine.json")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("writing machine file: %v", err)
	}
	if _, err := loader.ScanDir(dir, "test"); err != nil {
		t.Fatalf("ScanDir: %v", err)
	}
}

// ── RunTick happy-path tests ──────────────────────────────────────────────────

func TestRunTick_AdvancesCurrentTick(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	ticker := newTicker(db, loader, registry)

	if err := ticker.RunTick(); err != nil {
		t.Fatalf("RunTick: %v", err)
	}

	var tick int64
	if err := db.QueryRow(`SELECT CAST(value AS INTEGER) FROM world WHERE key = 'current_tick'`).Scan(&tick); err != nil {
		t.Fatalf("reading current_tick: %v", err)
	}
	if tick != 1 {
		t.Errorf("current_tick = %d, want 1", tick)
	}
}

func TestRunTick_AdvancesWorldVersion(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	ticker := newTicker(db, loader, registry)

	for range 3 {
		if err := ticker.RunTick(); err != nil {
			t.Fatalf("RunTick: %v", err)
		}
	}

	var version int64
	if err := db.QueryRow(`SELECT CAST(value AS INTEGER) FROM world WHERE key = 'world_version'`).Scan(&version); err != nil {
		t.Fatalf("reading world_version: %v", err)
	}
	if version != 3 {
		t.Errorf("world_version = %d, want 3", version)
	}
}

// ── event_queue drain tests ───────────────────────────────────────────────────

func TestRunTick_DrainsDueEventQueueRows(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	ticker := newTicker(db, loader, registry)

	// Insert a due event (target_tick=0; current_tick starts at 0 so it's due immediately).
	if _, err := db.Exec(
		`INSERT INTO event_queue (entity_id, machine_id, event_type, target_tick) VALUES (99, 'none', 'SOME_EVENT', 0)`,
	); err != nil {
		t.Fatalf("inserting event_queue row: %v", err)
	}

	if err := ticker.RunTick(); err != nil {
		t.Fatalf("RunTick: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM event_queue`).Scan(&count); err != nil {
		t.Fatalf("counting event_queue: %v", err)
	}
	if count != 0 {
		t.Errorf("event_queue count = %d after drain, want 0", count)
	}
}

func TestRunTick_LeavesNonDueEventQueueRows(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	ticker := newTicker(db, loader, registry)

	// target_tick=100 — should not be drained on tick 0.
	if _, err := db.Exec(
		`INSERT INTO event_queue (entity_id, machine_id, event_type, target_tick) VALUES (99, 'none', 'FUTURE', 100)`,
	); err != nil {
		t.Fatalf("inserting event_queue row: %v", err)
	}

	if err := ticker.RunTick(); err != nil {
		t.Fatalf("RunTick: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM event_queue`).Scan(&count); err != nil {
		t.Fatalf("counting event_queue: %v", err)
	}
	if count != 1 {
		t.Errorf("event_queue count = %d, want 1 (non-due row retained)", count)
	}
}

// ── behavior_components path tests ───────────────────────────────────────────

func TestRunTick_BehaviorComponentUnknownMachine(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	ticker := newTicker(db, loader, registry)

	entityID := insertEntity(t, db)
	insertBehaviorComponent(t, db, entityID, "not-loaded", []string{"idle"})

	// RunTick should skip the unknown machine and still advance the tick.
	if err := ticker.RunTick(); err != nil {
		t.Fatalf("RunTick: %v", err)
	}

	var tick int64
	if err := db.QueryRow(`SELECT CAST(value AS INTEGER) FROM world WHERE key = 'current_tick'`).Scan(&tick); err != nil {
		t.Fatalf("reading current_tick: %v", err)
	}
	if tick != 1 {
		t.Errorf("current_tick = %d, want 1", tick)
	}
}

func TestRunTick_BehaviorComponentBadJSON(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	ticker := newTicker(db, loader, registry)

	entityID := insertEntity(t, db)
	// Insert raw bad JSON directly.
	if _, err := db.Exec(
		`INSERT INTO behavior_components (entity_id, machine_id, current_states, updated_at) VALUES (?, 'test-machine', 'not-json', 0)`,
		entityID,
	); err != nil {
		t.Fatalf("inserting bad behavior_component: %v", err)
	}
	loadTestMachine(t, loader)

	// Should log and skip without returning an error.
	if err := ticker.RunTick(); err != nil {
		t.Fatalf("RunTick with bad JSON: %v", err)
	}
}

func TestRunTick_DeliversTICKToBehaviorComponent(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	ticker := newTicker(db, loader, registry)

	entityID := insertEntity(t, db)
	insertBehaviorComponent(t, db, entityID, "test-machine", []string{"idle"})
	loadTestMachine(t, loader)

	// RunTick should deliver TICK, find no transition, and complete cleanly.
	if err := ticker.RunTick(); err != nil {
		t.Fatalf("RunTick: %v", err)
	}

	var tick int64
	if err := db.QueryRow(`SELECT CAST(value AS INTEGER) FROM world WHERE key = 'current_tick'`).Scan(&tick); err != nil {
		t.Fatalf("reading current_tick: %v", err)
	}
	if tick != 1 {
		t.Errorf("current_tick = %d, want 1", tick)
	}
}

// ── loadAgentFromDB tests ─────────────────────────────────────────────────────

func TestLoadAgentFromDB_NilWhenNoDefinition(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	ticker := newTicker(db, loader, registry)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	got, err := ticker.loadAgentFromDB(tx, 1, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("want nil agent for unknown machine, got %+v", got)
	}
}

func TestLoadAgentFromDB_NilWhenNoRow(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	ticker := newTicker(db, loader, registry)
	loadTestMachine(t, loader)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	got, err := ticker.loadAgentFromDB(tx, 999, "test-machine")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("want nil for missing behavior_components row, got %+v", got)
	}
}

func TestLoadAgentFromDB_ReconstructsAgent(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	ticker := newTicker(db, loader, registry)
	loadTestMachine(t, loader)

	entityID := insertEntity(t, db)
	// State IDs in behavior_components use the fully-qualified form: machineID + "." + stateName.
	insertBehaviorComponent(t, db, entityID, "test-machine", []string{"test-machine.idle"})

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	got, err := ticker.loadAgentFromDB(tx, entityID, "test-machine")
	if err != nil {
		t.Fatalf("loadAgentFromDB: %v", err)
	}
	if got == nil {
		t.Fatal("want agent, got nil")
	}
	if got.EntityID != entityID {
		t.Errorf("EntityID = %d, want %d", got.EntityID, entityID)
	}
	if len(got.Configuration) != 1 || got.Configuration[0].ID != "test-machine.idle" {
		t.Errorf("Configuration[0].ID = %q, want %q", got.Configuration[0].ID, "test-machine.idle")
	}
}
