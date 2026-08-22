package agent

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/tmbritton/ecs-db/internal/jsonorder"
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
	// Bare records that this was authored as a plain string rather than an
	// object. See Form.
	Bare bool
	// Extra holds fields this package does not model. XState v4 gives an action
	// a description; dropping it is the same loss as dropping a state's.
	Extra      map[string]json.RawMessage
	ExtraOrder []string
}

// CondSpec is a guard condition — either a string shorthand or {type, params}.
type CondSpec struct {
	Type   string
	Params map[string]any
	// Bare records that this was authored as a plain string rather than an
	// object. See Form for why any of this is tracked.
	Bare bool
	// Extra holds fields this package does not model. See ActionSpec.Extra.
	Extra      map[string]json.RawMessage
	ExtraOrder []string
}

// Transition is a single transition within an "on" or "after" map entry.
type Transition struct {
	Target  string
	Cond    *CondSpec // nil = unconditional
	Actions []ActionSpec
	// Bare records that this was authored as a plain target string.
	Bare bool
	// Extra holds fields this package does not model — description, id, meta,
	// and notably "internal", which decides whether entry and exit actions
	// re-fire on a self-transition. Dropping that one changes what the machine
	// does, not just what the file looks like.
	Extra      map[string]json.RawMessage
	ExtraOrder []string
}

// Form records whether a value XState lets you write either way was authored
// wrapped in an array or on its own.
//
// XState accepts several spellings of the same thing: "on": {"E": "next"},
// {"E": {"target": "next"}} and {"E": [{"target": "next"}]} are one transition
// three ways, and entry/exit are the same. The engine does not care, but the
// file is in version control and read in diffs, so a save that rewrote every
// transition into this package's preferred spelling would be a whole-file diff
// nobody asked for — the same reason internal/jsonorder exists.
//
// The zero value is the canonical form, so a machine built in code — a state
// added on the canvas, say — emits the way the live files are written without
// anything having to say so.
type Form uint8

const (
	FormArray  Form = iota // [ … ]
	FormSingle             // one value, not wrapped in an array
)

// StateNode is one node in the machine tree.
//
// The *Order fields record the order keys appeared in the source file so the
// emitter can write it back that way. States are usually authored in the order
// they run rather than alphabetically, and alphabetising them on save would be
// a whole-file diff nobody asked for. Empty for a node built in code, in which
// case the emitter sorts — deterministic either way, never map order.
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

	StateOrder []string // authored order of Children
	OnOrder    []string // authored order of On's event keys
	AfterOrder []string // authored order of After's duration keys

	// How each of the above was spelled in the file. See Form.
	EntryForm Form
	ExitForm  Form
	OnForm    map[string]Form
	AfterForm map[string]Form

	// Extra holds fields this package does not model, so a save does not delete
	// them. See MachineDefinition.Extra.
	Extra      map[string]json.RawMessage
	ExtraOrder []string
}

// MachineDefinition is the parsed in-memory representation of an XState v4 machine.
type MachineDefinition struct {
	ID              string
	Initial         string
	Context         map[string]any
	States          map[string]*StateNode // top-level states
	ContextManifest map[string]string     // field → component name; populated by ValidateMachine

	// See StateNode for why these exist.
	StateOrder   []string
	ContextOrder []string

	// Extra holds top-level fields this package does not model, kept verbatim
	// so that emitting a machine does not delete them.
	//
	// The engine ignores them, which is the point: XState files carry
	// description, tags and meta, Stately Studio writes all three, and a
	// round trip through a tool that silently dropped them would destroy work
	// on a save the user asked for and would never think to check.
	Extra      map[string]json.RawMessage
	ExtraOrder []string
}

// ── Raw JSON structs ──────────────────────────────────────────────────────────

// rawMachine is what the machine object is decoded into, and — through
// knownKeys — the definition of which fields are "modelled" and therefore not
// preserved verbatim.
//
// It once declared on, entry, exit and after as well. Nothing ever read them:
// the machine root is a state node in XState and this package does not
// implement root-level transitions, so those four fields existed only to be
// swallowed by the decoder. That was invisible dead weight until unknown fields
// started being preserved, at which point it became a deletion allowlist — a
// machine with a global "on": {"RESET": "idle"} had it silently removed on
// save, with no error, which is the exact failure this story exists to stop.
//
// They are gone, so they reach Extra and survive. The engine still does not act
// on them; preserving something the engine ignores is a great deal better than
// deleting it.
type rawMachine struct {
	ID      string                     `json:"id"`
	Initial string                     `json:"initial"`
	Context map[string]any             `json:"context"`
	States  map[string]json.RawMessage `json:"states"`
	Invoke  json.RawMessage            `json:"invoke"`
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

	sections, err := sectionsOf(data)
	if err != nil {
		return nil, fmt.Errorf("machine %q: %w", rm.ID, err)
	}
	stateOrder, err := rawKeyOrder(sections, "states")
	if err != nil {
		return nil, fmt.Errorf("machine %q: reading state order: %w", rm.ID, err)
	}
	contextOrder, err := rawKeyOrder(sections, "context")
	if err != nil {
		return nil, fmt.Errorf("machine %q: reading context order: %w", rm.ID, err)
	}
	extra, extraOrder, err := extraFields(data, machineKeys)
	if err != nil {
		return nil, fmt.Errorf("machine %q: reading unmodelled fields: %w", rm.ID, err)
	}

	context := rm.Context
	if len(context) == 0 {
		// Same normalisation as an empty action list, for the same reason.
		context = nil
	}

	return &MachineDefinition{
		ID:           rm.ID,
		Initial:      rm.Initial,
		Context:      context,
		States:       states,
		StateOrder:   stateOrder,
		ContextOrder: contextOrder,
		Extra:        extra,
		ExtraOrder:   extraOrder,
	}, nil
}

// sectionsOf splits a JSON object into its raw top-level values once, so the
// order of several sections can be read without re-parsing the whole subtree
// for each — which made ParseMachine several times slower on a large machine.
func sectionsOf(data []byte) (map[string]json.RawMessage, error) {
	var sections map[string]json.RawMessage
	if err := json.Unmarshal(data, &sections); err != nil {
		return nil, err
	}
	return sections, nil
}

// rawKeyOrder reads the authored key order of one section.
//
// The lookup is case-insensitive because encoding/json's field matching is:
// a machine authored with "States" or "On" decodes perfectly well, and an
// exact-match lookup here would silently record no order for it and fall back
// to alphabetical — the whole-file diff on save that internal/jsonorder exists
// to prevent.
func rawKeyOrder(sections map[string]json.RawMessage, field string) ([]string, error) {
	if raw, ok := sections[field]; ok {
		return jsonorder.Keys(raw)
	}
	for key, raw := range sections {
		if strings.EqualFold(key, field) {
			return jsonorder.Keys(raw)
		}
	}
	return nil, nil
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
	extra, extraOrder, err := extraFields(data, stateKeys)
	if err != nil {
		return nil, fmt.Errorf("machine %q: state %q: reading unmodelled fields: %w", machineID, name, err)
	}

	sections, err := sectionsOf(data)
	if err != nil {
		return nil, fmt.Errorf("machine %q: state %q: %w", machineID, name, err)
	}
	stateOrder, err := rawKeyOrder(sections, "states")
	if err != nil {
		return nil, fmt.Errorf("machine %q: state %q: reading state order: %w", machineID, name, err)
	}
	onOrder, err := rawKeyOrder(sections, "on")
	if err != nil {
		return nil, fmt.Errorf("machine %q: state %q: reading event order: %w", machineID, name, err)
	}
	afterOrder, err := rawKeyOrder(sections, "after")
	if err != nil {
		return nil, fmt.Errorf("machine %q: state %q: reading after order: %w", machineID, name, err)
	}

	node := &StateNode{
		ID:         id,
		Type:       stateType,
		Parent:     parent,
		Initial:    raw.Initial,
		On:         on,
		Entry:      entry,
		Exit:       exit,
		After:      after,
		History:    raw.History,
		Target:     raw.Target,
		StateOrder: stateOrder,
		OnOrder:    onOrder,
		AfterOrder: afterOrder,
		EntryForm:  listForm(raw.Entry),
		ExitForm:   listForm(raw.Exit),
		OnForm:     transitionForms(raw.On),
		AfterForm:  transitionForms(raw.After),
		Extra:      extra,
		ExtraOrder: extraOrder,
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
// machineKeys and stateKeys are the JSON names the parser models, derived from
// the raw structs rather than typed out again.
//
// A hand-written list goes stale the first time someone adds a field to the
// parser, and it fails badly: the new field would be parsed normally *and*
// captured as an unknown one, so the emitter would write it twice and produce a
// file with two "initial" keys. Deriving it means adding a field to the parser
// automatically stops it being an extra.
//
// invoke is in these tags, so it is excluded by construction — which is right.
// It is a known field the engine refuses, not an unknown one, and preserving it
// would produce a file Forge accepts and the engine will not load.
var (
	machineKeys    = knownKeys(reflect.TypeOf(rawMachine{}))
	stateKeys      = knownKeys(reflect.TypeOf(rawStateNode{}))
	transitionKeys = knownKeys(reflect.TypeOf(rawTransition{}))
	specKeys       = knownKeys(reflect.TypeOf(rawSpec{}))
)

// rawTransition and rawSpec are the shapes the inline objects decode into.
// Named types rather than the anonymous structs they replace, so knownKeys can
// derive their field sets the same way it does for the two outer ones.
type rawTransition struct {
	Target  string          `json:"target"`
	Cond    json.RawMessage `json:"cond"`
	Actions json.RawMessage `json:"actions"`
}

// rawSpec is both an action and a guard: they have the same shape.
type rawSpec struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params"`
}

// knownKeys returns every field name encoding/json would bind on a struct,
// lowercased because its matching is case-insensitive: a machine authored with
// "States" decodes into States, so an extra by that name has already been
// consumed and must not be captured a second time.
//
// Three shapes made the first version wrong, and all three are ones a future
// field is likely to have: a field with no tag at all binds under its own name;
// a tag written `json:",omitempty"` has an empty name and binds under the field
// name too; and an embedded struct contributes its fields to the parent object.
// Each would have been parsed *and* captured, so the emitter would write it
// twice — the very failure deriving this set is supposed to make impossible.
func knownKeys(t reflect.Type) map[string]bool {
	keys := make(map[string]bool)
	for _, f := range reflect.VisibleFields(t) {
		if f.Anonymous {
			// Contributes its own fields, which VisibleFields also reports.
			continue
		}
		if !f.IsExported() {
			// VisibleFields reports these; encoding/json never binds them. A
			// future unexported field would otherwise claim its own name, and
			// an input key that happened to match would be excluded from Extra
			// and deleted on save.
			continue
		}
		tag := f.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		switch name {
		case "-":
			continue
		case "":
			name = f.Name
		}
		keys[strings.ToLower(name)] = true
	}
	return keys
}

// extraFields returns the object's keys that known does not claim, in document
// order, with their values untouched.
func extraFields(data json.RawMessage, known map[string]bool) (map[string]json.RawMessage, []string, error) {
	order, err := jsonorder.Keys(data)
	if err != nil {
		return nil, nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, nil, err
	}
	var kept []string
	out := make(map[string]json.RawMessage)
	for _, key := range order {
		if known[strings.ToLower(key)] {
			continue
		}
		if _, seen := out[key]; seen {
			// A duplicate key in the source. encoding/json keeps the last, so
			// so does this — emitting it twice would produce a file that no
			// longer parses the way the original did.
			continue
		}
		out[key] = fields[key]
		kept = append(kept, key)
	}
	if len(kept) == 0 {
		return nil, nil, nil
	}
	return out, kept, nil
}

func isPresent(data json.RawMessage) bool {
	return len(data) > 0 && string(data) != "null"
}

// ── Polymorphic parsing helpers ───────────────────────────────────────────────

// listForm reports whether a value was authored wrapped in an array. It reads
// the first byte rather than re-parsing: by the time it is called the value has
// already been parsed successfully, so the byte is enough and a second decode
// would only be another thing to keep in step.
func listForm(data json.RawMessage) Form {
	if !isPresent(data) || data[0] == '[' {
		return FormArray
	}
	return FormSingle
}

// transitionForms records the spelling of each entry in an on/after map.
func transitionForms(m map[string]json.RawMessage) map[string]Form {
	if len(m) == 0 {
		return nil
	}
	forms := make(map[string]Form, len(m))
	for key, raw := range m {
		forms[key] = listForm(raw)
	}
	return forms
}

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
		if len(raws) == 0 {
			// An empty list and an absent one mean the same thing here: no
			// actions. Normalising at parse is what makes the emitter's
			// round-trip property actually true — otherwise "entry": []
			// comes back nil and the definitions no longer compare equal.
			//
			// Deliberately unlike an action's params, where {} and absent are
			// genuinely different: {} is a parameter set that happens to be
			// empty, and specObject preserves that distinction.
			return nil, nil
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
		return ActionSpec{Type: name, Bare: true}, nil
	}
	var obj rawSpec
	if err := json.Unmarshal(data, &obj); err != nil {
		return ActionSpec{}, err
	}
	extra, extraOrder, err := extraFields(data, specKeys)
	if err != nil {
		return ActionSpec{}, err
	}
	return ActionSpec{Type: obj.Type, Params: obj.Params, Extra: extra, ExtraOrder: extraOrder}, nil
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
		return &CondSpec{Type: name, Bare: true}, nil
	}
	var obj rawSpec
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	extra, extraOrder, err := extraFields(data, specKeys)
	if err != nil {
		return nil, err
	}
	return &CondSpec{Type: obj.Type, Params: obj.Params, Extra: extra, ExtraOrder: extraOrder}, nil
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
		return []Transition{{Target: target, Bare: true}}, nil
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
		return Transition{Target: target, Bare: true}, nil
	case '[':
		return Transition{}, fmt.Errorf("nested transition arrays are not supported")
	default:
		return parseTransitionObject(data)
	}
}

func parseTransitionObject(data json.RawMessage) (Transition, error) {
	var raw rawTransition
	if err := json.Unmarshal(data, &raw); err != nil {
		return Transition{}, err
	}
	extra, extraOrder, err := extraFields(data, transitionKeys)
	if err != nil {
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
	return Transition{
		Target: raw.Target, Cond: cond, Actions: actions,
		Extra: extra, ExtraOrder: extraOrder,
	}, nil
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
