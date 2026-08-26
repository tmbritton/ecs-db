package components

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/savereport"
)

func TestSaveReports(t *testing.T) {
	tests := []struct {
		name    string
		reports []SaveReportView
		want    []string
		notWant []string
	}{
		{
			// The container must exist even with nothing in it, or the first
			// patch has no element to morph.
			name: "no reports still renders the patch target",
			want: []string{`id="save-reports"`},
		},
		{
			name: "saved with a game listening",
			reports: []SaveReportView{
				{Report: savereport.Report{Path: "/p/schema.json", Outcome: savereport.OutcomeSaved}},
			},
			want:    []string{"save-report--ok", "schema.json", "hot-reload live"},
			notWant: []string{"save-report--bad", "not saved"},
		},
		{
			name: "saved with nothing listening is not an error",
			reports: []SaveReportView{
				{Report: savereport.Report{Path: "/p/schema.json", Outcome: savereport.OutcomeSavedNoEngine}},
			},
			want:    []string{"save-report--idle", "no game running"},
			notWant: []string{"save-report--bad"},
		},
		{
			// Every problem, not the first.
			name: "rejected lists all the problems",
			reports: []SaveReportView{
				{Report: savereport.Report{
					Path:     "/p/goblin.json",
					Outcome:  savereport.OutcomeRejected,
					Problems: []string{"unknown action a", "unknown guard g", "bad initial"},
				}},
			},
			want: []string{
				"save-report--bad", "goblin.json", "not saved",
				"unknown action a", "unknown guard g", "bad initial",
			},
		},
		{
			name: "conflict",
			reports: []SaveReportView{
				{Report: savereport.Report{Path: "/p/schema.json", Outcome: savereport.OutcomeConflict}},
			},
			want:    []string{"save-report--bad", "changed on disk"},
			notWant: []string{"save-report--ok"},
		},
		{
			// One file's failure must survive another file's success.
			name: "two files, independent outcomes",
			reports: []SaveReportView{
				{Report: savereport.Report{Path: "/p/a.json", Outcome: savereport.OutcomeRejected, Problems: []string{"broken"}}},
				{Report: savereport.Report{Path: "/p/b.json", Outcome: savereport.OutcomeSaved}},
			},
			want: []string{"save-report--bad", "save-report--ok", "a.json", "b.json", "broken"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, SaveReports(SaveReportsProps{Reports: tt.reports}))
			assertOutput(t, got, tt.want, tt.notWant)
		})
	}
}

// The full path is what a caller matches on; the visible text is the base name.
func TestSaveReports_CarriesTheFullPath(t *testing.T) {
	got := render(t, SaveReports(SaveReportsProps{Reports: []SaveReportView{
		{Report: savereport.Report{Path: "/deep/nested/schema.json", Outcome: savereport.OutcomeSaved}},
	}}))
	if !strings.Contains(got, `data-file="/deep/nested/schema.json"`) {
		t.Errorf("the full path is not on the element:\n%s", got)
	}
}

// It updates without a page load, so a screen reader has to be told.
func TestSaveReports_IsALiveRegion(t *testing.T) {
	got := render(t, SaveReports(SaveReportsProps{}))
	if !strings.Contains(got, `role="status"`) {
		t.Errorf("the report region is not announced:\n%s", got)
	}
}

// An unknown outcome must not be styled as success.
func TestSaveReportClass_UnknownOutcomeIsNotSuccess(t *testing.T) {
	for _, o := range []savereport.Outcome{savereport.OutcomeNone, savereport.Outcome(99), savereport.Outcome(-1)} {
		if got := saveReportClass(o); got == "save-report--ok" {
			t.Errorf("outcome %v renders as success", o)
		}
	}
}
