package templates

import (
	"strconv"

	"github.com/a-h/templ"

	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
	"github.com/tmbritton/ecs-db/internal/forge/validation"
)

// SchemaFooter builds the save footer for the file both SCHEMA and ENTS edit.
//
// The actions post to the session endpoints. Both buttons are disabled when
// clean, which the primitive already supports and nothing had used until now —
// there is nothing to commit and nothing to throw away.
func SchemaFooter(file string, dirty bool, report validation.Report) templ.Component {
	return components.SaveFooter(components.SaveFooterProps{
		Dirty:         dirty,
		File:          file,
		Blocked:       report.Blocked(),
		BlockedReason: blockedReason(report),
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
