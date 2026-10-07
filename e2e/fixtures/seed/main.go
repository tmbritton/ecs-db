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
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/tilemap"
	"github.com/tmbritton/ecs-db/internal/world"
)

func main() {
	schemaPath := flag.String("schema", "e2e/fixtures/project/schema.json", "schema to bootstrap from")
	out := flag.String("out", "e2e/fixtures/project/e2e.db", "database to write")
	mapPath := flag.String("map", "", "optional authored TMX to import through the engine spawn loader")
	flag.Parse()

	if err := runWithMap(*schemaPath, *out, *mapPath); err != nil {
		log.Fatalf("seed: %v", err)
	}
	fmt.Printf("seeded %s\n", *out)
}

func run(schemaPath, out string) error {
	return runWithMap(schemaPath, out, "")
}

func runWithMap(schemaPath, out, mapPath string) error {
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

	if err := seedEntities(store, loaded); err != nil {
		return err
	}
	if mapPath == "" {
		return nil
	}
	// The fixture omits Tile as an entity type, so the complete tile importer
	// cannot run against it. This is the actual object importer LoadMap calls,
	// against the saved file and a new engine database rather than a Go value
	// constructed beside the browser test.
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		return err
	}
	raw, err = os.ReadFile(mapPath)
	if err != nil {
		return err
	}
	m, err := tiled.Parse(raw, mapPath)
	if err != nil {
		return err
	}
	svc := world.NewEntityService(store)
	svc.SetSchema(loaded)
	mapPath = filepath.Clean(mapPath)
	result, err := tilemap.SyncSpawns(context.Background(), svc, store.DB(), mapPath, m)
	if err != nil {
		return err
	}
	if len(result.Refused) > 0 {
		return fmt.Errorf("engine refused saved map spawns: %v", result.Refused)
	}
	key := m.Properties.Get(tiled.PropMapID)
	if key == "" {
		key = mapPath
	}
	for _, group := range m.ObjectGroups {
		for _, obj := range group.Objects {
			if obj.Type == "" {
				continue
			}
			var kind string
			var x, y int
			var hp sql.NullInt64
			if err := store.DB().QueryRow(`SELECT e.entity_type, p.x, p.y, h.hp FROM spawns s
				JOIN entities e ON e.id = s.entity_id
				JOIN comp_position p ON p.entity_id = e.id
				LEFT JOIN comp_health h ON h.entity_id = e.id
				WHERE s.map = ? AND s.object_id = ?`, key, obj.ID).Scan(&kind, &x, &y, &hp); err != nil {
				return fmt.Errorf("looking up saved object %d in the engine: %w", obj.ID, err)
			}
			fmt.Printf("imported spawn object %d: %s at %d,%d", obj.ID, kind, x, y)
			if hp.Valid {
				fmt.Printf(" with hp %d", hp.Int64)
			}
			fmt.Println()
		}
	}
	return nil
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
