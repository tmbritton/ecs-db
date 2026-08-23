package modes

import (
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
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

// ── the canvas's own actions ──────────────────────────────────────────────────

// dropAction turns a completed drag into a request.
//
// The JS dispatches one CustomEvent carrying what the drag meant; this reads it
// back out of evt.detail and posts. That is the whole boundary: pointer state
// is the client's, and everything past it is an ordinary Forge action.
//
// A delta for a move, not a position. Only the server knows the offsets between
// where a box is drawn and the coordinate the file records.
func dropAction(data Data) string {
	machine := "machine=" + urlValue(data.SelectedMachine)
	return "evt.detail.action === 'move' " +
		"? @post('/forge/agents/state?" + machine +
		"&move=' + encodeURIComponent(evt.detail.path) + '&dx=' + evt.detail.dx + '&dy=' + evt.detail.dy) " +
		": @post('/forge/agents/transition?op=connect&" + machine +
		"&from=' + encodeURIComponent(evt.detail.from) + '&to=' + encodeURIComponent(evt.detail.to))"
}

// addStateAction adds a state where the pointer is.
//
// offsetX/offsetY are relative to the element the handler is on, which is the
// canvas — so this needs no JS at all, and the coordinate arrives in the same
// space the chart draws in.
func addStateAction(data Data) string {
	return "@post('/forge/agents/state?machine=" + urlValue(data.SelectedMachine) +
		"&add=1&x=' + evt.offsetX + '&y=' + evt.offsetY)"
}

// menuAction opens the right-click menu over whatever was clicked.
//
// preventDefault, or the browser's own menu covers ours. Browsers also fire
// contextmenu for the keyboard menu key on the focused element, which is what
// keeps rename, set-initial and delete reachable without a pointer until
// Story 6 gives them an inspector.
func menuAction(data Data, target string, params ...string) string {
	url := "/forge/agents/menu?machine=" + urlValue(data.SelectedMachine) + "&target=" + urlValue(target)
	for i := 0; i+1 < len(params); i += 2 {
		url += "&" + params[i] + "=" + urlValue(params[i+1])
	}
	// Viewport coordinates, and the menu is positioned fixed. offsetX is
	// relative to the element the handler is on, so a menu asked for on a node
	// would open near the top-left of the canvas instead of under the pointer —
	// and the keyboard menu key, which reports the focused element's corner,
	// would be wrong in a different way.
	return "evt.preventDefault(); @post('" + url + "&x=' + evt.clientX + '&y=' + evt.clientY)"
}

func closeMenuAction() string {
	return "@post('/forge/agents/menu?close=1')"
}

// menuStyle places the menu where the pointer was, in viewport coordinates.
func menuStyle(m CanvasMenu) templ.SafeCSS {
	return templ.SafeCSS("left:" + num(m.X) + "px;top:" + num(m.Y) + "px")
}

// canvasMenuItems is what the open menu offers, which depends on what it is
// about.
func canvasMenuItems(data Data) []components.MenuItem {
	m := data.CanvasMenu
	machine := "machine=" + urlValue(data.SelectedMachine)
	switch m.Kind {
	case "state":
		state := urlValue(m.State)
		return []components.MenuItem{
			{
				Label: "Rename…",
				// Guarded, so cancelling the prompt does nothing. Posting the
				// empty string that Cancel returns raised "a state needs a
				// name" in the banner and an error in the log, which is a
				// refusal for an operation nobody asked to perform.
				Action: "(name => name && @post('/forge/agents/state?" + machine + "&rename=" + state +
					"&to=' + encodeURIComponent(name)))(prompt('Rename this state to:', " +
					jsString(shortName(m.State)) + "))",
			},
			{Label: "Set as initial", Action: "@post('/forge/agents/state?" + machine + "&initial=" + state + "')"},
			{Divider: true},
			{
				Label:  "Delete state",
				Danger: true,
				Action: confirmAction(data.CanvasMenuWarning,
					"@post('/forge/agents/state?"+machine+"&delete="+state+"')"),
			},
		}
	case "edge":
		return []components.MenuItem{{
			Label:  "Delete transition",
			Danger: true,
			Action: "@post('/forge/agents/transition?op=delete&" + machine + "&from=" + urlValue(m.From) +
				"&kind=" + urlValue(m.EdgeKind) + "&event=" + urlValue(m.Event) + "&index=" + urlValue(m.Index) + "')",
		}}
	default:
		// The menu's coordinates are the viewport's; the canvas wants its own.
		// The difference is read at click time from the element that knows it,
		// rather than stored — a chart that scrolled between opening the menu
		// and choosing from it would otherwise put the state somewhere else.
		return []components.MenuItem{{
			Label: "Add state here",
			Action: "@post('/forge/agents/state?" + machine + "&add=1&x=' + (" + num(m.X) +
				" - document.querySelector('.chart').getBoundingClientRect().left) + '&y=' + (" +
				num(m.Y) + " - document.querySelector('.chart').getBoundingClientRect().top))",
		}}
	}
}

// canvasMenuTitle names what the menu is about, in the mono caps the primitive
// expects.
func canvasMenuTitle(m CanvasMenu) string {
	switch m.Kind {
	case "state":
		return "STATE · " + m.State
	case "edge":
		return "TRANSITION · " + m.Event
	default:
		return "CANVAS"
	}
}

// shortName is the leaf of a dotted path, which is what a rename edits.
func shortName(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

// edgeIndex is which of the transitions on one event this edge is.
//
// Read off the edge rather than taken back out of its id, which joins four
// fields with bars — two of which may contain one.
func edgeIndex(e ChartEdge) string { return strconv.Itoa(e.Index) }
