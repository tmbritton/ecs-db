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
// rather than a column: a spawned entity's death takes its row, so the object
// looks unspawned and the file puts it back on the next load.
//
// It was SET NULL and is now CASCADE. The row used to be a record that an import
// had happened, kept so a dead spawn would not return; under the file-wins rule
// a dead spawn does return, because the file still says there is a goblin there,
// and a row whose entity is gone has nothing left to say.
func TestEnsureInterpreterTables_ASpawnRowDiesWithItsEntity(t *testing.T) {
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
	if err := db.QueryRow(`SELECT COUNT(*) FROM spawns`).Scan(&count); err != nil {
		t.Fatalf("reading spawns: %v", err)
	}
	if count != 0 {
		t.Errorf("%d spawn rows after the entity died, want none — a row that points at nothing "+
			"would make the object look spawned and stop the file putting it back", count)
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

// A database created before the spawns table's foreign key changed is migrated,
// not left on the old one.
//
// EnsureInterpreterTables is CREATE TABLE IF NOT EXISTS, so changing the DDL
// changes nothing about a database that already exists. The column went from
// ON DELETE SET NULL to ON DELETE CASCADE when the map re-import rule changed,
// and a database left on the old one is not merely stale: a row whose entity
// died holds a NULL, the importer reads that as "not spawned", and its insert
// collides with the primary key the row still occupies — refusing the object on
// every load, forever.
func TestEnsureInterpreterTables_MigratesTheOldSpawnsForeignKey(t *testing.T) {
	db := openMemoryDB(t)
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL)`,
		// The table exactly as the previous version created it.
		`CREATE TABLE spawns (
			map       TEXT NOT NULL,
			object_id INTEGER NOT NULL,
			entity_id INTEGER REFERENCES entities(id) ON DELETE SET NULL,
			PRIMARY KEY (map, object_id)
		)`,
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Goblin', 0)`,
		`INSERT INTO spawns (map, object_id, entity_id) VALUES ('level.tmx', 1, 1)`,
		// And a row whose entity died under the old rule.
		`INSERT INTO spawns (map, object_id, entity_id) VALUES ('level.tmx', 2, NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}

	var onDelete string
	if err := db.QueryRow(
		`SELECT on_delete FROM pragma_foreign_key_list('spawns') WHERE "from" = 'entity_id'`).
		Scan(&onDelete); err != nil {
		t.Fatalf("reading the foreign key: %v", err)
	}
	if onDelete != "CASCADE" {
		t.Errorf("the foreign key is %q, want CASCADE", onDelete)
	}

	// The live row came across; the one pointing at nothing did not, because
	// under the rule now the file should put that object back.
	var kept, orphans int
	if err := db.QueryRow(`SELECT COUNT(*) FROM spawns WHERE entity_id = 1`).Scan(&kept); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if kept != 1 {
		t.Errorf("%d live spawn rows survived the migration, want 1", kept)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM spawns WHERE entity_id IS NULL`).Scan(&orphans); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if orphans != 0 {
		t.Errorf("%d rows pointing at nothing survived, want none — their objects would never respawn", orphans)
	}

	// And the new key works.
	if _, err := db.Exec(`DELETE FROM entities WHERE id = 1`); err != nil {
		t.Fatalf("deleting: %v", err)
	}
	var left int
	if err := db.QueryRow(`SELECT COUNT(*) FROM spawns`).Scan(&left); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if left != 0 {
		t.Errorf("%d rows after the entity died, want none", left)
	}
}

// Running it again on an already-migrated database does nothing.
func TestEnsureInterpreterTables_TheSpawnsMigrationIsIdempotent(t *testing.T) {
	db := openMemoryDB(t)
	if _, err := db.Exec(
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL)`); err != nil {
		t.Fatalf("creating entities: %v", err)
	}
	for range 3 {
		if err := EnsureInterpreterTables(db); err != nil {
			t.Fatalf("EnsureInterpreterTables: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO spawns (map, object_id, entity_id) VALUES ('a', 1, NULL)`); err != nil {
		t.Errorf("the table is unusable after three calls: %v", err)
	}
}

// The migration is skipped on a table that is already right, rather than
// rebuilt on every start.
//
// Rebuilding is not merely wasteful: it drops and recreates the table, so
// anything holding a reference to it is invalidated, and it runs with foreign
// keys off — which is a window nobody should reopen once per launch.
func TestEnsureInterpreterTables_DoesNotRebuildASpawnsTableThatIsAlreadyRight(t *testing.T) {
	db := openMemoryDB(t)
	if _, err := db.Exec(
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL)`); err != nil {
		t.Fatalf("creating entities: %v", err)
	}
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}

	// The table's identity, which a drop-and-recreate changes and an untouched
	// table keeps.
	var before int
	if err := db.QueryRow(`SELECT rootpage FROM sqlite_master WHERE name = 'spawns'`).Scan(&before); err != nil {
		t.Fatalf("reading the table's rootpage: %v", err)
	}

	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatalf("second call: %v", err)
	}

	var after int
	if err := db.QueryRow(`SELECT rootpage FROM sqlite_master WHERE name = 'spawns'`).Scan(&after); err != nil {
		t.Fatalf("reading the table's rootpage: %v", err)
	}
	if after != before {
		t.Errorf("the spawns table was rebuilt on a call that had nothing to migrate (rootpage %d → %d)",
			before, after)
	}
}

func TestEnsureInterpreterTables_RecordsAuthoredComponentsOnAnExistingDatabase(t *testing.T) {
	db := openMemoryDB(t)
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL)`,
		`INSERT INTO entities VALUES (1, 'Goblin', 0)`,
		`CREATE TABLE spawns (map TEXT NOT NULL, object_id INTEGER NOT NULL,
			entity_id INTEGER REFERENCES entities(id) ON DELETE CASCADE,
			PRIMARY KEY (map, object_id))`,
		`INSERT INTO spawns VALUES ('level', 7, 1)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatal(err)
	}
	var authored sql.NullString
	if err := db.QueryRow(`SELECT components FROM spawns WHERE map = 'level' AND object_id = 7`).Scan(&authored); err != nil {
		t.Fatalf("old row or new column missing: %v", err)
	}
	if authored.Valid {
		t.Errorf("an old row has no known authored set, got %q", authored.String)
	}
	if _, err := db.Exec(`UPDATE spawns SET components = '["Position","Health"]' WHERE object_id = 7`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT components FROM spawns WHERE object_id = 7`).Scan(&authored); err != nil || authored.String != `["Position","Health"]` {
		t.Errorf("reopen lost the authored set: %+v, %v", authored, err)
	}
}

func TestTx_SetSpawnComponentsPersistsDeterministicAuthoredSet(t *testing.T) {
	db := openMemoryDB(t)
	if _, err := db.Exec(`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO entities VALUES (1, 'Goblin', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO spawns (map, object_id, entity_id) VALUES ('level', 7, 1)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	writer := &sqliteTx{tx: tx}
	if err := writer.SetSpawnComponents(t.Context(), "level", 7, []string{"Sprite", "Position", "Health"}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := db.QueryRow(`SELECT components FROM spawns WHERE map='level' AND object_id=7`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != `["Health","Position","Sprite"]` {
		t.Errorf("authored set = %s, want sorted JSON", got)
	}
}

func TestEnsureInterpreterTables_RepairsOldKeyWithoutLosingComponentSnapshot(t *testing.T) {
	db := openMemoryDB(t)
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL)`,
		`INSERT INTO entities VALUES (1, 'Goblin', 0)`,
		`CREATE TABLE spawns (map TEXT NOT NULL, object_id INTEGER NOT NULL,
			entity_id INTEGER REFERENCES entities(id) ON DELETE SET NULL,
			components TEXT, PRIMARY KEY (map, object_id))`,
		`INSERT INTO spawns VALUES ('level', 7, 1, '["Health","Position"]')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnsureInterpreterTables(db); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := db.QueryRow(`SELECT components FROM spawns WHERE object_id = 7`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != `["Health","Position"]` {
		t.Errorf("the constraint repair lost the authored set: %q", got)
	}
}
