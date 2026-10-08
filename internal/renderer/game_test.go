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

type mapScopedInputHandler struct{ playerID int64 }

func (h *mapScopedInputHandler) Handle(_ []agent.InputEvent, _ agent.WorldWriter, reader agent.WorldReader) error {
	id, err := reader.FindEntityByType("Player")
	h.playerID = id
	return err
}

func TestTicker_RunTickResolvesActiveMapPlayer(t *testing.T) {
	db := newTestDB(t)
	for _, stmt := range []string{
		`CREATE TABLE comp_tilelayer (entity_id INTEGER PRIMARY KEY, map_id TEXT NOT NULL)`,
		`CREATE TABLE comp_tileentityowner (entity_id INTEGER PRIMARY KEY, target_entity_id INTEGER NOT NULL)`,
		`INSERT INTO entities (entity_type, created_tick) VALUES ('Player',0),('Player',0)`,
		`INSERT INTO spawns (map, object_id, entity_id) VALUES ('map-a',1,1),('map-b',1,2)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	ticker := newTicker(db, loader, registry)
	ticker.SetMapID("map-b")
	handler := &mapScopedInputHandler{}
	ticker.SetInputHandler(handler)
	if err := ticker.RunTick(); err != nil {
		t.Fatal(err)
	}
	if handler.playerID != 2 {
		t.Fatalf("tick chose Player %d, want map-b's Player 2", handler.playerID)
	}
}

type mapTickMark struct{ ids []int64 }

func (m *mapTickMark) Run(ctx agent.ActionContext) error {
	m.ids = append(m.ids, ctx.EntityID)
	return nil
}

func TestTicker_MapScopeLeavesForeignEventsAndBehaviorsAlone(t *testing.T) {
	db := newTestDB(t)
	for _, stmt := range []string{
		`CREATE TABLE comp_tilelayer (entity_id INTEGER PRIMARY KEY, map_id TEXT NOT NULL)`,
		`CREATE TABLE comp_tileentityowner (entity_id INTEGER PRIMARY KEY, target_entity_id INTEGER NOT NULL)`,
		`CREATE TABLE comp_tilereferences (entity_id INTEGER PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO entities (entity_type, created_tick) VALUES ('Goblin',0),('Goblin',0),('Goblin',0),('Tile',0),('Floor',0),('Tile',0),('Floor',0)`,
		`INSERT INTO spawns (map, object_id, entity_id) VALUES ('map-a',1,1),('map-b',1,2)`,
		`INSERT INTO comp_tilelayer VALUES (4,'map-a'),(6,'map-b')`,
		`INSERT INTO comp_tileentityowner VALUES (5,4),(7,6)`,
		`INSERT INTO comp_tilereferences VALUES (4,'[3]'),(6,'[]')`,
		`INSERT INTO event_queue (entity_id, machine_id, event_type, target_tick) VALUES (1,'map-tick','FOREIGN',0),(2,'map-tick','LOCAL',0),(3,'map-tick','LINKED_RUNTIME',0),(5,'map-tick','FOREIGN_ART',0),(7,'map-tick','LOCAL_ART',0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	registry := builtins.NewRegistry()
	mark := &mapTickMark{}
	registry.RegisterAction(agent.ActionMeta{Name: "mark"}, mark)
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	dir := t.TempDir()
	machine := `{"id":"map-tick","initial":"idle","states":{"idle":{"on":{"TICK":[{"actions":[{"type":"mark"}]}]}}}}`
	if err := os.WriteFile(filepath.Join(dir, "map-tick.json"), []byte(machine), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.ScanDir(dir, "test"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 2, 3, 5, 7} {
		insertBehaviorComponent(t, db, id, "map-tick", []string{"idle"})
	}
	ticker := newTicker(db, loader, registry)
	ticker.SetMapID("map-b")
	if err := ticker.RunTick(); err != nil {
		t.Fatal(err)
	}
	var foreign int
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_queue WHERE entity_id=1`).Scan(&foreign); err != nil || foreign != 1 {
		t.Fatalf("foreign map's due event was consumed: count=%d, %v", foreign, err)
	}
	var local int
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_queue WHERE entity_id=2`).Scan(&local); err != nil || local != 0 {
		t.Fatalf("active map's due event remains: count=%d, %v", local, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_queue WHERE entity_id=5`).Scan(&foreign); err != nil || foreign != 1 {
		t.Fatalf("foreign owned entity's due event was consumed: %d,%v", foreign, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_queue WHERE entity_id=3`).Scan(&foreign); err != nil || foreign != 1 {
		t.Fatalf("other map's linked runtime entity consumed its event: %d,%v", foreign, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_queue WHERE entity_id=7`).Scan(&local); err != nil || local != 0 {
		t.Fatalf("active owned entity's due event remains: %d,%v", local, err)
	}
	seen := map[int64]bool{}
	for _, id := range mark.ids {
		seen[id] = true
	}
	if seen[1] || seen[3] || seen[5] || !seen[2] || !seen[7] {
		t.Fatalf("TICK reached %+v, want active-map entities without foreign linked runtime", mark.ids)
	}
	ticker.SetMapID("map-a")
	if err := ticker.RunTick(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_queue WHERE entity_id=3`).Scan(&local); err != nil || local != 0 {
		t.Fatalf("map-a did not consume its linked runtime entity's event: %d,%v", local, err)
	}
	linkedTicked := false
	for _, id := range mark.ids {
		linkedTicked = linkedTicked || id == 3
	}
	if !linkedTicked {
		t.Fatalf("map-a did not TICK its linked runtime entity: %v", mark.ids)
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
