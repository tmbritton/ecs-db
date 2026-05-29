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

// SendEvent runs the SCXML microstep algorithm for one event delivery.
// Returns nil immediately if no eligible transition exists.
// All state mutations (world and machine) happen through the provided interfaces;
// no SQL is written inside this function.
func SendEvent(agent *Agent, event Event, tick int64, registry *Registry, world WorldWriter, reader WorldReader, mw MachineWriter) error {
	fromStates := nodeIDs(agent.Configuration)

	// 1. Select eligible transitions.
	transitions, err := selectEligibleTransitions(agent, event, registry, reader, tick)
	if err != nil {
		return fmt.Errorf("SendEvent: %w", err)
	}
	if len(transitions) == 0 {
		return nil
	}

	// 2. Compute exit set (unordered).
	exitSet := computeExitSet(agent.Configuration, transitions, agent.Definition)

	// 3. Record history before any state exits.
	recordHistoryNodes(agent, exitSet)

	// 4. Exit actions + cancel after events (leaf→root).
	actionsRun := []string{}
	for _, state := range sortByDepthDesc(exitSet) {
		if err := mw.CancelAfterEvents(agent.EntityID, agent.Definition.ID, []string{state.ID}); err != nil {
			return fmt.Errorf("SendEvent: cancel after for %q: %w", state.ID, err)
		}
		ran, err := runActionList(state.Exit, ActionContext{
			EntityID: agent.EntityID, Tick: tick, World: world, Event: event,
		}, registry)
		if err != nil {
			return fmt.Errorf("SendEvent: exit actions for %q: %w", state.ID, err)
		}
		actionsRun = append(actionsRun, ran...)
	}

	// 5. Transition actions + capture cond result.
	var condResult *bool
	for _, sel := range transitions {
		condResult = sel.CondResult
		ran, err := runActionList(sel.Transition.Actions, ActionContext{
			EntityID: agent.EntityID, Tick: tick, World: world, Event: event,
		}, registry)
		if err != nil {
			return fmt.Errorf("SendEvent: transition actions: %w", err)
		}
		actionsRun = append(actionsRun, ran...)
	}

	// 6. Compute entry set.
	entrySet := computeEntrySet(agent.Definition, transitions, agent.History)

	// 7. Entry actions + schedule after events (root→leaf).
	for _, state := range sortByDepthAsc(entrySet) {
		if state.Type == StateTypeHistory {
			continue
		}
		ran, err := runActionList(state.Entry, ActionContext{
			EntityID: agent.EntityID, Tick: tick, World: world, Event: event,
		}, registry)
		if err != nil {
			return fmt.Errorf("SendEvent: entry actions for %q: %w", state.ID, err)
		}
		actionsRun = append(actionsRun, ran...)
		for duration := range state.After {
			targetTick := tick + parseDurationTicks(duration)
			if err := mw.ScheduleAfterEvent(agent.EntityID, agent.Definition.ID, afterEventType(duration, state.ID), targetTick); err != nil {
				return fmt.Errorf("SendEvent: schedule after for %q: %w", state.ID, err)
			}
		}
	}

	// 8. Final-state lifecycle.
	for _, state := range entrySet {
		if state.Type == StateTypeFinal && agent.ActivatedByComponent != "" {
			if err := world.DetachComponent(agent.EntityID, agent.ActivatedByComponent); err != nil {
				return fmt.Errorf("SendEvent: final detach: %w", err)
			}
			break
		}
	}

	// 9. Update configuration.
	agent.Configuration = atomicStates(entrySet)

	// 10. Persist.
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
			if cur.Type == StateTypeParallel {
				break // each parallel region selects independently
			}
			// Check event transitions then after-event transitions.
			var candidates []Transition
			if ts, ok := cur.On[event.Type]; ok {
				candidates = ts
			} else if ts, ok := cur.After[event.Type]; ok {
				candidates = ts
			}
			found := false
			for _, t := range candidates {
				eligible, condResult, err := evaluateTransition(t, agent.EntityID, tick, event, registry, reader)
				if err != nil {
					return nil, err
				}
				if eligible {
					selected = append(selected, selectedTransition{Source: cur, Transition: t, CondResult: condResult})
					for mark := atom; mark != cur.Parent; mark = mark.Parent {
						handled[mark] = true
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

func evaluateTransition(t Transition, entityID, tick int64, event Event, registry *Registry, reader WorldReader) (eligible bool, condResult *bool, err error) {
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
	})
	b := result
	return result, &b, nil
}

// ── Exit set ──────────────────────────────────────────────────────────────────

// computeExitSet computes all states that must be exited for the selected transitions.
// For each transition, active states that are descendants of the LCA(source, target)
// are added to the exit set, along with all their ancestors up to (not including) the LCA.
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
	return isDescendant(s, ancestor)
}

// lcaNode returns the Lowest Common Ancestor of nodes a and b for the purpose
// of computing the exit set. For external transitions (including self-transitions),
// this is the deepest ancestor that is a proper ancestor of both source and target.
// When source == target (self-transition), returns source.Parent.
func lcaNode(a, b *StateNode) *StateNode {
	if a == nil || b == nil {
		return nil
	}
	// Self-transition: exit and re-enter the state itself; LCA is its parent.
	if a == b {
		return a.Parent
	}
	// Build ancestor set for a (proper ancestors only, not a itself).
	aAnc := make(map[*StateNode]bool)
	for cur := a.Parent; cur != nil; cur = cur.Parent {
		aAnc[cur] = true
	}
	// Walk up from b (proper ancestors) to find deepest common ancestor.
	for cur := b.Parent; cur != nil; cur = cur.Parent {
		if aAnc[cur] {
			return cur
		}
	}
	// a and b are siblings at top level (their common ancestor is nil/root).
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
					if isDescendant(active, state) {
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
	if target == "" {
		return nil
	}
	return findState(def.States, target)
}

// findState resolves a target string to a StateNode.
// Targets may be:
//   - a simple name ("b") matched against map keys
//   - a dot-separated path ("c.h") traversed segment by segment
//   - a full node ID ("m.b") matched against node.ID
func findState(states map[string]*StateNode, target string) *StateNode {
	// Try dot-separated path traversal first (e.g., "c.h" → states["c"].Children["h"]).
	if idx := strings.Index(target, "."); idx >= 0 {
		head, tail := target[:idx], target[idx+1:]
		if parent, ok := states[head]; ok {
			if found := findState(parent.Children, tail); found != nil {
				return found
			}
		}
	}
	// Try direct name or ID match at this level.
	for name, node := range states {
		if name == target || node.ID == target {
			return node
		}
	}
	// Recurse into children for full-tree search.
	for _, node := range states {
		if found := findState(node.Children, target); found != nil {
			return found
		}
	}
	return nil
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
