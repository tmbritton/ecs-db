package tilemap

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

// readTiled turns a Tiled map file into the cells it describes and the size of
// the grid they sit in.
//
// The tilesets are resolved through os.ReadFile against the directory the map
// was read from, which is where Tiled writes them relative to. A map that names
// a tileset nobody shipped is refused here rather than loaded with a hole in it.
func readTiled(path string, data []byte) (loaded, error) {
	m, err := tiled.Parse(data, path)
	if err != nil {
		return loaded{}, err
	}
	// Point is a square cell, and so is everything that reads a TileGrid: A*
	// costs one per step, line of sight walks a straight line through it. An
	// isometric or hexagonal map's coordinates are neither, and loading one
	// would put every tile somewhere plausible and wrong.
	//
	// An absent orientation is orthogonal. Tiled always writes the attribute;
	// a file that does not is not thereby claiming to be a hex map.
	if err := checkOrientation(m); err != nil {
		return loaded{}, err
	}
	if err := m.ResolveTilesets(filepath.Dir(path), os.ReadFile); err != nil {
		return loaded{}, err
	}
	layers, err := LayerTilesOfTiled(m, MapID(path, m))
	if err != nil {
		return loaded{}, err
	}
	return loaded{LayerTiles: layers, Width: m.Width, Height: m.Height, Source: m}, nil
}

func checkOrientation(m *tiled.Map) error {
	if m.Orientation != "" && m.Orientation != "orthogonal" {
		return fmt.Errorf("tilemap: %s is %s, and this engine's grid is square cells", m.Name, m.Orientation)
	}
	return nil
}

// MapID is shared by import and game rendering. An authored ID survives file
// renames; a map without one falls back to its cleaned path, like spawns.
func MapID(path string, m *tiled.Map) string {
	if m != nil {
		if id := m.Properties.Get(tiled.PropMapID); id != "" {
			return id
		}
	}
	return filepath.Clean(path)
}

// checkShape refuses a map that would import as fewer cells than it has.
//
// Re-import *deletes* every (layer, cell) the map no longer describes, so a
// truncated layer must be refused rather than silently deleting valid tiles.
//
// It is narrower than "refuse a map that imports as nothing", and deliberately:
// a Tiled map whose tile layer has been deleted really does describe no cells,
// and emptying the world is the file-wins rule working rather than a mistake to
// catch. What is refused is a map that contradicts itself — one that says how
// big it is and then is not that.
//
// A size of zero is what that looks like here. The parser refuses a negative
// one and accepts a map with no dimensions at all, deliberately — nothing below
// it had a reason to care. Tiled always writes them, so the file that arrives
// without them is a hand-edited one, a generator's, or Forge's MAP mode, which
// is the caller this package is being built for.
//
// A layer smaller than its map is the same failure one level down, and it can
// only arrive from a map built in memory: the parser checks every layer it
// reads. Out of range, Layer.TileAt answers "empty" rather than panicking —
// which is right for a renderer walking a viewport, and here would quietly turn
// the missing part of a layer into cells the file no longer has.
func checkShape(m *tiled.Map) error {
	if m.Width <= 0 || m.Height <= 0 {
		return fmt.Errorf("tilemap: %s is %d×%d tiles, which describes no map",
			m.Name, m.Width, m.Height)
	}
	for _, layer := range m.Layers {
		if layer.Width != m.Width || layer.Height != m.Height {
			return fmt.Errorf("tilemap: %s: layer %q is %d×%d but the map is %d×%d",
				m.Name, layer.Name, layer.Width, layer.Height, m.Width, m.Height)
		}
	}
	return nil
}

// tileOf is the tileset a global id belongs to and the id of the tile within
// it, or a refusal naming the cell.
//
// Refused by position rather than skipped. A gid nothing can resolve is a map
// referring to art that was not shipped with it, and loading the rest of the
// map would leave a hole with nothing said about it — which looks like a hole
// somebody drew.
//
// Three ways to be unresolvable, and they are three different mistakes: a map
// with no tilesets at all, a gid below every first gid the map declares, and a
// gid its tileset does not hold. The last is the common one, because Tiled
// addresses tilesets by range and the last range has no top — so a stale gid
// from a tileset that has since shrunk resolves to it and reads as a tile that
// is not there.
func tileOf(m *tiled.Map, where string, gid uint32) (*tiled.Tileset, uint32, error) {
	if len(m.Tilesets) == 0 {
		return nil, 0, fmt.Errorf("%s, and the map declares no tilesets at all", where)
	}
	ref, local, ok := m.TilesetFor(gid)
	if !ok {
		return nil, 0, fmt.Errorf("%s, which is below the first tile of every tileset the map declares", where)
	}
	if ref.Tileset == nil {
		// Unreachable through LoadMap, which resolves its tilesets before it
		// reads them. Kept because the alternative is a nil dereference, and
		// because this is what Forge's MAP mode will call with a map it built
		// rather than read.
		return nil, 0, fmt.Errorf("%s, from a tileset that was never read", where)
	}
	if !holds(ref.Tileset, local) {
		return nil, 0, fmt.Errorf("%s, which is tile %d of tileset %q, and that tileset does not hold it",
			where, local, ref.Tileset.Name)
	}
	return ref.Tileset, local, nil
}

// holds reports whether a tileset has a tile at a local id, which is a
// different question for each of the two kinds of tileset.
//
// A sheet is a grid cut out of one image, so its ids run 0 to TileCount-1 with
// nothing missing and the count is the bound. A collection is one file per
// tile, and Tiled leaves a gap when a tile is deleted from one — so its ids are
// whatever it declares, and counting them answers neither question: id 3 of a
// three-tile collection is ordinary, and id 1 of that same collection may be
// the hole. Membership is the only test that is right for both, and range is
// the only one that is right for a sheet, whose tiles mostly declare nothing
// and so appear in Tiles not at all.
func holds(ts *tiled.Tileset, local uint32) bool {
	if ts.Collection() {
		_, ok := ts.Tiles[local]
		return ok
	}
	return int(local) < ts.TileCount
}

// stateOf reads a tile's class, not collision or visibility metadata.
// tile_type is the tile's class, then its tileset's, then empty. Nothing draws
// it any more — the renderer took its colours from it until Story 7 deleted
// them, and appearance comes from the tileset image now. What is left reading
// comp_tile.tile_type is SyncTiles' own diff, deciding whether to write it
// again. It stays because it is the only place a map says what a cell *is*
// rather than what it looks like, and a game rule that wants "is this lava" has
// nowhere else to ask — but it is a column with no consumer today, and the
// story records that rather than leaving it to be discovered.
func stateOf(ts *tiled.Tileset, local uint32, where string) (TileState, error) {
	if props := ts.Tiles[local].Properties; props.Has(tiled.PropPassable) {
		return TileState{}, fmt.Errorf("%s: deprecated tile artwork property passable=%q; author a Passability component on a separate occupant entity", where, props.Get(tiled.PropPassable))
	}
	if ts.Properties.Has(tiled.PropPassable) {
		return TileState{}, fmt.Errorf("%s: tileset %q has deprecated tile artwork property passable=%q; author a Passability component on a separate occupant entity", where, ts.Name, ts.Properties.Get(tiled.PropPassable))
	}
	entityType := ts.Tiles[local].Properties.Get("entityType")
	if entityType == "" {
		entityType = ts.Properties.Get("entityType")
	}
	if entityType == "" {
		return TileState{}, fmt.Errorf("%s: tileset tile has no entityType reference template", where)
	}
	kind := ts.Tiles[local].Type
	if kind == "" {
		kind = ts.Class
	}
	return TileState{TileType: kind, EntityType: entityType}, nil
}
