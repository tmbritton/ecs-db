// Package tiled reads the map and tileset formats the Tiled editor writes.
//
// It knows about files and nothing else: no entities, no database, no grid, no
// images. That is what lets every one of its tests be a string, and it is why
// internal/tilemap depends on this package and never the other way round.
//
// Tiled writes two interchangeable serialisations of the same map — .tmx (XML)
// and .tmj (JSON) — and both parse into the value below, so nothing above this
// package learns which was on disk.
package tiled

import (
	"bytes"
	"fmt"
	"strconv"
)

// Map is a Tiled map, whichever serialisation it arrived in.
type Map struct {
	// Name is the file this map was read from, for refusals raised after
	// parsing — a tileset that will not open is the map's problem to report.
	Name                  string
	Width, Height         int // in tiles
	TileWidth, TileHeight int // in pixels
	Orientation           string
	RenderOrder           string
	Infinite              bool

	// Layers are the tile layers in file order, which is draw order: a later
	// layer covers an earlier one.
	Layers []Layer
	// ObjectGroups are the object layers, kept in file order.
	//
	// Nothing in this story reads them. They are here because Story 6 spawns
	// entities from them, and a parser that dropped what it did not use would
	// have to be reopened to add it back — the argument Epic 13 Story 1 made
	// about unknown fields in a machine file.
	ObjectGroups []ObjectGroup
	// Tilesets are the map's tileset references, unresolved.
	Tilesets   []TilesetRef
	Properties Properties
}

// Layer is one tile layer.
type Layer struct {
	ID            int
	Name          string
	Width, Height int
	Visible       bool
	Opacity       float64
	// Data is one raw global tile id per cell, row-major, of exactly
	// Width*Height. Raw: the flip flags are still in the top bits.
	//
	// Raw because it is lossless and because which bits are flags is a Tiled
	// version question rather than a fact — a caller that wants them can have
	// them. TileAt is what every caller in the engine actually wants.
	Data       []uint32
	Properties Properties
}

// TileAt is the tile at a cell, with its flags separated from its id.
//
// Out of range is the empty tile rather than a panic: a renderer walking a
// viewport that overhangs the map is ordinary, and the alternative is every
// caller bounds-checking the same way.
func (l Layer) TileAt(x, y int) Tile {
	if x < 0 || y < 0 || x >= l.Width || y >= l.Height {
		return Tile{}
	}
	return TileOf(l.Data[y*l.Width+x])
}

// Tile is a global tile id with its transform flags taken out of it.
type Tile struct {
	// GID is the tile's global id, or 0 for an empty cell.
	GID                 uint32
	FlipH, FlipV, FlipD bool
	// RotatedHex is Tiled 1.9's hexagonal 120° rotation. Meaningless on an
	// orthogonal map, and carried anyway because dropping it silently is what
	// makes a rotated tile draw wrong two stories from here.
	RotatedHex bool
}

// The transform flags Tiled packs into the top of a global tile id.
//
// Four, not three. Tiled 1.9 added the hexagonal rotation bit, so the widely
// quoted 0x1FFFFFFF clear mask leaves it in the id — which produces a tile
// index 268 million too high for any map a current Tiled wrote.
const (
	flagFlipH      uint32 = 0x80000000
	flagFlipV      uint32 = 0x40000000
	flagFlipD      uint32 = 0x20000000
	flagRotatedHex uint32 = 0x10000000

	gidMask uint32 = 0x0FFFFFFF
)

// TileOf splits a raw global tile id into its id and its flags.
func TileOf(raw uint32) Tile {
	return Tile{
		GID:        raw & gidMask,
		FlipH:      raw&flagFlipH != 0,
		FlipV:      raw&flagFlipV != 0,
		FlipD:      raw&flagFlipD != 0,
		RotatedHex: raw&flagRotatedHex != 0,
	}
}

// ObjectGroup is an object layer.
type ObjectGroup struct {
	ID         int
	Name       string
	Visible    bool
	Objects    []Object
	Properties Properties
}

// Object is one object on an object layer: a spawn, in this engine's use.
type Object struct {
	ID   int
	Name string
	// Type is the object's class. Tiled 1.9 renamed the attribute from "type"
	// to "class" and 1.10 renamed it back, so both are read.
	Type string
	// X and Y are pixels, as Tiled writes them — and the origin differs by
	// shape: a *tile* object is positioned by its bottom-left corner and every
	// other object by its top-left. Story 6 places spawns from these and will
	// put every tile object one tile too low if it forgets.
	X, Y          float64
	Width, Height float64
	Rotation      float64
	Visible       bool
	// GID is the raw global tile id for a tile object, flags and all, and zero
	// for every other shape. Use Tile.
	GID uint32
	// Template names a .tx file this object is an instance of, unresolved here
	// for the reason TilesetRef.Source is. It matters more than it looks: a
	// template instance carries almost nothing inline — its name, class and
	// properties live in the template — so an object with one set and nothing
	// else is not an empty object.
	Template   string
	Properties Properties
}

// Tile is the object's tile with its flags separated, for a tile object.
//
// The accessor exists for the reason Layer.TileAt does: GID is raw, and the
// obvious use of it as an index is wrong by 0x80000000 the first time somebody
// flips a spawn in the editor.
func (o Object) Tile() Tile { return TileOf(o.GID) }

// TilesetRef is a map's reference to a tileset, deliberately unresolved.
//
// Source names an external .tsx and is empty for a tileset embedded in the map.
// Resolving either needs the TSX parser and a filesystem, which is Story 2 —
// and which would make this package impossible to test from a string.
type TilesetRef struct {
	FirstGID uint32
	Source   string
	// Embedded is the raw element for a tileset defined inline, kept whole —
	// attributes and all — so it parses with the same code a .tsx file does.
	Embedded []byte
	// Tileset is the resolved tileset, and is nil until ResolveTilesets has
	// run. Nil rather than a zero value: a tileset nobody has read is not a
	// tileset with no tiles in it.
	Tileset *Tileset
}

// TilesetFor is the tileset a global tile id belongs to, and the id within it.
//
// Tilesets are addressed by range: each carries the first gid it owns, and it
// owns every id up to the next one's. Every caller from Story 4 onwards needs
// this and would otherwise write the descending scan itself, which is the copy
// that goes wrong.
//
// Reports false for gid 0, which is the empty cell. Tiled numbers first gids
// from 1 so nothing would match it anyway, but a file this package did not
// write may say otherwise and an empty cell has no tile either way.
//
// The highest first gid at or below, not the last one that fits: Tiled writes
// its tilesets in ascending order and a hand-edited or third-party file need
// not.
func (m *Map) TilesetFor(gid uint32) (TilesetRef, uint32, bool) {
	id := TileOf(gid).GID
	if id == 0 {
		return TilesetRef{}, 0, false
	}
	best, found := TilesetRef{}, false
	for _, ts := range m.Tilesets {
		if ts.FirstGID <= id && (!found || ts.FirstGID > best.FirstGID) {
			best, found = ts, true
		}
	}
	if !found {
		return TilesetRef{}, 0, false
	}
	return best, id - best.FirstGID, true
}

// Properties are Tiled's custom properties, by name.
//
// Values are kept as the strings Tiled wrote plus the type it declared, so a
// type this package does not model survives rather than being dropped — the
// same argument the machine parser makes about unknown fields. The accessors
// are what read them back as something typed.
type Properties map[string]Property

// Property is one custom property.
type Property struct {
	Type string // "string", "int", "float", "bool", or whatever Tiled wrote
	// PropertyType names the custom type or enum a "class"-typed property is an
	// instance of, and is empty for the built-in types.
	PropertyType string
	Value        string
}

// Get is a property's value as written, or "" when there is none.
//
// Not String: a method called String that takes an argument is not a
// fmt.Stringer and reads like one, which is a trap for the next person to
// format a Properties value.
func (p Properties) Get(name string) string { return p[name].Value }

// Has reports whether a property was declared at all, which is different from
// its value being empty.
func (p Properties) Has(name string) bool {
	_, ok := p[name]
	return ok
}

// Int, Float and Bool are a property read as the type it declares. The second
// result is false when the property is absent or will not parse, so a caller
// can tell "not set" from "set to zero" — which for passable is the difference
// between a tileset that says nothing and one that says no.
// No "was it declared" check in any of the three: an absent property is the
// zero Property, whose empty value parses as none of these types, so the answer
// is already false. Has is what asks the other question.
func (p Properties) Int(name string) (int, bool) {
	v, err := strconv.Atoi(p[name].Value)
	return v, err == nil
}

func (p Properties) Float(name string) (float64, bool) {
	v, err := strconv.ParseFloat(p[name].Value, 64)
	return v, err == nil
}

func (p Properties) Bool(name string) (bool, bool) {
	// Tiled writes "true"/"false" in both serialisations. ParseBool also takes
	// "1", "t" and five other spellings the format never produces; accepting
	// them would be inventing a format.
	switch p[name].Value {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

// Parse reads a map from either serialisation.
//
// name is the file it came from and appears in every refusal: the message is
// what someone reads when a map they have just exported will not load, and "an
// unsupported encoding" without a filename is a search rather than a report.
func Parse(data []byte, name string) (*Map, error) {
	var (
		m   *Map
		err error
	)
	switch first := firstMeaningfulByte(data); first {
	case '{':
		m, err = parseTMJ(data, name)
	case '<':
		m, err = parseTMX(data, name)
	default:
		return nil, fmt.Errorf("tiled: %s is neither a .tmx nor a .tmj map", name)
	}
	if err != nil {
		return nil, err
	}
	// Recorded for the refusals raised after parsing: a tileset that will not
	// open is the map's problem to report, and by then the caller's name for it
	// is gone.
	m.Name = name
	return m, nil
}

// LooksLike reports whether data begins the way a map this package reads
// begins.
//
// For a caller with more than one format to choose between — the loader in
// internal/tilemap still reads the character map this epic is replacing — which
// would otherwise keep its own copy of what a Tiled file starts with, in a
// package that does not know. Whether it really is one is Parse's answer.
func LooksLike(data []byte) bool {
	switch firstMeaningfulByte(data) {
	case '{', '<':
		return true
	default:
		return false
	}
}

// firstMeaningfulByte skips leading whitespace and a UTF-8 byte-order mark,
// which Tiled does not write but an editor in between may have added.
func firstMeaningfulByte(data []byte) byte {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	for _, b := range data {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return b
		}
	}
	return 0
}
