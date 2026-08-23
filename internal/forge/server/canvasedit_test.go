package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
)

// canvasMachine is what the canvas handlers edit: two states, a transition
// between them, and a compound state to reach into.
const canvasMachine = `{
  "id": "wander",
  "initial": "idle",
  "states": {
    "idle": {
      "meta": { "notes": "hand written", "forge": { "x": 40, "y": 40 } },
      "on": { "GO": [{ "target": "moving" }] }
    },
    "moving": {},
    "combat": { "initial": "attacking", "states": { "attacking": {} } }
  }
}
`

func TestCanvasMove_AddsTheDeltaToTheRecordedPosition(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	q := "&machine=" + url.QueryEscape(machine)

	if code := post(t, srv, "/forge/agents/state?move=idle&dx=25&dy=-10"+q); code != 204 {
		t.Fatalf("move: %d (%s)", code, s.lastEditProblem())
	}
	def, _ := s.cfg.MachineSession.Working(machine)
	meta := string(def.States["idle"].Extra["meta"])
	// 40+25, 40-10 — the delta applied to what the file recorded, not to where
	// the box happened to be drawn.
	if !strings.Contains(meta, `"x":65`) || !strings.Contains(meta, `"y":30`) {
		t.Errorf("meta = %s, want x 65 and y 30", meta)
	}
	if !strings.Contains(meta, "hand written") {
		t.Errorf("the user's own meta was destroyed: %s", meta)
	}
}

// A state the fallback grid placed has a recorded position too, so its first
// drag moves it from where it looks like it is rather than from the origin.
// A nested state is the only case where the recorded position, the rendered one
// and the absolute one all differ — for a top-level state in this fixture they
// are the same number, so a handler using any of the three passes.
func TestCanvasMove_MovesANestedStateFromWhereTheFileSaysItIs(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	q := "&machine=" + url.QueryEscape(machine)

	def, _ := s.cfg.MachineSession.Working(machine)
	before, ok := findNodeIn(chart.Build(def, "").Nodes, "combat.attacking")
	if !ok {
		t.Fatal("no nested state")
	}
	if before.RecordedX == before.AbsX || before.RecordedX == before.X {
		t.Fatalf("the fixture does not distinguish the three: recorded %v, relative %v, absolute %v",
			before.RecordedX, before.X, before.AbsX)
	}

	if code := post(t, srv, "/forge/agents/state?move=combat.attacking&dx=7&dy=3"+q); code != 204 {
		t.Fatalf("move: %d (%s)", code, s.lastEditProblem())
	}
	def, _ = s.cfg.MachineSession.Working(machine)
	after, _ := findNodeIn(chart.Build(def, "").Nodes, "combat.attacking")
	if after.RecordedX != before.RecordedX+7 || after.RecordedY != before.RecordedY+3 {
		t.Errorf("recorded (%v,%v) → (%v,%v), want +7,+3",
			before.RecordedX, before.RecordedY, after.RecordedX, after.RecordedY)
	}
	// And it moved by the same amount inside the state that holds it, which is
	// what the user did. Not on the canvas as a whole: a compound state grows
	// to hold its children, so dragging one right widens the box around it and
	// the fallback grid moves that box too.
	if after.X != before.X+7 || after.Y != before.Y+3 {
		t.Errorf("the box moved by (%v,%v) inside its parent, want (7,3)",
			after.X-before.X, after.Y-before.Y)
	}
}

func TestCanvasMove_RefusesACoordinateThatIsNotOne(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	q := "&machine=" + url.QueryEscape(machine)

	for _, bad := range []string{"dx=east&dy=1", "dx=1&dy=", "dx=&dy=1"} {
		s.setEditProblem("")
		if code := post(t, srv, "/forge/agents/state?move=idle&"+bad+q); code != 204 {
			t.Fatalf("unexpected status %d", code)
		}
		if s.lastEditProblem() == "" {
			t.Errorf("%q was accepted as a position", bad)
		}
	}
}

func TestCanvasAdd_PutsTheStateWhereThePointerWas(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	q := "&machine=" + url.QueryEscape(machine)

	if code := post(t, srv, "/forge/agents/state?add=1&x=300&y=120"+q); code != 204 {
		t.Fatalf("add: %d (%s)", code, s.lastEditProblem())
	}
	def, _ := s.cfg.MachineSession.Working(machine)
	added := addedState(t, def, "idle", "moving", "combat")
	if got := string(def.States[added].Extra["meta"]); !strings.Contains(got, `"x":300`) {
		t.Errorf("the new state is not where it was dropped: %s", got)
	}
}

// The canvas shifts everything when something would fall off the top or left of
// it, and a pointer coordinate is in the shifted space. Without taking the
// shift back off, every state added to a machine with a self-transition on its
// top row lands further down the canvas than the pointer was.
//
// This needs a machine that is actually shifted: the ordinary fixture has no
// offset, so the subtraction could be deleted and nothing noticed.
func TestCanvasAdd_TakesTheCanvasesOwnShiftBackOff(t *testing.T) {
	const shifted = `{
	  "id": "wander", "initial": "idle",
	  "states": {
	    "idle": { "meta": { "forge": { "x": 0, "y": 0 } }, "on": { "SELF": [{ "target": "idle" }] } }
	  }
	}`
	srv, s, core := machineServerWith(t, shifted)
	machine := filepath.Join(core, "wander.json")

	def, _ := s.cfg.MachineSession.Working(machine)
	built := chart.Build(def, "")
	if built.OffsetY == 0 {
		t.Fatal("this fixture is meant to be shifted; check what it is asserting")
	}

	if code := post(t, srv, "/forge/agents/state?add=1&x=200&y=200&machine="+url.QueryEscape(machine)); code != 204 {
		t.Fatalf("add: %d (%s)", code, s.lastEditProblem())
	}
	def, _ = s.cfg.MachineSession.Working(machine)
	added := addedState(t, def, "idle")

	var got struct{ X, Y float64 }
	var meta struct {
		Forge json.RawMessage `json:"forge"`
	}
	if err := json.Unmarshal(def.States[added].Extra["meta"], &meta); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(meta.Forge, &got); err != nil {
		t.Fatal(err)
	}
	if got.Y != 200-built.OffsetY {
		t.Errorf("recorded y = %v, want %v — the canvas's own shift is still in it",
			got.Y, 200-built.OffsetY)
	}
	// And the state comes back out where the pointer was.
	after, _ := findNodeIn(chart.Build(def, "").Nodes, added)
	if after.Y != 200 {
		t.Errorf("the new state is drawn at y=%v, but was dropped at 200", after.Y)
	}
}

func addedState(t *testing.T, def *agent.MachineDefinition, existing ...string) string {
	t.Helper()
	known := map[string]bool{}
	for _, name := range existing {
		known[name] = true
	}
	var added []string
	for _, name := range def.StateOrder {
		if !known[name] {
			added = append(added, name)
		}
	}
	if len(added) != 1 {
		t.Fatalf("expected one new state, got %v", added)
	}
	return added[0]
}

func findNodeIn(nodes []chart.Node, path string) (chart.Node, bool) {
	for _, n := range nodes {
		if n.Path == path {
			return n, true
		}
		if got, ok := findNodeIn(n.Children, path); ok {
			return got, true
		}
	}
	return chart.Node{}, false
}

func TestCanvasConnect_CreatesATransitionAndNamesItsEvent(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	q := "&machine=" + url.QueryEscape(machine)

	if code := post(t, srv, "/forge/agents/transition?connect=moving&to=combat.attacking"+q); code != 204 {
		t.Fatalf("connect: %d (%s)", code, s.lastEditProblem())
	}
	def, _ := s.cfg.MachineSession.Working(machine)
	if len(def.States["moving"].On) != 1 {
		t.Fatalf("moving.on = %+v", def.States["moving"].On)
	}
	for event, list := range def.States["moving"].On {
		if event == "GO" {
			t.Errorf("the new transition reuses %q, which already means something else", event)
		}
		if _, resolved := agent.FindState(def, list[0].Target); resolved != "combat.attacking" {
			t.Errorf("target %q resolves to %q", list[0].Target, resolved)
		}
	}
}

func TestCanvasDisconnect_RemovesTheTransitionNamed(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	q := "&machine=" + url.QueryEscape(machine)

	if code := post(t, srv, "/forge/agents/transition?delete=idle&kind=on&event=GO&index=0"+q); code != 204 {
		t.Fatalf("delete: %d (%s)", code, s.lastEditProblem())
	}
	def, _ := s.cfg.MachineSession.Working(machine)
	if _, ok := def.States["idle"].On["GO"]; ok {
		t.Error("the transition is still there")
	}
}

func TestCanvasDisconnect_RefusesAnIndexThatIsNotANumber(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	q := "&machine=" + url.QueryEscape(machine)

	s.setEditProblem("")
	if code := post(t, srv, "/forge/agents/transition?delete=idle&kind=on&event=GO&index=first"+q); code != 204 {
		t.Fatalf("unexpected status %d", code)
	}
	if s.lastEditProblem() == "" {
		t.Error("a non-numeric index was accepted")
	}
}

// The parameter names a file, and a handler that passed it through would let a
// crafted request address any path on disk — the check every machine handler
// already makes, and one the canvas's own must not skip.
func TestCanvasEdits_RefuseAMachineThisProjectDoesNotHaveOpen(t *testing.T) {
	s, srv, _ := canvasFixture(t)
	for _, target := range []string{
		"/forge/agents/state?move=idle&dx=1&dy=1&machine=/etc/passwd",
		"/forge/agents/state?add=1&x=1&y=1&machine=/etc/passwd",
		"/forge/agents/transition?connect=idle&to=moving&machine=/etc/passwd",
		"/forge/agents/transition?delete=idle&kind=on&event=GO&index=0&machine=/etc/passwd",
	} {
		s.setEditProblem("")
		if code := post(t, srv, target); code != 204 {
			t.Fatalf("unexpected status %d for %s", code, target)
		}
		if !strings.Contains(s.lastEditProblem(), "not a machine this project has open") {
			t.Errorf("%s was not refused: %q", target, s.lastEditProblem())
		}
	}
}

func TestCanvasMenu_OpensOverWhatWasClickedAndClosesOnTheNextEdit(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	q := "&machine=" + url.QueryEscape(machine)

	if code := post(t, srv, "/forge/agents/menu?target=state&state=idle&x=120&y=80"+q); code != 204 {
		t.Fatalf("menu: %d", code)
	}
	m := s.openCanvasMenu()
	if !m.Open || m.Kind != "state" || m.State != "idle" || m.X != 120 || m.Y != 80 {
		t.Fatalf("menu = %+v", m)
	}

	// An edit closes it: a menu left open over a machine that has just changed
	// describes something that may no longer be there.
	if code := post(t, srv, "/forge/agents/state?initial=moving"+q); code != 204 {
		t.Fatalf("set initial: %d (%s)", code, s.lastEditProblem())
	}
	if s.openCanvasMenu().Open {
		t.Error("the menu is still open over a machine that has changed")
	}
}

func TestCanvasMenu_ClosesOnRequest(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	post(t, srv, "/forge/agents/menu?target=state&state=idle&x=1&y=1&machine="+url.QueryEscape(machine))
	if code := post(t, srv, "/forge/agents/menu?close=1"); code != 204 {
		t.Fatalf("close: %d", code)
	}
	if s.openCanvasMenu().Open {
		t.Error("the menu did not close")
	}
}

// Named rather than counted: "2 transitions will dangle" does not tell you
// whether the two that matter are among them.
func TestCanvasDeleteWarning_NamesWhatWouldDangle(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	q := "&machine=" + url.QueryEscape(machine)

	post(t, srv, "/forge/agents/menu?target=state&state=moving&x=1&y=1"+q)
	got := s.canvasDeleteWarning(machine, s.openCanvasMenu())
	if !strings.Contains(got, "GO") || !strings.Contains(got, "idle") {
		t.Errorf("the warning does not name the transition that would dangle: %q", got)
	}

	post(t, srv, "/forge/agents/menu?target=state&state=combat.attacking&x=1&y=1"+q)
	if got := s.canvasDeleteWarning(machine, s.openCanvasMenu()); !strings.Contains(got, "Nothing transitions into it") {
		t.Errorf("a state nothing targets should say so: %q", got)
	}
	// And nothing is computed for a menu that is not about a state, or for one
	// that is not open — it is a walk of the machine on every render otherwise.
	if got := s.canvasDeleteWarning(machine, CanvasMenu{}); got != "" {
		t.Errorf("a closed menu computed a warning: %q", got)
	}
}

// Saving is the footer's job. A canvas that wrote on every drag would make
// Discard a lie, and would hot-swap a half-finished machine into a running game.
func TestCanvasEdits_DoNotTouchTheFile(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	q := "&machine=" + url.QueryEscape(machine)

	before, err := os.ReadFile(machine)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"/forge/agents/state?move=idle&dx=5&dy=5",
		"/forge/agents/state?add=1&x=9&y=9",
		"/forge/agents/transition?connect=moving&to=idle",
		"/forge/agents/state?initial=moving",
	} {
		if code := post(t, srv, target+q); code != 204 {
			t.Fatalf("%s: %d (%s)", target, code, s.lastEditProblem())
		}
	}
	after, err := os.ReadFile(machine)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("a canvas edit wrote to the file")
	}
	if dirty, err := s.cfg.MachineSession.Dirty(); err != nil || len(dirty) != 1 {
		t.Errorf("the session does not report the machine as unsaved: %v %v", dirty, err)
	}
}

// canvasFixture is a project with one machine the canvas can edit, handing back
// the server, its test HTTP server and the machine's path.
func canvasFixture(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	srv, s, core := machineServerWith(t, canvasMachine)
	return s, srv, filepath.Join(core, "wander.json")
}

// The menu's backdrop covers the viewport, so that clicking anywhere closes it.
// Combined with the menu being per-server, one left open somewhere else would
// block this page's rail, list and menu bar the moment it loaded — the same
// reason a refused edit does not follow a page it never happened on.
func TestCanvasMenu_DoesNotFollowAPageLoad(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	post(t, srv, "/forge/agents/menu?target=state&state=idle&x=1&y=1&machine="+url.QueryEscape(machine))
	if !s.openCanvasMenu().Open {
		t.Fatal("the menu did not open")
	}

	if _, body := get(t, srv, "/forge/agents"); strings.Contains(body, `data-testid="canvas-menu-backdrop"`) {
		t.Error("a freshly loaded page is covered by a backdrop it never asked for")
	}
	if s.openCanvasMenu().Open {
		t.Error("the menu survived a page load")
	}
}

// rename and delete had no handler-level test at all: a typo in either case arm
// falls through to "no state operation named" and only a browser would notice.
func TestCanvasState_RenameAndDeleteReachTheSession(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	q := "&machine=" + url.QueryEscape(machine)

	if code := post(t, srv, "/forge/agents/state?rename=moving&to=running"+q); code != 204 {
		t.Fatalf("rename: %d", code)
	}
	if got := s.lastEditProblem(); got != "" {
		t.Fatalf("rename was refused: %s", got)
	}
	def, _ := s.cfg.MachineSession.Working(machine)
	if _, ok := def.States["running"]; !ok {
		t.Fatalf("the state was not renamed: %v", def.StateOrder)
	}

	if code := post(t, srv, "/forge/agents/state?delete=running"+q); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if got := s.lastEditProblem(); got != "" {
		t.Fatalf("delete was refused: %s", got)
	}
	def, _ = s.cfg.MachineSession.Working(machine)
	if _, ok := def.States["running"]; ok {
		t.Error("the state is still there")
	}
}

// A refusal is reported rather than swallowed, for the operations whose
// arguments come from a prompt.
func TestCanvasState_RefusesANameTheEngineCannotUse(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	q := "&machine=" + url.QueryEscape(machine)

	for _, bad := range []string{"a.b", "", "idle"} {
		s.setEditProblem("")
		post(t, srv, "/forge/agents/state?rename=moving&to="+url.QueryEscape(bad)+q)
		if s.lastEditProblem() == "" {
			t.Errorf("%q was accepted as a state name", bad)
		}
	}
}

// ParseFloat accepts these; nothing downstream does. They used to surface as
// "json: unsupported value: +Inf" from deep inside the writer.
func TestCanvasMove_RefusesACoordinateThatIsNotFinite(t *testing.T) {
	s, srv, machine := canvasFixture(t)
	q := "&machine=" + url.QueryEscape(machine)

	for _, bad := range []string{"dx=NaN&dy=1", "dx=1&dy=Inf", "dx=-Inf&dy=0"} {
		s.setEditProblem("")
		if code := post(t, srv, "/forge/agents/state?move=idle&"+bad+q); code != 204 {
			t.Fatalf("unexpected status %d", code)
		}
		if !strings.Contains(s.lastEditProblem(), "finite") {
			t.Errorf("%q was accepted: %q", bad, s.lastEditProblem())
		}
	}
}
