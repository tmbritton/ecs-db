package modes

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
)

// inspectorFixture selects a state and hands the inspector a catalogue that is
// the engine's shape: sorted, described, with parameter schemas.
func inspectorFixture(t *testing.T, sel string) Data {
	t.Helper()
	data := canvasFixture(t, canvasMachine, sel)
	data.SelectedState = stateFor(t, data, sel)
	data.Actions = []agent.ActionMeta{
		{
			Name:        "dealDamage",
			Description: "Decrement Health.hp on target entity by amount.",
			Params: []agent.ParamSchema{
				{Name: "amount", Type: "number", Required: true},
				{Name: "target", Type: "string", Required: false, Default: "$player"},
			},
		},
		{
			Name:        "attachComponent",
			Description: "Attach a component to the current entity.",
			Params: []agent.ParamSchema{
				{Name: "component", Type: "string", Required: true},
				{Name: "data", Type: "object", Required: false},
			},
		},
		{Name: "setPursueTarget", Description: "Copy the Player entity's position."},
	}
	return data
}

func stateFor(t *testing.T, data Data, sel string) *agent.StateNode {
	t.Helper()
	if !strings.HasPrefix(sel, chart.SelState) {
		return nil
	}
	node, err := stateNodeAt(data.Machine, strings.TrimPrefix(sel, chart.SelState))
	if err != nil {
		t.Fatalf("stateNodeAt: %v", err)
	}
	return node
}

func TestInspector_SaysWhatToDoWhenNothingIsSelected(t *testing.T) {
	html := renderAgents(t, inspectorFixture(t, ""))
	if !strings.Contains(html, `data-testid="inspector-empty"`) {
		t.Error("the inspector shows an empty form rather than saying what to do")
	}
	if strings.Contains(html, `data-testid="state-name"`) {
		t.Error("a name field is offered for no state")
	}
}

// One rail, and the selection decides which panel is in it. The state panel
// does not claim an edge, and it does not render alongside the transition one.
func TestInspector_ShowsTheTransitionPanelForASelectedEdge(t *testing.T) {
	html := renderAgents(t, inspectorFixture(t, "edge:idle|on|SPOTTED|0"))
	if !strings.Contains(html, `data-testid="transition-inspector"`) {
		t.Error("a selected transition gets no panel")
	}
	if strings.Contains(html, `data-testid="state-inspector"`) {
		t.Error("the state inspector filled itself in for a transition")
	}
}

func TestInspector_FillsInTheSelectedState(t *testing.T) {
	data := inspectorFixture(t, "state:idle")
	data.SelectedState.Entry = []agent.ActionSpec{
		{Type: "dealDamage", Params: map[string]any{"amount": float64(5)}},
	}
	html := renderAgents(t, data)

	if !strings.Contains(html, `value="idle"`) {
		t.Error("the name field does not hold the state's name")
	}
	if !strings.Contains(html, `data-testid="entry-action-0"`) {
		t.Error("the entry action is not listed")
	}
	// The description is the registry's, so the row says what the action does
	// rather than only what it is called. Asserted inside the row: the same
	// text is on every option in the add dropdown, so a search over the whole
	// page passes with the row carrying nothing.
	row := elementWith(t, html, "entry-action-0")
	if !strings.Contains(row, "Decrement Health.hp") {
		t.Errorf("the action's description is missing from its row: %s", row)
	}
	if !strings.Contains(html, `value="5"`) {
		t.Error("the parameter's value is not shown")
	}
}

// Authored order is the order they run in, so sorting them would be sorting the
// logic.
func TestInspector_ListsActionsInAuthoredOrder(t *testing.T) {
	data := inspectorFixture(t, "state:idle")
	data.SelectedState.Entry = []agent.ActionSpec{
		{Type: "setPursueTarget"}, {Type: "attachComponent"}, {Type: "dealDamage"},
	}
	html := renderAgents(t, data)

	want := []string{"setPursueTarget", "attachComponent", "dealDamage"}
	at := 0
	for _, name := range want {
		i := strings.Index(html[at:], `data-action="`+name+`"`)
		if i < 0 {
			t.Fatalf("%s is out of order or missing; want %v", name, want)
		}
		at += i
	}
}

// The catalogue is the registry's, and the engine's for *this* project: a
// mapless project does not register computePath, and a machine using it would
// not load.
func TestInspector_OffersOnlyWhatTheRegistryHas(t *testing.T) {
	html := renderAgents(t, inspectorFixture(t, "state:idle"))
	add := elementWith(t, html, "add-entry-action")

	for _, want := range []string{"dealDamage", "attachComponent", "setPursueTarget"} {
		if !strings.Contains(add, `value="`+want+`"`) {
			t.Errorf("the catalogue is missing %s", want)
		}
	}
	// Each with its description, so what an action does is there while you are
	// choosing rather than only after you have chosen.
	if !strings.Contains(add, "dealDamage — Decrement Health.hp") {
		t.Errorf("the options carry no description: %s", add)
	}
	if strings.Contains(add, "computePath") {
		t.Error("the catalogue offers an action this project's engine does not register")
	}
	// Exactly the catalogue plus the prompt, so nothing is typed in beside it.
	if got := strings.Count(add, "<option"); got != len(inspectorFixture(t, "state:idle").Actions)+1 {
		t.Errorf("%d options for %d actions plus a prompt", got, len(inspectorFixture(t, "state:idle").Actions))
	}
}

func TestInspector_GeneratesTheFormFromTheParameterSchema(t *testing.T) {
	data := inspectorFixture(t, "state:idle")
	data.SelectedState.Entry = []agent.ActionSpec{
		{Type: "dealDamage", Params: map[string]any{"amount": float64(5)}},
		{Type: "attachComponent"},
	}
	html := renderAgents(t, data)

	// Each assertion on its own control. A search over the whole page passes
	// with the field it names carrying nothing — the mistake this epic has now
	// made four times.
	amount := section(t, html, `<input class="action__input mono"`, ">")
	if !strings.Contains(amount, `data-param-type="number"`) {
		t.Errorf("the number parameter does not say what it is: %s", amount)
	}
	// A text input, deliberately: an input type=number reports an empty value
	// for anything it cannot parse, so typing "1e" on the way to "1e5" and
	// tabbing out posted an empty value and cleared what was already there.
	if !strings.Contains(amount, `type="text"`) {
		t.Errorf("a number field that reports empty for half-typed input: %s", amount)
	}
	if !strings.Contains(amount, `inputmode="decimal"`) {
		t.Errorf("a number parameter offers no numeric keypad: %s", amount)
	}

	object := section(t, html, `<textarea`, ">")
	if !strings.Contains(object, `data-testid="input-entry-1-data"`) {
		t.Errorf("the object parameter is not a textarea: %s", object)
	}

	// The registered default is a placeholder, never a value: writing it in
	// would put the default into the file as though someone had chosen it, and
	// there would be no way to say "leave this alone".
	target := section(t, html, `data-testid="param-entry-0-target"`, "</label>")
	if !strings.Contains(target, `placeholder="$player"`) {
		t.Errorf("the registered default is not shown as a hint: %s", target)
	}
	if strings.Contains(target, `value="$player"`) {
		t.Errorf("the registered default was written in as a value: %s", target)
	}
	// An action with no parameters says so rather than rendering an empty box.
	data.SelectedState.Entry = []agent.ActionSpec{{Type: "setPursueTarget"}}
	if !strings.Contains(renderAgents(t, data), `data-testid="noparams-entry-0"`) {
		t.Error("an action with no parameters renders as nothing at all")
	}
}

// ValidateMachine checks that an action is registered and stops; it does not
// look at parameters. So this is Forge's rule, reported against the field and
// never blocking the save — inventing a rule the engine does not have is the
// line internal/forge/validation already draws.
func TestInspector_ReportsARequiredParameterAgainstItsField(t *testing.T) {
	data := inspectorFixture(t, "state:idle")
	data.SelectedState.Entry = []agent.ActionSpec{{Type: "dealDamage"}}
	html := renderAgents(t, data)

	if !strings.Contains(html, "amount is required") {
		t.Fatal("a required parameter left empty is not reported")
	}
	// Associated with its input, not merely near it: proximity is not
	// association, which is the pattern Epic 12 Story 7 established.
	id := section(t, html, `data-testid="problem-entry-0-amount"`, ">")
	if !strings.Contains(id, `id="`) {
		t.Errorf("the message has no id to point at: %s", id)
	}
	// The value, not merely the attribute: an aria-describedby pointing at an
	// id nothing carries is the same as none at all, and looks identical.
	wantID := paramProblemsID("entry-0", "amount")
	input := section(t, html, `<input class="action__input mono"`, ">")
	if !strings.Contains(input, `aria-describedby="`+wantID+`"`) {
		t.Errorf("the input does not point at its message: %s", input)
	}
	// And *not* marked invalid. This is a warning: the file is one the engine
	// accepts, ValidateMachine never looks at parameters, and the save is not
	// blocked — so aria-invalid would tell a screen reader something the screen
	// does not say. components.Blocking is what decides it.
	if strings.Contains(input, "aria-invalid") {
		t.Errorf("a warning marks the field invalid: %s", input)
	}
	if !strings.Contains(html, `id="`+wantID+`"`) {
		t.Errorf("nothing carries the id the input points at (%s)", wantID)
	}
	// The optional one is not reported, or every form arrives full of warnings.
	if strings.Contains(html, "target is required") {
		t.Error("an optional parameter was reported as required")
	}
	// And filling it in clears the message.
	data.SelectedState.Entry = []agent.ActionSpec{{Type: "dealDamage", Params: map[string]any{"amount": float64(1)}}}
	if strings.Contains(renderAgents(t, data), "amount is required") {
		t.Error("the message survives the value being supplied")
	}
}

// A machine written by hand may name an action the engine will refuse. The
// problem list says so; the inspector does not invent a description for it.
func TestInspector_SaysWhenAnActionIsNotRegistered(t *testing.T) {
	data := inspectorFixture(t, "state:idle")
	data.SelectedState.Entry = []agent.ActionSpec{{Type: "teleport"}}
	html := renderAgents(t, data)

	if !strings.Contains(html, `data-testid="entry-unknown-0"`) {
		t.Error("an unregistered action is shown as though it were fine")
	}
	if !strings.Contains(html, "engine registers") {
		t.Error("nothing says why it is marked")
	}
}

func TestInspector_OffersInitialOnlyWhereItWouldDoSomething(t *testing.T) {
	// canvasMachine's initial is idle.
	if html := renderAgents(t, inspectorFixture(t, "state:idle")); !strings.Contains(html, `data-testid="already-initial"`) {
		t.Error("the current initial state is offered the chance to become it")
	} else if strings.Contains(html, `data-testid="set-initial"`) {
		t.Error("an action that would do nothing is on screen")
	}

	html := renderAgents(t, inspectorFixture(t, "state:combat"))
	if !strings.Contains(html, `data-testid="set-initial"`) {
		t.Error("a state that is not the initial one cannot become it")
	}
	if !strings.Contains(html, "the machine starts in") {
		t.Error("the control does not say what it would change")
	}
}

// A nested state's initial belongs to the compound state that holds it, and the
// machine's is a different field. The control says which one it means.
func TestInspector_NamesTheContainerAnInitialBelongsTo(t *testing.T) {
	html := renderAgents(t, inspectorFixture(t, "state:combat.attacking"))
	if strings.Contains(html, `data-testid="set-initial"`) {
		t.Error("attacking is already what combat enters")
	}

	data := inspectorFixture(t, "state:combat.attacking")
	data.Machine.States["combat"].Initial = "elsewhere"
	html = renderAgents(t, data)
	if !strings.Contains(html, "the state combat enters") {
		t.Error("the control does not name the container it acts on")
	}
	if strings.Contains(html, "the machine starts in") {
		t.Error("a nested state's control claims to move the machine's initial")
	}
}

func TestInspector_OffersDeleteWithTheSameWarningAsTheCanvas(t *testing.T) {
	data := inspectorFixture(t, "state:combat")
	data.SelectedStateWarning = "Delete state combat? on SPOTTED from idle will be left pointing at a state that does not exist."
	html := renderAgents(t, data)

	if !strings.Contains(html, `data-testid="delete-state"`) {
		t.Error("the inspector does not offer to delete the state")
	}
	if !strings.Contains(html, "on SPOTTED from idle") {
		t.Error("the warning does not name what would dangle")
	}
}

// State names, action names and parameter values all come out of a JSON file.
func TestInspector_EscapesWhatItCarries(t *testing.T) {
	data := inspectorFixture(t, "state:idle")
	data.SelectedState.Entry = []agent.ActionSpec{
		{Type: "dealDamage", Params: map[string]any{"target": `"><script>x</script>`}},
	}
	html := renderAgents(t, data)

	if strings.Contains(html, "<script>x</script>") {
		t.Fatal("a parameter value reached the page as markup")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("nothing looks escaped; check what this asserts")
	}
}

// The checkbox branch, which the browser suite cannot reach: setTilePassable is
// the only builtin with a boolean parameter and it is map-gated, so the fixture
// project does not have it.
func TestInspector_RendersABooleanAsACheckboxThatSaysItsDefault(t *testing.T) {
	data := inspectorFixture(t, "state:idle")
	data.Actions = append(data.Actions, agent.ActionMeta{
		Name:        "setTilePassable",
		Description: "Set a tile's passability.",
		Params: []agent.ParamSchema{
			{Name: "passable", Type: "boolean", Required: true, Default: true},
		},
	})
	data.SelectedState.Entry = []agent.ActionSpec{{Type: "setTilePassable"}}
	html := renderAgents(t, data)

	box := section(t, html, `<input class="action__check"`, ">")
	if !strings.Contains(box, `type="checkbox"`) {
		t.Fatalf("a boolean is not a checkbox: %s", box)
	}
	// The message is associated here too. It was not: the checkbox branch
	// rendered the problem list with an id and pointed nothing at it, which is
	// exactly the failure aria-describedby exists to prevent, in the one branch
	// no test reached.
	wantID := paramProblemsID("entry-0", "passable")
	if !strings.Contains(box, `aria-describedby="`+wantID+`"`) {
		t.Errorf("the checkbox does not point at its message: %s", box)
	}
	if !strings.Contains(html, `id="`+wantID+`"`) {
		t.Errorf("nothing carries the id the checkbox points at (%s)", wantID)
	}
	// A checkbox cannot carry a placeholder, so the registered default is said
	// beside it — otherwise an absent boolean renders unchecked while the
	// engine would use true, and the form states the opposite of what happens.
	if !strings.Contains(html, "default true") {
		t.Error("a boolean's registered default is nowhere on the form")
	}
	// And it is unchecked, because that is what the file says. Asserted on the
	// attributes before the handler, which mentions evt.target.checked.
	attrs, _, _ := strings.Cut(box, "data-on:change")
	if strings.Contains(attrs, "checked") {
		t.Errorf("an absent boolean renders as though it were set: %s", attrs)
	}
	// The premise: a boolean that *is* set does render checked.
	data.SelectedState.Entry[0].Params = map[string]any{"passable": true}
	set := section(t, renderAgents(t, data), `<input class="action__check"`, ">")
	setAttrs, _, _ := strings.Cut(set, "data-on:change")
	if !strings.Contains(setAttrs, "checked") {
		t.Errorf("a boolean that is set does not render checked: %s", setAttrs)
	}
}

// An object parameter has to survive the round trip: file → textarea → post →
// file. It did not — a Go map printed as map[hp:3 max:10], which is not JSON,
// so touching an object parameter at all was refused and the only way to edit
// one was to select all and retype it.
func TestInspector_ShowsAnObjectParameterAsTheJSONItIs(t *testing.T) {
	data := inspectorFixture(t, "state:idle")
	data.SelectedState.Entry = []agent.ActionSpec{
		{Type: "attachComponent", Params: map[string]any{
			"data": map[string]any{"hp": float64(3), "max": float64(10)},
		}},
	}
	html := renderAgents(t, data)

	if strings.Contains(html, "map[") {
		t.Fatal("a parameter is shown as a Go value rather than as JSON")
	}
	if !strings.Contains(html, "&#34;hp&#34;:3") {
		t.Errorf("the object is not shown as the JSON the file holds")
	}
}

// A magnitude %v prints in exponent form is not a number a JSON file holds, and
// it would come back as one.
func TestInspector_ShowsALargeNumberWithoutAnExponent(t *testing.T) {
	data := inspectorFixture(t, "state:idle")
	data.SelectedState.Entry = []agent.ActionSpec{
		{Type: "dealDamage", Params: map[string]any{"amount": float64(1000000)}},
	}
	if got := renderAgents(t, data); !strings.Contains(got, `value="1000000"`) {
		t.Error("a large number is shown in exponent form")
	}
}

// The catalogue is closed and looks extensible. Both the plan and the story say
// the panel states that; for a while neither was true of the panel.
func TestInspector_SaysTheCatalogueIsClosed(t *testing.T) {
	html := renderAgents(t, inspectorFixture(t, "state:idle"))
	if !strings.Contains(html, `data-testid="catalogue-is-closed"`) {
		t.Fatal("nothing says a modder cannot add to this list")
	}
	// Once per state, not once per list.
	if got := strings.Count(html, `data-testid="catalogue-is-closed"`); got != 1 {
		t.Errorf("said %d times", got)
	}
}
