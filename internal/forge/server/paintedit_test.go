package server

import (
	"encoding/json"
	"html"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

// paintSignals is what the page would send: a tool, a tile in hand, a layer.
func held(tool string, gid int, extra ...string) string {
	s := `{"tool":"` + tool + `","tile":` + itoa(gid) + `,"layer":0`
	for _, e := range extra {
		s += "," + e
	}
	return s + "}"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	out, neg := "", n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	if neg {
		return "-" + out
	}
	return out
}

func layerData(t *testing.T, s *Server, path string) []uint32 {
	t.Helper()
	var out []uint32
	if err := s.cfg.MapSession.Read(path, func(d *tiled.Document) error {
		m, err := d.Map()
		if err != nil {
			return err
		}
		out = append(out, m.Layers[0].Data...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPaint_StampsTheTileInHand(t *testing.T) {
	srv, s, sess, configured := mapServer(t)
	before := layerData(t, s, configured)
	// A tile the cell does not already hold, or this passes by painting what
	// was there and calling the no-op a success. The fixture's first cell is 2.
	if before[0] != 2 {
		t.Fatalf("the fixture changed: cell 0 holds %d", before[0])
	}

	code := postSignals(t, srv, "/forge/map/paint?map="+url.QueryEscape(configured)+"&x=0&y=0",
		held("stamp", 1))
	if code != 204 {
		t.Fatalf("paint = %d (%s)", code, problemOf(s))
	}

	after := layerData(t, s, configured)
	if after[0] != 1 {
		t.Errorf("cell 0 is %d, want the tile in hand: %v", after[0], after)
	}
	if after[1] != before[1] || after[2] != before[2] || after[3] != before[3] {
		t.Errorf("a click changed more than its own cell: %v -> %v", before, after)
	}
	// And it is an edit: the map is unsaved until somebody saves it.
	if dirty, _ := sess.Dirty(); len(dirty) != 1 {
		t.Errorf("painting did not mark the map unsaved: %v", dirty)
	}
}

// A drag arrives as two corners and is one request, not one per cell.
func TestPaint_FillsARectangleInOneRequest(t *testing.T) {
	srv, s, _, configured := mapServer(t)

	code := postSignals(t, srv, "/forge/map/paint?map="+url.QueryEscape(configured)+"&x=0&y=0&x2=1&y2=1",
		held("fill", 2))
	if code != 204 {
		t.Fatalf("fill = %d (%s)", code, problemOf(s))
	}

	for i, gid := range layerData(t, s, configured) {
		if gid != 2 {
			t.Errorf("cell %d is %d, want the whole 2x2 filled", i, gid)
		}
	}
}

// Gid 0 is a cell with nothing in it. The tile in hand is ignored.
func TestPaint_ErasesToAnEmptyCell(t *testing.T) {
	srv, s, _, configured := mapServer(t)

	code := postSignals(t, srv, "/forge/map/paint?map="+url.QueryEscape(configured)+"&x=0&y=0",
		held("erase", 2))
	if code != 204 {
		t.Fatalf("erase = %d (%s)", code, problemOf(s))
	}
	if got := layerData(t, s, configured)[0]; got != 0 {
		t.Errorf("the erased cell is %d, want 0", got)
	}
}

// The flags travel with the tile, so a rotated stamp writes a rotated cell.
func TestPaint_CarriesTheStampsRotation(t *testing.T) {
	srv, s, _, configured := mapServer(t)

	code := postSignals(t, srv, "/forge/map/paint?map="+url.QueryEscape(configured)+"&x=0&y=0",
		held("stamp", 2, `"flipD":true`, `"flipH":true`))
	if code != 204 {
		t.Fatalf("paint = %d (%s)", code, problemOf(s))
	}

	got := tiled.TileOf(layerData(t, s, configured)[0])
	if got.GID != 2 || !got.FlipD || !got.FlipH {
		t.Errorf("the cell holds %+v, want gid 2 turned a quarter", got)
	}
}

// Painting what is already there is a legal stroke that changed nothing. It must
// not dirty the map, or the footer stops meaning anything.
func TestPaint_PaintingWhatIsAlreadyThereChangesNothing(t *testing.T) {
	srv, s, sess, configured := mapServer(t)
	before := layerData(t, s, configured)

	code := postSignals(t, srv, "/forge/map/paint?map="+url.QueryEscape(configured)+"&x=0&y=0",
		held("stamp", int(before[0])))
	if code != 204 {
		t.Fatalf("paint = %d (%s)", code, problemOf(s))
	}
	if dirty, _ := sess.Dirty(); len(dirty) != 0 {
		t.Errorf("painting the tile that was already there marked the map unsaved: %v", dirty)
	}
	if problemOf(s) != "" {
		t.Errorf("a stroke that changed nothing was reported as a problem: %s", problemOf(s))
	}
}

func TestPaint_RefusesWithAReason(t *testing.T) {
	for _, tc := range []struct{ name, query, signals, want string }{
		{"a cell off the map", "&x=99&y=0", held("stamp", 2), "outside"},
		{"a layer that is not there", "&x=0&y=0", `{"tool":"stamp","tile":2,"layer":9}`, "no layer"},
		{"a layer the view is hiding", "&x=0&y=0", `{"tool":"stamp","tile":2,"layer":0,"hide0":true}`, "not drawing"},
		{"a tool nobody has written", "&x=0&y=0", held("scribble", 2), "not something"},
		{"nothing in hand", "&x=0&y=0", held("stamp", 0), "no tile in hand"},
		{"a cell that is not a number", "&x=here&y=0", held("stamp", 2), "which cell"},
		{"a stroke with no position", "", held("stamp", 2), "does not say where"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, s, sess, configured := mapServer(t)

			code := postSignals(t, srv, "/forge/map/paint?map="+url.QueryEscape(configured)+tc.query, tc.signals)
			if code != 204 {
				t.Fatalf("status = %d", code)
			}
			if got := problemOf(s); !strings.Contains(got, tc.want) {
				t.Errorf("refused with %q, which does not say %q", got, tc.want)
			}
			// Refused means nothing was written.
			if dirty, _ := sess.Dirty(); len(dirty) != 0 {
				t.Errorf("a refused stroke still changed the map: %v", dirty)
			}
		})
	}
}

// A page with no ?map= is showing the first map, and a stroke from it must land
// there rather than being refused. The two resolutions have to agree; if they
// ever stop agreeing, a stroke edits a file nobody was looking at.
func TestPaint_LandsOnTheMapThePageIsShowing(t *testing.T) {
	srv, s, _, _ := mapServer(t)
	shown := selectMap(s.cfg.MapSession.Maps(), "")

	code := postSignals(t, srv, "/forge/map/paint?x=0&y=0", held("stamp", 1))
	if code != 204 {
		t.Fatalf("paint = %d (%s)", code, problemOf(s))
	}
	if got := layerData(t, s, shown)[0]; got != 1 {
		t.Errorf("the stroke did not reach %s: cell 0 is %d", shown, got)
	}
}

// A named map that is not open is a typo, not a default. Falling back would
// silently edit the wrong file.
func TestPaint_RefusesAMapThatIsNotOpen(t *testing.T) {
	srv, s, sess, _ := mapServer(t)

	code := postSignals(t, srv, "/forge/map/paint?map=nowhere.tmx&x=0&y=0", held("stamp", 1))
	if code != 204 {
		t.Fatalf("status = %d", code)
	}
	if got := problemOf(s); !strings.Contains(got, "nowhere.tmx") {
		t.Errorf("refused with %q, which does not name the map that was asked for", got)
	}
	if dirty, _ := sess.Dirty(); len(dirty) != 0 {
		t.Errorf("a stroke naming an unknown map edited something anyway: %v", dirty)
	}
}

// What Forge paints has to survive the trip to disk and back through the
// engine's own parser. The flag bits are the fragile part: Raw packs them into
// the high bits of a gid, the CSV writer prints that as one large unsigned
// number, and a signed round-trip anywhere in between turns a rotated tile into
// a negative gid the engine reads as empty.
func TestPaint_WhatIsSavedIsWhatTheEngineReads(t *testing.T) {
	srv, s, sess, configured := mapServer(t)

	code := postSignals(t, srv, "/forge/map/paint?map="+url.QueryEscape(configured)+"&x=0&y=0",
		held("stamp", 2, `"flipD":true`, `"flipH":true`))
	if code != 204 {
		t.Fatalf("paint = %d (%s)", code, problemOf(s))
	}
	if got := post(t, srv, "/forge/map/save?map="+url.QueryEscape(configured)); got != 204 {
		t.Fatalf("save = %d (%s)", got, problemOf(s))
	}
	if dirty, _ := sess.Dirty(); len(dirty) != 0 {
		t.Fatalf("the map is still unsaved after a save: %v", dirty)
	}

	raw, err := os.ReadFile(configured)
	if err != nil {
		t.Fatal(err)
	}
	m, err := tiled.Parse(raw, configured)
	if err != nil {
		t.Fatalf("the engine's parser could not read what Forge saved: %v", err)
	}
	got := tiled.TileOf(m.Layers[0].Data[0])
	if got.GID != 2 || !got.FlipD || !got.FlipH || got.FlipV {
		t.Errorf("the engine reads %+v, not the quarter-turned tile that was painted", got)
	}
}

// A click sends one corner, and the route fills the other in. Defaulting it to
// the zero value instead would paint a rectangle from (0,0) to wherever the
// click was — which is invisible when the click is at the origin, so this
// deliberately clicks somewhere else.
func TestPaint_AClickPaintsOneCellWhereverItLands(t *testing.T) {
	srv, s, _, configured := mapServer(t)
	before := layerData(t, s, configured)

	code := postSignals(t, srv, "/forge/map/paint?map="+url.QueryEscape(configured)+"&x=1&y=1",
		held("stamp", 1))
	if code != 204 {
		t.Fatalf("paint = %d (%s)", code, problemOf(s))
	}

	after := layerData(t, s, configured)
	if after[3] != 1 {
		t.Errorf("the clicked cell is %d, want the tile in hand: %v", after[3], after)
	}
	for i := range 3 {
		if after[i] != before[i] {
			t.Errorf("a click at (1,1) also changed cell %d: %v -> %v", i, before, after)
		}
	}
}

// A refusal stays on screen until something goes right. Leaving the last one
// standing over a stroke that worked is how a reader learns to ignore it.
func TestPaint_ASuccessfulStrokeClearsTheLastRefusal(t *testing.T) {
	srv, s, _, configured := mapServer(t)

	if code := postSignals(t, srv, "/forge/map/paint?map="+url.QueryEscape(configured)+"&x=99&y=0",
		held("stamp", 1)); code != 204 {
		t.Fatalf("status = %d", code)
	}
	if problemOf(s) == "" {
		t.Fatal("the refused stroke reported nothing, so this proves nothing")
	}

	if code := postSignals(t, srv, "/forge/map/paint?map="+url.QueryEscape(configured)+"&x=0&y=0",
		held("stamp", 1)); code != 204 {
		t.Fatalf("status = %d", code)
	}
	if got := problemOf(s); got != "" {
		t.Errorf("the refusal %q is still on screen after a stroke that worked", got)
	}
}

// The visibility signals are named for their layer, so a map with ten or more
// layers is where a prefix match on a fixed width would go wrong.
func TestPaintSignals_ReadTheRightLayersVisibility(t *testing.T) {
	sig := signalsFrom(map[string]any{
		"tool": "stamp", "tile": float64(3), "layer": float64(11),
		modes.HideSignal(1): true, modes.HideSignal(11): true, modes.HideSignal(2): false,
	})
	if sig.Layer != 11 {
		t.Fatalf("layer = %d", sig.Layer)
	}
	if !sig.Hidden[modes.HideSignal(11)] {
		t.Errorf("layer 11's own visibility was not read: %v", sig.Hidden)
	}
	if sig.Hidden[modes.HideSignal(2)] {
		t.Errorf("a visible layer read as hidden: %v", sig.Hidden)
	}
}

// The seam the two halves share: whatever the page seeds for a layer's
// visibility is the key the route looks up. Pinned across the boundary, because
// a disagreement here does not fail — it returns the zero value, the hidden
// refusal stops happening, and strokes land on a layer nobody can see.
func TestPaint_ReadsTheVisibilitySignalThePageActuallySeeds(t *testing.T) {
	srv, s, _, configured := mapServer(t)

	// The signals as the browser really receives them, read off the page.
	_, page := get(t, srv, "/forge/map?map="+url.QueryEscape(configured))
	seeded := seededSignals(t, page)

	// Hide layer 0 by the name the page uses, and the stroke must be refused.
	code := postSignals(t, srv, "/forge/map/paint?map="+url.QueryEscape(configured)+"&x=0&y=0",
		`{"tool":"stamp","tile":1,"layer":0,"`+modes.HideSignal(0)+`":true}`)
	if code != 204 {
		t.Fatalf("status = %d", code)
	}
	if got := problemOf(s); !strings.Contains(got, "not drawing") {
		t.Fatalf("a stroke on a hidden layer was not refused: %q", got)
	}
	// And that name is one the page really seeds, rather than one only this
	// test and the route agree on.
	if _, ok := seeded[modes.HideSignal(0)]; !ok {
		t.Errorf("the page seeds %v, which does not include %q",
			keysOf(seeded), modes.HideSignal(0))
	}
}

// seededSignals pulls the data-signals attribute off the rendered page.
func seededSignals(t *testing.T, page string) map[string]any {
	t.Helper()
	m := regexp.MustCompile(`data-signals="([^"]*)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("the page declares no signals at all")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &out); err != nil {
		t.Fatalf("the page's signals are not JSON: %v (%s)", err, m[1])
	}
	return out
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
