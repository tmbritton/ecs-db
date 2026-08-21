package components

import (
	"path/filepath"

	"github.com/tmbritton/ecs-db/internal/forge/savereport"
)

type SaveReportsProps struct {
	Reports []savereport.Report
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
