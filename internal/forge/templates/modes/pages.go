package modes

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/a-h/templ"
)

// The Page functions in Registry, curried.
//
// templ components are not curried, so a mode whose assembly needs a constant
// alongside its regions — a test id, or the order the three MAP regions go in —
// needs one of these between the registry and the template.

// TwoPanePage assembles a list rail and an editor panel from two rendered
// regions.
func TwoPanePage(testid string) func([]string) templ.Component {
	return func(rendered []string) templ.Component {
		return twoPanePage(testid, rendered)
	}
}

// MapPage assembles MAP's four regions: the rail, and the three that share the
// column beside it.
func MapPage(rendered []string) templ.Component {
	return mapPage(at(rendered, 0), at(rendered, 1), at(rendered, 2), at(rendered, 3))
}

// at is a region that may not have been rendered — which happens only if
// Registry and the page assembly disagree about how many regions a mode has.
// regions_test.go asserts every region reaches the page, so this returning
// empty is a test failure rather than a blank panel in front of anyone.
func at(rendered []string, i int) string {
	if i >= len(rendered) {
		return ""
	}
	return rendered[i]
}

// Render assembles a mode's whole content from Data.
//
// The server does not use this: it renders the regions one at a time so it can
// send them one at a time, which is the point of having regions. This is for
// the callers that want the finished thing — tests asking "what does this mode
// look like", and anything that needs a mode rendered in one piece.
//
// It goes through the same regions and the same Page, so what it produces is
// what the server would assemble, not a second opinion about it.
func Render(slug string, data Data) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		content, ok := Registry[slug]
		if !ok {
			return fmt.Errorf("modes: no content registered for %q", slug)
		}
		rendered := make([]string, len(content.Regions))
		for i, region := range content.Regions {
			var buf bytes.Buffer
			if err := region.Render(data).Render(ctx, &buf); err != nil {
				return fmt.Errorf("modes: rendering %s/%s: %w", slug, region.ID, err)
			}
			rendered[i] = buf.String()
		}
		return content.Page(rendered).Render(ctx, w)
	})
}
