package modes

import (
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
)

// Chart types are aliased for the same reason Component and MachineDefinition
// are: a .templ file in this package cannot import chart without colliding with
// templ's codegen.
type (
	Statechart = chart.Chart
	ChartNode  = chart.Node
	ChartEdge  = chart.Edge
)

// num formats a coordinate for markup, to a tenth of a pixel.
//
// One decimal and not shortest-round-trip, which is what this used to do while
// its comment claimed otherwise. The chart rounds the geometry it computes, but
// a position read straight out of a file does not go through that, and neither
// do the sums that size a compound box — so a state whose meta says x: 0.1
// produced width:224.29999999999998px. It is deterministic either way, so the
// stream's patch suppression was never at risk; it is noise in every frame on
// the wire and in every diff, and Story 5 writes dragged coordinates into
// exactly the field this comes from.
//
// Trailing zeros are trimmed so a whole number stays a whole number: "14"
// rather than "14.0", which is what most of a chart is.
func num(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}

// canvasStyle sizes the canvas to what it holds, so the scroll container has
// something to scroll.
func canvasStyle(c Statechart) templ.SafeCSS {
	return templ.SafeCSS("width:" + num(c.W) + "px;height:" + num(c.H) + "px")
}

// nodeStyle places a node inside its parent. Relative coordinates, so a
// compound state's children move with it and the browser does the arithmetic.
func nodeStyle(n ChartNode) templ.SafeCSS {
	return templ.SafeCSS("left:" + num(n.X) + "px;top:" + num(n.Y) + "px;" +
		"width:" + num(n.W) + "px;height:" + num(n.H) + "px")
}

func headStyle(n ChartNode) templ.SafeCSS {
	return templ.SafeCSS("height:" + num(n.HeadH) + "px")
}

// labelStyle centres an edge's label on its midpoint.
func labelStyle(e ChartEdge) templ.SafeCSS {
	return templ.SafeCSS("left:" + num(e.LabelX) + "px;top:" + num(e.LabelY) + "px")
}

// edgePath is the SVG geometry: a straight line, or an arc over the node for a
// transition that starts and ends in the same place.
//
// Straight lines with a midpoint label, deliberately. The design's curves are
// worth nothing if the wrong states are connected, and routing is the part of
// this most likely to eat the story.
func edgePath(e ChartEdge) string {
	if e.Loop {
		return "M " + num(e.X1) + " " + num(e.Y1) +
			" C " + num(e.X1) + " " + num(e.Y1-e.Arc) +
			" " + num(e.X2) + " " + num(e.Y2-e.Arc) +
			" " + num(e.X2) + " " + num(e.Y2)
	}
	return "M " + num(e.X1) + " " + num(e.Y1) + " L " + num(e.X2) + " " + num(e.Y2)
}

// edgeClass says in the markup what the stroke says in the picture: a guarded
// transition is dashed because it might not fire, a dangling one is red because
// it goes nowhere.
func edgeClass(e ChartEdge) string {
	classes := []string{"chart-edge"}
	switch {
	case e.Dangling:
		classes = append(classes, "chart-edge--dangling")
	case e.Guard != "":
		classes = append(classes, "chart-edge--guarded")
	}
	if e.Selected {
		classes = append(classes, "chart-edge--selected")
	}
	return strings.Join(classes, " ")
}

// edgeMarker picks the arrowhead. Three of them rather than one inheriting the
// line's colour: context-stroke is SVG 2 and not everywhere, and an arrowhead
// in the wrong hue on a broken transition is the one place it would be read as
// meaning something.
func edgeMarker(e ChartEdge) templ.SafeURL {
	switch {
	case e.Selected:
		return templ.SafeURL("url(#chart-arrow-selected)")
	case e.Dangling:
		return templ.SafeURL("url(#chart-arrow-dangling)")
	default:
		return templ.SafeURL("url(#chart-arrow)")
	}
}

func nodeClass(n ChartNode) string {
	classes := []string{"chart-node", "chart-node--" + string(n.Kind)}
	if n.Selected {
		classes = append(classes, "chart-node--selected")
	}
	return strings.Join(classes, " ")
}

// historyMark is what a history node is drawn as. H for shallow, H* for deep,
// which is the notation statecharts have used since Harel.
func historyMark(n ChartNode) string {
	if n.History == "deep" {
		return "H*"
	}
	return "H"
}

// selectStateHref and selectEdgeHref are how selection is made.
//
// A URL and not a signal: selection survives a reload, it can be linked, and
// the inspectors in Stories 6 and 7 read it from the request rather than from
// client state. One parameter for both kinds, so exactly one thing being
// selected is structural rather than a rule about which of two wins.
func selectStateHref(data Data, path string) string {
	return selectionHref(data, chart.SelState+path)
}

func selectEdgeHref(data Data, id string) string {
	return selectionHref(data, chart.SelEdge+id)
}

// clearSelectionHref is the canvas ground: clicking off everything deselects.
func clearSelectionHref(data Data) string { return selectionHref(data, "") }

func selectionHref(data Data, sel string) string {
	href := machineHref(data.SelectedMachine)
	if sel != "" {
		href += "&sel=" + urlValue(sel)
	}
	return href
}

// selectionLabel is what is selected, in words.
//
// The story asks for the selection to be readable from the DOM rather than only
// visible, and a class name on a box is neither: it is invisible to a screen
// reader and it is three lookups away from a test that wants to know what is
// selected. This is one element that says.
func selectionLabel(data Data) string {
	sel := data.Chart.Selected
	switch {
	case strings.HasPrefix(sel, chart.SelState):
		return "state " + strings.TrimPrefix(sel, chart.SelState)
	case strings.HasPrefix(sel, chart.SelEdge):
		id := strings.TrimPrefix(sel, chart.SelEdge)
		for _, e := range data.Chart.Edges {
			if e.ID == id {
				return "transition " + e.From + " · " + e.Label()
			}
		}
		return "transition " + id
	default:
		return "nothing selected"
	}
}

// canvasEmpty is a machine with no states at all — the state a machine created
// from the skeleton is in until someone adds one.
func canvasEmpty(data Data) bool { return len(data.Chart.Nodes) == 0 }

// stateKindLabel names what a node is, for the title attribute, since the
// borders that distinguish the kinds are only borders.
func stateKindLabel(n ChartNode) string {
	switch n.Kind {
	case agent.StateTypeCompound:
		return "compound state"
	case agent.StateTypeParallel:
		return "parallel state"
	case agent.StateTypeFinal:
		return "final state"
	case agent.StateTypeHistory:
		if n.History == "deep" {
			return "deep history"
		}
		return "shallow history"
	default:
		return "state"
	}
}
