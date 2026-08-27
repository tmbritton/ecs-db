package server

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
)

// subscriptionOf pulls the stream URL out of a rendered page, the way the
// browser does: whatever data-init says is what actually gets subscribed, so a
// test that built the URL itself would be testing its own arithmetic.
var initAttr = regexp.MustCompile(`data-init="@get\(&#39;(.*?)&#39;\)"`)

func subscriptionOf(t *testing.T, body string) string {
	t.Helper()
	m := initAttr.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no data-init subscription in the page")
	}
	return strings.ReplaceAll(m[1], "&amp;", "&")
}

// The page and the stream's first render are the same HTML, produced a
// millisecond apart. Sending it again made every navigation parse 79 KB and
// then immediately morph <main> to a byte-identical copy of itself.
func TestModeEvents_SendsNothingToAPageThatJustRendered(t *testing.T) {
	srv, s := pushServer(t)

	_, body := get(t, srv, mode.Default.Path())
	sub := subscriptionOf(t, body)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := frames(t, ctx, srv.URL+sub)

	select {
	case f, ok := <-ch:
		if ok {
			t.Errorf("a page that had just rendered was sent %d bytes of itself back:\n%.200s", len(f), f)
		} else {
			t.Error("the stream closed rather than staying quiet")
		}
	case <-time.After(700 * time.Millisecond):
	}

	// Silence has two causes and only one of them is the optimisation. A
	// handler that returned early after writing its headers looks identical
	// from out here, and this is the test guarding the headline claim.
	if open := s.openStreams(); open != 1 {
		t.Errorf("%d streams open, want the quiet one still to be there", open)
	}
}

// The skip is an optimisation, and an optimisation that can be wrong silently
// is a bug. A stream whose version does not match what the server holds gets
// everything — which is also what a browser reconnecting after a dropped
// connection needs, having missed whatever happened while it was away.
func TestModeEvents_SendsEverythingWhenTheVersionIsStale(t *testing.T) {
	srv, _ := pushServer(t)

	_, body := get(t, srv, mode.Default.Path())
	sub := subscriptionOf(t, body)

	// Something happened between the page rendering and the stream connecting —
	// another tab saving, the engine coming up.
	if code := post(t, srv, "/forge/schema/version"); code != 204 {
		t.Fatalf("bumping the version = %d, want 204", code)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := frames(t, ctx, srv.URL+sub)

	awaitFrame(t, ch, "save-footer", "a stream whose page is behind")
}

// No version at all is the shape every subscription had before this, and the
// shape anything not built by the shell will have. It must mean "send me
// everything", not "send me nothing".
func TestModeEvents_SendsEverythingWhenNoVersionIsGiven(t *testing.T) {
	srv, _ := pushServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events")

	awaitFrame(t, ch, "save-footer", "a stream that named no version")
}

// The property the skip rests on, asserted directly rather than assumed: what
// the page ships and what the stream would have sent are the same bytes.
//
// This is the failure the skip would otherwise hide. streamQuery rebuilds a
// subset of the page's query by hand, and it has been wrong three times — each
// time visible as the page changing on its own a moment after load. Skipping
// the first send would have turned that into nothing at all, until the next
// edit made the page jump.
func TestPageAndStreamRenderTheSameRegions(t *testing.T) {
	srv, s := pushServer(t)

	// With a selection as well as without. The premise above is about
	// streamQuery rebuilding the page's query by hand, so a page loaded at a
	// bare path is the one case that cannot exercise it.
	for _, m := range modesWithAndWithoutASelection() {
		_, body := get(t, srv, m.at)

		req := mustRequest(t, srv.URL+m.at)
		data := s.modeData(req)

		regions, _, err := s.renderRegions(m.mode, data)
		if err != nil {
			t.Fatalf("%s: rendering regions: %v", m.at, err)
		}
		for name, region := range map[string]string{
			"engine status": regions.EngineStatus,
			"save reports":  regions.SaveReports,
			"save footer":   regions.SaveFooter,
			"mode content":  regions.ModeContent,
			"save confirm":  regions.SaveConfirm,
		} {
			if !strings.Contains(body, region) {
				t.Errorf("%s: the page does not contain the %s region the stream would send:\n%.300s",
					m.at, name, region)
			}
		}
	}
}

func mustRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building a request for %s: %v", url, err)
	}
	// modeSlug reads the routed path value, which only a real mux sets. The
	// tests above go through httptest, so this stands in for what the router
	// would have filled.
	req.SetPathValue("mode", strings.TrimPrefix(req.URL.Path, "/forge/"))
	return req
}

// A stamp describes the page at the moment it loaded, and is spent on the first
// pass. Left in play it goes on matching — so a change that happens to restore
// the state the page loaded with would be recognised as "you already have this"
// and never sent, leaving the browser showing the state in between with nothing
// to correct it.
func TestModeEvents_AChangeBackToWhatThePageLoadedWithIsStillSent(t *testing.T) {
	srv, _ := pushServer(t)

	_, body := get(t, srv, mode.Default.Path())
	sub := subscriptionOf(t, body)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := frames(t, ctx, srv.URL+sub)
	drainFrames(ch, 200*time.Millisecond)

	// Away from what the page loaded with.
	if code := post(t, srv, "/forge/schema/version"); code != 204 {
		t.Fatalf("bumping the version = %d, want 204", code)
	}
	awaitFrame(t, ch, "● unsaved", "the edit")

	// And back to it: discard restores the file's own state, which is exactly
	// what the page rendered.
	if code := post(t, srv, "/forge/schema/discard"); code != 204 {
		t.Fatalf("discarding = %d, want 204", code)
	}
	awaitFrame(t, ch, "✓ saved", "the return to what the page loaded with")
}

// A page is composed of the regions the stream would send, so a region that
// cannot be rendered has no honest empty value — leaving it out ships a page
// with no element at the id every later patch targets, and the mode would go
// quiet for the life of the tab.
func TestRenderRegions_RefusesRatherThanShippingAPageWithAHoleInIt(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	_, _, err := s.renderRegions(mode.Mode{Slug: "not-a-mode"}, modes.Data{})
	if err == nil {
		t.Fatal("a mode with no content registered rendered a page anyway")
	}
	// Named, so that a failure in one of the regions rendered before it cannot
	// pass this test under a name about something else.
	if !strings.Contains(err.Error(), "not-a-mode") {
		t.Errorf("the error does not name the mode that could not be rendered: %v", err)
	}
}

// modesWithAndWithoutASelection is every mode at its bare path, plus the two
// that select with a query parameter at a path that carries one. Those are the
// pages whose stream subscription is rebuilt rather than copied.
func modesWithAndWithoutASelection() []struct {
	mode mode.Mode
	at   string
} {
	out := make([]struct {
		mode mode.Mode
		at   string
	}, 0, len(mode.All)+2)
	for _, m := range mode.All {
		out = append(out, struct {
			mode mode.Mode
			at   string
		}{m, m.Path()})
	}
	for _, sel := range []struct {
		slug, query string
	}{
		{"schema", "?component=Position"},
		{"ents", "?type=Player"},
	} {
		m, ok := mode.Lookup(sel.slug)
		if !ok {
			continue
		}
		out = append(out, struct {
			mode mode.Mode
			at   string
		}{m, m.Path() + sel.query})
	}
	return out
}

// Every region lands in its own slot, exactly once, in the whole document.
//
// The mode's own assembly is checked in modes/regions_test.go, but that cannot
// see the shell — and the shell is where the slots can be crossed. Reading the
// save confirmation out of a fixed index rather than the end of the slice puts
// a mode region into the confirmation slot: the page then carries that id
// twice, every patch for it lands on whichever comes first in the document, and
// the region it displaced never appears at all. Nothing errors.
func TestModePage_CarriesEveryRegionExactlyOnce(t *testing.T) {
	srv, _ := pushServer(t)

	for _, m := range mode.All {
		_, body := get(t, srv, m.Path())

		ids := []string{"engine-status", "save-reports", "save-footer", "save-confirm", "mode-content"}
		for _, region := range modes.Registry[m.Slug].Regions {
			ids = append(ids, region.ID)
		}
		for _, id := range ids {
			// Anchored on the whitespace before the attribute: `data-testid="x"`
			// ends with `id="x"`, so an unanchored count reports a test id as an
			// element id and every one of these passes for the wrong reason.
			if n := len(regexp.MustCompile(`\sid="`+regexp.QuoteMeta(id)+`"`).FindAllString(body, -1)); n != 1 {
				t.Errorf("%s: id %q appears %d times in the page, want exactly 1", m.Slug, id, n)
			}
		}
	}
}

// The stream's subscription has to name the map the page is showing.
//
// Without it the stream renders whatever selectMap falls back to — the first in
// the list — so a page editing the second map would have the first one patched
// over it on the next event. With one map in the project the fallback and the
// selection are the same thing and nothing goes wrong, which is exactly why
// this needs two.
func TestStreamQuery_NamesTheMapThePageIsShowing(t *testing.T) {
	srv, s, sess, configured := mapServer(t)

	other := ""
	for _, path := range sess.Paths() {
		if path != configured {
			other = path
			break
		}
	}
	if other == "" {
		t.Fatalf("the fixture holds one map, so this can prove nothing: %v", sess.Paths())
	}

	_, body := get(t, srv, "/forge/map?map="+url.QueryEscape(other))
	sub := subscriptionOf(t, body)

	req := mustRequest(t, srv.URL+"/forge/map/events?"+strings.TrimPrefix(sub, "/forge/map/events?"))
	if got := s.modeData(req).SelectedMap; got != other {
		t.Errorf("the page shows %q and its stream renders %q", other, got)
	}
}
