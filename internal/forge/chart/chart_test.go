package chart_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
)

func parse(t *testing.T, src string) *agent.MachineDefinition {
	t.Helper()
	def, err := agent.ParseMachine([]byte(src))
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	return def
}

// nested is the shape most of these tests need: a compound state with children,
// a transition into one of them, an after, a guard, and a history node.
const nested = `{
  "id": "goblin",
  "initial": "idle",
  "states": {
    "idle": {
      "entry": [{ "type": "setAnimation", "params": { "clip": "walk", "loop": true, "speed": 2, "from": "idle" } }],
      "on": {
        "SPOTTED": [{ "target": "combat" }],
        "POKED": [{ "target": "combat.attacking", "cond": "isAngry" }]
      },
      "after": { "500": [{ "target": "wander" }] }
    },
    "combat": {
      "initial": "attacking",
      "states": {
        "attacking": { "on": { "LOST": [{ "target": "idle" }] } },
        "fleeing": {},
        "hist": { "type": "history", "history": "deep" }
      }
    },
    "wander": {}
  }
}`

func findNode(c chart.Chart, path string) (chart.Node, bool) {
	var walk func([]chart.Node) (chart.Node, bool)
	walk = func(nodes []chart.Node) (chart.Node, bool) {
		for _, n := range nodes {
			if n.Path == path {
				return n, true
			}
			if got, ok := walk(n.Children); ok {
				return got, true
			}
		}
		return chart.Node{}, false
	}
	return walk(c.Nodes)
}

func paths(c chart.Chart) []string {
	var out []string
	var walk func([]chart.Node)
	walk = func(nodes []chart.Node) {
		for _, n := range nodes {
			out = append(out, n.Path)
			walk(n.Children)
		}
	}
	walk(c.Nodes)
	return out
}

func edgeByID(c chart.Chart, id string) (chart.Edge, bool) {
	for _, e := range c.Edges {
		if e.ID == id {
			return e, true
		}
	}
	return chart.Edge{}, false
}

func TestBuild_ANodePerState_NestedOnesIncluded(t *testing.T) {
	c := chart.Build(parse(t, nested), "")

	want := []string{"idle", "combat", "combat.attacking", "combat.fleeing", "combat.hist", "wander"}
	got := paths(c)
	if len(got) != len(want) {
		t.Fatalf("drew %d nodes %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("node %d = %q, want %q", i, got[i], want[i])
		}
	}
	// Structural, not only enumerated: a flat drawing of a tree is a different
	// machine, so the children have to hang off the node that owns them.
	combat, _ := findNode(c, "combat")
	if len(combat.Children) != 3 {
		t.Fatalf("combat has %d children, want 3", len(combat.Children))
	}
	if len(c.Nodes) != 3 {
		t.Errorf("chart has %d top-level nodes, want 3", len(c.Nodes))
	}
}

func TestBuild_InitialIsTaggedAtEveryLevel(t *testing.T) {
	c := chart.Build(parse(t, nested), "")
	for path, want := range map[string]bool{
		"idle":             true,
		"combat":           false,
		"wander":           false,
		"combat.attacking": true,
		"combat.fleeing":   false,
	} {
		n, ok := findNode(c, path)
		if !ok {
			t.Fatalf("no node %q", path)
		}
		if n.Initial != want {
			t.Errorf("%s initial = %v, want %v", path, n.Initial, want)
		}
	}
}

func TestBuild_AnEdgePerTransition(t *testing.T) {
	c := chart.Build(parse(t, nested), "")

	want := []string{
		"idle|on|SPOTTED|0",
		"idle|on|POKED|0",
		"idle|after|500|0",
		"combat.attacking|on|LOST|0",
	}
	if len(c.Edges) != len(want) {
		var got []string
		for _, e := range c.Edges {
			got = append(got, e.ID)
		}
		t.Fatalf("drew %d edges %v, want %d %v", len(c.Edges), got, len(want), want)
	}
	for i, id := range want {
		if c.Edges[i].ID != id {
			t.Errorf("edge %d = %q, want %q", i, c.Edges[i].ID, id)
		}
	}
}

// Two transitions on one event is the ordinary way to write a guarded fork, and
// keying an edge on the event alone would draw one of them.
func TestBuild_TwoTransitionsOnOneEventAreTwoEdges(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "fork",
	  "initial": "a",
	  "states": {
	    "a": { "on": { "GO": [{ "target": "b", "cond": "isReady" }, { "target": "c" }] } },
	    "b": {}, "c": {}
	  }
	}`), "")

	if len(c.Edges) != 2 {
		t.Fatalf("drew %d edges, want 2", len(c.Edges))
	}
	if c.Edges[0].ID != "a|on|GO|0" || c.Edges[1].ID != "a|on|GO|1" {
		t.Fatalf("edge ids %q, %q", c.Edges[0].ID, c.Edges[1].ID)
	}
	if c.Edges[0].To != "b" || c.Edges[1].To != "c" {
		t.Errorf("targets %q, %q — want b, c", c.Edges[0].To, c.Edges[1].To)
	}
}

func TestBuild_AfterEdgeIsLabelledWithItsDuration(t *testing.T) {
	c := chart.Build(parse(t, nested), "")
	e, ok := edgeByID(c, "idle|after|500|0")
	if !ok {
		t.Fatal("no after edge")
	}
	if e.Kind != chart.EdgeAfter {
		t.Errorf("kind = %v, want after", e.Kind)
	}
	// The key is the duration; an after transition has no event name to fall
	// back on, so labelling it with one would label it with nothing.
	if got := e.Label(); got != "after 500" {
		t.Errorf("label = %q, want %q", got, "after 500")
	}
}

func TestBuild_AGuardedEdgeCarriesItsGuard(t *testing.T) {
	c := chart.Build(parse(t, nested), "")
	guarded, _ := edgeByID(c, "idle|on|POKED|0")
	plain, _ := edgeByID(c, "idle|on|SPOTTED|0")

	if guarded.Guard != "isAngry" {
		t.Errorf("guard = %q, want isAngry", guarded.Guard)
	}
	if plain.Guard != "" {
		t.Errorf("unconditional edge reports guard %q", plain.Guard)
	}
	// Distinguishable in the label as well as in an attribute: a guard is the
	// difference between "this happens" and "this might".
	if got := guarded.Label(); got != "POKED [isAngry]" {
		t.Errorf("label = %q, want %q", got, "POKED [isAngry]")
	}
	if got := plain.Label(); got != "SPOTTED" {
		t.Errorf("label = %q, want %q", got, "SPOTTED")
	}
}

func TestBuild_TargetsResolveTheWayTheEngineResolvesThem(t *testing.T) {
	c := chart.Build(parse(t, nested), "")
	// A dotted path into a compound state, which a top-level-names-only
	// resolver would call dangling.
	e, _ := edgeByID(c, "idle|on|POKED|0")
	if e.To != "combat.attacking" || e.Dangling {
		t.Errorf("POKED resolved to %q (dangling=%v), want combat.attacking", e.To, e.Dangling)
	}
	// And a bare name that only exists nested, which the interpreter finds by
	// descending.
	c2 := chart.Build(parse(t, `{
	  "id": "deep",
	  "initial": "outer",
	  "states": {
	    "outer": { "initial": "inner", "states": { "inner": {} }, "on": { "GO": [{ "target": "inner" }] } }
	  }
	}`), "")
	e2, ok := edgeByID(c2, "outer|on|GO|0")
	if !ok {
		t.Fatal("no edge")
	}
	if e2.To != "outer.inner" || e2.Dangling {
		t.Errorf("GO resolved to %q (dangling=%v), want outer.inner", e2.To, e2.Dangling)
	}
}

func TestBuild_ATransitionToNowhereStillDraws(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "broken",
	  "initial": "a",
	  "states": { "a": { "on": { "GO": [{ "target": "ghost" }] } } }
	}`), "")

	if len(c.Edges) != 1 {
		t.Fatalf("drew %d edges, want 1 — a transition you cannot see is one you cannot fix", len(c.Edges))
	}
	e := c.Edges[0]
	if !e.Dangling {
		t.Error("the edge is not marked dangling")
	}
	if e.To != "" {
		t.Errorf("To = %q, want empty", e.To)
	}
	// The label names the state that is missing, because that is the thing to
	// go and create or spell differently.
	if got := e.Label(); got != "GO → ghost?" {
		t.Errorf("label = %q, want %q", got, "GO → ghost?")
	}
	// And it has somewhere to point, or it is not drawn at all.
	if e.X1 == e.X2 && e.Y1 == e.Y2 {
		t.Error("the dangling edge has zero length")
	}
}

func TestBuild_ATargetlessTransitionIsInternalNotDangling(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "internal",
	  "initial": "a",
	  "states": { "a": { "on": { "PING": [{ "actions": ["bump"] }] } } }
	}`), "")

	if len(c.Edges) != 1 {
		t.Fatalf("drew %d edges, want 1", len(c.Edges))
	}
	e := c.Edges[0]
	if e.Dangling {
		t.Error("a targetless transition is not a broken one: it runs actions and changes no state")
	}
	if !e.Internal || !e.Loop {
		t.Errorf("internal = %v, loop = %v; want both true", e.Internal, e.Loop)
	}
}

func TestBuild_AHistoryNodeIsDrawnAsOne(t *testing.T) {
	c := chart.Build(parse(t, nested), "")
	n, ok := findNode(c, "combat.hist")
	if !ok {
		t.Fatal("no history node")
	}
	if n.Kind != agent.StateTypeHistory {
		t.Errorf("kind = %q, want history", n.Kind)
	}
	if n.History != "deep" {
		t.Errorf("history = %q, want deep", n.History)
	}
	// Drawn as a marker rather than as a box the size of a state.
	if n.W >= 100 {
		t.Errorf("history node is %vpx wide — it is being drawn as an ordinary state", n.W)
	}
}

func TestBuild_EntryActionsAreOnTheNode(t *testing.T) {
	// Four parameters, deliberately: with two, a label built by ranging the map
	// comes out right half the time and the test passes half the time.
	c := chart.Build(parse(t, `{
	  "id": "acts",
	  "initial": "a",
	  "states": {
	    "a": {
	      "entry": [
	        { "type": "setAnimation", "params": { "clip": "walk", "loop": true, "speed": 2, "from": "idle" } },
	        "stopMoving"
	      ]
	    }
	  }
	}`), "")
	n, _ := findNode(c, "a")
	// Sorted by key — clip, from, loop, speed — and the values shown rather
	// than the names, which is what tells you which animation.
	want := []string{"setAnimation · walk · idle · true · 2", "stopMoving"}
	if len(n.Entry) != len(want) {
		t.Fatalf("entry = %v, want %v", n.Entry, want)
	}
	for i := range want {
		if n.Entry[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, n.Entry[i], want[i])
		}
	}
}

func TestBuild_ACompoundNodeContainsItsChildren(t *testing.T) {
	c := chart.Build(parse(t, nested), "")
	parent, _ := findNode(c, "combat")
	for _, child := range parent.Children {
		if child.AbsX < parent.AbsX || child.AbsY < parent.AbsY ||
			child.AbsX+child.W > parent.AbsX+parent.W ||
			child.AbsY+child.H > parent.AbsY+parent.H {
			t.Errorf("%s at (%v,%v,%vx%v) is not inside %s at (%v,%v,%vx%v)",
				child.Path, child.AbsX, child.AbsY, child.W, child.H,
				parent.Path, parent.AbsX, parent.AbsY, parent.W, parent.H)
		}
	}
}

func TestBuild_EdgeEndpointsSitOnTheBoxes(t *testing.T) {
	// Three transitions between two states placed off-axis, so the fan vector
	// is not parallel to a box edge. The first version of this test picked an
	// edge whose group had one member, where spread is zero and the fan cannot
	// be wrong — the one case the property it names cannot fail in.
	c := chart.Build(parse(t, `{
	  "id": "fanned", "initial": "a",
	  "states": {
	    "a": {
	      "meta": { "forge": { "x": 0, "y": 0 } },
	      "on": {
	        "ONE": [{ "target": "b" }],
	        "TWO": [{ "target": "b" }],
	        "THREE": [{ "target": "b" }]
	      }
	    },
	    "b": { "meta": { "forge": { "x": 500, "y": 120 } } }
	  }
	}`), "")

	from, _ := findNode(c, "a")
	to, _ := findNode(c, "b")
	if len(c.Edges) != 3 {
		t.Fatalf("drew %d edges, want 3", len(c.Edges))
	}
	for _, e := range c.Edges {
		if !onBoundary(from, e.X1, e.Y1) {
			t.Errorf("%s starts at (%v,%v), not on the source box (%v,%v %vx%v)",
				e.ID, e.X1, e.Y1, from.AbsX, from.AbsY, from.W, from.H)
		}
		if !onBoundary(to, e.X2, e.Y2) {
			t.Errorf("%s ends at (%v,%v), not on the target box (%v,%v %vx%v) — the arrowhead points at nothing",
				e.ID, e.X2, e.Y2, to.AbsX, to.AbsY, to.W, to.H)
		}
	}
}

// onBoundary is "inside the box, and touching one of its sides".
func onBoundary(n chart.Node, x, y float64) bool {
	const eps = 0.2
	inside := x >= n.AbsX-eps && x <= n.AbsX+n.W+eps && y >= n.AbsY-eps && y <= n.AbsY+n.H+eps
	touching := math.Abs(x-n.AbsX) < eps || math.Abs(x-(n.AbsX+n.W)) < eps ||
		math.Abs(y-n.AbsY) < eps || math.Abs(y-(n.AbsY+n.H)) < eps
	return inside && touching
}

// And the ordinary unfanned case, which is most of a chart.
func TestBuild_ALoneEdgeEndsOnTheBoxesToo(t *testing.T) {
	c := chart.Build(parse(t, nested), "")
	e, _ := edgeByID(c, "idle|on|SPOTTED|0")
	from, _ := findNode(c, "idle")
	to, _ := findNode(c, "combat")

	if !onBoundary(from, e.X1, e.Y1) {
		t.Errorf("start (%v,%v) is not on the source box", e.X1, e.Y1)
	}
	if !onBoundary(to, e.X2, e.Y2) {
		t.Errorf("end (%v,%v) is not on the target box — the arrowhead is under the node", e.X2, e.Y2)
	}
}

func TestBuild_AnInvalidMachineStillDraws(t *testing.T) {
	// Every kind of wrong at once: no initial, an unknown action, a context key
	// naming nothing, a target that does not exist. This is the editor for
	// fixing it, so it has to be able to show it.
	c := chart.Build(parse(t, `{
	  "id": "wrong",
	  "context": { "nonesuch": 1 },
	  "states": {
	    "a": { "entry": ["noSuchAction"], "on": { "GO": [{ "target": "nowhere" }] } },
	    "b": { "initial": "missing", "states": { "x": {} } }
	  }
	}`), "")

	if len(paths(c)) != 3 {
		t.Fatalf("drew %v", paths(c))
	}
	if len(c.Edges) != 1 {
		t.Fatalf("drew %d edges, want 1", len(c.Edges))
	}
}

func TestBuild_NoMachineIsAnEmptyChart(t *testing.T) {
	c := chart.Build(nil, "state:a")
	if len(c.Nodes) != 0 || len(c.Edges) != 0 || c.Selected != "" {
		t.Errorf("nil machine drew %+v", c)
	}
}

// The chart is on a 2-second stream that suppresses a patch when the markup is
// unchanged, so any map iteration order leaking into the output both flickers
// the canvas and defeats the suppression.
func TestBuild_TwoBuildsAgree(t *testing.T) {
	def := parse(t, nested)
	first := fmt.Sprintf("%+v", chart.Build(def, "state:combat.attacking"))
	for i := 0; i < 50; i++ {
		if got := fmt.Sprintf("%+v", chart.Build(def, "state:combat.attacking")); got != first {
			t.Fatalf("build %d differs:\n%s\n%s", i, first, got)
		}
	}
}

func TestBuild_SelectionResolvesServerSide(t *testing.T) {
	def := parse(t, nested)

	c := chart.Build(def, "state:combat.attacking")
	if c.Selected != "state:combat.attacking" {
		t.Errorf("Selected = %q", c.Selected)
	}
	n, _ := findNode(c, "combat.attacking")
	if !n.Selected {
		t.Error("the node is not marked selected")
	}

	c = chart.Build(def, "edge:idle|on|SPOTTED|0")
	if c.Selected != "edge:idle|on|SPOTTED|0" {
		t.Errorf("Selected = %q", c.Selected)
	}
	e, _ := edgeByID(c, "idle|on|SPOTTED|0")
	if !e.Selected {
		t.Error("the edge is not marked selected")
	}
}

func TestBuild_ASelectionNamingNothingIsDropped(t *testing.T) {
	def := parse(t, nested)
	for _, sel := range []string{"state:gone", "edge:gone|on|X|0", "nonsense", "", "state:", "edge:"} {
		c := chart.Build(def, sel)
		if c.Selected != "" {
			t.Errorf("sel %q resolved to %q; a bookmark to a renamed state is not an error", sel, c.Selected)
		}
		if n := countSelected(c); n != 0 {
			t.Errorf("sel %q marked %d things selected", sel, n)
		}
	}
}

func TestBuild_ExactlyOneThingIsSelected(t *testing.T) {
	def := parse(t, nested)
	for _, sel := range []string{"state:idle", "edge:idle|on|SPOTTED|0", "state:combat"} {
		if n := countSelected(chart.Build(def, sel)); n != 1 {
			t.Errorf("sel %q marked %d things selected, want 1", sel, n)
		}
	}
}

func countSelected(c chart.Chart) int {
	n := 0
	var walk func([]chart.Node)
	walk = func(nodes []chart.Node) {
		for _, node := range nodes {
			if node.Selected {
				n++
			}
			walk(node.Children)
		}
	}
	walk(c.Nodes)
	for _, e := range c.Edges {
		if e.Selected {
			n++
		}
	}
	return n
}

// A compound state's children start below its own title, or the name of the
// state is drawn underneath one of the states it holds.
func TestBuild_ChildrenClearTheirParentsTitle(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "titled",
	  "initial": "outer",
	  "states": {
	    "outer": {
	      "entry": ["one", "two", "three"],
	      "initial": "inner",
	      "states": { "inner": {} }
	    }
	  }
	}`), "")

	outer, _ := findNode(c, "outer")
	inner, _ := findNode(c, "outer.inner")
	if outer.HeadH <= 0 {
		t.Fatal("the parent reports no title height")
	}
	if inner.Y < outer.HeadH {
		t.Errorf("inner starts at y=%v, inside a title %v tall", inner.Y, outer.HeadH)
	}
	// And the title grew with the entry actions rather than being a constant
	// that three of them overflow.
	plain, _ := findNode(chart.Build(parse(t, `{
	  "id": "titled", "initial": "outer",
	  "states": { "outer": { "initial": "inner", "states": { "inner": {} } } }
	}`), ""), "outer")
	if outer.HeadH <= plain.HeadH {
		t.Errorf("title with three entry actions is %v, without is %v", outer.HeadH, plain.HeadH)
	}
}

// Three transitions between the same two states land on the same straight line
// and their labels on the same midpoint, so the picture says "these two states
// are connected" and refuses to say how many times or by what.
//
// Asserted on the lines and not on the labels. The first version of this test
// checked the label positions, and separateLabels pushes those apart whether
// the lines were fanned or not — so it passed with the fan removed entirely.
// That is the neighbour of the property, not the property.
func TestBuild_ParallelEdgesAreFannedApart(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "many", "initial": "a",
	  "states": {
	    "a": { "on": { "ONE": [{ "target": "b" }], "TWO": [{ "target": "b" }] } },
	    "b": { "on": { "BACK": [{ "target": "a" }] } }
	  }
	}`), "")

	if len(c.Edges) != 3 {
		t.Fatalf("drew %d edges, want 3", len(c.Edges))
	}
	seen := map[[4]float64]string{}
	for _, e := range c.Edges {
		// Normalised, because BACK runs the other way along the same line: an
		// un-normalised key would call two coincident lines distinct purely
		// because their endpoints are written in the other order.
		line := [4]float64{e.X1, e.Y1, e.X2, e.Y2}
		if e.X1 > e.X2 {
			line = [4]float64{e.X2, e.Y2, e.X1, e.Y1}
		}
		if other, clash := seen[line]; clash {
			t.Errorf("%s and %s are drawn as the same line at %v", other, e.ID, line)
		}
		seen[line] = e.ID
	}
	// And the return edge is fanned with the other two rather than being left
	// on the centre line by itself: A→B and B→A run along the same line, and
	// the perpendicular of a reversed line points the other way, so without
	// the reversal BACK lands exactly on top of ONE.
	if len(seen) != 3 {
		t.Fatalf("%d distinct lines, want 3", len(seen))
	}
	// The labels are deliberately not asserted here. separateLabels pushes them
	// apart whether the lines were fanned or not, so a label assertion in this
	// test passes with the fan removed entirely — which is how the first
	// version of it passed. TestBuild_LabelsThatWouldCollideArePushedApart owns
	// that property.
}

// A lone edge is not moved at all, or every straight line in a simple machine
// would be drawn off-centre for no reason.
func TestBuild_ALoneEdgeIsNotFanned(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "one", "initial": "a",
	  "states": { "a": { "on": { "GO": [{ "target": "b" }] } }, "b": {} }
	}`), "")

	e := c.Edges[0]
	a, _ := findNode(c, "a")
	b, _ := findNode(c, "b")
	if e.Y1 != a.AbsY+a.H/2 || e.Y2 != b.AbsY+b.H/2 {
		t.Errorf("the only edge between two states was pushed off their centres")
	}
}

// A state with two self-transitions draws two loops, not one on top of another.
func TestBuild_SelfTransitionsStack(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "self", "initial": "a",
	  "states": { "a": { "on": { "ONE": [{ "actions": ["x"] }], "TWO": [{ "target": "a" }] } } }
	}`), "")

	if len(c.Edges) != 2 {
		t.Fatalf("drew %d edges, want 2", len(c.Edges))
	}
	// The arc is the load-bearing one. Two loops with the same arc are drawn on
	// top of each other and one of them is invisible, whatever their labels do
	// — and separateLabels pushes labels apart regardless, so a test that
	// checked only the labels passed with the stacking removed. The implication
	// runs one way only: differing arcs give differing labels, not the reverse.
	if c.Edges[0].Arc == c.Edges[1].Arc {
		t.Errorf("both loops arc to %v, so one is drawn on top of the other", c.Edges[0].Arc)
	}
	if gap := math.Abs(c.Edges[0].LabelY - c.Edges[1].LabelY); gap < 14 {
		t.Errorf("the two loops label %vpx apart, which is less than a line", gap)
	}
}

// Fanning handles edges sharing a pair of states; this is the ones that do not
// and collide anyway — a transition into a compound state and one into its
// child run almost the same line. Two labels in the same place read as one word
// made of two, and the one underneath cannot be clicked at all.
func TestBuild_LabelsThatWouldCollideArePushedApart(t *testing.T) {
	c := chart.Build(parse(t, nested), "")

	for i, e := range c.Edges {
		for _, other := range c.Edges[i+1:] {
			dx, dy := e.LabelX-other.LabelX, e.LabelY-other.LabelY
			if dx < 0 {
				dx = -dx
			}
			if dy < 0 {
				dy = -dy
			}
			if dx < 40 && dy < 12 {
				t.Errorf("%s and %s label at (%v,%v) and (%v,%v)",
					e.ID, other.ID, e.LabelX, e.LabelY, other.LabelX, other.LabelY)
			}
		}
	}
}

// Centred on the line they share, not stacked off one side of it.
//
// "Distinct" and "a lone edge is unmoved" are both true of a fan that starts at
// the centre and walks outwards in one direction, and that one puts every pair
// of transitions lopsidedly to the left of the states they connect. What makes
// it centred is that the offsets sum to zero.
func TestBuild_AFanIsCentredOnTheLineItShares(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "pair", "initial": "a",
	  "states": {
	    "a": { "on": { "ONE": [{ "target": "b" }], "TWO": [{ "target": "b" }] } },
	    "b": {}
	  }
	}`), "")

	a, _ := findNode(c, "a")
	b, _ := findNode(c, "b")
	// The line the two edges share, and the point they are fanned around.
	midY := ((a.AbsY + a.H/2) + (b.AbsY + b.H/2)) / 2

	if len(c.Edges) != 2 {
		t.Fatalf("drew %d edges, want 2", len(c.Edges))
	}
	var sum float64
	for _, e := range c.Edges {
		off := (e.Y1+e.Y2)/2 - midY
		if off == 0 {
			t.Errorf("%s was not fanned at all", e.ID)
		}
		sum += off
	}
	if sum != 0 {
		t.Errorf("the two edges are offset by %v in total; a centred fan sums to zero", sum)
	}
}

// A state name containing a dot collides with the path notation the chart keys
// nodes on. This pins what happens rather than claiming it cannot: XState uses
// the dot as its path separator, so such a machine is already ambiguous to the
// engine — findState resolves "a.b" to the nested state, not the top-level one
// of that name — and the canvas follows the engine rather than inventing a
// second answer.
//
// The test exists because the design rests on paths being unique, and an
// assumption worth resting on is worth writing down where it fails.
func TestBuild_ADotInAStateNameCollidesWithThePathNotation(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "d", "initial": "a",
	  "states": {
	    "a": { "initial": "b", "states": { "b": {} }, "on": { "GO": [{ "target": "a.b" }] } },
	    "a.b": {}
	  }
	}`), "")

	got := paths(c)
	seen := map[string]int{}
	for _, p := range got {
		seen[p]++
	}
	if seen["a.b"] != 2 {
		t.Fatalf("expected the collision this test documents; drew %v", got)
	}
	// The edge follows the engine: agent.FindState traverses the dotted path
	// first, so "a.b" is the nested state.
	e, _ := edgeByID(c, "a|on|GO|0")
	if e.To != "a.b" || e.Dangling {
		t.Errorf("GO resolved to %q (dangling=%v)", e.To, e.Dangling)
	}
}
