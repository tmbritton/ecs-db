package tiled

import (
	"encoding/xml"
	"fmt"
)

// Tileset says how to cut an image into tiles and what each one means.
//
// The second half is why this epic exists: the ASCII map it replaces could only
// ever say "wall" or "not wall", and a tile's properties are where passability,
// terrain and class come from instead.
type Tileset struct {
	Name string
	// Class is Tiled 1.9's tileset-level class, the sibling of a tile's Type.
	Class                 string
	TileWidth, TileHeight int
	TileCount             int
	// Columns is 0 for a collection of images, which has no sheet to have
	// columns in. Collection reports which kind this is.
	Columns         int
	Spacing, Margin int
	// TileOffsetX and TileOffsetY shift every tile when it is drawn, and are
	// how a tileset of tiles taller than the grid lines up. Dropping them draws
	// the whole map in the wrong place, which looks like a layout fault rather
	// than a missing attribute.
	TileOffsetX, TileOffsetY int
	Image                    Image
	// Tiles holds only the tiles that declare something, keyed by *local* id.
	// A 256-tile sheet with two walls has two entries.
	Tiles      map[uint32]TilesetTile
	Properties Properties
}

// Collection reports whether this is a collection of images — one file per
// tile, no sheet — rather than a single sheet cut into a grid.
//
// Tiled writes columns="0" and no tileset-level image for one, and it is the
// obvious way to author prop and creature art. A parser that assumed a sheet
// refused them outright.
func (t *Tileset) Collection() bool { return t.Image.Source == "" }

// Image is the sheet a tileset cuts up, or the one file a collection's tile is.
type Image struct {
	// Source is the attribute as written, relative to the tileset file.
	Source string
	// Path is Source resolved against the directory the tileset was read from,
	// and is empty until ResolveTilesets has run. Both are kept: the first is
	// what the file says and the second is what opens.
	Path          string
	Width, Height int
	// Trans is the colour key a magenta-keyed sheet uses, as Tiled writes it
	// ("ff00ff"), and empty when there is none.
	Trans string
}

// TilesetTile is one tile that carries something.
type TilesetTile struct {
	ID uint32
	// Type is Tiled's per-tile class.
	Type string
	// Image is the tile's own file, for a collection. Empty for a sheet tile,
	// whose picture is a rectangle of the tileset's image.
	Image      Image
	Properties Properties
}

// PropPassable names a former engine-specific Tiled property. The game
// refuses it on tile artwork; the TSX document tests still use it to prove
// lossless editing of an existing, otherwise arbitrary property.
const PropPassable = "passable"

// Passable reads the former property solely for TSX parse/write fidelity. It
// does not answer whether any mover can enter a cell; occupant interaction
// rules and the mover's components do that.
func (t *Tileset) Passable(local uint32) (bool, bool) {
	return t.Tiles[local].Properties.Bool(PropPassable)
}

// SourceRect is where a tile sits in the sheet.
//
// The arithmetic a renderer would otherwise write itself, from columns, margin
// and spacing — and the last thing in this epic anyone would get subtly wrong
// by hand. Reports false for an id the tileset does not hold.
func (t *Tileset) SourceRect(local uint32) (x, y, w, h int, ok bool) {
	// A collection has no grid: each tile is a whole file, and its rectangle is
	// that file. Reporting false is the honest answer rather than arithmetic
	// over a sheet that does not exist.
	if t.Collection() || t.Columns <= 0 || int(local) >= t.TileCount {
		return 0, 0, 0, 0, false
	}
	col := int(local) % t.Columns
	row := int(local) / t.Columns
	return t.Margin + col*(t.TileWidth+t.Spacing),
		t.Margin + row*(t.TileHeight+t.Spacing),
		t.TileWidth, t.TileHeight, true
}

type xmlTilesetFile struct {
	XMLName    xml.Name      `xml:"tileset"`
	Name       string        `xml:"name,attr"`
	Class      string        `xml:"class,attr"`
	TileWidth  int           `xml:"tilewidth,attr"`
	TileHeight int           `xml:"tileheight,attr"`
	TileCount  int           `xml:"tilecount,attr"`
	Columns    int           `xml:"columns,attr"`
	Spacing    int           `xml:"spacing,attr"`
	Margin     int           `xml:"margin,attr"`
	TileOffset xmlTileOffset `xml:"tileoffset"`
	Properties xmlProperties `xml:"properties"`
	Image      xmlImage      `xml:"image"`
	Tiles      []xmlTsxTile  `xml:"tile"`
}

type xmlTileOffset struct {
	X int `xml:"x,attr"`
	Y int `xml:"y,attr"`
}

type xmlImage struct {
	Source string `xml:"source,attr"`
	Trans  string `xml:"trans,attr"`
	Width  int    `xml:"width,attr"`
	Height int    `xml:"height,attr"`
}

type xmlTsxTile struct {
	ID         uint32        `xml:"id,attr"`
	Type       string        `xml:"type,attr"`
	Class      string        `xml:"class,attr"`
	Image      xmlImage      `xml:"image"`
	Properties xmlProperties `xml:"properties"`
}

// ParseTileset reads a tileset from a .tsx file or from the element a map
// embeds.
//
// One function for both, because they are the same element — which is only true
// because Story 1 captures an embedded tileset whole rather than keeping what
// was between its tags.
func ParseTileset(data []byte, name, dir string) (*Tileset, error) {
	switch firstMeaningfulByte(data) {
	case '{':
		return parseTSJ(data, name, dir)
	case '<':
		return parseTSX(data, name, dir)
	default:
		return nil, fmt.Errorf("tiled: %s is neither a .tsx nor a .tsj tileset", name)
	}
}

func parseTSX(data []byte, name, dir string) (*Tileset, error) {
	var wire xmlTilesetFile
	if err := xml.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("tiled: %s: %w", name, err)
	}
	ts := &Tileset{
		Name:        wire.Name,
		Class:       wire.Class,
		TileWidth:   wire.TileWidth,
		TileHeight:  wire.TileHeight,
		TileCount:   wire.TileCount,
		Columns:     wire.Columns,
		Spacing:     wire.Spacing,
		Margin:      wire.Margin,
		TileOffsetX: wire.TileOffset.X,
		TileOffsetY: wire.TileOffset.Y,
		Image:       wire.Image.convert(dir),
		Properties:  wire.Properties.convert(),
	}
	for _, tile := range wire.Tiles {
		kind := tile.Type
		if kind == "" {
			// Renamed to "class" in Tiled 1.9 and back to "type" in 1.10, the
			// same as on an object — so a project's files may use either
			// depending on which version last saved them.
			kind = tile.Class
		}
		if err := ts.addTile(TilesetTile{
			ID:         tile.ID,
			Type:       kind,
			Image:      tile.Image.convert(dir),
			Properties: tile.Properties.convert(),
		}, name); err != nil {
			return nil, err
		}
	}
	return ts, ts.check(name)
}

func (i xmlImage) convert(dir string) Image {
	return Image{
		Source: i.Source,
		Path:   resolvePath(dir, i.Source),
		Trans:  i.Trans,
		Width:  i.Width,
		Height: i.Height,
	}
}

// addTile refuses a second tile with an id another already has.
//
// Last-wins would be a tileset quietly disagreeing with the file it came from,
// and the only way to notice would be a tile drawn or walked wrongly.
func (t *Tileset) addTile(tile TilesetTile, name string) error {
	if t.Tiles == nil {
		t.Tiles = map[uint32]TilesetTile{}
	}
	if _, taken := t.Tiles[tile.ID]; taken {
		return fmt.Errorf("tiled: %s: two tiles both claim id %d", name, tile.ID)
	}
	t.Tiles[tile.ID] = tile
	return nil
}

// check is what a tileset has to be true of whichever serialisation it arrived
// in.
//
// The divisibility rule applies to a *sheet* only. A collection's columns is a
// display width someone dragged in the editor and has nothing to do with its
// tile count, and its ids are not consecutive — deleting a tile leaves a gap —
// so refusing on either would reject files Tiled writes routinely.
func (t *Tileset) check(name string) error {
	if t.TileWidth <= 0 || t.TileHeight <= 0 {
		return fmt.Errorf("tiled: %s: tiles are %dx%d, which is not a size",
			name, t.TileWidth, t.TileHeight)
	}
	if t.Spacing < 0 || t.Margin < 0 {
		return fmt.Errorf("tiled: %s: spacing %d and margin %d, which are not distances",
			name, t.Spacing, t.Margin)
	}
	if t.Collection() {
		return nil
	}
	if t.Columns <= 0 {
		return fmt.Errorf("tiled: %s: a sheet of %d tiles says it has %d columns",
			name, t.TileCount, t.Columns)
	}
	if t.TileCount%t.Columns != 0 {
		// A wrong columns puts every tile after the first row at the wrong
		// place in the image, and nothing downstream could tell — the renderer
		// would simply draw the wrong picture.
		return fmt.Errorf("tiled: %s: %d tiles do not fit into rows of %d",
			name, t.TileCount, t.Columns)
	}
	return nil
}
