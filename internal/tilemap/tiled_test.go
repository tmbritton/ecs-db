package tilemap

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

// A tileset of two tiles: 0 is floor and says it can be walked through, 1 is
// wall and says it cannot. The properties are the point of the epic — the ASCII
// format could only ever say "wall" or "not wall", and this is where
// passability stops being a character.
const twoTileTSX = `<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" name="dungeon" tilewidth="8" tileheight="8" tilecount="2" columns="2">
 <image source="dungeon.png" width="16" height="8"/>
 <tile id="0" type="floor">
  <properties><property name="passable" type="bool" value="true"/></properties>
 </tile>
 <tile id="1" type="wall">
  <properties><property name="passable" type="bool" value="false"/></properties>
 </tile>
</tileset>`

// tiledMap writes a map and the tileset it names into a temp directory and
// returns the path to the map. The tileset is a real file opened through the
// real Opener, so these tests exercise the resolution Story 2 built rather than
// a stand-in for it.
func tiledMap(t *testing.T, tileset, file, name string) string {
	t.Helper()
	dir := t.TempDir()
	if tileset != "" {
		if err := os.WriteFile(filepath.Join(dir, "dungeon.tsx"), []byte(tileset), 0o644); err != nil {
			t.Fatalf("writing tileset: %v", err)
		}
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(file), 0o644); err != nil {
		t.Fatalf("writing map: %v", err)
	}
	return p
}

// mapOf wraps layers in a map element of the given size with the two-tile
// tileset attached at first gid 1 — so gid 1 is floor and gid 2 is wall.
func mapOf(w, h int, layers ...string) string {
	index := 0
	layout := regexp.MustCompile(`<layer\b[^>]*>`).ReplaceAllStringFunc(strings.Join(layers, "\n"), func(tag string) string {
		index++
		if strings.Contains(tag, ` id="`) {
			return tag
		}
		return strings.Replace(tag, "<layer ", fmt.Sprintf(`<layer id="%d" `, index), 1)
	})
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<map version="1.10" orientation="orthogonal" renderorder="right-down" width="%d" height="%d" tilewidth="8" tileheight="8">
 <tileset firstgid="1" source="dungeon.tsx"/>
%s
</map>`, w, h, layout)
}

// layerOf is one CSV tile layer of the given size.
func layerOf(name string, w, h int, attrs, csv string) string {
	return fmt.Sprintf(` <layer name=%q width="%d" height="%d" %s><data encoding="csv">%s</data></layer>`,
		name, w, h, attrs, csv)
}

// loadTiled loads a map through the real LoadMap and hands back the grid and
// the database, so a test can ask both what the engine sees and what was
// stored.
func loadTiledMap(t *testing.T, path string) (*TileGrid, *sql.DB) {
	t.Helper()
	svc, db := syncFixture(t)
	grid, _, err := LoadMap(context.Background(), svc, db, path)
	if err != nil {
		t.Fatalf("LoadMap: %v", err)
	}
	return grid, db
}

func tileTypeAt(t *testing.T, db *sql.DB, x, y int) string {
	t.Helper()
	var got string
	err := db.QueryRow(`SELECT t.tile_type FROM comp_tile t
		JOIN comp_tilelayer l ON l.entity_id = t.entity_id WHERE t.x = ? AND t.y = ?
		ORDER BY l.layer_order DESC, l.draw_order DESC LIMIT 1`, x, y).Scan(&got)
	if err != nil {
		t.Fatalf("reading tile_type at (%d,%d): %v", x, y, err)
	}
	return got
}

func tileCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&n); err != nil {
		t.Fatalf("counting tiles: %v", err)
	}
	return n
}

func TestLoadMap_ReadsATiledMap(t *testing.T) {
	path := tiledMap(t, twoTileTSX, mapOf(3, 2,
		layerOf("ground", 3, 2, "", "2,1,2,\n2,2,2")), "level.tmx")

	grid, db := loadTiledMap(t, path)

	if grid.Width != 3 || grid.Height != 2 {
		t.Fatalf("grid = %d×%d, want 3×2", grid.Width, grid.Height)
	}
	if !grid.IsPassable(1, 0) {
		t.Error("(1,0) holds the floor tile and should be passable")
	}
	if grid.IsPassable(0, 0) {
		t.Error("(0,0) holds the wall tile and should not be passable")
	}
	if got := tileTypeAt(t, db, 0, 0); got != "wall" {
		t.Errorf("tile_type at (0,0) = %q, want %q", got, "wall")
	}
	if got := tileTypeAt(t, db, 1, 0); got != "floor" {
		t.Errorf("tile_type at (1,0) = %q, want %q", got, "floor")
	}
	if n := tileCount(t, db); n != 6 {
		t.Errorf("tile count = %d, want 6", n)
	}
}

// A square map of symmetric rows cannot tell a loader that reads (x,y) from one
// that reads (y,x).
func TestLoadMap_TiledRowsAreRowsAndColumnsAreColumns(t *testing.T) {
	path := tiledMap(t, twoTileTSX, mapOf(3, 2,
		layerOf("ground", 3, 2, "", "1,1,2,\n2,2,2")), "level.tmx")

	grid, _ := loadTiledMap(t, path)

	if !grid.IsPassable(1, 0) {
		t.Error("(1,0) is the second cell of the first row and is floor")
	}
	if grid.IsPassable(0, 1) {
		t.Error("(0,1) is the first cell of the second row and is wall")
	}
}

// The two serialisations are the same map, so they are the same tiles.
func TestLoadMap_ReadsTMJAsWellAsTMX(t *testing.T) {
	const tmj = `{"width":3,"height":1,"tilewidth":8,"tileheight":8,
 "orientation":"orthogonal",
 "tilesets":[{"firstgid":1,"source":"dungeon.tsx"}],
 "layers":[{"id":1,"type":"tilelayer","name":"ground","width":3,"height":1,"data":[2,1,2]}]}`

	grid, db := loadTiledMap(t, tiledMap(t, twoTileTSX, tmj, "level.tmj"))

	if grid.IsPassable(0, 0) || !grid.IsPassable(1, 0) || grid.IsPassable(2, 0) {
		t.Error("the JSON map did not load as wall, floor, wall")
	}
	if n := tileCount(t, db); n != 3 {
		t.Errorf("tile count = %d, want 3", n)
	}
}

// Until Story 3, the topmost tile decides legacy movement; every authored tile
// still has its own entity, including the floor below this wall.
func TestLoadMap_TheTopmostNonEmptyTileDecidesTheCell(t *testing.T) {
	// Floor everywhere; a wall drawn over the middle cell only.
	path := tiledMap(t, twoTileTSX, mapOf(3, 1,
		layerOf("floor", 3, 1, "", "1,1,1"),
		layerOf("walls", 3, 1, "", "0,2,0")), "level.tmx")

	grid, db := loadTiledMap(t, path)

	if grid.IsPassable(1, 0) {
		t.Error("the wall drawn on the upper layer did not win its cell")
	}
	if !grid.IsPassable(0, 0) || !grid.IsPassable(2, 0) {
		t.Error("a cell the upper layer leaves empty should keep the floor beneath it")
	}
	if got := tileTypeAt(t, db, 1, 0); got != "wall" {
		t.Errorf("tile_type at (1,0) = %q — the type comes from the same tile the passability does", got)
	}
	if n := tileCount(t, db); n != 4 {
		t.Errorf("tile count = %d, want 4 — every nonempty tile is an entity", n)
	}
}

// Layer order is the file's order, so a wall under a floor is a floor. The
// mirror of the test above, and the one that fails if the layers are walked the
// wrong way round.
func TestLoadMap_TheLowerLayerLosesTheCell(t *testing.T) {
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("walls", 1, 1, "", "2"),
		layerOf("floor", 1, 1, "", "1")), "level.tmx")

	grid, _ := loadTiledMap(t, path)

	if !grid.IsPassable(0, 0) {
		t.Error("the floor is the upper layer here and should have won")
	}
}

// A layer inside a folder is a layer, and where the folder sits is where its
// layers sit. This is the case that told the two serialisations apart.
func TestLoadMap_ALayerInsideAFolderKeepsItsPlaceInTheStack(t *testing.T) {
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		` <group name="scenery">`+
			layerOf("walls", 1, 1, "", "2")+
			` </group>`,
		layerOf("floor", 1, 1, "", "1")), "level.tmx")

	grid, _ := loadTiledMap(t, path)

	if !grid.IsPassable(0, 0) {
		t.Error("the floor is written below the folder and should be on top of it")
	}
}

// Hiding a layer is a view setting. An author who hides the wall layer to look
// at the floor underneath it, saves, and finds the walls gone has been robbed
// by a checkbox.
func TestLoadMap_AHiddenLayerStillCounts(t *testing.T) {
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("floor", 1, 1, "", "1"),
		layerOf("walls", 1, 1, `visible="0"`, "2")), "level.tmx")

	grid, _ := loadTiledMap(t, path)

	if grid.IsPassable(0, 0) {
		t.Error("a hidden wall layer stopped being a wall")
	}
}

// An empty cell is not a tile: no entity, and no row for the grid to index. It
// is impassable all the same, because TileGrid has nothing there — which is a
// different statement from a tile that says nothing.
func TestLoadMap_AnEmptyCellIsNotATile(t *testing.T) {
	path := tiledMap(t, twoTileTSX, mapOf(3, 1,
		layerOf("ground", 3, 1, "", "1,0,1")), "level.tmx")

	grid, db := loadTiledMap(t, path)

	if _, ok := grid.EntityAt(1, 0); ok {
		t.Error("gid 0 produced a tile entity")
	}
	if grid.IsPassable(1, 0) {
		t.Error("a cell with nothing in it is not somewhere to stand")
	}
	if n := tileCount(t, db); n != 2 {
		t.Errorf("tile count = %d, want 2", n)
	}
}

const undeclaredTSX = `<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" name="plain" tilewidth="8" tileheight="8" tilecount="2" columns="2">
 <image source="dungeon.png" width="16" height="8"/>
 <tile id="0" type="rug"/>
</tileset>`

// A map is a floor with obstacles on it, so a tile that says nothing is floor.
// The default a 200-tile decoration set can live with is the one that will not
// be worked around with a script.
func TestLoadMap_ATileWhoseTilesetSaysNothingIsPassable(t *testing.T) {
	grid, db := loadTiledMap(t, tiledMap(t, undeclaredTSX, mapOf(2, 1,
		layerOf("ground", 2, 1, "", "1,2")), "level.tmx"))

	if !grid.IsPassable(0, 0) || !grid.IsPassable(1, 0) {
		t.Error("a tileset that declares no passability made an impassable map")
	}
	if got := tileTypeAt(t, db, 0, 0); got != "rug" {
		t.Errorf("tile_type = %q, want the tile's own class", got)
	}
	// Tile 1 declares nothing at all, not even a class.
	if got := tileTypeAt(t, db, 1, 0); got != "" {
		t.Errorf("tile_type = %q, want empty — the renderer draws anything that is not a wall", got)
	}
}

const tilesetDefaultTSX = `<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" name="solid" class="rock" tilewidth="8" tileheight="8" tilecount="2" columns="2">
 <properties><property name="passable" type="bool" value="false"/></properties>
 <image source="dungeon.png" width="16" height="8"/>
 <tile id="1">
  <properties><property name="passable" type="bool" value="true"/></properties>
 </tile>
</tileset>`

// One line at the top of a tileset instead of a declaration on every tile in
// it, and a tile that disagrees still wins.
func TestLoadMap_ATilesetCanDeclareTheDefaultForItsTiles(t *testing.T) {
	grid, db := loadTiledMap(t, tiledMap(t, tilesetDefaultTSX, mapOf(2, 1,
		layerOf("ground", 2, 1, "", "1,2")), "level.tmx"))

	if grid.IsPassable(0, 0) {
		t.Error("the tileset said its tiles are solid and tile 0 did not disagree")
	}
	if !grid.IsPassable(1, 0) {
		t.Error("tile 1 declares itself passable and outranks its tileset")
	}
	if got := tileTypeAt(t, db, 0, 0); got != "rock" {
		t.Errorf("tile_type = %q, want the tileset's class where the tile has none", got)
	}
}

// Refused by position, not skipped: a map referring to art nobody shipped is a
// map with a hole in it, and loading the rest would leave nothing said about it.
func TestLoadMap_RefusesAGidPastTheEndOfItsTileset(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(2, 2,
		layerOf("ground", 2, 2, "", "1,1,\n1,99")), "level.tmx")

	_, _, err := LoadMap(context.Background(), svc, db, path)
	if err == nil {
		t.Fatal("a tile past the end of its tileset was accepted")
	}
	for _, want := range []string{"ground", "(1,1)", "99", "dungeon", "does not hold it"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not say %q", err, want)
		}
	}
	if n := tileCount(t, db); n != 0 {
		t.Errorf("%d tiles landed from a map that was refused", n)
	}

	// The boundary, which is the one an author actually hits: a tileset that
	// has since lost its last tile leaves gids one past its end, and the last
	// tileset in a map has no upper bound of its own to catch them.
	onePast := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("ground", 1, 1, "", "3")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, onePast); err == nil {
		t.Error("global id 3 is tile 2 of a tileset holding 2, and it was accepted")
	}
}

// The other way to be unresolvable, and a different mistake: a tileset removed
// or renumbered leaves gids below every first gid the map still declares.
func TestLoadMap_RefusesAGidBelowEveryTileset(t *testing.T) {
	svc, db := syncFixture(t)
	file := strings.Replace(mapOf(1, 1, layerOf("ground", 1, 1, "", "3")),
		`firstgid="1"`, `firstgid="10"`, 1)

	_, _, err := LoadMap(context.Background(), svc, db, tiledMap(t, twoTileTSX, file, "level.tmx"))
	if err == nil {
		t.Fatal("a tile below every tileset was accepted")
	}
	if !strings.Contains(err.Error(), "below the first tile") {
		t.Errorf("refusal %q does not say the id is below every tileset", err)
	}
}

// Point is a square cell. An isometric or hexagonal map's coordinates are not
// this grid's, and loading one would put every tile somewhere plausible and
// wrong.
func TestLoadMap_RefusesAMapThatIsNotOrthogonal(t *testing.T) {
	svc, db := syncFixture(t)
	file := strings.Replace(mapOf(1, 1, layerOf("ground", 1, 1, "", "1")),
		`orientation="orthogonal"`, `orientation="isometric"`, 1)

	_, _, err := LoadMap(context.Background(), svc, db, tiledMap(t, twoTileTSX, file, "level.tmx"))
	if err == nil {
		t.Fatal("an isometric map was accepted")
	}
	if !strings.Contains(err.Error(), "isometric") {
		t.Errorf("refusal %q does not say which orientation it refused", err)
	}
}

// A tileset the map names and the directory does not hold: refused, with the
// map that pulled it in and the path that was tried.
func TestLoadMap_RefusesAMapWhoseTilesetWillNotOpen(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, "", mapOf(1, 1, layerOf("ground", 1, 1, "", "1")), "level.tmx")

	_, _, err := LoadMap(context.Background(), svc, db, path)
	if err == nil {
		t.Fatal("a map whose tileset is missing was accepted")
	}
	if !strings.Contains(err.Error(), "dungeon.tsx") {
		t.Errorf("refusal %q does not name the tileset", err)
	}
	if n := tileCount(t, db); n != 0 {
		t.Errorf("%d tiles landed from a map that was refused", n)
	}
}

// A map that will not parse leaves the database as it was — the same promise
// Story 3 makes about the TOML format, made about this one.
func TestLoadMap_AnUnparseableTiledFileLeavesTheDatabaseAlone(t *testing.T) {
	svc, db := syncFixture(t)
	good := tiledMap(t, twoTileTSX, mapOf(2, 1, layerOf("ground", 2, 1, "", "1,2")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, good); err != nil {
		t.Fatalf("first LoadMap: %v", err)
	}
	before := tileRows(t, db)

	if err := os.WriteFile(good, []byte(`<map width="2" height="1"><layer`), 0o644); err != nil {
		t.Fatalf("truncating map: %v", err)
	}
	_, _, err := LoadMap(context.Background(), svc, db, good)
	if err == nil {
		t.Fatal("a half-written map was accepted")
	}
	// From the parser, not merely refused. The shape check downstream refuses a
	// map of no size too, and telling someone whose file is half-written that
	// their map is 0×0 sends them to the wrong place. The two prefixes are how
	// they are told apart: "tiled:" is the reader, "tilemap:" is this package.
	if !strings.HasPrefix(err.Error(), "tiled:") {
		t.Errorf("error = %q, want it to come from the parser", err)
	}
	if got := tileRows(t, db); !reflect.DeepEqual(got, before) {
		t.Error("a map that failed to parse still moved the database")
	}
}

// Re-import is Story 3's, not a second copy of it: a second load of an
// unchanged map writes nothing, and an edited one updates in place.
func TestLoadMap_ATiledMapReImportsThroughSyncTiles(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(2, 1, layerOf("ground", 2, 1, "", "1,2")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatalf("first LoadMap: %v", err)
	}
	before := tileRows(t, db)

	writes := watchTiles(t, db)
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatalf("second LoadMap: %v", err)
	}
	if got := writes(); len(got) != 0 {
		t.Errorf("an unchanged map wrote %v", got)
	}

	// The wall becomes floor. The cell keeps its entity, which is what makes
	// the diff worth having over a reload.
	edited := mapOf(2, 1, layerOf("ground", 2, 1, "", "1,1"))
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatalf("rewriting map: %v", err)
	}
	grid, _, err := LoadMap(context.Background(), svc, db, path)
	if err != nil {
		t.Fatalf("third LoadMap: %v", err)
	}
	if !grid.IsPassable(1, 0) {
		t.Error("the edited map still loads as the old one")
	}
	after := tileRows(t, db)
	if after[Point{X: 1}].ID != before[Point{X: 1}].ID {
		t.Errorf("the changed cell was recreated: id %d became %d",
			before[Point{X: 1}].ID, after[Point{X: 1}].ID)
	}
}

// Every file the loader is handed goes to the Tiled reader — there is no second
// format to dispatch to since Story 7 — so a file that is not one is refused by
// the parser rather than tried as something else.
func TestLoadMap_RefusesAFileThatIsNeitherFormat(t *testing.T) {
	cases := []struct{ name, content string }{
		{"prose", "this is not a map\n"},
		{"the character format", "width=3\nheight=3\nrows=[\"###\",\"#.#\",\"###\"]\n"},
		{"markup that is not a map", `<notamap version="1"/>`},
		{"JSON that is not a map", `{"hello":"world"}`},
		{"a JSON map of no size", `{"type":"map","width":0,"height":0}`},
		{"a map element with no size", `<?xml version="1.0"?><map version="1.10" tilewidth="8" tileheight="8"/>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, db := syncFixture(t)
			// A map already loaded, so a refusal has something to lose.
			good := tiledMap(t, twoTileTSX, mapOf(2, 1, layerOf("g", 2, 1, "", "1,2")), "level.tmx")
			if _, _, err := LoadMap(context.Background(), svc, db, good); err != nil {
				t.Fatalf("first LoadMap: %v", err)
			}
			before := tileRows(t, db)

			path := filepath.Join(t.TempDir(), "level.tmx")
			if err := os.WriteFile(path, []byte(c.content), 0o644); err != nil {
				t.Fatalf("writing: %v", err)
			}
			if _, _, err := LoadMap(context.Background(), svc, db, path); err == nil {
				t.Fatal("a file that describes no map was accepted")
			}
			if got := tileRows(t, db); !reflect.DeepEqual(got, before) {
				t.Errorf("the refusal still moved the database: %d tiles left of %d",
					len(got), len(before))
			}
		})
	}
}

// A layer inside a hidden folder is hidden, and it counts for the same reason a
// hidden layer does.
func TestLoadMap_ALayerInsideAHiddenFolderStillCounts(t *testing.T) {
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("floor", 1, 1, "", "1"),
		` <group name="scenery" visible="0">`+
			layerOf("walls", 1, 1, "", "2")+
			` </group>`), "level.tmx")

	grid, _ := loadTiledMap(t, path)

	if grid.IsPassable(0, 0) {
		t.Error("a wall inside a hidden folder stopped being a wall")
	}
}

// Tiled always writes the orientation. A file that does not is not thereby
// claiming to be a hex map, and refusing it would refuse every map written by
// hand or by a tool that leaves the default implicit.
func TestLoadMap_AMapThatDoesNotSayItsOrientationIsOrthogonal(t *testing.T) {
	file := strings.Replace(mapOf(1, 1, layerOf("ground", 1, 1, "", "2")),
		` orientation="orthogonal"`, "", 1)

	grid, db := loadTiledMap(t, tiledMap(t, twoTileTSX, file, "level.tmx"))

	if _, ok := grid.EntityAt(0, 0); !ok {
		t.Fatal("the map loaded with no tile in it")
	}
	if grid.IsPassable(0, 0) {
		t.Error("the wall did not load as a wall")
	}
	if n := tileCount(t, db); n != 1 {
		t.Errorf("tile count = %d, want 1", n)
	}
}

// Import may also be asked to project a map built in memory, without a
// resolved tileset. That mistake is named rather than dereferenced.
func TestLayerTilesOfTiled_RefusesATilesetThatWasNeverRead(t *testing.T) {
	m := &tiled.Map{
		Name: "in-memory", Width: 1, Height: 1,
		Tilesets: []tiled.TilesetRef{{FirstGID: 1}},
		Layers: []tiled.Layer{{
			ID: 1, Name: "ground", Width: 1, Height: 1, Visible: true, Opacity: 1, Data: []uint32{1},
		}},
	}

	_, err := LayerTilesOfTiled(m, "memory")
	if err == nil {
		t.Fatal("a tile from an unresolved tileset was accepted")
	}
	if !strings.Contains(err.Error(), "never read") {
		t.Errorf("refusal %q does not say the tileset was never read", err)
	}
}

// A flipped tile is the same tile. The flags live in the top bits of the id, so
// looking the tileset up by the raw value asks for tile 2,147,483,650 — which
// belongs to no tileset anyone has ever drawn.
func TestLoadMap_AFlippedTileIsStillItsTile(t *testing.T) {
	const flippedWall = 2 | 0x80000000
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("ground", 1, 1, "", fmt.Sprint(uint32(flippedWall)))), "level.tmx")

	grid, db := loadTiledMap(t, path)

	if grid.IsPassable(0, 0) {
		t.Error("a flipped wall stopped being a wall")
	}
	if got := tileTypeAt(t, db, 0, 0); got != "wall" {
		t.Errorf("tile_type = %q, want %q", got, "wall")
	}
}

func TestLoadMap_RefusesAMapFileThatIsNotThere(t *testing.T) {
	svc, db := syncFixture(t)

	_, _, err := LoadMap(context.Background(), svc, db, filepath.Join(t.TempDir(), "absent.tmx"))
	if err == nil {
		t.Fatal("a map file that does not exist was loaded")
	}
	if !strings.Contains(err.Error(), "reading") {
		t.Errorf("refusal %q does not say the file would not open", err)
	}
}

// A collection of images is one file per tile and no sheet, which is how prop
// and creature art is authored — and deleting a tile from one leaves a gap, so
// its ids are not consecutive and counting them answers nothing. The tileset
// parser already refuses to judge a collection by its count; the loader has to
// make the same distinction or it refuses maps Tiled writes routinely.
// A map that draws tiles and names no tileset to draw them from is a different
// mistake from a map whose tilesets were renumbered, and saying "below the
// first tile of every tileset" sends someone looking at first gids that are not
// there.
func TestLoadMap_RefusesAMapThatNamesNoTilesetAtAll(t *testing.T) {
	svc, db := syncFixture(t)
	file := `<?xml version="1.0"?>
<map version="1.10" orientation="orthogonal" width="1" height="1" tilewidth="8" tileheight="8">
 <layer id="1" name="ground" width="1" height="1"><data encoding="csv">5</data></layer>
</map>`

	_, _, err := LoadMap(context.Background(), svc, db, tiledMap(t, "", file, "level.tmx"))
	if err == nil {
		t.Fatal("a map that declares no tilesets was accepted")
	}
	if !strings.Contains(err.Error(), "no tilesets at all") {
		t.Errorf("refusal %q does not say the map declares no tilesets", err)
	}
}

const collectionTSX = `<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" name="props" tilewidth="8" tileheight="8" tilecount="3" columns="0">
 <tile id="0"><image source="barrel.png" width="8" height="8"/></tile>
 <tile id="2" type="crate">
  <image source="crate.png" width="8" height="8"/>
  <properties><property name="passable" type="bool" value="false"/></properties>
 </tile>
 <tile id="3"><image source="rug.png" width="8" height="8"/></tile>
</tileset>`

func TestLoadMap_ACollectionsLastTileIsNotPastItsEnd(t *testing.T) {
	// Local id 3 of a collection that holds three tiles: ordinary, and refused
	// by a bound taken from the count.
	grid, db := loadTiledMap(t, tiledMap(t, collectionTSX, mapOf(2, 1,
		layerOf("ground", 2, 1, "", "3,4")), "level.tmx"))

	if _, ok := grid.EntityAt(1, 0); !ok {
		t.Fatal("the last tile of a collection was refused")
	}
	if grid.IsPassable(0, 0) {
		t.Error("the crate declares itself solid")
	}
	if got := tileTypeAt(t, db, 0, 0); got != "crate" {
		t.Errorf("tile_type = %q, want %q", got, "crate")
	}
}

func TestLoadMap_RefusesAGidInACollectionsGap(t *testing.T) {
	svc, db := syncFixture(t)
	// Local id 1: inside the count, and a tile the collection does not have.
	path := tiledMap(t, collectionTSX, mapOf(1, 1,
		layerOf("ground", 1, 1, "", "2")), "level.tmx")

	_, _, err := LoadMap(context.Background(), svc, db, path)
	if err == nil {
		t.Fatal("a tile the collection does not hold was accepted as a passable, typeless floor")
	}
	if !strings.Contains(err.Error(), "does not hold it") {
		t.Errorf("refusal %q does not say the tileset has no such tile", err)
	}
}

const malformedTSX = `<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" name="typo" tilewidth="8" tileheight="8" tilecount="2" columns="2">
 <image source="dungeon.png" width="16" height="8"/>
 <tile id="0" type="wall">
  <properties><property name="passable" type="int" value="0"/></properties>
 </tile>
</tileset>`

// Tiled makes you pick a type when you add a custom property, and picking int
// and typing 0 is what somebody does the first time. Defaulting there draws a
// wall the player walks straight through — invisible until someone tests the
// geometry, because the tile keeps its "wall" class and the renderer draws it.
func TestLoadMap_RefusesAPassableThatIsDeclaredAndUnreadable(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, malformedTSX, mapOf(1, 1,
		layerOf("ground", 1, 1, "", "1")), "level.tmx")

	_, _, err := LoadMap(context.Background(), svc, db, path)
	if err == nil {
		t.Fatal("passable=0 loaded as a floor")
	}
	for _, want := range []string{"(0,0)", "passable", `"0"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not say %q", err, want)
		}
	}
}

const malformedTilesetTSX = `<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" name="typo" tilewidth="8" tileheight="8" tilecount="2" columns="2">
 <properties><property name="passable" type="string" value="no"/></properties>
 <image source="dungeon.png" width="16" height="8"/>
</tileset>`

func TestLoadMap_RefusesATilesetsPassableThatIsDeclaredAndUnreadable(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, malformedTilesetTSX, mapOf(1, 1,
		layerOf("ground", 1, 1, "", "1")), "level.tmx")

	_, _, err := LoadMap(context.Background(), svc, db, path)
	if err == nil {
		t.Fatal(`a tileset saying passable="no" loaded as a floor`)
	}
	if !strings.Contains(err.Error(), "typo") || !strings.Contains(err.Error(), `"no"`) {
		t.Errorf("refusal %q does not name the tileset and what it said", err)
	}
}

const bothClassesTSX = `<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" name="both" class="rock" tilewidth="8" tileheight="8" tilecount="2" columns="2">
 <image source="dungeon.png" width="16" height="8"/>
 <tile id="0" type="wall"/>
</tileset>`

// The tile's class outranks its tileset's. Only a tile carrying both can tell
// the two orders apart, and without one the fallback can be written backwards
// and every test still passes.
func TestLoadMap_ATilesClassOutranksItsTilesets(t *testing.T) {
	_, db := loadTiledMap(t, tiledMap(t, bothClassesTSX, mapOf(2, 1,
		layerOf("ground", 2, 1, "", "1,2")), "level.tmx"))

	if got := tileTypeAt(t, db, 0, 0); got != "wall" {
		t.Errorf("tile_type = %q, want the tile's own class", got)
	}
	if got := tileTypeAt(t, db, 1, 0); got != "rock" {
		t.Errorf("tile_type = %q, want the tileset's class where the tile has none", got)
	}
}

// What these cells are handed to deletes every tile they do not mention, so a
// map that describes no cells is not an empty map — it is the level gone. The
// parser accepts a map with no dimensions because nothing below it had a reason
// to care; this is where the reason lives.
func TestLayerTilesOfTiled_RefusesAMapThatDescribesNoCells(t *testing.T) {
	cases := []struct {
		name string
		m    *tiled.Map
		says string
	}{
		{"no dimensions at all", &tiled.Map{Name: "in-memory"}, "0×0"},
		{
			"a layer smaller than its map",
			&tiled.Map{
				Name: "in-memory", Width: 2, Height: 2,
				Layers: []tiled.Layer{{Name: "ground", Width: 1, Height: 1, Data: []uint32{0}}},
			},
			`layer "ground" is 1×1`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := LayerTilesOfTiled(c.m, "memory")
			if err == nil {
				t.Fatal("accepted a map that would import as no cells")
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Errorf("refusal %q does not say %q", err, c.says)
			}
		})
	}
}

// ── What the loader hands its other consumers ───────────────────────────────

// The parsed map is still needed for spawns and startup tile-size validation;
// game tile drawing reads its imported entities instead.
func TestLoadMap_ReturnsTheParsedMapForSpawnAndMetadata(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(3, 2,
		layerOf("ground", 3, 2, "", "2,1,2,\n2,2,2")), "level.tmx")

	_, src, err := LoadMap(context.Background(), svc, db, path)
	if err != nil {
		t.Fatalf("LoadMap: %v", err)
	}
	if src == nil {
		t.Fatal("a Tiled map came back with no source metadata")
	}
	if len(src.Layers) != 1 || src.Layers[0].Name != "ground" {
		t.Errorf("the map has layers %+v, want the one named ground", src.Layers)
	}
	// Import resolved the tileset path before projecting visual components.
	if !src.Drawable() {
		t.Error("the map came back with unresolved tilesets, so nothing can be drawn from it")
	}
	if got := src.Tilesets[0].Tileset.Image.Path; got == "" {
		t.Error("the tileset's image was never resolved to a path")
	}
}
