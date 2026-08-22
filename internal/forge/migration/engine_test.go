package migration

import (
	"database/sql"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
)

// The engine only migrates when the database's recorded schema_version differs
// from schema.json's. storage.checkAndMigrate returns early when they match,
// so an edit saved without a version bump is never applied — the statements
// this package generates are real, and the engine will not run any of them.
//
// This test exists because the preview makes a claim about another package's
// behaviour, and a claim like that has to be checked against the code rather
// than read off it. If the engine ever migrates on structure alone, this fails
// and the panel's wording is wrong.
func TestEngine_SkipsMigrationWhenTheVersionIsUnchanged(t *testing.T) {
	path := bootstrap(t, built())

	edited := built() // same SchemaVersion: 3
	comp := edited.Components["Position"]
	comp.Properties["z"] = property("number")
	comp.PropertyOrder = append(comp.PropertyOrder, "z")
	edited.Components["Position"] = comp

	// The preview says there is work to do.
	if p := Check(path, edited, built()); len(p.Statements) == 0 {
		t.Fatal("the preview found no pending change to test against")
	}

	reopen(t, path, edited)

	if columnExists(t, path, "comp_position", "z") {
		t.Fatal("the engine migrated without a version bump — the panel's warning is now wrong")
	}
}

// With the version bumped, the same edit is applied. Together these two pin
// the difference the panel reports.
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
