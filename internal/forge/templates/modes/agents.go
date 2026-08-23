package modes

import (
	"fmt"
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
	return strings.TrimSuffix(fileName(path), ".json")
}

func machineID(def *MachineDefinition) string {
	if def == nil {
		return ""
	}
	return def.ID
}

func modOptions(mods []project.Mod) []components.Option {
	out := make([]components.Option, 0, len(mods)+1)
	out = append(out, components.Option{Value: "", Label: "choose a mod…"})
	for _, m := range mods {
		out = append(out, components.Option{Value: m.Name, Label: m.Name})
	}
	return out
}

// newMachineAction posts the chosen mod along with the proposed id.
func newMachineAction(id string) string {
	return "@post('/forge/agents/machine?add=" + urlValue(id) + "&mod=' + encodeURIComponent(evt.target.value))"
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

// ManifestState is what the CONTEXT MANIFEST panel can be showing.
//
// Three states, and the first two are the reason the type exists. The engine
// works a manifest out only when a machine validates completely, so an empty
// manifest means either "this machine seeds nothing" or "nobody knows what this
// machine seeds" — and the panel that answers "what does this attach on spawn"
// must never give the first answer when the truth is the second.
type ManifestState int

const (
	// ManifestUnavailable is a machine that does not validate, so the engine
	// never got as far as mapping its context.
	ManifestUnavailable ManifestState = iota
	// ManifestEmpty is a machine that validates and declares no context.
	ManifestEmpty
	// ManifestMapped is the ordinary case.
	ManifestMapped
)

func manifestState(data Data) ManifestState {
	if !data.Inspection.Computed {
		return ManifestUnavailable
	}
	if len(machineSeeds(data)) == 0 {
		return ManifestEmpty
	}
	return ManifestMapped
}

// machineSeeds reads the selected machine's context in authored order.
//
// Seed is ENTS's type, deliberately: the two panels answer the same question
// about the same data, and a second view type would be a second place for
// "which component declares this" to be decided.
//
// There is no unresolved-component state here, and there is one in ENTS. The
// difference is real rather than an omission: ENTS renders a manifest computed
// against schema.json as it was at startup, which goes stale as the schema is
// edited, while this one is recomputed against the schema being edited on every
// render. A key naming no component is not a stale entry here — it is a
// validation error, so the machine does not validate and the panel is already
// in its unavailable state, with that key named in the problem list beside it.
func machineSeeds(data Data) []Seed {
	def := data.Machine
	if def == nil {
		return nil
	}
	keys := def.ContextOrder
	if len(keys) == 0 {
		for k := range def.Context {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	out := make([]Seed, 0, len(keys))
	for _, k := range keys {
		if _, ok := def.Context[k]; !ok {
			continue
		}
		out = append(out, Seed{Key: k, Value: def.Context[k], Component: data.Inspection.Manifest[k]})
	}
	return out
}

// validityLine is the readout the prototype puts at the top right of the
// canvas: what saving this machine would do.
func validityLine(data Data) string {
	if data.Inspection.Computed {
		return "✓ valid · saves & hot-swaps into the running game"
	}
	n := len(data.Inspection.Errors)
	if n == 0 {
		// The zero value: not computed and no reason given. No render reaches
		// it — the header is inside the editor, which needs a selected machine,
		// and the server fills an inspection for every one of those, reporting
		// its own failure as a problem if Inspect refused. It is here so the
		// function is total rather than answering "0 problems", and the test
		// says so.
		return "not checked"
	}
	if n == 1 {
		return "✕ 1 problem — the engine will not load this machine"
	}
	return fmt.Sprintf("✕ %d problems — the engine will not load this machine", n)
}

// machineProblems is what is wrong with the machine as a whole.
//
// Only that. Until Story 8 this was every error the engine gave, flattened into
// one list with "state <id>: " glued on the front — which was the honest answer
// while nothing could place them, and is the wrong one now that everything can.
// An error about a state is rendered against that state, on the canvas and in
// the rail; what is left here is what belongs to no state, plus anything naming
// a state the canvas did not draw, because a message with nowhere to go still
// has to go somewhere.
//
// Blocking, all of them: Session.Save refuses a machine that does not validate,
// so the save of this one is genuinely refused — which is what
// validation.Problem.Blocking means and the only thing it means.
func machineProblems(data Data) []components.Problem { return data.Problems.Machine }

// machineSource is the mod and file the selected machine came from.
//
// Not the absolute path, which is what the skeleton showed: it is the
// developer's filesystem, it is long enough to push the rest of the header off
// the line, and the half of it that answers a question — which mod owns this —
// is the half a path buries in the middle.
func machineSource(data Data) string {
	name := fileName(data.SelectedMachine)
	if mod := machineMod(data, data.SelectedMachine); mod != "" {
		return mod + "/" + name
	}
	return name
}

// machineMod is the mod a path resolved from, or "" for a path the resolved set
// does not know — a machine stranded by a re-resolve, which still has to render
// as something.
func machineMod(data Data, path string) string {
	for _, m := range data.Machines {
		if m.Path == path {
			return m.Mod
		}
	}
	return ""
}

func fileName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// overrideTitle says which mod won, in the tag itself. "override" alone tells
// you something happened and not what: the winning mod is on the row, and the
// tag names it again so the two cannot be read apart.
func overrideTitle(m project.Machine) string {
	return "shadows an earlier mod's machine of the same id — " + m.Mod + " wins"
}

// strandedProblem reports whether a problem is about work the session is still
// holding, which is the only kind that has anything left to do about it.
func strandedProblem(data Data, p project.Problem) bool {
	return data.StrandedMachines[p.Path]
}
