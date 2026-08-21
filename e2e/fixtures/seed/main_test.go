package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Nothing in the browser suite reads the fixture database yet — Forge only
// starts opening it in Story 6 — so a seeder that silently produced an empty
// database would leave every spec green. That is precisely the failure the
// fixture's "distinctive values" are meant to make impossible, so the guard
// lives here until a spec can carry it.
func TestSeed_ProducesTheFixtureTheSuiteExpects(t *testing.T) {
	out := filepath.Join(t.TempDir(), "e2e.db")
	if err := run("../project/schema.json", out); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	db, err := sql.Open("sqlite", "file:"+out+"?mode=ro")
	if err != nil {
		t.Fatalf("opening seeded db: %v", err)
	}
	defer func() { _ = db.Close() }()

	// The version the engine-status readout reports. Deliberately not the
	// repo's own 3: if Forge ever resolved the wrong schema.json, this is what
	// makes it obvious rather than plausible.
	var version int
	if err := db.QueryRow("SELECT value FROM meta WHERE key = 'schema_version'").Scan(&version); err != nil {
		t.Fatalf("reading schema_version: %v", err)
	}
	if version != 7 {
		t.Errorf("schema_version = %d, want 7", version)
	}

	counts := map[string]int{}
	rows, err := db.Query("SELECT entity_type, COUNT(*) FROM entities GROUP BY entity_type")
	if err != nil {
		t.Fatalf("counting entities: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		counts[name] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}

	// Three and one, chosen so they can never be confused with an off-by-one.
	want := map[string]int{"TestDummy": 3, "TestGoblin": 1}
	for typ, n := range want {
		if counts[typ] != n {
			t.Errorf("%s count = %d, want %d", typ, counts[typ], n)
		}
	}
	if len(counts) != len(want) {
		t.Errorf("entity types = %v, want exactly %v", counts, want)
	}

	// The component table whose name exists nowhere in the repo's own schema.
	var probe int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='comp_e2eprobe'",
	).Scan(&probe); err != nil {
		t.Fatalf("looking for comp_e2eprobe: %v", err)
	}
	if probe != 1 {
		t.Error("comp_e2eprobe is missing; the fixture schema was not applied")
	}
}

// Reseeding must rebuild, not migrate. A leftover database from an older
// fixture would otherwise be carried forward into a shape no seed run produces.
func TestSeed_IsIdempotent(t *testing.T) {
	out := filepath.Join(t.TempDir(), "e2e.db")
	for i := range 2 {
		if err := run("../project/schema.json", out); err != nil {
			t.Fatalf("seed run %d: %v", i+1, err)
		}
	}

	db, err := sql.Open("sqlite", "file:"+out+"?mode=ro")
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer func() { _ = db.Close() }()

	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM entities").Scan(&total); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if total != 4 {
		t.Errorf("entities after two seed runs = %d, want 4 — the seed accumulated", total)
	}
}
