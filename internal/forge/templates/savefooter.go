package templates

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
	"github.com/tmbritton/ecs-db/internal/forge/validation"
)

// SchemaFooter builds the save footer for the file both SCHEMA and ENTS edit.
//
// The actions post to the session endpoints. Both buttons are disabled when
// clean, which the primitive already supports and nothing had used until now —
// there is nothing to commit and nothing to throw away.
func SchemaFooter(file string, dirty bool, report validation.Report, elsewhere Elsewhere) templ.Component {
	return components.SaveFooter(components.SaveFooterProps{
		Dirty:         dirty,
		File:          file,
		Blocked:       report.Blocked(),
		BlockedReason: blockedReason(report),
		Elsewhere:     elsewhere.describe(),
		SaveAction:    "@post('/forge/schema/save')",
		// Confirmed, because it throws away work with no undo. It also used to
		// be the only control offered after a conflict, which made an
		// accidental click delete the edit and leave the footer claiming
		// "saved" over a file it had not written.
		DiscardAction: "confirm('Discard unsaved changes to " + file + "?') && @post('/forge/schema/discard')",
	})
}

// NoFooter renders nothing, for a page with no editing session — a project that
// failed to open. An empty element still renders so the SSE patch has a target.
func NoFooter() templ.Component { return templ.NopComponent }

// blockedReason is the one-line summary beside a disabled Save.
//
// A count, not the first message: the messages render against the controls
// that caused them, and repeating one of them here would make the footer look
// like the place to read them — with the other two invisible.
func blockedReason(report validation.Report) string {
	errors, _ := report.Counts()
	if errors == 0 {
		return ""
	}
	if errors == 1 {
		return "1 problem to fix first"
	}
	return strconv.Itoa(errors) + " problems to fix first"
}

// MachinesFooter builds the save footer for AGENTS mode.
//
// Blocked by validity as of Epic 13 Story 8, which reverses what Story 2 chose
// and what this comment used to say. The reversal is recorded rather than
// erased, because the argument against it has not gone away.
//
// Story 2's reasoning: an invalid schema.json stops the engine starting, so
// blocking its save protects the project; an invalid *machine* is rejected by
// the loader on its own, so the blast radius is one file. Session.Save is still
// built that way — it writes every dirty machine it can and reports the ones it
// could not, per file.
//
// Story 8 asks for the opposite in as many words ("an error blocks the save;
// the footer says how many"), and the case for it is that a Save which silently
// declines one of five files is a save someone believes happened. The cost is
// real and worth naming: with one machine half-built, a *different* machine's
// finished work cannot be written, and the only way out is to fix it or to
// Discard — which discards every machine, not the broken one. Per-machine
// discard exists only for stranded files.
//
// If that trade turns out to be wrong, the fix is a per-machine save or a
// per-machine discard, not a return to a Save that half-works in silence.
func MachinesFooter(file string, dirty bool, title string, elsewhere Elsewhere, invalid map[string]int) templ.Component {
	return components.SaveFooter(components.SaveFooterProps{
		Dirty:         dirty,
		File:          file,
		Title:         title,
		Elsewhere:     elsewhere.describe(),
		Blocked:       len(invalid) > 0,
		BlockedReason: machinesBlockedReason(invalid),
		SaveAction:    "@post('/forge/agents/save')",
		DiscardAction: "confirm('Discard unsaved changes to every machine?') && " +
			"@post('/forge/agents/discard')",
	})
}

// machinesBlockedReason is the one line beside a disabled Save.
//
// Across every machine the save would write, not just the one on screen: the
// button saves all of them, so a footer reporting only the selected machine
// would offer a Save that fails on a file in another tab of the same list. The
// problems themselves render against what caused them, on the canvas of the
// machine they belong to.
func machinesBlockedReason(invalid map[string]int) string {
	if len(invalid) == 0 {
		return ""
	}
	problems := 0
	for _, n := range invalid {
		problems += n
	}
	where := "1 machine"
	if len(invalid) > 1 {
		where = strconv.Itoa(len(invalid)) + " machines"
	}
	if problems == 1 {
		return "1 problem in " + where + " — the engine would refuse it"
	}
	return strconv.Itoa(problems) + " problems in " + where + " — the engine would refuse them"
}

// MapFooter builds the save footer for MAP mode.
//
// Not blocked by validity, and that is a deliberate difference from
// MachinesFooter. A map is edited by painting: half a stroke is not a state the
// UI can be in, and the things that make a map unloadable — a gid no tileset
// holds, a spawn whose class is not an entity type — are Story 9's to report
// against the cell and the spawn that caused them. Blocking Save on them would
// mean an author who pasted a tileset in the wrong order could not write the
// file they need to fix by hand.
// path names the map in every action, and is not optional. The footer follows
// the selection, so a Save that posted without one would write whichever map
// the server picked as a default — which is the configured map, not the one on
// screen. Found by review: the confirm dialog named the map you were looking at
// while the request discarded a different one's work.
func MapFooter(path, file string, dirty bool, elsewhere Elsewhere) templ.Component {
	q := "?map=" + url.QueryEscape(path)
	return components.SaveFooter(components.SaveFooterProps{
		Dirty:      dirty,
		File:       file,
		Title:      path,
		Elsewhere:  elsewhere.describe(),
		SaveAction: "@post('/forge/map/save" + q + "')",
		DiscardAction: "confirm('Discard unsaved changes to " + file + "?') && " +
			"@post('/forge/map/discard" + q + "')",
	})
}

// Elsewhere is unsaved work the footer on screen cannot save.
//
// One Save button does one thing and the footer follows the mode, so without
// this, switching to AGENTS with a dirty schema.json showed "✓ saved" and a
// disabled Save — a way to lose work rather than a wording miss. It was two
// parameters on two footers until maps made it three sources and three
// footers; a struct is what stops the next mode adding a fourth positional
// argument to each of them.
//
// Each footer passes what it cannot save and leaves out what it can.
type Elsewhere struct {
	Schema   bool
	Machines int
	Maps     int
}

// describe is the one line the footer shows, or "" when everything else is
// saved. Joined rather than truncated to the first: two kinds of unsaved work
// elsewhere is exactly when knowing about only one is worst.
func (e Elsewhere) describe() string {
	var parts []string
	if e.Schema {
		parts = append(parts, "unsaved changes to schema.json in SCHEMA")
	}
	switch {
	case e.Machines == 1:
		parts = append(parts, "1 unsaved machine in AGENTS")
	case e.Machines > 1:
		parts = append(parts, strconv.Itoa(e.Machines)+" unsaved machines in AGENTS")
	}
	switch {
	case e.Maps == 1:
		parts = append(parts, "1 unsaved map in MAP")
	case e.Maps > 1:
		parts = append(parts, strconv.Itoa(e.Maps)+" unsaved maps in MAP")
	}
	return strings.Join(parts, " · ")
}
