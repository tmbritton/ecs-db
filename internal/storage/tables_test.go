package storage

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openMemoryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enabling foreign_keys: %v", err)
	}
	return db
}

func columnNamesForTable(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query("SELECT name FROM pragma_table_info(?) ORDER BY cid", table)
	if err != nil {
		t.Fatalf("pragma_table_info(%q): %v", table, err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning column name: %v", err)
		}
		names = append(names, name)
	}
	return names
}

func TestEnsureInterpreterTables_CreatesAllThreeTables(t *testing.T) {
	db := openMemoryDB(t)
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	for _, table := range []string{"behavior_components", "transitions", "event_queue"} {
		if !tableExists(t, db, table) {
			t.Errorf("table %q not created", table)
		}
	}
}

func TestEnsureInterpreterTables_Idempotent(t *testing.T) {
	db := openMemoryDB(t)
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatalf("second call (idempotent): %v", err)
	}
}

func TestEnsureInterpreterTables_BehaviorComponents_Schema(t *testing.T) {
	db := openMemoryDB(t)
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	got := columnNamesForTable(t, db, "behavior_components")
	want := []string{"entity_id", "machine_id", "current_states", "updated_at"}
	if len(got) != len(want) {
		t.Fatalf("behavior_components columns = %v, want %v", got, want)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("column[%d] = %q, want %q", i, got[i], name)
		}
	}
}

func TestEnsureInterpreterTables_Transitions_Schema(t *testing.T) {
	db := openMemoryDB(t)
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	got := columnNamesForTable(t, db, "transitions")
	want := []string{"id", "tick", "wall_ms", "entity_id", "machine_id", "from_states", "to_states", "event", "cond_result", "actions_run"}
	if len(got) != len(want) {
		t.Fatalf("transitions columns = %v, want %v", got, want)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("column[%d] = %q, want %q", i, got[i], name)
		}
	}
}

func TestEnsureInterpreterTables_EventQueue_Schema(t *testing.T) {
	db := openMemoryDB(t)
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	got := columnNamesForTable(t, db, "event_queue")
	want := []string{"id", "entity_id", "machine_id", "event_type", "payload", "target_tick"}
	if len(got) != len(want) {
		t.Fatalf("event_queue columns = %v, want %v", got, want)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("column[%d] = %q, want %q", i, got[i], name)
		}
	}
}

func TestEnsureInterpreterTables_BehaviorComponents_CompositeKey(t *testing.T) {
	db := openMemoryDB(t)
	// Create a minimal entities table to satisfy the behavior_components FK.
	if _, err := db.Exec("CREATE TABLE entities (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("creating entities: %v", err)
	}
	if _, err := db.Exec("INSERT INTO entities VALUES (1)"); err != nil {
		t.Fatalf("inserting entity: %v", err)
	}
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}

	// Same entity_id, different machine_id — both inserts must succeed.
	_, err := db.Exec(`INSERT INTO behavior_components (entity_id, machine_id, current_states, updated_at) VALUES (1, 'primary', '["idle"]', 0)`)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err = db.Exec(`INSERT INTO behavior_components (entity_id, machine_id, current_states, updated_at) VALUES (1, 'burning', '["active"]', 0)`)
	if err != nil {
		t.Fatalf("second insert (same entity_id, different machine_id): %v", err)
	}

	// Duplicate (entity_id, machine_id) must be rejected.
	_, err = db.Exec(`INSERT INTO behavior_components (entity_id, machine_id, current_states, updated_at) VALUES (1, 'primary', '["running"]', 1)`)
	if err == nil {
		t.Fatal("expected UNIQUE constraint violation for duplicate (entity_id, machine_id), got nil")
	}
}
