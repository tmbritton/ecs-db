// Package mapcanvas turns a resolved Tiled map into something a template can
// render, and does no rendering itself.
//
// A sibling of internal/forge/chart, and for the same reason: everything that
// can be got wrong about a picture — which tile goes where, which layer covers
// which, what a flipped tile's transform is — is decided here, in a package a
// test can call, rather than inside a template nothing can assert on.
//
// It knows nothing about HTTP or the filesystem. Image paths become URLs
// through a function the caller supplies, because which route serves a
// project's files is the server's business and this package must not be able to
// invent one.
package mapcanvas

import (
	"sort"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

// Options is what the caller contributes to a canvas.
//
// One field, and it used to be five. Zoom, which layers are hidden, which layer
// is active and which tile is selected are all the browser's now — signals it
// owns outright — so the server neither takes them nor renders them, and a
// re-render has nothing of theirs to undo.
type Options struct {
	// AssetURL turns a tileset's image path into a URL the browser may fetch.
	// Injected because the route belongs to the server package, and a canvas
	// that built the URL itself would have to know about routing.
	AssetURL func(path string) string
}

// Canvas is a map, ready to draw.
type Canvas struct {
	// Cols and Rows are cells; W and H are the map's own pixels — unscaled,
	// because zoom is a CSS transform the browser applies and the server has no
	// opinion about it after the page opens. Both, because the grid is laid out
	// in one and the tiles are placed in the other.
	Cols, Rows   int
	TileW, TileH int
	W, H         int

	// Cells are every non-empty cell of every layer, in draw order. The
	// template emits them grouped, via CellsByLayer, so that a layer can be
	// hidden without asking the server for the map — which means the stacking
	// now rests on Layers being in file order rather than on this list alone.
	Cells []Cell
	// Layers is every tile layer the file has, in file order, whether drawn or
	// not: the panel lists what the map holds, not what is currently visible.
	Layers []Layer
	// CellsByLayer indexes Cells by layer, so a template can emit one group per
	// layer without walking every cell once per layer. Same slices, same order.
	CellsByLayer map[int][]Cell
	// Tilesets is the palette.
	Tilesets []Tileset
	// Problems is one line per distinct reason cells could not be drawn.
	Problems []string
}

// steps are the zoom levels the control offers, and the ones FitScale chooses
// between. Ascending, which FitScale relies on.
//
// Discrete and integral: a fractional scale puts a tile boundary between device
// pixels, and pixel art either blurs or gains a seam depending on which way the
// browser rounds. Powers-of-two plus 3 covers the useful range — 1 for 32px art,
// 8 for 8px art — without a slider nobody can hit an exact value on.
var steps = [6]int{1, 2, 3, 4, 6, 8}

// Steps is the zoom levels the control offers, as a copy: a caller that
// appended to or reordered a shared slice would change both what the control
// offers and what ParseZoom will accept.
//
// A real copy, not steps[:] — slicing an array gives a window onto it, so the
// first caller to write through that window changes the array for everyone.
func Steps() []int {
	out := make([]int, len(steps))
	copy(out, steps[:])
	return out
}

// fitBudget is the canvas area a map is scaled to fit by default, in pixels.
//
// Not the viewport, which the server cannot know. A number that is comfortably
// inside a laptop window beside Forge's two side panels, chosen so the common
// case — open a map, see the map — needs no interaction.
// Exported so a test can assert the property rather than a copy of the numbers:
// moving the budget should change what the test means, not silently pass.
const FitBudgetW, FitBudgetH = 900, 640

// fitCap is the most FitScale will magnify by. A tiny map filling the whole
// panel is its own kind of wrong, and every step at or below it divides both
// budget dimensions exactly — see the comparison in FitScale.
const fitCap = 4

// paletteScale is how much the swatches are magnified, and it is deliberately
// not the canvas's zoom.
//
// Zoom is a canvas concern. The palette lives in a rail 212px wide with about
// 188px of room, and following the canvas to 8x made a 32px tile a 256px
// swatch: flex shrank each one to fit the width while its height and its
// background-size did not, so every tile in the palette became a distorted crop
// of itself. Even at the fitted default it turned 216 swatches into seven
// thousand pixels of scrolling.
//
// So the swatches are sized for the rail: about 32px each, which is legible for
// 8px art and does not magnify art that is already big enough.
func paletteScale(tileW int) int {
	const target = 32
	if tileW <= 0 || tileW >= target {
		return 1
	}
	s := target / tileW
	if s > fitCap {
		s = fitCap
	}
	return s
}

// FitScale is the zoom a map gets when nobody has asked for one: the largest
// step that keeps the whole map inside the budget, and never more than 4.
//
// Both ends matter. A 16px tileset at 1:1 draws a picture nobody can see, so
// small art is magnified; a 20x15 map of 32px tiles at 3x is 1920x1440, which
// is what shipping a fixed scale chosen for the other case did. Capped at
// fitCap because a tiny map filling the whole panel is its own kind of wrong.
func FitScale(cols, rows, tileW, tileH int) int {
	if cols <= 0 || rows <= 0 || tileW <= 0 || tileH <= 0 {
		return 1
	}
	best := 1
	for _, s := range steps {
		if s > fitCap {
			break
		}
		// Divided rather than multiplied. checkSize only refuses a *negative*
		// map size, so a hand-written .tmx can declare a width of 4e18 — and
		// cols*tileW*s then overflows to a negative number, passes the budget
		// test, and picks a scale for a map that fits nothing. Every step at or
		// below the cap divides 900 and 640 exactly, so this is the same
		// comparison with no product to overflow.
		if cols*tileW <= FitBudgetW/s && rows*tileH <= FitBudgetH/s {
			best = s
		}
	}
	return best
}

// Cell is one placed tile, or one that could not be placed.
type Cell struct {
	X, Y  int
	Layer string
	// LayerIndex is which layer this cell belongs to, which is what the
	// template groups by so a layer can be hidden client-side. The name is not
	// enough: Tiled permits two layers to share one.
	LayerIndex int
	// Image is the URL to draw from, empty for an unresolved cell.
	Image string
	// SX, SY is the source rectangle's origin within that image; SW, SH its
	// size, which is also the element's size before the transform.
	SX, SY, SW, SH int
	// A, B, C, D, TX, TY are the transform that places it, flips and all —
	// tiled.Draw.Transform's matrix, which already includes the translation.
	A, B, C, D, TX, TY float64
	Alpha              float64
	// Problem is why this cell could not be drawn, and empty when it could. A
	// cell with one is drawn as a marker: a tile you cannot see is one you
	// cannot fix, and the engine will refuse to load the map over it.
	Problem string
}

// Unresolved reports whether this cell is a marker rather than a tile.
func (c Cell) Unresolved() bool { return c.Problem != "" }

// Layer is one row of the layer panel.
type Layer struct {
	Index int
	ID    int // stable positive Tiled ID, or zero when missing/ambiguous
	Name  string
	// HiddenInFile is what the map says, which is where Hidden starts. The two
	// are shown separately because they mean different things: one is a view
	// and the other is a fact about the file — and neither changes what the
	// engine imports.
	HiddenInFile bool
	// Empty marks a layer with no tiles at all, so "nothing is drawn" can be
	// told from "everything is hidden".
	Empty bool
	// Transparent marks a layer at opacity 0. Tiled's other way of making a
	// layer invisible, and one the visible attribute says nothing about — so a
	// panel that only knew about the checkbox showed an open eye over a layer
	// that draws nothing, with no explanation anywhere.
	Transparent bool
}

// UniqueLayerID returns a layer's Tiled ID only when it identifies exactly one
// layer. Idless or malformed maps remain editable using index view signals, but
// must not let two rows share an eye or route a stroke to a guessed first match.
func UniqueLayerID(layers []tiled.Layer, index int) int {
	if index < 0 || index >= len(layers) || layers[index].ID <= 0 {
		return 0
	}
	id := layers[index].ID
	for i, layer := range layers {
		if i != index && layer.ID == id {
			return 0
		}
	}
	return id
}

// Tileset is one palette section.
type Tileset struct {
	Name     string
	FirstGID uint32
	Tiles    []PaletteTile
	// Problem is why this tileset contributes no tiles, and empty when it does.
	Problem string
}

// PaletteTile is one tile to choose from.
type PaletteTile struct {
	GID   uint32
	Image string
	// SX, SY, SW, SH are the window onto the sheet, already scaled; SheetW and
	// SheetH are the whole sheet at the same scale, which is what
	// background-size needs.
	SX, SY, SW, SH int
	SheetW, SheetH int
	Selected       bool
}

// Build lays out a map for rendering.
//
// The map must have had its tilesets resolved. It is not mutated: the layers
// are copied before the view state is applied to them, so a caller may hold on
// to what it passed and pass it again.
func Build(m *tiled.Map, opts Options) Canvas {
	if m == nil || opts.AssetURL == nil {
		return Canvas{}
	}
	c := Canvas{
		Cols: m.Width, Rows: m.Height,
		TileW: m.TileWidth, TileH: m.TileHeight,
		W: m.Width * m.TileWidth, H: m.Height * m.TileHeight,
		Cells:    make([]Cell, 0),
		Layers:   make([]Layer, 0, len(m.Layers)),
		Tilesets: make([]Tileset, 0, len(m.Tilesets)),
		Problems: make([]string, 0),
	}

	for i, layer := range m.Layers {
		c.Layers = append(c.Layers, Layer{
			Index:        i,
			ID:           UniqueLayerID(m.Layers, i),
			Name:         layer.Name,
			HiddenInFile: !layer.Visible,
			Empty:        allEmpty(layer.Data),
			Transparent:  layer.Opacity == 0,
		})
	}
	// Every layer is drawn, including the ones the file hides.
	//
	// Which layers are *shown* is the browser's now: the eye toggles a signal
	// and CSS hides a group, with no round trip and nothing for a later
	// re-render to undo. So the server's job is to emit them all and say which
	// ones the file starts hidden — a layer omitted here would have an eye that
	// could not turn it back on.
	//
	// Replaced rather than edited in place, because Build would
	// otherwise leave the caller's map permanently changed: a second Build
	// would then report the file hides nothing, a false statement about the
	// .tmx on the one row whose job is telling view state and file state apart.
	// It is safe today only because Resolved re-parses, and the moment anybody
	// memoises that it stops being.
	layers := make([]tiled.Layer, len(m.Layers))
	copy(layers, m.Layers)
	for i := range layers {
		layers[i].Visible = true
	}
	m = &tiled.Map{
		Name: m.Name, Width: m.Width, Height: m.Height,
		TileWidth: m.TileWidth, TileHeight: m.TileHeight,
		Orientation: m.Orientation, RenderOrder: m.RenderOrder,
		Tilesets: m.Tilesets, Layers: layers, Properties: m.Properties,
	}

	seen := map[string]bool{}
	for _, p := range m.Placements() {
		cell := Cell{X: p.X, Y: p.Y, Layer: p.Layer, LayerIndex: p.LayerIndex, Problem: p.Problem}
		if p.Problem != "" {
			// A marker occupies its cell exactly: there is no tile to take a
			// size from, and the thing worth showing is which cell is wrong.
			cell.SW, cell.SH = m.TileWidth, m.TileHeight
			cell.A, cell.D, cell.Alpha = 1, 1, 1
			cell.TX = float64(p.X * m.TileWidth)
			cell.TY = float64(p.Y * m.TileHeight)
			if !seen[p.Problem] {
				seen[p.Problem] = true
				c.Problems = append(c.Problems, p.Problem)
			}
			c.Cells = append(c.Cells, cell)
			continue
		}
		// Unscaled. Zoom is a CSS transform on the whole canvas now, so a cell
		// carries only the flips and the translation the map itself gives it —
		// and changing zoom moves no bytes rather than re-rendering every cell.
		mx := p.Transform()
		cell.Image = opts.AssetURL(p.Image)
		cell.SX, cell.SY, cell.SW, cell.SH = p.SX, p.SY, p.SW, p.SH
		cell.A, cell.B, cell.C, cell.D = mx.A, mx.B, mx.C, mx.D
		cell.TX, cell.TY = mx.TX, mx.TY
		cell.Alpha = p.Alpha
		c.Cells = append(c.Cells, cell)
	}

	// Grouped once, here, rather than by the template filtering the flat list
	// per layer: that was O(layers x cells) on every canvas render, which for a
	// 100x100 map with eight layers is ~640k comparisons for each push.
	c.CellsByLayer = make(map[int][]Cell, len(c.Layers))
	for _, cell := range c.Cells {
		c.CellsByLayer[cell.LayerIndex] = append(c.CellsByLayer[cell.LayerIndex], cell)
	}

	c.Tilesets = palette(m, opts, paletteScale(m.TileWidth))
	return c
}

// palette is every tile a map could paint with, by tileset in first-gid order.
//
// Built from the tileset's own exported shape rather than through the drawing
// path: a palette shows what a tileset *holds*, and a tile whose sheet
// rectangle runs off the end of its image is still a tile you can select — it
// is the map that is then wrong about it, which is what the canvas says.
//
// First-gid order rather than file order, so two renders of one map agree: the
// strip is on a stream whose identical-patch suppression depends on it, and a
// palette that reshuffled would both flicker and defeat the suppression.
//
// Its own scale, not the canvas's — see paletteScale.
func palette(m *tiled.Map, opts Options, scale int) []Tileset {
	refs := append([]tiled.TilesetRef(nil), m.Tilesets...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].FirstGID < refs[j].FirstGID })

	out := make([]Tileset, 0, len(refs))
	for _, ref := range refs {
		set := Tileset{FirstGID: ref.FirstGID}
		if ref.Tileset == nil {
			set.Name = ref.Source
			if set.Name == "" {
				set.Name = "embedded"
			}
			set.Problem = "this tileset was never read, so it offers no tiles"
			out = append(out, set)
			continue
		}
		ts := ref.Tileset
		set.Name = ts.Name
		set.Tiles = make([]PaletteTile, 0, len(ts.Tiles))

		if ts.Collection() {
			// A collection has no grid: each tile is a whole file, and its ids
			// are sparse — deleting one in the editor leaves a gap — so the
			// only list of what it holds is the map it keeps.
			for _, local := range sortedIDs(ts.Tiles) {
				tile := ts.Tiles[local]
				if tile.Image.Path == "" {
					continue
				}
				set.Tiles = append(set.Tiles, PaletteTile{
					GID:   ref.FirstGID + local,
					Image: opts.AssetURL(tile.Image.Path),
					SW:    tile.Image.Width * scale, SH: tile.Image.Height * scale,
					// A collection tile is a whole file, so the picture to
					// scale is the tile itself.
					SheetW: tile.Image.Width * scale, SheetH: tile.Image.Height * scale,
				})
			}
		} else {
			for local := 0; local < ts.TileCount; local++ {
				sx, sy, sw, sh, ok := ts.SourceRect(uint32(local))
				if !ok {
					continue
				}
				set.Tiles = append(set.Tiles, PaletteTile{
					GID:   ref.FirstGID + uint32(local),
					Image: opts.AssetURL(ts.Image.Path),
					SX:    sx * scale, SY: sy * scale, SW: sw * scale, SH: sh * scale,
					// The whole sheet, scaled: a palette tile shows a window
					// onto it, so background-size is what makes the window's
					// contents the right size rather than a transform, which
					// would scale the element's own box as well.
					SheetW: ts.Image.Width * scale, SheetH: ts.Image.Height * scale,
				})
			}
		}
		if len(set.Tiles) == 0 {
			set.Problem = "this tileset holds no tiles with pictures"
		}
		out = append(out, set)
	}
	return out
}

func sortedIDs(tiles map[uint32]tiled.TilesetTile) []uint32 {
	out := make([]uint32, 0, len(tiles))
	for id := range tiles {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func allEmpty(data []uint32) bool {
	for _, gid := range data {
		if gid != 0 {
			return false
		}
	}
	return true
}

// InitialScale is the zoom a map opens at: the largest step that fits it in the
// budget, and 1 for a canvas that could not be built at all.
//
// The zoom itself is the browser's from here on — see modes.MapSignals — so
// this is the one moment the server has an opinion about it. A map that will
// not resolve gets 1 rather than 0: nothing is drawn either way, and a zoom of
// zero would make every derived size on the page read as a map of no size.
func InitialScale(c Canvas) int {
	// FitScale already answers 1 for a canvas with no size, so there is no
	// separate fallback here — an earlier one looked like a guard and was
	// unreachable, and its test passed by exercising FitScale's guard instead.
	return FitScale(c.Cols, c.Rows, c.TileW, c.TileH)
}

// Drawn reports whether this canvas has a map on it.
//
// Not "has cells": an empty map is a map, and it still has a size, a grid and a
// zoom worth offering. What this rules out is a canvas that could not be built
// at all — no tilesets resolved, the file would not parse — where every derived
// number is zero and a control marking none of its steps is worse than no
// control.
func (c Canvas) Drawn() bool { return c.Cols > 0 && c.Rows > 0 && c.TileW > 0 && c.TileH > 0 }
