// Package machinevalidation says where each of a machine's validation errors
// belongs, so that a message can be shown against the thing that caused it.
//
// It decides nothing about validity. Every problem here is one
// agent.ValidateMachine already reported, carried through unchanged; this
// package answers only "which node, which edge, or the machine as a whole", and
// the answer is structural rather than a second reading of the message text.
//
// A sibling of internal/forge/validation rather than an extension of it. That
// package is schema.json's — its owners are components and entity types — and
// the two share the Problem type so both render through components.Problems
// without either pretending to be the other.
package machinevalidation

import (
	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
	"github.com/tmbritton/ecs-db/internal/forge/validation"
)

// Problem is validation's, so one primitive renders both.
type Problem = validation.Problem

// Report is every validation error for one machine, grouped by where it goes.
type Report struct {
	// Machine is what belongs to no node: the machine-level errors, and
	// anything whose state the chart did not draw. The second is why this is
	// never empty by accident — a message with nowhere to go goes here rather
	// than being dropped.
	Machine []Problem
	// States is keyed by dotted path, the same key the canvas keys a node on.
	States map[string][]Problem
	// Edges is keyed by chart edge id.
	Edges map[string][]Problem
}

// Check groups the errors the engine reported onto what the chart drew.
//
// The chart rather than the definition, because the canvas is what the messages
// have to be findable on: a problem attributed to something not drawn is a
// problem nobody can see, which is the one outcome this is for.
func Check(def *agent.MachineDefinition, errs []agent.ValidationError, c chart.Chart) Report {
	report := Report{States: map[string][]Problem{}, Edges: map[string][]Problem{}}
	if len(errs) == 0 {
		return report
	}
	machineID := ""
	if def != nil {
		machineID = def.ID
	}
	nodes := nodePaths(c.Nodes)

	for _, e := range errs {
		p := Problem{
			Owner: validation.Owner{Kind: validation.OwnerMachine, Name: machineID},
			Field: e.Field,
			// Every one of these is a reason ValidateMachineError fails, and
			// that is the check the session saves through — so every one of
			// them is a reason the save would be refused. There is no
			// non-blocking kind here to distinguish.
			Blocking: true,
			Message:  e.Message,
		}
		switch {
		case e.StatePath == "":
			report.Machine = append(report.Machine, p)
		case !nodes[e.StatePath]:
			// Named a state the chart did not draw. Rather than dropping it —
			// which is the only way a real error becomes invisible — it goes to
			// the machine, where it is at least readable.
			report.Machine = append(report.Machine, p)
		case e.Origin == agent.OriginTransition:
			if id, ok := edgeFor(c, e); ok {
				report.Edges[id] = append(report.Edges[id], p)
				continue
			}
			// About a transition, and no edge matched — the chart draws one per
			// transition, so this should not happen. It goes to the state that
			// declares it rather than nowhere.
			report.States[e.StatePath] = append(report.States[e.StatePath], p)
		default:
			report.States[e.StatePath] = append(report.States[e.StatePath], p)
		}
	}
	return report
}

// edgeFor finds which of a state's transitions an error is about.
//
// Only ever called for an error the engine already said is about a transition:
// matching on Field alone put a state's own errors on its edges, because an
// entry action and a transition action are the same action type on the same
// state, and a state's `initial` is a string a sibling transition may target.
// Both left the node unmarked and filed its message under a transition.
//
// Which of them is still structural, and not read off the message: the engine's
// wording is not an API, and a switch over ten message prefixes would be a
// second copy of validator.go that goes stale the first time one is reworded.
// What an error about a transition can name is that transition's target, its
// guard, its duration or one of its actions, all of which the edge carries.
//
// An error matching two edges is attached to the first in the chart's order,
// which is the file's. Two transitions out of one state naming the same missing
// guard are two faults with one message between them; putting it on both would
// double the count, and the count is what the footer reports.
func edgeFor(c chart.Chart, e agent.ValidationError) (string, bool) {
	// No guard on an empty Field. There was one, against an internal
	// transition's empty target swallowing every error that carried no field —
	// and Origin removed the case: every error the engine reports about a
	// transition names its target, its guard, an action or its duration, and a
	// target error is only reported for a target that is not empty.
	for _, edge := range c.Edges {
		if edge.From != e.StatePath {
			continue
		}
		if edge.Target == e.Field || edge.Guard == e.Field ||
			(edge.Kind == chart.EdgeAfter && edge.Event == e.Field) ||
			contains(edge.Actions, e.Field) {
			return edge.ID, true
		}
	}
	return "", false
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// nodePaths is every path the chart drew, including nested ones.
func nodePaths(nodes []chart.Node) map[string]bool {
	out := map[string]bool{}
	var walk func([]chart.Node)
	walk = func(ns []chart.Node) {
		for _, n := range ns {
			out[n.Path] = true
			walk(n.Children)
		}
	}
	walk(nodes)
	return out
}

// ── what the canvas, the rail and the footer ask ─────────────────────────────

// ForState is every problem about one node, by dotted path.
func (r Report) ForState(path string) []Problem { return r.States[path] }

// ForEdge is every problem about one transition, by chart edge id.
func (r Report) ForEdge(id string) []Problem { return r.Edges[id] }

// HasState and HasEdge are what the canvas marks on. A mark rather than a
// message, because a message in the rail is no use for a node scrolled out of
// view — the lesson Epic 12 Story 7 learned from a problem on an unselected row
// being invisible behind a disabled Save.
func (r Report) HasState(path string) bool { return len(r.States[path]) > 0 }

func (r Report) HasEdge(id string) bool { return len(r.Edges[id]) > 0 }

// Count is how many problems there are, wherever they went — and deliberately
// the only aggregate here. A Blocked() and an All() were written alongside it
// and neither ever had a caller outside a test: the footer blocks on
// Session.Invalid, which counts across every machine a Save would write rather
// than the one on screen, and every list rendered is one group.
//
// Order comes from the engine. Each group holds its problems in the order
// ValidateMachine reported them, which since Story 8 is the file's — so the
// page stream, which patches an element only when its markup changed, sees the
// same markup for the same machine.
func (r Report) Count() int {
	n := len(r.Machine)
	for _, ps := range r.States {
		n += len(ps)
	}
	for _, ps := range r.Edges {
		n += len(ps)
	}
	return n
}
