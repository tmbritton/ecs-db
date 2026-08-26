package tilemap

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
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

// A room with walls round it: gid 2 is wall and gid 1 is floor, so the one open
// cell is the middle. The character format wrote this as ["###","#.#","###"]
// until Story 7 migrated the last map that used it.
func threeByThreeRoom() string {
	return mapOf(3, 3,
		layerOf("ground", 3, 3, "", "2,2,2,\n2,1,2,\n2,2,2"))
}

func TestLoadMap_Idempotent(t *testing.T) {
	svc, db := syncFixture(t)

	path := tiledMap(t, twoTileTSX, threeByThreeRoom(), "level.tmx")
	for range 2 {
		if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
			t.Fatalf("LoadMap: %v", err)
		}
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&count); err != nil {
		t.Fatalf("counting tiles: %v", err)
	}
	if count != 9 {
		t.Errorf("tile count = %d after two calls, want 9", count)
	}
}

func TestLoadMap_ASecondLoadPicksUpAnEditedFile(t *testing.T) {
	svc, db := syncFixture(t)

	path := tiledMap(t, twoTileTSX, threeByThreeRoom(), "level.tmx")
	first, _, err := LoadMap(context.Background(), svc, db, path)
	if err != nil {
		t.Fatalf("first LoadMap: %v", err)
	}
	// Asserted before the edit, or "the middle cell is a wall now" is satisfied
	// by a fixture where it always was.
	if !first.IsPassable(1, 1) {
		t.Fatal("the middle cell is not floor before the edit — the fixture proves nothing")
	}

	// The author walls off the one open cell.
	walled := mapOf(3, 3, layerOf("ground", 3, 3, "", "2,2,2,\n2,2,2,\n2,2,2"))
	if err := os.WriteFile(path, []byte(walled), 0o644); err != nil {
		t.Fatalf("rewriting map: %v", err)
	}
	grid, _, err := LoadMap(context.Background(), svc, db, path)
	if err != nil {
		t.Fatalf("second LoadMap: %v", err)
	}

	if grid.IsPassable(1, 1) {
		t.Error("the edited map still loads as the old one — re-import did nothing")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&count); err != nil {
		t.Fatalf("counting tiles: %v", err)
	}
	if count != 9 {
		t.Errorf("tile count = %d, want the same 9 entities updated in place", count)
	}
}

func TestLoadMap_ASmallerMapDropsTheTilesItNoLongerHas(t *testing.T) {
	svc, db := syncFixture(t)

	path := tiledMap(t, twoTileTSX, threeByThreeRoom(), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatalf("first LoadMap: %v", err)
	}

	smaller := mapOf(2, 2, layerOf("ground", 2, 2, "", "2,2,\n2,1"))
	if err := os.WriteFile(path, []byte(smaller), 0o644); err != nil {
		t.Fatalf("rewriting map: %v", err)
	}
	grid, _, err := LoadMap(context.Background(), svc, db, path)
	if err != nil {
		t.Fatalf("second LoadMap: %v", err)
	}

	if grid.Width != 2 || grid.Height != 2 {
		t.Errorf("grid = %d×%d, want 2×2", grid.Width, grid.Height)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&count); err != nil {
		t.Fatalf("counting tiles: %v", err)
	}
	if count != 4 {
		t.Errorf("tile count = %d, want 4 — the five cells the map lost are gone", count)
	}
	if _, ok := grid.EntityAt(2, 2); ok {
		t.Error("the grid still names an entity outside the map")
	}
}

func TestLoadMap_AnImportTheDatabaseRefusesIsNotLoadedAnyway(t *testing.T) {
	svc, db := syncFixture(t)

	if _, err := db.Exec(`CREATE TRIGGER no_tiles BEFORE INSERT ON comp_tile
		BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("installing refusal: %v", err)
	}

	path := tiledMap(t, twoTileTSX, threeByThreeRoom(), "level.tmx")
	grid, _, err := LoadMap(context.Background(), svc, db, path)
	if err == nil {
		t.Fatal("LoadMap returned a grid for a map the database refused to store")
	}
	if grid != nil {
		t.Error("LoadMap handed back a grid built on an import that failed")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&n); err != nil {
		t.Fatalf("counting tiles: %v", err)
	}
	if n != 0 {
		t.Errorf("%d tiles landed from an import the database refused", n)
	}
}

// The character format is gone, and the file that named it is refused rather
// than read as something else. It is not enough that it fails: a map that has
// been in a project since Epic 5 stops loading the day this lands, and the
// error is the only thing telling anybody why.
func TestLoadMap_RefusesTheFormatItUsedToRead(t *testing.T) {
	svc, db := syncFixture(t)

	// A map already loaded, so a refusal has something to lose.
	good := tiledMap(t, twoTileTSX, threeByThreeRoom(), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, good); err != nil {
		t.Fatalf("first LoadMap: %v", err)
	}
	before := tileRows(t, db)

	old := filepath.Join(t.TempDir(), "level1.toml")
	if err := os.WriteFile(old, []byte("width=3\nheight=3\nrows=[\"###\",\"#.#\",\"###\"]\n"), 0o644); err != nil {
		t.Fatalf("writing the old format: %v", err)
	}
	_, _, err := LoadMap(context.Background(), svc, db, old)
	if err == nil {
		t.Fatal("the character format still loads")
	}
	if !strings.Contains(err.Error(), "level1.toml") {
		t.Errorf("error = %q, want it to name the file that will not load", err)
	}
	if got := tileRows(t, db); !reflect.DeepEqual(got, before) {
		t.Errorf("the refusal still moved the database: %d tiles left of %d", len(got), len(before))
	}
}
