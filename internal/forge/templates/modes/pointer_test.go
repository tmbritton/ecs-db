package modes

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/paint"
)

// The stroke expression is the whole boundary between paint.js and the server:
// the module knows pixels and dispatches an event, and this is what turns that
// event into a request. Every part of it is a silent failure — a Datastar
// expression that names a signal nothing declares reads as undefined and posts
// the string "undefined" as a coordinate.
func TestStrokeAction_SendsTheGestureTheToolMeans(t *testing.T) {
	got := strokeAction(mapRegionFixture())

	if !strings.Contains(got, "$tool === '"+string(paint.Fill)+"'") {
		t.Errorf("the expression does not branch on the tool: %s", got)
	}
	// Fill means the rectangle its two ends span, so it is the branch that sends
	// the far corner and no trail.
	fill, trailed, ok := strings.Cut(got, ":")
	if !ok {
		t.Fatalf("the expression is not a choice between two posts: %s", got)
	}
	for _, want := range []string{"x2=", "y2="} {
		if !strings.Contains(fill, want) {
			t.Errorf("the fill branch does not send %s: %s", want, fill)
		}
	}
	if strings.Contains(fill, "cells=") {
		t.Errorf("the fill branch sends a trail, which it would then ignore: %s", fill)
	}
	if !strings.Contains(trailed, "cells=") {
		t.Errorf("the other branch does not send the trail: %s", trailed)
	}
	if !strings.Contains(got, "/forge/map/paint") {
		t.Errorf("the expression does not post to the paint route: %s", got)
	}
}

// The stroke has to reach the map the page is showing. mapShown defaults an
// empty ?map= to the same map the page resolved, but naming it is what keeps a
// second tab editing another map from taking the stroke.
func TestStrokeAction_NamesTheMapThePageIsShowing(t *testing.T) {
	data := mapRegionFixture()
	data.SelectedMap = "levels/a b.tmx"
	got := strokeAction(data)
	if !strings.Contains(got, "map=levels%2Fa%20b.tmx") {
		t.Errorf("the map is not named, or not escaped: %s", got)
	}
}

// The detail keys are the contract with paint.js. strokeResult returns exactly
// these four, and a rename on either side is a request full of "undefined".
func TestStrokeAction_ReadsTheKeysTheModuleDispatches(t *testing.T) {
	got := strokeAction(mapRegionFixture())
	for _, key := range []string{"evt.detail.x", "evt.detail.y", "evt.detail.x2", "evt.detail.y2", "evt.detail.cells"} {
		if !strings.Contains(got, key) {
			t.Errorf("the expression never reads %s: %s", key, got)
		}
	}
}

// The shortcuts are the prototype's tooltips. Each has to set the signal the
// toolbar reads, or the button lights up and the stroke goes out with the old
// tool.
func TestShortcutAction_SetsTheSignalsTheToolbarShows(t *testing.T) {
	got := shortcutAction()
	for _, want := range []string{
		"'b'", "'r'", "'e'", "'q'", "'x'", "'g'",
		"$tool = 'stamp'", "$tool = 'fill'", "$tool = 'erase'",
		"$flipH = !$flipH", "$grid = !$grid",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the shortcuts do not carry %q: %s", want, got)
		}
	}
	// Q turns the stamp, and by the same tables the button does rather than a
	// second copy of the cycle that could drift from it.
	d, _, _ := turnTables()
	if !strings.Contains(got, d) {
		t.Errorf("Q does not turn the stamp with the same table the button uses: %s", got)
	}
}

// A shortcut that fires while someone is typing eats the letter and changes the
// tool. Every text surface in Forge is one of these.
func TestShortcutAction_DoesNothingWhileTyping(t *testing.T) {
	got := shortcutAction()
	for _, want := range []string{"INPUT", "TEXTAREA", "SELECT", "isContentEditable"} {
		if !strings.Contains(got, want) {
			t.Errorf("the shortcuts fire inside %s: %s", want, got)
		}
	}
	// And a modified key is a browser or OS command — ctrl+E is the address bar
	// on some platforms, cmd+R is reload.
	for _, want := range []string{"evt.ctrlKey", "evt.metaKey", "evt.altKey"} {
		if !strings.Contains(got, want) {
			t.Errorf("the shortcuts do not ignore %s: %s", want, got)
		}
	}
}

// The regex characters a Datastar expression must not contain: it rewrites
// $name and @name( inside the expression text, and a literal that happened to
// hold either would be rewritten with it.
func TestShortcutAction_HasNothingDatastarWillRewrite(t *testing.T) {
	got := shortcutAction()
	if strings.Contains(got, "/^") || strings.Contains(got, "$/") {
		t.Errorf("the shortcuts use a regexp literal, which Datastar's $ pass reaches into: %s", got)
	}
}

// The ghost says what a stroke would do, which is the tool as well as the
// orientation: "stamp 90°" over an eraser is a lie about what the click does.
func TestGhostNote_SaysWhatTheStrokeWouldDo(t *testing.T) {
	got := ghostNoteExpr()
	if !strings.Contains(got, "$tool") {
		t.Errorf("the ghost's note does not read the tool: %s", got)
	}
	if !strings.Contains(got, turnLabels()) {
		t.Errorf("the ghost's note does not name the orientation: %s", got)
	}
}

// The grid is view state like every other control on this page, so it is a
// signal and the seed declares it. Without the declaration Datastar creates it
// on first read as the empty string, and the grid comes up off.
func TestMapSignals_DeclareTheGrid(t *testing.T) {
	declared := MapSignals(mapRegionFixture())
	if !strings.Contains(declared, `"grid":true`) {
		t.Errorf("MapSignals does not start the grid on: %s", declared)
	}
}

// The marquee is the fill tool's preview and only its preview. The stamp and
// the eraser follow the pointer, and a rectangle drawn round a curved drag
// promises cells the stroke will not paint.
func TestMarqueeClass_IsShownForTheToolThatMeansARectangle(t *testing.T) {
	got := marqueeClass()
	if !strings.Contains(got, "$tool === '"+string(paint.Fill)+"'") {
		t.Errorf("the marquee is not tied to the fill tool: %s", got)
	}
	for _, other := range []string{string(paint.Stamp), string(paint.Erase)} {
		if strings.Contains(got, "'"+other+"'") {
			t.Errorf("the marquee is shown for %q, which does not paint a rectangle: %s", other, got)
		}
	}
}

// The rendered half of the pointer surface. Every one of these is a silent
// failure if it goes missing: paint.js dispatches into nothing, the ghost never
// appears, the marquee never shows. None of it produces an error anywhere.
func TestMapCanvas_CarriesThePointerSurface(t *testing.T) {
	rendered := renderRegionsOf(t, Registry["map"], mapRegionFixture())
	canvas := unescaped(rendered["map-canvas-region"])

	for _, tc := range []struct{ want, why string }{
		{"data-on:paintstroke", "paint.js dispatches this and nothing else listens for it"},
		{"data-attr:data-cell-w", "paint.js reads the drawn cell size from here"},
		{"data-attr:data-cell-h", "paint.js reads the drawn cell size from here"},
		{`data-testid="map-ghost"`, "the ghost is what says where a click would land"},
		{`data-testid="map-ghost-note"`, "the note is what says what the click would do"},
		{`data-testid="map-marquee"`, "the marquee is the fill tool's preview"},
		{"map-marquee--on", "without the class the marquee never shows"},
		{"map-canvas__grid--off", "without the class the grid can never be turned off"},
	} {
		if !strings.Contains(canvas, tc.want) {
			t.Errorf("the canvas does not carry %s — %s:\n%.500s", tc.want, tc.why, canvas)
		}
	}

	// The ghost and the marquee sit outside the scaled box, with the grid. A 2px
	// dashed border inside it is 16px at 8x, which reads as a rendering fault.
	scaled := strings.Index(canvas, "map-canvas__scaled")
	for _, name := range []string{"map-ghost", "map-marquee"} {
		if at := strings.Index(canvas, `class="`+name+`"`); at == -1 || at > scaled {
			t.Errorf("%s is inside the scaled box, so its border scales with the zoom", name)
		}
	}
}

// The toolbar's half: the grid control, and the shortcuts bound where a key
// press can reach them without the canvas having been clicked first.
func TestMapToolbar_CarriesTheGridAndTheShortcuts(t *testing.T) {
	head := unescaped(renderRegionsOf(t, Registry["map"], mapRegionFixture())["map-head"])
	for _, want := range []string{`data-testid="grid-toggle"`, "data-on:keydown__window", "$grid"} {
		if !strings.Contains(head, want) {
			t.Errorf("the toolbar does not carry %s:\n%.500s", want, head)
		}
	}
}

// A map that could not be drawn has no tools, so the shortcuts have nothing to
// change. Binding them anyway would leave B and E doing something invisible on
// a page whose whole content is an explanation of why there is no canvas.
func TestMapToolbar_BindsNoShortcutsWithoutACanvas(t *testing.T) {
	data := mapRegionFixture()
	data.Canvas.TileW, data.Canvas.TileH, data.Canvas.Cols, data.Canvas.Rows = 0, 0, 0, 0
	data.Canvas.Cells, data.Canvas.CellsByLayer = nil, nil
	if data.Canvas.Drawn() {
		t.Fatal("the fixture still draws, so this proves nothing")
	}
	head := unescaped(renderRegionsOf(t, Registry["map"], data)["map-head"])
	if strings.Contains(head, "data-on:keydown__window") {
		t.Errorf("the shortcuts are bound on a map with no tools:\n%.500s", head)
	}
}

// The keys are compared in lower case. Without it every shortcut stops working
// the moment caps lock is on or shift is held, which reads as the toolbar
// having quietly broken rather than as a key being the wrong case.
func TestShortcutAction_IgnoresTheCaseOfTheKey(t *testing.T) {
	if !strings.Contains(shortcutAction(), "toLowerCase") {
		t.Errorf("the shortcuts compare the key as it arrives: %s", shortcutAction())
	}
}

// The eye's tooltip has to say what clicking would do, which is the opposite of
// what it is doing now. One sentence for both states is a tooltip that tells
// you nothing you could not already see.
func TestGridTitle_SaysWhatTheClickWouldDo(t *testing.T) {
	got := gridTitleExpr()
	if !strings.Contains(got, "$grid") {
		t.Errorf("the grid's tooltip does not depend on the grid: %s", got)
	}
	if !strings.Contains(got, "'hide the grid'") || !strings.Contains(got, "'show the grid'") {
		t.Errorf("the grid's tooltip does not name both states: %s", got)
	}
	// And the right way round: it names the action, not the state.
	hide, show := strings.Index(got, "'hide the grid'"), strings.Index(got, "'show the grid'")
	if hide > show {
		t.Errorf("the tooltip offers to show the grid while it is drawn: %s", got)
	}
}
