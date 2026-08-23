package tiled

import (
	"fmt"
	"path"
)

// Opener reads a file a map refers to.
//
// Injected rather than assumed, which is what keeps every test in this package
// a string — the property that made the map parser cheap to get right. The
// caller in Story 4 passes os.ReadFile.
type Opener func(name string) ([]byte, error)

// ResolveTilesets reads the tilesets a map refers to and attaches them.
//
// dir is the directory the map was read from, because Tiled writes a tileset's
// source relative to the map. An embedded tileset resolves without the opener
// being called: there is no file to read, and asking for one would be a read
// that cannot succeed.
func (m *Map) ResolveTilesets(dir string, open Opener) error {
	for i := range m.Tilesets {
		ref := &m.Tilesets[i]
		if ref.Tileset != nil {
			continue
		}
		var (
			data []byte
			from string
			err  error
		)
		if ref.Source == "" {
			data, from = ref.Embedded, dir
		} else {
			at := resolvePath(dir, ref.Source)
			from = path.Dir(at)
			data, err = open(at)
			if err != nil {
				// The path actually tried, not the attribute: when a relative
				// reference is wrong, where it looked is the one fact worth
				// having. And the map, because a tileset is shared and the
				// question is which map pulled it in.
				return fmt.Errorf("tiled: %s: reading tileset %q (%s): %w",
					m.Name, ref.Source, at, err)
			}
		}
		// The tileset's directory, not the map's: an image is relative to the
		// tileset that names it, and the two are in different directories in
		// every Tiled project laid out normally.
		ts, err := ParseTileset(data, tilesetName(m, ref), from)
		if err != nil {
			return err
		}
		ref.Tileset = ts
	}
	return nil
}

// tilesetName is what a refusal calls the tileset. An embedded one has no name
// of its own, so it borrows the map's — which is where it is.
func tilesetName(m *Map, ref *TilesetRef) string {
	if ref.Source != "" {
		return ref.Source
	}
	return "the tileset embedded in " + m.Name
}

// resolvePath joins a reference to the directory it is relative to, unless it
// is not relative.
//
// path.Join does not treat a leading slash as anchoring, so an absolute source
// — which Tiled writes the moment art lives outside the project directory —
// came out as "maps/home/tom/art/dungeon.png" and then could not be opened.
func resolvePath(dir, ref string) string {
	switch {
	case ref == "":
		return ""
	case path.IsAbs(ref):
		return ref
	default:
		return path.Join(dir, ref)
	}
}
