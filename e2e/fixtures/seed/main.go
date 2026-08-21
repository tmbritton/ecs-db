// Command seed builds the SQLite database the e2e suite runs Forge against.
//
// Forge reads real engine files, so the suite gives it a real project rather
// than mocking the transport — mocking would stand in for exactly the thing
// under test. The database is generated rather than committed: it is a binary
// artefact, `*.db` is gitignored, and regenerating it is how the fixture stays
// in step with the engine's own bootstrap code.
//
// It is deliberately distinctive: every value differs from the repo's own
// project (schemaVersion 7, not 3; entity types named TestDummy and TestGoblin),
// so a test asserting on them is also asserting that Forge read the fixture and
// not the developer's working copy.
//
// The entity population is deterministic — same rows, same ids, same values on
// every run. The database *structure* is not: storage bootstraps component
// tables by ranging over the schema's map, and Go randomises map iteration, so
// comp_* tables are created in a different order each time. meta.build_time is
// a wall-clock stamp for the same reason. Anything reading these back must
// ORDER BY rather than trusting insertion order; with retries disabled, a spec
// that depends on it becomes an unreproducible build failure.
//
//	go run ./e2e/fixtures/seed          # rebuild at the default path
//	go run ./e2e/fixtures/seed -out X   # elsewhere
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/world"
)

func main() {
	schemaPath := flag.String("schema", "e2e/fixtures/project/schema.json", "schema to bootstrap from")
	out := flag.String("out", "e2e/fixtures/project/e2e.db", "database to write")
	flag.Parse()

	if err := run(*schemaPath, *out); err != nil {
		log.Fatalf("seed: %v", err)
	}
	fmt.Printf("seeded %s\n", *out)
}

func run(schemaPath, out string) error {
	// Always start from nothing. A leftover database from an older fixture
	// would be *migrated* rather than rebuilt, so the suite would run against
	// a shape no seed run ever produced.
	for _, p := range []string{out, out + "-wal", out + "-shm"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clearing %s: %w", p, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(out), err)
	}

	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", schemaPath, err)
	}
	loaded, err := schema.LoadSchema(raw)
	if err != nil {
		return fmt.Errorf("loading %s: %w", schemaPath, err)
	}
	if err := schema.ValidateSchema(loaded); err != nil {
		return fmt.Errorf("validating %s: %w", schemaPath, err)
	}

	store, err := storage.NewSQLiteStore(out, loaded, "")
	if err != nil {
		return fmt.Errorf("bootstrapping %s: %w", out, err)
	}
	defer func() { _ = store.Close() }()

	return seedEntities(store, loaded)
}

// seedEntities writes a small, fixed population. Counts are what the ENTS and
// SCHEMA modes will assert on, so they are chosen to be unambiguous: three
// dummies and one goblin can never be confused with an off-by-one.
func seedEntities(store *storage.SQLiteStore, s schema.DatabaseSchema) error {
	svc := world.NewEntityService(store)
	svc.SetSchema(s)
	ctx := context.Background()

	for i := range 3 {
		if _, err := svc.CreateEntity(ctx, "TestDummy", []world.EntityComponent{
			{Name: "Position", Values: world.ComponentValues{"x": float64(i), "y": float64(i)}},
			{Name: "E2EProbe", Values: world.ComponentValues{"marker": fmt.Sprintf("dummy-%d", i)}},
		}); err != nil {
			return fmt.Errorf("creating TestDummy %d: %w", i, err)
		}
	}

	if _, err := svc.CreateEntity(ctx, "TestGoblin", []world.EntityComponent{
		{Name: "Position", Values: world.ComponentValues{"x": 9.0, "y": 9.0}},
		{Name: "Health", Values: world.ComponentValues{"hp": 12, "maxHp": 20}},
	}); err != nil {
		return fmt.Errorf("creating TestGoblin: %w", err)
	}
	return nil
}
