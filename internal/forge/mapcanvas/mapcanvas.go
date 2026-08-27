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

// Options is what the view state contributes: which layers Forge is hiding and
// which tile the palette has selected.
type Options struct {
	// Hidden layers, by index into Map.Layers.
	//
	// **Authoritative, and the caller seeds it** — from the file's own visible
	// attribute when the view state says nothing. Not OR'd with the file, which
	// is what this did first: a layer hidden in Tiled then had an eye you could
	// click that changed the URL, flipped nothing on screen and never changed
	// its own state, so the one place an author would go to look at a hidden
	// layer could not show it.
	//
	// Forge's view, never the file's contents. Toggling it changes what is drawn
	// and never what is written, and the engine imports a hidden layer's tiles
	// either way.
	Hidden map[int]bool
	// Active is the layer a stroke would land on, by index. Story 4 paints into
	// it; this story marks it, because the most common way to lose an hour in a
	// tile editor is painting into the layer you were not looking at.
	Active int
	// SelectedGID is the palette's selection, or 0 for none. A global id, so
	// one number identifies a tile across every tileset the map declares.
	SelectedGID uint32
	// AssetURL turns a resolved image path into something the browser can
	// fetch. Required; a nil one would render every tile pointing at nothing.
	AssetURL func(path string) string
	// Scale is how many screen pixels one map pixel gets. Zero means 1.
	//
	// A display concern and nothing else: the map is still measured in cells and
	// the file is still measured in pixels, and this multiplies neither. It
	// exists because a 16px tileset at 1:1 is a picture nobody can see — the
	// fixture map comes out 96x64 — and an editor whose canvas is unreadable is
	// not an editor.
	//
	// An integer, so a tile boundary always lands on a device pixel and
	// pixelated art stays crisp. Story 5's pointer arithmetic divides by
	// TileW*Scale, which is one more factor and not a second coordinate system.
	Scale int
}

// Canvas is a map, ready to draw.
type Canvas struct {
	// Cols and Rows are cells; W and H are the pixels they occupy on screen,
	// scaled. Both, because the grid is laid out in one and the tiles are
	// placed in the other.
	Cols, Rows   int
	TileW, TileH int
	W, H         int
	// Scale is how many screen pixels one map pixel got. Every cell's matrix
	// already has it applied; this is here so the grid and Story 5's pointer
	// maths can use the same number.
	Scale int

	// Cells are every non-empty cell of every drawn layer, in draw order — so
	// a template that emits them in order gets the stacking right without
	// knowing what stacking is.
	Cells []Cell
	// Layers is every tile layer the file has, in file order, whether drawn or
	// not: the panel lists what the map holds, not what is currently visible.
	Layers []Layer
	// Tilesets is the palette.
	Tilesets []Tileset
	// Problems is one line per distinct reason cells could not be drawn.
	Problems []string
}

// Cell is one placed tile, or one that could not be placed.
type Cell struct {
	X, Y  int
	Layer string
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
	Name  string
	// Hidden is what Forge is drawing right now.
	Hidden bool
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
	// Active is the layer a stroke would land on.
	Active bool
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
	scale := opts.Scale
	if scale < 1 {
		scale = 1
	}
	c := Canvas{
		Cols: m.Width, Rows: m.Height,
		TileW: m.TileWidth, TileH: m.TileHeight,
		Scale: scale,
		W:     m.Width * m.TileWidth * scale, H: m.Height * m.TileHeight * scale,
		Cells:    make([]Cell, 0),
		Layers:   make([]Layer, 0, len(m.Layers)),
		Tilesets: make([]Tileset, 0, len(m.Tilesets)),
		Problems: make([]string, 0),
	}

	for i, layer := range m.Layers {
		c.Layers = append(c.Layers, Layer{
			Index:        i,
			Name:         layer.Name,
			Hidden:       opts.Hidden[i],
			HiddenInFile: !layer.Visible,
			Empty:        allEmpty(layer.Data),
			Transparent:  layer.Opacity == 0,
			Active:       i == opts.Active,
		})
	}
	// The layers are replaced rather than edited in place, and the view state is
	// written over them wholesale rather than only clearing.
	//
	// Replaced, because Build would otherwise leave the caller's map permanently
	// changed: a second Build with nothing hidden would then report the file
	// hides a layer that it does not — a false statement about the .tmx on the
	// one row whose job is telling view state and file state apart. It is safe
	// today only because Resolved re-parses, and the moment anybody memoises
	// that it stops being.
	//
	// Wholesale, because Hidden is the whole answer: a layer the file hides can
	// be shown, which is the point of an eye you can click.
	layers := make([]tiled.Layer, len(m.Layers))
	copy(layers, m.Layers)
	for i := range layers {
		layers[i].Visible = !opts.Hidden[i]
	}
	m = &tiled.Map{
		Name: m.Name, Width: m.Width, Height: m.Height,
		TileWidth: m.TileWidth, TileHeight: m.TileHeight,
		Orientation: m.Orientation, RenderOrder: m.RenderOrder,
		Tilesets: m.Tilesets, Layers: layers, Properties: m.Properties,
	}

	seen := map[string]bool{}
	for _, p := range m.Placements() {
		cell := Cell{X: p.X, Y: p.Y, Layer: p.Layer, Problem: p.Problem}
		if p.Problem != "" {
			// A marker occupies its cell exactly: there is no tile to take a
			// size from, and the thing worth showing is which cell is wrong.
			cell.SW, cell.SH = m.TileWidth, m.TileHeight
			cell.A, cell.D, cell.Alpha = float64(scale), float64(scale), 1
			cell.TX = float64(p.X * m.TileWidth * scale)
			cell.TY = float64(p.Y * m.TileHeight * scale)
			if !seen[p.Problem] {
				seen[p.Problem] = true
				c.Problems = append(c.Problems, p.Problem)
			}
			c.Cells = append(c.Cells, cell)
			continue
		}
		// Scaled by post-multiplication, so the flips still happen in the
		// engine's order and only the result is made bigger. Scaling the tile
		// before its transform would flip it about the wrong point.
		mx := p.Transform()
		f := float64(scale)
		cell.Image = opts.AssetURL(p.Image)
		cell.SX, cell.SY, cell.SW, cell.SH = p.SX, p.SY, p.SW, p.SH
		cell.A, cell.B, cell.C, cell.D = mx.A*f, mx.B*f, mx.C*f, mx.D*f
		cell.TX, cell.TY = mx.TX*f, mx.TY*f
		cell.Alpha = p.Alpha
		c.Cells = append(c.Cells, cell)
	}

	c.Tilesets = palette(m, opts, scale)
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
					Selected: ref.FirstGID+local == opts.SelectedGID,
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
					Selected: ref.FirstGID+uint32(local) == opts.SelectedGID,
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
