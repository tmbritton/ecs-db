package modes

import (
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
	"github.com/tmbritton/ecs-db/internal/forge/machines"
	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
)

// GuardMeta is aliased for the same reason ActionMeta is: a .templ file in this
// package cannot import agent without colliding with templ's codegen.
type GuardMeta = agent.GuardMeta

// selectedEdge is the transition the canvas selection names, or nil.
//
// Read off the chart rather than resolved a second time by the server. The
// chart already marked exactly one edge selected against what it actually drew,
// and taking the panel's answer from anywhere else is how a panel comes to
// describe a transition the canvas is not showing.
func selectedEdge(data Data) *ChartEdge {
	for i := range data.Chart.Edges {
		if data.Chart.Edges[i].Selected {
			return &data.Chart.Edges[i]
		}
	}
	return nil
}

// selectedTransition is what the file holds for that edge.
func selectedTransition(data Data) *agent.Transition {
	e := selectedEdge(data)
	if e == nil || data.Machine == nil {
		return nil
	}
	t, err := machines.TransitionAt(data.Machine, transitionRefOf(*e))
	if err != nil {
		return nil
	}
	return t
}

func transitionRefOf(e ChartEdge) machines.TransitionRef {
	return machines.TransitionRef{From: e.From, Kind: string(e.Kind), Event: e.Event, Index: e.Index}
}

// transitionRoute is where the transition goes, in words — the AC's "source and
// target, both readable", and readable to something reading the page aloud
// rather than only to something looking at two boxes and a line.
func transitionRoute(e ChartEdge) string {
	switch {
	case e.Internal:
		return e.From + " → stays in " + e.From
	case e.Dangling:
		return e.From + " → " + e.Target + " · no state of that name"
	default:
		return e.From + " → " + e.To
	}
}

// eventLabel names the field, which means something different for an after
// transition: its key is a duration rather than an event name.
func eventLabel(e ChartEdge) string {
	if e.Kind == chart.EdgeAfter {
		return "after"
	}
	return "event"
}

func isAfter(e ChartEdge) bool { return e.Kind == chart.EdgeAfter }

// targetOptions is every state in this machine, plus the explicit none.
//
// The none is not decoration: a transition with actions and no target runs them
// and changes no state, which is a legitimate thing to write. A dropdown that
// could not say it would silently retarget every internal transition the first
// time anyone opened this panel.
//
// A target that resolves to no state is offered too, as itself, so the control
// tells the truth about a machine that is currently broken rather than
// appearing to have been set to something else.
func targetOptions(data Data, e ChartEdge) []components.Option {
	out := []components.Option{{Value: "", Label: "— none · runs its actions and stays put"}}
	if e.Dangling {
		out = append(out, components.Option{Value: e.Target, Label: e.Target + " · no state of that name"})
	}
	for _, path := range data.StateTargets {
		out = append(out, components.Option{Value: path, Label: path})
	}
	return out
}

// selectedTarget is which option is current: the *resolved* path, because that
// is what the dropdown offers. A target authored as an id — "wander.idle" — is
// the same state as "idle" and must show as the option that is there.
func selectedTarget(e ChartEdge) string {
	if e.Dangling {
		return e.Target
	}
	return e.To
}

// guardOptions is the registered guards plus an explicit none, each carrying
// the description the registry holds — so what a guard does is on screen while
// you are choosing rather than only after you have chosen.
func guardOptions(data Data, t *agent.Transition) []components.Option {
	out := make([]components.Option, 0, len(data.Guards)+2)
	out = append(out, components.Option{Value: "", Label: "— none · always taken"})
	for _, meta := range data.Guards {
		label := meta.Name
		if meta.Description != "" {
			label += " — " + meta.Description
		}
		out = append(out, components.Option{Value: meta.Name, Label: label})
	}
	// A guard the registry does not have is a machine the engine will refuse.
	// Offered as itself so the control says what the file says; dropping it
	// would make the panel show "none" over a transition that has one.
	if name := guardName(t); name != "" && !hasGuardMeta(data.Guards, name) {
		out = append(out, components.Option{
			Value: name, Label: name + " — not a guard this project's engine registers",
		})
	}
	return out
}

func guardName(t *agent.Transition) string {
	if t == nil || t.Cond == nil {
		return ""
	}
	return t.Cond.Type
}

func hasGuardMeta(metas []GuardMeta, name string) bool {
	for _, m := range metas {
		if m.Name == name {
			return true
		}
	}
	return false
}

// guardParams is what the selected guard takes, from the registry — the same
// lookup actionParams does, against the other catalogue.
func guardParams(data Data, name string) []ParamSchema {
	for _, meta := range data.Guards {
		if meta.Name == name {
			return meta.Params
		}
	}
	return nil
}

// guardForm is the guard's parameter form: the same generated form as an
// action's, pointed at the cond's params.
func guardForm(data Data, e ChartEdge, t *agent.Transition) ParamForm {
	var values map[string]any
	if t != nil && t.Cond != nil {
		values = t.Cond.Params
	}
	return ParamForm{
		Scope:  "guard",
		What:   "guard",
		Params: guardParams(data, guardName(t)),
		Values: values,
		Post: func(param string, checkbox bool) string {
			return transitionValueAction(data, e, "guardparam", checkbox, "param", param)
		},
	}
}

// transitionActionList is the transition's own actions — the same list, the
// same catalogue and the same generated form as a state's.
func transitionActionList(data Data, e ChartEdge, t *agent.Transition) ActionList {
	var specs []ActionSpec
	if t != nil {
		specs = t.Actions
	}
	return ActionList{
		Title: "actions",
		Scope: "taction",
		Specs: specs,
		Add:   transitionAddAction(data, e),
		Remove: func(i int) string {
			return transitionAction(data, e, "removeaction", "action", strconv.Itoa(i))
		},
		Form: func(i int, spec ActionSpec) ParamForm {
			return ParamForm{
				Scope:  "taction-" + strconv.Itoa(i),
				What:   "action",
				Params: actionParams(data, spec.Type),
				Values: spec.Params,
				Post: func(param string, checkbox bool) string {
					return transitionValueAction(data, e, "actionparam", checkbox,
						"action", strconv.Itoa(i), "param", param)
				},
			}
		},
		Note: true,
	}
}

// orderLabel is how one row of the order reads.
//
// The chart's own label plus where it goes, because in this list the event is
// the one thing every row shares: two unguarded transitions on one event would
// otherwise render as two identical rows with two identically-named buttons,
// and the order they are in is the whole point of the list.
func orderLabel(e ChartEdge) string {
	switch {
	case e.Internal:
		return e.Label() + " → stays put"
	case e.Dangling:
		return e.Label()
	default:
		return e.Label() + " → " + e.To
	}
}

// transitionSiblings is every transition sharing this one's event, in file
// order — which is the order XState tries them in.
func transitionSiblings(data Data, e ChartEdge) []ChartEdge {
	var out []ChartEdge
	for _, other := range data.Chart.Edges {
		if other.From == e.From && other.Kind == e.Kind && other.Event == e.Event {
			out = append(out, other)
		}
	}
	return out
}

// unreachableFrom is the first transition after an unconditional one, or -1.
//
// XState takes the first whose guard passes, and an unconditional guard always
// passes — so nothing below one can ever fire. A warning and not an error:
// ValidateMachine does not check it, and blocking a save for it would invent a
// rule the engine does not have.
func unreachableFrom(siblings []ChartEdge) int {
	for i, e := range siblings {
		if e.Guard == "" && i+1 < len(siblings) {
			return i + 1
		}
	}
	return -1
}

func isUnreachable(siblings []ChartEdge, i int) bool {
	first := unreachableFrom(siblings)
	return first >= 0 && i >= first
}

// ── where each control posts ──────────────────────────────────────────────────

// transitionAction addresses the selected transition by its four parts and
// names the operation.
//
// Named rather than implied by which parameter is present, because two of these
// write an empty value on purpose: clearing a target makes the transition
// internal and clearing a guard removes the cond, and "absent" and "empty" are
// different answers.
func transitionAction(data Data, e ChartEdge, op string, extra ...string) string {
	return action("/forge/agents/transition", transitionArgs(data, e, op, extra...)...)
}

// transitionArgs is the four fields that address a transition, plus the
// operation and whatever else it takes.
func transitionArgs(data Data, e ChartEdge, op string, extra ...string) []string {
	return append([]string{
		"op", op,
		"machine", data.SelectedMachine,
		"from", e.From,
		"kind", string(e.Kind),
		"event", e.Event,
		"index", strconv.Itoa(e.Index),
	}, extra...)
}

// transitionValueAction is transitionAction carrying the value of the control
// that fired it.
//
// The ordinary case is valueAction's, spelled the same way every other control
// in Forge spells it. Only a checkbox is different, and only because what it
// means is whether it is checked rather than what its value attribute says.
func transitionValueAction(data Data, e ChartEdge, op string, checkbox bool, extra ...string) string {
	args := transitionArgs(data, e, op, extra...)
	if !checkbox {
		return valueAction("/forge/agents/transition", "value", args...)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(action("/forge/agents/transition", args...), "@post('"), "')")
	return "@post('" + inner + "&value=' + evt.target.checked)"
}

func setEventAction(data Data, e ChartEdge) string {
	return transitionValueAction(data, e, "event", false)
}

func setTargetAction(data Data, e ChartEdge) string {
	return transitionValueAction(data, e, "target", false)
}

func setGuardAction(data Data, e ChartEdge) string {
	return transitionValueAction(data, e, "guard", false)
}

func transitionAddAction(data Data, e ChartEdge) string {
	return transitionValueAction(data, e, "addaction", false)
}

// moveTransitionAction moves the sibling at index i, which is not necessarily
// the selected one: reordering is about the list, and being able to move only
// the transition you have selected would mean selecting each in turn to sort
// three of them.
func moveTransitionAction(data Data, e ChartEdge, index, by int) string {
	sibling := e
	sibling.Index = index
	return transitionAction(data, sibling, "move", "by", strconv.Itoa(by))
}

func deleteTransitionAction(data Data, e ChartEdge) string {
	return confirmAction(
		"Delete the transition "+transitionRoute(e)+"? Its guard and actions go with it.",
		transitionAction(data, e, "delete"))
}

// ── refusals, reported where the mistake was made ─────────────────────────────

// fieldProblems is the last refusal, when it was about this field.
//
// Against the field rather than only in the banner: a duration the engine
// cannot read is a mistake made in one box, and the box is where the answer
// belongs. The banner still carries it too — it is on screen everywhere, and
// this panel is not.
func fieldProblems(data Data, field string) []components.Problem {
	if data.ProblemField != field || data.Problem == "" {
		return nil
	}
	return []components.Problem{{Field: field, Message: data.Problem}}
}

func fieldProblemsID(field string) string {
	return components.ProblemsID("transition", field)
}

// edgeFieldProblems is everything wrong with one control of the transition
// panel: the last refusal if it was about this control, and any validation
// error the engine reported about the value this control holds.
//
// The second half is what makes "associated with the field they are about"
// true rather than "positioned nearby". ValidationError.Field carries the value
// that failed — the target that resolves to nothing, the guard that is not
// registered, the duration that will not parse — so an error whose Field is
// what this control currently shows is an error about this control, and the
// control can point at it with aria-describedby.
func edgeFieldProblems(data Data, e ChartEdge, field, value string) []components.Problem {
	out := fieldProblems(data, field)
	if value == "" {
		return out
	}
	for _, p := range data.Problems.ForEdge(e.ID) {
		if p.Field == value {
			out = append(out, p)
		}
	}
	return out
}

// edgeOtherProblems is what no control on the panel holds: an action the
// transition runs, which is a row rather than a field. They render as the
// panel's own list.
func edgeOtherProblems(data Data, e ChartEdge) []components.Problem {
	claimed := map[string]bool{e.Event: true, e.Target: true, e.Guard: true}
	var out []components.Problem
	for _, p := range data.Problems.ForEdge(e.ID) {
		if !claimed[p.Field] {
			out = append(out, p)
		}
	}
	return out
}
