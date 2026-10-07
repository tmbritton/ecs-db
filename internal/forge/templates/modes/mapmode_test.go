package modes

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
)

func TestMapView_AnEmptyViewIsThePlainMode(t *testing.T) {
	if got := (MapView{}).Href(); got != "/forge/map" {
		t.Errorf("got %q", got)
	}
}

func TestMapView_HrefNamesTheMapAndNothingElse(t *testing.T) {
	got := MapView{Path: "mods/map/level1.tmx"}.Href()
	if got != "/forge/map?map=mods%2Fmap%2Flevel1.tmx" {
		t.Errorf("got %q", got)
	}
}

func TestMapView_SwitchingMapsPointsAtTheNewOne(t *testing.T) {
	got := MapView{Path: "/p/a.tmx"}.WithMap("/p/b.tmx")
	if got.Path != "/p/b.tmx" {
		t.Errorf("path is %q", got.Path)
	}
}

func TestMapSignals_LayerIdentitySurvivesReorder(t *testing.T) {
	data := mapRegionFixture()
	data.Canvas.Layers = []mapcanvas.Layer{
		{Index: 0, ID: 3, Name: "props"}, {Index: 1, ID: 1, Name: "ground"},
	}
	markup := MapSignals(data) + render(t, mapLayers(data))
	for _, want := range []string{`"layerID":3`, `"hideID3":false`, `"hideID1":false`, "$layerID === 3", "$hideID3"} {
		if !strings.Contains(markup, want) {
			t.Errorf("layer view state is still indexed after a move: missing %q", want)
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

// ftoa must never write a coordinate in exponent notation: no CSS parser reads
// `1e+06`, and a single unparseable matrix stops the whole cell drawing.
//
// Restored after being dropped in the move to signals — ftoa and cellStyle are
// untouched by that change, and this was their only guard.
func TestCellStyle_ALargeCoordinateIsStillANumber(t *testing.T) {
	got := string(cellStyle(mapcanvas.Cell{
		SW: 16, SH: 16, A: 1, D: 1, TX: 1200000, TY: 2500000, Alpha: 1,
	}))
	if strings.Contains(got, "e+") || strings.Contains(got, "E+") {
		t.Errorf("a coordinate came out in exponent notation, which no CSS parser reads: %s", got)
	}
	if !strings.Contains(got, "1200000") || !strings.Contains(got, "2500000") {
		t.Errorf("the coordinates are not in the matrix: %s", got)
	}
}

// The palette shows a window onto a sheet, so the sheet behind the window has
// to be scaled with it — background-size rather than a transform, which would
// scale the element's own box too and squash it in a 212px rail.
//
// Restored for the same reason: paletteStyle is untouched by the move to
// signals, and dropping its test dropped the only cover it had.
func TestPaletteStyle_ScalesTheSheetBehindTheWindow(t *testing.T) {
	got := string(paletteStyle(mapcanvas.PaletteTile{
		GID: 1, Image: "/a.png", SX: 32, SY: 16, SW: 32, SH: 32, SheetW: 128, SheetH: 64,
	}))
	for _, want := range []string{
		"width:32px", "height:32px",
		"background-size:128px 64px",
		"background-position:-32px -16px",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("paletteStyle is missing %q: %s", want, got)
		}
	}
}

// A layer name is written by whoever authored the .tmx, and it ends up inside a
// JavaScript string literal in a Datastar expression.
//
// Quotes and backslashes are the obvious hazard and are escaped. The one that
// is not obvious is `@`: Datastar rewrites `@name(` into an action call *after*
// it has finished protecting string literals, so the rewrite reaches inside
// them and a layer called "@post(x)" makes the status line read
// `__action("post",evt,x)`. Corruption of someone's layer name rather than
// execution of it — the literal is protected from the $signal pass, which is
// the one that could matter — but a name should arrive as it was written.
func TestQuoteJS_NeutralisesEverythingThatCanEscapeTheLiteral(t *testing.T) {
	for _, tc := range []struct{ name, in, wantAbsent string }{
		{"a single quote", `it's`, `'it's'`},
		{"a backslash", `a\b`, `'a\b'`},
		{"a Datastar action", `@post(x)`, `@post(`},
		{"a line separator", "a b", " "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := quoteJS(tc.in)
			if strings.Contains(got, tc.wantAbsent) {
				t.Errorf("quoteJS(%q) = %s, which still contains %q", tc.in, got, tc.wantAbsent)
			}
			if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
				t.Errorf("quoteJS(%q) = %s, which is not a single-quoted literal", tc.in, got)
			}
		})
	}
}
