package modes

import (
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
