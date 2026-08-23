package agent

import (
	"errors"
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
//   - every initial state names a child of the state that declares it
//   - every context key matches exactly one component field in s
//
// All errors are collected; the machine is rejected as a whole if any are found.
// invoke detection is handled at parse time by ParseMachine — since StateNode
// has no Invoke field, a successfully parsed definition cannot contain invoke.
func ValidateMachine(def *MachineDefinition, registry *Registry, s schema.DatabaseSchema) []ValidationError {
	var errs []ValidationError

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

	if def.Initial == "" && len(def.States) > 0 {
		errs = append(errs, ValidationError{
			MachineID: def.ID,
			Message:   "machine has child states but no initial state",
		})
	}
	errs = append(errs, validateInitial(def.ID, "", def.Initial, def.States)...)

	resolver := NewStateResolver(def)
	for _, node := range def.States {
		errs = append(errs, validateStateNode(def, node, registry, resolver)...)
	}

	if len(errs) == 0 {
		manifest := make(map[string]string, len(def.Context))
		for key := range def.Context {
			if comps := fieldIndex[key]; len(comps) == 1 {
				manifest[key] = comps[0]
			}
		}
		def.ContextManifest = manifest
	}

	return errs
}

// ValidateMachineError is ValidateMachine as a single error, joining every
// failure rather than collapsing to the first.
//
// It exists because that is the shape an editor needs. ValidateMachine returns
// a slice precisely so all of what is wrong can be shown at once — a
// first-failure-only report sends someone round the edit-save loop once per
// mistake — but a validation hook takes one error. errors.Join preserves the
// list, so a caller can pull it back apart with Unwrap() []error.
//
// Loader.LoadMachine deliberately keeps its own semicolon-joined string: its
// audience is a log line, not a list.
func ValidateMachineError(def *MachineDefinition, registry *Registry, s schema.DatabaseSchema) error {
	errs := ValidateMachine(def, registry, s)
	if len(errs) == 0 {
		return nil
	}
	joined := make([]error, len(errs))
	for i, e := range errs {
		joined[i] = e
	}
	return errors.Join(joined...)
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

// FieldIndex maps each component property name to the list of component names
// that declare a property with that name.
//
// Exported because Forge predicts this check while the schema is being edited:
// a context key matching two components' fields is a hard ValidateMachine
// error, and it surfaces at machine-load time, a long way from the schema edit
// that caused it. Sharing the index rather than rebuilding it is what keeps the
// prediction and the error it predicts from drifting apart.
//
// The returned lists are in map order. ValidateMachine sorts before rendering
// them; so should any other caller that shows them to a human.
func FieldIndex(s schema.DatabaseSchema) map[string][]string { return buildFieldIndex(s) }

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

// validateStateNode checks one state and everything under it.
//
// Targets are resolved with FindState — the interpreter's own resolver — rather
// than checked against a set of names. They used to be checked against
// collectStateIDs, which holds every bare state key and every StateNode.ID and
// so contains no dotted path at all. "combat.attacking" is XState v4's own
// notation and the interpreter traverses it segment by segment, so a machine
// with a transition into a nested state was resolvable at runtime and refused
// at load: it could not run, and it could not be opened in Forge to be fixed,
// because the loader validates before it registers anything.
func validateStateNode(def *MachineDefinition, node *StateNode, registry *Registry, resolver *StateResolver) []ValidationError {
	machineID := def.ID
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
			errs = append(errs, validateTransition(def, node.ID, t, registry, resolver)...)
		}
	}
	for duration, transitions := range node.After {
		if _, err := ParseDurationMs(duration); err != nil {
			errs = append(errs, ValidationError{
				MachineID: machineID, StateID: node.ID, Field: duration,
				Message: fmt.Sprintf("after duration %q is invalid: %v", duration, err),
			})
		}
		for _, t := range transitions {
			errs = append(errs, validateTransition(def, node.ID, t, registry, resolver)...)
		}
	}
	if node.Initial == "" && requiresInitial(node) {
		errs = append(errs, ValidationError{
			MachineID: machineID, StateID: node.ID,
			Message: "state has child states but no initial state",
		})
	}
	errs = append(errs, validateInitial(machineID, node.ID, node.Initial, node.Children)...)

	if node.Type == StateTypeHistory && node.Target != "" {
		if n, _ := resolver.Resolve(node.Target); n == nil {
			errs = append(errs, ValidationError{
				MachineID: machineID, StateID: node.ID, Field: node.Target,
				Message: fmt.Sprintf("history default target %q is not a known state", node.Target),
			})
		}
	}
	for _, child := range node.Children {
		errs = append(errs, validateStateNode(def, child, registry, resolver)...)
	}

	return errs
}

// validateInitial checks that an initial state names one of the children it is
// choosing between.
//
// Scope is the point. Transition targets are validated against every state in
// the machine, but `initial` selects a child of the state that declares it —
// validating it against the machine-wide set would accept a compound state
// whose initial names a state in a different branch entirely, which is exactly
// as broken at runtime as naming one that does not exist.
//
// Both forms are accepted: the bare child key as written in JSON, and the
// child's fully qualified dotted id. The rest of the codebase treats those
// interchangeably for targets, and making `initial` the one place a dotted id
// is rejected would be a trap rather than a rule.
//
// Parallel states are exempt: their regions are all entered at once, so an
// initial would be meaningless rather than missing. Final and history states
// have no children to choose between.
func validateInitial(machineID, stateID, initial string, children map[string]*StateNode) []ValidationError {
	if initial == "" {
		return nil
	}
	for name, child := range children {
		if initial == name || initial == child.ID {
			return nil
		}
	}
	return []ValidationError{{
		MachineID: machineID, StateID: stateID, Field: initial,
		Message: fmt.Sprintf("initial state %q is not a child state", initial),
	}}
}

// requiresInitial reports whether a node with children must name one to enter.
// A compound state without an initial has no defined entry point: the engine
// would enter it and settle in none of its children.
func requiresInitial(node *StateNode) bool {
	return len(node.Children) > 0 && node.Type != StateTypeParallel
}

func validateTransition(def *MachineDefinition, stateID string, t Transition, registry *Registry, resolver *StateResolver) []ValidationError {
	machineID := def.ID
	var errs []ValidationError

	if node, _ := resolver.Resolve(t.Target); node == nil && t.Target != "" {
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
