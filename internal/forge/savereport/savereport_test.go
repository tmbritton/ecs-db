package savereport

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/editable"
	"github.com/tmbritton/ecs-db/internal/forge/status"
)

func TestObserve(t *testing.T) {
	tests := []struct {
		name         string
		saveErr      error
		engine       status.State
		wantOutcome  Outcome
		wantProblems []string
	}{
		{
			name:        "written, and a game is listening",
			engine:      status.StateConnected,
			wantOutcome: OutcomeSaved,
		},
		{
			// Not a failure. Forge is useful with no game running; that is the
			// normal case while authoring, and reporting it as an error would
			// train people to ignore the readout.
			name:        "written, nothing listening",
			engine:      status.StateOffline,
			wantOutcome: OutcomeSavedNoEngine,
		},
		{
			name:        "written, but the database is stale",
			engine:      status.StateMismatch,
			wantOutcome: OutcomeSavedNoEngine,
		},
		{
			name:        "written, schema unreadable",
			engine:      status.StateSchemaUnreadable,
			wantOutcome: OutcomeSavedNoEngine,
		},
		{
			name:         "refused as invalid",
			saveErr:      errors.New("component Position: bad"),
			engine:       status.StateConnected,
			wantOutcome:  OutcomeRejected,
			wantProblems: []string{"component Position: bad"},
		},
		{
			name:        "refused because the file changed underneath",
			saveErr:     &editable.ConflictError{Path: "/p/schema.json"},
			engine:      status.StateConnected,
			wantOutcome: OutcomeConflict,
		},
		{
			// ValidateMachine returns every error for a reason: a
			// first-failure-only report sends someone round the loop once per
			// mistake.
			name: "every validation error, not the first",
			saveErr: errors.Join(
				errors.New("state idle: unknown action a"),
				errors.New("state idle: unknown guard g"),
				errors.New("initial state nowhere is not a child state"),
			),
			engine:      status.StateConnected,
			wantOutcome: OutcomeRejected,
			wantProblems: []string{
				"state idle: unknown action a",
				"state idle: unknown guard g",
				"initial state nowhere is not a child state",
			},
		},
		{
			// The shape editable.Save actually produces: its own wrap around
			// whatever the codec's Validate returned. The previous version of
			// this case passed a bare join and so never exercised the unwrap
			// branch at all — deleting that branch left every test green.
			name: "a join inside a wrap, as editable.Save produces",
			saveErr: fmt.Errorf("editable: %s is not valid: %w", "/p/goblin.json",
				errors.Join(errors.New("first"), errors.New("second"))),
			engine:       status.StateOffline,
			wantOutcome:  OutcomeRejected,
			wantProblems: []string{"first", "second"},
		},
		{
			// Nested joins, which agent.ValidateMachineError can produce once
			// a caller composes validators. Flattening has to recurse.
			name: "a join inside a join",
			saveErr: errors.Join(
				errors.New("outer"),
				errors.Join(errors.New("inner one"), errors.New("inner two")),
			),
			engine:       status.StateConnected,
			wantOutcome:  OutcomeRejected,
			wantProblems: []string{"outer", "inner one", "inner two"},
		},
		{
			// One problem, not "1 problems".
			name:         "a single problem",
			saveErr:      errors.New("only this"),
			engine:       status.StateConnected,
			wantOutcome:  OutcomeRejected,
			wantProblems: []string{"only this"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Observe("/p/file.json", tt.saveErr, tt.engine)

			if got.Outcome != tt.wantOutcome {
				t.Errorf("Outcome = %v, want %v", got.Outcome, tt.wantOutcome)
			}
			if got.Path != "/p/file.json" {
				t.Errorf("Path = %q", got.Path)
			}
			if len(tt.wantProblems) != len(got.Problems) {
				t.Fatalf("Problems = %v, want %v", got.Problems, tt.wantProblems)
			}
			for i, want := range tt.wantProblems {
				if !strings.Contains(got.Problems[i], want) {
					t.Errorf("Problems[%d] = %q, want it to contain %q", i, got.Problems[i], want)
				}
			}
		})
	}
}

// The zero Report means "no save has happened", so a caller rendering one
// before anything was saved shows nothing rather than claiming success.
func TestReport_ZeroValueIsSilent(t *testing.T) {
	var r Report
	if r.Outcome != OutcomeNone {
		t.Errorf("zero Outcome = %v, want OutcomeNone", r.Outcome)
	}
	if r.Rendered() {
		t.Error("a zero Report should not render")
	}
}

// The copy is the whole point of the story: it must be precise about what
// Forge actually knows.
func TestReport_Summary(t *testing.T) {
	tests := []struct {
		name    string
		report  Report
		want    string
		notWant []string
	}{
		{
			name:   "saved with a game running",
			report: Report{Outcome: OutcomeSaved},
			want:   "hot-reload live",
			// Forge and the engine are separate processes. Forge validates the
			// same way the engine will and reports that; it cannot observe the
			// reload, and claiming to would be a stronger claim than it can
			// make.
			notWant: []string{"engine accepted", "reloaded"},
		},
		{
			name:    "saved with nothing listening",
			report:  Report{Outcome: OutcomeSavedNoEngine},
			want:    "no game running",
			notWant: []string{"failed", "error"},
		},
		{
			name:   "rejected, several problems",
			report: Report{Outcome: OutcomeRejected, Problems: []string{"a", "b"}},
			want:   "2 problems",
		},
		{
			// "1 problems" is the kind of thing that ships because the only
			// fixture had two.
			name:    "rejected, one problem",
			report:  Report{Outcome: OutcomeRejected, Problems: []string{"a"}},
			want:    "1 problem",
			notWant: []string{"1 problems"},
		},
		{
			name:   "conflict",
			report: Report{Outcome: OutcomeConflict},
			want:   "changed on disk",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.report.Summary()
			if !strings.Contains(got, tt.want) {
				t.Errorf("Summary() = %q, want it to contain %q", got, tt.want)
			}
			for _, no := range tt.notWant {
				if strings.Contains(strings.ToLower(got), no) {
					t.Errorf("Summary() = %q, must not claim %q", got, no)
				}
			}
		})
	}
}

// Reports are per file: saving one must not clear the error shown for another.
func TestSet_IsPerFile(t *testing.T) {
	set := NewSet()
	if got := set.All(); len(got) != 0 {
		t.Errorf("All() = %v on a new set, want none", got)
	}

	set.Record(Observe("/p/a.json", errors.New("bad"), status.StateConnected))
	set.Record(Observe("/p/b.json", nil, status.StateConnected))

	all := set.All()
	if len(all) != 2 {
		t.Fatalf("All() = %v, want two reports", all)
	}
	// Sorted by path, so rendering is stable.
	if all[0].Path != "/p/a.json" || all[1].Path != "/p/b.json" {
		t.Errorf("All() is not ordered by path: %v", all)
	}
	if all[0].Outcome != OutcomeRejected {
		t.Errorf("a.json = %v, want its rejection preserved", all[0].Outcome)
	}

	// Saving b again leaves a's failure alone.
	set.Record(Observe("/p/b.json", nil, status.StateOffline))
	all = set.All()
	if len(all) != 2 || all[0].Outcome != OutcomeRejected {
		t.Errorf("saving one file cleared another's report: %v", all)
	}
	if all[1].Outcome != OutcomeSavedNoEngine {
		t.Errorf("b.json = %v, want the newer outcome", all[1].Outcome)
	}
}

func TestSet_Clear(t *testing.T) {
	set := NewSet()
	set.Record(Observe("/p/a.json", errors.New("bad"), status.StateConnected))
	set.Clear("/p/a.json")
	if got := set.All(); len(got) != 0 {
		t.Errorf("All() = %v after Clear, want none", got)
	}
}
