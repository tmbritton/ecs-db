package templates

import (
	"bytes"
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mode"
)

var testIDRE = regexp.MustCompile(`data-testid="([^"]+)"`)

// assertTestIDs checks that every id the e2e suite selects on is present, and
// that none is rendered twice — a duplicate makes a Playwright locator match
// two elements, which surfaces as a strict-mode violation a long way from its
// cause.
func assertTestIDs(t *testing.T, markup string, want []string) {
	t.Helper()
	seen := map[string]int{}
	for _, id := range testIDs(markup) {
		seen[id]++
	}
	for _, id := range want {
		switch seen[id] {
		case 1:
			// as expected
		case 0:
			t.Errorf("data-testid=%q is no longer rendered; e2e/specs select on it", id)
		default:
			t.Errorf("data-testid=%q is rendered %d times; it must be unique", id, seen[id])
		}
	}
}

func testIDs(markup string) []string {
	var out []string
	for _, m := range testIDRE.FindAllStringSubmatch(markup, -1) {
		out = append(out, m[1])
	}
	return out
}

// The e2e suite selects by test id, so a rename is a broken suite. Catching it
// here means the failure arrives in `go test` — seconds, no browser — rather
// than as a timeout in Playwright some minutes later.
func TestShell_ExposesTheTestIDsTheSuiteSelectsOn(t *testing.T) {
	got := map[string]bool{}
	for _, id := range testIDs(renderShell(t, mode.Default)) {
		got[id] = true
	}

	want := []string{
		"shell", "menubar", "wordmark", "engine-status",
		"mode-rail", "rail-cog", "mode-content",
		"menu-item-file", "menu-item-edit", "menu-item-view",
		"menu-item-map", "menu-item-engine", "menu-item-help",
	}
	for _, m := range mode.All {
		want = append(want, "rail-"+m.Slug, "rail-caption-"+m.Slug)
	}
	for _, id := range want {
		if !got[id] {
			t.Errorf("shell no longer renders data-testid=%q; e2e/specs select on it", id)
		}
	}
}

// The /dev/tokens gallery is what 04-primitives.spec.js drives, so its card ids
// need pinning too. They were not, at first, and the mapping was silently wrong
// from the fifth card onward — the ids had been assigned by assuming the source
// order rather than reading each card's own label.
func TestDevTokens_ExposesTheGalleryTestIDs(t *testing.T) {
	var buf bytes.Buffer
	page := DevTokens(Surfaces, Borders, TextTones, Accents)
	if err := page.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render /dev/tokens: %v", err)
	}
	assertTestIDs(t, buf.String(), []string{
		"demo-listrow", "demo-chip", "demo-segmented", "demo-inputs",
		"demo-iconbutton", "demo-panel", "demo-ctxmenu", "demo-savefooter",
		"demo-density", "demo-modal",
	})
}

// A duplicate test id makes a locator match two elements, which Playwright
// reports as strict-mode violation — a confusing failure a long way from its
// cause.
func TestShell_TestIDsAreUnique(t *testing.T) {
	for _, m := range mode.All {
		t.Run(m.Slug, func(t *testing.T) {
			seen := map[string]int{}
			for _, id := range testIDs(renderShell(t, m)) {
				seen[id]++
			}
			var dupes []string
			for id, n := range seen {
				if n > 1 {
					dupes = append(dupes, id)
				}
			}
			sort.Strings(dupes)
			if len(dupes) > 0 {
				t.Errorf("duplicate test ids: %v", dupes)
			}
		})
	}
}

// data-active is written on every rail button, not just the active one, so a
// test can assert the negative directly instead of inferring it from a missing
// attribute — which is indistinguishable from a typo in the attribute name.
func TestShell_RailButtonsCarryTheirActiveState(t *testing.T) {
	for _, active := range mode.All {
		t.Run(active.Slug, func(t *testing.T) {
			got := renderShell(t, active)
			for _, m := range mode.All {
				want := "false"
				if m.Slug == active.Slug {
					want = "true"
				}
				re := regexp.MustCompile(
					`data-testid="rail-` + m.Slug + `"[^>]*data-active="` + want + `"`)
				if !re.MatchString(got) {
					t.Errorf("rail-%s should have data-active=%q when %s is active",
						m.Slug, want, active.Slug)
				}
			}
			if n := strings.Count(got, `data-active="true"`); n != 1 {
				t.Errorf("%d rail buttons marked active, want 1", n)
			}
		})
	}
}
