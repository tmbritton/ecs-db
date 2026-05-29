package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// ValidationError carries context for a single validation failure.
type ValidationError struct {
	MachineID string
	StateID   string // empty when the error is machine-level, not state-level
	Field     string // the action type, guard name, target, or context key that failed
	Message   string
}

func (e ValidationError) Error() string {
	if e.StateID != "" {
		return fmt.Sprintf("machine %q: state %q: %s", e.MachineID, e.StateID, e.Message)
	}
	return fmt.Sprintf("machine %q: %s", e.MachineID, e.Message)
}

// ValidateMachine checks a parsed machine definition for semantic correctness:
//   - every action and guard name exists in registry
//   - every transition target and history default target is a known state
//   - every context key matches exactly one component field in s
//
// All errors are collected; the machine is rejected as a whole if any are found.
// invoke detection is handled at parse time by ParseMachine — since StateNode
// has no Invoke field, a successfully parsed definition cannot contain invoke.
func ValidateMachine(def *MachineDefinition, registry *Registry, s schema.DatabaseSchema) []ValidationError {
	var errs []ValidationError

	knownStates := collectStateIDs(def.States)
	fieldIndex := buildFieldIndex(s)

	for key := range def.Context {
		comps := fieldIndex[key]
		switch len(comps) {
		case 0:
			errs = append(errs, ValidationError{
				MachineID: def.ID,
				Field:     key,
				Message:   fmt.Sprintf("context key %q does not match any component field", key),
			})
		case 1:
			// exactly one match — valid
		default:
			sort.Strings(comps)
			errs = append(errs, ValidationError{
				MachineID: def.ID,
				Field:     key,
				Message:   fmt.Sprintf("context key %q is ambiguous: found in %s", key, strings.Join(comps, " and ")),
			})
		}
	}

	for _, node := range def.States {
		errs = append(errs, validateStateNode(def.ID, node, registry, knownStates)...)
	}

	manifest := make(map[string]string, len(def.Context))
	for key := range def.Context {
		if comps := fieldIndex[key]; len(comps) == 1 {
			manifest[key] = comps[0]
		}
	}
	def.ContextManifest = manifest

	return errs
}

// collectStateIDs returns the set of all valid state identifiers in the tree:
// both bare state keys (as they appear in JSON) and full dot-prefixed IDs.
func collectStateIDs(states map[string]*StateNode) map[string]bool {
	known := make(map[string]bool)
	for name, node := range states {
		known[name] = true
		known[node.ID] = true
		for k := range collectStateIDs(node.Children) {
			known[k] = true
		}
	}
	return known
}

// buildFieldIndex maps each component property name to the list of component
// names that declare a property with that name. Used for context key validation.
func buildFieldIndex(s schema.DatabaseSchema) map[string][]string {
	index := make(map[string][]string)
	for compName, comp := range s.Components {
		for fieldName := range comp.Properties {
			index[fieldName] = append(index[fieldName], compName)
		}
	}
	return index
}

func validateStateNode(machineID string, node *StateNode, registry *Registry, knownStates map[string]bool) []ValidationError {
	var errs []ValidationError

	for _, action := range node.Entry {
		if _, ok := registry.GetAction(action.Type); !ok {
			errs = append(errs, ValidationError{
				MachineID: machineID, StateID: node.ID, Field: action.Type,
				Message: fmt.Sprintf("entry action %q is not registered", action.Type),
			})
		}
	}
	for _, action := range node.Exit {
		if _, ok := registry.GetAction(action.Type); !ok {
			errs = append(errs, ValidationError{
				MachineID: machineID, StateID: node.ID, Field: action.Type,
				Message: fmt.Sprintf("exit action %q is not registered", action.Type),
			})
		}
	}
	for _, transitions := range node.On {
		for _, t := range transitions {
			errs = append(errs, validateTransition(machineID, node.ID, t, registry, knownStates)...)
		}
	}
	for _, transitions := range node.After {
		for _, t := range transitions {
			errs = append(errs, validateTransition(machineID, node.ID, t, registry, knownStates)...)
		}
	}
	if node.Type == StateTypeHistory && node.Target != "" {
		if !knownStates[node.Target] {
			errs = append(errs, ValidationError{
				MachineID: machineID, StateID: node.ID, Field: node.Target,
				Message: fmt.Sprintf("history default target %q is not a known state", node.Target),
			})
		}
	}
	for _, child := range node.Children {
		errs = append(errs, validateStateNode(machineID, child, registry, knownStates)...)
	}

	return errs
}

func validateTransition(machineID, stateID string, t Transition, registry *Registry, knownStates map[string]bool) []ValidationError {
	var errs []ValidationError

	if t.Target != "" && !knownStates[t.Target] {
		errs = append(errs, ValidationError{
			MachineID: machineID, StateID: stateID, Field: t.Target,
			Message: fmt.Sprintf("transition target %q is not a known state", t.Target),
		})
	}
	if t.Cond != nil {
		if _, ok := registry.GetGuard(t.Cond.Type); !ok {
			errs = append(errs, ValidationError{
				MachineID: machineID, StateID: stateID, Field: t.Cond.Type,
				Message: fmt.Sprintf("guard %q is not registered", t.Cond.Type),
			})
		}
	}
	for _, action := range t.Actions {
		if _, ok := registry.GetAction(action.Type); !ok {
			errs = append(errs, ValidationError{
				MachineID: machineID, StateID: stateID, Field: action.Type,
				Message: fmt.Sprintf("transition action %q is not registered", action.Type),
			})
		}
	}

	return errs
}
