package agent

import (
	"fmt"
)

// Agent is a running instance of a MachineDefinition bound to a specific entity.
// Configuration holds the currently active atomic (leaf) states only — never
// ancestor compound or parallel nodes.
type Agent struct {
	Definition           *MachineDefinition
	Configuration        []*StateNode // active atomic (leaf) states
	EntityID             int64
	History              map[string][]*StateNode // history node ID → recorded atomic snapshot
	ActivatedByComponent string                  // non-empty if activated via AttachComponent
	TickDurationMs       int64
}

// NewAgent returns an Agent with no active configuration.
// Call StartAgent once before delivering events via SendEvent.
// tickDurationMs sets the tick duration for after-event scheduling; if <= 0 it defaults to 50.
func NewAgent(def *MachineDefinition, entityID int64, activatedByComponent string, tickDurationMs int64) *Agent {
	if tickDurationMs <= 0 {
		tickDurationMs = 50
	}
	return &Agent{
		Definition:           def,
		EntityID:             entityID,
		History:              make(map[string][]*StateNode),
		ActivatedByComponent: activatedByComponent,
		TickDurationMs:       tickDurationMs,
	}
}

// StartAgent performs machine startup:
//  1. Seeds each context-declared component missing from the entity.
//  2. Enters the initial state tree (root→leaf), running entry actions.
//  3. Schedules any after-transitions for entered states.
//  4. Persists the initial configuration to behavior_components.
func StartAgent(agent *Agent, registry *Registry, tick int64, world WorldWriter, reader WorldReader, mw MachineWriter) error {
	def := agent.Definition

	// Group context fields by component, then attach missing ones.
	compValues := make(map[string]map[string]any)
	for field, initVal := range def.Context {
		compName := def.ContextManifest[field]
		if compName == "" {
			continue
		}
		if compValues[compName] == nil {
			compValues[compName] = make(map[string]any)
		}
		compValues[compName][field] = initVal
	}
	for compName, values := range compValues {
		has, err := reader.HasComponent(agent.EntityID, compName)
		if err != nil {
			return fmt.Errorf("StartAgent: checking %q: %w", compName, err)
		}
		if !has {
			if err := world.AttachComponent(agent.EntityID, compName, values); err != nil {
				return fmt.Errorf("StartAgent: attaching %q: %w", compName, err)
			}
		}
	}

	// Enter initial state tree.
	entered := expandEntry(def.States[def.Initial])
	initEvent := Event{Type: "xstate.init"}
	for _, state := range entered {
		if state.Type == StateTypeHistory {
			continue
		}
		if _, err := runActionList(state.Entry, ActionContext{
			EntityID: agent.EntityID, Tick: tick, World: world, Reader: reader,
			Event: initEvent, ContextManifest: def.ContextManifest,
		}, registry); err != nil {
			return fmt.Errorf("StartAgent: entry actions for %q: %w", state.ID, err)
		}
		for duration := range state.After {
			targetTick := tick + parseDurationTicks(duration, agent.TickDurationMs)
			evType := afterEventType(duration, state.ID)
			if err := mw.ScheduleAfterEvent(agent.EntityID, def.ID, evType, targetTick); err != nil {
				return fmt.Errorf("StartAgent: scheduling after for %q: %w", state.ID, err)
			}
		}
	}

	agent.Configuration = atomicStates(entered)
	return mw.SetMachineState(agent.EntityID, def.ID, nodeIDs(agent.Configuration), tick)
}

// ── Helpers shared by agent.go and interpreter.go ────────────────────────────

// expandEntry returns states to enter (root→leaf) when targeting node.
// Compound: enters node then recurses into its initial child.
// Parallel: enters node then all children.
func expandEntry(node *StateNode) []*StateNode {
	if node == nil {
		return nil
	}
	result := []*StateNode{node}
	switch node.Type {
	case StateTypeCompound:
		if node.Initial != "" {
			result = append(result, expandEntry(node.Children[node.Initial])...)
		}
	case StateTypeParallel:
		for _, child := range node.Children {
			result = append(result, expandEntry(child)...)
		}
	}
	return result
}

// atomicStates filters to atomic and final nodes only.
func atomicStates(nodes []*StateNode) []*StateNode {
	var out []*StateNode
	for _, n := range nodes {
		if n.Type == StateTypeAtomic || n.Type == StateTypeFinal {
			out = append(out, n)
		}
	}
	return out
}

// nodeIDs returns the ID of each node.
func nodeIDs(nodes []*StateNode) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

// nodeDepth counts how many ancestors a node has (root = 0).
func nodeDepth(n *StateNode) int {
	depth := 0
	for p := n.Parent; p != nil; p = p.Parent {
		depth++
	}
	return depth
}

// isDescendant reports whether s is equal to ancestor or a descendant of it.
func isDescendant(s, ancestor *StateNode) bool {
	for cur := s; cur != nil; cur = cur.Parent {
		if cur == ancestor {
			return true
		}
	}
	return false
}

// runActionList dispatches each action through the registry.
// Returns names of actions that ran (for transitions.actions_run).
func runActionList(actions []ActionSpec, ctx ActionContext, registry *Registry) ([]string, error) {
	var ran []string
	for _, spec := range actions {
		handler, ok := registry.GetAction(spec.Type)
		if !ok {
			continue
		}
		ctx.Params = spec.Params
		if err := handler.Run(ctx); err != nil {
			return ran, fmt.Errorf("action %q: %w", spec.Type, err)
		}
		ran = append(ran, spec.Type)
	}
	return ran, nil
}
