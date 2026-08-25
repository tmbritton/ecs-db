package migration

import (
	"database/sql"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
)

// The engine migrates on structure, not only on a version bump.
//
// It used to do the opposite — storage.checkAndMigrate returned the moment the
// stored schema_version equalled the file's — and this panel had to warn that
// the statements it was showing would not be run. Story 11 opened that gate,
// because the generator's output also changes when the engine changes, and no
// schema file moves when it does.
//
// This test exists because the preview makes a claim about another package's
// behaviour, and a claim like that has to be checked against the code rather
// than read off it. If the engine ever goes back to needing a version bump,
// this fails and the panel is wrong again.
func TestEngine_MigratesWhenOnlyTheStructureChanged(t *testing.T) {
	path := bootstrap(t, built())

	edited := built() // same SchemaVersion: 3
	comp := edited.Components["Position"]
	comp.Properties["z"] = property("number")
	comp.PropertyOrder = append(comp.PropertyOrder, "z")
	edited.Components["Position"] = comp

	if p := Check(path, edited, built()); len(p.Statements) == 0 {
		t.Fatal("the preview found no pending change to test against")
	}

	reopen(t, path, edited)

	if !columnExists(t, path, "comp_position", "z") {
		t.Fatal("the engine did not apply an edit saved without a version bump")
	}
}

// A database that already matches is left alone — the other half of the same
// claim, and the one that stops every open from rebuilding every table.
func TestEngine_LeavesAMatchingDatabaseAlone(t *testing.T) {
	path := bootstrap(t, built())

	if p := Check(path, built(), built()); len(p.Statements) != 0 {
		t.Errorf("a database matching its schema has %d pending statement(s): %+v",
			len(p.Statements), p.Statements)
	}
}

// With the version bumped, the same edit is applied. Together these pin the
// difference the panel reports.
func TestEngine_MigratesWhenTheVersionIsBumped(t *testing.T) {
	path := bootstrap(t, built())

	edited := built()
	edited.SchemaVersion = built().SchemaVersion + 1
	comp := edited.Components["Position"]
	comp.Properties["z"] = property("number")
	comp.PropertyOrder = append(comp.PropertyOrder, "z")
	edited.Components["Position"] = comp

	reopen(t, path, edited)

	if !columnExists(t, path, "comp_position", "z") {
		t.Fatal("the engine did not apply the migration after a version bump")
	}
}

// reopen starts the engine's store against an existing database, which is the
// path that decides whether to migrate.
func reopen(t *testing.T, path string, s schema.DatabaseSchema) {
	t.Helper()
	store, err := storage.NewSQLiteStore(path, s, "")
	if err != nil {
		t.Fatalf("reopening the database: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
}

func columnExists(t *testing.T, path, table, column string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		t.Fatalf("reading columns: %v", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating columns: %v", err)
	}
	return false
}
