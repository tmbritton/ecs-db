package machines

// The mutations the canvas performs: move, add, rename, delete, connect,
// disconnect, set-initial.
//
// Every one of them goes through Session.Edit, which clones the working value,
// applies the change and swaps it under one lock. Nothing here writes a file or
// calls EmitMachine — saving is the footer's job, and a canvas that wrote on
// every drag would make Discard a lie. It is also what makes two edits arriving
// together safe: the second is applied to the result of the first rather than
// to whatever the client last saw.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
	"github.com/tmbritton/ecs-db/internal/jsonorder"
)

// MoveBy moves a state by a delta, reading where it currently sits and writing
// where it lands inside one edit.
//
// Inside one edit, and that is the whole point. The first version read the
// recorded position through a separate call and then wrote position+delta,
// which is a read-modify-write across two acquisitions of the session lock:
// eight drags arriving together left five of them lost. Reading the draft under
// the same lock that writes it is what makes "the second edit is applied to the
// result of the first" true rather than merely intended.
//
// The delta is in canvas pixels, which is also the unit a recorded position is
// in, so no conversion is needed — only the starting point, which is what the
// caller could not safely supply.
func (s *Session) MoveBy(path, statePath string, dx, dy float64) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		node, err := stateAt(def, statePath)
		if err != nil {
			return err
		}
		x, y := recordedPosition(def, statePath)
		return setLayout(node, x+dx, y+dy)
	})
}

// MoveState records where a state sits on the canvas, absolutely.
//
// x and y are in the file's own coordinates — what chart.Node.RecordedX reports.
// Callers moving a state by a gesture want MoveBy, which does the read and the
// write under one lock.
func (s *Session) MoveState(path, statePath string, x, y float64) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		node, err := stateAt(def, statePath)
		if err != nil {
			return err
		}
		return setLayout(node, x, y)
	})
}

// recordedPosition is where a state sits in the file's coordinates, including
// the fallback slot a state with no recorded position is drawn in — so the
// first drag of an unplaced state moves it from where it looks like it is
// rather than from the origin.
//
// Built from the chart because the fallback is the chart's rule and there must
// not be a second copy of it. The chart is a walk of a parsed machine; this is
// the same cost the render already pays each tick.
func recordedPosition(def *agent.MachineDefinition, statePath string) (x, y float64) {
	if node, ok := findChartNode(chart.Build(def, "").Nodes, statePath); ok {
		return node.RecordedX, node.RecordedY
	}
	return 0, 0
}

func findChartNode(nodes []chart.Node, statePath string) (chart.Node, bool) {
	for _, n := range nodes {
		if n.Path == statePath {
			return n, true
		}
		if got, ok := findChartNode(n.Children, statePath); ok {
			return got, true
		}
	}
	return chart.Node{}, false
}

// AddState adds an atomic state where the pointer was, named after nothing that
// is already there, and reports the name it chose.
//
// x and y are in canvas pixels. The chart may have shifted everything to bring
// something negative on screen, and that shift is taken off here — inside the
// edit, so it is read from the same value the state is added to.
func (s *Session) AddState(path string, x, y float64) (string, error) {
	var name string
	err := s.Edit(path, func(def *agent.MachineDefinition) error {
		built := chart.Build(def, "")
		x, y = x-built.OffsetX, y-built.OffsetY
		name = freeStateName(def.States, "state")
		node := &agent.StateNode{ID: def.ID + "." + name}
		// The back-pointer, which the interpreter walks in four places. Both
		// Edit and Working round-trip through the emitter and the parser, which
		// rebuilds them — but Edit stores the mutated draft directly, so
		// without this f.Current briefly holds a node whose Parent is nil.
		node.Parent = nil // a top-level state has none; named so the omission is deliberate
		if err := setLayout(node, x, y); err != nil {
			return err
		}
		if def.States == nil {
			def.States = map[string]*agent.StateNode{}
		}
		def.States[name] = node
		// At the end of the authored order, so the file reads in the order it
		// was built rather than resorting itself around a new arrival.
		def.StateOrder = append(jsonorder.Apply(def.StateOrder, def.States), name)
		def.StateOrder = dedupe(def.StateOrder)
		// A machine with states and no initial does not validate, so the first
		// state added to an empty machine becomes it.
		if def.Initial == "" {
			def.Initial = name
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return name, nil
}

// RenameState renames a state and everything in the machine that named it.
//
// Transitions that targeted it follow, and so does an initial that pointed at
// it — a rename that left either behind would break the machine in a way whose
// cause is two edits back by the time it is noticed.
func (s *Session) RenameState(path, statePath, to string) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		if err := validStateName(to); err != nil {
			return err
		}
		parent, key, err := stateParent(def, statePath)
		if err != nil {
			return err
		}
		siblings := levelOf(def, parent)
		if _, taken := siblings[to]; taken && to != key {
			return fmt.Errorf("a state called %q is already here", to)
		}
		// Where every transition points *before* anything moves, resolved the
		// engine's way. Matching target strings after the fact cannot work:
		// two states in different branches may share a bare name, so rewriting
		// every "attacking" in the machine repoints transitions that meant a
		// different one.
		aimed := resolveAllTargets(def)

		node := siblings[key]
		delete(siblings, key)
		siblings[to] = node
		node.ID = renamedID(node.ID, def.ID, key, to)
		renameInOrder(orderOf(def, parent), key, to)
		if parent == nil {
			if def.Initial == key {
				def.Initial = to
			}
		} else if parent.Initial == key {
			parent.Initial = to
		}
		retarget(def, aimed, statePath, replaceLast(statePath, to), to)
		return nil
	})
}

// DeleteState removes a state, and leaves the transitions that targeted it
// pointing at nothing.
//
// Deliberately: Story 4 draws a dangling edge rather than dropping it, and
// removing the transition would take its actions and its guard with it —
// silently, as a side effect of deleting something else. The caller warns first;
// TransitionsTargeting is what it warns with.
func (s *Session) DeleteState(path, statePath string) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		parent, key, err := stateParent(def, statePath)
		if err != nil {
			return err
		}
		siblings := levelOf(def, parent)
		if parent == nil && len(siblings) == 1 {
			return fmt.Errorf("a machine needs at least one state, and %q is the last one", key)
		}
		delete(siblings, key)
		removeFromOrder(orderOf(def, parent), key)
		// An initial naming a state that is gone is a validation error the
		// author did not make. Move it to whatever is left.
		if parent == nil {
			if def.Initial == key {
				def.Initial = firstOf(def.StateOrder, siblings)
			}
			return nil
		}
		if parent.Initial == key {
			parent.Initial = firstOf(parent.StateOrder, siblings)
		}
		return nil
	})
}

// SetInitial makes a state the one its container enters.
//
// Its container, not the machine: a nested state's initial belongs to the
// compound state that holds it, and setting the machine's to a name that is not
// one of its children is a validation error and a different machine.
func (s *Session) SetInitial(path, statePath string) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		parent, key, err := stateParent(def, statePath)
		if err != nil {
			return err
		}
		if parent == nil {
			def.Initial = key
			return nil
		}
		parent.Initial = key
		return nil
	})
}

// AddTransition connects two states and reports the event it invented.
//
// The server names it, not the client: an event is authored vocabulary, the
// machine already uses some of it, and a canvas that posted "EVENT" would
// silently merge the new transition into an existing one the second time.
func (s *Session) AddTransition(path, from, to string) (string, error) {
	var event string
	err := s.Edit(path, func(def *agent.MachineDefinition) error {
		src, err := stateAt(def, from)
		if err != nil {
			return fmt.Errorf("the transition's source: %w", err)
		}
		if _, err := stateAt(def, to); err != nil {
			return fmt.Errorf("the transition's target: %w", err)
		}
		event = freeEventName(def)
		if src.On == nil {
			src.On = map[string][]agent.Transition{}
		}
		// The dotted path, which the engine resolves unambiguously, rather than
		// the bare name — which may belong to a state in a different branch.
		src.On[event] = []agent.Transition{{Target: to}}
		src.OnOrder = append(jsonorder.Apply(src.OnOrder, src.On), event)
		src.OnOrder = dedupe(src.OnOrder)
		return nil
	})
	if err != nil {
		return "", err
	}
	return event, nil
}

// DeleteTransition removes one transition, addressed by its parts.
//
// By its parts and not by the chart's edge id: that id is source, kind, event
// and index joined with bars, and both a state name and an event name may
// contain a bar, so splitting it back apart is guesswork.
func (s *Session) DeleteTransition(path, from, kind, event string, index int) error {
	return s.Edit(path, func(def *agent.MachineDefinition) error {
		src, err := stateAt(def, from)
		if err != nil {
			return err
		}
		var (
			set   map[string][]agent.Transition
			order *[]string
		)
		switch kind {
		case "on":
			set, order = src.On, &src.OnOrder
		case "after":
			set, order = src.After, &src.AfterOrder
		default:
			return fmt.Errorf("%q is not a kind of transition", kind)
		}
		list, ok := set[event]
		if !ok {
			return fmt.Errorf("state %q has no %s %q", from, kind, event)
		}
		if index < 0 || index >= len(list) {
			return fmt.Errorf("state %q has %d transitions on %q, not %d", from, len(list), event, index+1)
		}
		list = append(list[:index:index], list[index+1:]...)
		if len(list) == 0 {
			// An event key with an empty list fires on nothing, draws no edge,
			// and reads in the file as something someone meant.
			delete(set, event)
			removeFromOrder(order, event)
			return nil
		}
		set[event] = list
		return nil
	})
}

// TransitionsTargeting is every transition that would be left dangling if a
// state went away, described rather than counted — a count does not tell you
// whether the thing about to break matters.
func (s *Session) TransitionsTargeting(path, statePath string) []string {
	var out []string
	// The error is deliberately dropped: this is what a warning is built from,
	// and a warning that fails to render is worse than one that is empty. The
	// operation it warns about reports its own failure.
	_ = s.Read(path, func(def *agent.MachineDefinition) {
		target, _ := stateAt(def, statePath)
		if target == nil {
			return
		}
		walkStates(def.States, "", func(path string, node *agent.StateNode) {
			for _, kind := range []struct {
				name  string
				set   map[string][]agent.Transition
				order []string
			}{{"on", node.On, node.OnOrder}, {"after", node.After, node.AfterOrder}} {
				for _, event := range jsonorder.Apply(kind.order, kind.set) {
					for _, tr := range kind.set[event] {
						if tr.Target == "" {
							continue
						}
						if found, _ := agent.FindState(def, tr.Target); found == target {
							out = append(out, fmt.Sprintf("%s on %s from %s", kind.name, event, path))
						}
					}
				}
			}
		})
	})
	return out
}

// ── the state tree ────────────────────────────────────────────────────────────

// stateAt resolves a dotted path exactly, segment by segment.
//
// Exactly, and not through agent.FindState, which is the *engine's* resolver: it
// searches by bare name and by id across the whole tree because that is what an
// author may write in a transition target. A path handed back by the canvas
// names one node, and accepting a fuzzy match for it would let an edit land on a
// state nobody pointed at.
func stateAt(def *agent.MachineDefinition, path string) (*agent.StateNode, error) {
	if path == "" {
		return nil, fmt.Errorf("no state named")
	}
	states := def.States
	var node *agent.StateNode
	for _, part := range strings.Split(path, ".") {
		next, ok := states[part]
		if !ok {
			return nil, fmt.Errorf("this machine has no state %q", path)
		}
		node, states = next, next.Children
	}
	return node, nil
}

// stateParent is stateAt, handing back the node's container and its key there.
// A nil parent means the machine root.
func stateParent(def *agent.MachineDefinition, path string) (*agent.StateNode, string, error) {
	if path == "" {
		return nil, "", fmt.Errorf("no state named")
	}
	idx := strings.LastIndex(path, ".")
	if idx < 0 {
		if _, ok := def.States[path]; !ok {
			return nil, "", fmt.Errorf("this machine has no state %q", path)
		}
		return nil, path, nil
	}
	parent, err := stateAt(def, path[:idx])
	if err != nil {
		return nil, "", err
	}
	key := path[idx+1:]
	if _, ok := parent.Children[key]; !ok {
		return nil, "", fmt.Errorf("this machine has no state %q", path)
	}
	return parent, key, nil
}

func levelOf(def *agent.MachineDefinition, parent *agent.StateNode) map[string]*agent.StateNode {
	if parent == nil {
		return def.States
	}
	return parent.Children
}

func orderOf(def *agent.MachineDefinition, parent *agent.StateNode) *[]string {
	if parent == nil {
		return &def.StateOrder
	}
	return &parent.StateOrder
}

func walkStates(states map[string]*agent.StateNode, prefix string, fn func(string, *agent.StateNode)) {
	for _, name := range jsonorder.Apply(nil, states) {
		node := states[name]
		fn(prefix+name, node)
		walkStates(node.Children, prefix+name+".", fn)
	}
}

// ── naming ────────────────────────────────────────────────────────────────────

// validStateName is narrower than the engine requires, on one point that
// matters: a dot is XState's path separator and the canvas keys its nodes on a
// dotted path, so a state called "a.b" is ambiguous to the engine's own
// resolver as well as to the chart. Story 4 pinned what happens to one; this is
// where Forge stops writing them.
func validStateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("a state needs a name")
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("a state name cannot start or end with a space")
	}
	if strings.Contains(name, ".") {
		return fmt.Errorf(
			"%q cannot be used: a dot separates the parts of a state path, so a name "+
				"containing one is ambiguous to the engine as well as to the canvas", name)
	}
	if len(name) > 96 {
		return fmt.Errorf("a state name must be 96 characters or fewer, got %d", len(name))
	}
	return nil
}

func freeStateName(states map[string]*agent.StateNode, base string) string {
	if _, taken := states[base]; !taken {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s%d", base, n)
		if _, taken := states[candidate]; !taken {
			return candidate
		}
	}
}

// freeEventName invents an event nothing in the machine already answers to.
//
// Across the whole machine rather than only the source state: an event name is
// the machine's vocabulary, and reusing one that means something elsewhere is
// how a new transition arrives already entangled with an old one.
//
// on keys only. XState allows a named delay as an after key, which would be
// another namespace to avoid — but this engine runs every after key through
// ParseDurationMs, so a machine with one never loads and the session never
// holds it. Scanning them would be a guard nothing can reach.
func freeEventName(def *agent.MachineDefinition) string {
	used := map[string]bool{}
	walkStates(def.States, "", func(_ string, node *agent.StateNode) {
		for event := range node.On {
			used[event] = true
		}
	})
	if !used["EVENT"] {
		return "EVENT"
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("EVENT%d", n)
		if !used[candidate] {
			return candidate
		}
	}
}

// ── layout ────────────────────────────────────────────────────────────────────

// forgeKey is where canvas coordinates live inside a state's meta. Story 1
// settled on meta rather than a sidecar, because ScanDir loads every .json in a
// behaviours directory and a sidecar registers itself as a machine with an
// empty id.
const (
	metaKey  = "meta"
	forgeKey = "forge"
)

// setLayout merges a position into a state's meta.
//
// Merges at both levels, and the second one is not obvious. meta is XState's
// sanctioned place for arbitrary per-state data and a user may have their own
// keys in it — the cost Story 1 accepted when it chose meta over a sidecar — so
// the position goes in beside them. But meta.forge is Forge's own object and
// later stories will put things in it, so replacing that wholesale would make
// the first drag destroy whatever they wrote. Only x and y are touched.
//
// Key order is preserved at both levels for the same reason everything else in
// this codebase preserves it: the file is read in diffs.
//
// A meta that is not an object cannot be merged into. Overwriting it would
// destroy whatever is there, so the move is refused instead: a canvas that eats
// your notes because you nudged a box is worse than one that says it cannot.
func setLayout(node *agent.StateNode, x, y float64) error {
	meta, order, err := objectOf(node.Extra[metaKey])
	if err != nil {
		return fmt.Errorf(
			"this state's meta is not an object, so there is nowhere to record a "+
				"position without destroying what is there: %w", err)
	}
	forge, forgeOrder, err := objectOf(meta[forgeKey])
	if err != nil {
		// Forge's own key, so this one is replaced rather than refused: a
		// non-object there is not something a user wrote on purpose.
		forge, forgeOrder = map[string]json.RawMessage{}, nil
	}
	forge["x"], forge["y"] = number(x), number(y)

	encoded, err := encodeObject(forge, forgeOrder)
	if err != nil {
		return err
	}
	meta[forgeKey] = encoded
	if encoded, err = encodeObject(meta, order); err != nil {
		return err
	}

	if node.Extra == nil {
		node.Extra = map[string]json.RawMessage{}
	}
	node.Extra[metaKey] = encoded
	if !contains(node.ExtraOrder, metaKey) {
		node.ExtraOrder = append(node.ExtraOrder, metaKey)
	}
	return nil
}

// objectOf decodes a raw JSON object along with its authored key order. An
// absent value is an empty object; anything that is not an object is an error.
func objectOf(raw json.RawMessage) (map[string]json.RawMessage, []string, error) {
	fields := map[string]json.RawMessage{}
	if len(raw) == 0 || string(raw) == "null" {
		return fields, nil, nil
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, nil, fmt.Errorf("%s", raw)
	}
	order, err := jsonorder.Keys(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("%s", raw)
	}
	return fields, order, nil
}

// encodeObject writes an object back out in the order its keys were authored,
// with anything new appended.
//
// Not json.Marshal over the map, which would alphabetise. Not with HTML
// escaping either: internal/agent/emit.go turns it off deliberately, and a
// state whose meta holds "a&b" should not come back holding "a&b" because
// someone nudged the box.
func encodeObject(fields map[string]json.RawMessage, order []string) (json.RawMessage, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, key := range jsonorder.Apply(order, fields) {
		if i > 0 {
			b.WriteByte(',')
		}
		encoded, err := jsonString(key)
		if err != nil {
			return nil, err
		}
		b.WriteString(encoded)
		b.WriteByte(':')
		b.Write(fields[key])
	}
	b.WriteByte('}')
	return json.RawMessage(b.String()), nil
}

// jsonString quotes a string without HTML escaping, matching what the emitter
// does for every other string it writes.
func jsonString(s string) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

func number(v float64) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		// A non-finite coordinate, which floats() refuses before this is
		// reached. Zero beats a malformed file.
		return json.RawMessage("0")
	}
	return raw
}

// ── small helpers ─────────────────────────────────────────────────────────────

// aimedAt records where one transition pointed before the machine was changed.
type aimedAt struct {
	set   map[string][]agent.Transition
	event string
	index int
	path  string // the state it resolved to, dotted from the root
}

// resolveAllTargets is where every transition in the machine points, resolved
// through the engine's own resolver.
//
// Taken before a rename rather than reconstructed after it, because resolution
// is what a target *means* and a string is only how it is spelled. Two states
// in different branches may share a bare name; rewriting by string would
// repoint a transition that meant the other one.
func resolveAllTargets(def *agent.MachineDefinition) []aimedAt {
	resolver := agent.NewStateResolver(def)
	var out []aimedAt
	walkStates(def.States, "", func(_ string, node *agent.StateNode) {
		for _, set := range []map[string][]agent.Transition{node.On, node.After} {
			for _, event := range jsonorder.Apply(nil, set) {
				for i, tr := range set[event] {
					if tr.Target == "" {
						continue
					}
					if _, path := resolver.Resolve(tr.Target); path != "" {
						out = append(out, aimedAt{set, event, i, path})
					}
				}
			}
		}
	})
	return out
}

// retarget points every transition that aimed at the renamed state, or at
// anything inside it, back at where it now lives.
//
// Three cases, and the middle one is what a string rewrite gets wrong. A
// transition that named the state itself follows it, keeping the spelling it
// was written in — a bare name stays bare. A transition into a *descendant*
// only needs rewriting if it was spelled as a path through the renamed state,
// because a bare name still resolves by descent. Everything else is left alone,
// including a transition elsewhere in the machine that happens to name a
// different state of the same name.
func retarget(def *agent.MachineDefinition, aimed []aimedAt, oldPath, newPath, newName string) {
	for _, a := range aimed {
		list := a.set[a.event]
		if a.index >= len(list) {
			continue
		}
		target := list[a.index].Target
		switch {
		case a.path == oldPath:
			if strings.Contains(target, ".") {
				list[a.index].Target = newPath
			} else {
				list[a.index].Target = newName
			}
		case strings.HasPrefix(a.path, oldPath+"."):
			// Into something inside the renamed state. Only a spelling that
			// goes *through* it has moved.
			if strings.HasPrefix(target, oldPath+".") {
				list[a.index].Target = newPath + strings.TrimPrefix(target, oldPath)
			}
		}
	}
	_ = def
}

// renamedID updates the id parsing derived from the state's name, and leaves an
// id the author wrote alone.
//
// The derived form is machineID + "." + name. Anything else is an "id" written
// in the file, which other transitions may target by name — rewriting it would
// break every one of them to fix a name nobody referred to.
func renamedID(id, machineID, from, to string) string {
	if id == machineID+"."+from {
		return machineID + "." + to
	}
	return id
}

func replaceLast(path, name string) string {
	if idx := strings.LastIndex(path, "."); idx >= 0 {
		return path[:idx+1] + name
	}
	return name
}

func renameInOrder(order *[]string, from, to string) {
	for i, name := range *order {
		if name == from {
			(*order)[i] = to
			return
		}
	}
	*order = append(*order, to)
}

func removeFromOrder(order *[]string, name string) {
	out := (*order)[:0]
	for _, got := range *order {
		if got != name {
			out = append(out, got)
		}
	}
	*order = out
}

func firstOf(order []string, states map[string]*agent.StateNode) string {
	for _, name := range order {
		if _, ok := states[name]; ok {
			return name
		}
	}
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func dedupe(names []string) []string {
	seen := map[string]bool{}
	out := names[:0]
	for _, name := range names {
		if !seen[name] {
			out = append(out, name)
			seen[name] = true
		}
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, got := range haystack {
		if got == needle {
			return true
		}
	}
	return false
}
