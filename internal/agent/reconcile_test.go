package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	_ "modernc.org/sqlite"
)

func setupReconcileDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE entities (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		entity_type  TEXT NOT NULL,
		created_tick INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatalf("setup entities: %v", err)
	}
	// Inline the three interpreter-managed tables (mirrors storage.EnsureInterpreterTables).
	// We cannot import storage here because storage imports agent (cycle).
	interpreterTables := []string{
		`CREATE TABLE IF NOT EXISTS behavior_components (
			entity_id      INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
			machine_id     TEXT NOT NULL,
			current_states TEXT NOT NULL,
			updated_at     INTEGER NOT NULL,
			PRIMARY KEY (entity_id, machine_id)
		)`,
		`CREATE TABLE IF NOT EXISTS transitions (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			tick        INTEGER NOT NULL,
			wall_ms     INTEGER NOT NULL,
			entity_id   INTEGER NOT NULL,
			machine_id  TEXT NOT NULL,
			from_states TEXT NOT NULL,
			to_states   TEXT NOT NULL,
			event       TEXT NOT NULL,
			cond_result INTEGER,
			actions_run TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS event_queue (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_id   INTEGER NOT NULL,
			machine_id  TEXT NOT NULL,
			event_type  TEXT NOT NULL,
			payload     TEXT,
			target_tick INTEGER NOT NULL
		)`,
	}
	for _, stmt := range interpreterTables {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup interpreter tables: %v", err)
		}
	}
	return db
}

func insertEntity(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO entities (entity_type, created_tick) VALUES ('goblin', 1)`)
	if err != nil {
		t.Fatalf("insertEntity: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func insertBehavior(t *testing.T, db *sql.DB, entityID int64, machineID string, states []string) {
	t.Helper()
	data, _ := json.Marshal(states)
	if _, err := db.Exec(
		`INSERT INTO behavior_components (entity_id, machine_id, current_states, updated_at) VALUES (?, ?, ?, 1)`,
		entityID, machineID, string(data),
	); err != nil {
		t.Fatalf("insertBehavior: %v", err)
	}
}

func insertEventQueueRow(t *testing.T, db *sql.DB, entityID int64, machineID string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO event_queue (entity_id, machine_id, event_type, target_tick) VALUES (?, ?, 'after', 10)`,
		entityID, machineID,
	); err != nil {
		t.Fatalf("insertEventQueueRow: %v", err)
	}
}

func readCurrentStates(t *testing.T, db *sql.DB, entityID int64, machineID string) []string {
	t.Helper()
	var raw string
	if err := db.QueryRow(
		`SELECT current_states FROM behavior_components WHERE entity_id = ? AND machine_id = ?`,
		entityID, machineID,
	).Scan(&raw); err != nil {
		t.Fatalf("readCurrentStates: %v", err)
	}
	var states []string
	_ = json.Unmarshal([]byte(raw), &states)
	return states
}

func countEventQueue(t *testing.T, db *sql.DB, entityID int64, machineID string) int {
	t.Helper()
	var n int
	_ = db.QueryRow(
		`SELECT COUNT(*) FROM event_queue WHERE entity_id = ? AND machine_id = ?`,
		entityID, machineID,
	).Scan(&n)
	return n
}

func TestReconciler_ResetsEntityWithRemovedState(t *testing.T) {
	db := setupReconcileDB(t)
	entityID := insertEntity(t, db)
	insertBehavior(t, db, entityID, "m1", []string{"patrol"})
	insertEventQueueRow(t, db, entityID, "m1")

	r := &Reconciler{}
	if err := r.Reconcile(context.Background(), db, "m1", map[string]bool{"idle": true}, "idle"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if s := readCurrentStates(t, db, entityID, "m1"); len(s) != 1 || s[0] != "idle" {
		t.Errorf("current_states = %v, want [idle]", s)
	}
	if n := countEventQueue(t, db, entityID, "m1"); n != 0 {
		t.Errorf("event_queue count = %d, want 0 (stale timers must be cancelled)", n)
	}
}

func TestReconciler_NoopWhenAllStatesValid(t *testing.T) {
	db := setupReconcileDB(t)
	entityID := insertEntity(t, db)
	insertBehavior(t, db, entityID, "m1", []string{"idle"})
	insertEventQueueRow(t, db, entityID, "m1")

	r := &Reconciler{}
	if err := r.Reconcile(context.Background(), db, "m1", map[string]bool{"idle": true, "patrol": true}, "idle"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if s := readCurrentStates(t, db, entityID, "m1"); len(s) != 1 || s[0] != "idle" {
		t.Errorf("current_states = %v, want [idle] (unchanged)", s)
	}
	if n := countEventQueue(t, db, entityID, "m1"); n != 1 {
		t.Errorf("event_queue count = %d, want 1 (valid entity timers must be preserved)", n)
	}
}

func TestReconciler_ResetsOnlyInvalidEntities(t *testing.T) {
	db := setupReconcileDB(t)
	e1 := insertEntity(t, db)
	e2 := insertEntity(t, db)
	insertBehavior(t, db, e1, "m1", []string{"patrol"}) // removed
	insertBehavior(t, db, e2, "m1", []string{"idle"})   // valid
	insertEventQueueRow(t, db, e1, "m1")
	insertEventQueueRow(t, db, e2, "m1")

	r := &Reconciler{}
	if err := r.Reconcile(context.Background(), db, "m1", map[string]bool{"idle": true}, "idle"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if s := readCurrentStates(t, db, e1, "m1"); len(s) != 1 || s[0] != "idle" {
		t.Errorf("e1 current_states = %v, want [idle]", s)
	}
	if n := countEventQueue(t, db, e1, "m1"); n != 0 {
		t.Errorf("e1 event_queue count = %d, want 0", n)
	}
	if s := readCurrentStates(t, db, e2, "m1"); len(s) != 1 || s[0] != "idle" {
		t.Errorf("e2 current_states = %v, want [idle] (unchanged)", s)
	}
	if n := countEventQueue(t, db, e2, "m1"); n != 1 {
		t.Errorf("e2 event_queue count = %d, want 1 (must be preserved)", n)
	}
}

func TestReconciler_EmptyTableIsNoop(t *testing.T) {
	db := setupReconcileDB(t)
	r := &Reconciler{}
	if err := r.Reconcile(context.Background(), db, "m1", map[string]bool{"idle": true}, "idle"); err != nil {
		t.Fatalf("Reconcile on empty table: %v", err)
	}
}
