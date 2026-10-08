package modes

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
	"github.com/tmbritton/ecs-db/internal/forge/tilelinks"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
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

func TestMapView_CellSelectionKeepsLayerIdentityAndClearsOnOtherSelection(t *testing.T) {
	base := MapView{Path: "mods/map/level.tmx"}
	for _, tt := range []struct {
		name string
		view MapView
		want string
	}{
		{"origin cell", base.WithCell(7, 0, 0), "/forge/map?layer=7&map=mods%2Fmap%2Flevel.tmx&x=0&y=0"},
		{"other cell", base.WithCell(9, 3, 2), "/forge/map?layer=9&map=mods%2Fmap%2Flevel.tmx&x=3&y=2"},
		{"spawn clears cell", base.WithCell(7, 0, 0).WithSpawn(6), "/forge/map?map=mods%2Fmap%2Flevel.tmx&spawn=6"},
		{"switching map clears cell", base.WithCell(7, 0, 0).WithMap("other.tmx"), "/forge/map?map=other.tmx"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.view.Href(); got != tt.want {
				t.Fatalf("MapView.Href = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMapSignals_SelectedCellSeedsItsLayerAndKeyboardCursor(t *testing.T) {
	data := mapRegionFixture()
	data.Canvas.Layers = []mapcanvas.Layer{{Index: 0, ID: 4}, {Index: 1, ID: 9}}
	data.MapView = MapView{Path: "level.tmx"}.WithCell(9, 1, 0)
	got := MapSignals(data)
	for _, want := range []string{`"layer":1`, `"layerID":9`, `"inspectX":1`, `"inspectY":0`} {
		if !strings.Contains(got, want) {
			t.Errorf("selected Tile URL did not seed view's keyboard cursor: missing %q in %s", want, got)
		}
	}
}

func TestRestoreMapViewAction_DoesNotOverwriteANewLayerWithMissingSavedState(t *testing.T) {
	data := mapRegionFixture()
	data.Canvas.Layers = []mapcanvas.Layer{{Index: 0, ID: 1}, {Index: 1, ID: 9}}
	got := restoreMapViewAction(data)
	for _, id := range []string{"hideID1", "hideID9"} {
		if !strings.Contains(got, `hasOwnProperty.call(view,`+quoteJS(id)+`)`) {
			t.Errorf("restoring a page that gained layer %s would overwrite its file visibility: %s", id, got)
		}
	}
}

func TestInspectKeyAction_PersistsViewBeforeKeyboardNavigation(t *testing.T) {
	action := inspectKeyAction(mapRegionFixture())
	if save, navigate := strings.Index(action, "sessionStorage.setItem("), strings.Index(action, "window.location.assign("); save < 0 || navigate < save {
		t.Errorf("keyboard inspection discards the current view state: %s", action)
	}
}

func TestMapInspector_PinsBrowserTileSelectionTestIDs(t *testing.T) {
	data := mapRegionFixture()
	data.MapView = MapView{Path: "level.tmx", LayerID: 1, CellX: 2, CellY: 1}
	data.SelectedTile = &tilelinks.Inspection{LayerID: 1, X: 2, Y: 1, GID: 3, EntityType: "Wall"}
	markup := render(t, mapToolbar(data)) + render(t, mapCanvas(data)) + render(t, MapInspectorRegion(data))
	for _, id := range []string{"tool-inspect", "map-inspect-cursor", "tile-selected", "tile-template", "tile-close", "tile-linked-objects"} {
		if n := strings.Count(markup, `data-testid="`+id+`"`); n != 1 {
			t.Errorf("MAP browser test ID %s occurs %d times, want once", id, n)
		}
	}
}

func TestTileInspector_OffersOnlyUniqueUnlinkedObjects(t *testing.T) {
	data := mapRegionFixture()
	data.Schema = schema.DatabaseSchema{Components: map[string]schema.Component{
		"Position":    {Type: "object", Properties: map[string]schema.Property{"x": {Type: "number"}, "y": {Type: "number"}}},
		"Health":      {Type: "object", Properties: map[string]schema.Property{"hp": {Type: "integer"}}},
		"TileVisual":  {Type: "object", Properties: map[string]schema.Property{"image": {Type: "string"}}},
		"Passability": {Type: "object", Properties: map[string]schema.Property{"kind": {Type: "string"}}},
	}, EntityTypes: map[string]schema.EntityType{
		"River": {RequiredComponents: []string{"Position", "Health"}, OptionalComponents: []string{"TileVisual", "Passability"}, ValidationLevel: "strict"},
	}, Interactions: map[string]map[string]schema.InteractionRule{"Passability": {"liquid": {Open: true}}}}
	data.MapView = MapView{Path: "level.tmx", LayerID: 1, CellX: 0, CellY: 0}
	data.SelectedTile = &tilelinks.Inspection{GID: 1, EntityType: "Floor", ObjectIDs: []int{5}}
	data.ObjectGroups = []tiled.ObjectGroup{{Objects: []tiled.Object{
		{ID: 5, Type: "Wall"},
		{ID: 6, Type: "River", Properties: tiled.Properties{
			"Health.hp": {Type: "int", Value: "4"}, "TileVisual.image": {Value: "river.png"}, "Passability.kind": {Value: "liquid"},
		}},
		{ID: 7, Type: "Door"},
		{ID: 7, Type: "Door"},
		{ID: 8},
		{ID: 9, Type: "River", Properties: tiled.Properties{"TileLink.layerID": {Type: "int", Value: "2"}}},
		{ID: 10, Type: "River"},
	}}}
	markup := render(t, tileInspector(data))
	for _, want := range []string{`data-testid="tile-link-target"`, `<option value="6"`, `River · object 6 · art river.png · movement liquid`, `data-testid="tile-linked-5"`, `data-testid="tile-unlink-5"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("Tile inspector missing %q", want)
		}
	}
	for _, unwanted := range []string{`<option value="5">`, `<option value="7">`, `<option value="8">`, `<option value="9">`, `<option value="10">`} {
		if strings.Contains(markup, unwanted) {
			t.Errorf("Tile inspector offered ambiguous or already-linked %q", unwanted)
		}
	}
}

func TestTileInspector_ShowsLinkedObjectsVisualAndInteractionOwnership(t *testing.T) {
	data := mapRegionFixture()
	data.MapView = MapView{Path: "level.tmx", LayerID: 1, CellX: 0, CellY: 0}
	data.SelectedTile = &tilelinks.Inspection{GID: 1, EntityType: "Floor", ObjectIDs: []int{5}}
	data.ObjectGroups = []tiled.ObjectGroup{{Objects: []tiled.Object{{ID: 5, Type: "River", Properties: tiled.Properties{
		"Passability.kind": {Value: "liquid"}, "Visibility.kind": {Value: "clear"},
		"TileVisual.image": {Value: "river.png"},
	}}}}}
	markup := render(t, tileInspector(data))
	for _, want := range []string{"River", "liquid", "clear", "river.png"} {
		if !strings.Contains(markup, want) {
			t.Errorf("linked River inspector does not name %s", want)
		}
	}
}

func TestTileInspector_DescribesCaseInsensitiveAuthoredComponents(t *testing.T) {
	data := mapRegionFixture()
	data.MapView = MapView{Path: "level.tmx", LayerID: 1, CellX: 0, CellY: 0}
	data.SelectedTile = &tilelinks.Inspection{GID: 1, EntityType: "Floor", ObjectIDs: []int{5}}
	data.ObjectGroups = []tiled.ObjectGroup{{Objects: []tiled.Object{{ID: 5, Type: "River", Properties: tiled.Properties{
		"passability.kind": {Value: "liquid"}, "visibility.KIND": {Value: "clear"},
		"tilevisual.image": {Value: "river.png"},
	}}}}}
	markup := render(t, tileInspector(data))
	for _, want := range []string{"River", "liquid", "clear", "river.png"} {
		if !strings.Contains(markup, want) {
			t.Errorf("case-insensitive linked River component %q not described", want)
		}
	}
}

func TestTileInspector_ExplainsIncompatibleSharedArtBeforeLinking(t *testing.T) {
	ts, err := tiled.ParseTileset([]byte(`<tileset name="river" tilewidth="16" tileheight="16" tilecount="2" columns="2"><image source="river.png" width="32" height="16"/><properties><property name="entityType" value="River"/></properties></tileset>`), "river.tsx", ".")
	if err != nil {
		t.Fatal(err)
	}
	data := mapRegionFixture()
	data.MapView = MapView{Path: "level.tmx", LayerID: 1, CellX: 1, CellY: 0}
	data.SelectedTile = &tilelinks.Inspection{LayerID: 1, X: 1, Y: 0, GID: 2, EntityType: "River"}
	data.Schema = schema.DatabaseSchema{Components: map[string]schema.Component{
		"Position":   {Type: "object", Properties: map[string]schema.Property{"x": {Type: "number"}, "y": {Type: "number"}}},
		"TileVisual": {Type: "object", Properties: map[string]schema.Property{"image": {Type: "string"}}},
	}, EntityTypes: map[string]schema.EntityType{
		"River": {RequiredComponents: []string{"Position"}, OptionalComponents: []string{"TileVisual"}, ValidationLevel: "strict"},
	}}
	obj := tiled.Object{ID: 5, Type: "River", X: 0, Y: 0, Properties: tiled.Properties{
		"TileLink.layerID": {Type: "int", Value: "1"}, "TileVisual.image": {Value: "river.png"},
	}}
	data.ObjectGroups = []tiled.ObjectGroup{{Objects: []tiled.Object{obj}}}
	data.MapPreview = &tiled.Map{
		Width: 2, Height: 1, TileWidth: 16, TileHeight: 16,
		Layers:   []tiled.Layer{{ID: 1, Name: "ground", Width: 2, Height: 1, Visible: true, Opacity: 1, Data: []uint32{1, 2}}},
		Tilesets: []tiled.TilesetRef{{FirstGID: 1, Tileset: ts}}, ObjectGroups: data.ObjectGroups,
	}
	markup := render(t, tileInspector(data))
	if !strings.Contains(markup, `<option value="5" disabled`) || !strings.Contains(markup, "distinct artwork") {
		t.Fatalf("shared visual mismatch not explained before selection: %s", markup)
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
