package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tmbritton/ecs-db/internal/jsonorder"
)

// EmitMachine serialises a machine definition back to XState v4 JSON.
//
// It is hand-written, and has to be. json.Marshal cannot do it for two separate
// reasons: StateNode.Parent is a back-pointer, so a machine with nested states
// produces "json: unsupported value: encountered a cycle via *agent.StateNode";
// and even where no cycle forms, the output is Go's field names — {"ID":"goblin",
// "States":{"idle":{"Type":"atomic","Parent":null,…}}} — which is not XState and
// never was.
//
// The formatting reproduces what the authored files use, so a load-emit round
// trip is byte-stable and a save diffs only what changed:
//
//   - two-space indent, trailing newline
//   - context, states, on and after render as expanded objects
//   - entry and exit render as expanded arrays, one action per line
//   - a transition array renders inline when it holds a single transition, and
//     expanded when it holds more
//   - action, guard, params and transition objects render inline
//
// EmitMachine is not validation. It writes what it is given, because the caller
// may be halfway through an edit; ValidateMachine is a separate call the save
// path makes first.
func EmitMachine(def *MachineDefinition) ([]byte, error) {
	if def == nil {
		return nil, fmt.Errorf("EmitMachine: nil definition")
	}

	var b strings.Builder
	b.WriteString("{\n")

	id, err := jsonString(def.ID)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(&b, "  \"id\": %s,\n", id)

	initial, err := jsonString(def.Initial)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(&b, "  \"initial\": %s,\n", initial)

	if len(def.Context) > 0 {
		b.WriteString("  \"context\": {\n")
		if err := writeContext(&b, "    ", def.ContextOrder, def.Context); err != nil {
			return nil, err
		}
		b.WriteString("  },\n")
	}

	if len(def.States) == 0 {
		// An empty object on one line, rather than a brace pair around nothing.
		b.WriteString("  \"states\": {}\n")
	} else {
		b.WriteString("  \"states\": {\n")
		if err := writeStates(&b, "    ", def.ID, def.StateOrder, def.States); err != nil {
			return nil, err
		}
		b.WriteString("  }\n")
	}

	b.WriteString("}\n")
	return []byte(b.String()), nil
}

func writeContext(b *strings.Builder, indent string, order []string, ctx map[string]any) error {
	keys := jsonorder.Apply(order, ctx)
	for i, key := range keys {
		k, err := jsonString(key)
		if err != nil {
			return err
		}
		v, err := jsonValue(ctx[key])
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "%s%s: %s", indent, k, v)
		if i != len(keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	return nil
}

func writeStates(b *strings.Builder, indent, machineID string, order []string, states map[string]*StateNode) error {
	names := jsonorder.Apply(order, states)
	for i, name := range names {
		key, err := jsonString(name)
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "%s%s: ", indent, key)
		if err := writeStateNode(b, indent, machineID, name, states[name]); err != nil {
			return err
		}
		if i != len(names)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	return nil
}

// writeStateNode emits one state. A state with nothing in it renders as {},
// which is how the authored files write a leaf.
func writeStateNode(b *strings.Builder, indent, machineID, name string, n *StateNode) error {
	inner := indent + "  "
	var out []string

	add := func(s string) { out = append(out, s) }

	// Only when it was authored. ParseMachine derives an id of
	// "<machine>.<state>" for any state that does not declare one, so emitting
	// it unconditionally would add a field to every state the file never had.
	if n.ID != "" && n.ID != machineID+"."+name {
		id, err := jsonString(n.ID)
		if err != nil {
			return err
		}
		add(fmt.Sprintf("%s\"id\": %s", inner, id))
	}

	// Only what was authored. Type is inferred at parse time from the presence
	// of states/history, so emitting it for every node would add a field the
	// file never had — except where it cannot be inferred.
	if n.Type == StateTypeParallel || n.Type == StateTypeFinal {
		typ, err := jsonString(string(n.Type))
		if err != nil {
			return err
		}
		add(fmt.Sprintf("%s\"type\": %s", inner, typ))
	}
	if n.Type == StateTypeHistory && n.History == "" {
		add(fmt.Sprintf("%s\"type\": \"history\"", inner))
	}
	if n.History != "" {
		if n.Type == StateTypeHistory {
			add(fmt.Sprintf("%s\"type\": \"history\"", inner))
		}
		hist, err := jsonString(n.History)
		if err != nil {
			return err
		}
		add(fmt.Sprintf("%s\"history\": %s", inner, hist))
	}
	if n.Target != "" {
		target, err := jsonString(n.Target)
		if err != nil {
			return err
		}
		add(fmt.Sprintf("%s\"target\": %s", inner, target))
	}
	if n.Initial != "" {
		initial, err := jsonString(n.Initial)
		if err != nil {
			return err
		}
		add(fmt.Sprintf("%s\"initial\": %s", inner, initial))
	}
	if len(n.Entry) > 0 {
		block, err := actionArray(n.Entry, inner)
		if err != nil {
			return err
		}
		add(fmt.Sprintf("%s\"entry\": %s", inner, block))
	}
	if len(n.Exit) > 0 {
		block, err := actionArray(n.Exit, inner)
		if err != nil {
			return err
		}
		add(fmt.Sprintf("%s\"exit\": %s", inner, block))
	}
	if len(n.On) > 0 {
		block, err := transitionMap(n.OnOrder, n.On, inner)
		if err != nil {
			return err
		}
		add(fmt.Sprintf("%s\"on\": %s", inner, block))
	}
	if len(n.After) > 0 {
		block, err := transitionMap(n.AfterOrder, n.After, inner)
		if err != nil {
			return err
		}
		add(fmt.Sprintf("%s\"after\": %s", inner, block))
	}
	if len(n.Children) > 0 {
		var nested strings.Builder
		nested.WriteString("{\n")
		if err := writeStates(&nested, inner+"  ", machineID, n.StateOrder, n.Children); err != nil {
			return err
		}
		fmt.Fprintf(&nested, "%s}", inner)
		add(fmt.Sprintf("%s\"states\": %s", inner, nested.String()))
	}
	if len(out) == 0 {
		b.WriteString("{}")
		return nil
	}
	b.WriteString("{\n")
	b.WriteString(strings.Join(out, ",\n"))
	fmt.Fprintf(b, "\n%s}", indent)
	return nil
}

// actionArray renders entry/exit: expanded, one action per line, each inline.
func actionArray(actions []ActionSpec, indent string) (string, error) {
	var b strings.Builder
	b.WriteString("[\n")
	for i, a := range actions {
		spec, err := actionObject(a)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%s  %s", indent, spec)
		if i != len(actions)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "%s]", indent)
	return b.String(), nil
}

func transitionMap(order []string, m map[string][]Transition, indent string) (string, error) {
	keys := jsonorder.Apply(order, m)
	var b strings.Builder
	b.WriteString("{\n")
	for i, key := range keys {
		k, err := jsonString(key)
		if err != nil {
			return "", err
		}
		list, err := transitionArray(m[key], indent+"  ")
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%s  %s: %s", indent, k, list)
		if i != len(keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "%s}", indent)
	return b.String(), nil
}

// transitionArray renders one event's transitions. A single transition stays on
// one line; several expand, which is how the authored files read.
func transitionArray(ts []Transition, indent string) (string, error) {
	parts := make([]string, 0, len(ts))
	for _, t := range ts {
		s, err := transitionObject(t)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	if len(parts) <= 1 {
		return "[" + strings.Join(parts, "") + "]", nil
	}

	var b strings.Builder
	b.WriteString("[\n")
	for i, p := range parts {
		fmt.Fprintf(&b, "%s  %s", indent, p)
		if i != len(parts)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "%s]", indent)
	return b.String(), nil
}

func transitionObject(t Transition) (string, error) {
	var parts []string
	if t.Target != "" {
		target, err := jsonString(t.Target)
		if err != nil {
			return "", err
		}
		parts = append(parts, "\"target\": "+target)
	}
	if t.Cond != nil {
		cond, err := specObject(t.Cond.Type, t.Cond.Params)
		if err != nil {
			return "", err
		}
		parts = append(parts, "\"cond\": "+cond)
	}
	if len(t.Actions) > 0 {
		specs := make([]string, 0, len(t.Actions))
		for _, a := range t.Actions {
			s, err := actionObject(a)
			if err != nil {
				return "", err
			}
			specs = append(specs, s)
		}
		parts = append(parts, "\"actions\": ["+strings.Join(specs, ", ")+"]")
	}
	if len(parts) == 0 {
		return "{}", nil
	}
	return "{ " + strings.Join(parts, ", ") + " }", nil
}

func actionObject(a ActionSpec) (string, error) { return specObject(a.Type, a.Params) }

// specObject renders an action or guard.
//
// Always the object form, even with no params. The authored files write
// { "type": "computePath" } rather than the bare string "computePath", and both
// are valid XState — so the long form is emitted consistently rather than
// switching on whether params happen to be present.
//
// A params map that is present but empty is not the same as an absent one:
// {"type":"a","params":{}} parses to an empty map and must come back that way,
// so the emptiness is checked with nil rather than len.
func specObject(typ string, params map[string]any) (string, error) {
	t, err := jsonString(typ)
	if err != nil {
		return "", err
	}
	if params == nil {
		return "{ \"type\": " + t + " }", nil
	}
	body, err := jsonValue(params)
	if err != nil {
		return "", err
	}
	return "{ \"type\": " + t + ", \"params\": " + body + " }", nil
}

// jsonValue renders an arbitrary context or params value compactly, with keys
// sorted — encoding/json sorts map keys, which is what makes it deterministic.
func jsonValue(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	compact := strings.TrimRight(buf.String(), "\n")
	return spaceInsideBraces(compact), nil
}

// spaceInsideBraces turns {"a":1} into { "a": 1 }, matching how the authored
// files write inline objects. Operating on the encoder's output rather than
// building the JSON by hand keeps the escaping correct.
func spaceInsideBraces(s string) string {
	var b strings.Builder
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && (c == '{'):
			b.WriteByte(c)
			if i+1 < len(s) && s[i+1] != '}' {
				b.WriteByte(' ')
			}
			continue
		case !inString && c == '}':
			if b.Len() > 0 && b.String()[b.Len()-1] != '{' {
				b.WriteByte(' ')
			}
		case !inString && (c == ':' || c == ','):
			b.WriteByte(c)
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// jsonString quotes a string the way encoding/json would, with HTML escaping
// off so < and & stay readable in a diff.
func jsonString(s string) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}
