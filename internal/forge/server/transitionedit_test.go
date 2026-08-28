package server

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
)

// transitionMachine is what the transition inspector edits: two transitions on
// one event (the order is the logic), a guard authored as the bare shorthand, an
// internal transition with actions and no target, and an after.
const transitionMachine = `{
  "id": "wander",
  "initial": "idle",
  "states": {
    "idle": {
      "on": {
        "GO": [{ "target": "moving", "cond": "inRange" }, { "target": "resting" }],
        "POKE": [{ "actions": ["log"] }]
      },
      "after": { "500": [{ "target": "resting" }] }
    },
    "moving": {},
    "resting": {}
  }
}
`

func transitionFixture(t *testing.T) (*Server, *httptest.Server, string, string) {
	t.Helper()
	srv, s, core := machineServerWith(t, transitionMachine)
	machine := filepath.Join(core, "wander.json")
	return s, srv, machine, "&machine=" + url.QueryEscape(machine)
}

func transitionsOn(t *testing.T, s *Server, machine, state, event string) []agent.Transition {
	t.Helper()
	def, err := s.cfg.MachineSession.Working(machine)
	if err != nil {
		t.Fatalf("Working: %v", err)
	}
	return def.States[state].On[event]
}

const goRef = "&from=idle&kind=on&event=GO&index=0"

// problemOf and fieldOf split the pair lastEditProblem hands back, so a test
// reading one of them stays a single expression.
func problemOf(s *Server) string { msg, _ := s.lastEditProblem(); return msg }

func fieldOf(s *Server) string { _, field := s.lastEditProblem(); return field }

// ── the ops the canvas already had, through the one dispatch ──────────────────

func TestTransitionEdit_ConnectsTwoStates(t *testing.T) {
	s, srv, machine, q := transitionFixture(t)

	if code := post(t, srv, "/forge/agents/transition?op=connect&from=moving&to=resting"+q); code != 204 {
		t.Fatalf("connect: %d (%s)", code, problemOf(s))
	}
	def, _ := s.cfg.MachineSession.Working(machine)
	if len(def.States["moving"].On) != 1 {
		t.Errorf("moving has %d events, want 1", len(def.States["moving"].On))
	}
}

// Deleting shifts every sibling after it down one, so the selection cannot stay
// where it is: left alone it would name the survivor that took the index, and a
// second Delete would remove that one too.
func TestTransitionEdit_DeletingOneLetsGoOfTheSelection(t *testing.T) {
	s, srv, machine, q := transitionFixture(t)

	page := s.newPage()
	s.setPageSel(page, "edge:idle|on|GO|0")

	if code := post(t, srv, "/forge/agents/transition?op=delete"+goRef+q+"&page="+page); code != 204 {
		t.Fatalf("delete: %d (%s)", code, problemOf(s))
	}
	got := transitionsOn(t, s, machine, "idle", "GO")
	if len(got) != 1 || got[0].Target != "resting" {
		t.Errorf("GO = %+v, want only the unguarded one", got)
	}
	// It used to answer with a navigation, because selection lived in the URL.
	// The page's own record moves instead, and the chart follows on the stream.
	if got := s.pageSel(page); got != "" {
		t.Errorf("the page still has %q selected, naming a transition that is gone", got)
	}
}

// Renaming an event to the name it already has moves nothing, so it must not
// answer with a navigation — a stray blur would reload the page.
func TestTransitionEdit_ANoOpRenameDoesNotNavigate(t *testing.T) {
	s, srv, _, q := transitionFixture(t)

	if code := post(t, srv, "/forge/agents/transition?op=event&value=GO"+goRef+q); code != 204 {
		t.Errorf("a rename to the same name answered %d (%s)", code, problemOf(s))
	}
}

// ── the event ─────────────────────────────────────────────────────────────────

// Renaming an event moves the transition, so the id the selection is keyed on
// changes. The server moves this page's selection after it, because only the
// server knows where it went: moving onto an event that already exists appends.
func TestTransitionEdit_RenamingTheEventFollowsWhereItWent(t *testing.T) {
	s, srv, machine, q := transitionFixture(t)

	page := s.newPage()
	s.setPageSel(page, "edge:idle|on|GO|0")

	if code := post(t, srv, "/forge/agents/transition?op=event&value=PROD"+goRef+q+"&page="+page); code != 204 {
		t.Fatalf("event: %d (%s)", code, problemOf(s))
	}
	if len(transitionsOn(t, s, machine, "idle", "PROD")) != 1 {
		t.Error("the transition did not move to PROD")
	}
	// The new selection, not the old one: PROD did not exist, so it is index 0.
	if got := s.pageSel(page); got != "edge:idle|on|PROD|0" {
		t.Errorf("the page has %q selected, not where the transition went", got)
	}
}

func TestTransitionEdit_RefusesADurationTheEngineCannotRead(t *testing.T) {
	s, srv, machine, q := transitionFixture(t)

	code := post(t, srv, "/forge/agents/transition?op=event&value=1+second&from=idle&kind=after&event=500&index=0"+q)
	if code != 204 {
		t.Fatalf("event: %d", code)
	}
	def, _ := s.cfg.MachineSession.Working(machine)
	if _, still := def.States["idle"].After["500"]; !still {
		t.Error("the refused duration landed anyway")
	}
	if !strings.Contains(problemOf(s), "1s") {
		t.Errorf("the refusal carries no spelling that works: %q", problemOf(s))
	}
	// Against the field, so the panel can say it where the mistake was made
	// rather than only in the banner at the top of the page.
	if fieldOf(s) != "event" {
		t.Errorf("problem field = %q, want event", fieldOf(s))
	}
}

// ── target, guard and the guard's parameters ──────────────────────────────────

func TestTransitionEdit_SetsAndClearsTheTarget(t *testing.T) {
	s, srv, machine, q := transitionFixture(t)

	if code := post(t, srv, "/forge/agents/transition?op=target&value=resting"+goRef+q); code != 204 {
		t.Fatalf("target: %d (%s)", code, problemOf(s))
	}
	if got := transitionsOn(t, s, machine, "idle", "GO")[0].Target; got != "resting" {
		t.Errorf("target = %q", got)
	}
	// An empty value is a transition that runs its actions and changes no
	// state, which is a thing the file can hold and the panel has to be able to
	// say. A dispatch keyed on "which parameter is present" could not express
	// it, which is why every transition op names itself.
	if code := post(t, srv, "/forge/agents/transition?op=target&value="+goRef+q); code != 204 {
		t.Fatalf("clear target: %d (%s)", code, problemOf(s))
	}
	if got := transitionsOn(t, s, machine, "idle", "GO")[0].Target; got != "" {
		t.Errorf("target = %q, want empty", got)
	}
}

func TestTransitionEdit_SetsAndClearsTheGuard(t *testing.T) {
	s, srv, machine, q := transitionFixture(t)

	if code := post(t, srv, "/forge/agents/transition?op=guard&value=atTarget&from=idle&kind=on&event=GO&index=1"+q); code != 204 {
		t.Fatalf("guard: %d (%s)", code, problemOf(s))
	}
	if got := transitionsOn(t, s, machine, "idle", "GO")[1].Cond; got == nil || got.Type != "atTarget" {
		t.Errorf("cond = %+v", got)
	}
	if code := post(t, srv, "/forge/agents/transition?op=guard&value="+goRef+q); code != 204 {
		t.Fatalf("clear guard: %d (%s)", code, problemOf(s))
	}
	if got := transitionsOn(t, s, machine, "idle", "GO")[0].Cond; got != nil {
		t.Errorf("cond = %+v, want nil", got)
	}
}

func TestTransitionEdit_WritesAGuardParameterByItsDeclaredType(t *testing.T) {
	s, srv, machine, q := transitionFixture(t)

	if code := post(t, srv, "/forge/agents/transition?op=guardparam&param=distance&value=4"+goRef+q); code != 204 {
		t.Fatalf("guardparam: %d (%s)", code, problemOf(s))
	}
	if got := transitionsOn(t, s, machine, "idle", "GO")[0].Cond.Params["distance"]; got != float64(4) {
		t.Errorf("distance = %#v, want the number 4", got)
	}
}

// ── the transition's own actions ──────────────────────────────────────────────

func TestTransitionEdit_AddsRemovesAndFillsInAnAction(t *testing.T) {
	s, srv, machine, q := transitionFixture(t)
	poke := "&from=idle&kind=on&event=POKE&index=0"

	if code := post(t, srv, "/forge/agents/transition?op=addaction&value=dealDamage"+poke+q); code != 204 {
		t.Fatalf("addaction: %d (%s)", code, problemOf(s))
	}
	if code := post(t, srv, "/forge/agents/transition?op=actionparam&action=1&param=amount&value=6"+poke+q); code != 204 {
		t.Fatalf("actionparam: %d (%s)", code, problemOf(s))
	}
	got := transitionsOn(t, s, machine, "idle", "POKE")[0].Actions
	if len(got) != 2 || got[1].Params["amount"] != float64(6) {
		t.Fatalf("actions = %+v", got)
	}
	if code := post(t, srv, "/forge/agents/transition?op=removeaction&action=0"+poke+q); code != 204 {
		t.Fatalf("removeaction: %d (%s)", code, problemOf(s))
	}
	if got := transitionsOn(t, s, machine, "idle", "POKE")[0].Actions; len(got) != 1 || got[0].Type != "dealDamage" {
		t.Errorf("actions = %+v, want only dealDamage", got)
	}
}

// ── the order, which is the logic ─────────────────────────────────────────────

func TestTransitionEdit_MovingOneFollowsAfterIt(t *testing.T) {
	s, srv, machine, q := transitionFixture(t)

	page := s.newPage()
	s.setPageSel(page, "edge:idle|on|GO|0")

	if code := post(t, srv, "/forge/agents/transition?op=move&by=1"+goRef+q+"&page="+page); code != 204 {
		t.Fatalf("move: %d (%s)", code, problemOf(s))
	}
	list := transitionsOn(t, s, machine, "idle", "GO")
	if list[0].Target != "resting" {
		t.Errorf("order = %q first", list[0].Target)
	}
	// Without this the button would not repeat: the selection would name
	// whatever took the old index, and clicking again would undo the move.
	if got := s.pageSel(page); got != "edge:idle|on|GO|1" {
		t.Errorf("the page has %q selected, not where the transition went", got)
	}
}

func TestTransitionEdit_RefusesAMoveOffTheEnd(t *testing.T) {
	s, srv, _, q := transitionFixture(t)

	if code := post(t, srv, "/forge/agents/transition?op=move&by=-1"+goRef+q); code != 204 {
		t.Fatalf("move: %d", code)
	}
	if problemOf(s) == "" {
		t.Error("moving off the end was accepted silently")
	}
}

func TestTransitionEdit_RefusesAMoveThatIsNotANumber(t *testing.T) {
	s, srv, _, q := transitionFixture(t)

	if code := post(t, srv, "/forge/agents/transition?op=move&by=sideways"+goRef+q); code != 204 {
		t.Fatalf("move: %d", code)
	}
	if problemOf(s) == "" {
		t.Error("a move by \"sideways\" was accepted")
	}
}

// ── addressing ────────────────────────────────────────────────────────────────

func TestTransitionEdit_RefusesAnIndexThatIsNotANumber(t *testing.T) {
	s, srv, _, q := transitionFixture(t)

	if code := post(t, srv, "/forge/agents/transition?op=target&value=resting&from=idle&kind=on&event=GO&index=first"+q); code != 204 {
		t.Fatalf("target: %d", code)
	}
	if problemOf(s) == "" {
		t.Error("an index of \"first\" was accepted")
	}
}

func TestTransitionEdit_RefusesAnActionIndexThatIsNotANumber(t *testing.T) {
	s, srv, _, q := transitionFixture(t)
	poke := "&from=idle&kind=on&event=POKE&index=0"

	if code := post(t, srv, "/forge/agents/transition?op=removeaction&action=first"+poke+q); code != 204 {
		t.Fatalf("removeaction: %d", code)
	}
	// Not silently the first one, which is what Atoi's zero would make it.
	if problemOf(s) == "" {
		t.Error("an action index of \"first\" was accepted")
	}
}

// The page carries the two catalogues and the state list the panel is built
// from, and it carries them for this project — the template tests set Data by
// hand, so nothing else covers the wiring.
func TestAgentsPage_CarriesWhatTheTransitionPanelIsBuiltFrom(t *testing.T) {
	srv, _, core := machineServerWith(t, transitionMachine)
	machine := filepath.Join(core, "wander.json")

	_, body := get(t, srv, "/forge/agents?machine="+url.QueryEscape(machine)+
		"&sel="+url.QueryEscape("edge:idle|on|GO|0"))

	if !strings.Contains(body, `value="inRange"`) {
		t.Error("the guard catalogue is not on the page")
	}
	if !strings.Contains(body, "True when distance between this entity") {
		t.Error("the registry's guard description did not reach the page")
	}
	// The fixture project has no map, so the engine would not register it.
	if strings.Contains(body, "inLineOfSight") {
		t.Error("the page offers a guard the engine would not register")
	}
	if !strings.Contains(body, `value="resting"`) {
		t.Error("the machine's states are not on the page as targets")
	}
}

// A refusal reaches the panel as well as the banner, so it is reported where the
// mistake was made.
//
// Asserted on modeData rather than on a page load, because a full page load
// deliberately starts clean — an edit refused in another tab is not this page's
// problem to report. The stream is what carries a refusal to the panel that was
// open when it happened, and the stream renders from this.
func TestModeData_CarriesTheFieldARefusalWasAbout(t *testing.T) {
	s, srv, machine, q := transitionFixture(t)

	post(t, srv, "/forge/agents/transition?op=event&value=1+second&from=idle&kind=after&event=500&index=0"+q)
	if fieldOf(s) != "event" {
		t.Fatalf("the refusal named field %q", fieldOf(s))
	}

	req := httptest.NewRequest("GET", "/forge/agents?machine="+url.QueryEscape(machine)+
		"&sel="+url.QueryEscape("edge:idle|after|500|0"), nil)
	data := s.modeData(req)
	if data.ProblemField != "event" {
		t.Errorf("ProblemField = %q, want event", data.ProblemField)
	}
	if data.Problem == "" {
		t.Error("the refusal itself did not reach the panel")
	}
}

func TestTransitionEdit_RefusesAnOperationItDoesNotHave(t *testing.T) {
	s, srv, _, q := transitionFixture(t)

	if code := post(t, srv, "/forge/agents/transition?op=reverse"+goRef+q); code != 204 {
		t.Fatalf("reverse: %d", code)
	}
	if problemOf(s) == "" {
		t.Error("an operation nothing implements was accepted")
	}
}

// A refusal that is not about one field must not mark one, or the panel would
// point at whatever field was named last.
func TestTransitionEdit_ClearsTheFieldWhenTheRefusalIsNotAboutOne(t *testing.T) {
	s, srv, _, q := transitionFixture(t)

	post(t, srv, "/forge/agents/transition?op=event&value=1+second&from=idle&kind=after&event=500&index=0"+q)
	if fieldOf(s) == "" {
		t.Fatal("the duration refusal named no field")
	}
	post(t, srv, "/forge/agents/transition?op=reverse"+goRef+q)
	if fieldOf(s) != "" {
		t.Errorf("field = %q, want none", fieldOf(s))
	}
}

// A successful edit clears the last refusal, field and all.
func TestTransitionEdit_ClearsTheFieldOnceTheEditLands(t *testing.T) {
	s, srv, _, q := transitionFixture(t)

	post(t, srv, "/forge/agents/transition?op=event&value=1+second&from=idle&kind=after&event=500&index=0"+q)
	if code := post(t, srv, "/forge/agents/transition?op=target&value=resting"+goRef+q); code != 204 {
		t.Fatalf("target: %d", code)
	}
	if problemOf(s) != "" || fieldOf(s) != "" {
		t.Errorf("a landed edit left %q on %q", problemOf(s), fieldOf(s))
	}
}
