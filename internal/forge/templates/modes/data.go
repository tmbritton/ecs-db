package modes

import "github.com/tmbritton/ecs-db/internal/schema"

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
	// Machines are the behaviour machine IDs the project resolved, for the
	// binding dropdowns.
	Machines []string
	// Problem is why the last edit was refused, if it was. Rendered in the
	// editor rather than only returned as a status code: an edit that vanishes
	// with no explanation teaches you that the control is broken.
	Problem string
}
