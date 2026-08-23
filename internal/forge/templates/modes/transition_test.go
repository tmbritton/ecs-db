package modes

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
	"github.com/tmbritton/ecs-db/internal/forge/machinevalidation"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// forkMachine has what the transition panel is about: two transitions on one
// event, one of them guarded; an internal transition with actions; an after; and
// a target that resolves to nothing.
const forkMachine = `{
  "id": "wander",
  "initial": "idle",
  "states": {
    "idle": {
      "on": {
        "GO": [{ "target": "combat", "cond": "inRange" }, { "target": "resting" }],
        "POKE": [{ "actions": ["log"] }],
        "LOST": [{ "target": "nowhere" }]
      },
      "after": { "500": [{ "target": "resting" }] }
    },
    "combat": { "initial": "attacking", "states": { "attacking": {} } },
    "resting": {}
  }
}`

// transitionFixture hands the panel the catalogues in the engine's shape:
// sorted, described, with parameter schemas.
func transitionFixture(t *testing.T, sel string) Data {
	t.Helper()
	data := inspectorFixture(t, sel)
	data.Machine = mustParse(t, forkMachine)
	data.Chart = chart.Build(data.Machine, sel)
	data.Guards = []agent.GuardMeta{
		{
			Name:        "atTarget",
			Description: "True when the entity is within 1 unit of its target.",
		},
		{
			Name:        "inRange",
			Description: "True when the distance to target is at most distance.",
			Params: []agent.ParamSchema{
				{Name: "target", Type: "string", Required: true},
				{Name: "distance", Type: "number", Required: true, Default: float64(3)},
			},
		},
	}
	data.StateTargets = []string{"idle", "combat", "combat.attacking", "resting"}
	data.EventNames = []string{"GO", "LOST", "POKE"}
	data.SelectedState = nil
	return data
}

func renderTransition(t *testing.T, sel string) string {
	t.Helper()
	return renderAgents(t, transitionFixture(t, sel))
}

const (
	guardedGo = "edge:idle|on|GO|0"
	plainGo   = "edge:idle|on|GO|1"
	pokeSel   = "edge:idle|on|POKE|0"
	lostSel   = "edge:idle|on|LOST|0"
	afterSel  = "edge:idle|after|500|0"
)

// ── what it is ────────────────────────────────────────────────────────────────

// Readable, and readable to something reading the page aloud rather than only
// to something looking at two boxes and a line.
func TestTransitionInspector_SaysWhereTheTransitionGoes(t *testing.T) {
	for sel, want := range map[string]string{
		guardedGo: "idle → combat",
		pokeSel:   "idle → stays in idle",
		lostSel:   "idle → nowhere · no state of that name",
	} {
		route := elementOf(t, renderTransition(t, sel), "transition-route")
		if !strings.Contains(route, want) {
			t.Errorf("%s: route = %s, want %q", sel, route, want)
		}
	}
}

func TestTransitionInspector_FillsInTheEvent(t *testing.T) {
	field := tagWith(t, renderTransition(t, guardedGo), "transition-event")
	if !strings.Contains(field, `value="GO"`) {
		t.Errorf("the event field is not filled in: %s", field)
	}
	if !strings.Contains(field, "op=event") {
		t.Errorf("the event field posts nothing that sets an event: %s", field)
	}
}

// An event name is authored rather than registered, so the control is a text
// field with suggestions — not a dropdown, which would prevent a new one.
// An after transition's key is a duration, so the machine's event names are not
// suggestions for it — every one of them would be refused by ParseDurationMs
// the moment it was chosen.
func TestTransitionInspector_DoesNotSuggestEventNamesForADuration(t *testing.T) {
	if strings.Contains(tagWith(t, renderTransition(t, afterSel), "transition-event"), "list=") {
		t.Error("the duration field offers the machine's event names")
	}
	if !strings.Contains(tagWith(t, renderTransition(t, guardedGo), "transition-event"), "list=") {
		t.Error("an event field offers no suggestions at all")
	}
}

// Two transitions on one event share the event, so a row that says only the
// event reads the same as its neighbour — and so does the button beside it.
func TestTransitionInspector_TellsTheRowsOfTheOrderApart(t *testing.T) {
	data := transitionFixture(t, guardedGo)
	// Both unguarded, so the guard cannot be what distinguishes them.
	data.Machine.States["idle"].On["GO"][0].Cond = nil
	data.Chart = chart.Build(data.Machine, guardedGo)
	html := renderAgents(t, data)

	first := elementOf(t, html, "order-0")
	second := elementOf(t, html, "order-1")
	if !strings.Contains(first, "combat") || !strings.Contains(second, "resting") {
		t.Errorf("the rows do not say where each transition goes:\n%s\n%s", first, second)
	}
	if !strings.Contains(tagWith(t, html, "move-down-0"), "combat") {
		t.Errorf("the move control is named after the event alone: %s", tagWith(t, html, "move-down-0"))
	}
}

func TestTransitionInspector_SuggestsTheEventsThisMachineUses(t *testing.T) {
	html := renderTransition(t, guardedGo)
	field := tagWith(t, html, "transition-event")
	if !strings.Contains(field, `list="transition-events"`) {
		t.Errorf("the event field offers no suggestions: %s", field)
	}
	if strings.Contains(field, "<select") {
		t.Error("the event is a dropdown, which cannot express a new event")
	}
	for _, want := range []string{`value="GO"`, `value="LOST"`, `value="POKE"`} {
		if !strings.Contains(html, "<option "+want) {
			t.Errorf("the suggestions do not offer %s", want)
		}
	}
}

// An after transition's key is a duration, so the field means something else
// and says so — with a spelling that works, since nothing else on screen says
// what one looks like.
func TestTransitionInspector_ShowsADurationForAnAfterTransition(t *testing.T) {
	html := renderTransition(t, afterSel)
	if !strings.Contains(html, `<span class="inspector__label">after</span>`) {
		t.Error("an after transition's field is labelled as an event")
	}
	hint := elementOf(t, html, "duration-hint")
	if !strings.Contains(hint, "1s") || !strings.Contains(hint, "1 second") {
		t.Errorf("the hint gives no spelling that works: %s", hint)
	}
	if strings.Contains(renderTransition(t, guardedGo), `data-testid="duration-hint"`) {
		t.Error("an ordinary event is described as a duration")
	}
}

// ── the target ────────────────────────────────────────────────────────────────

func TestTransitionInspector_OffersOnlyThisMachinesStatesAsTargets(t *testing.T) {
	target := elementOf(t, renderTransition(t, guardedGo), "transition-target")
	for _, want := range []string{">idle<", ">combat<", ">combat.attacking<", ">resting<"} {
		if !strings.Contains(target, want) {
			t.Errorf("the target dropdown does not offer %s: %s", want, target)
		}
	}
	if !strings.Contains(target, `value="combat" selected`) {
		t.Errorf("the current target is not the selected option: %s", target)
	}
}

// A transition with actions and no target runs them and changes no state. The
// dropdown has to be able to say that, or opening the panel would retarget it.
func TestTransitionInspector_OffersNoTargetAsAChoice(t *testing.T) {
	target := elementOf(t, renderTransition(t, pokeSel), "transition-target")
	if !strings.Contains(target, `<option value="" selected`) {
		t.Errorf("an internal transition does not show as having no target: %s", target)
	}
	if !strings.Contains(target, "none") {
		t.Errorf("nothing on the control says what an empty target means: %s", target)
	}
}

// A target that resolves to no state is offered as itself, so the control says
// what the file says rather than appearing to have been set to something else.
func TestTransitionInspector_OffersATargetThatResolvesToNothing(t *testing.T) {
	target := elementOf(t, renderTransition(t, lostSel), "transition-target")
	if !strings.Contains(target, `value="nowhere" selected`) {
		t.Errorf("a dangling target is silently shown as something else: %s", target)
	}
}

// ── the guard ─────────────────────────────────────────────────────────────────

func TestTransitionInspector_OffersTheRegisteredGuardsAndAnExplicitNone(t *testing.T) {
	guard := elementOf(t, renderTransition(t, guardedGo), "transition-guard")
	if !strings.Contains(guard, `<option value="" `) && !strings.Contains(guard, `<option value="">`) {
		t.Errorf("there is no way to say a transition has no guard: %s", guard)
	}
	if !strings.Contains(guard, "atTarget") || !strings.Contains(guard, "inRange") {
		t.Errorf("the registered guards are not offered: %s", guard)
	}
	// The description travels with the name, so what a guard does is on screen
	// while you are choosing rather than only after you have chosen.
	if !strings.Contains(guard, "True when the distance to target is at most distance.") {
		t.Errorf("the options carry no description: %s", guard)
	}
	if !strings.Contains(guard, `value="inRange" selected`) {
		t.Errorf("the transition's own guard is not selected: %s", guard)
	}
}

// A guard the registry does not have is a machine the engine will refuse.
// Dropping it from the options would make the panel show "none" over a
// transition that has one.
func TestTransitionInspector_KeepsAGuardTheRegistryDoesNotHave(t *testing.T) {
	data := transitionFixture(t, guardedGo)
	data.Guards = data.Guards[:1] // inRange is gone, as it is without a map
	guard := elementOf(t, renderAgents(t, data), "transition-guard")
	if !strings.Contains(guard, `value="inRange" selected`) {
		t.Errorf("an unregistered guard was silently dropped: %s", guard)
	}
	if !strings.Contains(guard, "not a guard this project") {
		t.Errorf("nothing says the guard will not load: %s", guard)
	}
}

// The same generated form as an action's, from the same ParamSchema, through
// the same component — which is what the shared test id proves.
func TestTransitionInspector_GeneratesTheGuardsParametersFromItsSchema(t *testing.T) {
	html := renderTransition(t, guardedGo)
	distance := tagWith(t, html, "input-guard-distance")
	if !strings.Contains(distance, `data-param-type="number"`) {
		t.Errorf("the guard parameter is not typed from the registry: %s", distance)
	}
	if !strings.Contains(distance, `inputmode="decimal"`) {
		t.Errorf("a number parameter offers no numeric keypad: %s", distance)
	}
	// The registered default is a placeholder, never a value.
	if !strings.Contains(distance, `placeholder="3"`) {
		t.Errorf("the registered default is not shown as a hint: %s", distance)
	}
	if strings.Contains(distance, `value="3"`) {
		t.Errorf("the registered default was written in as a value: %s", distance)
	}
	if !strings.Contains(distance, "op=guardparam") {
		t.Errorf("the guard parameter posts nothing that sets one: %s", distance)
	}
}

func TestTransitionInspector_ReportsARequiredGuardParameterAgainstItsField(t *testing.T) {
	html := renderTransition(t, guardedGo)
	// Under its own field, not merely somewhere on the page — which every other
	// assertion in this file is careful about and this one was not.
	problem := elementOf(t, html, "problem-guard-target")
	if !strings.Contains(problem, "target is required") {
		t.Errorf("a required guard parameter left empty is not reported: %s", problem)
	}
	// And it says a *guard* fails, not an action: the form is shared and its
	// copy has to be too.
	if !strings.Contains(problem, "guard fails when it runs") {
		t.Errorf("a guard's required parameter is described as an action's: %s", problem)
	}
	wantID := paramProblemsID("guard", "target")
	input := tagWith(t, html, "input-guard-target")
	if !strings.Contains(input, `aria-describedby="`+wantID+`"`) {
		t.Errorf("the input does not point at its message: %s", input)
	}
}

func TestTransitionInspector_ShowsNoParameterFormWithoutAGuard(t *testing.T) {
	html := renderTransition(t, plainGo)
	if strings.Contains(html, `data-testid="input-guard-`) {
		t.Error("a transition with no guard has a guard parameter form")
	}
	if strings.Contains(html, `data-testid="noparams-guard"`) {
		t.Error("a transition with no guard says its guard takes no parameters")
	}
}

// ── the transition's actions ──────────────────────────────────────────────────

func TestTransitionInspector_ListsTheActionsItRunsOnTheWayThrough(t *testing.T) {
	html := renderTransition(t, pokeSel)
	row := elementOf(t, html, "taction-action-0")
	if !strings.Contains(row, `data-action="log"`) {
		t.Errorf("the transition's action is not listed: %s", row)
	}
	remove := tagWith(t, html, "remove-taction-0")
	if !strings.Contains(remove, "op=removeaction") {
		t.Errorf("the remove control posts nothing that removes one: %s", remove)
	}
	add := elementOf(t, html, "add-taction-action")
	if !strings.Contains(add, "op=addaction") {
		t.Errorf("the add control posts nothing that adds one: %s", add)
	}
}

// ── the order, which is the logic ─────────────────────────────────────────────

func TestTransitionInspector_ShowsTheOrderOnlyWhenThereIsOne(t *testing.T) {
	if !strings.Contains(renderTransition(t, guardedGo), `data-testid="transition-order"`) {
		t.Error("two transitions on one event show no order")
	}
	if strings.Contains(renderTransition(t, pokeSel), `data-testid="transition-order"`) {
		t.Error("the only transition on an event is offered a reorder")
	}
}

func TestTransitionInspector_MarksWhichOfTheOrderIsSelected(t *testing.T) {
	html := renderTransition(t, plainGo)
	if !strings.Contains(tagWith(t, html, "order-1"), `data-current="true"`) {
		t.Error("the selected transition is not marked in the order")
	}
	if !strings.Contains(tagWith(t, html, "order-0"), `data-current="false"`) {
		t.Error("a transition that is not selected is marked as if it were")
	}
}

// Reordering is the only way to express priority, and the buttons address the
// row they are on rather than the selected one: sorting three of them by
// selecting each in turn is not reordering, it is a chore.
func TestTransitionInspector_MovesTheRowTheButtonIsOn(t *testing.T) {
	html := renderTransition(t, guardedGo)
	down := tagWith(t, html, "move-down-0")
	if !strings.Contains(down, "op=move") || !strings.Contains(down, "by=1") {
		t.Errorf("the move-down control posts nothing that moves one: %s", down)
	}
	if !strings.Contains(down, "index=0") {
		t.Errorf("the move-down control does not address its own row: %s", down)
	}
	up := tagWith(t, html, "move-up-1")
	if !strings.Contains(up, "index=1") || !strings.Contains(up, "by=-1") {
		t.Errorf("the move-up control does not move its own row up: %s", up)
	}
}

// A button that cannot do anything is worse than one that is not there, and a
// disabled one at least says so.
func TestTransitionInspector_DisablesTheMovesOffEitherEnd(t *testing.T) {
	html := renderTransition(t, guardedGo)
	if !strings.Contains(tagWith(t, html, "move-up-0"), "disabled") {
		t.Error("the first transition offers a move up")
	}
	if !strings.Contains(tagWith(t, html, "move-down-1"), "disabled") {
		t.Error("the last transition offers a move down")
	}
	if strings.Contains(tagWith(t, html, "move-down-0"), "disabled") {
		t.Error("a move that can happen is disabled")
	}
}

func TestTransitionInspector_SaysTheOrderIsTheLogic(t *testing.T) {
	note := elementOf(t, renderTransition(t, guardedGo), "order-note")
	if !strings.Contains(note, "first") {
		t.Errorf("nothing says what the order means: %s", note)
	}
}

// Nothing below an unconditional transition can ever fire. A warning and not an
// error: ValidateMachine does not check it.
func TestTransitionInspector_WarnsAboutWhatCanNeverFire(t *testing.T) {
	if strings.Contains(renderTransition(t, guardedGo), `data-testid="unreachable-warning"`) {
		t.Error("a guarded transition followed by an unguarded one is called unreachable")
	}
	data := transitionFixture(t, guardedGo)
	// Take the guard off the first one, so the second can never be reached.
	data.Machine.States["idle"].On["GO"][0].Cond = nil
	data.Chart = chart.Build(data.Machine, guardedGo)
	if !strings.Contains(renderAgents(t, data), `data-testid="unreachable-warning"`) {
		t.Error("a transition that can never fire is not reported")
	}
}

// ── deleting ──────────────────────────────────────────────────────────────────

func TestTransitionInspector_AsksBeforeDeleting(t *testing.T) {
	button := tagWith(t, renderTransition(t, guardedGo), "delete-transition")
	if !strings.Contains(button, "confirm(") {
		t.Errorf("the transition is deleted without asking: %s", button)
	}
	if !strings.Contains(button, "op=delete") {
		t.Errorf("the delete control posts nothing that deletes: %s", button)
	}
	// Named, so the question says what is about to go rather than "are you sure".
	if !strings.Contains(button, "idle") {
		t.Errorf("the question does not say which transition: %s", button)
	}
}

// ── refusals, reported where the mistake was made ─────────────────────────────

func TestTransitionInspector_ReportsARefusalUnderTheFieldItWasAbout(t *testing.T) {
	data := transitionFixture(t, afterSel)
	data.Problem = `"1 second" is not a duration the engine reads — write 500 for milliseconds, or 1s, or 1.5s`
	data.ProblemField = "event"
	html := renderAgents(t, data)

	problem := elementOf(t, html, "problem-transition-event")
	if !strings.Contains(problem, "1s") {
		t.Errorf("the refusal is not reported against the field: %s", problem)
	}
	field := tagWith(t, html, "transition-event")
	if !strings.Contains(field, `aria-describedby="`+fieldProblemsID("event")+`"`) {
		t.Errorf("the field does not point at the message: %s", field)
	}
	if !strings.Contains(field, `aria-invalid="true"`) {
		t.Errorf("the field is not marked invalid: %s", field)
	}
	// And only then. An aria-describedby naming an id nothing carries is the
	// same as none at all, and looks identical — the failure it exists to
	// prevent, in the state the field is in almost all of the time.
	clean := tagWith(t, renderTransition(t, afterSel), "transition-event")
	if strings.Contains(clean, "aria-describedby") {
		t.Errorf("the field points at a message that is not there: %s", clean)
	}
}

// The other two fields report the same way. Reachable, if rarely: both are
// dropdowns of what exists right now, so the only way to choose something that
// is not there is to act on a page that has gone stale.
func TestTransitionInspector_ReportsARefusalUnderTheTargetAndTheGuard(t *testing.T) {
	for _, field := range []string{"target", "guard"} {
		data := transitionFixture(t, guardedGo)
		data.Problem = "there is no state called that any more"
		data.ProblemField = field
		panel := elementOf(t, renderAgents(t, data), "transition-"+field)
		if !strings.Contains(panel, "no state called that any more") {
			t.Errorf("a refused %s is not reported against its control: %s", field, panel)
		}
		if !strings.Contains(panel, fieldProblemsID(field)) {
			t.Errorf("the %s control does not point at its message: %s", field, panel)
		}
	}
}

// A refusal about something else must not appear under this field, or every
// unrelated failure would look like a bad duration.
func TestTransitionInspector_LeavesTheFieldAloneForAnUnrelatedRefusal(t *testing.T) {
	data := transitionFixture(t, afterSel)
	data.Problem = "no machine is open"
	data.ProblemField = ""
	// In the panel, not on the page: the banner carries every refusal and is on
	// screen everywhere, which is the point of it.
	panel := elementOf(t, renderAgents(t, data), "transition-inspector")
	if strings.Contains(panel, "no machine is open") {
		t.Error("a refusal about nothing in this panel was reported in it")
	}
}

// tagWith is the opening tag that carries a test id — the attributes of one
// control and nothing else.
//
// elementWith finds the enclosing <div>, which is right for a row and wrong for
// an input: the nearest div around a field is the whole panel, and an assertion
// "on the field" would really be an assertion about everything beside it. That
// is the same mistake Story 6 made four times.
func tagWith(t *testing.T, html, testid string) string {
	t.Helper()
	open := tagStart(t, html, testid)
	end := strings.Index(html[open:], ">")
	if end < 0 {
		t.Fatalf("the tag carrying %q is never closed", testid)
	}
	return html[open : open+end+1]
}

// elementOf is the whole element carrying a test id, whatever tag it is,
// balanced against its own closing tag.
func elementOf(t *testing.T, html, testid string) string {
	t.Helper()
	open := tagStart(t, html, testid)
	name := tagName(html[open:])
	depth, j := 0, open
	for j < len(html) {
		switch {
		case strings.HasPrefix(html[j:], "<"+name):
			depth++
			j += len(name) + 1
		case strings.HasPrefix(html[j:], "</"+name+">"):
			depth--
			j += len(name) + 3
			if depth == 0 {
				return html[open:j]
			}
		default:
			j++
		}
	}
	t.Fatalf("<%s> carrying %q is never closed", name, testid)
	return ""
}

func tagStart(t *testing.T, html, testid string) int {
	t.Helper()
	i := strings.Index(html, `data-testid="`+testid+`"`)
	if i < 0 {
		t.Fatalf("no element with test id %q", testid)
	}
	open := strings.LastIndex(html[:i], "<")
	if open < 0 {
		t.Fatalf("%q is not in a tag", testid)
	}
	return open
}

func tagName(s string) string {
	name := strings.TrimPrefix(s, "<")
	if i := strings.IndexAny(name, " >\n\t"); i >= 0 {
		name = name[:i]
	}
	return name
}

// ── validation, placed ────────────────────────────────────────────────────────

// The rail says what is wrong with the transition it is showing — against the
// control whose value failed, not merely somewhere in the panel. `LOST` targets
// a state that does not exist, so the target dropdown is what carries it and
// what points at it.
func TestTransitionInspector_AssociatesAProblemWithTheControlItIsAbout(t *testing.T) {
	data := transitionFixture(t, lostSel)
	errs := agent.ValidateMachine(data.Machine, agent.NewRegistry(), schema.DatabaseSchema{})
	data.Problems = machinevalidation.Check(data.Machine, errs, data.Chart)

	target := elementOf(t, renderAgents(t, data), "transition-target")
	if !strings.Contains(target, "not a known state") {
		t.Errorf("the problem is not under the control it is about: %s", target)
	}
	// Pointed at, not merely near: proximity is not association.
	if !strings.Contains(target, `aria-describedby="`+fieldProblemsID("target")+`"`) {
		t.Errorf("the control does not point at the message: %s", target)
	}
	// Stated as an error, not merely coloured: every one of these is a reason
	// the save is refused.
	if !strings.Contains(target, "error") {
		t.Errorf("the severity is only a hue: %s", target)
	}
}

// A guard the engine does not register is the guard control's problem.
func TestTransitionInspector_AssociatesAGuardProblemWithTheGuardControl(t *testing.T) {
	data := transitionFixture(t, guardedGo)
	errs := agent.ValidateMachine(data.Machine, agent.NewRegistry(), schema.DatabaseSchema{})
	data.Problems = machinevalidation.Check(data.Machine, errs, data.Chart)

	guard := elementOf(t, renderAgents(t, data), "transition-guard")
	if !strings.Contains(guard, "is not registered") {
		t.Errorf("the guard problem is not under the guard control: %s", guard)
	}
	if !strings.Contains(guard, `aria-describedby="`+fieldProblemsID("guard")+`"`) {
		t.Errorf("the guard control does not point at the message: %s", guard)
	}
}

// An action a transition runs is a row rather than a control, so it has nowhere
// to be associated to and renders as the panel's own list.
func TestTransitionInspector_ShowsAnActionProblemInThePanelsOwnList(t *testing.T) {
	data := transitionFixture(t, pokeSel)
	errs := agent.ValidateMachine(data.Machine, agent.NewRegistry(), schema.DatabaseSchema{})
	data.Problems = machinevalidation.Check(data.Machine, errs, data.Chart)

	problems := elementOf(t, renderAgents(t, data), "edge-problems")
	if !strings.Contains(problems, "transition action") {
		t.Errorf("the action problem is nowhere in the panel: %s", problems)
	}
}

func mustParse(t *testing.T, src string) *agent.MachineDefinition {
	t.Helper()
	def, err := agent.ParseMachine([]byte(src))
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	return def
}
