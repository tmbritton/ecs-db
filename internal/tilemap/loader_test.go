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

func TestLoadMap_ASecondLoadPicksUpAnEditedFile(t *testing.T) {
	ds := tileSchema()
	store := newTestStore(t)
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	path := writeTempMap(t, threeByThree)
	if _, err := LoadMap(context.Background(), svc, store.DB(), path); err != nil {
		t.Fatalf("first LoadMap: %v", err)
	}

	// The author walls off the one open cell.
	if err := os.WriteFile(path, []byte("width=3\nheight=3\nrows=[\"###\",\"###\",\"###\"]\n"), 0o644); err != nil {
		t.Fatalf("rewriting map: %v", err)
	}
	grid, err := LoadMap(context.Background(), svc, store.DB(), path)
	if err != nil {
		t.Fatalf("second LoadMap: %v", err)
	}

	if grid.IsPassable(1, 1) {
		t.Error("the edited map still loads as the old one — re-import did nothing")
	}
	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&count); err != nil {
		t.Fatalf("counting tiles: %v", err)
	}
	if count != 9 {
		t.Errorf("tile count = %d, want the same 9 entities updated in place", count)
	}
}

func TestLoadMap_ASmallerMapDropsTheTilesItNoLongerHas(t *testing.T) {
	ds := tileSchema()
	store := newTestStore(t)
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	path := writeTempMap(t, threeByThree)
	if _, err := LoadMap(context.Background(), svc, store.DB(), path); err != nil {
		t.Fatalf("first LoadMap: %v", err)
	}

	if err := os.WriteFile(path, []byte("width=2\nheight=2\nrows=[\"##\",\"#.\"]\n"), 0o644); err != nil {
		t.Fatalf("rewriting map: %v", err)
	}
	grid, err := LoadMap(context.Background(), svc, store.DB(), path)
	if err != nil {
		t.Fatalf("second LoadMap: %v", err)
	}

	if grid.Width != 2 || grid.Height != 2 {
		t.Errorf("grid = %d×%d, want 2×2", grid.Width, grid.Height)
	}
	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&count); err != nil {
		t.Fatalf("counting tiles: %v", err)
	}
	if count != 4 {
		t.Errorf("tile count = %d, want 4 — the five cells the map lost are gone", count)
	}
	if _, ok := grid.EntityAt(2, 2); ok {
		t.Error("the grid still names an entity outside the map")
	}
}

func TestLoadMap_AnUnparseableFileLeavesTheDatabaseAlone(t *testing.T) {
	ds := tileSchema()
	store := newTestStore(t)
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	path := writeTempMap(t, threeByThree)
	if _, err := LoadMap(context.Background(), svc, store.DB(), path); err != nil {
		t.Fatalf("first LoadMap: %v", err)
	}
	before := tileRows(t, store.DB())

	if err := os.WriteFile(path, []byte("width=3\nheight=3\nrows=[\"##\n"), 0o644); err != nil {
		t.Fatalf("rewriting map: %v", err)
	}
	_, err := LoadMap(context.Background(), svc, store.DB(), path)
	if err == nil {
		t.Fatal("LoadMap accepted a file that is not TOML")
	}
	// Named, not merely refused: the size check downstream would also refuse a
	// file that failed to parse, and "no size" is the wrong thing to tell
	// someone whose map file is half-written.
	if !strings.Contains(err.Error(), "parsing") {
		t.Errorf("error = %q, want it to say the file would not parse", err)
	}

	if got := tileRows(t, store.DB()); !reflect.DeepEqual(got, before) {
		t.Error("a map that failed to parse still moved the database")
	}
}

func TestLoadMap_CharactersThatAreNotTilesDescribeNoCell(t *testing.T) {
	ds := tileSchema()
	store := newTestStore(t)
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	path := writeTempMap(t, "width=3\nheight=1\nrows=[\"#?.\"]\n")
	grid, err := LoadMap(context.Background(), svc, store.DB(), path)
	if err != nil {
		t.Fatalf("LoadMap: %v", err)
	}
	if _, ok := grid.EntityAt(1, 0); ok {
		t.Error("'?' produced a tile")
	}
	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&count); err != nil {
		t.Fatalf("counting tiles: %v", err)
	}
	if count != 2 {
		t.Errorf("tile count = %d, want 2", count)
	}
}

func TestLoadMap_RowsAreRowsAndColumnsAreColumns(t *testing.T) {
	ds := tileSchema()
	store := newTestStore(t)
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	// Asymmetric on purpose: a square map of symmetric rows cannot tell a
	// loader that reads (x,y) from one that reads (y,x).
	path := writeTempMap(t, "width=3\nheight=2\nrows=[\"..#\",\"###\"]\n")
	grid, err := LoadMap(context.Background(), svc, store.DB(), path)
	if err != nil {
		t.Fatalf("LoadMap: %v", err)
	}

	if !grid.IsPassable(1, 0) {
		t.Error("(1,0) should be floor — the second character of the first row")
	}
	if grid.IsPassable(0, 1) {
		t.Error("(0,1) should be wall — the first character of the second row")
	}
}

func TestLoadMap_AnImportTheDatabaseRefusesIsNotLoadedAnyway(t *testing.T) {
	ds := tileSchema()
	store := newTestStore(t)
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	if _, err := store.DB().Exec(`CREATE TRIGGER no_tiles BEFORE INSERT ON comp_tile
		BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("installing refusal: %v", err)
	}

	path := writeTempMap(t, threeByThree)
	grid, err := LoadMap(context.Background(), svc, store.DB(), path)
	if err == nil {
		t.Fatal("LoadMap returned a grid for a map the database refused to store")
	}
	if grid != nil {
		t.Error("LoadMap handed back a grid built on an import that failed")
	}
	var n int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&n); err != nil {
		t.Fatalf("counting tiles: %v", err)
	}
	if n != 0 {
		t.Errorf("%d tiles landed from an import the database refused", n)
	}
}

func TestLoadMap_RefusesAFileThatParsedButDescribesNoMap(t *testing.T) {
	cases := []struct {
		name, content, says string
	}{
		{
			// The one that matters: TOML ignores a key it was not asked for, so a
			// typo produces a map of zeroes, and under a diff a map of zeroes
			// deletes every tile there is.
			"the rows are under a key nobody reads",
			"width=3\nheight=3\nrowz=[\"###\",\"#.#\",\"###\"]\n", "holds 0 rows",
		},
		{"no size at all", "rows=[\"###\"]\n", "no size"},
		{"a negative width", "width=-3\nheight=1\nrows=[\"###\"]\n", "no size"},
		{"fewer rows than it claims", "width=3\nheight=3\nrows=[\"###\"]\n", "holds 1 rows"},
		{"more rows than it claims", "width=3\nheight=1\nrows=[\"###\",\"###\"]\n", "holds 2 rows"},
		{"a row narrower than the map", "width=3\nheight=2\nrows=[\"###\",\"##\"]\n", "row 1 holds 2 cells"},
		{"a row wider than the map", "width=3\nheight=1\nrows=[\"####\"]\n", "row 0 holds 4 cells"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ds := tileSchema()
			store := newTestStore(t)
			svc := world.NewEntityService(store)
			svc.SetSchema(ds)

			// A map already loaded, so a refusal has something to lose.
			if _, err := LoadMap(context.Background(), svc, store.DB(), writeTempMap(t, threeByThree)); err != nil {
				t.Fatalf("first LoadMap: %v", err)
			}
			before := tileRows(t, store.DB())

			_, err := LoadMap(context.Background(), svc, store.DB(), writeTempMap(t, c.content))
			if err == nil {
				t.Fatal("accepted a file that does not describe a map")
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Errorf("error = %q, want it to say %q", err, c.says)
			}
			if got := tileRows(t, store.DB()); !reflect.DeepEqual(got, before) {
				t.Errorf("the refusal still moved the database: %d tiles left of %d",
					len(got), len(before))
			}
		})
	}
}

func TestLoadMap_ACharacterWiderThanAByteDoesNotShiftTheRow(t *testing.T) {
	ds := tileSchema()
	store := newTestStore(t)
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	// '…' is three bytes and one cell. Ranging over the string would put the
	// wall after it at x=4 in a three-wide map.
	path := writeTempMap(t, "width=3\nheight=1\nrows=[\"#…#\"]\n")
	grid, err := LoadMap(context.Background(), svc, store.DB(), path)
	if err != nil {
		t.Fatalf("LoadMap: %v", err)
	}

	if _, ok := grid.EntityAt(2, 0); !ok {
		t.Error("the second wall is not at x=2")
	}
	for _, x := range []int{3, 4} {
		if _, ok := grid.EntityAt(x, 0); ok {
			t.Errorf("a tile landed at x=%d, outside a map three cells wide", x)
		}
	}
	var n int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&n); err != nil {
		t.Fatalf("counting tiles: %v", err)
	}
	if n != 2 {
		t.Errorf("tile count = %d, want the two walls", n)
	}
}
