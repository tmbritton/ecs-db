package modes

import (
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
)

// MachineDefinition is aliased for the same reason Component and EntityType
// are: a .templ file in this package cannot import agent without colliding with
// templ's codegen.
type MachineDefinition = agent.MachineDefinition

// machineHref links to a machine by path, so a selection survives a reload.
// By path and not by id, because the id lives inside the file and can be edited
// — a URL keyed on it would stop resolving the moment someone renamed it.
func machineHref(path string) string { return "/forge/agents?machine=" + urlValue(path) }

// machineBase is the filename without its extension, which is what the rename
// control edits.
func machineBase(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		path = path[i+1:]
	}
	return strings.TrimSuffix(path, ".json")
}

func machineID(def *MachineDefinition) string {
	if def == nil {
		return ""
	}
	return def.ID
}

// stateNames lists a machine's top-level states in authored order.
func stateNames(def *MachineDefinition) []string {
	if def == nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(def.States))
	for _, name := range def.StateOrder {
		if _, ok := def.States[name]; ok && !seen[name] {
			out = append(out, name)
			seen[name] = true
		}
	}
	var rest []string
	for name := range def.States {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func isInitial(def *MachineDefinition, name string) bool {
	return def != nil && def.Initial == name
}

func modOptions(mods []project.Mod) []components.Option {
	out := make([]components.Option, 0, len(mods)+1)
	out = append(out, components.Option{Value: "", Label: "choose a mod…"})
	for _, m := range mods {
		out = append(out, components.Option{Value: m.Name, Label: m.Name})
	}
	return out
}

// newMachineAction posts the chosen mod along with a starter id.
func newMachineAction() string {
	return "@post('/forge/agents/machine?add=NewMachine&mod=' + encodeURIComponent(evt.target.value))"
}

// bindingWarning says what renaming this id would break, before it breaks.
//
// Epic 12's inline validation reports a dangling binding after the fact; saying
// it here means the user is told while the decision is still theirs to make.
func bindingWarning(data Data, id string) string {
	bound := boundBy(data, id)
	switch len(bound) {
	case 0:
		return "Nothing in schema.json binds this machine, so renaming the id breaks nothing."
	case 1:
		return "Renaming this id will leave " + bound[0] + " bound to a machine that no longer exists."
	default:
		return "Renaming this id will leave " + strings.Join(bound, ", ") +
			" bound to a machine that no longer exists."
	}
}

func deleteWarning(data Data, id string) string {
	bound := boundBy(data, id)
	if len(bound) == 0 {
		return "Delete machine " + id + "? Nothing in schema.json binds it."
	}
	return "Delete machine " + id + "? " + strings.Join(bound, ", ") +
		" bind it and will be left pointing at a machine that does not exist."
}

// boundBy is everything in schema.json whose behavior field names this machine,
// components as well as entity types — reporting only half the damage would be
// worse than reporting none.
func boundBy(data Data, id string) []string {
	if id == "" {
		return nil
	}
	var out []string
	for _, name := range componentNames(data.Schema) {
		if data.Schema.Components[name].Behavior == id {
			out = append(out, name)
		}
	}
	for _, name := range entityTypeNames(data.Schema) {
		if data.Schema.EntityTypes[name].Behavior == id {
			out = append(out, name)
		}
	}
	return out
}
