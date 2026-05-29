package agent

import (
	"encoding/json"
	"fmt"
)

// StateType values matching XState v4 semantics.
type StateType string

const (
	StateTypeAtomic   StateType = "atomic"
	StateTypeCompound StateType = "compound"
	StateTypeParallel StateType = "parallel"
	StateTypeFinal    StateType = "final"
	StateTypeHistory  StateType = "history"
)

// ActionSpec is an action — either a string shorthand or {type, params}.
type ActionSpec struct {
	Type   string
	Params map[string]any
}

// CondSpec is a guard condition — either a string shorthand or {type, params}.
type CondSpec struct {
	Type   string
	Params map[string]any
}

// Transition is a single transition within an "on" or "after" map entry.
type Transition struct {
	Target  string
	Cond    *CondSpec // nil = unconditional
	Actions []ActionSpec
}

// StateNode is one node in the machine tree.
type StateNode struct {
	ID       string
	Type     StateType
	Parent   *StateNode // nil for top-level states
	Children map[string]*StateNode
	Initial  string
	On       map[string][]Transition
	Entry    []ActionSpec
	Exit     []ActionSpec
	After    map[string][]Transition // key = raw duration string ("500", "1000ms")
	History  string                  // "shallow" or "deep"; history nodes only
	Target   string                  // default history target; history nodes only
}

// MachineDefinition is the parsed in-memory representation of an XState v4 machine.
type MachineDefinition struct {
	ID              string
	Initial         string
	Context         map[string]any
	States          map[string]*StateNode // top-level states
	ContextManifest map[string]string     // field → component name; populated by ValidateMachine
}

// ── Raw JSON structs ──────────────────────────────────────────────────────────

type rawMachine struct {
	ID      string                     `json:"id"`
	Initial string                     `json:"initial"`
	Context map[string]any             `json:"context"`
	States  map[string]json.RawMessage `json:"states"`
	Invoke  json.RawMessage            `json:"invoke"`
	On      map[string]json.RawMessage `json:"on"`
	Entry   json.RawMessage            `json:"entry"`
	Exit    json.RawMessage            `json:"exit"`
	After   map[string]json.RawMessage `json:"after"`
}

type rawStateNode struct {
	ID      string                     `json:"id"`
	Type    string                     `json:"type"`
	Initial string                     `json:"initial"`
	History string                     `json:"history"`
	Target  string                     `json:"target"`
	On      map[string]json.RawMessage `json:"on"`
	Entry   json.RawMessage            `json:"entry"`
	Exit    json.RawMessage            `json:"exit"`
	After   map[string]json.RawMessage `json:"after"`
	States  map[string]json.RawMessage `json:"states"`
	Invoke  json.RawMessage            `json:"invoke"`
}

// ── Public API ────────────────────────────────────────────────────────────────

// ParseMachine parses XState v4 JSON bytes into a MachineDefinition.
// invoke at any nesting level is rejected. Unknown fields are silently ignored.
func ParseMachine(data []byte) (*MachineDefinition, error) {
	var rm rawMachine
	if err := json.Unmarshal(data, &rm); err != nil {
		return nil, fmt.Errorf("ParseMachine: %w", err)
	}
	if isPresent(rm.Invoke) {
		return nil, fmt.Errorf("machine %q: invoke is not supported", rm.ID)
	}

	states := make(map[string]*StateNode, len(rm.States))
	for name, rawState := range rm.States {
		node, err := parseStateNode(rm.ID, name, rawState, nil)
		if err != nil {
			return nil, err
		}
		states[name] = node
	}

	return &MachineDefinition{
		ID:      rm.ID,
		Initial: rm.Initial,
		Context: rm.Context,
		States:  states,
	}, nil
}

// ── Tree builder ──────────────────────────────────────────────────────────────

func parseStateNode(machineID, name string, data json.RawMessage, parent *StateNode) (*StateNode, error) {
	var raw rawStateNode
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("machine %q: state %q: %w", machineID, name, err)
	}

	if isPresent(raw.Invoke) {
		return nil, fmt.Errorf("machine %q: state %q: invoke is not supported", machineID, name)
	}

	id := raw.ID
	if id == "" {
		id = machineID + "." + name
	}

	stateType := inferStateType(raw)

	entry, err := parseActionSpecs(raw.Entry)
	if err != nil {
		return nil, fmt.Errorf("machine %q: state %q: entry: %w", machineID, name, err)
	}
	exit, err := parseActionSpecs(raw.Exit)
	if err != nil {
		return nil, fmt.Errorf("machine %q: state %q: exit: %w", machineID, name, err)
	}
	on, err := parseTransitionMap(raw.On)
	if err != nil {
		return nil, fmt.Errorf("machine %q: state %q: on: %w", machineID, name, err)
	}
	after, err := parseTransitionMap(raw.After)
	if err != nil {
		return nil, fmt.Errorf("machine %q: state %q: after: %w", machineID, name, err)
	}

	node := &StateNode{
		ID:      id,
		Type:    stateType,
		Parent:  parent,
		Initial: raw.Initial,
		On:      on,
		Entry:   entry,
		Exit:    exit,
		After:   after,
		History: raw.History,
		Target:  raw.Target,
	}

	if len(raw.States) > 0 {
		node.Children = make(map[string]*StateNode, len(raw.States))
		for childName, childData := range raw.States {
			child, err := parseStateNode(machineID, childName, childData, node)
			if err != nil {
				return nil, err
			}
			node.Children[childName] = child
		}
	}

	return node, nil
}

func inferStateType(raw rawStateNode) StateType {
	switch raw.Type {
	case "parallel":
		return StateTypeParallel
	case "final":
		return StateTypeFinal
	case "history", "deep":
		return StateTypeHistory
	}
	if raw.History != "" {
		return StateTypeHistory
	}
	if len(raw.States) > 0 {
		return StateTypeCompound
	}
	return StateTypeAtomic
}

// isPresent reports whether a RawMessage contains a non-null JSON value.
func isPresent(data json.RawMessage) bool {
	return len(data) > 0 && string(data) != "null"
}

// ── Polymorphic parsing helpers ───────────────────────────────────────────────

func parseActionSpecs(data json.RawMessage) ([]ActionSpec, error) {
	if !isPresent(data) {
		return nil, nil
	}
	switch data[0] {
	case '"':
		spec, err := parseActionSpec(data)
		if err != nil {
			return nil, err
		}
		return []ActionSpec{spec}, nil
	case '[':
		var raws []json.RawMessage
		if err := json.Unmarshal(data, &raws); err != nil {
			return nil, err
		}
		specs := make([]ActionSpec, 0, len(raws))
		for _, r := range raws {
			spec, err := parseActionSpec(r)
			if err != nil {
				return nil, err
			}
			specs = append(specs, spec)
		}
		return specs, nil
	default:
		spec, err := parseActionSpec(data)
		if err != nil {
			return nil, err
		}
		return []ActionSpec{spec}, nil
	}
}

func parseActionSpec(data json.RawMessage) (ActionSpec, error) {
	if !isPresent(data) {
		return ActionSpec{}, fmt.Errorf("action spec is null or empty")
	}
	if data[0] == '"' {
		var name string
		if err := json.Unmarshal(data, &name); err != nil {
			return ActionSpec{}, err
		}
		return ActionSpec{Type: name}, nil
	}
	var obj struct {
		Type   string         `json:"type"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return ActionSpec{}, err
	}
	return ActionSpec{Type: obj.Type, Params: obj.Params}, nil
}

func parseCondSpec(data json.RawMessage) (*CondSpec, error) {
	if !isPresent(data) {
		return nil, nil
	}
	if data[0] == '"' {
		var name string
		if err := json.Unmarshal(data, &name); err != nil {
			return nil, err
		}
		return &CondSpec{Type: name}, nil
	}
	var obj struct {
		Type   string         `json:"type"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	return &CondSpec{Type: obj.Type, Params: obj.Params}, nil
}

func parseTransitions(data json.RawMessage) ([]Transition, error) {
	if !isPresent(data) {
		return nil, nil
	}
	switch data[0] {
	case '"':
		var target string
		if err := json.Unmarshal(data, &target); err != nil {
			return nil, err
		}
		return []Transition{{Target: target}}, nil
	case '[':
		var raws []json.RawMessage
		if err := json.Unmarshal(data, &raws); err != nil {
			return nil, err
		}
		transitions := make([]Transition, 0, len(raws))
		for _, r := range raws {
			t, err := parseTransitionItem(r)
			if err != nil {
				return nil, err
			}
			transitions = append(transitions, t)
		}
		return transitions, nil
	default:
		t, err := parseTransitionObject(data)
		if err != nil {
			return nil, err
		}
		return []Transition{t}, nil
	}
}

// parseTransitionItem handles a single element inside a transitions array.
// Each element must be a string (target shorthand) or object — not a nested array.
func parseTransitionItem(data json.RawMessage) (Transition, error) {
	if !isPresent(data) {
		return Transition{}, fmt.Errorf("transition element is null or empty")
	}
	switch data[0] {
	case '"':
		var target string
		if err := json.Unmarshal(data, &target); err != nil {
			return Transition{}, err
		}
		return Transition{Target: target}, nil
	case '[':
		return Transition{}, fmt.Errorf("nested transition arrays are not supported")
	default:
		return parseTransitionObject(data)
	}
}

func parseTransitionObject(data json.RawMessage) (Transition, error) {
	var raw struct {
		Target  string          `json:"target"`
		Cond    json.RawMessage `json:"cond"`
		Actions json.RawMessage `json:"actions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Transition{}, err
	}
	cond, err := parseCondSpec(raw.Cond)
	if err != nil {
		return Transition{}, fmt.Errorf("cond: %w", err)
	}
	actions, err := parseActionSpecs(raw.Actions)
	if err != nil {
		return Transition{}, fmt.Errorf("actions: %w", err)
	}
	return Transition{Target: raw.Target, Cond: cond, Actions: actions}, nil
}

func parseTransitionMap(raw map[string]json.RawMessage) (map[string][]Transition, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	m := make(map[string][]Transition, len(raw))
	for key, data := range raw {
		ts, err := parseTransitions(data)
		if err != nil {
			return nil, fmt.Errorf("event %q: %w", key, err)
		}
		m[key] = ts
	}
	return m, nil
}
