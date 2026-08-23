package chart_test

import (
	"fmt"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/chart"
)

func TestLayout_PositionComesFromMeta(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "placed",
	  "initial": "a",
	  "states": {
	    "a": { "meta": { "forge": { "x": 320, "y": 44 } } },
	    "b": {}
	  }
	}`), "")

	a, _ := findNode(c, "a")
	if a.X != 320 || a.Y != 44 {
		t.Errorf("a at (%v,%v), want (320,44)", a.X, a.Y)
	}
	b, _ := findNode(c, "b")
	if b.X == 320 && b.Y == 44 {
		t.Error("b took a's position")
	}
}

// A child's coordinates are relative to the compound state that holds it, so
// Story 5 can move a parent without rewriting every child's meta.
//
// And the mapping is pinned, not just the containment: a nested node's rendered
// X is its recorded x plus the parent's inner margin, and its Y is its recorded
// y plus however tall the parent's own title is. Story 5 writes dragged
// positions back into meta and has to undo exactly that, so an earlier version
// of this test — which asserted only that the child was somewhere inside its
// parent, and that AbsX was the parent's plus its own — could not see a drift
// of (padX, HeadH) per drag.
func TestLayout_AChildIsPlacedInsideItsParent(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "rel",
	  "initial": "outer",
	  "states": {
	    "outer": {
	      "meta": { "forge": { "x": 200, "y": 100 } },
	      "initial": "inner",
	      "states": { "inner": { "meta": { "forge": { "x": 10, "y": 4 } } } }
	    }
	  }
	}`), "")

	outer, _ := findNode(c, "outer")
	inner, _ := findNode(c, "outer.inner")
	// A top-level node's rendered position is the one the file records.
	if outer.X != 200 || outer.Y != 100 || outer.AbsX != 200 || outer.AbsY != 100 {
		t.Fatalf("outer at (%v,%v) / abs (%v,%v), want (200,100)", outer.X, outer.Y, outer.AbsX, outer.AbsY)
	}
	// A nested one's is offset into its parent's content box.
	if inner.X != 10+chart.PadX {
		t.Errorf("inner.X = %v, want its recorded 10 plus the inner margin %v", inner.X, chart.PadX)
	}
	if inner.Y != 4+outer.HeadH {
		t.Errorf("inner.Y = %v, want its recorded 4 below a title %v tall", inner.Y, outer.HeadH)
	}
	if inner.AbsX != outer.AbsX+inner.X || inner.AbsY != outer.AbsY+inner.Y {
		t.Error("inner's absolute position is not its parent's plus its own")
	}
}

// The slot a state falls back to is keyed on where it sits in the authored
// order, not on which slots are still free. Otherwise positioning one state in
// Story 5 shuffles every unpositioned state after it.
func TestLayout_AFallbackSlotIsKeyedOnAuthoredIndex(t *testing.T) {
	before := chart.Build(parse(t, `{
	  "id": "mix", "initial": "a",
	  "states": { "a": {}, "b": {}, "c": {} }
	}`), "")
	after := chart.Build(parse(t, `{
	  "id": "mix", "initial": "a",
	  "states": { "a": { "meta": { "forge": { "x": 900, "y": 900 } } }, "b": {}, "c": {} }
	}`), "")

	for _, name := range []string{"b", "c"} {
		wasNode, _ := findNode(before, name)
		isNode, _ := findNode(after, name)
		if wasNode.X != isNode.X || wasNode.Y != isNode.Y {
			t.Errorf("placing a moved %s from (%v,%v) to (%v,%v)",
				name, wasNode.X, wasNode.Y, isNode.X, isNode.Y)
		}
	}
}

func TestLayout_UnpositionedStatesDoNotOverlap(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "grid", "initial": "a",
	  "states": { "a": {}, "b": {}, "c": {}, "d": {}, "e": {}, "f": {}, "g": {} }
	}`), "")

	for i, n := range c.Nodes {
		for _, m := range c.Nodes[i+1:] {
			if n.X < m.X+m.W && m.X < n.X+n.W && n.Y < m.Y+m.H && m.Y < n.Y+n.H {
				t.Errorf("%s and %s overlap", n.Path, m.Path)
			}
		}
	}
}

// meta is hand-editable and shared with Stately Studio. A chart that refused to
// draw because someone's meta held a note would be worse than one that places
// the node itself.
func TestLayout_MalformedMetaIsIgnoredNotFatal(t *testing.T) {
	fallback, _ := findNode(chart.Build(parse(t, `{"id":"m","initial":"a","states":{"a":{}}}`), ""), "a")

	for _, meta := range []string{
		`"a note"`,
		`{ "forge": "over there" }`,
		`{ "forge": { "x": "left", "y": 2 } }`,
		`{ "forge": {} }`,
		`{ "notes": "hi" }`,
		`null`,
		`[1,2]`,
	} {
		src := `{"id":"m","initial":"a","states":{"a":{"meta":` + meta + `}}}`
		n, ok := findNode(chart.Build(parse(t, src), ""), "a")
		if !ok {
			t.Fatalf("meta %s made the node disappear", meta)
		}
		if n.X != fallback.X || n.Y != fallback.Y {
			t.Errorf("meta %s placed the node at (%v,%v), want the fallback (%v,%v)",
				meta, n.X, n.Y, fallback.X, fallback.Y)
		}
	}
}

// The reverse of drawing from the machine: layout for a state that has been
// deleted in another tool is ignored rather than drawn, and is not an error
// either. Both directions are ordinary results of editing the file elsewhere.
func TestLayout_LayoutForAStateThatIsGoneDrawsNothing(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "stale", "initial": "a",
	  "states": {
	    "a": { "meta": { "forge": { "x": 10, "y": 10 }, "deletedSibling": { "forge": { "x": 99, "y": 99 } } } }
	  }
	}`), "")

	if got := paths(c); len(got) != 1 || got[0] != "a" {
		t.Errorf("drew %v, want just [a]", got)
	}
}

// Layout says where a node sits; it must never say whether a node exists.
func TestLayout_AStateWithNoLayoutIsStillDrawn(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "partial", "initial": "a",
	  "states": { "a": { "meta": { "forge": { "x": 0, "y": 0 } } }, "b": {} }
	}`), "")

	if got := paths(c); len(got) != 2 {
		t.Fatalf("drew %v, want both states — a stale layout must not hide part of a machine", got)
	}
}

func TestLayout_ChartIsBigEnoughForWhatItHolds(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "far", "initial": "a",
	  "states": { "a": { "meta": { "forge": { "x": 900, "y": 700 } } } }
	}`), "")

	a, _ := findNode(c, "a")
	if c.W < a.X+a.W || c.H < a.Y+a.H {
		t.Errorf("chart is %vx%v but holds a node ending at (%v,%v)", c.W, c.H, a.X+a.W, a.Y+a.H)
	}
}

// A compound state grown to hold its children is wider than the default cell,
// and a grid that ignored that would stack it on top of its own siblings.
func TestLayout_CompoundSiblingsDoNotOverlap(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "big", "initial": "one",
	  "states": {
	    "one": { "initial": "a", "states": { "a": {}, "b": {}, "c": {} } },
	    "two": { "initial": "d", "states": { "d": {}, "e": {}, "f": {} } },
	    "three": {}
	  }
	}`), "")

	for i, n := range c.Nodes {
		for _, m := range c.Nodes[i+1:] {
			if n.X < m.X+m.W && m.X < n.X+n.W && n.Y < m.Y+m.H && m.Y < n.Y+n.H {
				t.Errorf("%s (%vx%v at %v,%v) overlaps %s (%vx%v at %v,%v)",
					n.Path, n.W, n.H, n.X, n.Y, m.Path, m.W, m.H, m.X, m.Y)
			}
		}
	}
}

// Cell size comes from what is at a level, not from what is still unplaced, so
// positioning one state cannot move another. The overlap test above would pass
// with either rule; this one only passes with the right one.
func TestLayout_PlacingAWideStateDoesNotMoveItsSiblings(t *testing.T) {
	const states = `"one": { %s "initial": "a", "states": { "a": {}, "b": {}, "c": {} } }, "two": {}, "three": {}`
	before := chart.Build(parse(t, `{"id":"w","initial":"one","states":{`+
		fmt.Sprintf(states, "")+`}}`), "")
	after := chart.Build(parse(t, `{"id":"w","initial":"one","states":{`+
		fmt.Sprintf(states, `"meta": { "forge": { "x": 900, "y": 900 } },`)+`}}`), "")

	for _, name := range []string{"two", "three"} {
		was, _ := findNode(before, name)
		is, _ := findNode(after, name)
		if was.X != is.X || was.Y != is.Y {
			t.Errorf("placing one moved %s from (%v,%v) to (%v,%v)", name, was.X, was.Y, is.X, is.Y)
		}
	}
}

// Nothing scrolls into negative overflow, so a node placed at a negative
// coordinate — hand-edited, or written by another tool — would be painted
// outside the clip and could never be brought into view or clicked. That is
// layout deciding whether a node exists, which the story forbids in the other
// direction and means in both.
func TestLayout_ANegativeCoordinateIsBroughtBackOntoTheCanvas(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "neg", "initial": "a",
	  "states": {
	    "a": { "meta": { "forge": { "x": -400, "y": -200 } } },
	    "b": { "meta": { "forge": { "x": 40, "y": 40 } } }
	  }
	}`), "")

	a, _ := findNode(c, "a")
	if a.X < 0 || a.Y < 0 || a.AbsX < 0 || a.AbsY < 0 {
		t.Errorf("a is still off the canvas at (%v,%v)", a.X, a.Y)
	}
	if c.OffsetX != 400 || c.OffsetY != 200 {
		t.Errorf("offset = (%v,%v), want (400,200)", c.OffsetX, c.OffsetY)
	}
	// Everything moved together, so the layout the author made is intact.
	b, _ := findNode(c, "b")
	if b.X-a.X != 440 || b.Y-a.Y != 240 {
		t.Errorf("the two states are now %v,%v apart; they were 440,240", b.X-a.X, b.Y-a.Y)
	}
}

// A self-transition arcs above the node it loops over, and a state in the first
// row of the fallback grid has that arc — and its label, which is the edge's
// only pointer target — above the origin. Clipped, unclickable, and the default
// state of a machine with no layout yet.
func TestLayout_ASelfTransitionOnTheTopRowIsNotClippedAway(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "loops", "initial": "a",
	  "states": { "a": { "on": { "ONE": [{ "target": "a" }], "TWO": [{ "target": "a" }] } } }
	}`), "")

	// A chip's own height either side of the position it is centred on: the
	// chart assumes the same, and a label brought half back on is still half
	// unreadable.
	const chip = 10
	for _, e := range c.Edges {
		if e.LabelY-chip < 0 || e.Y1-e.Arc < 0 {
			t.Errorf("%s labels from y=%v and arcs to y=%v", e.ID, e.LabelY-chip, e.Y1-e.Arc)
		}
		if e.LabelY+chip > c.H {
			t.Errorf("%s labels to y=%v, below a canvas %v tall", e.ID, e.LabelY+chip, c.H)
		}
	}
}

// The canvas is sized to everything on it. A dangling edge points off to the
// right of its source and its label is centred on its own position, so a canvas
// measured from the boxes alone clips exactly the thing it is there to show.
func TestLayout_TheCanvasHoldsTheEdgesToo(t *testing.T) {
	c := chart.Build(parse(t, `{
	  "id": "dang", "initial": "a",
	  "states": { "a": { "on": { "GO": [{ "target": "ghost" }] } } }
	}`), "")

	e := c.Edges[0]
	if e.X2 > c.W {
		t.Errorf("the dangling edge ends at x=%v on a canvas %v wide", e.X2, c.W)
	}
	// The label is not asserted here and does not need to be: it sits at the
	// midpoint of this edge, so it is inside the endpoint above by
	// construction. The far side — a loop's label, which is above the node it
	// hangs from — is what normalise handles, and its own test owns that.
	if e.LabelX > e.X2 {
		t.Errorf("the label at x=%v is beyond the end of its own edge at %v", e.LabelX, e.X2)
	}
}
