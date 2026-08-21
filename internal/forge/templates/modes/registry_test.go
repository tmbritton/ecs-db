package modes

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mode"
)

// Every mode in the table must have a stub. A missing entry is a nil
// component, which panics at render time rather than 404ing — so the gap has
// to be caught here, not in production.
func TestRegistry_CoversEveryMode(t *testing.T) {
	for _, m := range mode.All {
		t.Run(m.Slug, func(t *testing.T) {
			build, ok := Registry[m.Slug]
			if !ok {
				t.Fatalf("no stub registered for %q", m.Slug)
			}
			if build == nil {
				t.Fatalf("stub for %q is nil", m.Slug)
			}
			c := build(Data{})
			if c == nil {
				t.Fatalf("stub for %q built a nil component", m.Slug)
			}

			var buf bytes.Buffer
			if err := c.Render(context.Background(), &buf); err != nil {
				t.Fatalf("render %s: %v", m.Slug, err)
			}
			got := buf.String()
			// An unfinished build should say what it is and what fills it,
			// rather than rendering an ambiguous blank region.
			if !strings.Contains(got, m.Caption) {
				t.Errorf("%s stub does not name itself (%q)\ngot: %s", m.Slug, m.Caption, got)
			}
			if !strings.Contains(strings.ToLower(got), "epic") {
				t.Errorf("%s stub does not say which epic fills it\ngot: %s", m.Slug, got)
			}
		})
	}
}

// A stub registered under a slug that is not a mode would never render, and
// would quietly outlive the mode it was written for.
func TestRegistry_HasNoStubsForUnknownModes(t *testing.T) {
	for slug := range Registry {
		if _, ok := mode.Lookup(slug); !ok {
			t.Errorf("Registry has %q, which is not a mode", slug)
		}
	}
}
