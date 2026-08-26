package tilemap

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/world"
)

// LoadMap reads a Tiled map, brings the Tile entities and the object layers'
// spawns in the database in line with it, and returns a populated TileGrid.
//
// It used to be a one-time bootstrap: it counted Tile entities and created none
// if any existed, which made the database the source of truth after the first
// run and the file decoration. A map edited afterwards loaded without error and
// changed nothing. Loading now diffs — see SyncTiles for what the file owns and
// what survives it.
//
// One format. The engine started with a bespoke TOML of '.' and '#' characters,
// and Story 7 migrated the one map that used it and deleted the reader with the
// file: a second way to load a map is a second thing to keep working, and Forge
// writes Tiled. A file that is not a Tiled map is refused by name, by the
// parser, rather than tried as something else.
//
// It needs storage.EnsureInterpreterTables to have run: the spawns table is how
// an object that has left the map takes its entity with it.
//
// The second result is the parsed map, for callers that need the file rather
// than the grid — the renderer, which draws layers and tilesets that comp_tile
// has no room for. Never nil when the error is nil.
func LoadMap(ctx context.Context, svc *world.EntityService, db *sql.DB, path string) (*TileGrid, *tiled.Map, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("LoadMap: reading %q: %w", path, err)
	}
	file, err := readTiled(path, data)
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
	// Source is the parsed map. The renderer draws from it rather than from
	// comp_tile, which holds one row per cell and so cannot say what is
	// stacked on it.
	Source *tiled.Map
}
