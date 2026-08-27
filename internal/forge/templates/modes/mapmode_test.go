package modes

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
)

// Every link in MAP mode is built from the whole view, or looking at a
// different layer would deselect your brush.
func TestMapView_HrefCarriesEveryPart(t *testing.T) {
	v := MapView{Path: "/p/level1.tmx", Hidden: map[int]bool{2: true, 0: true}, Active: 3, SelectedGID: 7}
	got := v.Href()

	for _, want := range []string{"map=%2Fp%2Flevel1.tmx", "hide=0%2C2", "layer=3", "tile=7"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing %q", got, want)
		}
	}
}

// One view has one URL. The hidden set is a map, and an unordered rendering
// would give the same view two addresses — which both defeats the page
// stream's identical-patch suppression and makes a link unstable.
func TestMapView_HiddenLayersAreOrdered(t *testing.T) {
	v := MapView{Path: "/p/m.tmx", Hidden: map[int]bool{5: true, 1: true, 3: true}}
	first := v.Href()
	for i := 0; i < 20; i++ {
		if got := v.Href(); got != first {
			t.Fatalf("two renders of one view differ:\n%s\n%s", first, got)
		}
	}
	if !strings.Contains(first, "hide=1%2C3%2C5") {
		t.Errorf("hidden layers are not in order: %s", first)
	}
}

func TestMapView_AnEmptyViewIsThePlainMode(t *testing.T) {
	if got := (MapView{}).Href(); got != "/forge/map" {
		t.Errorf("got %q", got)
	}
}

func TestMapView_TogglingALayerGoesBothWays(t *testing.T) {
	v := MapView{Path: "/p/m.tmx"}

	hidden := v.WithLayerToggled(1)
	if !hidden.Hidden[1] {
		t.Fatal("toggling a visible layer did not hide it")
	}
	shown := hidden.WithLayerToggled(1)
	if shown.Hidden[1] {
		t.Error("toggling a hidden layer did not show it")
	}
	// And it leaves the original alone: a link builder that mutated the view it
	// was given would make every later link on the page wrong.
	if v.Hidden[1] {
		t.Error("building a link changed the view it was built from")
	}
}

func TestMapView_TogglingALayerKeepsTheRestOfTheView(t *testing.T) {
	v := MapView{Path: "/p/m.tmx", Hidden: map[int]bool{4: true}, Active: 2, SelectedGID: 9}
	got := v.WithLayerToggled(1)

	if got.SelectedGID != 9 {
		t.Error("toggling a layer deselected the tile")
	}
	// Hiding the layer you are painting into does not stop it being the one you
	// are painting into: they are two controls on one row for that reason.
	if got.Active != 2 {
		t.Errorf("toggling a layer's visibility moved the brush to %d", got.Active)
	}
	if !got.Hidden[4] {
		t.Error("toggling a layer showed another one")
	}
	if got.Path != v.Path {
		t.Error("toggling a layer changed the map")
	}
}

// Clicking the tile you already have selected puts the brush down, which is the
// only way to deselect without a separate control.
func TestMapView_ClickingTheSelectedTileClearsIt(t *testing.T) {
	v := MapView{Path: "/p/m.tmx", SelectedGID: 3}

	if got := v.WithTile(4); got.SelectedGID != 4 {
		t.Errorf("selecting another tile gave %d", got.SelectedGID)
	}
	if got := v.WithTile(3); got.SelectedGID != 0 {
		t.Errorf("clicking the selected tile gave %d, want none", got.SelectedGID)
	}
}

// A layer index and a global id mean different things in a different map, so
// carrying them across would hide a layer nobody asked about and select a tile
// that is not there.
func TestMapView_SwitchingMapsCarriesNothingElse(t *testing.T) {
	v := MapView{Path: "/p/a.tmx", Hidden: map[int]bool{1: true}, SelectedGID: 9}
	got := v.WithMap("/p/b.tmx")

	if got.Path != "/p/b.tmx" {
		t.Errorf("path is %q", got.Path)
	}
	if len(got.Hidden) != 0 {
		t.Errorf("hidden layers came along: %v", got.Hidden)
	}
	if got.SelectedGID != 0 {
		t.Errorf("the tile selection came along: %d", got.SelectedGID)
	}
}

func TestParseHidden_TakesWhatItCanAndIgnoresTheRest(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want map[int]bool
	}{
		{"", nil},
		{"0,2", map[int]bool{0: true, 2: true}},
		{" 1 , 3 ", map[int]bool{1: true, 3: true}},
		// A stale or hand-edited URL should show you the map, not an error page.
		{"nonsense", nil},
		{"1,nonsense,-4", map[int]bool{1: true}},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got := ParseHidden(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k := range tc.want {
				if !got[k] {
					t.Errorf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestParseGID_TakesWhatItCanAndIgnoresTheRest(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want uint32
	}{
		{"", 0},
		{"7", 7},
		{" 7 ", 7},
		{"nonsense", 0},
		{"-1", 0},
		{"99999999999999999999", 0},
	} {
		if got := ParseGID(tc.in); got != tc.want {
			t.Errorf("ParseGID(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// CSS matrix(a,b,c,d,e,f) is column-major — x' = a·x + c·y + e — and the
// engine's Matrix is written the other way round, so b and c swap crossing
// over. Getting it wrong flips every rotated tile about the wrong axis, which
// looks like art authored badly rather than a transposed pair of numbers.
func TestCellStyle_WritesTheMatrixInCSSOrder(t *testing.T) {
	got := string(cellStyle(mapcanvas.Cell{
		SW: 16, SH: 16, Image: "/forge/asset?path=x.png", SX: 32, SY: 48,
		A: 1, B: 2, C: 3, D: 4, TX: 5, TY: 6, Alpha: 1,
	}))
	if !strings.Contains(got, "transform:matrix(1,3,2,4,5,6)") {
		t.Errorf("matrix is not in CSS order: %s", got)
	}
	if !strings.Contains(got, "background-position:-32px -48px") {
		t.Errorf("the source rectangle is not offset: %s", got)
	}
	if !strings.Contains(got, "width:16px;height:16px") {
		t.Errorf("the cell is not its tile's size: %s", got)
	}
	if strings.Contains(got, "opacity") {
		t.Errorf("a fully opaque cell writes an opacity: %s", got)
	}
}

func TestCellStyle_AnUnresolvedCellHasNoImage(t *testing.T) {
	got := string(cellStyle(mapcanvas.Cell{SW: 16, SH: 16, A: 1, D: 1, Alpha: 1, Problem: "no tileset"}))
	if strings.Contains(got, "background-image") {
		t.Errorf("a marker points at an image: %s", got)
	}
}

func TestCellStyle_ATransparentLayerCarriesItsOpacity(t *testing.T) {
	got := string(cellStyle(mapcanvas.Cell{SW: 16, SH: 16, A: 1, D: 1, Alpha: 0.5, Image: "x"}))
	if !strings.Contains(got, "opacity:0.5") {
		t.Errorf("got %s", got)
	}
}

// Written as a number at every magnitude. Go's 'g' verb switches to exponent
// notation at a million, which no CSS parser reads — so a map big enough would
// stop drawing entirely.
func TestCellStyle_ALargeCoordinateIsStillANumber(t *testing.T) {
	got := string(cellStyle(mapcanvas.Cell{SW: 16, SH: 16, A: 1, D: 1, TX: 2000000, Alpha: 1}))
	if strings.Contains(got, "e+") {
		t.Errorf("a coordinate was written in exponent notation: %s", got)
	}
	if !strings.Contains(got, "2000000") {
		t.Errorf("got %s", got)
	}
}

// A palette tile is a window onto a sheet, so the sheet is what
// background-size scales — scaling the window instead shows the wrong tile.
func TestPaletteStyle_ScalesTheSheetBehindTheWindow(t *testing.T) {
	got := string(paletteStyle(mapcanvas.PaletteTile{
		Image: "/forge/asset?path=s.png",
		SX:    32, SY: 0, SW: 32, SH: 32, SheetW: 256, SheetH: 480,
	}))
	if !strings.Contains(got, "background-size:256px 480px") {
		t.Errorf("the sheet is not scaled: %s", got)
	}
	if !strings.Contains(got, "background-position:-32px 0px") {
		t.Errorf("the window is not offset: %s", got)
	}
}

// The eye and the name are two controls. Choosing where a stroke lands must not
// disturb what is drawn or what is in hand.
func TestMapView_ChoosingALayerToPaintIntoKeepsTheRestOfTheView(t *testing.T) {
	v := MapView{Path: "/p/m.tmx", Hidden: map[int]bool{4: true}, Active: 0, SelectedGID: 9}
	got := v.WithActiveLayer(2)

	if got.Active != 2 {
		t.Errorf("active layer is %d", got.Active)
	}
	if got.SelectedGID != 9 {
		t.Error("choosing a layer put the tile down")
	}
	if !got.Hidden[4] {
		t.Error("choosing a layer showed one that was hidden")
	}
	if got.Path != v.Path {
		t.Error("choosing a layer changed the map")
	}
}

func TestParseLayer_TakesWhatItCanAndIgnoresTheRest(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{{"", 0}, {"2", 2}, {" 2 ", 2}, {"nonsense", 0}, {"-1", 0}} {
		if got := ParseLayer(tc.in); got != tc.want {
			t.Errorf("ParseLayer(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestMapView_ZoomTravelsWithEveryOtherLink(t *testing.T) {
	v := MapView{Path: "/p/m.tmx", Hidden: map[int]bool{4: true}, Active: 2, Zoom: 3, SelectedGID: 9}

	if !strings.Contains(v.Href(), "zoom=3") {
		t.Errorf("the view's own href drops the zoom: %s", v.Href())
	}
	for name, got := range map[string]MapView{
		"toggling a layer": v.WithLayerToggled(1),
		"choosing a layer": v.WithActiveLayer(0),
		"selecting a tile": v.WithTile(11),
		"zooming":          v.WithZoom(2),
	} {
		want := 3
		if name == "zooming" {
			want = 2
		}
		if got.Zoom != want {
			t.Errorf("%s left the zoom at %d, want %d", name, got.Zoom, want)
		}
	}
	// Switching maps drops it, like everything else: a zoom that fits one map
	// need not fit another, and the next one gets its own fitted default.
	if got := v.WithMap("/p/other.tmx"); got.Zoom != 0 {
		t.Errorf("switching maps carried the zoom: %d", got.Zoom)
	}
}

// Only the steps the control offers. A hand-typed 137 would draw one tile the
// size of the panel from a URL nothing in the UI can produce.
func TestParseZoom_TakesOnlyTheStepsTheControlOffers(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"", 0},
		{"1", 1},
		{"4", 4},
		{"8", 8},
		{" 2 ", 2},
		{"5", 0},
		{"137", 0},
		{"0", 0},
		{"-2", 0},
		{"nonsense", 0},
		{"2.5", 0},
	} {
		if got := ParseZoom(tc.in); got != tc.want {
			t.Errorf("ParseZoom(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
