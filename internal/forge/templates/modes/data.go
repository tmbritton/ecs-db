package modes

import (
	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
	"github.com/tmbritton/ecs-db/internal/forge/machines"
	"github.com/tmbritton/ecs-db/internal/forge/migration"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/forge/usage"
	"github.com/tmbritton/ecs-db/internal/forge/validation"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// Data is everything a mode needs to render.
//
// One struct rather than a parameter per mode: the shell hands the same value
// to whichever mode is open, so it stays free of mode-specific knowledge — the
// property Epic 10 Story 5 established and the reason the registry is a lookup
// rather than a switch. A mode ignores what it does not use.
type Data struct {
	// Schema is the working value from the editing session, already a copy —
	// Session.Read hands out a deep one, so a template cannot reach the
	// session through it.
	Schema schema.DatabaseSchema
	// HasSession is false when no project could be opened. The modes stay
	// readable in that state rather than the editor refusing to start.
	HasSession bool
	// Selected is the component or entity type the URL names, resolved to
	// something that exists.
	Selected string
	// Machines are the behaviour machines the project resolved, for the binding
	// dropdowns and for ENTS's read-only view of a bound machine's context.
	Machines []project.Machine
	// MachineProblems is why any behaviour file did not resolve, which is what
	// turns "that machine is not loaded" into a sentence naming the reason.
	MachineProblems []project.Problem
	// Selected machine, by path. A machine's id lives inside its file and can
	// be edited, so the path is what a selection can safely be keyed on.
	SelectedMachine string
	// Dirty machines, by path, so the list can mark what is unsaved.
	DirtyMachines map[string]bool
	// ReformatMachines is the subset of those whose *content* matches the file
	// and whose difference is only its layout — a machine authored in another
	// whitespace style is unsaved the moment Forge opens it, with nobody having
	// edited anything.
	ReformatMachines map[string]bool
	// MachineMods are the mods a new machine could be created in.
	MachineMods []project.Mod
	// Machine is the selected machine's working value, already a copy.
	Machine *agent.MachineDefinition
	// Inspection is what the engine says about that working value right now:
	// every reason it would be refused, and the context manifest when it could
	// be worked out at all.
	//
	// It cannot be read off Machine, which is parse-derived and carries no
	// manifest, nor off the resolved Machines, which describe the files as last
	// read rather than what is being edited.
	Inspection machines.Inspection
	// Chart is the selected machine drawn: a node per state, an edge per
	// transition, positioned, with the URL's selection already resolved
	// against what it drew.
	//
	// Built from Machine and from nothing else. The canvas never consults the
	// inspection: a machine that does not validate is precisely the one
	// someone opened the editor to fix, and a chart that refused to draw it
	// would send them back to a text editor at the moment the tool is most
	// use.
	Chart chart.Chart
	// Actions is every action this project's engine registers, sorted by name
	// and each with the description and parameter schema the registry carries.
	//
	// The catalogue rather than a list: an action name that is not registered
	// makes a machine the engine refuses to load, and offering exactly what the
	// engine accepts makes that mistake unreachable rather than reported.
	Actions []agent.ActionMeta
	// Guards is every guard this project's engine registers, on exactly the
	// same terms as Actions — including the gating, so a mapless project does
	// not offer inLineOfSight.
	Guards []agent.GuardMeta
	// StateTargets is every state in the selected machine, as a dotted path in
	// authored order. What the transition inspector's target dropdown is made
	// of: offering only these makes "transition target is not a known state"
	// unreachable rather than merely reported.
	StateTargets []string
	// EventNames is every event the selected machine already reacts to, for the
	// event field's suggestions. Suggestions only — an event name is authored
	// rather than registered, so nothing can validate one.
	EventNames []string
	// SelectedState is the state the canvas selection names, or nil. Resolved
	// exactly, by the same rule the mutations use.
	SelectedState *agent.StateNode
	// SelectedStateWarning is what deleting the selected state would break, in
	// words — the same sentence the canvas menu uses, from the same place.
	SelectedStateWarning string
	// CanvasMenu is the right-click menu the statechart has open, if any.
	CanvasMenu CanvasMenu
	// CanvasMenuWarning is what deleting the state that menu is about would
	// break, in words. Computed by the server, because only the session can
	// say which transitions target it.
	CanvasMenuWarning string
	// StrandedMachines are held with unsaved work but no longer resolve, so
	// they are in no list and nothing can reach them except the problem panel.
	StrandedMachines map[string]bool
	// NewMachineID is an id nothing has claimed, for the create control. The
	// server proposes it rather than the template guessing: what counts as
	// claimed includes files that do not load, which only the session can see.
	NewMachineID string
	// HasMachines is false when no project could be opened, on the same terms
	// as HasSession.
	HasMachines bool
	// ProblemField is the control that refusal was about, so the panel can
	// report it under the field where the mistake was made. Empty when the
	// refusal belongs to no one control.
	ProblemField string
	// Problem is why the last edit was refused, if it was. Rendered in the
	// editor rather than only returned as a status code: an edit that vanishes
	// with no explanation teaches you that the control is broken.
	Problem string
	// Migration is what the engine would do to the database on its next start.
	// Recomputed per render and never cached: it depends on a database another
	// process is writing, and a stale migration warning is worse than a slow
	// one.
	Migration migration.Preview
	// Counts is how many entities of each type exist right now. Read from the
	// database and refreshed on the page stream, so it is never a design fact
	// even when it looks like one.
	Counts usage.Counts
	// Validation is what is wrong with the schema right now, from the engine's
	// own checks. Recomputed per render rather than on save: finding out at the
	// end of a train of thought is finding out too late, which is the whole
	// point of the story that added it.
	Validation validation.Report
	// Confirming is true when a save was held back because it would destroy
	// data, and the confirmation is on screen. The statements it lists come
	// from Migration, not from a copy taken when the save was attempted.
	Confirming bool
}

// CanvasMenu is the statechart's right-click menu: what it is about, and where
// the pointer was when it was asked for.
//
// One type for both kinds of target, because a menu is one thing on screen and
// two nearly-identical structs would need a rule about which one is live. Kind
// says which of the rest mean anything: "state", "edge" or "canvas".
type CanvasMenu struct {
	Kind  string
	State string // Kind == "state"
	Edge  string // Kind == "edge": the chart's edge id, for the DOM
	// The parts of that edge, because the id joins them with bars and both a
	// state name and an event name may contain one.
	From     string
	EdgeKind string
	Event    string
	Index    string
	X, Y     float64
	Open     bool
}
