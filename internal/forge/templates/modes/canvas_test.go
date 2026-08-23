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

func TestCanvas_SelectionIsALink(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "")
	// The path is escaped into the URL, and the machine goes with it: a
	// selection with no machine names a state in whichever file sorts first.
	want := `href="/forge/agents?machine=%2Fp%2Fcore%2Fbehaviors%2Fwander.json&amp;sel=state%3Acombat.attacking"`
	if !strings.Contains(html, want) {
		t.Errorf("no link selects the nested state; wanted\n%s", want)
	}
	if !strings.Contains(html, `sel=edge%3Aidle%7Con%7CSPOTTED%7C0`) {
		t.Error("no link selects an edge")
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
	// And the ground is not offered, because there is nothing to let go of.
	if strings.Contains(html, `data-testid="canvas-ground"`) {
		t.Error("a link that clears nothing is still a link to everything that reads the page aloud")
	}
}

func TestCanvas_TheGroundClearsTheSelection(t *testing.T) {
	html := renderCanvas(t, canvasMachine, "state:idle")

	i := strings.Index(html, `data-testid="canvas-ground"`)
	if i < 0 {
		t.Fatal("no ground to click off onto")
	}
	ground := html[strings.LastIndex(html[:i], "<a"):i]
	if !strings.Contains(ground, `href="/forge/agents?machine=%2Fp%2Fcore%2Fbehaviors%2Fwander.json"`) {
		t.Errorf("the ground does not link back to the machine with no selection: %s", ground)
	}
	// It is a link with no text, so it needs a name to be one at all.
	if !strings.Contains(html[i:i+200], `aria-label="Clear selection"`) {
		t.Error("the ground is an unnamed link")
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

// nodeElement is a node's whole element, children included, found by matching
// the div that opens it against the one that closes it.
func nodeElement(t *testing.T, html, path string) string {
	t.Helper()
	i := strings.Index(html, `data-testid="state-`+path+`"`)
	if i < 0 {
		t.Fatalf("no node %s", path)
	}
	start := strings.LastIndex(html[:i], "<div")
	if start < 0 {
		t.Fatalf("node %s is not in an element", path)
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
	t.Fatalf("node %s is never closed", path)
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
	end := strings.Index(html[start:], "</a>")
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
	start := strings.LastIndex(html[:i], "<a")
	if start < 0 {
		t.Fatalf("edge %s is not in an element", id)
	}
	end := strings.Index(html[start:], "</a>")
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
	if !strings.Contains(html, "width:224.3px") {
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
