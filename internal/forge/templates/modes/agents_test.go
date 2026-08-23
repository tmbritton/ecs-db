package modes

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
	"github.com/tmbritton/ecs-db/internal/forge/machines"
	"github.com/tmbritton/ecs-db/internal/forge/machinevalidation"
	"github.com/tmbritton/ecs-db/internal/forge/project"
)

func renderAgents(t *testing.T, data Data) string {
	t.Helper()
	var buf bytes.Buffer
	if err := AgentsMode(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// A project with two mods, the later one shadowing the earlier's machine.
func agentsFixture() Data {
	data := modeFixture()
	data.HasMachines = true
	data.Machines = []project.Machine{
		{ID: "wander", Path: "/p/core/behaviors/wander.json", Mod: "core"},
		{ID: "door", Path: "/p/overlay/behaviors/door.json", Mod: "overlay", Overrides: true},
	}
	data.SelectedMachine = "/p/core/behaviors/wander.json"
	data.Machine = &agent.MachineDefinition{
		ID:      "wander",
		Initial: "idle",
		// Deliberately not alphabetical: authored order is what the file
		// records, and sorting would look identical on any other ordering.
		Context:      map[string]any{"speed": 2.0, "hp": float64(10)},
		ContextOrder: []string{"speed", "hp"},
		States:       map[string]*agent.StateNode{"idle": {ID: "wander.idle"}},
		StateOrder:   []string{"idle"},
	}
	data.Inspection = machines.Inspection{
		Computed: true,
		Manifest: map[string]string{"hp": "Health", "speed": "Motion"},
	}
	data.NewMachineID = "NewMachine"
	data.Chart = chart.Build(data.Machine, "")
	return data
}

func TestAgents_ListsEachMachineWithItsMod(t *testing.T) {
	html := renderAgents(t, agentsFixture())

	for _, want := range []string{
		`data-testid="machine-wander"`,
		`data-testid="machine-mod-wander"`,
		`data-testid="machine-door"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the list is missing %s", want)
		}
	}
	if !strings.Contains(html, ">overlay<") {
		t.Error("no row names the mod a machine came from")
	}
}

// The rule lives in project.loadMachines, derived from what the loader actually
// did. Reading it is the whole of the requirement; a second derivation here is
// the thing Epic 12 Story 5 deliberately did not write.
func TestAgents_TagsAnOverridingMachineAndNamesTheWinningMod(t *testing.T) {
	html := renderAgents(t, agentsFixture())

	if !strings.Contains(html, `data-testid="machine-override-door"`) {
		t.Fatal("the shadowing machine carries no override tag")
	}
	if strings.Contains(html, `data-testid="machine-override-wander"`) {
		t.Error("a machine that shadows nothing is tagged as an override")
	}
	// The row says which mod won, beside the tag — "override" alone reports
	// that something happened and not what.
	row := section(t, html, `data-testid="machine-door"`, "</a>")
	if !strings.Contains(row, ">override<") {
		t.Errorf("the tag is not on the row it belongs to: %s", row)
	}
	if !strings.Contains(row, "overlay") {
		t.Errorf("the row does not name the winning mod: %s", row)
	}
	if !strings.Contains(row, "overlay wins") {
		t.Errorf("the tag does not explain what it means: %s", row)
	}
}

func TestAgents_ShowsTheSourceLineForTheSelectedMachine(t *testing.T) {
	html := renderAgents(t, agentsFixture())

	if !strings.Contains(html, "wander.json") {
		t.Error("the header does not name the file")
	}
	if !strings.Contains(html, "XState v4 · round-trips with Stately") {
		t.Error("the header does not carry the source line the prototype shows")
	}
}

func TestAgents_ReportsValidity(t *testing.T) {
	html := renderAgents(t, agentsFixture())
	// The attribute as well as the sentence. The browser tests key on
	// data-valid, so a readout whose words change and whose attribute does not
	// would leave them asserting a constant.
	if !strings.Contains(html, `data-valid="true"`) {
		t.Error("a valid machine is not marked valid in the DOM")
	}
	if !strings.Contains(html, "saves &amp; hot-swaps into the running game") {
		t.Errorf("a valid machine does not say so:\n%s", section(t, html, `data-testid="machine-validity"`, "</span>"))
	}

	broken := renderAgents(t, brokenFixture(t))
	if !strings.Contains(broken, "2 problems") {
		t.Errorf("the count of problems is not reported:\n%s",
			section(t, broken, `data-testid="machine-validity"`, "</span>"))
	}
	if !strings.Contains(broken, `data-valid="false"`) {
		t.Error("a machine that does not validate is not marked invalid in the DOM")
	}
}

// brokenFixture is the agents fixture with two errors placed the way the server
// places them: one belonging to no state, one belonging to a node.
func brokenFixture(t *testing.T) Data {
	t.Helper()
	data := agentsFixture()
	errs := []agent.ValidationError{
		{MachineID: "wander", Message: "machine has child states but no initial state"},
		{
			MachineID: "wander", StateID: "wander.idle", StatePath: "idle", Field: "leap",
			Message: `entry action "leap" is not registered`,
		},
	}
	data.Inspection = machines.Inspection{Errors: errs}
	data.Problems = machinevalidation.Check(data.Machine, errs, data.Chart)
	return data
}

// Story 8's placement: an error about a state is rendered against that state,
// not glued into one list with "state <id>:" on the front. Only what belongs to
// no state is left at the top.
func TestAgents_PlacesEachProblemWhereItBelongs(t *testing.T) {
	broken := renderAgents(t, brokenFixture(t))

	top := section(t, broken, `data-testid="machine-problems"`, "</ul>")
	if !strings.Contains(top, "no initial state") {
		t.Errorf("the machine-level problem is not at the top: %s", top)
	}
	if strings.Contains(top, "leap") {
		t.Errorf("a problem about a state was left in the machine's list: %s", top)
	}
	// And the node that carries it is marked, so it is findable without being
	// selected — the whole reason the message is not simply listed.
	node := nodeSection(t, broken, "idle")
	if !strings.Contains(node, `data-invalid="true"`) {
		t.Errorf("the state carrying the problem is not marked: %s", node)
	}
	// Where the message itself renders is TestInspector_ShowsTheSelectedStates-
	// Problems'. This test is about placement, and asserting "leap is somewhere
	// on the page" would pass on the node's title attribute alone.
}

func TestAgents_ManifestMapsKeyToValueToComponent(t *testing.T) {
	html := renderAgents(t, agentsFixture())

	row := section(t, html, `data-testid="manifest-hp"`, "</div>")
	for _, want := range []string{"hp", "10", "Health"} {
		if !strings.Contains(row, want) {
			t.Errorf("the manifest row for hp is missing %q: %s", want, row)
		}
	}
	// Authored order, like every other list Forge renders out of a file. The
	// fixture's order is the reverse of alphabetical, so a sort shows up here
	// rather than passing by coincidence — and both rows are required to be
	// present first, because strings.Index answers -1 for a row that is gone
	// and -1 is less than everything.
	speed, hp := strings.Index(html, "manifest-speed"), strings.Index(html, "manifest-hp")
	if speed < 0 || hp < 0 {
		t.Fatalf("a manifest row is missing: speed=%d hp=%d", speed, hp)
	}
	if speed > hp {
		t.Error("the manifest is not in authored order")
	}
}

// The distinction the story exists for, at the layer that renders it.
func TestAgents_AnUncomputedManifestIsNotAnEmptyOne(t *testing.T) {
	empty := agentsFixture()
	empty.Machine.Context = nil
	empty.Machine.ContextOrder = nil
	empty.Inspection = machines.Inspection{Computed: true, Manifest: map[string]string{}}
	emptyHTML := renderAgents(t, empty)

	unavailable := agentsFixture()
	unavailable.Inspection = machines.Inspection{Errors: []agent.ValidationError{
		{MachineID: "wander", Message: "machine has child states but no initial state"},
	}}
	unavailableHTML := renderAgents(t, unavailable)

	emptyText := section(t, emptyHTML, `data-testid="manifest-empty"`, "</p>")
	unavailableText := section(t, unavailableHTML, `data-testid="manifest-unavailable"`, "</p>")

	if strings.Contains(emptyHTML, `data-testid="manifest-unavailable"`) {
		t.Error("a machine that seeds nothing is reported as not computed")
	}
	if strings.Contains(unavailableHTML, `data-testid="manifest-empty"`) {
		t.Error("a machine whose manifest could not be computed is reported as seeding nothing")
	}
	if emptyText == unavailableText {
		t.Fatal("the two absences read identically, which is the whole thing the panel must not do")
	}
	// And the unavailable one says *why*, rather than leaving a blank box.
	if !strings.Contains(unavailableText, "does not validate") {
		t.Errorf("the unavailable manifest does not name the reason: %s", unavailableText)
	}
}

func TestAgents_EmptyProjectOffersToCreateAMachine(t *testing.T) {
	data := agentsFixture()
	data.Machines = nil
	data.SelectedMachine = ""
	data.Machine = nil

	html := renderAgents(t, data)
	if !strings.Contains(html, `data-testid="agents-empty"`) {
		t.Fatal("a project with no machines renders no empty state")
	}
	if !strings.Contains(html, `data-testid="add-machine"`) {
		t.Error("the empty state does not offer to create a machine")
	}
	// One control, not two: a duplicated testid is a strict-mode violation
	// waiting for the first browser test that clicks it.
	if strings.Count(html, `data-testid="add-machine"`) != 1 {
		t.Errorf("the create control is rendered %d times", strings.Count(html, `data-testid="add-machine"`))
	}
}

func TestAgents_TheCreateControlProposesAFreeID(t *testing.T) {
	data := agentsFixture()
	data.NewMachineID = "NewMachine3"

	html := renderAgents(t, data)
	// With the closing quote: add=NewMachine is a prefix of add=NewMachine3,
	// so a bare Contains would pass on the id this test exists to rule out.
	if !strings.Contains(html, "add=NewMachine3&#39;)") {
		t.Errorf("the create control does not post the proposed id:\n%s",
			section(t, html, `data-testid="add-machine"`, "</button>"))
	}
}

func TestAgents_ReportsWhyAFileDidNotResolve(t *testing.T) {
	data := agentsFixture()
	data.MachineProblems = []project.Problem{
		{Path: "/p/core/behaviors/broken.json", Err: errString("unexpected end of JSON input")},
	}

	html := renderAgents(t, data)
	if !strings.Contains(html, `data-testid="machine-project-problems"`) {
		t.Fatal("nothing reports the file that did not load")
	}
	if !strings.Contains(html, "broken.json") || !strings.Contains(html, "unexpected end of JSON input") {
		t.Error("the problem does not name the file and the reason")
	}
}

// A machine stranded by a re-resolve is in no list, so the problem panel is the
// only place it can be reached from — and discarding is the only thing left to
// do with it.
func TestAgents_OffersToDiscardStrandedWork(t *testing.T) {
	data := agentsFixture()
	stranded := "/p/core/behaviors/door.json"
	data.MachineProblems = []project.Problem{
		{Path: stranded, Err: errString("has unsaved changes but no longer resolves as a machine")},
		{Path: "/p/core/behaviors/broken.json", Err: errString("unexpected end of JSON input")},
	}
	data.StrandedMachines = map[string]bool{stranded: true}

	html := renderAgents(t, data)
	if !strings.Contains(html, `data-testid="discard-stranded-door.json"`) {
		t.Fatal("stranded work has no control to give it up")
	}
	if strings.Contains(html, `data-testid="discard-stranded-broken.json"`) {
		t.Error("a file that never loaded is offered a discard, which would do nothing")
	}
	if !strings.Contains(html, "/forge/agents/discard") {
		t.Error("the discard control posts nowhere")
	}
}

func TestMachineSource_NamesTheModAndTheFile(t *testing.T) {
	data := agentsFixture()
	if got := machineSource(data); got != "core/wander.json" {
		t.Errorf("machineSource = %q, want core/wander.json", got)
	}

	// A path the resolved set does not know — a stranded machine — still reads
	// as something rather than as an empty header.
	data.SelectedMachine = "/p/core/behaviors/gone.json"
	if got := machineSource(data); got != "gone.json" {
		t.Errorf("machineSource = %q, want gone.json", got)
	}
}

func TestValidityLine_CountsInSingularAndPlural(t *testing.T) {
	one := Data{Inspection: machines.Inspection{Errors: []agent.ValidationError{{Message: "x"}}}}
	if got := validityLine(one); !strings.Contains(got, "1 problem") || strings.Contains(got, "1 problems") {
		t.Errorf("one problem reads %q", got)
	}
	two := Data{Inspection: machines.Inspection{Errors: []agent.ValidationError{{Message: "x"}, {Message: "y"}}}}
	if got := validityLine(two); !strings.Contains(got, "2 problems") {
		t.Errorf("two problems read %q", got)
	}
	// The zero value is neither valid nor a count of nothing: it is a machine
	// nobody has checked, which is what a render with no session holds.
	if got := validityLine(Data{}); strings.Contains(got, "0 problem") {
		t.Errorf("an unchecked machine reads as a count: %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// A machine stranded by a re-resolve can be the *only* machine, and then there
// is no editor to hang the problem list from. The panel and the discard control
// have to be outside it, or the promise to keep unsaved work is a promise to
// keep it somewhere unreachable.
func TestAgents_ReachesStrandedWorkWhenNothingResolves(t *testing.T) {
	data := agentsFixture()
	stranded := "/p/core/behaviors/wander.json"
	data.Machines = nil
	data.SelectedMachine = ""
	data.Machine = nil
	data.MachineProblems = []project.Problem{
		{Path: stranded, Err: errString("has unsaved changes but no longer resolves as a machine")},
	}
	data.StrandedMachines = map[string]bool{stranded: true}

	html := renderAgents(t, data)
	if !strings.Contains(html, `data-testid="machine-project-problems"`) {
		t.Fatal("with no machine selected there is nowhere the problem is reported")
	}
	if !strings.Contains(html, `data-testid="discard-stranded-wander.json"`) {
		t.Error("stranded work cannot be reached when it is the only machine")
	}
	// And the empty state does not claim there is nothing here.
	if strings.Contains(html, "declares no behaviour machines yet") {
		t.Error("the page says the project has no machines while holding unsaved work on one")
	}
	if !strings.Contains(html, `data-testid="agents-empty-stranded"`) {
		t.Error("the empty state does not say why nothing resolved")
	}
}

// The refusal has to render where the control is. The create button is in the
// empty state, and a project whose behaviours directory does not exist yet —
// which project.ResolveMachines calls a normal state for a project under
// construction — is refused by the session with nowhere to say so.
func TestAgents_ReportsARefusedEditWithNoMachineSelected(t *testing.T) {
	data := agentsFixture()
	data.Machines = nil
	data.SelectedMachine = ""
	data.Machine = nil
	data.Problem = "machines: atomicfile: writing NewMachine.json: no such file or directory"

	html := renderAgents(t, data)
	if !strings.Contains(html, `data-testid="edit-problem"`) {
		t.Fatal("an edit refused with no machine selected is reported nowhere")
	}
	if !strings.Contains(html, "no such file or directory") {
		t.Error("the reason is not shown")
	}
}

// The page stream patches an element when its markup changed. ValidateMachine
// produces its errors by ranging maps, so an unsorted list makes the mode
// content — the id and filename inputs included — get replaced on every tick
// for as long as a machine is invalid.
func TestAgents_RenderTheSameForAnUnchangedInvalidMachine(t *testing.T) {
	errs := []agent.ValidationError{
		{MachineID: "wander", Field: "hp", Message: `context key "hp" does not match any component field`},
		{MachineID: "wander", Field: "speed", Message: `context key "speed" does not match any component field`},
		{MachineID: "wander", Field: "aggro", Message: `context key "aggro" does not match any component field`},
		{MachineID: "wander", StateID: "idle", Message: `unknown action "leap"`},
	}
	data := agentsFixture()
	data.Inspection = machines.Inspection{Errors: errs}
	first := renderAgents(t, data)

	// The same errors, arriving in a different order every time — which is what
	// repeated calls to ValidateMachine on one machine actually produce. Every
	// rotation, so the check does not depend on which permutation was picked.
	for i := 1; i < len(errs); i++ {
		rotated := append(append([]agent.ValidationError(nil), errs[i:]...), errs[:i]...)
		data.Inspection = machines.Inspection{Errors: rotated}
		if got := renderAgents(t, data); got != first {
			t.Fatalf("rotation %d renders differently, so the stream re-patches the editor on every tick", i)
		}
	}
}

// The mod-choice branch of the create control: which mod a machine belongs to
// decides which one can override it later, so the control asks rather than
// picking — and it has to carry the proposed id the same way the button does.
func TestAgents_AskingWhichModCarriesTheProposedID(t *testing.T) {
	data := agentsFixture()
	data.MachineMods = []project.Mod{
		{Name: "core", Behaviors: "/p/core/behaviors"},
		{Name: "overlay", Behaviors: "/p/overlay/behaviors"},
	}
	data.NewMachineID = "NewMachine2"

	html := renderAgents(t, data)
	if !strings.Contains(html, `data-testid="add-machine-mod"`) {
		t.Fatal("a project with two mods that can hold a machine does not ask which")
	}
	if strings.Contains(html, `data-testid="add-machine"`) {
		t.Error("both create controls are rendered")
	}
	if !strings.Contains(html, "add=NewMachine2&amp;mod=") {
		t.Errorf("the mod-choice control does not carry the proposed id:\n%s",
			section(t, html, `data-testid="add-machine-mod"`, "</select>"))
	}
	// The mod names are the options, behind a placeholder — so the dropdown's
	// initial state is not itself a choice.
	for _, want := range []string{">choose a mod…<", ">core<", ">overlay<"} {
		if !strings.Contains(html, want) {
			t.Errorf("the mod dropdown is missing %s", want)
		}
	}
}

// The header's source line, pinned here as well as in the browser: renaming it
// should fail a go test rather than only time out a Playwright locator.
func TestAgents_HeaderNamesTheFileAndTheFormat(t *testing.T) {
	html := renderAgents(t, agentsFixture())
	if !strings.Contains(html, `data-testid="machine-source"`) {
		t.Error("the header has no source readout")
	}
	if !strings.Contains(html, "core/wander.json") {
		t.Error("the source readout does not name the mod and the file")
	}
	// The manifest says it is read-only and where the values live — the half of
	// ENTS's rule that the first version of this panel did not follow.
	hint := section(t, html, `data-testid="manifest-source"`, "</p>")
	if !strings.Contains(hint, "read-only") {
		t.Errorf("the manifest does not say it is read-only: %s", hint)
	}
}
