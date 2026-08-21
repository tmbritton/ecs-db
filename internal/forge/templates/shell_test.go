package templates

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/savereport"
	"github.com/tmbritton/ecs-db/internal/forge/status"
)

func renderShell(t *testing.T, active mode.Mode) string {
	t.Helper()
	return renderShellWith(t, active, status.Status{})
}

// renderShellWith lets a test choose the engine status the menu bar renders.
func renderShellWith(t *testing.T, active mode.Mode, engine status.Status) string {
	t.Helper()
	var buf bytes.Buffer
	body := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, "<p>STUB-CONTENT</p>")
		return err
	})
	if err := Shell(active, "", engine, nil, NoFooter(), body).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render shell: %v", err)
	}
	return buf.String()
}

// The rail is the app's navigation. Exactly one button is active, it is the
// one for the page being rendered, and every button links to its own mode.
func TestShell_RailMarksExactlyTheActiveMode(t *testing.T) {
	for _, active := range mode.All {
		t.Run(active.Slug, func(t *testing.T) {
			got := renderShell(t, active)

			if n := strings.Count(got, "rail__btn--active"); n != 1 {
				t.Errorf("found %d active rail buttons, want exactly 1", n)
			}
			for _, m := range mode.All {
				href := `href="/forge/` + m.Slug + `"`
				if !strings.Contains(got, href) {
					t.Errorf("rail is missing a link to %s (%s)", m.Slug, href)
				}
				if !strings.Contains(got, m.Caption) {
					t.Errorf("rail is missing the caption %q", m.Caption)
				}
			}
			// The active class must sit on the active mode's own anchor, not
			// merely somewhere on the page. Anchor-scoped so a stray class
			// elsewhere cannot satisfy it.
			activeAnchor := regexp.MustCompile(
				`<a[^>]*href="` + regexp.QuoteMeta(active.Path()) + `"[^>]*rail__btn--active`)
			if !activeAnchor.MatchString(got) {
				t.Errorf("the active class is not on %s's anchor\ngot: %s", active.Slug, got)
			}
		})
	}
}

// A mode switch is a full page load. Rail entries must be anchors with real
// hrefs — that is the whole of what makes deep links, bookmarks and the back
// button work, and none of it survives a rewrite into click handlers.
func TestShell_RailNavigatesWithAnchorsNotHandlers(t *testing.T) {
	got := renderShell(t, mode.Default)

	railAnchors := regexp.MustCompile(`<a href="/forge/[a-z]+" class="rail__btn`)
	if n := len(railAnchors.FindAllString(got, -1)); n != len(mode.All) {
		t.Errorf("found %d rail anchors, want %d", n, len(mode.All))
	}
	// A click handler on a rail button would mean someone reimplemented
	// routing on top of navigation that already worked.
	rail := section(t, got, `<nav class="rail"`, `</nav>`)
	if strings.Contains(rail, "data-on") {
		t.Errorf("the rail carries a Datastar event handler; mode switching is navigation\n%s", rail)
	}
}

// Each page opens exactly one SSE subscription, and it is the one for the mode
// being rendered. A per-widget stream would burn the browser's per-origin
// connection budget for no benefit.
func TestShell_OpensOneEventStreamForItsOwnMode(t *testing.T) {
	for _, active := range mode.All {
		t.Run(active.Slug, func(t *testing.T) {
			got := renderShell(t, active)

			want := `data-init="@get(&#39;/forge/` + active.Slug + `/events&#39;)"`
			if !strings.Contains(got, want) {
				t.Errorf("shell does not subscribe to its own event stream\nwant: %s\ngot: %s", want, got)
			}
			if n := strings.Count(got, "@get("); n != 1 {
				t.Errorf("page opens %d streams, want exactly 1", n)
			}
			for _, other := range mode.All {
				if other.Slug == active.Slug {
					continue
				}
				if strings.Contains(got, "/forge/"+other.Slug+"/events") {
					t.Errorf("%s page subscribes to %s's stream", active.Slug, other.Slug)
				}
			}
		})
	}
}

// The shell renders whatever component it is handed and knows nothing else
// about a mode. If this stops passing, mode-specific knowledge has leaked in.
func TestShell_RendersTheContentItIsGiven(t *testing.T) {
	got := renderShell(t, mode.Default)
	if !strings.Contains(got, "<p>STUB-CONTENT</p>") {
		t.Error("shell did not render the content component")
	}
	if !strings.Contains(got, `id="mode-content"`) {
		t.Error("shell has no mode-content region for later epics to patch into")
	}
}

// A Datastar patch finds its target by id. Without this element in the served
// HTML, every save report the server pushes lands on nothing and is dropped in
// silence — and deleting the component from the shell left every Go test green.
func TestShell_RendersTheSaveReportTarget(t *testing.T) {
	got := renderShell(t, mode.Default)
	if !regexp.MustCompile(`\sid="save-reports"`).MatchString(got) {
		t.Errorf("the shell has no save-report patch target\n%s", got)
	}
}

// And it renders the reports it is given, so a page loaded after a failed save
// shows it on first paint rather than waiting for the next stream tick.
func TestShell_RendersTheReportsItIsGiven(t *testing.T) {
	var buf bytes.Buffer
	body := templ.ComponentFunc(func(_ context.Context, w io.Writer) error { return nil })
	reports := []savereport.Report{{
		Path:     "/p/goblin.json",
		Outcome:  savereport.OutcomeRejected,
		Problems: []string{"unknown action alpha"},
	}}
	if err := Shell(mode.Default, "", status.Status{}, reports, NoFooter(), body).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{"goblin.json", "unknown action alpha"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("the shell did not render %q", want)
		}
	}
}

func TestShell_TitlesThePage(t *testing.T) {
	for _, m := range mode.All {
		t.Run(m.Slug, func(t *testing.T) {
			got := renderShell(t, m)
			want := "<title>Forge — " + m.Title + "</title>"
			if !strings.Contains(got, want) {
				t.Errorf("missing %q", want)
			}
		})
	}
}

func TestShell_MenuBar(t *testing.T) {
	got := renderShell(t, mode.Default)
	bar := section(t, got, `<div class="menubar"`, `</div><div class="shell__body"`)

	for _, want := range []string{"⚒ FORGE", "File", "Edit", "View", "Map", "Engine", "Help"} {
		if !strings.Contains(bar, want) {
			t.Errorf("menu bar missing %q", want)
		}
	}
	// Engine owns the engine-connection actions, so it carries the accent.
	if !regexp.MustCompile(`menu__item--accent[^>]*>Engine<`).MatchString(bar) {
		t.Errorf("Engine is not the accented menu item\n%s", bar)
	}
	// Story 6 patches the readout into this element over the page's SSE stream;
	// without a stable id there is nothing to target.
	if !strings.Contains(bar, `id="engine-status"`) {
		t.Error("menu bar has no engine-status slot for Story 6")
	}
}

// The menus open dialogs that do not exist until Epic 17. Rendering them as
// live controls would promise behaviour that is not there; disabled buttons
// say so to a screen reader as well as to the eye.
func TestShell_MenuItemsAreHonestlyDisabled(t *testing.T) {
	got := renderShell(t, mode.Default)
	bar := section(t, got, `<div class="menubar"`, `</div><div class="shell__body"`)

	items := regexp.MustCompile(`<button class="menu__item[^"]*" type="button" disabled`)
	if n := len(items.FindAllString(bar, -1)); n != 6 {
		t.Errorf("found %d disabled menu buttons, want 6\n%s", n, bar)
	}
	if strings.Contains(bar, "data-on") {
		t.Error("a menu item carries a handler, but the dialogs are Epic 17")
	}
}

// The cog opens Preferences, which is Epic 17. There is no /forge/settings
// route, so linking to one would be a 404 sitting in the primary navigation.
func TestShell_CogIsNotADeadLink(t *testing.T) {
	got := renderShell(t, mode.Default)
	if strings.Contains(got, "/forge/settings") {
		t.Error("the cog links to /forge/settings, which is not a route")
	}
	if !regexp.MustCompile(`rail__cog[^>]*disabled`).MatchString(got) {
		t.Error("the cog should be a disabled affordance until Epic 17 wires Preferences")
	}
}

// The rail caption is an abbreviation; the accessible name must not be. Left
// alone, the app's primary navigation announces "SPRT" for Sprites and "ENTS"
// for Entity Types.
func TestShell_RailAnnouncesFullModeNames(t *testing.T) {
	got := renderShell(t, mode.Default)
	for _, m := range mode.All {
		re := regexp.MustCompile(
			`data-testid="rail-` + m.Slug + `"[^>]*aria-label="` + regexp.QuoteMeta(m.Title) + `"`)
		if !re.MatchString(got) {
			t.Errorf("rail-%s should be labelled %q, not just captioned %q", m.Slug, m.Title, m.Caption)
		}
	}
}

// Everything the shell links to must resolve. Anchors are collected here and
// checked against the router in the server package's test; this one pins that
// the shell emits no href outside the mode routes.
func TestShell_LinksOnlyToRoutes(t *testing.T) {
	got := renderShell(t, mode.Default)
	hrefs := regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(got, -1)

	routable := map[string]bool{}
	for _, m := range mode.All {
		routable[m.Path()] = true
	}
	var found int
	for _, h := range hrefs {
		if strings.HasPrefix(h[1], "/static/") {
			continue // stylesheets
		}
		found++
		if !routable[h[1]] {
			t.Errorf("shell links to %q, which is not a mode route", h[1])
		}
	}
	if found != len(mode.All) {
		t.Errorf("found %d navigation links, want %d", found, len(mode.All))
	}
}

// section extracts the markup between two delimiters so an assertion can be
// scoped to one region. A test that greps the whole document proves only that
// a string exists somewhere on the page.
func section(t *testing.T, doc, start, end string) string {
	t.Helper()
	i := strings.Index(doc, start)
	if i < 0 {
		t.Fatalf("document has no %q", start)
	}
	rest := doc[i:]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("no %q after %q", end, start)
	}
	return rest[:j+len(end)]
}
