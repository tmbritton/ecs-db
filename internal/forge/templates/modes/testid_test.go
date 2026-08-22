package modes

import (
	"bytes"
	"context"
	"regexp"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/validation"
)

var testIDRE = regexp.MustCompile(`data-testid="([^"]+)"`)

// 05-app-shell.spec.js reads the stub through these, so a rename here is a
// broken browser suite. Catching it in `go test` costs seconds; catching it in
// Playwright costs a timeout and a screenshot.
func TestStubs_ExposeTheTestIDsTheSuiteSelectsOn(t *testing.T) {
	for _, m := range mode.All {
		t.Run(m.Slug, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Registry[m.Slug](Data{}).Render(context.Background(), &buf); err != nil {
				t.Fatalf("render: %v", err)
			}
			seen := map[string]int{}
			for _, match := range testIDRE.FindAllStringSubmatch(buf.String(), -1) {
				seen[match[1]]++
			}
			for _, id := range []string{"mode-stub", "mode-stub-caption", "mode-stub-epic"} {
				if seen[id] != 1 {
					t.Errorf("data-testid=%q appears %d times, want exactly 1", id, seen[id])
				}
			}
		})
	}
}

// 12-inline-validation.spec.js selects on these, and unlike the ids above they
// only render when something is wrong — so a rename would not show up in any
// other test either. AGENTS.md asks for the pin for exactly this reason: the
// failure arrives in `go test` in seconds instead of as a browser timeout.
func TestValidation_ExposesTheTestIDsTheSuiteSelectsOn(t *testing.T) {
	// One report carrying every shape of problem at once, so a single render of
	// each mode has to produce all of them.
	report := validation.Report{
		Partial: true,
		Problems: []validation.Problem{
			{Message: "a file-wide problem", Blocking: true},
			{
				Owner:    validation.Owner{Kind: validation.OwnerComponent, Name: "Health"},
				Message:  "a problem about the component itself",
				Blocking: true,
			},
			{
				Owner:   validation.Owner{Kind: validation.OwnerComponent, Name: "Health"},
				Field:   validation.PropertyField("hp"),
				Message: "a problem about a field",
			},
			{
				Owner:    validation.Owner{Kind: validation.OwnerEntityType, Name: "Player"},
				Message:  "a problem about the entity type itself",
				Blocking: true,
			},
		},
	}

	tests := []struct {
		mode     string
		selected string
		render   func(Data) string
		want     []string
	}{
		{
			mode: "schema", selected: "Health",
			render: func(d Data) string { return renderMode(t, d) },
			want: []string{
				"file-problems", "problems-partial", "component-problems",
				"field-problems-hp", "row-problem-Health",
			},
		},
		{
			mode: "ents", selected: "Player",
			render: func(d Data) string { return renderEnts(t, d) },
			want:   []string{"file-problems", "problems-partial", "type-problems", "row-problem-Player"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			data := modeFixture()
			data.Selected = tc.selected
			data.Validation = report

			seen := map[string]int{}
			for _, match := range testIDRE.FindAllStringSubmatch(tc.render(data), -1) {
				seen[match[1]]++
			}
			for _, id := range tc.want {
				if seen[id] != 1 {
					t.Errorf("data-testid=%q appears %d times, want exactly 1", id, seen[id])
				}
			}
		})
	}
}
