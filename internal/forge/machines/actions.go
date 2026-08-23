package machines

// A state's entry and exit actions, and the catalogue they are chosen from.
//
// Chosen, never typed. The registry is the list of what the engine will accept,
// so an action name that is not on it produces a machine the engine refuses to
// load — discovered at startup rather than at the moment of the mistake.
// Offering exactly that list, and refusing anything else here, makes the
// mistake unreachable rather than merely reported.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/project"
)

// ActionCatalogue is every action the engine would register for this project.
//
// Built the same way the engine builds it, which is why it can depend on
// whether the project has a map: the pathfinding and line-of-sight builtins
// capture a tile grid, so a mapless project does not get them and a machine
// using computePath would not load. Sorted by name, by Registry.Actions.
func (s *Session) ActionCatalogue() []agent.ActionMeta {
	return s.registry().Actions()
}

func (s *Session) registry() *agent.Registry {
	return project.BuildRegistry(s.cfg.HasMap)
}

// AddAction appends a registered action to a state's entry or exit list.
//
// Bare, with no parameters: the file then says only what has been chosen, and
// the control that adds one does not need a form before it knows what it is
// adding. SetActionParam fills them in.
func (s *Session) AddAction(path, statePath, kind, name string) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		node, err := stateAt(def, statePath)
		if err != nil {
			return err
		}
		list, err := actionList(node, kind)
		if err != nil {
			return err
		}
		return addActionTo(list, s.registry(), name)
	})
}

// addActionTo, removeActionFrom and setActionParamOn are the three edits an
// action list takes, written against the list and not against what holds it.
//
// A state's entry list, a state's exit list and a transition's actions are the
// same list with three owners, and Story 7 added the third. Two copies of
// "clearing the last parameter restores the bare shorthand" is how one of them
// stops doing it.
func addActionTo(list *[]agent.ActionSpec, r *agent.Registry, name string) error {
	if _, ok := actionMeta(r, name); !ok {
		return fmt.Errorf(
			"%q is not an action this project's engine registers, so a machine using it "+
				"would not load", name)
	}
	*list = append(*list, agent.ActionSpec{Type: name, Bare: true})
	return nil
}

func removeActionFrom(list *[]agent.ActionSpec, what string, index int) error {
	if index < 0 || index >= len(*list) {
		return fmt.Errorf("there are %d %s, not %d", len(*list), what, index+1)
	}
	*list = append((*list)[:index:index], (*list)[index+1:]...)
	if len(*list) == 0 {
		*list = nil
	}
	return nil
}

func setActionParamOn(list *[]agent.ActionSpec, r *agent.Registry, what string, index int, param, value string) error {
	if index < 0 || index >= len(*list) {
		return fmt.Errorf("there are %d %s, not %d", len(*list), what, index+1)
	}
	spec := &(*list)[index]
	schema, ok := paramSchema(r, spec.Type, param)
	if !ok {
		return fmt.Errorf("%q does not take a parameter called %q", spec.Type, param)
	}
	return setParam(&spec.Params, &spec.Bare, schema, value)
}

// RemoveAction takes one action out of a state's list, by position.
func (s *Session) RemoveAction(path, statePath, kind string, index int) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		node, err := stateAt(def, statePath)
		if err != nil {
			return err
		}
		list, err := actionList(node, kind)
		if err != nil {
			return err
		}
		return removeActionFrom(list, kind+" actions on this state", index)
	})
}

// SetActionParam writes one parameter of one action, converting the value by
// the type the registry declares for it.
//
// By the registry's type, because that is where the type is known. The engine
// reads params as their declared type, and a quoted number is a different value
// from a number.
//
// An empty value removes the parameter rather than writing "". An absent
// optional parameter is what the engine expects; an empty string is a value,
// and for dealDamage.target it is a different one from the default.
func (s *Session) SetActionParam(path, statePath, kind string, index int, param, value string) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		node, err := stateAt(def, statePath)
		if err != nil {
			return err
		}
		list, err := actionList(node, kind)
		if err != nil {
			return err
		}
		return setActionParamOn(list, s.registry(), kind+" actions on this state", index, param, value)
	})
}

// setParam writes one parameter into a spec's params, or clears it, restoring
// the bare shorthand when the last one goes.
//
// One implementation for actions and for guards. ActionSpec and CondSpec carry
// the same Params/Bare pair and the emitter applies the same rule to both, so
// the writing rule is one rule; a second copy is how a guard grows a behaviour
// an action does not have.
func setParam(params *map[string]any, bare *bool, schema agent.ParamSchema, value string) error {
	// Empty after trimming, so a field holding only spaces clears rather than
	// storing them. A string parameter's value is *not* trimmed when it is
	// stored: "  hi  " is something someone may have meant, and only a field
	// with nothing in it is a field that was cleared.
	if strings.TrimSpace(value) == "" {
		delete(*params, schema.Name)
		if len(*params) == 0 {
			// No empty object left behind: it would emit as "params": {}, which
			// reads as a decision someone made.
			*params = nil
			// And back to the bare string, which is the canonical spelling of
			// an action or a guard with nothing on it.
			//
			// Restored rather than preserved, because it cannot be preserved:
			// Edit clones by emitting and re-parsing, and one carrying params
			// emits as an object, so by the time the last one is cleared the
			// file has already forgotten it was ever written as a bare string.
			// Setting a parameter and changing your mind therefore left
			// {"type": "dealDamage"} behind forever. This returns the common
			// case — one Forge itself added — to exactly what it was.
			*bare = true
		}
		return nil
	}
	converted, err := convert(schema, value)
	if err != nil {
		return err
	}
	if *params == nil {
		*params = map[string]any{}
	}
	(*params)[schema.Name] = converted
	// Bare is deliberately not cleared. The emitter writes the bare string form
	// only when Bare is set *and* there are no params, so one carrying a
	// parameter emits as an object either way — and clearing the flag here
	// would only make that state stick after the params went again.
	return nil
}

// actionList picks the entry or the exit list, by name.
func actionList(node *agent.StateNode, kind string) (*[]agent.ActionSpec, error) {
	switch kind {
	case "entry":
		return &node.Entry, nil
	case "exit":
		return &node.Exit, nil
	default:
		return nil, fmt.Errorf("%q is not a kind of action list; entry and exit are", kind)
	}
}

func actionMeta(r *agent.Registry, name string) (agent.ActionMeta, bool) {
	for _, meta := range r.Actions() {
		if meta.Name == name {
			return meta, true
		}
	}
	return agent.ActionMeta{}, false
}

func paramSchema(r *agent.Registry, action, param string) (agent.ParamSchema, bool) {
	meta, ok := actionMeta(r, action)
	if !ok {
		return agent.ParamSchema{}, false
	}
	return schemaNamed(meta.Params, param)
}

// schemaNamed is the declaration of one parameter, from either an action's list
// or a guard's — the two are the same []ParamSchema and the lookup is the same
// lookup.
func schemaNamed(params []agent.ParamSchema, name string) (agent.ParamSchema, bool) {
	for _, p := range params {
		if p.Name == name {
			return p, true
		}
	}
	return agent.ParamSchema{}, false
}

// convert turns the string a form field produced into the type the registry
// declares, refusing anything that will not hold.
//
// Refused here rather than written through and discovered by the engine: a
// number that is really the word "quite" fails at the tick that runs the
// action, on a machine that loaded perfectly.
func convert(schema agent.ParamSchema, value string) (any, error) {
	switch schema.Type {
	case "number":
		// json.Unmarshal rather than strconv.ParseFloat, which accepts three
		// things the file cannot hold. NaN and ±Inf marshal into no JSON at
		// all, so storing one wedges the machine: every later edit clones
		// through the emitter and fails, and the only way out is Discard, which
		// throws away everything else unsaved. And ParseFloat takes Go literal
		// spellings — 1_000 and 0x1p4 — which would be silently reinterpreted
		// as 1000 and 16.
		var n float64
		if err := json.Unmarshal([]byte(value), &n); err != nil {
			return nil, fmt.Errorf("%s takes a number, and %q is not one", schema.Name, value)
		}
		return n, nil
	case "boolean":
		// The two spellings JSON has. ParseBool also takes "1", "t", "TRUE" and
		// five others, none of which the file can hold — and the control that
		// sends this is a checkbox, which posts exactly one of these two.
		switch value {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return nil, fmt.Errorf("%s takes true or false, and %q is neither", schema.Name, value)
		}
	case "object":
		var out map[string]any
		if err := json.Unmarshal([]byte(value), &out); err != nil {
			return nil, fmt.Errorf("%s takes a JSON object: %w", schema.Name, err)
		}
		if out == nil {
			// "null" unmarshals into a nil map without error, so it is the one
			// non-object that slips past the check above.
			return nil, fmt.Errorf("%s takes a JSON object, and null is not one", schema.Name)
		}
		return out, nil
	default:
		// "string", and anything a future built-in declares that is not one of
		// the above. A string is what the file holds and what the engine reads.
		return value, nil
	}
}

// StateAt is the state a dotted path names, resolved exactly.
//
// Exported for the inspector, which is addressing the node the canvas named
// rather than searching for one — the same rule every mutation in this package
// uses, and deliberately not agent.FindState, whose job is to resolve what an
// author wrote in a transition target.
func StateAt(def *agent.MachineDefinition, path string) (*agent.StateNode, error) {
	return stateAt(def, path)
}
