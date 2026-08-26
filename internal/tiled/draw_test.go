package tiled_test

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

// sheet is a tileset that cuts one image into a grid.
func sheet(name, image string, cols, count, tw, th int) *tiled.Tileset {
	return &tiled.Tileset{
		Name: name, Columns: cols, TileCount: count,
		TileWidth: tw, TileHeight: th,
		Image: tiled.Image{Source: image, Path: image, Width: cols * tw, Height: (count / cols) * th},
	}
}

// layer is a tile layer holding raw gids, row-major.
func layer(name string, w, h int, visible bool, gids ...uint32) tiled.Layer {
	return tiled.Layer{Name: name, Width: w, Height: h, Visible: visible, Opacity: 1, Data: gids}
}

// mapOf is a map whose tilesets are already resolved, which is the state
// DrawList is called in.
func mapOf(w, h, tw, th int, refs []tiled.TilesetRef, layers ...tiled.Layer) *tiled.Map {
	return &tiled.Map{
		Name: "m.tmx", Width: w, Height: h, TileWidth: tw, TileHeight: th,
		Orientation: "orthogonal", Tilesets: refs, Layers: layers,
	}
}

func oneSheet() []tiled.TilesetRef {
	return []tiled.TilesetRef{{FirstGID: 1, Tileset: sheet("floor", "floor.png", 4, 8, 16, 16)}}
}

// A tile is drawn from its tileset's image, at the rectangle Story 2 computes.
func TestDrawList_DrawsATileFromItsSheet(t *testing.T) {
	m := mapOf(2, 1, 16, 16, oneSheet(), layer("l", 2, 1, true, 1, 6))

	got, problems := m.DrawList()
	if len(problems) != 0 {
		t.Fatalf("problems: %+v", problems)
	}
	want := []tiled.Draw{
		// local 0: column 0, row 0.
		{Image: "floor.png", SX: 0, SY: 0, SW: 16, SH: 16, DX: 0, DY: 0, Alpha: 1},
		// local 5: column 1, row 1 of a 4-column sheet.
		{Image: "floor.png", SX: 16, SY: 16, SW: 16, SH: 16, DX: 16, DY: 0, Alpha: 1},
	}
	assertDraws(t, got, want)
}

// Empty cells contribute nothing, rather than a blank tile.
func TestDrawList_AnEmptyCellDrawsNothing(t *testing.T) {
	m := mapOf(3, 1, 16, 16, oneSheet(), layer("l", 3, 1, true, 1, 0, 2))

	got, _ := m.DrawList()
	if len(got) != 2 {
		t.Fatalf("got %d draws for two tiles and a hole: %+v", len(got), got)
	}
	for _, d := range got {
		if d.DX == 16 {
			t.Errorf("the empty cell was drawn: %+v", d)
		}
	}
}

// Layers draw in file order, back to front, so a later layer covers an earlier
// one. This is the reverse of the loader's rule, which wants the topmost tile
// and walks from the end.
func TestDrawList_LayersDrawInFileOrder(t *testing.T) {
	m := mapOf(1, 1, 16, 16, oneSheet(),
		layer("floor", 1, 1, true, 1),
		layer("wall", 1, 1, true, 3),
	)

	got, _ := m.DrawList()
	if len(got) != 2 {
		t.Fatalf("got %d draws: %+v", len(got), got)
	}
	if got[0].SX != 0 {
		t.Errorf("the first draw is %+v, want the floor's tile (local 0)", got[0])
	}
	if got[1].SX != 32 {
		t.Errorf("the second draw is %+v, want the wall's tile (local 2)", got[1])
	}
}

// An invisible layer does not draw. Deliberately unlike the loader, which
// ignores Visible because hiding a layer is what the editor shows you and not
// what the map is — drawing is the thing Visible is actually for.
func TestDrawList_AnInvisibleLayerDoesNotDraw(t *testing.T) {
	m := mapOf(1, 1, 16, 16, oneSheet(),
		layer("floor", 1, 1, true, 1),
		layer("hidden", 1, 1, false, 3),
	)

	got, _ := m.DrawList()
	if len(got) != 1 {
		t.Fatalf("got %d draws, want only the visible layer's: %+v", len(got), got)
	}
	if got[0].SX != 0 {
		t.Errorf("the drawn tile is %+v, want the floor's", got[0])
	}
}

// Which tileset owns a gid, when there is more than one.
func TestDrawList_ATileComesFromTheTilesetThatOwnsItsID(t *testing.T) {
	refs := []tiled.TilesetRef{
		{FirstGID: 1, Tileset: sheet("floor", "floor.png", 4, 8, 16, 16)},
		{FirstGID: 9, Tileset: sheet("props", "props.png", 2, 4, 16, 16)},
	}
	m := mapOf(2, 1, 16, 16, refs, layer("l", 2, 1, true, 8, 10))

	got, problems := m.DrawList()
	if len(problems) != 0 {
		t.Fatalf("problems: %+v", problems)
	}
	if got[0].Image != "floor.png" || got[0].SX != 48 || got[0].SY != 16 {
		t.Errorf("gid 8 drew %+v, want floor.png local 7 (column 3, row 1)", got[0])
	}
	if got[1].Image != "props.png" || got[1].SX != 16 || got[1].SY != 0 {
		t.Errorf("gid 10 drew %+v, want props.png local 1 (column 1, row 0)", got[1])
	}
}

// A tile taller than the grid rises out of its cell: Tiled aligns an orthogonal
// tile's bottom-left with the cell's, which is how a wall tileset overlaps the
// row above. Getting this wrong looks like a layout bug rather than a missing
// rule.
func TestDrawList_ATallTileIsBottomLeftAligned(t *testing.T) {
	refs := []tiled.TilesetRef{
		{FirstGID: 1, Tileset: sheet("walls", "walls.png", 2, 4, 16, 32)},
	}
	m := mapOf(1, 2, 16, 16, refs, layer("l", 1, 2, true, 0, 1))

	got, _ := m.DrawList()
	if len(got) != 1 {
		t.Fatalf("got %d draws: %+v", len(got), got)
	}
	// The cell is row 1, so its bottom is at y=32; a 32px tile starts at 0.
	if got[0].DY != 0 {
		t.Errorf("a 32px tile in row 1 of a 16px grid drew at y=%d, want 0", got[0].DY)
	}
	if got[0].SH != 32 {
		t.Errorf("the source height is %d, want the tileset's 32", got[0].SH)
	}
}

// The tileset's own offset shifts every tile it draws, and is how a tileset
// whose art does not sit flush in its cell lines up.
func TestDrawList_TheTilesetOffsetMovesEveryTile(t *testing.T) {
	ts := sheet("floor", "floor.png", 4, 8, 16, 16)
	ts.TileOffsetX, ts.TileOffsetY = 3, -5
	m := mapOf(1, 1, 16, 16, []tiled.TilesetRef{{FirstGID: 1, Tileset: ts}}, layer("l", 1, 1, true, 1))

	got, _ := m.DrawList()
	if got[0].DX != 3 || got[0].DY != -5 {
		t.Errorf("drew at (%d,%d), want the offset (3,-5)", got[0].DX, got[0].DY)
	}
}

// A collection has no sheet to cut, so a tile is its own whole file.
func TestDrawList_ACollectionDrawsTheTilesOwnFile(t *testing.T) {
	ts := &tiled.Tileset{
		Name: "props", Columns: 0, TileCount: 2, TileWidth: 16, TileHeight: 16,
		Tiles: map[uint32]tiled.TilesetTile{
			0: {ID: 0, Image: tiled.Image{Source: "barrel.png", Path: "art/barrel.png", Width: 24, Height: 40}},
		},
	}
	m := mapOf(1, 1, 16, 16, []tiled.TilesetRef{{FirstGID: 1, Tileset: ts}}, layer("l", 1, 1, true, 1))

	got, problems := m.DrawList()
	if len(problems) != 0 {
		t.Fatalf("problems: %+v", problems)
	}
	if len(got) != 1 {
		t.Fatalf("got %d draws: %+v", len(got), got)
	}
	d := got[0]
	if d.Image != "art/barrel.png" {
		t.Errorf("drew %q, want the tile's own file", d.Image)
	}
	if d.SX != 0 || d.SY != 0 || d.SW != 24 || d.SH != 40 {
		t.Errorf("source rect is (%d,%d %dx%d), want the whole file (0,0 24x40)", d.SX, d.SY, d.SW, d.SH)
	}
	// Bottom-left aligned like any other tile, so a 40px prop rises out of a
	// 16px cell.
	if d.DY != 16-40 {
		t.Errorf("drew at y=%d, want %d", d.DY, 16-40)
	}
}

// The flags Story 1 parsed reach the thing that draws them. Dropping them
// silently is what makes a rotated tile draw wrong with nothing to notice.
func TestDrawList_CarriesTheFlipFlags(t *testing.T) {
	const flipH, flipV, flipD = 0x80000000, 0x40000000, 0x20000000
	m := mapOf(4, 1, 16, 16, oneSheet(),
		layer("l", 4, 1, true, 1|flipH, 1|flipV, 1|flipD, 1|flipH|flipV|flipD))

	got, _ := m.DrawList()
	if len(got) != 4 {
		t.Fatalf("got %d draws: %+v", len(got), got)
	}
	want := []tiled.Draw{
		{FlipH: true}, {FlipV: true}, {FlipD: true}, {FlipH: true, FlipV: true, FlipD: true},
	}
	for i, w := range want {
		if got[i].FlipH != w.FlipH || got[i].FlipV != w.FlipV || got[i].FlipD != w.FlipD {
			t.Errorf("draw %d has flips h=%v v=%v d=%v, want h=%v v=%v d=%v",
				i, got[i].FlipH, got[i].FlipV, got[i].FlipD, w.FlipH, w.FlipV, w.FlipD)
		}
		// And the flags are out of the id: a flipped tile is the same tile.
		if got[i].SX != 0 || got[i].SY != 0 {
			t.Errorf("draw %d is at source (%d,%d) — the flags are still in the id",
				i, got[i].SX, got[i].SY)
		}
	}
}

// ── Problems ─────────────────────────────────────────────────────────

// A gid whose tileset does not hold it is reported once, with a cell to look
// at, however many cells hold it — the alternative is one line per tile per
// frame.
func TestDrawList_AGIDTheTilesetDoesNotHoldIsReportedOnce(t *testing.T) {
	// The bad cells are not the first ones: a problem that recorded nothing
	// would report (0,0), which is indistinguishable from the first cell when
	// the first cell is the one that failed.
	m := mapOf(3, 2, 16, 16, oneSheet(),
		layer("l", 3, 2, true, 1, 1, 1, 1, 99, 99))

	got, problems := m.DrawList()
	if len(got) != 4 {
		t.Errorf("got %d draws, want the four tiles that resolve: %+v", len(got), got)
	}
	if len(problems) != 1 {
		t.Fatalf("got %d problems for two bad cells of one kind: %+v", len(problems), problems)
	}
	p := problems[0]
	if p.Count != 2 {
		t.Errorf("the problem counts %d cells, want 2", p.Count)
	}
	if p.Layer != "l" || p.X != 1 || p.Y != 1 {
		t.Errorf("the problem points at %s (%d,%d), want the first cell that hit it (1,1)", p.Layer, p.X, p.Y)
	}
	if !strings.Contains(p.Reason, "99") {
		t.Errorf("the reason does not name the id: %q", p.Reason)
	}
}

// A gid below every first gid belongs to no tileset at all, which is a
// different fault from one a tileset owns by range and does not hold.
func TestDrawList_AGIDBelowEveryTilesetIsReported(t *testing.T) {
	refs := []tiled.TilesetRef{{FirstGID: 10, Tileset: sheet("floor", "floor.png", 4, 8, 16, 16)}}
	m := mapOf(1, 1, 16, 16, refs, layer("l", 1, 1, true, 5))

	got, problems := m.DrawList()
	if len(got) != 0 {
		t.Errorf("drew a tile no tileset owns: %+v", got)
	}
	if len(problems) != 1 {
		t.Fatalf("got %d problems: %+v", len(problems), problems)
	}
	if !strings.Contains(problems[0].Reason, "no tileset") || !strings.Contains(problems[0].Reason, "5") {
		t.Errorf("the reason is %q, want it to say no tileset holds id 5", problems[0].Reason)
	}
}

// A sheet whose image was never resolved to a path is one problem, not one per
// tile that came from it.
//
// Source set and Path empty, which is what an unresolved tileset looks like:
// clearing Source instead would make it a *collection*, since that is what
// Collection() means, and a collection's missing pictures really are one
// problem each because each tile is its own file.
func TestDrawList_ASheetWithNoResolvedImageIsReportedOnce(t *testing.T) {
	ts := sheet("floor", "floor.png", 4, 8, 16, 16)
	ts.Image.Path = ""
	m := mapOf(3, 1, 16, 16, []tiled.TilesetRef{{FirstGID: 1, Tileset: ts}},
		layer("l", 3, 1, true, 1, 2, 3))

	got, problems := m.DrawList()
	if len(got) != 0 {
		t.Errorf("drew %d tiles from a tileset with no image: %+v", len(got), got)
	}
	if len(problems) != 1 {
		t.Fatalf("got %d problems for three tiles of one tileset: %+v", len(problems), problems)
	}
	if problems[0].Count != 3 {
		t.Errorf("the problem counts %d cells, want 3", problems[0].Count)
	}
	if !strings.Contains(problems[0].Reason, "floor") {
		t.Errorf("the reason does not name the tileset: %q", problems[0].Reason)
	}
}

// A collection's missing pictures are one problem each, because each tile is
// its own file and a missing one says nothing about the others.
func TestDrawList_ACollectionsMissingTilesAreReportedSeparately(t *testing.T) {
	ts := &tiled.Tileset{
		Name: "props", Columns: 0, TileCount: 3, TileWidth: 16, TileHeight: 16,
		Tiles: map[uint32]tiled.TilesetTile{
			0: {ID: 0, Image: tiled.Image{Source: "barrel.png", Path: "art/barrel.png", Width: 16, Height: 16}},
		},
	}
	m := mapOf(3, 1, 16, 16, []tiled.TilesetRef{{FirstGID: 1, Tileset: ts}},
		layer("l", 3, 1, true, 1, 2, 3))

	got, problems := m.DrawList()
	if len(got) != 1 {
		t.Errorf("got %d draws, want the one tile that has a file: %+v", len(got), got)
	}
	if len(problems) != 2 {
		t.Fatalf("got %d problems for two tiles with no file: %+v", len(problems), problems)
	}
	for _, p := range problems {
		if p.Count != 1 {
			t.Errorf("a collection tile's missing file counts %d cells: %+v", p.Count, p)
		}
	}
}

// A tileset nobody resolved is not a tileset with no tiles in it.
func TestDrawList_AnUnresolvedTilesetIsAProblem(t *testing.T) {
	m := mapOf(1, 1, 16, 16,
		[]tiled.TilesetRef{{FirstGID: 1, Source: "floor.tsx"}},
		layer("l", 1, 1, true, 1))

	got, problems := m.DrawList()
	if len(got) != 0 {
		t.Errorf("drew from an unresolved tileset: %+v", got)
	}
	if len(problems) != 1 {
		t.Fatalf("got %d problems: %+v", len(problems), problems)
	}
	if !strings.Contains(problems[0].Reason, "floor.tsx") {
		t.Errorf("the reason does not name the tileset: %q", problems[0].Reason)
	}
}

// A map with no tilesets draws nothing and complains about nothing: it is the
// fallback path, not a fault. Every fixture in this epic is one.
func TestDrawList_AMapWithNoTilesetsIsNotAProblem(t *testing.T) {
	m := mapOf(1, 1, 16, 16, nil, layer("l", 1, 1, true, 0))

	got, problems := m.DrawList()
	if len(got) != 0 || len(problems) != 0 {
		t.Errorf("got %d draws and %d problems, want neither: %+v %+v", len(got), len(problems), got, problems)
	}
}

// Drawable says whether there is anything to draw, which is what decides
// between this and the colour fallback.
func TestMap_Drawable(t *testing.T) {
	if (&tiled.Map{}).Drawable() {
		t.Error("a map with no tilesets is drawable")
	}
	if mapOf(1, 1, 16, 16, []tiled.TilesetRef{{FirstGID: 1, Source: "x.tsx"}}).Drawable() {
		t.Error("a map whose tileset never resolved is drawable")
	}
	if !mapOf(1, 1, 16, 16, oneSheet()).Drawable() {
		t.Error("a map with a resolved sheet is not drawable")
	}
}

func assertDraws(t *testing.T, got, want []tiled.Draw) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d draws, want %d\n got: %+v\nwant: %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("draw %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// ── The transform ────────────────────────────────────────────────────

// Where the four corners of a tile land, which is the whole of what a flip
// means. Stated as corners rather than as matrix elements because a matrix
// element is not a thing anybody can check by looking at the screen.
func TestDraw_Transform(t *testing.T) {
	// A 16×16 tile at the origin, so the numbers are the tile's own.
	square := tiled.Draw{SW: 16, SH: 16}

	cases := []struct {
		name string
		draw tiled.Draw
		// where the source's top-left, top-right, bottom-left corners go
		tl, tr, bl [2]float64
	}{
		{
			name: "unflipped",
			draw: square,
			tl:   [2]float64{0, 0}, tr: [2]float64{16, 0}, bl: [2]float64{0, 16},
		},
		{
			name: "flipped horizontally, so the left edge is on the right",
			draw: withFlips(square, true, false, false),
			tl:   [2]float64{16, 0}, tr: [2]float64{0, 0}, bl: [2]float64{16, 16},
		},
		{
			name: "flipped vertically, so the top edge is at the bottom",
			draw: withFlips(square, false, true, false),
			tl:   [2]float64{0, 16}, tr: [2]float64{16, 16}, bl: [2]float64{0, 0},
		},
		{
			name: "flipped diagonally, which transposes it",
			draw: withFlips(square, false, false, true),
			tl:   [2]float64{0, 0}, tr: [2]float64{0, 16}, bl: [2]float64{16, 0},
		},
		{
			// Diagonal then horizontal is Tiled's 90° clockwise rotation.
			name: "diagonal and horizontal, which is a quarter turn",
			draw: withFlips(square, true, false, true),
			tl:   [2]float64{16, 0}, tr: [2]float64{16, 16}, bl: [2]float64{0, 0},
		},
		{
			name: "all three",
			draw: withFlips(square, true, true, true),
			tl:   [2]float64{16, 16}, tr: [2]float64{16, 0}, bl: [2]float64{0, 16},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.draw.Transform()
			assertCorner(t, "top-left", m, 0, 0, tc.tl)
			assertCorner(t, "top-right", m, 16, 0, tc.tr)
			assertCorner(t, "bottom-left", m, 0, 16, tc.bl)
		})
	}
}

// Whatever the flips, the tile covers the cell it was placed at — a flip turns
// a tile around, it does not move it somewhere else.
func TestDraw_TransformKeepsTheTileInItsCell(t *testing.T) {
	for _, flips := range [][3]bool{
		{false, false, false},
		{true, false, false},
		{false, true, false},
		{false, false, true},
		{true, true, false},
		{true, false, true},
		{false, true, true},
		{true, true, true},
	} {
		d := withFlips(tiled.Draw{SW: 16, SH: 16, DX: 32, DY: 48}, flips[0], flips[1], flips[2])
		m := d.Transform()

		minX, minY := 1e9, 1e9
		maxX, maxY := -1e9, -1e9
		for _, c := range [][2]float64{{0, 0}, {16, 0}, {0, 16}, {16, 16}} {
			x, y := m.Apply(c[0], c[1])
			minX, maxX = min(minX, x), max(maxX, x)
			minY, maxY = min(minY, y), max(maxY, y)
		}
		if minX != 32 || minY != 48 || maxX != 48 || maxY != 64 {
			t.Errorf("flips %v put the tile at (%g,%g)-(%g,%g), want (32,48)-(48,64)",
				flips, minX, minY, maxX, maxY)
		}
	}
}

// A diagonal flip transposes the tile, so a tile that is not square changes
// which way round it is — and the horizontal and vertical flips that follow are
// about the *transposed* extent. Every other transform test uses a square tile,
// where swapping the extent and not swapping it are the same thing.
func TestDraw_TransformOfANonSquareTile(t *testing.T) {
	tall := tiled.Draw{SW: 16, SH: 32}

	// Diagonal alone: the 16×32 source lands in a 32×16 box.
	m := withFlips(tall, false, false, true).Transform()
	x, y := m.Apply(16, 32)
	if x != 32 || y != 16 {
		t.Errorf("the far corner of a transposed 16x32 tile lands at (%g,%g), want (32,16)", x, y)
	}

	// Diagonal then horizontal, which flips about the transposed width of 32.
	m = withFlips(tall, true, false, true).Transform()
	if x, y := m.Apply(0, 0); x != 32 || y != 0 {
		t.Errorf("the source origin lands at (%g,%g), want (32,0) — the flip used the untransposed width", x, y)
	}
	if x, y := m.Apply(16, 32); x != 0 || y != 16 {
		t.Errorf("the far corner lands at (%g,%g), want (0,16)", x, y)
	}

	// Vertical alone, about the untransposed height of 32. This arm and the
	// next were missing, and a mutation flipping about the width instead
	// survived the whole suite: every other transform test uses a square tile,
	// where the width and the height are the same number.
	m = withFlips(tall, false, true, false).Transform()
	if x, y := m.Apply(0, 0); x != 0 || y != 32 {
		t.Errorf("a vertical flip sends the origin to (%g,%g), want (0,32)", x, y)
	}

	// Diagonal then vertical, about the transposed height of 16.
	m = withFlips(tall, false, true, true).Transform()
	if x, y := m.Apply(0, 0); x != 0 || y != 16 {
		t.Errorf("a transposed vertical flip sends the origin to (%g,%g), want (0,16)", x, y)
	}
	if x, y := m.Apply(16, 32); x != 32 || y != 0 {
		t.Errorf("the far corner lands at (%g,%g), want (32,0)", x, y)
	}
}

// The tile covers the cell it was placed at whatever the flips, for a tile that
// is not square as well as one that is — the square case cannot tell an extent
// swap from a missing one.
func TestDraw_TransformKeepsANonSquareTileInItsBox(t *testing.T) {
	for _, flips := range [][3]bool{
		{false, false, false},
		{true, false, false},
		{false, true, false},
		{false, false, true},
		{true, true, false},
		{true, false, true},
		{false, true, true},
		{true, true, true},
	} {
		d := withFlips(tiled.Draw{SW: 16, SH: 32, DX: 8, DY: 4}, flips[0], flips[1], flips[2])
		m := d.Transform()

		// Transposed, the box is 32x16; otherwise 16x32.
		wantW, wantH := 16.0, 32.0
		if flips[2] {
			wantW, wantH = 32, 16
		}
		minX, minY := 1e9, 1e9
		maxX, maxY := -1e9, -1e9
		for _, c := range [][2]float64{{0, 0}, {16, 0}, {0, 32}, {16, 32}} {
			x, y := m.Apply(c[0], c[1])
			minX, maxX = min(minX, x), max(maxX, x)
			minY, maxY = min(minY, y), max(maxY, y)
		}
		if minX != 8 || minY != 4 || maxX != 8+wantW || maxY != 4+wantH {
			t.Errorf("flips %v put the tile at (%g,%g)-(%g,%g), want (8,4)-(%g,%g)",
				flips, minX, minY, maxX, maxY, 8+wantW, 4+wantH)
		}
	}
}

func withFlips(d tiled.Draw, h, v, dg bool) tiled.Draw {
	d.FlipH, d.FlipV, d.FlipD = h, v, dg
	return d
}

func assertCorner(t *testing.T, name string, m tiled.Matrix, x, y float64, want [2]float64) {
	t.Helper()
	gx, gy := m.Apply(x, y)
	if gx != want[0] || gy != want[1] {
		t.Errorf("%s of the source lands at (%g,%g), want (%g,%g)", name, gx, gy, want[0], want[1])
	}
}

// ── The review's findings ────────────────────────────────────────────

// A map that declares no pixel tile size is refused, rather than placing every
// tile of every layer at x=0 and one tile above the top of the window — the
// whole map in a single stack, off screen, with nothing to report.
func TestParse_RefusesAMapWithNoPixelTileSize(t *testing.T) {
	cases := map[string]string{
		"neither": `<map version="1.10" orientation="orthogonal" width="2" height="1">
 <layer id="1" name="l" width="2" height="1"><data encoding="csv">1,2</data></layer>
</map>`,
		"no width": `<map version="1.10" orientation="orthogonal" width="2" height="1" tileheight="16">
 <layer id="1" name="l" width="2" height="1"><data encoding="csv">1,2</data></layer>
</map>`,
		"zero height": `<map version="1.10" orientation="orthogonal" width="2" height="1" tilewidth="16" tileheight="0">
 <layer id="1" name="l" width="2" height="1"><data encoding="csv">1,2</data></layer>
</map>`,
	}
	for name, file := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tiled.Parse([]byte(file), "probe.tmx")
			if err == nil {
				t.Fatal("a map with no pixel tile size was accepted")
			}
			if !strings.Contains(err.Error(), "pixels") {
				t.Errorf("the refusal does not say what is missing: %v", err)
			}
		})
	}
}

// A tile whose rectangle falls outside the tileset's own image draws nothing —
// the graphics library intersects the rectangle with the image and returns an
// empty one — so it has to be reported rather than left as a hole nobody can
// account for.
func TestDrawList_ASourceRectPastTheImageIsAProblem(t *testing.T) {
	// Four tiles' worth of image, eight tiles declared.
	ts := sheet("floor", "floor.png", 4, 8, 16, 16)
	ts.Image.Width, ts.Image.Height = 64, 16
	m := mapOf(2, 1, 16, 16, []tiled.TilesetRef{{FirstGID: 1, Tileset: ts}},
		layer("l", 2, 1, true, 1, 8))

	got, problems := m.DrawList()
	if len(got) != 1 {
		t.Errorf("got %d draws, want only the tile that is inside the image: %+v", len(got), got)
	}
	if len(problems) != 1 {
		t.Fatalf("got %d problems: %+v", len(problems), problems)
	}
	if !strings.Contains(problems[0].Reason, "floor.png") {
		t.Errorf("the reason does not name the image: %q", problems[0].Reason)
	}
}

// An image whose size the tileset does not declare is not checked against,
// because there is nothing to check against — and refusing every such tileset
// would refuse a legitimate hand-written one.
func TestDrawList_AnImageWithNoDeclaredSizeIsNotChecked(t *testing.T) {
	ts := sheet("floor", "floor.png", 4, 8, 16, 16)
	ts.Image.Width, ts.Image.Height = 0, 0
	m := mapOf(1, 1, 16, 16, []tiled.TilesetRef{{FirstGID: 1, Tileset: ts}}, layer("l", 1, 1, true, 8))

	got, problems := m.DrawList()
	if len(got) != 1 || len(problems) != 0 {
		t.Errorf("got %d draws and %d problems, want one draw and no problem: %+v %+v",
			len(got), len(problems), got, problems)
	}
}

// Render order decides which tile overlaps which, which is invisible for square
// tiles and the whole point for the tall ones this story exists to draw.
func TestDrawList_HonoursRenderOrder(t *testing.T) {
	// Two cells side by side on one row, and two rows.
	cells := func(m *tiled.Map) [][2]int {
		draws, _ := m.DrawList()
		out := make([][2]int, 0, len(draws))
		for _, d := range draws {
			out = append(out, [2]int{d.DX / 16, d.DY / 16})
		}
		return out
	}

	for order, want := range map[string][][2]int{
		"":           {{0, 0}, {1, 0}, {0, 1}, {1, 1}},
		"right-down": {{0, 0}, {1, 0}, {0, 1}, {1, 1}},
		"right-up":   {{0, 1}, {1, 1}, {0, 0}, {1, 0}},
		"left-down":  {{1, 0}, {0, 0}, {1, 1}, {0, 1}},
		"left-up":    {{1, 1}, {0, 1}, {1, 0}, {0, 0}},
	} {
		m := mapOf(2, 2, 16, 16, oneSheet(), layer("l", 2, 2, true, 1, 1, 1, 1))
		m.RenderOrder = order
		got := cells(m)
		if len(got) != len(want) {
			t.Errorf("%q drew %d tiles, want %d", order, len(got), len(want))
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%q drew in order %v, want %v", order, got, want)
				break
			}
		}
	}
}

// A layer's opacity reaches the thing that draws it, and a fully transparent
// layer draws nothing — the other way Tiled makes a layer invisible, and one
// the visible flag says nothing about.
func TestDrawList_CarriesLayerOpacity(t *testing.T) {
	half := layer("half", 1, 1, true, 1)
	half.Opacity = 0.5
	clear := layer("clear", 1, 1, true, 1)
	clear.Opacity = 0
	m := mapOf(1, 1, 16, 16, oneSheet(), half, clear)

	got, _ := m.DrawList()
	if len(got) != 1 {
		t.Fatalf("got %d draws, want only the layer that is not fully transparent: %+v", len(got), got)
	}
	if got[0].Alpha != 0.5 {
		t.Errorf("the half-transparent layer's tile has alpha %g, want 0.5", got[0].Alpha)
	}
}

// A diagonally flipped tile that is not square is transposed, so the height it
// covers is its source *width* — and that is what it has to be aligned by, or
// it lands a row out.
func TestDrawList_ADiagonallyFlippedTallTileIsStillBottomLeftAligned(t *testing.T) {
	const flipD = 0x20000000
	refs := []tiled.TilesetRef{{FirstGID: 1, Tileset: sheet("walls", "walls.png", 2, 4, 16, 32)}}
	m := mapOf(1, 2, 16, 16, refs, layer("l", 1, 2, true, 0, 1|flipD))

	got, _ := m.DrawList()
	if len(got) != 1 {
		t.Fatalf("got %d draws: %+v", len(got), got)
	}
	// Transposed, the tile covers 32 wide by 16 tall. The cell's bottom is at
	// y=32, so a 16-tall drawn tile starts at 16.
	if got[0].DY != 16 {
		t.Errorf("a transposed 16x32 tile in row 1 drew at y=%d, want 16", got[0].DY)
	}
}
