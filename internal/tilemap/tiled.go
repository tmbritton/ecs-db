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
	if m.Orientation != "" && m.Orientation != "orthogonal" {
		return loaded{}, fmt.Errorf("tilemap: %s is %s, and this engine's grid is square cells",
			m.Name, m.Orientation)
	}
	if err := m.ResolveTilesets(filepath.Dir(path), os.ReadFile); err != nil {
		return loaded{}, err
	}
	want, err := tilesOfTiled(m)
	if err != nil {
		return loaded{}, err
	}
	return loaded{Tiles: want, Width: m.Width, Height: m.Height, Source: m}, nil
}

// tilesOfTiled is the cell of every position the map describes a tile at.
//
// Iterated over the map's own dimensions rather than over the layers' data,
// because a cell nothing draws in is a cell and not a gap — and because the
// two guards below make the map's dimensions the only ones that matter.
func tilesOfTiled(m *tiled.Map) (map[Point]TileState, error) {
	if err := checkShape(m); err != nil {
		return nil, err
	}
	want := make(map[Point]TileState)
	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			state, ok, err := cellAt(m, x, y)
			if err != nil {
				return nil, err
			}
			if ok {
				want[Point{X: x, Y: y}] = state
			}
		}
	}
	return want, nil
}

// checkShape refuses a map that would import as fewer cells than it has.
//
// The same argument mapDef.check makes about the character format, and it has
// to be made again because the reason is the caller rather than the format:
// what these cells are handed to *deletes* every tile they do not mention. A
// map that describes no cells is not an empty map, it is every tile in the
// database gone, with no error to notice.
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

// cellAt is the tile a cell holds, and reports false when it holds none.
//
// **The topmost non-empty tile is the cell's tile**, and it decides both
// passability and type. Not a preference: comp_tile holds one row per cell, so
// the loader has to choose one tile out of a stack of layers, and the one the
// author drew on top is the one they see there.
//
// A cell every layer leaves empty is not a tile and creates no entity. It is
// still impassable, because TileGrid has no entry for it — which is a different
// statement from a tile whose tileset says nothing about passability. Nothing to
// stand on is not the same as a floor nobody described.
func cellAt(m *tiled.Map, x, y int) (TileState, bool, error) {
	for i := len(m.Layers) - 1; i >= 0; i-- {
		layer := m.Layers[i]
		// Visibility is not consulted. Hiding a layer is what the editor shows
		// you, not what the map is: an author who hides the wall layer to look
		// at the floor underneath it, saves, and finds every wall gone has been
		// robbed by a checkbox. Story 5 honours Visible for drawing, which is
		// what it is for.
		gid := layer.TileAt(x, y).GID
		if gid == 0 {
			continue
		}
		where := fmt.Sprintf("tilemap: %s: layer %q: the tile at (%d,%d) has global id %d",
			m.Name, layer.Name, x, y, gid)
		ts, local, err := tileOf(m, where, gid)
		if err != nil {
			return TileState{}, false, err
		}
		state, err := stateOf(ts, local, where)
		if err != nil {
			return TileState{}, false, err
		}
		return state, true, nil
	}
	return TileState{}, false, nil
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

// stateOf reads a tile's properties as the two fields comp_tile keeps.
//
// Passability is the tile's own declaration, then its tileset's, then passable.
// A map is a floor with obstacles on it: the overwhelming majority of tiles in
// any map are floor, and a default that made a 200-tile decoration set
// unwalkable until every tile in it was declared is a default that gets
// scripted around. The tileset-level property is how a set that is mostly solid
// says so once.
//
// A passable that is declared and will not read as a boolean is refused rather
// than defaulted. Tiled makes you pick a type when you add a property, and an
// int 0 or the string "no" means someone was trying to say something — falling
// back to the default there draws a wall the player walks through, which is
// invisible until somebody tests the geometry.
//
// tile_type is the tile's class, then its tileset's, then empty. The renderer
// draws anything that is not "wall" as floor, so a tile with no class draws
// rather than disappears.
func stateOf(ts *tiled.Tileset, local uint32, where string) (TileState, error) {
	props := ts.Tiles[local].Properties
	passable, declared := props.Bool(tiled.PropPassable)
	switch {
	case declared:
	case props.Has(tiled.PropPassable):
		return TileState{}, fmt.Errorf("%s, and its %s is %q rather than true or false",
			where, tiled.PropPassable, props.Get(tiled.PropPassable))
	default:
		if passable, declared = ts.Properties.Bool(tiled.PropPassable); !declared {
			if ts.Properties.Has(tiled.PropPassable) {
				return TileState{}, fmt.Errorf("%s, and tileset %q says its tiles' %s is %q rather than true or false",
					where, ts.Name, tiled.PropPassable, ts.Properties.Get(tiled.PropPassable))
			}
			passable = true
		}
	}
	kind := ts.Tiles[local].Type
	if kind == "" {
		kind = ts.Class
	}
	return TileState{Passable: passable, TileType: kind}, nil
}
