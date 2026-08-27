// Package chart turns a parsed XState machine into a drawable statechart:
// a node per state, an edge per transition, laid out and ready to position.
//
// It is deliberately free of HTML. The canvas is on a two-second page stream
// that patches an element only when its markup changed, so anything
// non-deterministic in here does not merely flicker — it re-sends the whole
// mode content forever, including the inputs someone may be typing in. Keeping
// the geometry in a package with no templates in it means that property is
// pinned by a table test rather than by reading rendered markup.
package chart

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
)

// Node is one state, positioned.
//
// Two coordinate systems, and they are named apart on purpose. X and Y are
// relative to the node's parent, because that is what the DOM needs: a child
// inside a compound state's box is placed by CSS, so Story 5 can move a parent
// and its children go with it without anything recomputing them. AbsX and AbsY
// are absolute on the canvas, because the edges are drawn in one SVG layer
// spanning the whole thing. Both are worked out in the same walk, so they
// cannot disagree.
type Node struct {
	// Path is the dotted path from the machine root — "combat.attacking" — and
	// is the node's identity.
	//
	// Not StateNode.ID, which cannot be one: unless a state declares an "id" of
	// its own, parsing gives it the machine id plus its own name at every
	// depth, so a nested state named "alert" is "goblin.alert" exactly as a
	// top-level one would be, and two compound states with a same-named child
	// collide.
	//
	// Unique as long as no state name contains a dot — which XState already
	// requires, since a dot is its path separator and a state named "a.b"
	// cannot be targeted unambiguously by the engine either. Nothing rejects
	// one, here or in the engine: a machine with such a name gets two nodes
	// with the same path, and the canvas draws it as the interpreter's own
	// resolver reads it. Rejecting the name belongs with Story 8, which is
	// where the machine's problems are reported.
	Path string
	// Name is the leaf name, which is what the node is labelled with.
	Name string
	Kind agent.StateType
	// History is "shallow" or "deep" on a history node, empty otherwise.
	History string
	// Initial is whether the machine, or this node's parent, names it as the
	// one to enter.
	Initial bool
	// Entry is the entry actions, already labelled.
	Entry []string

	// X, Y is where the node is drawn, relative to its parent's border box —
	// which is *not* the position the file records, except at the top level.
	// A nested node's X is its recorded x plus the parent's inner margin, and
	// its Y is its recorded y plus the parent's own title height, so that a
	// compound state's children move with it. Story 5 writes back into meta and
	// must subtract both, plus the chart's OffsetX/OffsetY.
	X, Y float64
	// RecordedX, RecordedY is the coordinate that, written into this state's
	// meta, reproduces where the node is now — captured before either offset
	// above is applied, and filled in for a node the fallback grid placed as
	// well as for one the file positions.
	//
	// It is what Story 5's drag adds its delta to. Without it the client would
	// have to know the chart's own shift, the parent's inner margin and the
	// parent's title height to work out what to write back, which is three
	// facts about layout that pointer code has no business knowing.
	RecordedX, RecordedY float64
	AbsX, AbsY           float64 // absolute on the canvas
	W, H                 float64
	// HeadH is how much of the box the name and entry actions take. A compound
	// state's children start below it, so the two cannot be chosen separately
	// without the title ending up underneath a child.
	HeadH float64

	Children []Node
	Selected bool
}

// EdgeKind separates the two maps a transition can live in.
type EdgeKind string

const (
	EdgeOn    EdgeKind = "on"
	EdgeAfter EdgeKind = "after"
)

// Edge is one transition, drawn.
type Edge struct {
	// ID is source path, kind, key and index — stable between two renders of
	// the same file, which is what a selection in the URL needs.
	ID   string
	From string // node path
	To   string // node path; empty when dangling or internal
	// Event is the event name, or the raw duration string for an after
	// transition, since that is what the key is.
	Event string
	// Index is which of the transitions on that key this one is.
	//
	// Carried rather than recovered from the id, which joins four fields with
	// bars: the inspector needs all four to address the transition, and taking
	// them back out of a string a state name may contain a bar in is guesswork.
	Index int
	// Guard is the cond's type, empty when the transition is unconditional.
	Guard string
	// Actions are the action types this transition runs, in authored order.
	//
	// Carried so that a validation error naming one can be attached to the edge
	// that runs it. Everything else an error about a transition can name — its
	// target, its guard, its duration — the edge already holds; without this,
	// "transition action X is not registered" would have to fall back to the
	// state, which is a near miss rather than an answer.
	Actions []string
	Kind    EdgeKind
	// Target is the raw target text as authored, so a dangling edge can name
	// the state that is missing.
	Target string
	// Dangling is a target that resolves to no state. Drawn rather than
	// dropped: a transition you cannot see is one you cannot fix.
	Dangling bool
	// Internal is a transition with no target at all — it runs its actions and
	// changes no state, which is a legitimate thing to write and not a fault.
	Internal bool
	// Loop is drawn as an arc over the source node rather than as a line
	// between two: a self-transition and an internal one both start and end in
	// the same place.
	Loop bool

	X1, Y1, X2, Y2 float64
	// LabelX, LabelY is where the label chip goes — the midpoint of the line,
	// or above the node for a loop.
	LabelX, LabelY float64
	// Arc is how far above the node a loop rises. Higher for each loop after
	// the first, so a state with several self-transitions shows all of them.
	Arc      float64
	Selected bool
}

// Label is what the edge reads as on the canvas.
func (e Edge) Label() string {
	label := e.Event
	if e.Kind == EdgeAfter {
		label = "after " + e.Event
	}
	if e.Guard != "" {
		label += " [" + e.Guard + "]"
	}
	if e.Dangling {
		label += " → " + e.Target + "?"
	}
	return label
}

// Chart is a whole machine, drawn.
type Chart struct {
	Nodes []Node
	Edges []Edge
	W, H  float64
	// OffsetX, OffsetY is how far everything was moved to bring it inside the
	// canvas. A recorded position may be negative — hand-edited, or written by
	// another tool — and a self-transition's arc rises above the node it loops
	// over, so the top-left of what is drawn is not always the origin. Nothing
	// scrolls into negative overflow, so a node or a label left up there is
	// invisible and unclickable, which is layout deciding whether a node
	// exists.
	//
	// Story 5 writes dragged positions back into meta and must subtract this,
	// or every drag would move the node by however far the chart happened to
	// be shifted on the render it was dragged from.
	OffsetX, OffsetY float64
	// Selected is the selection that actually resolved — "state:<path>" or
	// "edge:<id>" — and is empty when the URL named something that is not
	// there. One field, because one thing is selected at a time and making
	// that structural is cheaper than a rule about which of two wins.
	Selected string
}

// EdgeID is how a transition is named in a selection: its source, which of the
// two maps it is in, the key it is under and which of the transitions on that
// key it is.
//
// One place that spells it, because the server builds the same id when an edit
// moves a transition and the selection has to follow — and two spellings of a
// four-field join would agree until the day they did not.
func EdgeID(from, kind, key string, index int) string {
	return fmt.Sprintf("%s|%s|%s|%d", from, kind, key, index)
}

// Selection prefixes. A single ?sel= parameter carries both kinds.
const (
	SelState = "state:"
	SelEdge  = "edge:"
)

// Geometry. The node width is the prototype's, widened once; the rest follows
// from it.
//
// The prototype drew "setAnimation · walk" and 168px held it. This project's
// animations are named goblin_idle and goblin_walk, so the real entry is
// "setAnimation · goblin_walk" — 178px of text in a 164px line box, which
// truncated to "setAnimation · goblin_…" in every state that had one. Two
// different states rendering the same string is a node that has stopped saying
// what its state does, so the box grew to hold what this project actually
// writes rather than what the mock did.
const (
	nodeW   = 184
	titleH  = 26 // the name line
	lineH   = 14 // and one entry action
	bodyPad = 10
	histW   = 34
	histH   = 34
	// Wide enough that the line between two states is visible beside its own
	// label: at a 52px gap the label chip covered the whole edge, and an edge
	// you cannot see is one you cannot tell from a coincidence of placement.
	cellW      = 300
	cellH      = 170
	gap        = 132
	cols       = 3
	slotInsetX = 14 // where the fallback grid starts within its level
	slotInsetY = 14
	padX       = 14 // a compound state's inner margin
	margin     = 28 // around the whole chart
	loopArc    = 34
	fanStep    = 26 // how far apart edges sharing a pair of states are pushed
	// A label chip is about 10px of text with 4px of padding, and they are
	// worth keeping apart over a generous width because they are words.
	labelClearX = 74
	labelClearY = 15
	labelStep   = 16
	labelTries  = 8
	// What a label chip is assumed to take up either side of its own position.
	// The real size is the text's, which only the browser knows; this is enough
	// that a label above the top of the canvas is brought fully back on rather
	// than half on. Only normalise reads it: on the far side, a label sits at
	// the midpoint of its own edge and so is inside the endpoints already
	// counted.
	labelHalfW = 60
	labelHalfH = 10
)

// PadX is a compound state's inner margin, exported for the tests that pin how
// a recorded position maps onto a rendered one. Story 5 writes dragged
// positions back into meta and has to undo the same offset.
const PadX = padX

// Build draws def, resolving sel against what it drew.
//
// The definition and nothing else: the canvas never consults the validator. A
// machine that does not validate is precisely the one someone opened the editor
// to fix, and a chart that refused to draw it would send them back to a text
// editor at the moment the tool is most use.
func Build(def *agent.MachineDefinition, sel string) Chart {
	var c Chart
	if def == nil {
		return c
	}
	// The root level's origin is (0,0), so a top-level node's absolute position
	// is the one the file records — the canvas's own breathing room is the
	// stylesheet's business, not something baked into every coordinate the
	// editor writes back.
	c.Nodes = buildLevel(def.States, def.StateOrder, def.Initial, "")
	place(c.Nodes, 0, 0)
	c.Edges = buildEdges(def, c.Nodes)
	normalise(&c)
	c.W, c.H = extent(c.Nodes, c.Edges)
	c.Selected = applySelection(&c, sel)
	return c
}

// buildNodes lays out one level of the tree, in authored order.
//
// Authored order throughout, never map order: states are usually written in the
// order they run, the file is read in diffs, and — the reason this is a rule
// rather than a preference — a map range here would give the canvas a different
// drawing on every render of the page stream.
// buildLevel lays out the states at one level of the tree, in their own
// coordinate system starting at (0,0). The caller offsets them into place: the
// root not at all, a compound state by its own inner margin and title height.
//
// One level in one coordinate system is what makes a child's recorded position
// relative to the state that holds it, which is what lets Story 5 move a parent
// without rewriting every descendant's meta.
func buildLevel(states map[string]*agent.StateNode, order []string, initial, prefix string) []Node {
	names := orderedNames(states, order)
	out := make([]Node, 0, len(names))
	for _, name := range names {
		state := states[name]
		node := Node{
			Path:    prefix + name,
			Name:    name,
			Kind:    state.Type,
			History: state.History,
			Initial: initial == name,
			Entry:   actionLabels(state.Entry),
		}
		node.HeadH = headHeight(node)
		node.W, node.H = size(node)
		if len(state.Children) > 0 {
			node.Children = buildLevel(state.Children, state.StateOrder, state.Initial, node.Path+".")
			// Below the title, not over it.
			offsetLevel(node.Children, padX, node.HeadH)
			node.W, node.H = fit(node)
		}
		out = append(out, node)
	}
	// Positions last, because the cell a state falls back to has to be big
	// enough for the biggest state at this level — a compound node grown to
	// hold three children is wider than the default cell, and a grid that
	// ignored that would stack it on top of its own siblings.
	cw, ch := cellSize(out)
	for i := range out {
		// The slot is keyed on the index in authored order rather than on
		// which slots are still free, and the cell size on what is at this
		// level rather than on what is placed. Otherwise giving one state a
		// position in Story 5 would shuffle every unpositioned state after it,
		// and a layout that moves things you did not touch never settles.
		out[i].X, out[i].Y = slot(i, cw, ch)
		if x, y, ok := Position(states[out[i].Name]); ok {
			out[i].X, out[i].Y = x, y
		}
		// Captured before offsetLevel and before normalise, which is the whole
		// point: this is the position in the file's own terms.
		out[i].RecordedX, out[i].RecordedY = out[i].X, out[i].Y
	}
	return out
}

// offsetLevel shifts one level into its container. Not recursive: a
// grandchild's position is relative to its own parent, which has not moved.
func offsetLevel(nodes []Node, dx, dy float64) {
	for i := range nodes {
		nodes[i].X += dx
		nodes[i].Y += dy
	}
}

// place fills in the absolute positions, once the relative ones are settled.
func place(nodes []Node, absX, absY float64) {
	for i := range nodes {
		nodes[i].AbsX, nodes[i].AbsY = absX+nodes[i].X, absY+nodes[i].Y
		place(nodes[i].Children, nodes[i].AbsX, nodes[i].AbsY)
	}
}

// slot is the fallback grid position for the i'th state at a level.
func slot(i int, cw, ch float64) (x, y float64) {
	return slotInsetX + float64(i%cols)*cw, slotInsetY + float64(i/cols)*ch
}

// cellSize is the grid step at one level: big enough for the widest and tallest
// state there, never smaller than the default.
func cellSize(nodes []Node) (w, h float64) {
	w, h = cellW, cellH
	for _, n := range nodes {
		w = math.Max(w, n.W+gap)
		h = math.Max(h, n.H+gap)
	}
	return w, h
}

// headHeight is the name line plus one line per entry action.
//
// One line per entry is a promise the stylesheet has to keep: .chart-node__entry
// sets white-space:nowrap and a 14px line box for exactly this reason. It once
// did neither, and an entry too wide for the node wrapped — so a state with
// three entry actions drew four lines in a box built for three, and the last
// one hung out below the border. titleH and lineH are the two numbers that have
// to match the CSS; nothing else here does.
func headHeight(n Node) float64 {
	if n.Kind == agent.StateTypeHistory {
		return histH
	}
	return titleH + float64(len(n.Entry))*lineH
}

func size(n Node) (w, h float64) {
	if n.Kind == agent.StateTypeHistory {
		return histW, histH
	}
	return nodeW, n.HeadH + bodyPad
}

// fit grows a compound node to hold its children.
func fit(n Node) (w, h float64) {
	w, h = n.W, n.H
	for _, child := range n.Children {
		w = math.Max(w, child.X+child.W+padX)
		h = math.Max(h, child.Y+child.H+padX)
	}
	return w, h
}

// extent sizes the canvas to everything drawn on it, edges and labels included.
//
// Not just the nodes: a dangling edge points off to the right of its source and
// a label chip is centred on its own position, so a canvas measured from the
// boxes alone clips exactly the things that are there to be noticed.
func extent(nodes []Node, edges []Edge) (w, h float64) {
	for _, n := range nodes {
		w = math.Max(w, n.X+n.W+margin)
		h = math.Max(h, n.Y+n.H+margin)
	}
	// The endpoints only. A label sits at the midpoint of its own edge, so it
	// is inside the two points already counted here — the far side is where a
	// label can fall outside, and normalise is what handles that.
	for _, e := range edges {
		w = math.Max(w, math.Max(e.X1, e.X2)+margin)
		h = math.Max(h, math.Max(e.Y1, e.Y2)+margin)
	}
	return w, h
}

// normalise moves everything down and right until none of it is off the top or
// left of the canvas.
//
// Two things put it there. A recorded position may be negative — hand-edited,
// or written by another tool — and a self-transition arcs above the node it
// loops over, so a state in the first row of the fallback grid has its loop and
// its label above the origin. Nothing scrolls into negative overflow: both are
// painted outside the clip, cannot be brought into view, and cannot be clicked.
// A label is the only pointer target an edge has, so a clipped one is a
// transition that has quietly stopped existing.
func normalise(c *Chart) {
	minX, minY := math.Inf(1), math.Inf(1)
	for _, n := range c.Nodes {
		minX, minY = math.Min(minX, n.X), math.Min(minY, n.Y)
	}
	for _, e := range c.Edges {
		minX = math.Min(minX, math.Min(e.X1, e.X2))
		minY = math.Min(minY, math.Min(e.Y1, e.Y2))
		// The label covers the arc as well: a loop's label sits at its own arc
		// height, which is the highest point either reaches.
		minX = math.Min(minX, e.LabelX-labelHalfW)
		minY = math.Min(minY, e.LabelY-labelHalfH)
	}
	if math.IsInf(minX, 1) {
		return
	}
	// To zero, not to a margin. Shifting everything so the leftmost thing sits
	// at a comfortable inset would make the whole chart's coordinates depend on
	// which states happen to be placed, so giving one state a position would
	// move every other one on screen — the settling problem the fallback slot
	// rule exists to avoid, reintroduced one layer up. The breathing room
	// around the canvas is the stylesheet's, where it costs nothing.
	dx, dy := math.Max(0, -minX), math.Max(0, -minY)
	if dx == 0 && dy == 0 {
		return
	}
	c.OffsetX, c.OffsetY = dx, dy
	// The top level only: a child's position is relative to its parent, which
	// has already moved.
	offsetLevel(c.Nodes, dx, dy)
	shiftAbs(c.Nodes, dx, dy)
	for i := range c.Edges {
		e := &c.Edges[i]
		e.X1, e.Y1 = e.X1+dx, e.Y1+dy
		e.X2, e.Y2 = e.X2+dx, e.Y2+dy
		e.LabelX, e.LabelY = e.LabelX+dx, e.LabelY+dy
	}
}

func shiftAbs(nodes []Node, dx, dy float64) {
	for i := range nodes {
		nodes[i].AbsX, nodes[i].AbsY = nodes[i].AbsX+dx, nodes[i].AbsY+dy
		shiftAbs(nodes[i].Children, dx, dy)
	}
}

// buildEdges walks every state's on and after maps, in authored order.
func buildEdges(def *agent.MachineDefinition, nodes []Node) []Edge {
	index := indexNodes(nodes)
	// One resolver for the whole machine: a chart's transitions name the same
	// handful of states over and over, and each unresolved one costs a walk of
	// the entire tree before it gives up.
	resolver := agent.NewStateResolver(def)
	var out []Edge
	var walk func(states map[string]*agent.StateNode, order []string, prefix string)
	walk = func(states map[string]*agent.StateNode, order []string, prefix string) {
		for _, name := range orderedNames(states, order) {
			state := states[name]
			path := prefix + name
			for _, key := range orderedKeys(state.On, state.OnOrder) {
				for i, t := range state.On[key] {
					out = append(out, edge(resolver, path, EdgeOn, key, i, t))
				}
			}
			for _, key := range orderedKeys(state.After, state.AfterOrder) {
				for i, t := range state.After[key] {
					out = append(out, edge(resolver, path, EdgeAfter, key, i, t))
				}
			}
			walk(state.Children, state.StateOrder, path+".")
		}
	}
	walk(def.States, def.StateOrder, "")
	routeAll(out, index)
	return out
}

// routeAll works out where every line runs, fanning the ones that share a pair
// of states apart.
//
// Three transitions between the same two states land on the same straight line
// and their labels on the same midpoint, so the picture says "these two states
// are connected" and refuses to say how many times or by what. Spreading them
// perpendicular to the line is the cheapest honest answer, and it keeps each
// label its own clickable target.
func routeAll(edges []Edge, index map[string]Node) {
	groups := map[string][]int{}
	var order []string
	for i, e := range edges {
		key := groupKey(e)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], i)
	}
	for _, key := range order {
		members := groups[key]
		for k, i := range members {
			route(&edges[i], index, k, len(members))
		}
	}
	separateLabels(edges)
}

// separateLabels pushes apart labels that landed on top of each other.
//
// It tries a bounded number of times and then gives up, leaving the label where
// it is: a machine with a dozen mutually-colliding transitions still ends with
// some overlap. Bounded and deterministic beats a loop that can run for as long
// as the input is bad, and the alternative — moving labels arbitrarily far from
// the line they belong to — is a worse answer than a little overlap.
//
// Fanning handles edges that share a pair of states; this handles the ones that
// do not and collide anyway — a transition into a compound state and one into
// its child run almost the same line, and their midpoints are a few pixels
// apart. Two labels in the same place read as one word made of two, and the
// one underneath cannot be clicked at all.
//
// In edge order, which is authored order, so the answer is the same every time.
func separateLabels(edges []Edge) {
	placed := make([][2]float64, 0, len(edges))
	for i := range edges {
		x, y := edges[i].LabelX, edges[i].LabelY
		for tries := 0; tries < labelTries && overlaps(placed, x, y); tries++ {
			y += labelStep
		}
		edges[i].LabelX, edges[i].LabelY = x, y
		placed = append(placed, [2]float64{x, y})
	}
}

func overlaps(placed [][2]float64, x, y float64) bool {
	for _, p := range placed {
		if math.Abs(p[0]-x) < labelClearX && math.Abs(p[1]-y) < labelClearY {
			return true
		}
	}
	return false
}

// groupKey is unordered for an ordinary edge: A→B and B→A run along the same
// line, so they have to be fanned apart together or they land on top of each
// other having each been fanned to the middle of their own group of one.
func groupKey(e Edge) string {
	switch {
	case e.Internal || e.To == e.From:
		return e.From + "|loop"
	case e.Dangling:
		return e.From + "|dangling"
	case e.From < e.To:
		return e.From + "|" + e.To
	default:
		return e.To + "|" + e.From
	}
}

func edge(resolver *agent.StateResolver, from string, kind EdgeKind, key string, i int, t agent.Transition) Edge {
	e := Edge{
		ID:     EdgeID(from, string(kind), key, i),
		From:   from,
		Event:  key,
		Index:  i,
		Kind:   kind,
		Target: t.Target,
	}
	if t.Cond != nil {
		e.Guard = t.Cond.Type
	}
	for _, a := range t.Actions {
		e.Actions = append(e.Actions, a.Type)
	}
	if t.Target == "" {
		e.Internal = true
	} else if _, path := resolver.Resolve(t.Target); path != "" {
		// The engine's own resolver, not a second one written here. A chart
		// that disagreed with the interpreter about where an edge goes would
		// be drawing a different machine from the one running.
		e.To = path
	} else {
		e.Dangling = true
	}
	return e
}

// route works out where one line starts and ends. k of n is its place among
// the edges sharing the same pair of states, which is what it is fanned by.
func route(e *Edge, index map[string]Node, k, n int) {
	src, ok := index[e.From]
	if !ok {
		return
	}
	if e.Internal || e.To == e.From {
		loop(e, src, k)
		return
	}
	if e.Dangling {
		// Somewhere to point, or it is not drawn at all. Out to the right of
		// the state that holds the broken transition, which is the state
		// someone has to go and edit.
		e.X2, e.Y2 = src.AbsX+src.W+cellW/2, src.AbsY+src.H/2+spread(k, n)
		e.X1, e.Y1 = clip(src, e.X2, e.Y2)
		e.LabelX, e.LabelY = (e.X1+e.X2)/2, (e.Y1+e.Y2)/2
		return
	}
	dst, ok := index[e.To]
	if !ok {
		return
	}
	// The fanned line runs between two points offset from the centres, and each
	// end is walked out from its own offset point to its own box's boundary.
	//
	// The first version clipped from the true centres and then translated the
	// result by the fan vector, which is not the same thing: it returns a point
	// on the box moved sideways off it, so an arrowhead can float above the
	// target or stop short of it pointing at nothing. Invisible on an
	// axis-aligned layout, where the perpendicular runs along a box edge, which
	// is why the fixture never showed it.
	dx, dy := fan(src, dst, e.From > e.To, spread(k, n))
	sx, sy := src.AbsX+src.W/2+dx, src.AbsY+src.H/2+dy
	tx, ty := dst.AbsX+dst.W/2+dx, dst.AbsY+dst.H/2+dy
	e.X1, e.Y1 = clipFrom(src, sx, sy, tx, ty)
	e.X2, e.Y2 = clipFrom(dst, tx, ty, sx, sy)
	e.LabelX, e.LabelY = (e.X1+e.X2)/2, (e.Y1+e.Y2)/2
}

// spread is how far the k'th of n edges is pushed off the line they share.
//
// Centred on the line, which is what keeps a lone edge on it: for n == 1 the
// expression is (0 - 0) * fanStep. A guard for that case was here and a
// mutation showed it could never fire, which is the difference between a
// safety net and a line that reads like one.
func spread(k, n int) float64 {
	return (float64(k) - float64(n-1)/2) * fanStep
}

// fan turns that distance into a vector perpendicular to the line.
//
// Reversed for an edge running the other way, because the perpendicular of a
// reversed line points the other side and the two directions would otherwise
// be fanned onto each other.
func fan(src, dst Node, reversed bool, by float64) (dx, dy float64) {
	if by == 0 {
		return 0, 0
	}
	vx, vy := (dst.AbsX+dst.W/2)-(src.AbsX+src.W/2), (dst.AbsY+dst.H/2)-(src.AbsY+src.H/2)
	length := math.Hypot(vx, vy)
	if length == 0 {
		return 0, by
	}
	if reversed {
		by = -by
	}
	return -vy / length * by, vx / length * by
}

// loop arcs over the node it starts and ends on, higher for each one after the
// first so a state with several self-transitions shows all of them.
func loop(e *Edge, src Node, k int) {
	e.Loop = true
	e.Arc = loopArc * float64(k+1)
	e.X1, e.Y1 = src.AbsX+src.W*0.3, src.AbsY
	e.X2, e.Y2 = src.AbsX+src.W*0.7, src.AbsY
	e.LabelX, e.LabelY = src.AbsX+src.W/2, src.AbsY-e.Arc
}

// clip walks from a node's centre towards a point and stops at the node's own
// boundary, so the line does not run under the box and the arrowhead lands
// where it can be seen.
func clip(n Node, towardX, towardY float64) (x, y float64) {
	return clipFrom(n, n.AbsX+n.W/2, n.AbsY+n.H/2, towardX, towardY)
}

// clipFrom is the same walk from an arbitrary point inside the box, which is
// what a fanned edge needs: its line does not pass through the centre.
//
// A point outside the box falls back to the centre. That is not a
// hypothetical — a fan wide enough to leave a small box would otherwise walk
// the wrong way and put the endpoint on the far side.
func clipFrom(n Node, fromX, fromY, towardX, towardY float64) (x, y float64) {
	left, top := n.AbsX, n.AbsY
	right, bottom := n.AbsX+n.W, n.AbsY+n.H
	if fromX < left || fromX > right || fromY < top || fromY > bottom {
		fromX, fromY = n.AbsX+n.W/2, n.AbsY+n.H/2
	}
	dx, dy := towardX-fromX, towardY-fromY
	if dx == 0 && dy == 0 {
		return fromX, fromY
	}
	t := math.Inf(1)
	if dx > 0 {
		t = math.Min(t, (right-fromX)/dx)
	} else if dx < 0 {
		t = math.Min(t, (left-fromX)/dx)
	}
	if dy > 0 {
		t = math.Min(t, (bottom-fromY)/dy)
	} else if dy < 0 {
		t = math.Min(t, (top-fromY)/dy)
	}
	return fromX + dx*t, fromY + dy*t
}

func indexNodes(nodes []Node) map[string]Node {
	out := map[string]Node{}
	var walk func([]Node)
	walk = func(nodes []Node) {
		for _, n := range nodes {
			out[n.Path] = n
			walk(n.Children)
		}
	}
	walk(nodes)
	return out
}

// applySelection marks the one thing the URL names, and reports what that
// turned out to be.
//
// A selection naming nothing is dropped in silence rather than refused: it is
// what a bookmark becomes the moment someone renames a state, and it is what
// every link into this machine becomes when the canvas is redrawn from a file
// that changed on disk.
func applySelection(c *Chart, sel string) string {
	switch {
	case strings.HasPrefix(sel, SelState):
		path := strings.TrimPrefix(sel, SelState)
		if path != "" && markNode(c.Nodes, path) {
			return sel
		}
	case strings.HasPrefix(sel, SelEdge):
		id := strings.TrimPrefix(sel, SelEdge)
		for i := range c.Edges {
			if id != "" && c.Edges[i].ID == id {
				c.Edges[i].Selected = true
				return sel
			}
		}
	}
	return ""
}

func markNode(nodes []Node, path string) bool {
	for i := range nodes {
		if nodes[i].Path == path {
			nodes[i].Selected = true
			return true
		}
		if markNode(nodes[i].Children, path) {
			return true
		}
	}
	return false
}

// actionLabels renders entry actions as one line each.
//
// Params sorted by key, and their values shown rather than their names: the
// prototype's "setAnimation · walk" is what tells you which animation, and a
// key order taken from the map would change the label between two renders.
func actionLabels(specs []agent.ActionSpec) []string {
	if len(specs) == 0 {
		return nil
	}
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		label := spec.Type
		for _, key := range sortedKeys(spec.Params) {
			label += " · " + fmt.Sprintf("%v", spec.Params[key])
		}
		out = append(out, label)
	}
	return out
}

// orderedNames is the authored order of a state map, with anything the order
// does not mention appended alphabetically — a state added to the file after
// Forge read it, or a machine built in code, which records no order at all.
func orderedNames(states map[string]*agent.StateNode, order []string) []string {
	names := make([]string, 0, len(states))
	seen := make(map[string]bool, len(states))
	for _, name := range order {
		if _, ok := states[name]; ok && !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	rest := make([]string, 0, len(states)-len(names))
	for name := range states {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(names, rest...)
}

func orderedKeys(m map[string][]agent.Transition, order []string) []string {
	keys := make([]string, 0, len(m))
	seen := make(map[string]bool, len(m))
	for _, key := range order {
		if _, ok := m[key]; ok && !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	rest := make([]string, 0, len(m)-len(keys))
	for key := range m {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

func sortedKeys(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
