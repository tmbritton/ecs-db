package templates

import (
	"github.com/a-h/templ"

	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
)

// SchemaFooter builds the save footer for the file both SCHEMA and ENTS edit.
//
// The actions post to the session endpoints. Both buttons are disabled when
// clean, which the primitive already supports and nothing had used until now —
// there is nothing to commit and nothing to throw away.
func SchemaFooter(file string, dirty bool) templ.Component {
	return components.SaveFooter(components.SaveFooterProps{
		Dirty:      dirty,
		File:       file,
		SaveAction: "@post('/forge/schema/save')",
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
