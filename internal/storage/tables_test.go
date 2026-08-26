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

// Every table the function creates, counted as well as named.
//
// The count is what makes the list a tripwire rather than decoration: a fourth
// table was added and this test — with its name saying three — passed, because
// naming three tables that do exist says nothing about a fourth.
func TestEnsureInterpreterTables_CreatesEveryTableItClaimsTo(t *testing.T) {
	db := openMemoryDB(t)
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	want := []string{"behavior_components", "event_queue", "spawns", "transitions"}
	for _, table := range want {
		if !tableExists(t, db, table) {
			t.Errorf("table %q not created", table)
		}
	}
	got := engineTables(t, db)
	if len(got) != len(want) {
		t.Errorf("the engine's tables are %v, and this test knows about %v", got, want)
	}
}

// engineTables is every table EnsureInterpreterTables is responsible for, which
// is every table in a database it was given that the schema did not build.
func engineTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'
		AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'comp\_%' ESCAPE '\'
		AND name NOT IN ('meta','world','entities','input_events')
		ORDER BY name`)
	if err != nil {
		t.Fatalf("listing tables: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		out = append(out, n)
	}
	return out
}

// The spawns table's shape, and the one thing about it that is a decision
// rather than a column: a spawned entity's death empties the reference and
// leaves the row, so the object is not spawned again on the next load.
func TestEnsureInterpreterTables_SpawnsReleasesADeadEntity(t *testing.T) {
	db := openMemoryDB(t)
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL)`,
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Goblin', 0)`,
		`INSERT INTO spawns (map, object_id, entity_id) VALUES ('level.tmx', 4, 1)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	if _, err := db.Exec(`DELETE FROM entities WHERE id = 1`); err != nil {
		t.Fatalf("deleting the entity: %v", err)
	}

	var count int
	var entityID sql.NullInt64
	if err := db.QueryRow(`SELECT COUNT(*), MAX(entity_id) FROM spawns`).Scan(&count, &entityID); err != nil {
		t.Fatalf("reading spawns: %v", err)
	}
	if count != 1 {
		t.Fatalf("%d spawn rows after the entity died, want 1 — the row cascaded and the object will spawn again", count)
	}
	if entityID.Valid {
		t.Errorf("the row still points at entity %d, which is gone", entityID.Int64)
	}
}

// Two maps may each hold an object 1; one map may not hold two.
func TestEnsureInterpreterTables_SpawnsAreKeyedByMapAndObject(t *testing.T) {
	db := openMemoryDB(t)
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	// The foreign key needs its target to exist even for a NULL, because this
	// database has foreign keys on and no entities table of its own.
	if _, err := db.Exec(
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL)`); err != nil {
		t.Fatalf("creating entities: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO spawns (map, object_id, entity_id) VALUES ('a.tmx', 1, NULL)`); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO spawns (map, object_id, entity_id) VALUES ('b.tmx', 1, NULL)`); err != nil {
		t.Errorf("a second map's object 1 was refused: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO spawns (map, object_id, entity_id) VALUES ('a.tmx', 1, NULL)`); err == nil {
		t.Error("one map held object 1 twice")
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
