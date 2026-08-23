package machines

// A transition's own edits: its event, its target, its guard and the guard's
// parameters, the actions it runs on the way through, and its position among
// the transitions that share its event.
//
// The last of those is not presentation. XState takes the first transition on
// an event whose guard passes — internal/agent/interpreter.go iterates the list
// in slice order and stops at the first eligible one — so the order of that
// list is the machine's logic, and a control that reordered it silently would
// be changing what the machine does.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/jsonorder"
)

// TransitionRef addresses one transition: the state that holds it, which of the
// two maps it is in, the key it is under, and which of the transitions on that
// key it is.
//
// A struct and not the chart's edge id, which joins the same four fields with
// bars — and both a state name and an event name may contain a bar, so
// splitting that id back apart is guesswork. The id is for the URL; this is for
// the edits.
type TransitionRef struct {
	From  string // the source state's dotted path
	Kind  string // "on" or "after"
	Event string // the event name, or the raw duration string for an after
	Index int    // which of the transitions on that key
}

// GuardCatalogue is every guard the engine would register for this project.
//
// Built the same way the engine builds it and gated the same way, which is why
// a mapless project does not get inLineOfSight: its closure captures a tile
// grid, and a machine using it would not load. Sorted by name, by
// Registry.Guards.
func (s *Session) GuardCatalogue() []agent.GuardMeta {
	return s.registry().Guards()
}

// StateTargets is every state in the machine, as a dotted path, in authored
// order and depth first.
//
// What the target dropdown is made of. Offering exactly these makes
// ValidateMachine's "transition target is not a known state" unreachable rather
// than merely reported — the same argument the action catalogue makes.
// A function over a definition and not a Session method, deliberately. Every
// Session.Read clones by emitting and re-parsing the whole machine, so asking
// the session for this on a page that already holds the definition would
// serialise and re-parse it a second time every stream tick — and build the
// dropdown from a different snapshot than the chart beside it.
func StateTargets(def *agent.MachineDefinition) []string {
	if def == nil {
		return nil
	}
	var out []string
	walkOrdered(def.States, def.StateOrder, "", func(p string, _ *agent.StateNode) {
		out = append(out, p)
	})
	return out
}

// walkOrdered visits every state in the order the file declares them, which is
// walkStates' order only by accident — that one sorts, because the warnings it
// builds read better alphabetically. A dropdown is a picture of the file and
// wants the file's order: states are authored in the order they run.
func walkOrdered(states map[string]*agent.StateNode, order []string, prefix string, fn func(string, *agent.StateNode)) {
	for _, name := range jsonorder.Apply(order, states) {
		node := states[name]
		fn(prefix+name, node)
		walkOrdered(node.Children, node.StateOrder, prefix+name+".", fn)
	}
}

// EventNames is every event this machine already reacts to, sorted.
//
// Suggestions and nothing more. An event name is authored rather than
// registered — any string is a legal event — so the control is a text field and
// this only fills its datalist. The trap it softens is real: a typo produces a
// transition that never fires, and nothing validates it because nothing can.
// Over a definition rather than through the session, for the reason
// StateTargets gives.
func EventNames(def *agent.MachineDefinition) []string {
	if def == nil {
		return nil
	}
	seen := map[string]bool{}
	walkStates(def.States, "", func(_ string, node *agent.StateNode) {
		for event := range node.On {
			seen[event] = true
		}
	})
	out := make([]string, 0, len(seen))
	for event := range seen {
		out = append(out, event)
	}
	sort.Strings(out)
	return out
}

// SetTransitionEvent moves a transition to another event key, and answers where
// it went.
//
// Answers where it went because the caller cannot work it out: moving onto an
// event that already exists appends, and only this knows how long that list
// was. The chart's edge id is positional, so a selection that did not follow
// would name a different transition — and after a move, the control that made
// it would no longer repeat.
//
// For an after transition the key is a duration, and it is validated by the
// engine's own parser rather than by a shape written here.
func (s *Session) SetTransitionEvent(path string, r TransitionRef, to string) (TransitionRef, error) {
	to = strings.TrimSpace(to)
	moved := r
	err := s.Edit(path, func(def *agent.MachineDefinition) error {
		_, set, order, err := transitionSet(def, r)
		if err != nil {
			return err
		}
		list := set[r.Event]
		if r.Index < 0 || r.Index >= len(list) {
			return outOfRange(r, len(list))
		}
		if to == "" {
			return fmt.Errorf("a transition needs something to fire on")
		}
		if r.Kind == "after" {
			if _, err := agent.ParseDurationMs(to); err != nil {
				return fmt.Errorf(
					"%q is not a duration the engine reads — write 500 for milliseconds, "+
						"or 1s, or 1.5s", to)
			}
		}
		if to == r.Event {
			return nil
		}
		t := list[r.Index]
		rest := append(list[:r.Index:r.Index], list[r.Index+1:]...)
		if len(rest) == 0 && len(set[to]) == 0 {
			// The only transition under this key, moving to a key nothing
			// holds: rename in place, so the file's key order is untouched. A
			// remove-and-append would move the event to the end of the object
			// and produce a diff nobody asked for.
			delete(set, r.Event)
			set[to] = []agent.Transition{t}
			renameInOrder(order, r.Event, to)
			moved = TransitionRef{From: r.From, Kind: r.Kind, Event: to, Index: 0}
			return nil
		}
		if len(rest) == 0 {
			delete(set, r.Event)
			removeFromOrder(order, r.Event)
		} else {
			set[r.Event] = rest
		}
		// Appended, which is the only sane default: last means lowest priority,
		// and priority is the order.
		moved = TransitionRef{From: r.From, Kind: r.Kind, Event: to, Index: len(set[to])}
		set[to] = append(set[to], t)
		// The order is deliberately *not* touched for the arriving key. The
		// emitter runs jsonorder.Apply over the same pair, and a key it was
		// never told about sorts after the ones it was — which for one new key
		// is the end, where an added key belongs. Recording it here as well was
		// a second copy of that rule that no test could tell from its absence.
		return nil
	})
	if err != nil {
		return r, err
	}
	return moved, nil
}

// SetTransitionTarget points a transition at a state, or at nothing.
//
// At nothing is not an error state: a transition with actions and no target
// runs them and changes no state, which is a legitimate thing to write. A
// dropdown that could not express it would silently retarget every internal
// transition the first time anyone opened the panel.
//
// The path is written as the dotted path the dropdown offered, which the engine
// resolves unambiguously — not the bare name, which may belong to a state in
// another branch.
func (s *Session) SetTransitionTarget(path string, r TransitionRef, target string) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		t, err := transitionAt(def, r)
		if err != nil {
			return err
		}
		if target != "" {
			if _, err := stateAt(def, target); err != nil {
				return fmt.Errorf("the transition's target: %w", err)
			}
		} else {
			// Bare is "this was authored as a plain target string", and with no
			// target left there is no string to be. Leaving it set emits
			// "GO": "" — a transition aimed at the state called empty-string,
			// which parses back as internal and so breaks nothing, but says
			// something nobody wrote.
			t.Bare = false
		}
		t.Target = target
		return nil
	})
}

// SetTransitionGuard sets, changes or clears the cond. An empty name removes it
// entirely rather than writing an empty one.
func (s *Session) SetTransitionGuard(path string, r TransitionRef, guard string) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		t, err := transitionAt(def, r)
		if err != nil {
			return err
		}
		if guard == "" {
			t.Cond = nil
			return nil
		}
		if _, ok := guardMeta(s.registry(), guard); !ok {
			return fmt.Errorf(
				"%q is not a guard this project's engine registers, so a machine using it "+
					"would not load", guard)
		}
		if t.Cond != nil && t.Cond.Type == guard {
			return nil
		}
		// Bare, so a guard with nothing on it is written as the string it would
		// have been authored as rather than expanded into an object. Any
		// parameters the previous guard carried go with it: they were declared
		// by a different schema, and inRange's distance means nothing to
		// healthAbove.
		t.Cond = &agent.CondSpec{Type: guard, Bare: true}
		return nil
	})
}

// SetGuardParam writes one parameter of the transition's guard, converted by
// the type the registry declares for it — the same rule, and the same code, as
// an action's parameters.
func (s *Session) SetGuardParam(path string, r TransitionRef, param, value string) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		t, err := transitionAt(def, r)
		if err != nil {
			return err
		}
		if t.Cond == nil {
			return fmt.Errorf("this transition has no guard to give a %q to", param)
		}
		schema, ok := guardParamSchema(s.registry(), t.Cond.Type, param)
		if !ok {
			return fmt.Errorf("%q does not take a parameter called %q", t.Cond.Type, param)
		}
		return setParam(&t.Cond.Params, &t.Cond.Bare, schema, value)
	})
}

// AddTransitionAction, RemoveTransitionAction and SetTransitionActionParam are
// a state's three action edits pointed at a transition's list instead — the
// same three helpers, so the two lists cannot come to behave differently.
func (s *Session) AddTransitionAction(path string, r TransitionRef, name string) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		t, err := transitionAt(def, r)
		if err != nil {
			return err
		}
		return addActionTo(&t.Actions, s.registry(), name)
	})
}

func (s *Session) RemoveTransitionAction(path string, r TransitionRef, index int) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		t, err := transitionAt(def, r)
		if err != nil {
			return err
		}
		return removeActionFrom(&t.Actions, "actions on this transition", index)
	})
}

func (s *Session) SetTransitionActionParam(path string, r TransitionRef, index int, param, value string) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		t, err := transitionAt(def, r)
		if err != nil {
			return err
		}
		return setActionParamOn(&t.Actions, s.registry(), "actions on this transition", index, param, value)
	})
}

// MoveTransition swaps a transition with the one delta places away, and answers
// where it went — the selection has to follow it for the same reason
// SetTransitionEvent answers.
//
// A delta rather than a destination, because the control that sends it is two
// buttons and always sends ±1. Nothing here depends on that, and a drag would
// send a longer one. Moving off either end is refused rather than clamped, so a button
// that cannot do anything is one that reports rather than one that silently
// does nothing.
func (s *Session) MoveTransition(path string, r TransitionRef, delta int) (TransitionRef, error) {
	moved := r
	err := s.Edit(path, func(def *agent.MachineDefinition) error {
		_, set, _, err := transitionSet(def, r)
		if err != nil {
			return err
		}
		list := set[r.Event]
		if r.Index < 0 || r.Index >= len(list) {
			return outOfRange(r, len(list))
		}
		to := r.Index + delta
		if to < 0 || to >= len(list) {
			return fmt.Errorf(
				"there are %d transitions on %q, so this one cannot move there", len(list), r.Event)
		}
		list[r.Index], list[to] = list[to], list[r.Index]
		moved = TransitionRef{From: r.From, Kind: r.Kind, Event: r.Event, Index: to}
		return nil
	})
	if err != nil {
		return r, err
	}
	return moved, nil
}

// TransitionAt is the transition a ref names, for the inspector — which is
// reading the same working value the chart drew, so the panel and the canvas
// cannot be describing different transitions.
func TransitionAt(def *agent.MachineDefinition, r TransitionRef) (*agent.Transition, error) {
	return transitionAt(def, r)
}

func transitionAt(def *agent.MachineDefinition, r TransitionRef) (*agent.Transition, error) {
	_, set, _, err := transitionSet(def, r)
	if err != nil {
		return nil, err
	}
	list := set[r.Event]
	if r.Index < 0 || r.Index >= len(list) {
		return nil, outOfRange(r, len(list))
	}
	return &list[r.Index], nil
}

// transitionSet resolves the state and picks the map the ref names, refusing a
// key that is not there.
//
// The state path is resolved exactly by stateAt, never searched for: the panel
// is addressing the transition the canvas named, and a fuzzy match would let an
// edit land on a transition nobody pointed at.
func transitionSet(def *agent.MachineDefinition, r TransitionRef) (
	*agent.StateNode, map[string][]agent.Transition, *[]string, error,
) {
	node, err := stateAt(def, r.From)
	if err != nil {
		return nil, nil, nil, err
	}
	var (
		set   map[string][]agent.Transition
		order *[]string
	)
	switch r.Kind {
	case "on":
		set, order = node.On, &node.OnOrder
	case "after":
		set, order = node.After, &node.AfterOrder
	default:
		return nil, nil, nil, fmt.Errorf("%q is not a kind of transition", r.Kind)
	}
	// No check that the key exists: every caller reads set[r.Event] and then
	// checks the index against its length, and a key that is not there gives a
	// nil list and the same refusal. A second guard over the first one's answer
	// reads as a safety net and is not one.
	return node, set, order, nil
}

func outOfRange(r TransitionRef, n int) error {
	return fmt.Errorf("state %q has %d transitions on %q, not %d", r.From, n, r.Event, r.Index+1)
}

func guardMeta(reg *agent.Registry, name string) (agent.GuardMeta, bool) {
	for _, meta := range reg.Guards() {
		if meta.Name == name {
			return meta, true
		}
	}
	return agent.GuardMeta{}, false
}

func guardParamSchema(reg *agent.Registry, guard, param string) (agent.ParamSchema, bool) {
	meta, ok := guardMeta(reg, guard)
	if !ok {
		return agent.ParamSchema{}, false
	}
	return schemaNamed(meta.Params, param)
}
