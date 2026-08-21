// Package savereport says what became of a save.
//
// Being precise here is the whole job. Forge and the engine are separate
// processes, and the architecture deliberately keeps Forge read-only on the
// database, so Forge cannot observe a reload. What it can do is run the same
// validation the engine will and report that. "The engine accepted this" is a
// stronger claim than it can make; "this would validate, and something is
// listening" is the honest one.
package savereport

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/tmbritton/ecs-db/internal/forge/editable"
	"github.com/tmbritton/ecs-db/internal/forge/status"
)

// Outcome is what happened to a save. The zero value means no save has been
// attempted, so a caller rendering a fresh Report shows nothing rather than
// claiming success.
type Outcome int

const (
	OutcomeNone Outcome = iota
	// OutcomeRejected: the value did not validate and nothing was written.
	OutcomeRejected
	// OutcomeConflict: the file changed on disk since it was opened.
	OutcomeConflict
	// OutcomeSavedNoEngine: written, but nothing is listening. Not a failure —
	// Forge is useful with no game running, and that is the normal case while
	// authoring.
	OutcomeSavedNoEngine
	// OutcomeSaved: written, and a compatible database is present, so the
	// engine's watcher will pick it up.
	OutcomeSaved
)

// Report is the outcome of one save of one file.
type Report struct {
	Path     string
	Outcome  Outcome
	Problems []string
}

// Rendered reports whether there is anything to show.
func (r Report) Rendered() bool { return r.Outcome != OutcomeNone }

// Summary is the one-line message. See the package comment for why the
// connected wording stops short of claiming the engine reloaded anything.
func (r Report) Summary() string {
	switch r.Outcome {
	case OutcomeSaved:
		return "saved · hot-reload live"
	case OutcomeSavedNoEngine:
		return "saved · no game running"
	case OutcomeConflict:
		return "not saved · changed on disk since you opened it"
	case OutcomeRejected:
		if n := len(r.Problems); n > 0 {
			return fmt.Sprintf("not saved · %d problem%s", n, plural(n))
		}
		return "not saved"
	default:
		return ""
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Observe turns the result of a save attempt into a report.
//
// The engine state comes from internal/forge/status, the same question the menu
// bar readout already answers. Answering it a second way here would be worse
// than not answering it at all.
func Observe(path string, saveErr error, engine status.State) Report {
	r := Report{Path: path}

	switch {
	case saveErr == nil && engine == status.StateConnected:
		r.Outcome = OutcomeSaved
	case saveErr == nil:
		r.Outcome = OutcomeSavedNoEngine
	default:
		var conflict *editable.ConflictError
		if errors.As(saveErr, &conflict) {
			r.Outcome = OutcomeConflict
			return r
		}
		r.Outcome = OutcomeRejected
		r.Problems = flatten(saveErr)
	}
	return r
}

// flatten pulls every error out of a joined error rather than reporting the
// first. ValidateMachine and ValidateSchema return all of what is wrong for a
// reason: showing one at a time sends someone round the edit-save loop once per
// mistake.
func flatten(err error) []string {
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		// A wrap around a join still hides the list one level down.
		if inner := errors.Unwrap(err); inner != nil {
			if _, isJoin := inner.(interface{ Unwrap() []error }); isJoin {
				return flatten(inner)
			}
		}
		return []string{err.Error()}
	}
	var out []string
	for _, e := range joined.Unwrap() {
		out = append(out, flatten(e)...)
	}
	return out
}

// Set holds the latest report per file.
//
// Per file, because saving one thing must not clear the error shown for
// another — someone fixing two files should not lose sight of the second
// because they saved the first.
type Set struct {
	mu      sync.Mutex
	reports map[string]Report
}

func NewSet() *Set { return &Set{reports: map[string]Report{}} }

func (s *Set) Record(r Report) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports[r.Path] = r
}

func (s *Set) Clear(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.reports, path)
}

// All returns every report, ordered by path so rendering is stable.
func (s *Set) All() []Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Report, 0, len(s.reports))
	for _, r := range s.reports {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
