package tilemap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/world"
	_ "modernc.org/sqlite"
)

func tileSchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Tile": {
				Type: "object",
				Properties: map[string]schema.Property{
					"x":         {Type: "integer"},
					"y":         {Type: "integer"},
					"passable":  {Type: "boolean"},
					"tile_type": {Type: "string"},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{
			"Tile": {
				RequiredComponents:   []string{"Tile"},
				AllowExtraComponents: false,
				ValidationLevel:      "strict",
			},
		},
	}
}

func writeTempMap(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "level.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp map: %v", err)
	}
	return p
}

const threeByThree = `width=3
height=3
rows=["###","#.#","###"]
`

func newTestStore(t *testing.T) *storage.SQLiteStore {
	t.Helper()
	ds := tileSchema()
	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.sqlite", ds, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestLoadMap_CreatesEntities(t *testing.T) {
	ds := tileSchema()
	store := newTestStore(t)
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	grid, err := LoadMap(context.Background(), svc, store.DB(), writeTempMap(t, threeByThree))
	if err != nil {
		t.Fatalf("LoadMap: %v", err)
	}
	if grid.Width != 3 || grid.Height != 3 {
		t.Errorf("grid = %d×%d, want 3×3", grid.Width, grid.Height)
	}
	if !grid.IsPassable(1, 1) {
		t.Error("center cell should be passable (floor)")
	}
	if grid.IsPassable(0, 0) {
		t.Error("corner cell should be impassable (wall)")
	}
}

func TestLoadMap_Idempotent(t *testing.T) {
	ds := tileSchema()
	store := newTestStore(t)
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	path := writeTempMap(t, threeByThree)
	for range 2 {
		if _, err := LoadMap(context.Background(), svc, store.DB(), path); err != nil {
			t.Fatalf("LoadMap: %v", err)
		}
	}

	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&count); err != nil {
		t.Fatalf("counting tiles: %v", err)
	}
	if count != 9 {
		t.Errorf("tile count = %d after two calls, want 9", count)
	}
}
