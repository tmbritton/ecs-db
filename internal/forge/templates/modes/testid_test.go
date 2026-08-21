package modes

import (
	"bytes"
	"context"
	"regexp"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mode"
)

var testIDRE = regexp.MustCompile(`data-testid="([^"]+)"`)

// 05-app-shell.spec.js reads the stub through these, so a rename here is a
// broken browser suite. Catching it in `go test` costs seconds; catching it in
// Playwright costs a timeout and a screenshot.
func TestStubs_ExposeTheTestIDsTheSuiteSelectsOn(t *testing.T) {
	for _, m := range mode.All {
		t.Run(m.Slug, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Registry[m.Slug]().Render(context.Background(), &buf); err != nil {
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
