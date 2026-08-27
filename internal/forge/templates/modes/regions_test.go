package modes

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
	"github.com/tmbritton/ecs-db/internal/forge/maps"
)

// hasID reports whether html carries this element id.
//
// Anchored on the whitespace before the attribute, because `data-testid="x"`
// ends with the substring `id="x"` — so a plain Contains finds a test id and
// reports it as an element id. Every one of these tests would then pass by
// matching the wrong attribute, which is how `map-canvas` came to be both a
// region id and a test id without anything noticing.
func hasID(html, id string) bool { return idRE(id).MatchString(html) }

func idRE(id string) *regexp.Regexp {
	return regexp.MustCompile(`\sid="` + regexp.QuoteMeta(id) + `"`)
}

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// Every state a mode can be in, because the ids have to be there in all of
// them — see TestRegions_AreAlwaysPresent.
func modeStates(t *testing.T) map[string]Data {
	t.Helper()
	populated := modeFixture()
	populated.HasMaps, populated.HasMachines = true, true

	chart := canvasFixture(t, canvasMachine, "")
	chart.HasSession, chart.HasMaps = true, true

	return map[string]Data{
		"no project": {},
		"a project with nothing in it": {
			HasSession: true, HasMachines: true, HasMaps: true,
		},
		// A map actually open and drawn, because that is the state where the
		// regions have contents — and contents are where an id can collide
		// with a test id, or one region turn out to contain another.
		"a map open": withEverything(mapRegionFixture()),
		// The editors themselves, because the ids inside them are the ones that
		// could collide with a region id — components/problems.templ and the
		// statechart's SVG markers both emit ids of their own.
		"a component selected": populated,
		"a statechart drawn":   chart,
	}
}

// A patch replaces an element by id. An id that is present in one state and
// absent in another is an id a patch silently lands nowhere — the region stops
// updating, with no error anywhere, for the life of the tab.
func TestRegions_AreAlwaysPresent(t *testing.T) {
	for slug, content := range Registry {
		for state, data := range modeStates(t) {
			rendered := make([]string, len(content.Regions))
			for i, region := range content.Regions {
				rendered[i] = render(t, region.Render(data))
				if !hasID(rendered[i], region.ID) {
					t.Errorf("%s/%s: region %q does not carry id=%q:\n%.200s",
						slug, state, region.ID, region.ID, rendered[i])
				}
			}
			page := render(t, content.Page(rendered))
			for _, region := range content.Regions {
				if !hasID(page, region.ID) {
					t.Errorf("%s/%s: the page is missing region %q", slug, state, region.ID)
				}
			}
		}
	}
}

// Regions have to be disjoint or the split buys nothing: if one contains
// another, a change inside the inner one dirties the outer too, and both are
// sent — which is the whole-page patch this was meant to replace, with extra
// steps.
func TestRegions_DoNotContainOneAnother(t *testing.T) {
	for slug, content := range Registry {
		for state, data := range modeStates(t) {
			for _, outer := range content.Regions {
				html := render(t, outer.Render(data))
				for _, inner := range content.Regions {
					if inner.ID == outer.ID {
						continue
					}
					if hasID(html, inner.ID) {
						t.Errorf("%s/%s: region %q contains region %q, so a change to the inner one sends both",
							slug, state, outer.ID, inner.ID)
					}
				}
			}
		}
	}
}

func TestRegions_HaveDistinctIDs(t *testing.T) {
	for slug, content := range Registry {
		seen := make(map[string]bool, len(content.Regions))
		for _, region := range content.Regions {
			if seen[region.ID] {
				t.Errorf("%s: two regions share the id %q, so one of them can never be patched", slug, region.ID)
			}
			seen[region.ID] = true
		}
		if len(content.Regions) == 0 {
			t.Errorf("%s: no regions, so nothing on this mode can ever be patched", slug)
		}
	}
}

// The point of the exercise. Zoom, the tile in hand and which layers are drawn
// are not in what the server renders, so a re-render cannot undo them — which
// is the failure the query-string version had and the reason for the change.
//
// Asserted on the output rather than on the types, because "the field is gone"
// is not the property. The property is that the rendered HTML defers to a
// signal: a template could perfectly well read a signal *and* bake a value in,
// and the baked one would win on the next patch.
func TestMapRegions_ViewStateIsDeferredToTheBrowser(t *testing.T) {
	rendered := renderRegionsOf(t, Registry["map"], mapRegionFixture())

	for _, tc := range []struct{ region, want, why string }{
		{"mode-list", "$tile", "the palette marks the tile in hand"},
		{"mode-list", "$layer", "the panel marks the layer a stroke lands on"},
		{"mode-list", "$hide0", "the eye toggles the layer"},
		{"map-head", "$zoom", "the zoom control marks the current step"},
		{"map-canvas-region", "--map-zoom", "the canvas scales from the signal"},
		{"map-canvas-region", "$hide0", "the layer group hides from the signal"},
		{"map-foot", "$zoom", "the status line reads the zoom"},
		{"map-foot", "$tile", "the status line reads the tile in hand"},
	} {
		if !strings.Contains(rendered[tc.region], tc.want) {
			t.Errorf("%s does not read %s — %s:\n%.400s",
				tc.region, tc.want, tc.why, rendered[tc.region])
		}
	}

	// And nothing is baked in beside it. A cell carries the map's own
	// coordinates; the moment one carries a zoomed one, changing zoom means
	// asking the server for three hundred cells again.
	canvas := rendered["map-canvas-region"]
	if !strings.Contains(canvas, "--map-w:32") {
		t.Errorf("the canvas does not carry the map's own size:\n%.300s", canvas)
	}
	if strings.Contains(canvas, "palette-tile--selected") {
		t.Error("the canvas renders a tile selection the server should not know about")
	}
}

// mapRegionFixture is a map with cells, layers and a palette: enough that every
// MAP region renders its real contents rather than an empty wrapper.
func mapRegionFixture() Data {
	canvas := mapcanvas.Canvas{
		Cols: 2, Rows: 1, TileW: 16, TileH: 16, W: 32, H: 16,
		Cells: []mapcanvas.Cell{
			{X: 0, Y: 0, Layer: "ground", Image: "/forge/asset?path=t.png", SW: 16, SH: 16, A: 1, D: 1, Alpha: 1},
			{X: 1, Y: 0, Layer: "ground", Image: "/forge/asset?path=t.png", SX: 16, SW: 16, SH: 16, A: 1, D: 1, TX: 16, Alpha: 1},
		},
		Layers: []mapcanvas.Layer{{Index: 0, Name: "ground"}},
		Tilesets: []mapcanvas.Tileset{{
			Name: "starter",
			Tiles: []mapcanvas.PaletteTile{
				{GID: 1, Image: "/forge/asset?path=t.png", SW: 16, SH: 16, SheetW: 32, SheetH: 16},
				{GID: 2, Image: "/forge/asset?path=t.png", SX: 16, SW: 16, SH: 16, SheetW: 32, SheetH: 16},
			},
		}},
	}
	return Data{
		HasMaps:     true,
		Maps:        []maps.Map{{Path: "level.tmx", Name: "level.tmx", Configured: true}},
		SelectedMap: "level.tmx",
		MapView:     MapView{Path: "level.tmx"},
		Canvas:      canvas,
	}
}

// withEverything fills in the other modes' halves of a Data, so one state can
// exercise every mode rather than only the one it was built for.
func withEverything(d Data) Data {
	d.HasSession, d.HasMachines = true, true
	return d
}

func renderRegionsOf(t *testing.T, content Content, data Data) map[string]string {
	t.Helper()
	out := make(map[string]string, len(content.Regions))
	for _, region := range content.Regions {
		out[region.ID] = render(t, region.Render(data))
	}
	return out
}

// The ids are what the stream addresses. A duplicate anywhere in the assembled
// page — a region id that also appears inside another region's markup, or an
// id reused by an unrelated element — means a patch lands on whichever comes
// first in the document.
func TestRegions_IDsAreUniqueInTheAssembledPage(t *testing.T) {
	for slug, content := range Registry {
		for state, data := range modeStates(t) {
			rendered := make([]string, len(content.Regions))
			for i, region := range content.Regions {
				rendered[i] = render(t, region.Render(data))
			}
			page := render(t, content.Page(rendered))
			for _, region := range content.Regions {
				if n := len(idRE(region.ID).FindAllString(page, -1)); n != 1 {
					t.Errorf("%s/%s: id %q appears %d times in the page, want exactly 1",
						slug, state, region.ID, n)
				}
			}
		}
	}
}

// MAP's regions appear top to bottom in the order the column reads: the rail
// beside it, then the toolbar, then the canvas, then the status line.
//
// Named explicitly rather than checked against Registry's own order, which
// sounds like the same thing and is not: MapPage indexes its regions
// positionally, so swapping two entries in Registry swaps both the declaration
// and the slot, and a test comparing one to the other is satisfied by any
// permutation. The canvas rendering below the status line is the failure this
// has to see, and only a stated expectation sees it. Patches replace elements
// in place, so a wrong order would persist for the life of the page.
func TestMapRegions_ReadTopToBottom(t *testing.T) {
	content := Registry["map"]
	want := []string{"mode-list", "map-head", "map-canvas-region", "map-foot"}

	rendered := make([]string, len(content.Regions))
	for i, region := range content.Regions {
		rendered[i] = render(t, region.Render(mapRegionFixture()))
	}
	page := render(t, content.Page(rendered))

	at := -1
	for _, id := range want {
		loc := idRE(id).FindStringIndex(page)
		if loc == nil {
			t.Fatalf("region %q is not in the page", id)
		}
		if loc[0] < at {
			t.Errorf("region %q appears above the one that should precede it", id)
		}
		at = loc[0]
	}
}

// Signals are declared on <main id="mode-content">, which is rendered on a page
// load and is the one element inside the shell the stream never patches — its
// *children* are the regions. Anywhere else and every patch would reset the
// view to whatever the server last thought it was, which is precisely the
// clobbering that keeping view state out of the server is meant to end.
func TestSignals_AreDeclaredForTheModesThatOwnViewState(t *testing.T) {
	got := Registry["map"].Signals(mapRegionFixture())
	for _, want := range []string{"zoom", "tile", "layer", "hide0"} {
		if !strings.Contains(got, want) {
			t.Errorf("MAP declares no %q signal: %s", want, got)
		}
	}
}

// One per layer, seeded from what the file says rather than from nothing: a
// layer the map itself hides must come up hidden, and the eye must still be
// able to turn it on.
func TestSignals_SeedLayerVisibilityFromTheFile(t *testing.T) {
	data := mapRegionFixture()
	data.Canvas.Layers = []mapcanvas.Layer{
		{Index: 0, Name: "ground"},
		{Index: 1, Name: "props", HiddenInFile: true},
	}

	got := Registry["map"].Signals(data)
	if !strings.Contains(got, `"hide0":false`) {
		t.Errorf("a visible layer is not seeded visible: %s", got)
	}
	if !strings.Contains(got, `"hide1":true`) {
		t.Errorf("a layer the file hides is not seeded hidden: %s", got)
	}
}

// A mode with no view state of its own declares none, rather than an empty
// object that would still have to be parsed on every page load.
func TestSignals_AreAbsentWhereAModeHasNoViewState(t *testing.T) {
	for _, slug := range []string{"schema", "ents", "agents", "tiles", "sprites"} {
		if s := Registry[slug].Signals; s != nil && s(Data{}) != "" {
			t.Errorf("%s declares signals it has no use for: %s", slug, s(Data{}))
		}
	}
}

// A boolean attribute is not what any of these want.
//
// Datastar's attr plugin writes a bare attribute for `true` — `aria-pressed=""`
// — and removes it for `false`. That is right for `disabled` and wrong for an
// ARIA state, which is an enumerated string, and wrong for the data attributes
// the browser suite selects on, which are asserted in both states and cannot be
// asserted at all when absent.
func TestMapRegions_StateAttributesAreStringsAndNotBareBooleans(t *testing.T) {
	rendered := renderRegionsOf(t, Registry["map"], mapRegionFixture())
	page := rendered["mode-list"] + rendered["map-head"]

	for _, attr := range []string{"aria-pressed", "aria-current", "data-selected", "data-hidden"} {
		expr := attrExpr(page, attr)
		if expr == "" {
			t.Errorf("nothing sets %s", attr)
			continue
		}
		if !strings.Contains(expr, "'true' : 'false'") {
			t.Errorf("%s is set from %q, which Datastar writes as a bare attribute", attr, expr)
		}
	}
}

// attrExpr is the expression bound to one attribute, or "" if none is.
func attrExpr(html, attr string) string {
	m := regexp.MustCompile(`data-attr:` + regexp.QuoteMeta(attr) + `="([^"]*)"`).FindStringSubmatch(html)
	if m == nil {
		return ""
	}
	return strings.NewReplacer("&#39;", "'", "&#34;", `"`, "&amp;", "&").Replace(m[1])
}

// The key is separated from the plugin by a colon. A dash makes the whole thing
// a plugin name, which Datastar does not recognise and skips in silence — the
// control renders perfectly and does nothing.
//
// Named here because this project has now made the mistake three times:
// data-on-click across every primitive, data-on-load in a plan, and
// data-attr-aria-pressed on ten attributes of this mode.
func TestMapRegions_UseTheColonForm(t *testing.T) {
	rendered := renderRegionsOf(t, Registry["map"], mapRegionFixture())
	for id, html := range rendered {
		for _, bad := range []string{"data-attr-", "data-on-", "data-class-", "data-style-", "data-text-"} {
			if strings.Contains(html, bad) {
				t.Errorf("%s carries %s, which names a plugin nothing registers", id, bad)
			}
		}
	}
}

// A map opens painting into the first layer the file *draws*.
//
// Not simply the first layer: a map whose ground layer is hidden would open
// with the brush pointed at something invisible, so the first stroke would go
// somewhere the person painting cannot see.
func TestMapSignals_OpenPaintingIntoTheFirstLayerTheFileDraws(t *testing.T) {
	data := mapRegionFixture()
	data.Canvas.Layers = []mapcanvas.Layer{
		{Index: 0, Name: "notes", HiddenInFile: true},
		{Index: 1, Name: "ground"},
	}

	if got := Registry["map"].Signals(data); !strings.Contains(got, `"layer":1`) {
		t.Errorf("the page opens painting into a layer the file hides: %s", got)
	}
}

// The tile in hand has to be *visible*, not merely recorded. data-selected and
// aria-pressed are for a test and a screen reader; the class is the only thing
// the person choosing a tile can see.
func TestMapRegions_TheTileInHandIsMarkedVisibly(t *testing.T) {
	rendered := renderRegionsOf(t, Registry["map"], mapRegionFixture())
	if !strings.Contains(rendered["mode-list"], "palette-tile--selected") {
		t.Errorf("nothing applies the selected class to a palette tile:\n%.400s", rendered["mode-list"])
	}
}

// A map reloaded from disk with an extra layer produces a patched canvas that
// refers to a signal the page never declared — and Datastar auto-creates a
// missing signal as the empty string, which is falsy, so the new layer would
// come up drawn even when the .tmx hides it.
//
// The canvas re-declares each layer's signal, seeded from the file, __ifmissing
// so that a patch cannot reset a visibility somebody has since changed by hand.
func TestMapRegions_TheCanvasReseedsALayerThePageNeverDeclared(t *testing.T) {
	data := mapRegionFixture()
	data.Canvas.Layers = []mapcanvas.Layer{
		{Index: 0, Name: "ground"},
		{Index: 1, Name: "secret", HiddenInFile: true},
	}

	canvas := unescaped(renderRegionsOf(t, Registry["map"], data)["map-canvas-region"])

	if !strings.Contains(canvas, "data-signals__ifmissing") {
		t.Fatalf("the canvas does not re-declare its layers' signals:\n%.400s", canvas)
	}
	// Seeded from the file, so a layer the map hides arrives hidden.
	if !strings.Contains(canvas, `{"hide1":true}`) {
		t.Errorf("a layer the file hides is not re-seeded hidden:\n%.400s", canvas)
	}
	if !strings.Contains(canvas, `{"hide0":false}`) {
		t.Errorf("a visible layer is not re-seeded visible:\n%.400s", canvas)
	}
	// __ifmissing, or every push would reset the eye to what the file says.
	if strings.Contains(canvas, `data-signals="`) {
		t.Errorf("the canvas declares signals unconditionally, so a patch resets them:\n%.400s", canvas)
	}
}

// The eye's tooltip is view state, so it has to come from the signal. It was a
// Go function over Layer.Hidden, and when visibility became a signal Build
// stopped setting that field while the function went on reading it — so every
// eye said "drawn", including the ones that were not.
func TestMapRegions_TheEyeTooltipFollowsTheSignal(t *testing.T) {
	data := mapRegionFixture()
	data.Canvas.Layers = []mapcanvas.Layer{{Index: 0, Name: "ground"}}

	list := renderRegionsOf(t, Registry["map"], data)["mode-list"]
	if !strings.Contains(list, "data-attr:title") {
		t.Errorf("the eye's tooltip is fixed at render time:\n%.500s", list)
	}
	if !strings.Contains(list, "$hide0 ?") {
		t.Errorf("the tooltip does not read the visibility signal:\n%.500s", list)
	}
}

// Every control whose text comes from a signal ships that text server-rendered
// too. data-text replaces the contents on hydration, so seeding costs nothing —
// and without it the layer panel is a column of blank boxes and three of six
// status fields are missing until the bundle runs, or forever if it does not.
//
// Matched as element *content*, between the tags. The glyph and the labels also
// appear inside the data-text expression that will replace them, so a plain
// substring search finds the expression and passes on a control that renders
// empty — which is how the first version of this test did exactly that.
func TestMapRegions_SignalDrivenTextIsSeededForFirstPaint(t *testing.T) {
	rendered := renderRegionsOf(t, Registry["map"], mapRegionFixture())

	for _, tc := range []struct{ region, want string }{
		{"mode-list", "👁"},
		{"map-foot", "4× · 64px cells"}, // the fixture is 2x1 at 16px, so it fits at 4x
		{"map-foot", "painting ground"},
		{"map-foot", "no tile selected"},
	} {
		if !hasContent(rendered[tc.region], tc.want) {
			t.Errorf("%s renders empty before hydration — %q is only in an expression:\n%.400s",
				tc.region, tc.want, rendered[tc.region])
		}
	}
}

// hasContent reports whether text appears as an element's contents rather than
// only inside an attribute value.
func hasContent(html, text string) bool {
	return regexp.MustCompile(`>[^<]*` + regexp.QuoteMeta(text)).MatchString(html)
}

// unescaped reads rendered HTML the way a browser does. Datastar expressions and
// the signal JSON live in attributes, so every quote arrives as an entity, and a
// test matching the source form asserts about escaping rather than about the
// expression.
func unescaped(html string) string {
	return strings.NewReplacer("&#34;", `"`, "&#39;", "'", "&amp;", "&").Replace(html)
}

// Layer groups are emitted in file order, because that is what stacks them now.
//
// The cells used to arrive in one flat list in draw order and the template
// emitted them as they came, so stacking needed nothing else. Grouping them per
// layer moved that responsibility onto Canvas.Layers being in file order, and
// nothing said so.
func TestMapRegions_LayerGroupsAreEmittedInFileOrder(t *testing.T) {
	data := mapRegionFixture()
	data.Canvas.Layers = []mapcanvas.Layer{
		{Index: 0, Name: "floor"},
		{Index: 1, Name: "wall"},
	}
	data.Canvas.CellsByLayer = map[int][]mapcanvas.Cell{
		0: {{X: 0, Y: 0, LayerIndex: 0, Layer: "floor", Image: "/t.png", SW: 16, SH: 16, A: 1, D: 1, Alpha: 1}},
		1: {{X: 0, Y: 0, LayerIndex: 1, Layer: "wall", Image: "/t.png", SW: 16, SH: 16, A: 1, D: 1, Alpha: 1}},
	}

	canvas := renderRegionsOf(t, Registry["map"], data)["map-canvas-region"]
	floor := strings.Index(canvas, `data-cell="floor:0,0"`)
	wall := strings.Index(canvas, `data-cell="wall:0,0"`)
	if floor < 0 || wall < 0 {
		t.Fatalf("both layers should be drawn:\n%.500s", canvas)
	}
	if floor > wall {
		t.Error("the wall layer is emitted before the floor, so it stacks underneath")
	}
}
