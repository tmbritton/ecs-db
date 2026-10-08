package tiled

import "fmt"

// Draw is one tile, placed: which image, which rectangle of it, and where it
// goes. Pixels, not cells.
type Draw struct {
	// Image is the file to draw from — a tileset's sheet, or a collection
	// tile's own picture. Resolved, so it is a path that opens.
	Image string
	// SX, SY, SW, SH are the source rectangle within that image.
	SX, SY, SW, SH int
	// DX, DY are where the tile's top-left goes on the map, in pixels.
	DX, DY int
	// The transform flags Tiled packs into a global id. Carried whether or not
	// the thing drawing them acts on them: dropping them here would make a
	// flipped tile draw wrong with nothing left to say so.
	FlipH, FlipV, FlipD bool
	// Alpha is the opacity of the layer this tile came from, 0 to 1. The other
	// way Tiled makes a layer invisible, and one the visible flag says nothing
	// about — a layer at opacity 0 is hidden as surely as an unchecked one.
	Alpha float64
}

// Matrix places a tile: the flips, then the move to where it goes.
//
//	x' = A*x + B*y + TX
//	y' = C*x + D*y + TY
//
// Six numbers rather than calls on a graphics library's matrix type, because
// the composition is the part worth getting right and the renderer that would
// otherwise own it is behind a build tag and needs a display. This is the same
// arithmetic, in a function a test can call.
type Matrix struct {
	A, B, C, D, TX, TY float64
}

// Apply is where a point of the source image lands.
//
// Exported because the arithmetic is otherwise written out at every call site
// and in every test, and a test that carries its own copy of a formula is a
// test that agrees with it rather than checking it.
func (m Matrix) Apply(x, y float64) (float64, float64) {
	return m.A*x + m.B*y + m.TX, m.C*x + m.D*y + m.TY
}

// Transform is the matrix that draws this tile where it goes, flips and all.
//
// Tiled's order is fixed and is not the order the flags are written in: the
// diagonal flip happens first, and the horizontal and vertical ones are about
// the result. Applying them in the order they appear in the id gives a
// different tile for three of the eight combinations, and looks like art that
// was authored wrong.
//
// The diagonal flip is a transpose — (x,y) → (y,x) — so it swaps the extent of
// a non-square tile. DY was computed from the unflipped height, so a
// diagonally flipped tile that is not square lands in the wrong row; Tiled
// only rotates square tiles into anything sensible, and this is recorded
// rather than solved.
func (d Draw) Transform() Matrix {
	a, b, c, dd := 1.0, 0.0, 0.0, 1.0
	tx, ty := 0.0, 0.0
	w, h := float64(d.SW), float64(d.SH)

	if d.FlipD {
		a, b, c, dd = 0, 1, 1, 0
		w, h = h, w
	}
	if d.FlipH {
		a, b, tx = -a, -b, w-tx
	}
	if d.FlipV {
		c, dd, ty = -c, -dd, h-ty
	}
	return Matrix{A: a, B: b, C: c, D: dd, TX: tx + float64(d.DX), TY: ty + float64(d.DY)}
}

// DrawProblem is one reason cells could not be drawn.
//
// Counted rather than repeated. A tileset whose image is missing spoils every
// cell that came from it, and a renderer that said so per cell per frame would
// bury the one line that matters under three hundred copies of it — so each
// distinct reason is reported once, with the first cell that hit it to look at
// and how many followed.
type DrawProblem struct {
	Reason string
	Layer  string
	X, Y   int
	Count  int
}

func (p DrawProblem) String() string {
	return fmt.Sprintf("%s (first at layer %q cell %d,%d; %d cell(s))",
		p.Reason, p.Layer, p.X, p.Y, p.Count)
}

// Drawable reports whether this map has any tileset that could be drawn from.
//
// It used to be the question the renderer asked to decide between drawing tiles
// and colouring rectangles by tile_type; Story 7 deleted the colours with the
// character format that was the only thing taking them, so nothing in the engine
// asks it now. Kept because it is the honest answer to "will anything appear",
// which is what a map fixture and Forge's MAP mode both want to know, and
// because a map with no tilesets is not a fault — a caller has to be able to
// tell that from a failure.
func (m *Map) Drawable() bool {
	for _, ref := range m.Tilesets {
		if ref.Tileset != nil {
			return true
		}
	}
	return false
}

// Placement is one non-empty cell, either placed or refused.
//
// The full answer, of which DrawList is the half a renderer can use. A cell
// whose tile cannot be drawn is nothing to the engine — there is no picture for
// it — and is the whole point to an editor, where a tile you cannot see is one
// you cannot fix. Both come from one walk so the two cannot disagree about
// layer order, render order, tile offsets or which tileset owns an id.
type Placement struct {
	// Draw is meaningful only when Problem is empty.
	Draw
	// X and Y are the cell, and Layer is the layer it came from — neither of
	// which a Draw carries, because a renderer works in pixels.
	X, Y  int
	Layer string
	// LayerIndex is where that layer sits in Map.Layers.
	//
	// Alongside the name and not instead of it, because Tiled permits two
	// layers to share a name and an editor that keyed anything on the name
	// would then treat them as one. Forge groups the cells it draws by this so
	// a layer can be hidden without re-asking the server for the map.
	LayerIndex int
	// Problem is why this cell could not be drawn, or empty.
	Problem string
}

// Placements is every non-empty cell of every visible layer, in draw order.
//
// All the arithmetic and none of the drawing: no image is opened, nothing is
// decoded, and nothing here knows what a renderer is. That is deliberate. The
// renderer is behind a build tag and needs a display, so everything that can be
// got wrong — layer order, which tileset owns an id, where a tile taller than
// its cell goes — is decided in a function a test can call.
//
// Call after ResolveTilesets. A tileset that was never resolved is a problem
// rather than an empty one.
func (m *Map) Placements() []Placement {
	return m.placements(false)
}

// AllPlacements also projects hidden and fully transparent layer tiles. Import
// needs their art even when the current layer settings do not draw them.
func (m *Map) AllPlacements() []Placement {
	return m.placements(true)
}

func (m *Map) placements(includeHidden bool) []Placement {
	out := make([]Placement, 0)

	// Which corner a layer is drawn from. Invisible for square tiles and the
	// whole point for the tall ones this exists to draw: render order is what
	// decides which of two overlapping tiles ends up on top.
	rightward, downward := renderOrder(m.RenderOrder)

	// File order, first layer first: a later layer covers an earlier one. The
	// reverse of the loader's walk, which wants the topmost tile in a cell and
	// starts from the end — the same list read for two different questions.
	for i, layer := range m.Layers {
		// Visibility is honoured here and nowhere else. The loader ignores it
		// on purpose: hiding a layer is what the editor shows you, not what the
		// map holds. Drawing is the thing the checkbox is actually about.
		if !includeHidden && !layer.Visible {
			continue
		}
		// Fully transparent draws nothing, and there is no point placing it.
		if !includeHidden && layer.Opacity == 0 {
			continue
		}
		for _, y := range order(layer.Height, downward) {
			for _, x := range order(layer.Width, rightward) {
				tile := layer.TileAt(x, y)
				if tile.GID == 0 {
					continue // an empty cell, not a blank tile
				}
				place := Placement{X: x, Y: y, Layer: layer.Name, LayerIndex: i}
				d, why := m.drawOf(tile, x, y)
				if why != "" {
					place.Problem = why
				} else {
					d.Alpha = layer.Opacity
					place.Draw = d
				}
				out = append(out, place)
			}
		}
	}
	return out
}

// DrawList is every tile that can be drawn, and one problem per distinct reason
// the rest could not be.
//
// A projection of Placements: the placed ones in order, and the refusals
// aggregated. Counted rather than repeated, because a tileset whose image is
// missing spoils every cell that came from it and a renderer saying so per cell
// per frame would bury the one line that matters under three hundred copies.
func (m *Map) DrawList() ([]Draw, []DrawProblem) {
	places := m.Placements()
	draws := make([]Draw, 0, len(places))
	p := &problems{seen: map[string]int{}}
	for _, place := range places {
		if place.Problem == "" {
			draws = append(draws, place.Draw)
			continue
		}
		p.add(place.Problem, place.Layer, place.X, place.Y)
	}
	return draws, p.list
}

// drawOf places one tile, or says why it could not be placed.
//
// It returns the reason rather than filing it, so the caller can attach it to
// the cell that caused it. Filing it here meant reading it back off a list that
// keeps one entry per *distinct* reason — so a cell repeating an earlier fault
// added no entry and was handed whichever reason happened to be last.
func (m *Map) drawOf(tile Tile, x, y int) (Draw, string) {
	ref, local, ok := m.TilesetFor(tile.GID)
	if !ok {
		return Draw{}, fmt.Sprintf("no tileset holds global id %d", tile.GID)
	}
	if ref.Tileset == nil {
		return Draw{}, fmt.Sprintf("tileset %q was never resolved", refName(ref))
	}
	ts := ref.Tileset

	image, sx, sy, sw, sh, why := source(ts, local, tile.GID)
	if why != "" {
		return Draw{}, why
	}

	return Draw{
		Image: image,
		SX:    sx, SY: sy, SW: sw, SH: sh,
		// Bottom-left aligned, which is Tiled's rule for an orthogonal map: a
		// tile taller than its cell rises out of the top of it, which is how a
		// wall tileset overlaps the row behind. Aligning top-left instead draws
		// every such map one row low, and looks like a layout fault rather than
		// a missing rule.
		DX:    x*m.TileWidth + ts.TileOffsetX,
		DY:    (y+1)*m.TileHeight - drawnHeight(sw, sh, tile.FlipD) + ts.TileOffsetY,
		FlipH: tile.FlipH, FlipV: tile.FlipV, FlipD: tile.FlipD,
	}, ""
}

// drawnHeight is how tall the tile ends up, which is not how tall its source is
// when it has been transposed.
//
// A diagonal flip swaps the tile's extent, so a 16×32 tile covers 32×16 once it
// is drawn — and bottom-left alignment has to use the height it covers or the
// tile lands a row out. Square tiles, which is nearly all of them, are the same
// number either way, which is why this was written as `sh` first and nothing
// noticed.
func drawnHeight(sw, sh int, flipD bool) int {
	if flipD {
		return sw
	}
	return sh
}

// renderOrder is the direction a layer's cells are walked, from Tiled's
// renderorder attribute. Right-down is the default and what Tiled writes unless
// told otherwise.
func renderOrder(s string) (rightward, downward bool) {
	switch s {
	case "right-up":
		return true, false
	case "left-down":
		return false, true
	case "left-up":
		return false, false
	default: // "right-down", and anything unrecognised
		return true, true
	}
}

// order is the indices 0..n-1, forwards or backwards.
func order(n int, forward bool) []int {
	out := make([]int, n)
	for i := range out {
		if forward {
			out[i] = i
		} else {
			out[i] = n - 1 - i
		}
	}
	return out
}

// source is the picture a local id draws from and the rectangle of it, or the
// reason there is none.
//
// Two shapes, because there are two kinds of tileset. A sheet is cut into a
// grid and SourceRect does the arithmetic; a collection has no grid at all, and
// a tile is a whole file of its own size — which is also why a collection's
// tiles need not be the tileset's nominal tile size.
//
// The reasons are worded so that they group the way the fault does. A sheet
// with no image spoils every tile that came from it and says so once, without
// an id in it; an id the sheet does not hold is about that id and names it. The
// first draft folded both into one sentence carrying the local id, which turned
// a single missing PNG into one report per distinct tile.
//
// The *global* id is what a reason names, because that is the number Tiled
// shows and the file holds. The local id is this package's arithmetic.
func source(ts *Tileset, local uint32, gid uint32) (image string, sx, sy, sw, sh int, why string) {
	if ts.Collection() {
		tile, held := ts.Tiles[local]
		if !held || tile.Image.Path == "" {
			return "", 0, 0, 0, 0, fmt.Sprintf(
				"tileset %q has no image for global id %d", ts.Name, gid)
		}
		return tile.Image.Path, 0, 0, tile.Image.Width, tile.Image.Height, ""
	}
	if ts.Image.Path == "" {
		return "", 0, 0, 0, 0, fmt.Sprintf("tileset %q has no image", ts.Name)
	}
	x, y, w, h, held := ts.SourceRect(local)
	if !held {
		return "", 0, 0, 0, 0, fmt.Sprintf(
			"tileset %q holds no tile with global id %d", ts.Name, gid)
	}
	// Against the image the tileset declares, when it declares one. A tileset
	// that promises more tiles than its sheet holds — tilecount and columns
	// agree with each other and not with the picture — produces a rectangle
	// off the end of it, which a graphics library intersects with the image
	// and hands back empty: no pixels, no error, and a hole nobody can account
	// for. A tileset that declares no size is not checked, because there is
	// nothing to check against.
	if ts.Image.Width > 0 && ts.Image.Height > 0 && (x+w > ts.Image.Width || y+h > ts.Image.Height) {
		return "", 0, 0, 0, 0, fmt.Sprintf(
			"tileset %q wants (%d,%d %dx%d) of %s, which is %dx%d",
			ts.Name, x, y, w, h, ts.Image.Source, ts.Image.Width, ts.Image.Height)
	}
	return ts.Image.Path, x, y, w, h, ""
}

// refName is something to call a tileset that has not been read yet, which has
// no name of its own to give.
func refName(ref TilesetRef) string {
	if ref.Source != "" {
		return ref.Source
	}
	return fmt.Sprintf("embedded at first gid %d", ref.FirstGID)
}

// problems collects one entry per distinct reason, in the order first seen.
type problems struct {
	list []DrawProblem
	seen map[string]int // reason → index in list
}

func (p *problems) add(reason, layer string, x, y int) {
	if at, ok := p.seen[reason]; ok {
		p.list[at].Count++
		return
	}
	p.seen[reason] = len(p.list)
	p.list = append(p.list, DrawProblem{Reason: reason, Layer: layer, X: x, Y: y, Count: 1})
}
