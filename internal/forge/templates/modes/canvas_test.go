package modes

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
)

// canvasFixture is a machine with everything the canvas has to draw: a compound
// state with children, an initial at each level, a guard, an after, a
// self-transition and a history node.
const canvasMachine = `{
  "id": "wander",
  "initial": "idle",
  "states": {
    "idle": {
      "meta": { "forge": { "x": 40.1, "y": 20.2 } },
      "entry": ["setAnimation"],
      "on": {
        "SPOTTED": [{ "target": "combat" }],
        "POKED": [{ "target": "combat.attacking", "cond": "isAngry" }],
        "NUDGED": [{ "actions": ["bump"] }]
      },
      "after": { "500": [{ "target": "idle" }] }
    },
    "combat": {
      "initial": "attacking",
      "states": {
        "attacking": { "on": { "LOST": [{ "target": "idle" }] } },
        "hist": { "type": "history", "history": "deep" }
      }
    }
  }
}`

func canvasFixture(t *testing.T, src, sel string) Data {
	t.Helper()
	def, err := agent.ParseMachine([]byte(src))
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	data := agentsFixture()
	data.Machine = def
	data.Chart = chart.Build(def, sel)
	return data
}

func renderCanvas(t *testing.T, src, sel string) string {
	t.Helper()
	return renderAgents(t, canvasFixture(t, src, sel))
}

// The id every one of the browser tests waits on. Pinned here too, so renaming
// it fails in seconds rather than as nine thirty-second timeouts.
func TestCanvas_TheCanvasCarriesItsTestID(t *testing.T) {
	if !strings.Contains(renderCanvas(t, canvasMachine, ""), `data-testid="statechart"`) {
		t.Error("the canvas has no test id, and the browser suite selects on it")
	}
}

func TestCanvas_DrawsANodePerState(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")
	for _, want := range []string{
		`data-testid="state-idle"`,
		`data-testid="state-combat"`,
		`data-testid="state-combat.attacking"`,
		`data-testid="state-combat.hist"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("no node %s", want)
		}
	}
}

// A flat drawing of a tree is a different machine, so the child's markup has to
// be inside the parent's element and not merely somewhere on the page.
func TestCanvas_ChildrenAreNestedInsideTheirParent(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")

	combat := nodeElement(t, html, "combat")
	for _, child := range []string{"combat.attacking", "combat.hist"} {
		if !strings.Contains(combat, `data-testid="state-`+child+`"`) {
			t.Errorf("%s is not drawn inside combat", child)
		}
	}
	// And a top-level sibling is not, or "nested" would mean nothing.
	if strings.Contains(combat, `data-testid="state-idle"`) {
		t.Error("a top-level state is drawn inside another one")
	}
}

func TestCanvas_MarksTheInitialStateAtEachLevel(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")
	idle := nodeSection(t, html, "idle")
	if !strings.Contains(idle, `data-initial="true"`) || !strings.Contains(idle, "◉") {
		t.Error("the machine's initial state is not tagged")
	}
	combat := nodeSection(t, html, "combat")
	if !strings.Contains(combat, `data-initial="false"`) {
		t.Error("a state that is not initial is not marked as such")
	}
	attacking := nodeSection(t, html, "combat.attacking")
	if !strings.Contains(attacking, `data-initial="true"`) {
		t.Error("a compound state's own initial child is not tagged")
	}
}

func TestCanvas_LabelsEveryTransition(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")
	for id, label := range map[string]string{
		"idle|on|SPOTTED|0":          ">SPOTTED<",
		"idle|on|POKED|0":            ">POKED [isAngry]<",
		"idle|after|500|0":           ">after 500<",
		"combat.attacking|on|LOST|0": ">LOST<",
	} {
		if !strings.Contains(html, `data-testid="edge-`+id+`"`) {
			t.Errorf("no edge %s", id)
		}
		if !strings.Contains(html, label) {
			t.Errorf("edge %s is not labelled %s", id, label)
		}
	}
}

// A guard is the difference between "this happens" and "this might", and the
// dashed stroke that says so is only a stroke.
func TestCanvas_AGuardedEdgeIsDistinguishable(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")
	guarded := edgeSection(t, html, "idle|on|POKED|0")
	plain := edgeSection(t, html, "idle|on|SPOTTED|0")

	if !strings.Contains(guarded, `data-guard="isAngry"`) {
		t.Error("the guarded edge does not name its guard")
	}
	if !strings.Contains(plain, `data-guard=""`) {
		t.Error("an unconditional edge does not say it has no guard")
	}
	if !strings.Contains(html, "chart-edge--guarded") {
		t.Error("nothing draws the guarded edge differently")
	}
}

func TestCanvas_ADanglingEdgeIsDrawnAndNamed(t *testing.T) {
	html := renderCanvas(t, `{
	  "id": "broken", "initial": "a",
	  "states": { "a": { "on": { "GO": [{ "target": "ghost" }] } } }
	}`, "")

	edge := edgeSection(t, html, "a|on|GO|0")
	if !strings.Contains(edge, `data-dangling="true"`) {
		t.Error("the dangling edge is not marked")
	}
	if !strings.Contains(edge, "ghost") {
		t.Error("the label does not name the state that is missing")
	}
	if !strings.Contains(html, "chart-edge--dangling") {
		t.Error("nothing draws the dangling edge differently")
	}
	// And the ordinary case says false rather than omitting the attribute, so
	// a test can tell "not dangling" from "not rendered".
	ok := renderCanvas(t, canvasMachine, "")
	if !strings.Contains(edgeSection(t, ok, "idle|on|SPOTTED|0"), `data-dangling="false"`) {
		t.Error("a sound edge does not say it is sound")
	}
}

func TestCanvas_HistoryNodesAreDrawnAsHistory(t *testing.T) {
	hist := nodeSection(t, renderCanvas(t, canvasMachine, ""), "combat.hist")
	if !strings.Contains(hist, `data-kind="history"`) {
		t.Error("the history node is drawn as an ordinary state")
	}
	if !strings.Contains(hist, ">H*<") {
		t.Error("a deep history node is not marked deep")
	}
}

// Selecting a node posts rather than navigating. It was a link, and selecting a
// node in an editor should not reload the page — and it is not a signal either,
// because the selection decides what the *inspector* renders, which only the
// server can do. See server.pageStates.
func TestCanvas_SelectionIsARequestAndNotANavigation(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")

	want := `data-on:click="@post(&#39;/forge/agents/select?sel=state%3Acombat.attacking&#39;)"`
	if !strings.Contains(html, want) {
		t.Errorf("nothing selects the nested state; wanted\n%s", want)
	}
	if !strings.Contains(html, `sel=edge%3Aidle%7Con%7CSPOTTED%7C0`) {
		t.Error("nothing selects an edge")
	}
	// And the machine is not in it: the server knows which machine this page is
	// showing, so the selection only has to say what.
	if strings.Contains(html, `/forge/agents/select?machine=`) {
		t.Error("the selection carries a machine it does not need")
	}
}

func TestCanvas_SelectingANodeMarksExactlyThatNode(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "state:combat.attacking")

	if got := strings.Count(html, `data-selected="true"`); got != 1 {
		t.Errorf("%d things are marked selected, want 1", got)
	}
	if !strings.Contains(nodeSection(t, html, "combat.attacking"), `data-selected="true"`) {
		t.Error("the selected node is not the one that is marked")
	}
	if !strings.Contains(html, `data-testid="canvas-selection"`) ||
		!strings.Contains(html, ">state combat.attacking<") {
		t.Error("the selection is not readable from the DOM, only visible")
	}
}

func TestCanvas_SelectingAnEdgeMarksTheEdgeAndNotTheNode(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "edge:idle|on|SPOTTED|0")

	if got := strings.Count(html, `data-selected="true"`); got != 1 {
		t.Errorf("%d things are marked selected, want 1", got)
	}
	if !strings.Contains(edgeSection(t, html, "idle|on|SPOTTED|0"), `data-selected="true"`) {
		t.Error("the edge is not the thing marked")
	}
	if !strings.Contains(html, ">transition idle · SPOTTED<") {
		t.Error("the readout does not say which transition is selected")
	}
}

// A selection naming nothing is what a bookmark becomes the moment a state is
// renamed. It is dropped, and the page says nothing is selected rather than
// refusing to render.
func TestCanvas_ASelectionNamingNothingIsDropped(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "state:gone")
	if strings.Contains(html, `data-selected="true"`) {
		t.Error("something is marked selected")
	}
	if !strings.Contains(html, ">nothing selected<") {
		t.Error("the readout does not say the selection went nowhere")
	}
	// The ground is still there — it is what double-click and right-click land
	// on — and it is still a button, because the tag must not change with the
	// selection: a patch that swapped the element would drop keyboard focus to
	// <body> every time somebody cleared a selection.
	//
	// What changes is what it says it does. With nothing selected there is no
	// click handler and the name is the surface rather than the action.
	ground := section(t, html, `data-testid="canvas-ground"`, ">")
	if strings.Contains(ground, "href=") {
		t.Errorf("the ground is a link again: %s", ground)
	}
	if strings.Contains(ground, "data-on:click") {
		t.Errorf("the ground offers to clear a selection that is not there: %s", ground)
	}
	if !strings.Contains(ground, `aria-label="Canvas"`) {
		t.Errorf("the ground is unnamed: %s", ground)
	}
}

func TestCanvas_TheGroundClearsTheSelection(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "state:idle")

	ground := section(t, html, `class="chart__ground"`, ">")
	if !strings.Contains(ground, `data-on:click="@post(&#39;/forge/agents/select&#39;)"`) {
		t.Errorf("the ground does not clear the selection: %s", ground)
	}
	// It is a control with no text, so it needs a name to be one at all.
	if !strings.Contains(ground, `aria-label="Clear selection"`) {
		t.Errorf("the ground is an unnamed control: %s", ground)
	}
}

// A machine that does not validate is exactly the one someone opened the editor
// to fix.
func TestCanvas_AnInvalidMachineStillDraws(t *testing.T) {
	html := renderCanvas(t, `{
	  "id": "wrong",
	  "context": { "nonesuch": 1 },
	  "states": { "a": { "entry": ["noSuchAction"], "on": { "GO": [{ "target": "nowhere" }] } } }
	}`, "")

	if !strings.Contains(html, `data-testid="state-a"`) {
		t.Error("the canvas refused to draw a machine that does not validate")
	}
	if !strings.Contains(html, `data-testid="edge-a|on|GO|0"`) {
		t.Error("the broken transition was dropped rather than drawn")
	}
}

func TestCanvas_AMachineWithNoStatesSaysSo(t *testing.T) {
	html := renderCanvas(t, `{"id":"empty"}`, "")
	if !strings.Contains(html, `data-testid="canvas-empty"`) {
		t.Error("a machine with no states draws nothing and says nothing")
	}
	if strings.Contains(html, `class="chart__scroll"`) {
		t.Error("an empty canvas is still drawn")
	}
}

// The chart is on a two-second stream that patches an element only when its
// markup changed. Anything non-deterministic in here does not merely flicker —
// it re-sends the whole mode content forever, inputs and all.
func TestCanvas_TwoRendersAreIdentical(t *testing.T) {
	data := canvasFixture(t, canvasMachine, "state:combat.attacking")
	first := renderAgents(t, data)
	for i := 0; i < 50; i++ {
		// Rebuilt each time, not rendered from one build: a map range in the
		// builder is exactly as fatal as one in the template.
		again := canvasFixture(t, canvasMachine, "state:combat.attacking")
		if got := renderAgents(t, again); got != first {
			t.Fatalf("render %d differs from the first", i)
		}
	}
}

// State names come out of a JSON file and may contain anything a JSON key may.
func TestCanvas_StateNamesAreEscaped(t *testing.T) {
	html := renderCanvas(t, `{
	  "id": "esc", "initial": "a\"><script>x</script>",
	  "states": { "a\"><script>x</script>": {} }
	}`, "")

	if strings.Contains(html, "<script>x</script>") {
		t.Fatal("a state name reached the page as markup")
	}
	// Named exactly, so this cannot pass because the name went missing
	// altogether: the escaped form has to be on the page, in the test id, in
	// the label, and percent-encoded in the link that selects it.
	escaped := "a&#34;&gt;&lt;script&gt;x&lt;/script&gt;"
	if !strings.Contains(html, `data-testid="state-`+escaped+`"`) {
		t.Error("the test id is not the escaped name")
	}
	if !strings.Contains(html, `>`+escaped+` `) {
		t.Error("the label is not the escaped name")
	}
	if !strings.Contains(html, "sel=state%3Aa%22%3E%3Cscript%3Ex%3C%2Fscript%3E") {
		t.Error("the selection link does not carry the name percent-encoded")
	}
}

// nodeElement is a node's whole element, children included.
func nodeElement(t *testing.T, html, path string) string {
	t.Helper()
	return elementWith(t, html, "state-"+path)
}

// elementWith is the whole element carrying a test id, found by matching the
// div that opens it against the one that closes it.
//
// Balanced, and that is the point: a section that ran from a test id to the
// next "</div></div>" swallowed everything after the element as well, so an
// assertion "inside the row" was really an assertion about the rest of the
// page — which is how a description test passed against text that was only in
// the dropdown below it.
func elementWith(t *testing.T, html, testid string) string {
	t.Helper()
	i := strings.Index(html, `data-testid="`+testid+`"`)
	if i < 0 {
		t.Fatalf("no element with test id %q", testid)
	}
	start := strings.LastIndex(html[:i], "<div")
	if start < 0 {
		t.Fatalf("%s is not in a div", testid)
	}
	depth, j := 0, start
	for j < len(html) {
		switch {
		case strings.HasPrefix(html[j:], "<div"):
			depth++
			j += 4
		case strings.HasPrefix(html[j:], "</div>"):
			depth--
			j += 6
			if depth == 0 {
				return html[start:j]
			}
		default:
			j++
		}
	}
	t.Fatalf("%s is never closed", testid)
	return ""
}

// nodeSection is one node's own markup: the box's attributes and the header
// inside it, stopping before any child node.
//
// From the start of the element rather than from the test id, because templ
// writes attributes in source order and a section starting at the test id would
// miss every attribute declared above it — the mistake Story 3's override test
// made and its review caught.
func nodeSection(t *testing.T, html, path string) string {
	t.Helper()
	i := strings.Index(html, `data-testid="state-`+path+`"`)
	if i < 0 {
		t.Fatalf("no node %s", path)
	}
	start := strings.LastIndex(html[:i], "<div")
	if start < 0 {
		t.Fatalf("node %s is not in an element", path)
	}
	end := strings.Index(html[start:], "</button>")
	if end < 0 {
		t.Fatalf("node %s has no header", path)
	}
	return html[start : start+end]
}

// edgeSection is one edge label's element.
func edgeSection(t *testing.T, html, id string) string {
	t.Helper()
	i := strings.Index(html, `data-testid="edge-`+id+`"`)
	if i < 0 {
		t.Fatalf("no edge %s", id)
	}
	// Back to the start of the tag, so attributes written before the test id
	// are in the section too — the mistake Story 3's override test made.
	start := strings.LastIndex(html[:i], "<button")
	if start < 0 {
		t.Fatalf("edge %s is not in an element", id)
	}
	end := strings.Index(html[start:], "</button>")
	if end < 0 {
		t.Fatalf("edge %s has no closing tag", id)
	}
	return html[start : start+end]
}

// Coordinates go into markup, and float64 arithmetic on recorded positions does
// not stay short: a compound state at x 0.1 holding a child at x 0.2 sizes the
// canvas to 224.29999999999998. Deterministic, so the stream's patch
// suppression was never at risk — but it is noise in every frame on the wire
// and in every diff, and Story 5 writes dragged coordinates into exactly the
// field this comes from.
//
// The nested case specifically. An earlier version put a fraction on one atomic
// state, where the sums happen to come out exact, and passed with the rounding
// removed entirely.
func TestCanvas_CoordinatesArePrintedShort(t *testing.T) {
	html := renderCanvas(t, `{
	  "id": "frac", "initial": "outer",
	  "states": {
	    "outer": {
	      "meta": { "forge": { "x": 0.1, "y": 0.2 } },
	      "initial": "inner",
	      "states": { "inner": { "meta": { "forge": { "x": 0.2, "y": 0.1 } } } }
	    }
	  }
	}`, "")

	long := regexp.MustCompile(`(\d+\.\d{2,})`)
	for _, m := range long.FindAllString(html, -1) {
		t.Errorf("a coordinate reached the page as %q", m)
	}
	// The premise, stated twice, because this passed once for the wrong reason:
	// the recorded fraction has to reach the page, and it has to flow through
	// the sums that size the box and the canvas — which is where the chart's
	// own rounding does not reach.
	if !strings.Contains(html, "left:0.1px") {
		t.Error("the recorded fractional position did not reach the page at all")
	}
	// The literal moves with chart.nodeW, which this sum is built from — it is
	// in another package and unexported, so there is nothing to reference. What
	// is being asserted is the ".3", not the 240.
	if !strings.Contains(html, "width:240.3px") {
		t.Errorf("the canvas is not sized from the fractional sum; check what this asserts")
	}
}

// The kind is what the stylesheet draws a compound box, a dashed parallel
// border, a final state's double rule and a history circle from. The data
// attribute beside it is for tests and says nothing to a browser.
func TestCanvas_TheNodeKindIsInItsClass(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")
	for path, kind := range map[string]string{
		"idle":        "chart-node--atomic",
		"combat":      "chart-node--compound",
		"combat.hist": "chart-node--history",
	} {
		if !strings.Contains(nodeSection(t, html, path), kind) {
			t.Errorf("%s is not drawn as %s", path, kind)
		}
	}
}

// The borders that tell the kinds apart are only borders, so the name is in the
// title too — which is also the only form of it a screen reader reaches.
func TestCanvas_EachKindOfStateIsNamed(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")
	for path, title := range map[string]string{
		"idle":        `title="state"`,
		"combat":      `title="compound state"`,
		"combat.hist": `title="deep history"`,
	} {
		if !strings.Contains(nodeSection(t, html, path), title) {
			t.Errorf("%s does not carry %s", path, title)
		}
	}

	shallow := renderCanvas(t, `{
	  "id": "sh", "initial": "c",
	  "states": { "c": { "initial": "h", "states": { "h": { "type": "history" } } } }
	}`, "")
	if !strings.Contains(nodeSection(t, shallow, "c.h"), `title="shallow history"`) {
		t.Error("a shallow history node is named as a deep one, or not named")
	}
	if !strings.Contains(shallow, ">H<") {
		t.Error("a shallow history node is not marked H")
	}
}

// A self-transition is drawn as an arc over its own node, and the height of
// that arc is what keeps two of them apart. Nothing else in the render reads
// Arc, so nothing else would notice it being dropped.
func TestCanvas_ALoopIsDrawnAsAnArc(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")
	i := strings.Index(html, `data-testid="edge-line-idle|on|NUDGED|0"`)
	if i < 0 {
		t.Fatal("no line for the self-transition")
	}
	line := html[strings.LastIndex(html[:i], "<path"):i]
	if !strings.Contains(line, " C ") {
		t.Fatalf("the self-transition is drawn as a straight line: %s", line)
	}
	// The control points are above the endpoints, which is what an arc is.
	d := regexp.MustCompile(`d="M ([\d.-]+) ([\d.-]+) C ([\d.-]+) ([\d.-]+)`).FindStringSubmatch(line)
	if d == nil {
		t.Fatalf("could not read the path out of %s", line)
	}
	y1, err := strconv.ParseFloat(d[2], 64)
	if err != nil {
		t.Fatal(err)
	}
	cy, err := strconv.ParseFloat(d[4], 64)
	if err != nil {
		t.Fatal(err)
	}
	if cy >= y1 {
		t.Errorf("the control point at y=%v is not above the endpoint at y=%v", cy, y1)
	}
}

// The readout is prose; this is the same fact in a form something can read
// without parsing a sentence, which is what Stories 6 and 7 will want when they
// render an inspector for whatever is selected.
func TestCanvas_TheSelectionIsOnTheWrapperToo(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "edge:idle|on|SPOTTED|0")
	wrap := section(t, html, `class="chart-wrap"`, ">")
	if !strings.Contains(wrap, `data-selection="edge:idle|on|SPOTTED|0"`) {
		t.Errorf("the wrapper does not carry the selection: %s", wrap)
	}
	empty := renderCanvas(t, canvasMachine, "state:gone")
	if !strings.Contains(section(t, empty, `class="chart-wrap"`, ">"), `data-selection=""`) {
		t.Error("a dropped selection is not reported as empty")
	}
}

// ── direct manipulation ───────────────────────────────────────────────────────

func TestCanvas_EveryNodeHasAPortToDragFrom(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")
	for _, path := range []string{"idle", "combat", "combat.attacking"} {
		if !strings.Contains(html, `data-testid="port-`+path+`"`) {
			t.Errorf("%s has no port, so nothing can be connected from it", path)
		}
	}
	// A history node is not a source: it is where a compound state resumes, and
	// a transition out of one is not a thing XState models.
	if strings.Contains(html, `data-testid="port-combat.hist"`) {
		t.Error("a history node was given an output port")
	}
}

// The whole JS boundary, in one attribute: the module dispatches an event
// carrying what the drag meant, and this turns it into the same @post every
// other control uses.
func TestCanvas_TheDropHandlerIsBoundToTheCanvas(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")
	wrap := section(t, html, `class="chart-wrap"`, ">")

	if !strings.Contains(wrap, "data-on:canvasdrop") {
		t.Fatalf("nothing listens for a completed drag: %s", wrap)
	}
	// Both kinds, and each to its own endpoint — a move is a state edit and a
	// connection is a transition.
	if !strings.Contains(wrap, "/forge/agents/state?machine=") || !strings.Contains(wrap, "&amp;move=") {
		t.Error("a completed move posts nowhere")
	}
	if !strings.Contains(wrap, "/forge/agents/transition?op=connect&amp;machine=") || !strings.Contains(wrap, "&amp;from=") {
		t.Error("a completed connection posts nowhere")
	}
	// A delta, not a position: only the server knows the offsets between where
	// a box is drawn and the coordinate the file records.
	if !strings.Contains(wrap, "dx=") || !strings.Contains(wrap, "dy=") {
		t.Error("the move posts a position rather than a delta")
	}
}

func TestCanvas_EmptySpaceAddsAndOffersToAdd(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")
	ground := section(t, html, `data-testid="canvas-ground"`, ">")

	if !strings.Contains(ground, "data-on:dblclick") || !strings.Contains(ground, "add=1") {
		t.Errorf("double-clicking empty space adds nothing: %s", ground)
	}
	// offsetX on the ground, which is inset over the chart, so the coordinate
	// arrives in the space the chart draws in.
	if !strings.Contains(ground, "evt.offsetX") {
		t.Error("the new state is not placed where the pointer was")
	}
	if !strings.Contains(ground, "data-on:contextmenu") {
		t.Error("empty space offers no menu")
	}
}

// One element and not two. A separate overlay for the pointer gestures sat on
// top of the ground link and swallowed every click meant for it — the same
// defect the node layer had in Story 4, reintroduced with a different element,
// and invisible to everything except a browser.
func TestCanvas_TheGroundIsTheOnlyThingOverTheCanvas(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "state:idle")
	if n := strings.Count(html, `class="chart__ground"`); n != 1 {
		t.Errorf("%d full-canvas overlays; one of them is on top of the other", n)
	}
	ground := section(t, html, `data-testid="canvas-ground"`, ">")
	// The same element does all three jobs.
	for _, want := range []string{"data-on:click", "data-on:dblclick", "data-on:contextmenu"} {
		if !strings.Contains(ground, want) {
			t.Errorf("the ground is missing %s: %s", want, ground)
		}
	}
}

func TestCanvas_RightClickingOffersAMenuOnEachKindOfThing(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")

	node := nodeSection(t, html, "idle")
	if !strings.Contains(node, "data-on:contextmenu") || !strings.Contains(node, "target=state") {
		t.Errorf("a state offers no menu: %s", node)
	}
	edge := edgeSection(t, html, "idle|on|SPOTTED|0")
	if !strings.Contains(edge, "data-on:contextmenu") || !strings.Contains(edge, "target=edge") {
		t.Errorf("a transition offers no menu: %s", edge)
	}
	// preventDefault, or the browser's own menu covers ours and none of this is
	// reachable at all.
	if !strings.Contains(node, "evt.preventDefault()") {
		t.Error("the browser's own menu is not suppressed")
	}
	// Viewport coordinates: offsetX on a node is relative to the node, so a
	// menu asked for on one would open at the corner of the canvas.
	if !strings.Contains(node, "evt.clientX") {
		t.Error("the menu is positioned from the wrong origin")
	}
}

func TestCanvas_TheMenuRendersWhatItIsAbout(t *testing.T) {
	data := canvasFixture(t, canvasMachine, "")
	data.CanvasMenu = CanvasMenu{Kind: "state", State: "combat.attacking", X: 120, Y: 80, Open: true}
	data.CanvasMenuWarning = "Delete state combat.attacking? on POKED from idle will be left pointing at a state that does not exist."
	html := renderAgents(t, data)

	if !strings.Contains(html, `data-testid="canvas-menu"`) {
		t.Fatal("the menu did not render")
	}
	if !strings.Contains(html, "left:120px;top:80px") {
		t.Error("the menu is not where the pointer was")
	}
	for _, want := range []string{"Rename…", "Set as initial", "Delete state"} {
		if !strings.Contains(html, want) {
			t.Errorf("the state menu is missing %q", want)
		}
	}
	// The warning is the server's, and it names what would break rather than
	// counting it.
	if !strings.Contains(html, "on POKED from idle") {
		t.Error("deleting does not say what it would leave dangling")
	}
	// Clicking anywhere else closes it, and so does Escape — both at window
	// scope, rather than through a full-viewport backdrop that blocks the rest
	// of the application while it is up.
	menu := section(t, html, `class="ctx-menu__at"`, ">")
	if !strings.Contains(menu, "data-on:click__window") {
		t.Errorf("clicking elsewhere does not close the menu: %s", menu)
	}
	if !strings.Contains(menu, "data-on:keydown__window") || !strings.Contains(menu, "Escape") {
		t.Errorf("Escape does not close the menu: %s", menu)
	}
	// And a click inside it is not a click elsewhere.
	if !strings.Contains(menu, "el.contains(evt.target)") {
		t.Error("choosing from the menu would also count as clicking away from it")
	}
	// Focus moves into it, or a keyboard user reaches it only by tabbing past
	// everything between.
	if !strings.Contains(html, "autofocus") {
		t.Error("the menu does not take focus when it opens")
	}
}

func TestCanvas_TheEdgeMenuAddressesTheTransitionByItsParts(t *testing.T) {
	data := canvasFixture(t, canvasMachine, "")
	data.CanvasMenu = CanvasMenu{
		Kind: "edge", Edge: "idle|on|SPOTTED|0",
		From: "idle", EdgeKind: "on", Event: "SPOTTED", Index: "0",
		X: 10, Y: 10, Open: true,
	}
	html := renderAgents(t, data)

	// Inside the menu, not anywhere on the page. Every edge on the canvas
	// carries these same four parts in its own contextmenu action, so a search
	// over the whole document passes with the menu's action carrying none of
	// them — which is how the first version of this test passed.
	menu := section(t, html, `data-testid="canvas-menu"`, "</div></div>")
	for _, want := range []string{"op=delete", "from=idle", "kind=on", "event=SPOTTED", "index=0"} {
		if !strings.Contains(menu, want) {
			t.Errorf("the delete action is missing %q: %s", want, menu)
		}
	}
	if strings.Contains(menu, "from=idle%7Con") {
		t.Error("the transition is addressed by its joined id, which cannot be split back apart")
	}
}

func TestCanvas_NoMenuIsRenderedWhenNoneIsOpen(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")
	if strings.Contains(html, `data-testid="canvas-menu"`) {
		t.Error("a menu nobody asked for is on the page")
	}
	if strings.Contains(html, "ctx-menu__at") {
		t.Error("an empty menu container is on the page")
	}
}

// State names and event names come out of a JSON file and reach these
// expressions, which are JavaScript inside an HTML attribute.
func TestCanvas_MenuActionsEscapeWhatTheyCarry(t *testing.T) {
	data := canvasFixture(t, canvasMachine, "")
	data.CanvasMenu = CanvasMenu{Kind: "state", State: `a'; alert(1); '`, Open: true}
	data.CanvasMenuWarning = `Delete "it"? It's gone.`
	html := renderAgents(t, data)

	// Percent-encoded where it goes into a URL, so the quote cannot close the
	// string the expression is building.
	if !strings.Contains(html, "initial=a%27%3B%20alert%281%29%3B%20%27") {
		t.Error("the state name is not percent-encoded into the action")
	}
	// And quoted as a JS string literal where it is one — the prompt's default.
	if !strings.Contains(html, `&#34;a&#39;; alert(1); &#39;&#34;`) {
		t.Error("the rename prompt's default is not a quoted string")
	}
	// Asserted positively as well as negatively: a name that vanished
	// altogether would satisfy every "does not contain" on its own.
	if strings.Contains(html, `('a'; alert(1); ')`) {
		t.Error("a state name reached the page as executable JavaScript")
	}

	// The confirmation is a JS string literal, so its own quotes are escaped
	// rather than ending it early.
	if !strings.Contains(html, `confirm(&#34;Delete \&#34;it\&#34;? It&#39;s gone.&#34;)`) {
		t.Error("the warning is not quoted as a JavaScript string")
	}
}
