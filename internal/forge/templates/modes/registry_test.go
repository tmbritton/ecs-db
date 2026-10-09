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
			content, ok := Registry[m.Slug]
			if !ok {
				t.Fatalf("no content registered for %q", m.Slug)
			}
			if len(content.Regions) == 0 {
				t.Fatalf("%q registers no regions, so nothing on it can be patched", m.Slug)
			}
			if content.Page == nil {
				t.Fatalf("%q registers no way to assemble its regions into a page", m.Slug)
			}
			c := Render(m.Slug, Data{})

			var buf bytes.Buffer
			if err := c.Render(context.Background(), &buf); err != nil {
				t.Fatalf("render %s: %v", m.Slug, err)
			}
			got := buf.String()
			if m.Slug == "tiles" || m.Slug == "sprites" {
				if !strings.Contains(got, `data-testid="`+m.Slug+`-mode"`) {
					t.Errorf("implemented %s mode did not render its project file surface", m.Slug)
				}
				return
			}
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
