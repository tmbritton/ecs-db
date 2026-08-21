package components

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// render runs a component to a string. Tests assert on the state-carrying
// signal in that string — a class, a glyph, an attribute — never on whole-HTML
// equality, which would turn every future style tweak into a test failure.
func render(t *testing.T, c templ.Component) string {
	t.Helper()
	return renderWith(t, context.Background(), c)
}

// renderChildren renders a component that takes `{ children... }`, with a
// marker string as the child so tests can prove the slot is filled.
func renderChildren(t *testing.T, c templ.Component, child string) string {
	t.Helper()
	ctx := templ.WithChildren(context.Background(), text(child))
	return renderWith(t, ctx, c)
}

func renderWith(t *testing.T, ctx context.Context, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(ctx, &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// text is a trivial component used as child content in tests.
func text(s string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, s)
		return err
	})
}

// assertOutput keeps the table cases declarative: each case lists the
// substrings that must appear and the ones that must not.
func assertOutput(t *testing.T, got string, want, notWant []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("output missing %q\ngot: %s", w, got)
		}
	}
	for _, w := range notWant {
		if strings.Contains(got, w) {
			t.Errorf("output should not contain %q\ngot: %s", w, got)
		}
	}
}

// countSubstring counts non-overlapping occurrences — used where the assertion
// is "exactly one of these", which a Contains check cannot express.
func countSubstring(haystack, needle string) int {
	return strings.Count(haystack, needle)
}
