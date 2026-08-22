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

	var parts []string

	id, err := jsonString(def.ID)
	if err != nil {
		return nil, err
	}
	parts = append(parts, fmt.Sprintf("  \"id\": %s", id))

	initial, err := jsonString(def.Initial)
	if err != nil {
		return nil, err
	}
	parts = append(parts, fmt.Sprintf("  \"initial\": %s", initial))

	if len(def.Context) > 0 {
		var ctx strings.Builder
		ctx.WriteString("  \"context\": {\n")
		if err := writeContext(&ctx, "    ", def.ContextOrder, def.Context); err != nil {
			return nil, err
		}
		ctx.WriteString("  }")
		parts = append(parts, ctx.String())
	}

	if len(def.States) == 0 {
		// An empty object on one line, rather than a brace pair around nothing.
		parts = append(parts, "  \"states\": {}")
	} else {
		var st strings.Builder
		st.WriteString("  \"states\": {\n")
		if err := writeStates(&st, "    ", def.ID, def.StateOrder, def.States); err != nil {
			return nil, err
		}
		st.WriteString("  }")
		parts = append(parts, st.String())
	}

	extras, err := writeExtras("  ", def.ExtraOrder, def.Extra, machineKeys)
	if err != nil {
		return nil, err
	}
	parts = append(parts, extras...)

	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString(strings.Join(parts, ",\n"))
	b.WriteString("\n}\n")
	return []byte(b.String()), nil
}

// writeExtras renders the fields this package does not model, in the order they
// were authored, after the ones it does.
//
// After rather than interleaved, deliberately: writeStateNode already emits
// known fields in a fixed order rather than the authored one, so slotting
// unknown fields back into their original positions among them would produce a
// jumble rather than the original file. Relative order among the extras is
// preserved, which is what makes a second round trip a no-op.
//
// The value is written as it was authored, shifted to the depth it is being
// written at. json.Indent was the obvious choice and is the wrong one: it
// imposes its own layout, so ["enemy", "stationary"] comes back as three lines
// and every Stately file is reformatted by a tool that was supposed to be
// leaving it alone. Since json.RawMessage keeps the source bytes whitespace and
// all, shifting is both simpler and exact — for a plain round trip the depth is
// unchanged, so the text is reproduced byte for byte.
//
// A scalar passes through untouched either way, so an extra's number formatting
// survives exactly — "weight": 2.0 stays 2.0, even though a context value's
// does not.
func writeExtras(indent string, order []string, extra map[string]json.RawMessage, known map[string]bool) ([]string, error) {
	if len(extra) == 0 {
		return nil, nil
	}
	// jsonorder.Apply, not a bare range over order: a key in the map but not in
	// the recorded order was silently never written, and that is precisely the
	// path Story 4 takes — writing layout into a state whose file had no meta
	// at all, so nothing recorded an order for it. Apply appends unrecorded
	// keys, sorted, which is what every other order-preserving site here does.
	keys := jsonorder.Apply(order, extra)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		raw := extra[key]
		if known[strings.ToLower(key)] {
			// The emitter writes this field itself. Emitting it again would
			// produce an object with the key twice — the failure the derived
			// key set prevents on the way in, which was unguarded on the way
			// out and reachable by anything that writes into Extra.
			return nil, fmt.Errorf("field %q is written by the emitter and must not also be carried as an extra", key)
		}
		k, err := jsonString(key)
		if err != nil {
			return nil, err
		}
		if !json.Valid(raw) {
			// Unreachable through ParseMachine, which decoded this value to get
			// here. Guarded anyway: a caller assembling a definition by hand
			// could put anything in the map, and emitting it unchecked would
			// produce a file that no longer parses.
			return nil, fmt.Errorf("field %q: not valid JSON", key)
		}
		out = append(out, fmt.Sprintf("%s%s: %s", indent, k, shiftIndent(raw, indent)))
	}
	return out, nil
}

// shiftIndent moves a raw JSON value to a new indentation depth, preserving the
// author's own line breaks and spacing within it.
//
// The first line is left alone — it follows "key": on a line already indented.
// Every other line is re-based from the value's original depth, which is the
// shallowest indentation it uses: that is where its closing brace sits, and the
// closing brace sits at the key's depth.
func shiftIndent(raw json.RawMessage, indent string) string {
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 1 {
		return lines[0]
	}
	// A carriage return would otherwise leave every line ending in \r, so a
	// blank interior line measures as depth 0, base collapses to 0, and the
	// value is handed back unshifted with CRLF spliced into an LF file.
	base := -1
	for _, line := range lines[1:] {
		trimmed := strings.TrimLeft(strings.TrimRight(line, "\r"), " \t")
		if trimmed == "" {
			continue
		}
		if depth := len(line) - len(trimmed); base < 0 || depth < base {
			base = depth
		}
	}
	if base <= 0 {
		return string(raw)
	}
	out := make([]string, 0, len(lines))
	out = append(out, lines[0])
	for _, line := range lines[1:] {
		stripped := strings.TrimRight(line, "\r")
		trimmed := strings.TrimLeft(stripped, " \t")
		if trimmed == "" {
			out = append(out, "")
			continue
		}
		// depth cannot be below base: base is the minimum over these same lines.
		depth := len(stripped) - len(trimmed)
		out = append(out, indent+strings.Repeat(" ", depth-base)+trimmed)
	}
	return strings.Join(out, "\n")
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
		block, err := actionList(n.Entry, n.EntryForm, inner)
		if err != nil {
			return err
		}
		add(fmt.Sprintf("%s\"entry\": %s", inner, block))
	}
	if len(n.Exit) > 0 {
		block, err := actionList(n.Exit, n.ExitForm, inner)
		if err != nil {
			return err
		}
		add(fmt.Sprintf("%s\"exit\": %s", inner, block))
	}
	if len(n.On) > 0 {
		block, err := transitionMap(n.OnOrder, n.On, n.OnForm, inner)
		if err != nil {
			return err
		}
		add(fmt.Sprintf("%s\"on\": %s", inner, block))
	}
	if len(n.After) > 0 {
		block, err := transitionMap(n.AfterOrder, n.After, n.AfterForm, inner)
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
	extras, err := writeExtras(inner, n.ExtraOrder, n.Extra, stateKeys)
	if err != nil {
		return err
	}
	out = append(out, extras...)
	if len(out) == 0 {
		b.WriteString("{}")
		return nil
	}
	b.WriteString("{\n")
	b.WriteString(strings.Join(out, ",\n"))
	fmt.Fprintf(b, "\n%s}", indent)
	return nil
}

// actionList renders entry/exit the way it was authored.
//
// The unwrapped spelling is only available while the list still holds exactly
// one action: adding a second has to produce an array, whatever the file said
// before. Every form decision in this file is guarded that way, so an edit can
// always be expressed and a spelling is never preserved into a lie.
func actionList(actions []ActionSpec, form Form, indent string) (string, error) {
	if form == FormSingle && len(actions) == 1 {
		return actionObject(actions[0])
	}
	return actionArray(actions, indent)
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

func transitionList(ts []Transition, form Form, indent string) (string, error) {
	if form == FormSingle && len(ts) == 1 {
		return transitionObject(ts[0])
	}
	return transitionArray(ts, indent)
}

func transitionMap(order []string, m map[string][]Transition, forms map[string]Form, indent string) (string, error) {
	keys := jsonorder.Apply(order, m)
	var b strings.Builder
	b.WriteString("{\n")
	for i, key := range keys {
		k, err := jsonString(key)
		if err != nil {
			return "", err
		}
		list, err := transitionList(m[key], forms[key], indent+"  ")
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
	// A bare target string, but only while the target is all there is: adding a
	// guard or an action has to produce an object, whatever the file said.
	if t.Bare && t.Cond == nil && len(t.Actions) == 0 && len(t.Extra) == 0 {
		return jsonString(t.Target)
	}
	var parts []string
	if t.Target != "" {
		target, err := jsonString(t.Target)
		if err != nil {
			return "", err
		}
		parts = append(parts, "\"target\": "+target)
	}
	if t.Cond != nil {
		cond, err := condObject(*t.Cond)
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
	rest, err := inlineExtras(t.ExtraOrder, t.Extra, transitionKeys)
	if err != nil {
		return "", err
	}
	parts = append(parts, rest...)
	if len(parts) == 0 {
		return "{}", nil
	}
	return "{ " + strings.Join(parts, ", ") + " }", nil
}

func actionObject(a ActionSpec) (string, error) {
	// As transitionObject: the bare spelling holds only while there is nothing
	// else to say. Params is compared against nil rather than by length,
	// matching specObject: a params map that is present but empty is not the
	// same as an absent one, and collapsing to a bare string would lose it.
	if a.Bare && a.Params == nil && len(a.Extra) == 0 {
		return jsonString(a.Type)
	}
	return specObject(a.Type, a.Params, a.ExtraOrder, a.Extra)
}

// condObject renders a guard, bare when that is how it was written and there is
// nothing more to say about it.
func condObject(c CondSpec) (string, error) {
	if c.Bare && c.Params == nil && len(c.Extra) == 0 {
		return jsonString(c.Type)
	}
	return specObject(c.Type, c.Params, c.ExtraOrder, c.Extra)
}

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
func specObject(typ string, params map[string]any, extraOrder []string, extra map[string]json.RawMessage) (string, error) {
	t, err := jsonString(typ)
	if err != nil {
		return "", err
	}
	parts := []string{"\"type\": " + t}
	if params != nil {
		body, err := jsonValue(params)
		if err != nil {
			return "", err
		}
		parts = append(parts, "\"params\": "+body)
	}
	rest, err := inlineExtras(extraOrder, extra, specKeys)
	if err != nil {
		return "", err
	}
	parts = append(parts, rest...)
	return "{ " + strings.Join(parts, ", ") + " }", nil
}

// inlineExtras renders unmodelled fields for the objects this file writes on a
// single line — a transition, an action, a guard.
//
// A value already on one line is used as authored. One that is not is compacted
// rather than splayed across the inline object it sits in; that changes its
// spacing but never its content, and it settles, because a compacted value is
// itself one line.
func inlineExtras(order []string, extra map[string]json.RawMessage, known map[string]bool) ([]string, error) {
	if len(extra) == 0 {
		return nil, nil
	}
	keys := jsonorder.Apply(order, extra)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		raw := extra[key]
		if known[strings.ToLower(key)] {
			return nil, fmt.Errorf("field %q is written by the emitter and must not also be carried as an extra", key)
		}
		k, err := jsonString(key)
		if err != nil {
			return nil, err
		}
		if !json.Valid(raw) {
			return nil, fmt.Errorf("field %q: not valid JSON", key)
		}
		value := string(raw)
		if strings.Contains(value, "\n") {
			var buf bytes.Buffer
			if err := json.Compact(&buf, raw); err != nil {
				return nil, fmt.Errorf("field %q: %w", key, err)
			}
			value = buf.String()
		}
		out = append(out, k+": "+value)
	}
	return out, nil
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
