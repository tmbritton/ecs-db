package tilemap

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/world"
)

type mapDef struct {
	Width  int      `toml:"width"`
	Height int      `toml:"height"`
	Rows   []string `toml:"rows"`
}

// LoadMap reads a map file, brings the Tile entities in the database in line
// with it, and returns a populated TileGrid.
//
// It used to be a one-time bootstrap: it counted Tile entities and created none
// if any existed, which made the database the source of truth after the first
// run and the file decoration. A map edited afterwards loaded without error and
// changed nothing. Loading now diffs — see SyncTiles for what the file owns and
// what survives it.
//
// Loading a Tiled map needs storage.EnsureInterpreterTables to have run: the
// spawns table is how an object that has left the map takes its entity with it.
// The character format has no objects and does not.
//
// The second result is the parsed map, for callers that need the file rather
// than the grid — the renderer, which draws layers and tilesets that
// comp_tile has no room for. Nil for the character format, which has neither.
func LoadMap(ctx context.Context, svc *world.EntityService, db *sql.DB, path string) (*TileGrid, *tiled.Map, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("LoadMap: reading %q: %w", path, err)
	}
	file, err := readMap(path, data)
	if err != nil {
		return nil, nil, err
	}

	if _, err := SyncTiles(ctx, svc, db, file.Tiles); err != nil {
		return nil, nil, fmt.Errorf("LoadMap: importing %q: %w", path, err)
	}

	// Spawns come from the same file as the tiles and are loaded with them: a
	// caller that wanted one without the other would be asking for half a map.
	// Refusals are returned rather than raised — one object with a typo in its
	// type should not stop the other nineteen, and the caller decides how loud
	// to be about it.
	spawns, err := SyncSpawns(ctx, svc, db, filepath.Clean(path), file.Source)
	if err != nil {
		return nil, nil, fmt.Errorf("LoadMap: spawning from %q: %w", path, err)
	}
	for _, refused := range spawns.Refused {
		log.Printf("tilemap: %s: %s", path, refused)
	}
	// Warnings are entities that were created and should not have been — a type
	// at validationLevel "warning" missing a required component. Louder than
	// nothing and quieter than a refusal, which is what "warning" asked for.
	for _, warning := range spawns.Warnings {
		log.Printf("tilemap: %s: created anyway: %s", path, warning)
	}

	grid := NewTileGrid(file.Width, file.Height)
	if err := grid.Rebuild(ctx, db); err != nil {
		return nil, nil, fmt.Errorf("LoadMap: rebuilding grid: %w", err)
	}
	return grid, file.Source, nil
}

// loaded is what a map file yields.
//
// A struct rather than four returns and an error: the third and fourth were
// already a width and a height nobody could tell apart at a call site, and the
// parsed map makes five.
type loaded struct {
	Tiles  map[Point]TileState
	Width  int
	Height int
	// Source is the parsed Tiled map, and nil for the character format — which
	// has no layers, no tilesets and nothing to draw from.
	Source *tiled.Map
}

// readMap turns a map file into the cells it describes and the size of the grid
// they sit in, whichever of the two formats it holds.
//
// Dispatched on the first thing in the file rather than on its extension, which
// is what tiled.Parse does to tell its own two serialisations apart, and which
// makes a renamed file load as what it is. There is no third format to be
// ambiguous with: a Tiled map opens with an element or an object, and the
// character format opens with a key. What a Tiled file opens with is tiled's to
// know, not this package's.
//
// The character format is here until Story 7 migrates the one map that uses it.
// It is not a second way to write a map — it is the way the old map is written,
// and it goes with the file.
func readMap(path string, data []byte) (loaded, error) {
	if tiled.LooksLike(data) {
		return readTiled(path, data)
	}
	return readTOML(path, data)
}

// readTOML reads the bespoke character format this engine started with.
func readTOML(path string, data []byte) (loaded, error) {
	var def mapDef
	if err := toml.Unmarshal(data, &def); err != nil {
		return loaded{}, fmt.Errorf("LoadMap: parsing %q: %w", path, err)
	}
	if err := def.check(path); err != nil {
		return loaded{}, err
	}
	return loaded{Tiles: tilesOf(def), Width: def.Width, Height: def.Height}, nil
}

// check refuses a file that parsed but does not describe a map.
//
// This is load-bearing now in a way it was not before. TOML ignores keys it was
// not asked for, so `rowz = [...]` — a typo, a renamed key, a file that is not
// a map at all — unmarshals cleanly into a mapDef of zeroes. Under the old
// bootstrap that was harmless: no rows meant no tiles to create and the guard
// skipped anyway. Under a diff it means the file describes no cells, and every
// tile in the database is a cell the file dropped. One misspelled key would
// delete the map.
//
// The row-count and row-width checks are the same argument one step further: a
// file truncated halfway is a map with real rows and missing ones, and nothing
// downstream could tell that from an author who meant it.
func (d mapDef) check(path string) error {
	if d.Width <= 0 || d.Height <= 0 {
		return fmt.Errorf("LoadMap: %q declares no size (%d×%d) — it may not be a map file",
			path, d.Width, d.Height)
	}
	if len(d.Rows) != d.Height {
		return fmt.Errorf("LoadMap: %q says height %d and holds %d rows",
			path, d.Height, len(d.Rows))
	}
	for y, row := range d.Rows {
		if n := len([]rune(row)); n != d.Width {
			return fmt.Errorf("LoadMap: %q says width %d and row %d holds %d cells",
				path, d.Width, y, n)
		}
	}
	return nil
}

// tilesOf turns the character rows into the cells the file describes.
//
// A character that is neither '.' nor '#' describes no tile, so the cell is
// absent rather than empty — and a re-import deletes whatever used to be there,
// which is the same answer as a map that shrank.
//
// The rows are indexed as runes rather than ranged over as a string. Ranging
// yields byte offsets, so one non-ASCII character used to shift every cell after
// it to the right and push the last ones outside the grid — where TileGrid keeps
// their entity ids but refuses to store their passability, so setTilePassable
// would find an id, write the row, and no-op on the grid. A one-time bootstrap
// wrote that once; a diff writes it on every load.
func tilesOf(def mapDef) map[Point]TileState {
	want := make(map[Point]TileState)
	for y, row := range def.Rows {
		for x, ch := range []rune(row) {
			switch ch {
			case '.':
				want[Point{X: x, Y: y}] = TileState{Passable: true, TileType: "floor"}
			case '#':
				want[Point{X: x, Y: y}] = TileState{Passable: false, TileType: "wall"}
			}
		}
	}
	return want
}
