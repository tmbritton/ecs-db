package modes

import (
	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
	"github.com/tmbritton/ecs-db/internal/forge/machines"
	"github.com/tmbritton/ecs-db/internal/forge/machinevalidation"
	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
	"github.com/tmbritton/ecs-db/internal/forge/maps"
	"github.com/tmbritton/ecs-db/internal/forge/mapvalidation"
	"github.com/tmbritton/ecs-db/internal/forge/migration"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/forge/tilelinks"
	"github.com/tmbritton/ecs-db/internal/forge/usage"
	"github.com/tmbritton/ecs-db/internal/forge/validation"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

// Data is everything a mode needs to render.
//
// One struct rather than a parameter per mode: the shell hands the same value
// to whichever mode is open, so it stays free of mode-specific knowledge — the
// property Epic 10 Story 5 established and the reason the registry is a lookup
// rather than a switch. A mode ignores what it does not use.
type Data struct {
	// PageID identifies this page to the server across the requests it makes,
	// so that what it has selected is its own rather than every tab's. Issued
	// on render; see server.pageStates.
	PageID string
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
	// Maps are the project's Tiled maps, the configured one first — the tab
	// strip is made of these.
	Maps []maps.Map
	// SelectedMap is the map on screen, by path, resolved against what exists.
	SelectedMap string
	// DirtyMaps are the maps with unsaved work, by path.
	//
	// Computed for every mode, not only MAP: the save footer is in the shell
	// and is on screen everywhere, so a footer that only knew about unsaved
	// maps while MAP happened to be open is a way to lose work.
	DirtyMaps map[string]bool
	// MapProblems is why a map the project has is not open, or is open and
	// incomplete — a file that will not parse, a .tmj, a tileset that will not
	// resolve. Shown rather than hidden, for the reason project.Problem exists:
	// the broken file is the one somebody came here to fix.
	MapProblems []project.Problem
	// Canvas is the selected map laid out: every cell placed, the layer rows,
	// the palette and one line per reason a cell could not be drawn.
	Canvas mapcanvas.Canvas
	// The working map's identity and all current engine/project validation
	// findings, attributed to the thing that caused each of them.
	MapID         string
	MapValidation mapvalidation.Report
	// MapPreview is the detached, tileset-resolved working map used to check a
	// candidate Tile link before the inspector offers its action.
	MapPreview *tiled.Map
	// Objects are parsed from the same working map as Canvas, with their group
	// and stable id. A selection is URL-addressed and never owns another copy.
	ObjectGroups  []tiled.ObjectGroup
	SelectedSpawn *tiled.Object
	MissingSpawn  int
	// SelectedTile describes one authored layer cell, distinct from the tile in
	// hand in MapSignals. TileProblem explains a selected but invalid cell.
	SelectedTile *tilelinks.Inspection
	TileProblem  string
	// A duplicated ID is not a selection: ID-addressed edit routes cannot
	// distinguish its claimants. Keep their marks visible, but no inspector
	// action may be wired to one arbitrarily.
	AmbiguousSpawn int
	SpawnErrors    []string
	SpawnWarnings  []string
	// MapMenu is the one MAP context menu currently open on this server, if its
	// target belongs to the map this page is showing.
	MapMenu MapMenu
	// MapView is the URL's view state — which map, which layers Forge is
	// hiding, which tile is selected — and is what every link in the mode is
	// built from, so none of them can drop part of it.
	MapView MapView
	// ConfiguredMap is the map game.toml names, or empty for a project that
	// declares none.
	//
	// What tells two states apart that both leave Maps empty: a project without
	// a level, and one whose configured map is missing or will not parse.
	// Answering both with "this project declares no map" told the second group
	// to add a section they already have.
	ConfiguredMap string
	// HasMaps is false when no project could be opened, on the same terms as
	// HasSession. A project that opened and declares no [map] has this true and
	// Maps empty, which is a different state and reads differently.
	HasMaps bool
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
	// Problems is every reason the engine would refuse the selected machine,
	// grouped onto the node, the edge or the machine that caused it.
	//
	// Grouped rather than listed: a message about a state scrolled out of view
	// is a message nobody reads, so the canvas marks what carries one and the
	// rail says what it is.
	Problems machinevalidation.Report
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

// MapMenu is one right-click target and its identity at open time. It is kept
// by the server, not by the browser: the rendered action and the target it
// changes must agree across a page-stream patch.
type MapMenu struct {
	Open                   bool
	Kind, Path             string
	Token                  string // identifies this opening, even after the same target is reopened
	Layer, LayerID         int
	ViewID                 int // stable only when the Tiled ID is unique
	LayerName              string
	CanMoveUp, CanMoveDown bool
	CanDelete              bool
	ObjectID               int
	X, Y                   float64 // viewport coordinates for a fixed-position menu
	CellX, CellY           int
	Tile                   tiled.Tile
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
