package modes

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
	"github.com/tmbritton/ecs-db/internal/jsonorder"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// ValidationLevels are the levels the engine enforces, in the order offered.
var ValidationLevels = []schema.ValidationLevel{
	schema.ValidationStrict,
	schema.ValidationWarning,
}

// entityTypeNames returns the types in authored order, for the same reason
// componentNames does: the file's arrangement is intent.
func entityTypeNames(s schema.DatabaseSchema) []string {
	return jsonorder.Apply(s.EntityTypeOrder, s.EntityTypes)
}

// selectEntityType resolves the URL's ?type= to one that exists, falling back
// to the first rather than 404ing a stale bookmark.
func selectEntityType(s schema.DatabaseSchema, want string) string {
	if _, ok := s.EntityTypes[want]; ok {
		return want
	}
	if names := entityTypeNames(s); len(names) > 0 {
		return names[0]
	}
	return ""
}

// attachable lists the components not already on a type.
//
// A component may not be both required and optional, so each add control
// excludes both lists rather than only its own — offering a choice that the
// server will refuse is worse than not offering it.
func attachable(s schema.DatabaseSchema, et schema.EntityType) []string {
	on := map[string]bool{}
	for _, c := range et.RequiredComponents {
		on[c] = true
	}
	for _, c := range et.OptionalComponents {
		on[c] = true
	}
	var out []string
	for _, name := range componentNames(s) {
		if !on[name] {
			out = append(out, name)
		}
	}
	return out
}

// SeedsState is what the context-seeds panel can be showing.
type SeedsState int

const (
	// SeedsNone is a type with no machine bound.
	SeedsNone SeedsState = iota
	// SeedsMissing is a type naming a machine that did not resolve — either
	// no file declares that ID, or the file was rejected on the way in.
	SeedsMissing
	// SeedsMapped is the ordinary case.
	SeedsMapped
)

// Seed is one context key as the panel shows it.
type Seed struct {
	Key       string
	Value     any
	Component string // "" when the mapping is unavailable
}

// seedsFor describes the bound machine's context block.
//
// Only three states, not four. The plan called for a fourth — a machine that
// loaded but did not validate, so its ContextManifest was never built — and
// that state cannot occur: agent.Loader.LoadMachine returns an error and
// stores nothing when ValidateMachine reports anything, so every machine that
// reaches here validated, and its manifest covers every context key.
//
// A machine whose file exists and was rejected is therefore simply absent, and
// indistinguishable here from one nothing declares. Saying which it is needs
// the project's problem list, which is Story 7's subject; until then the panel
// names both possibilities rather than asserting the wrong one.
func seedsFor(data Data, et schema.EntityType) (SeedsState, []Seed) {
	if et.Behavior == "" {
		return SeedsNone, nil
	}
	m, ok := machineByID(data.Machines, et.Behavior)
	if !ok || m.Definition == nil {
		return SeedsMissing, nil
	}
	return SeedsMapped, contextSeeds(m.Definition, data.Schema)
}

// contextSeeds reads the machine's context in authored order.
//
// The manifest was computed by ValidateMachine against schema.json as it was
// when Forge started, and the schema being edited has moved on since. A
// component the manifest names that the session no longer declares is reported
// as unknown rather than printed with the same confidence as a live one —
// renaming a component in SCHEMA mode would otherwise leave this panel naming
// it for the rest of the process's life.
func contextSeeds(def *agent.MachineDefinition, current schema.DatabaseSchema) []Seed {
	keys := def.ContextOrder
	if len(keys) == 0 {
		for k := range def.Context {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	out := make([]Seed, 0, len(keys))
	for _, k := range keys {
		component := def.ContextManifest[k]
		if _, ok := current.Components[component]; !ok {
			component = ""
		}
		out = append(out, Seed{Key: k, Value: def.Context[k], Component: component})
	}
	return out
}

// attachOptions offers the free components behind a "choose" placeholder, so
// the dropdown's initial state is not itself a selection.
func attachOptions(free []string) []components.Option {
	out := []components.Option{{Value: "", Label: "choose…"}}
	for _, name := range free {
		out = append(out, components.Option{Value: name, Label: name})
	}
	return out
}

// seedValue renders a context value the way the behaviour file writes it, so a
// string reads as a string and a number as a number.
func seedValue(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "?"
	}
	return string(b)
}

// lower is the engine's table-naming rule, for the note that names the table.
func lower(s string) string { return strings.ToLower(s) }

func validationOptions() []components.Option {
	out := make([]components.Option, 0, len(ValidationLevels))
	for _, l := range ValidationLevels {
		out = append(out, components.Option{Value: string(l), Label: string(l)})
	}
	return out
}

// entityTypeHref links to a type, so selection survives a reload.
func entityTypeHref(name string) string {
	return "/forge/ents?type=" + urlValue(name)
}
