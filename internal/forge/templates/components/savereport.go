package components

import (
	"path/filepath"

	"github.com/tmbritton/ecs-db/internal/forge/savereport"
)

type SaveReportsProps struct {
	Reports []SaveReportView
}

// SaveReportView is a report plus the two ways out of a conflict, for the file
// it is about.
//
// The actions are computed rather than fixed, which they were not: every
// report's "Use theirs" and "Keep mine" posted to /forge/schema/, whatever file
// the conflict was on. A map or a machine conflict therefore offered two
// buttons that operated on schema.json — "Use theirs" discarding unsaved schema
// work to resolve a conflict somewhere else entirely. Found by review of Epic
// 15 Story 2, and older than that story.
type SaveReportView struct {
	savereport.Report
	// ReloadAction takes what is on disk; OverwriteAction keeps what Forge
	// holds. Empty for a report whose owner cannot be worked out, which renders
	// no buttons rather than the wrong ones.
	ReloadAction    string
	OverwriteAction string
}

// saveReportClass colours the outcome. Green only for a save that reached a
// listening engine; amber for one that landed on disk with nothing running,
// because that is worth noticing but is not a problem; red for the two that
// did not write.
func saveReportClass(o savereport.Outcome) string {
	switch o {
	case savereport.OutcomeSaved:
		return "save-report--ok"
	case savereport.OutcomeSavedNoEngine:
		return "save-report--idle"
	case savereport.OutcomeRejected, savereport.OutcomeConflict:
		return "save-report--bad"
	default:
		return ""
	}
}

// baseName keeps the report readable: the full path is on the element as a
// data attribute for anything that needs to match on it.
func baseName(path string) string { return filepath.Base(path) }
