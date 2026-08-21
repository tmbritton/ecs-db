package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/web"
)

// newRealAssetServer serves the embedded asset tree rather than a fake, so
// tests that follow the links a page emits check them against what actually
// ships.
func newRealAssetServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(New(Config{Addr: "127.0.0.1:0"}, web.Static).routes())
	t.Cleanup(srv.Close)
	return srv
}

// newTestServerWithRef also hands back the *Server, for the assertions that
// need to see inside — specifically, whether a stream is still held open.
func newTestServerWithRef(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	return srv, s
}

func get(t *testing.T, srv *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return resp, string(body)
}

// Every mode is a URL that serves a complete page with its own rail button
// lit. The table is mode.All, so a mode added to the rail without a route
// fails here rather than 404ing in the browser.
func TestModeRoutes(t *testing.T) {
	srv := newTestServer(t)

	for _, m := range mode.All {
		t.Run(m.Slug, func(t *testing.T) {
			resp, body := get(t, srv, m.Path())

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q", ct)
			}
			// The one that pins the model: a mode route has exactly one
			// representation, a whole document. Datastar's contract is a full
			// page load plus an SSE subscription — it never asks a route for a
			// fragment, so there is nothing here to content-negotiate.
			for _, want := range []string{"<!doctype html>", "<html", `<nav class="rail"`, "</html>"} {
				if !strings.Contains(strings.ToLower(body), strings.ToLower(want)) {
					t.Errorf("%s is not a complete shell document: missing %q", m.Path(), want)
				}
			}
			if !strings.Contains(body, "<title>Forge — "+m.Title+"</title>") {
				t.Errorf("wrong page title for %s", m.Slug)
			}

			active := regexp.MustCompile(
				`<a href="` + regexp.QuoteMeta(m.Path()) + `"[^>]*rail__btn--active`)
			if !active.MatchString(body) {
				t.Errorf("%s does not mark its own rail button active", m.Slug)
			}
			if n := strings.Count(body, "rail__btn--active"); n != 1 {
				t.Errorf("%d rail buttons active, want 1", n)
			}
			if !strings.Contains(body, m.Caption) {
				t.Errorf("%s does not render its stub", m.Slug)
			}
		})
	}
}

func TestModeRoutes_UnknownModeIs404(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{"/forge/nope", "/forge/", "/forge/MAP", "/forge/map/extra"} {
		t.Run(path, func(t *testing.T) {
			resp, _ := get(t, srv, path)
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
			}
		})
	}
}

// "/" is not a page of its own; it lands you in the default mode, which keeps
// one canonical URL per mode for bookmarks and history.
func TestRoot_RedirectsToDefaultMode(t *testing.T) {
	srv := newTestServer(t)
	srv.Client().CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, _ := get(t, srv, "/")

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("GET / = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != mode.Default.Path() {
		t.Errorf("Location = %q, want %q", loc, mode.Default.Path())
	}
}

// Anything the shell links to must resolve. This is the cross-check between
// the template and the router: a rail entry pointing at a route that does not
// exist is a 404 sitting in the primary navigation, and the template test
// alone cannot see it.
func TestShellLinks_AllResolve(t *testing.T) {
	srv := newRealAssetServer(t)
	_, body := get(t, srv, mode.Default.Path())

	links := regexp.MustCompile(`(?:href|src)="(/[^"]*)"`).FindAllStringSubmatch(body, -1)
	if len(links) < len(mode.All) {
		t.Fatalf("found only %d links in the shell; the scan is not working", len(links))
	}
	seen := map[string]bool{}
	for _, l := range links {
		target := l[1]
		if seen[target] {
			continue
		}
		seen[target] = true
		t.Run(target, func(t *testing.T) {
			resp, _ := get(t, srv, target)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("the shell links to %s, which returns %d", target, resp.StatusCode)
			}
		})
	}
}

// Each page opens one SSE subscription. The endpoint has to exist and speak
// event-stream from this story, or Story 6 has nowhere to push the readout.
//
// The headers alone prove nothing: datastar.NewSSE writes and flushes them
// before the handler body runs, so a handler that returns immediately still
// answers 200 with text/event-stream. This therefore also asserts the stream
// is *still held open* afterwards — verified by mutation, since the header
// checks on their own pass against a handler that hangs up at once.
func TestModeEvents_IsAnSSEStream(t *testing.T) {
	srv, s := newTestServerWithRef(t)

	for _, m := range mode.All {
		t.Run(m.Slug, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+m.Path()+"/events", nil)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("GET events: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
				t.Errorf("Content-Type = %q, want text/event-stream", ct)
			}
			if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
				t.Errorf("Cache-Control = %q, want no-cache", cc)
			}

			// Datastar does not reconnect after a clean end-of-stream, so a
			// handler that returns here would leave the page permanently and
			// silently unsubscribed.
			waitFor(t, func() bool { return s.openStreams() == 1 },
				"the handler never registered an open stream")
			time.Sleep(50 * time.Millisecond)
			if got := s.openStreams(); got != 1 {
				t.Fatalf("the stream closed on its own: openStreams = %d, want 1", got)
			}

			// And it must end when the client leaves, or every closed tab
			// leaks a goroutine.
			cancel()
			waitFor(t, func() bool { return s.openStreams() == 0 },
				"the handler did not return when the client disconnected")
		})
	}
}

// waitFor polls until cond holds, failing with msg if it never does. Used
// instead of a sleep so a slow machine does not turn into a flaky test.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

func TestModeEvents_UnknownModeIs404(t *testing.T) {
	srv := newTestServer(t)
	resp, _ := get(t, srv, "/forge/nope/events")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// An SSE handler blocks for the life of the connection. http.Server.Shutdown
// waits for in-flight requests, so without a server-scoped context to cancel,
// Ctrl-C on `ecs-db forge` would hang for as long as a browser tab stayed
// open — which, for a stream, is forever.
func TestShutdown_DoesNotWaitForOpenEventStreams(t *testing.T) {
	srv := New(Config{Addr: "127.0.0.1:0"}, testFS())
	if err := srv.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve() }()

	// The connection has to be held open deliberately, not incidentally. An
	// earlier version read one byte and returned, which closed the body — that
	// only looked like "holding a stream" while the stream had nothing to say.
	// The moment Story 6 gave it a payload, the read returned at once and the
	// stream was gone before the assertion ran.
	release := make(chan struct{})
	streamOpen := make(chan struct{})
	go func() {
		defer close(streamOpen)
		resp, err := http.Get("http://" + srv.Addr() + mode.Default.Path() + "/events")
		if err != nil {
			return
		}
		defer resp.Body.Close()
		// Read one byte so the handler is provably running before shutdown,
		// then keep the body open until the test is finished with it.
		buf := make([]byte, 1)
		_, _ = resp.Body.Read(buf)
		<-release
	}()

	// Give the stream a moment to be accepted and reach the handler.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && srv.openStreams() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if srv.openStreams() == 0 {
		t.Fatal("no event stream was open; the test would pass vacuously")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown with an open stream: %v", err)
	}

	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve returned %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after Shutdown")
	}
	// Released here rather than by defer: the wait below cannot complete until
	// the reader goroutine returns, and the goroutine cannot return until this
	// is closed. A defer runs only after the test function returns, so it would
	// deadlock against the wait.
	close(release)
	<-streamOpen
}
