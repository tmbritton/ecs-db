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

// The point of the exercise, asserted on the mode where it pays: the canvas is
// the great majority of the page, and choosing a tile to paint with does not
// change a single cell of it.
//
// Two things have to be true for this to test anything, and the first version
// of it had neither. The key has to be the region *id* — `map-canvas-region`,
// not `map-canvas`, which is a test id on an element inside it — or both sides
// of the comparison are the empty string. And the fixture has to have cells: a
// Data with HasMaps but no Maps renders every region as an empty div, so the
// canvas, the head and the rail are all byte-identical to each other and the
// comparison passes no matter what the code does.
func TestMapRegions_ChoosingATileLeavesTheCanvasAlone(t *testing.T) {
	content := Registry["map"]

	base := mapRegionFixture()
	withTile := mapRegionFixture()
	withTile.MapView.SelectedGID = 4
	withTile.Canvas.Tilesets[0].Tiles[1].Selected = true

	before := renderRegionsOf(t, content, base)
	after := renderRegionsOf(t, content, withTile)

	// The fixture is only worth anything if the canvas actually drew something.
	if !strings.Contains(before["map-canvas-region"], `data-testid="map-cell"`) {
		t.Fatalf("the fixture renders no cells, so this compares nothing:\n%.300s",
			before["map-canvas-region"])
	}
	if before["map-canvas-region"] != after["map-canvas-region"] {
		t.Error("selecting a tile changed the canvas region")
	}
	// And the selection did land somewhere, or the two Datas are the same Data
	// and the assertion above is vacuous for a different reason.
	if before["mode-list"] == after["mode-list"] {
		t.Error("selecting a tile changed nothing at all — the fixtures are identical")
	}
}

// mapRegionFixture is a map with cells, layers and a palette: enough that every
// MAP region renders its real contents rather than an empty wrapper.
func mapRegionFixture() Data {
	canvas := mapcanvas.Canvas{
		Cols: 2, Rows: 1, TileW: 16, TileH: 16, W: 32, H: 16, Scale: 1,
		Cells: []mapcanvas.Cell{
			{X: 0, Y: 0, Layer: "ground", Image: "/forge/asset?path=t.png", SW: 16, SH: 16, A: 1, D: 1, Alpha: 1},
			{X: 1, Y: 0, Layer: "ground", Image: "/forge/asset?path=t.png", SX: 16, SW: 16, SH: 16, A: 1, D: 1, TX: 16, Alpha: 1},
		},
		Layers: []mapcanvas.Layer{{Index: 0, Name: "ground", Active: true}},
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
