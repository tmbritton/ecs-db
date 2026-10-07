package mapcanvas_test

import (
	"os"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func url(path string) string { return "/asset?p=" + path }

func sheet(name, image string, cols, count, tw, th int) *tiled.Tileset {
	return &tiled.Tileset{
		Name: name, Columns: cols, TileCount: count,
		TileWidth: tw, TileHeight: th,
		Image: tiled.Image{Source: image, Path: image, Width: cols * tw, Height: (count / cols) * th},
	}
}

func layer(name string, w, h int, visible bool, gids ...uint32) tiled.Layer {
	return tiled.Layer{Name: name, Width: w, Height: h, Visible: visible, Opacity: 1, Data: gids}
}

func mapOf(w, h, tw, th int, refs []tiled.TilesetRef, layers ...tiled.Layer) *tiled.Map {
	return &tiled.Map{
		Name: "m.tmx", Width: w, Height: h, TileWidth: tw, TileHeight: th,
		Orientation: "orthogonal", Tilesets: refs, Layers: layers,
	}
}

func oneSheet() []tiled.TilesetRef {
	return []tiled.TilesetRef{{FirstGID: 1, Tileset: sheet("floor", "floor.png", 2, 4, 16, 16)}}
}

// Scale 1 unless a test is about zoom: everything else here is about where a
// tile goes and which slice of the sheet it is, and a fitted default would
// multiply every expected coordinate by a number the test does not care about.
func opts() mapcanvas.Options { return mapcanvas.Options{AssetURL: url} }

func TestBuild_PlacesEveryNonEmptyCell(t *testing.T) {
	m := mapOf(2, 2, 16, 16, oneSheet(), layer("ground", 2, 2, true, 1, 0, 2, 3))

	c := mapcanvas.Build(m, opts())
	if c.Cols != 2 || c.Rows != 2 || c.W != 32 || c.H != 32 {
		t.Errorf("canvas is %dx%d cells, %dx%d px", c.Cols, c.Rows, c.W, c.H)
	}
	if len(c.Cells) != 3 {
		t.Fatalf("got %d cells for three tiles and a hole", len(c.Cells))
	}
	first := c.Cells[0]
	if first.Image != url("floor.png") {
		t.Errorf("image is %q", first.Image)
	}
	if first.SW != 16 || first.SH != 16 {
		t.Errorf("source size is %dx%d", first.SW, first.SH)
	}
	if first.TX != 0 || first.TY != 0 {
		t.Errorf("first cell is at (%v,%v)", first.TX, first.TY)
	}
}

func TestBuild_DuplicateLayerIDsDoNotShareViewState(t *testing.T) {
	a, b, c := layer("first", 1, 1, true, 1), layer("second", 1, 1, true, 2), layer("third", 1, 1, true, 3)
	a.ID, b.ID, c.ID = 7, 7, 9
	m := mapOf(1, 1, 16, 16, oneSheet(), a, b, c)
	canvas := mapcanvas.Build(m, opts())
	if canvas.Layers[0].ID != 0 || canvas.Layers[1].ID != 0 || canvas.Layers[2].ID != 9 {
		t.Errorf("duplicate IDs share eye/selection signals: %+v", canvas.Layers)
	}
	if mapcanvas.UniqueLayerID(m.Layers, 0) != 0 || mapcanvas.UniqueLayerID(m.Layers, 2) != 9 {
		t.Error("server and renderer disagree on which ID is unique")
	}
}

// The transform comes from the engine's own matrix, so a flipped tile in Forge
// and a flipped tile in the game are the same picture.
func TestBuild_AFlippedTileCarriesTheEnginesTransform(t *testing.T) {
	const flipH = 0x80000000
	m := mapOf(1, 1, 16, 16, oneSheet(), layer("ground", 1, 1, true, 1|flipH))

	c := mapcanvas.Build(m, opts())
	if len(c.Cells) != 1 {
		t.Fatalf("got %d cells", len(c.Cells))
	}
	want := tiled.Draw{SW: 16, SH: 16, FlipH: true}.Transform()
	got := c.Cells[0]
	if got.A != want.A || got.B != want.B || got.C != want.C || got.D != want.D {
		t.Errorf("matrix is (%v,%v,%v,%v), want (%v,%v,%v,%v)",
			got.A, got.B, got.C, got.D, want.A, want.B, want.C, want.D)
	}
}

// Every layer is drawn, including the ones the file hides.
//
// Which layers are *shown* is the browser's: the eye toggles a signal and CSS
// hides a group. So a layer left out here would be a layer whose eye could
// never turn it back on — and the server would have to be asked for the map
// again to change what is on screen, which is the round trip this removed.
func TestBuild_DrawsEveryLayerIncludingTheOnesTheFileHides(t *testing.T) {
	m := mapOf(1, 1, 16, 16, oneSheet(),
		layer("floor", 1, 1, true, 1),
		layer("wall", 1, 1, false, 2),
	)

	c := mapcanvas.Build(m, opts())
	if len(c.Cells) != 2 {
		t.Fatalf("cells: %+v", c.Cells)
	}
	if len(c.Layers) != 2 {
		t.Fatalf("layers: %+v", c.Layers)
	}
	// In draw order, first layer first, so a later layer covers an earlier one.
	// Asserted as a sequence rather than a set: order is the whole reason the
	// flat list exists, and a map keyed by index would report the same thing
	// whichever way round they came out.
	var order []string
	for _, cell := range c.Cells {
		order = append(order, cell.Layer)
	}
	if len(order) != 2 || order[0] != "floor" || order[1] != "wall" {
		t.Errorf("cells are not in draw order: %v", order)
	}
	// And each cell says which layer it belongs to, because that is what the
	// canvas groups by. The name is not enough — Tiled permits two layers to
	// share one.
	if c.Cells[0].LayerIndex != 0 || c.Cells[1].LayerIndex != 1 {
		t.Errorf("cells do not carry their layer index: %+v", c.Cells)
	}
	// Grouped once by Build, and the groups keep the flat list's order.
	if len(c.CellsByLayer[0]) != 1 || c.CellsByLayer[0][0].Layer != "floor" {
		t.Errorf("layer 0's group is wrong: %+v", c.CellsByLayer[0])
	}
	if len(c.CellsByLayer[1]) != 1 || c.CellsByLayer[1][0].Layer != "wall" {
		t.Errorf("layer 1's group is wrong: %+v", c.CellsByLayer[1])
	}
}

// What the file says is still reported, because it is what seeds the signal —
// a layer the map hides must come up hidden — and because the panel's "file"
// badge is the one row whose job is telling view state and file state apart.
func TestBuild_ReportsWhichLayersTheFileHides(t *testing.T) {
	m := mapOf(1, 1, 16, 16, oneSheet(), layer("secret", 1, 1, false, 1))

	c := mapcanvas.Build(m, opts())
	if len(c.Layers) != 1 || !c.Layers[0].HiddenInFile {
		t.Fatalf("layers: %+v", c.Layers)
	}
}

// Build must not change the map it was given: the caller may hold it, and a
// second Build with nothing hidden would otherwise report the file hides a
// layer that it does not — a false statement about the .tmx.
func TestBuild_DoesNotChangeTheMapItWasGiven(t *testing.T) {
	// A layer the file hides, because that is the field Build overwrites: it
	// forces every layer visible so the placements include them all, and doing
	// that in place would leave the caller holding a map that no longer says
	// what the .tmx says.
	m := mapOf(1, 1, 16, 16, oneSheet(), layer("secret", 1, 1, false, 1))

	if got := mapcanvas.Build(m, opts()); len(got.Cells) != 1 {
		t.Fatalf("cells: %+v", got.Cells)
	}
	if m.Layers[0].Visible {
		t.Fatal("Build made the caller's hidden layer visible")
	}
	second := mapcanvas.Build(m, opts())
	if !second.Layers[0].HiddenInFile {
		t.Error("a second build no longer reports that the file hides the layer")
	}
}

// Tiled's other way of making a layer invisible, and one the visible attribute
// says nothing about. An open eye over a layer that draws nothing, with no
// explanation, is worse than either state on its own.
func TestBuild_ALayerAtZeroOpacityIsMarkedTransparent(t *testing.T) {
	m := mapOf(1, 1, 16, 16, oneSheet(), layer("ghost", 1, 1, true, 1))
	m.Layers[0].Opacity = 0

	c := mapcanvas.Build(m, opts())
	if len(c.Cells) != 0 {
		t.Errorf("a fully transparent layer was drawn: %+v", c.Cells)
	}
	if !c.Layers[0].Transparent {
		t.Error("it is not marked transparent")
	}
	if c.Layers[0].HiddenInFile {
		t.Error("transparent is not the same as hidden, and the panel says so")
	}
}

func TestBuild_AnEmptyLayerIsMarkedEmpty(t *testing.T) {
	m := mapOf(2, 1, 16, 16, oneSheet(),
		layer("nothing", 2, 1, true, 0, 0),
		layer("something", 2, 1, true, 1, 0),
	)
	c := mapcanvas.Build(m, opts())
	if !c.Layers[0].Empty {
		t.Error("a layer with no tiles is not marked empty")
	}
	if c.Layers[1].Empty {
		t.Error("a layer with a tile is marked empty")
	}
}

// A tile you cannot see is a tile you cannot fix, and the engine refuses to
// load a map with one.
func TestBuild_AnUnresolvedGIDIsAVisibleMarker(t *testing.T) {
	m := mapOf(2, 1, 16, 16, oneSheet(), layer("ground", 2, 1, true, 1, 9999))

	c := mapcanvas.Build(m, opts())
	if len(c.Cells) != 2 {
		t.Fatalf("got %d cells, want the tile and the marker", len(c.Cells))
	}
	marker := c.Cells[1]
	if !marker.Unresolved() {
		t.Fatalf("the bad cell is not marked: %+v", marker)
	}
	if marker.Image != "" {
		t.Errorf("a marker points at an image: %q", marker.Image)
	}
	if marker.SW != 16 || marker.SH != 16 {
		t.Errorf("a marker is %dx%d, want one cell", marker.SW, marker.SH)
	}
	if marker.TX != 16 || marker.TY != 0 {
		t.Errorf("the marker is at (%v,%v), want the cell it is in", marker.TX, marker.TY)
	}
	if len(c.Problems) != 1 || !strings.Contains(c.Problems[0], "9999") {
		t.Errorf("problems: %v", c.Problems)
	}
}

func TestBuild_ProblemsAreListedOncePerReason(t *testing.T) {
	m := mapOf(3, 1, 16, 16, oneSheet(), layer("ground", 3, 1, true, 9999, 9999, 9999))

	c := mapcanvas.Build(m, opts())
	if len(c.Cells) != 3 {
		t.Errorf("every bad cell should still be marked: %d", len(c.Cells))
	}
	if len(c.Problems) != 1 {
		t.Errorf("one reason should be reported once, got %v", c.Problems)
	}
}

func TestPalette_OffersEveryTileOfASheet(t *testing.T) {
	m := mapOf(1, 1, 16, 16, oneSheet(), layer("ground", 1, 1, true, 1))
	c := mapcanvas.Build(m, opts())
	if len(c.Tilesets) != 1 {
		t.Fatalf("tilesets: %+v", c.Tilesets)
	}
	set := c.Tilesets[0]
	if set.Name != "floor" || set.FirstGID != 1 {
		t.Errorf("tileset is %q at %d", set.Name, set.FirstGID)
	}
	if len(set.Tiles) != 4 {
		t.Fatalf("got %d tiles, want 4", len(set.Tiles))
	}
	if set.Tiles[0].GID != 1 || set.Tiles[3].GID != 4 {
		t.Errorf("gids run %d..%d", set.Tiles[0].GID, set.Tiles[3].GID)
	}
	// local 2 is row 1, column 0 of a two-column sheet — at the palette's own
	// scale, which for 16px art doubles it.
	if set.Tiles[2].SX != 0 || set.Tiles[2].SY != 32 {
		t.Errorf("tile 2 is at (%d,%d)", set.Tiles[2].SX, set.Tiles[2].SY)
	}
	if set.Tiles[0].Selected {
		t.Error("an unselected tile is marked selected")
	}
}

// Epic 14 Story 2 supports two kinds of tileset. A palette that only handled
// sheets would be broken for half of them.
func TestPalette_OffersACollectionsIndividualImages(t *testing.T) {
	// Enough tiles that map iteration order will not agree with ascending order
	// by luck: with two of them it does about half the time, which is a test
	// that passes on a coin toss.
	coll := &tiled.Tileset{
		Name: "props", TileWidth: 16, TileHeight: 16,
		Tiles: map[uint32]tiled.TilesetTile{
			// Sparse ids: deleting a tile in the editor leaves a gap.
			0:  {ID: 0, Image: tiled.Image{Path: "barrel.png", Width: 16, Height: 32}},
			5:  {ID: 5, Image: tiled.Image{Path: "crate.png", Width: 16, Height: 16}},
			9:  {ID: 9, Image: tiled.Image{Path: "urn.png", Width: 16, Height: 16}},
			12: {ID: 12, Image: tiled.Image{Path: "sign.png", Width: 16, Height: 16}},
			20: {ID: 20, Image: tiled.Image{Path: "well.png", Width: 32, Height: 32}},
			31: {ID: 31, Image: tiled.Image{Path: "tree.png", Width: 16, Height: 32}},
		},
	}
	m := mapOf(1, 1, 16, 16, []tiled.TilesetRef{{FirstGID: 10, Tileset: coll}},
		layer("ground", 1, 1, true, 10))

	c := mapcanvas.Build(m, opts())
	set := c.Tilesets[0]
	if len(set.Tiles) != 6 {
		t.Fatalf("got %d tiles: %+v", len(set.Tiles), set.Tiles)
	}
	// Ascending, so one map has one palette however Go happens to iterate — and
	// checked over many builds, because Go randomises where a map walk starts
	// rather than the order within it, so a handful of keys can come out sorted
	// by luck for a whole test run.
	want := []uint32{10, 15, 19, 22, 30, 41}
	for i := 0; i < 200; i++ {
		got := gids(mapcanvas.Build(m, opts()).Tilesets[0].Tiles)
		for j, g := range want {
			if got[j] != g {
				t.Fatalf("build %d gave %v, want %v", i, got, want)
			}
		}
	}
	if set.Tiles[0].Image != url("barrel.png") {
		t.Errorf("the first tile draws from %q", set.Tiles[0].Image)
	}
	// A collection tile keeps its own proportions: 16x32 art at the palette's
	// 2x for 16px tiles.
	if set.Tiles[0].SW != 32 || set.Tiles[0].SH != 64 {
		t.Errorf("a collection tile is %dx%d, want 32x64", set.Tiles[0].SW, set.Tiles[0].SH)
	}
}

func TestPalette_ATilesetThatWasNeverReadSaysSoInsteadOfVanishing(t *testing.T) {
	m := mapOf(1, 1, 16, 16, []tiled.TilesetRef{{FirstGID: 1, Source: "missing.tsx"}},
		layer("ground", 1, 1, true, 1))

	c := mapcanvas.Build(m, opts())
	if len(c.Tilesets) != 1 {
		t.Fatalf("tilesets: %+v", c.Tilesets)
	}
	if c.Tilesets[0].Problem == "" {
		t.Error("an unread tileset offers no explanation")
	}
	if c.Tilesets[0].Name != "missing.tsx" {
		t.Errorf("it is not named after the file: %q", c.Tilesets[0].Name)
	}
}

// The strip is on a stream whose identical-patch suppression depends on two
// renders agreeing.
func TestPalette_IsInFirstGIDOrderWhateverTheFileSays(t *testing.T) {
	refs := []tiled.TilesetRef{
		{FirstGID: 100, Tileset: sheet("props", "props.png", 2, 2, 16, 16)},
		{FirstGID: 1, Tileset: sheet("floor", "floor.png", 2, 2, 16, 16)},
	}
	m := mapOf(1, 1, 16, 16, refs, layer("ground", 1, 1, true, 1))

	c := mapcanvas.Build(m, opts())
	if len(c.Tilesets) != 2 {
		t.Fatalf("tilesets: %+v", c.Tilesets)
	}
	if c.Tilesets[0].FirstGID != 1 || c.Tilesets[1].FirstGID != 100 {
		t.Errorf("palette is in file order, not first-gid order: %d then %d",
			c.Tilesets[0].FirstGID, c.Tilesets[1].FirstGID)
	}
}

func TestBuild_RefusesToInventAnAssetRoute(t *testing.T) {
	m := mapOf(1, 1, 16, 16, oneSheet(), layer("ground", 1, 1, true, 1))
	if got := mapcanvas.Build(m, mapcanvas.Options{}); len(got.Cells) != 0 {
		t.Error("a canvas was built with no way to turn a path into a URL")
	}
	if got := mapcanvas.Build(nil, opts()); len(got.Cells) != 0 {
		t.Error("a canvas was built from no map")
	}
}

// A palette tile is a window onto a sheet, so the sheet is what background-size
// scales. Scaling the window instead shows the wrong tile.
func TestPalette_ScalesTheSheetAndTheWindowTogether(t *testing.T) {
	m := mapOf(1, 1, 16, 16, oneSheet(), layer("ground", 1, 1, true, 1))

	c := mapcanvas.Build(m, opts())
	tile := c.Tilesets[0].Tiles[2] // local 2: row 1, column 0
	if tile.SX != 0 || tile.SY != 32 {
		t.Errorf("window is at (%d,%d), want (0,32)", tile.SX, tile.SY)
	}
	if tile.SW != 32 || tile.SH != 32 {
		t.Errorf("window is %dx%d, want 32x32", tile.SW, tile.SH)
	}
	// oneSheet is 2 columns of 4 tiles at 16px: 32x32 natural, 64x64 doubled.
	if tile.SheetW != 64 || tile.SheetH != 64 {
		t.Errorf("sheet is %dx%d, want 64x64", tile.SheetW, tile.SheetH)
	}
}

// Sized for the rail: legible for tiny art, and never magnifying art that is
// already big enough. The rail has about 188px of room.
func TestPalette_SwatchesFitTheRail(t *testing.T) {
	for _, tile := range []int{8, 16, 24, 32, 48, 64} {
		m := mapOf(1, 1, tile, tile,
			[]tiled.TilesetRef{{FirstGID: 1, Tileset: sheet("s", "s.png", 2, 4, tile, tile)}},
			layer("ground", 1, 1, true, 1))
		got := mapcanvas.Build(m, opts()).Tilesets[0].Tiles[0].SW
		if got < 24 && tile >= 24 {
			t.Errorf("%dpx art gave a %dpx swatch", tile, got)
		}
		if got > 96 {
			t.Errorf("%dpx art gave a %dpx swatch, which will not fit the rail", tile, got)
		}
	}
}

func gids(tiles []mapcanvas.PaletteTile) []uint32 {
	out := make([]uint32, len(tiles))
	for i, t := range tiles {
		out[i] = t.GID
	}
	return out
}

// The default zoom. Both ends matter: 16px art at 1:1 is a picture nobody can
// see, and a 20x15 map of 32px tiles at the 3x that suited the 16px case is
// 1920x1440 — which is what shipping a fixed scale actually did.
func TestFitScale_ScalesSmallArtUpAndLeavesLargeMapsAlone(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		cols, rows, tileW, thH int
		want                   int
	}{
		{"the engine's own map, 20x15 at 32px", 20, 15, 32, 32, 1},
		{"the e2e fixture, 6x4 at 16px", 6, 4, 16, 16, 4},
		{"a small map of tiny art", 8, 8, 8, 8, 4},
		{"a big map of small art", 100, 100, 16, 16, 1},
		{"a map larger than the budget at any zoom", 200, 200, 32, 32, 1},
		// Both dimensions bind. A tall, narrow map has width to spare and no
		// height, and a fit that only looked at one of them would magnify it
		// four times off the bottom of the panel.
		{"tall and narrow", 10, 40, 16, 16, 1},
		{"short and wide", 50, 4, 16, 16, 1},
		{"a map with no size", 0, 0, 32, 32, 1},
		{"a map with no tile size", 10, 10, 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mapcanvas.FitScale(tc.cols, tc.rows, tc.tileW, tc.thH); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// The chosen scale keeps the map inside the budget, which is the property the
// numbers above are examples of.
func TestFitScale_KeepsTheMapWithinTheBudget(t *testing.T) {
	// Non-square shapes included, and both dimensions checked. A square-only
	// loop measuring width alone is exactly the shape that let a width-only fit
	// go unnoticed.
	for _, tile := range []int{8, 16, 24, 32, 48} {
		for _, cols := range []int{1, 6, 20, 60, 300} {
			for _, rows := range []int{1, 4, 20, 90, 300} {
				s := mapcanvas.FitScale(cols, rows, tile, tile)
				if s < 1 {
					t.Fatalf("%dx%d at %dpx gave scale %d", cols, rows, tile, s)
				}
				if s == 1 {
					// Either it fits at 1x or it does not, and zooming out
					// further is not something integers offer — the author
					// scrolls.
					continue
				}
				w, h := cols*tile*s, rows*tile*s
				if w > mapcanvas.FitBudgetW || h > mapcanvas.FitBudgetH {
					t.Errorf("%dx%d at %dpx chose %dx, which is %dx%d — the budget is %dx%d",
						cols, rows, tile, s, w, h, mapcanvas.FitBudgetW, mapcanvas.FitBudgetH)
				}
			}
		}
	}
}

// The claim the fitted default exists to make, read off the map this repository
// actually ships rather than from numbers copied into a test: resizing it must
// not leave this green while its name goes on being true by coincidence.
func TestFitScale_TheShippedMapFits(t *testing.T) {
	raw, err := os.ReadFile("../../../mods/map/level1.tmx")
	if err != nil {
		t.Fatal(err)
	}
	m, err := tiled.Parse(raw, "level1.tmx")
	if err != nil {
		t.Fatal(err)
	}
	s := mapcanvas.FitScale(m.Width, m.Height, m.TileWidth, m.TileHeight)
	w, h := m.Width*m.TileWidth*s, m.Height*m.TileHeight*s
	if w > mapcanvas.FitBudgetW || h > mapcanvas.FitBudgetH {
		t.Errorf("the shipped map is %dx%d cells of %dpx and defaults to %dx, which is %dx%d",
			m.Width, m.Height, m.TileWidth, s, w, h)
	}
}

// The one moment the server has an opinion about zoom: what the page opens at.
// After that it is a signal and the server never hears about it again.
func TestInitialScale_FitsTheMap(t *testing.T) {
	m := mapOf(6, 4, 16, 16, oneSheet(), layer("ground", 6, 4, true, make([]uint32, 24)...))
	c := mapcanvas.Build(m, opts())

	if got, want := mapcanvas.InitialScale(c), mapcanvas.FitScale(6, 4, 16, 16); got != want {
		t.Errorf("a map opens at %dx, want the fitted %dx", got, want)
	}
}

// A canvas that could not be built at all opens at 1x rather than 0x. Nothing
// is drawn either way, and a zoom of zero would make every size derived from it
// on the page read as a map of no size — which is a different and untrue thing
// from a map that could not be drawn.
func TestInitialScale_IsOneForACanvasThatCouldNotBeBuilt(t *testing.T) {
	if got := mapcanvas.InitialScale(mapcanvas.Canvas{}); got != 1 {
		t.Errorf("an unbuilt canvas opens at %dx, want 1x", got)
	}
}

// A copy, so a caller that appends to or reorders what it was given does not
// change both what the control offers and what ParseZoom will accept.
func TestSteps_HandsOutACopy(t *testing.T) {
	first := mapcanvas.Steps()
	if len(first) == 0 {
		t.Fatal("there are no zoom steps")
	}
	first[0] = 99
	first = append(first, 1000)

	second := mapcanvas.Steps()
	if second[0] == 99 {
		t.Error("a caller changed the shared steps")
	}
	if len(second) == len(first) {
		t.Error("a caller appended to the shared steps")
	}
}
