package agent

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// selectedTransition pairs a source StateNode with the chosen transition.
type selectedTransition struct {
	Source     *StateNode
	Transition Transition
	CondResult *bool // nil = unconditional
}

func SendEvent(agent *Agent, event Event, tick int64, registry *Registry, world WorldWriter, reader WorldReader, mw MachineWriter) error {
	fromStates := nodeIDs(agent.Configuration)

	transitions, err := selectEligibleTransitions(agent, event, registry, reader, tick)
	if err != nil {
		return fmt.Errorf("SendEvent: %w", err)
	}
	if len(transitions) == 0 {
		return nil
	}

	exitSet := computeExitSet(agent.Configuration, transitions, agent.Definition)

	// Record history before exits fire so the snapshot reflects pre-exit state.
	recordHistoryNodes(agent, exitSet)

	// Exit leaf→root: cancel after-timers before running exit actions.
	actionsRun := []string{}
	for _, state := range sortByDepthDesc(exitSet) {
		if err := mw.CancelAfterEvents(agent.EntityID, agent.Definition.ID, []string{state.ID}); err != nil {
			return fmt.Errorf("SendEvent: cancel after for %q: %w", state.ID, err)
		}
		ran, err := runActionList(state.Exit, ActionContext{
			EntityID: agent.EntityID, Tick: tick, World: world, Reader: reader,
			Event: event, ContextManifest: agent.Definition.ContextManifest,
		}, registry)
		if err != nil {
			return fmt.Errorf("SendEvent: exit actions for %q: %w", state.ID, err)
		}
		actionsRun = append(actionsRun, ran...)
	}

	// Transition actions run between exit and entry. For parallel machines with
	// multiple transitions, condResult records the first guarded result encountered.
	var condResult *bool
	for _, sel := range transitions {
		if condResult == nil && sel.CondResult != nil {
			condResult = sel.CondResult
		}
		ran, err := runActionList(sel.Transition.Actions, ActionContext{
			EntityID: agent.EntityID, Tick: tick, World: world, Reader: reader,
			Event: event, ContextManifest: agent.Definition.ContextManifest,
		}, registry)
		if err != nil {
			return fmt.Errorf("SendEvent: transition actions: %w", err)
		}
		actionsRun = append(actionsRun, ran...)
	}

	entrySet := computeEntrySet(agent.Definition, transitions, agent.History)

	// Entry root→leaf: run entry actions then schedule after-timers.
	for _, state := range sortByDepthAsc(entrySet) {
		if state.Type == StateTypeHistory {
			continue
		}
		ran, err := runActionList(state.Entry, ActionContext{
			EntityID: agent.EntityID, Tick: tick, World: world, Reader: reader,
			Event: event, ContextManifest: agent.Definition.ContextManifest,
		}, registry)
		if err != nil {
			return fmt.Errorf("SendEvent: entry actions for %q: %w", state.ID, err)
		}
		actionsRun = append(actionsRun, ran...)
		for duration := range state.After {
			targetTick := tick + parseDurationTicks(duration, agent.TickDurationMs)
			if err := mw.ScheduleAfterEvent(agent.EntityID, agent.Definition.ID, afterEventType(duration, state.ID), targetTick); err != nil {
				return fmt.Errorf("SendEvent: schedule after for %q: %w", state.ID, err)
			}
		}
	}

	for _, state := range entrySet {
		if state.Type == StateTypeFinal && agent.ActivatedByComponent != "" {
			if err := world.DetachComponent(agent.EntityID, agent.ActivatedByComponent); err != nil {
				return fmt.Errorf("SendEvent: final detach: %w", err)
			}
			break
		}
	}

	// Only update Configuration for targeted transitions.
	// Targetless transitions run actions without changing active states.
	if len(entrySet) > 0 {
		agent.Configuration = atomicStates(entrySet)
	}

	toStates := nodeIDs(agent.Configuration)
	if err := mw.SetMachineState(agent.EntityID, agent.Definition.ID, toStates, tick); err != nil {
		return fmt.Errorf("SendEvent: SetMachineState: %w", err)
	}
	return mw.AppendTransition(TransitionRecord{
		Tick:       tick,
		WallMs:     time.Now().UnixMilli(),
		EntityID:   agent.EntityID,
		MachineID:  agent.Definition.ID,
		FromStates: fromStates,
		ToStates:   toStates,
		Event:      event.Type,
		CondResult: condResult,
		ActionsRun: actionsRun,
	})
}

// ── Transition selection ──────────────────────────────────────────────────────

func selectEligibleTransitions(agent *Agent, event Event, registry *Registry, reader WorldReader, tick int64) ([]selectedTransition, error) {
	var selected []selectedTransition
	handled := make(map[*StateNode]bool)

	for _, atom := range sortByDepthDesc(agent.Configuration) {
		if handled[atom] {
			continue
		}
		for cur := atom; cur != nil; cur = cur.Parent {
			var candidates []Transition
			if ts, ok := cur.On[event.Type]; ok {
				candidates = ts
			} else {
				candidates = afterCandidates(cur, event.Type)
			}
			found := false
			for _, t := range candidates {
				eligible, condResult, err := evaluateTransition(t, agent.EntityID, tick, event, registry, reader, agent.Definition.ContextManifest)
				if err != nil {
					return nil, err
				}
				if eligible {
					selected = append(selected, selectedTransition{Source: cur, Transition: t, CondResult: condResult})
					// Mark all active atoms that are descendants of cur as handled.
					// This prevents sibling parallel-region atoms from firing the
					// same transition a second time when cur is a parallel ancestor.
					for _, otherAtom := range agent.Configuration {
						if isDescendantOrSelf(otherAtom, cur) {
							handled[otherAtom] = true
						}
					}
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	return selected, nil
}

func evaluateTransition(t Transition, entityID, tick int64, event Event, registry *Registry, reader WorldReader, contextManifest map[string]string) (eligible bool, condResult *bool, err error) {
	if t.Cond == nil {
		return true, nil, nil
	}
	handler, ok := registry.GetGuard(t.Cond.Type)
	if !ok {
		return false, nil, nil
	}
	result := handler.Evaluate(GuardContext{
		EntityID: entityID, Tick: tick, World: reader,
		Params: t.Cond.Params, Event: event,
		ContextManifest: contextManifest,
	})
	b := result
	return result, &b, nil
}

// ── Exit set ──────────────────────────────────────────────────────────────────

func computeExitSet(config []*StateNode, transitions []selectedTransition, def *MachineDefinition) []*StateNode {
	exit := make(map[*StateNode]bool)
	for _, sel := range transitions {
		if sel.Transition.Target == "" {
			// Targetless/internal transition: no states exit.
			continue
		}
		targetNode := resolveTarget(sel.Transition.Target, def)
		lca := lcaNode(sel.Source, targetNode)
		for _, active := range config {
			if isDescendantOrRoot(active, lca) {
				// Exit active and all ancestors up to (but not including) lca.
				for cur := active; cur != lca; cur = cur.Parent {
					exit[cur] = true
				}
			}
		}
	}
	result := make([]*StateNode, 0, len(exit))
	for n := range exit {
		result = append(result, n)
	}
	return result
}

// isDescendantOrRoot reports whether s is a descendant of ancestor,
// or if ancestor is nil (representing the machine root, which is an ancestor of all nodes).
func isDescendantOrRoot(s, ancestor *StateNode) bool {
	if ancestor == nil {
		return true // nil = machine root; every node is a descendant
	}
	return isDescendantOrSelf(s, ancestor)
}

// lcaNode returns the Lowest Common Ancestor of a and b.
// Self-transitions (a == b) return a.Parent so the state exits and re-enters.
func lcaNode(a, b *StateNode) *StateNode {
	if a == nil || b == nil {
		return nil
	}
	if a == b {
		return a.Parent
	}
	aAnc := make(map[*StateNode]bool)
	for cur := a.Parent; cur != nil; cur = cur.Parent {
		aAnc[cur] = true
	}
	for cur := b.Parent; cur != nil; cur = cur.Parent {
		if aAnc[cur] {
			return cur
		}
	}
	return nil
}

// ── History recording ─────────────────────────────────────────────────────────

func recordHistoryNodes(agent *Agent, exitSet []*StateNode) {
	for _, state := range exitSet {
		if state.Type != StateTypeCompound && state.Type != StateTypeParallel {
			continue
		}
		for _, child := range state.Children {
			if child.Type != StateTypeHistory {
				continue
			}
			var snapshot []*StateNode
			for _, active := range agent.Configuration {
				if child.History == "shallow" || child.History == "" {
					if active.Parent == state {
						snapshot = append(snapshot, active)
					}
				} else {
					if isDescendantOrSelf(active, state) {
						snapshot = append(snapshot, active)
					}
				}
			}
			agent.History[child.ID] = snapshot
		}
	}
}

// ── Entry set ─────────────────────────────────────────────────────────────────

func computeEntrySet(def *MachineDefinition, transitions []selectedTransition, history map[string][]*StateNode) []*StateNode {
	seen := make(map[*StateNode]bool)
	var result []*StateNode

	for _, sel := range transitions {
		target := resolveTarget(sel.Transition.Target, def)
		if target == nil {
			continue // internal transition (no target)
		}
		for _, n := range expandEntryWithHistory(target, history, def) {
			if !seen[n] {
				seen[n] = true
				result = append(result, n)
			}
		}
	}
	return result
}

func resolveTarget(target string, def *MachineDefinition) *StateNode {
	node, _ := FindState(def, target)
	return node
}

// FindState resolves a transition target to a state and to that state's dotted
// path from the machine root — "combat.attacking" — or to (nil, "") if nothing
// matches. Dot-separated paths are traversed segment-by-segment before falling
// back to a full-tree name/ID search, matching XState v4 target notation.
//
// The path is returned because StateNode.ID cannot identify a node: parsing
// passes the machine id down unchanged at every depth, so a state named
// "alert" is "goblin.alert" whether it sits at the root or three levels in, and
// two compound states with a same-named child collide on one ID. Forge's canvas
// keys a node on the path for that reason, and takes it from here rather than
// resolving targets itself — a chart that disagreed with the interpreter about
// where an edge goes would be drawing a different machine from the one running.
func FindState(def *MachineDefinition, target string) (*StateNode, string) {
	return NewStateResolver(def).Resolve(target)
}

// StateResolver answers "where does this target go" for one machine.
//
// It exists for cost, not for behaviour: Resolve is FindState and gives the
// same answers. FindState walks the tree, a machine's transitions name the same
// handful of states over and over, and every target that resolves to nothing —
// which is every target being typed, half-typed or just renamed — walks the
// whole tree before giving up. Forge revalidates and redraws on every render
// and every tick of a two-second stream, which is what turned an unremarkable
// cost into a measurable one. The resolver remembers two things across those
// searches: what each distinct target resolved to, and the authored order of
// each level, which is otherwise rebuilt at every level of every search.
//
// It holds the definition it was built for. Anything that edits the machine
// must build a new one; there is no invalidation, deliberately, because a
// resolver that quietly answered for a previous version of the machine is
// exactly the disagreement this type exists to prevent.
type StateResolver struct {
	def    *MachineDefinition
	cache  map[string]resolved
	orders map[*StateNode][]string // keyed by the parent; nil is the machine root
}

type resolved struct {
	node *StateNode
	path string
}

func NewStateResolver(def *MachineDefinition) *StateResolver {
	return &StateResolver{
		def:    def,
		cache:  map[string]resolved{},
		orders: map[*StateNode][]string{},
	}
}

// Resolve is FindState, remembered.
func (r *StateResolver) Resolve(target string) (*StateNode, string) {
	if r.def == nil || target == "" {
		return nil, ""
	}
	if hit, ok := r.cache[target]; ok {
		return hit.node, hit.path
	}
	node, path := r.find(r.def.States, r.def.StateOrder, nil, target, "")
	r.cache[target] = resolved{node, path}
	return node, path
}

// namesFor is the authored order of one level, worked out once.
func (r *StateResolver) namesFor(parent *StateNode, states map[string]*StateNode, order []string) []string {
	if got, ok := r.orders[parent]; ok {
		return got
	}
	names := orderedNames(states, order)
	r.orders[parent] = names
	return names
}

// find searches one level and then descends, in authored order.
//
// Authored order and not map order, which is what this did until the canvas
// needed two renders of a machine to agree. A target naming a state that exists
// in two subtrees — "alert" under both "patrol" and "combat" — resolved to
// whichever one Go's randomised map iteration reached first, so the same file
// made the running game behave differently between two launches. Ambiguity in
// the file is the author's to fix; picking a different answer each time is not
// a way to tell them about it.
func (r *StateResolver) find(states map[string]*StateNode, order []string, parent *StateNode, target, prefix string) (*StateNode, string) {
	// The dotted branch first, and before the level's order is worked out: a
	// machine whose targets are all dotted paths never reaches the two loops
	// below, and ordering a level the search walks straight past was pure cost.
	if idx := strings.Index(target, "."); idx >= 0 {
		head, tail := target[:idx], target[idx+1:]
		if child, ok := states[head]; ok {
			if found, path := r.find(child.Children, child.StateOrder, child, tail, prefix+head+"."); found != nil {
				return found, path
			}
		}
	}
	names := r.namesFor(parent, states, order)
	for _, name := range names {
		if node := states[name]; name == target || node.ID == target {
			return node, prefix + name
		}
	}
	for _, name := range names {
		node := states[name]
		if len(node.Children) == 0 {
			// Nothing to descend into, and skipping it before building the
			// prefix matters: on a flat machine every miss was allocating one
			// string per state, per target, per search.
			continue
		}
		if found, path := r.find(node.Children, node.StateOrder, node, target, prefix+name+"."); found != nil {
			return found, path
		}
	}
	return nil, ""
}

// orderedNames is the authored order of a state map, with anything the order
// does not mention appended alphabetically.
//
// The remainder is not hypothetical: a machine built in code — a state added on
// the canvas — records no order at all, and a state added to a file by hand
// after Forge read it would be missing from one taken earlier.
func orderedNames(states map[string]*StateNode, order []string) []string {
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

func expandEntryWithHistory(node *StateNode, history map[string][]*StateNode, def *MachineDefinition) []*StateNode {
	if node.Type == StateTypeHistory {
		if recorded, ok := history[node.ID]; ok && len(recorded) > 0 {
			var result []*StateNode
			for _, s := range recorded {
				result = append(result, expandEntryWithHistory(s, history, def)...)
			}
			return result
		}
		if node.Target != "" {
			if t := resolveTarget(node.Target, def); t != nil {
				return expandEntryWithHistory(t, history, def)
			}
		}
		return nil
	}
	result := []*StateNode{node}
	switch node.Type {
	case StateTypeCompound:
		if node.Initial != "" {
			if child := node.Children[node.Initial]; child != nil {
				result = append(result, expandEntryWithHistory(child, history, def)...)
			}
		}
	case StateTypeParallel:
		for _, child := range node.Children {
			result = append(result, expandEntryWithHistory(child, history, def)...)
		}
	}
	return result
}

// ── Sort helpers ──────────────────────────────────────────────────────────────

func sortByDepthDesc(nodes []*StateNode) []*StateNode {
	out := make([]*StateNode, len(nodes))
	copy(out, nodes)
	sort.Slice(out, func(i, j int) bool { return nodeDepth(out[i]) > nodeDepth(out[j]) })
	return out
}

func sortByDepthAsc(nodes []*StateNode) []*StateNode {
	out := make([]*StateNode, len(nodes))
	copy(out, nodes)
	sort.Slice(out, func(i, j int) bool { return nodeDepth(out[i]) < nodeDepth(out[j]) })
	return out
}
