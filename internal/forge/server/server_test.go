package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/forge/web"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"js/vendor/datastar.js": {Data: []byte("// Datastar\n")},
		"css/forge.css":         {Data: []byte("body{}\n")},
	}
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(New(Config{Addr: "127.0.0.1:0"}, testFS()).routes())
	t.Cleanup(srv.Close)
	return srv
}

func TestRoutes(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		wantStatus  int
		wantBody    string // substring, "" to skip
		wantHeaders map[string]string
	}{
		{
			name:       "index renders the layout",
			path:       "/",
			wantStatus: http.StatusOK,
			// The page must link Datastar and both stylesheets, or nothing
			// downstream in Epic 10 works.
			wantBody:    "/static/js/vendor/datastar.js",
			wantHeaders: map[string]string{"Content-Type": "text/html; charset=utf-8"},
		},
		{
			name:       "unknown path is not swallowed by the catch-all",
			path:       "/nope",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "healthz",
			path:       "/healthz",
			wantStatus: http.StatusOK,
		},
		{
			name:        "static asset is served from the embedded FS",
			path:        "/static/js/vendor/datastar.js",
			wantStatus:  http.StatusOK,
			wantBody:    "// Datastar",
			wantHeaders: map[string]string{"Cache-Control": "public, max-age=31536000, immutable"},
		},
		{
			name:       "missing static asset 404s",
			path:       "/static/nope.js",
			wantStatus: http.StatusNotFound,
		},
	}

	srv := newTestServer(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := srv.Client().Get(srv.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			for k, want := range tt.wantHeaders {
				if got := resp.Header.Get(k); got != want {
					t.Errorf("header %s = %q, want %q", k, got, want)
				}
			}
			if tt.wantBody != "" {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("reading body: %v", err)
				}
				if !strings.Contains(string(body), tt.wantBody) {
					t.Errorf("body does not contain %q", tt.wantBody)
				}
			}
		})
	}
}

// The index must render a complete document. Datastar's model is a full page
// load plus an SSE subscription — if this ever starts returning a fragment,
// something has reintroduced content negotiation.
func TestIndex_ReturnsCompleteDocument(t *testing.T) {
	srv := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	for _, want := range []string{"<!doctype html>", "<html", "</html>"} {
		if !strings.Contains(strings.ToLower(string(body)), want) {
			t.Errorf("body missing %q — not a complete document", want)
		}
	}
}

// Listen/Serve/Shutdown are the lifecycle the forge command drives. Exercising
// them through a real socket also covers the ErrServerClosed -> nil branch,
// which is the only real logic in the lifecycle.
func TestServerLifecycle(t *testing.T) {
	srv := New(Config{Addr: "127.0.0.1:0"}, testFS())
	if err := srv.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Port 0 means the kernel picks; Addr must report what was actually bound.
	addr := srv.Addr()
	if addr == "127.0.0.1:0" || !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("Addr() = %q, want a resolved 127.0.0.1 address", addr)
	}

	served := make(chan error, 1)
	go func() { served <- srv.Serve() }()

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d, want 200", resp.StatusCode)
	}

	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-served:
		// A clean shutdown must not surface as an error to the caller.
		if err != nil {
			t.Errorf("Serve returned %v after clean Shutdown, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after Shutdown")
	}
}

func TestServe_BeforeListen(t *testing.T) {
	if err := New(Config{Addr: "127.0.0.1:0"}, testFS()).Serve(); err == nil {
		t.Error("Serve before Listen should error, got nil")
	}
}

// An empty Addr would make net/http bind :80 on every interface. config.Load
// defaults it, but the boundary that actually binds should not depend on that.
func TestNew_EmptyAddrDefaultsToLoopback(t *testing.T) {
	if got := New(Config{}, testFS()).Addr(); got != config.DefaultForgeAddr {
		t.Errorf("Addr() = %q, want %q", got, config.DefaultForgeAddr)
	}
}

// Directory listings of the asset tree are noise, and were previously served
// with a one-year immutable cache header.
func TestStatic_DirectoryListingIsNotServed(t *testing.T) {
	srv := newTestServer(t)
	for _, p := range []string{"/static/", "/static/js/vendor/"} {
		t.Run(p, func(t *testing.T) {
			resp, err := srv.Client().Get(srv.URL + p)
			if err != nil {
				t.Fatalf("GET %s: %v", p, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want 404", resp.StatusCode)
			}
		})
	}
}

// /nope returning 404 alone doesn't prove the catch-all guard fired rather than
// the mux. A non-GET method takes the mux's method-mismatch path instead.
func TestRoot_NonGETIsMethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	resp, err := srv.Client().Post(srv.URL+"/", "text/plain", strings.NewReader(""))
	if err != nil {
		t.Fatalf("POST /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

// /dev/tokens is the page a visual change is reviewed against, so it must
// render every palette group and both type scales.
func TestDevTokens(t *testing.T) {
	srv := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/dev/tokens")
	if err != nil {
		t.Fatalf("GET /dev/tokens: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	got := string(body)

	// Case-insensitive: the design renders these as uppercase mono labels, and
	// the test should pin that the page documents them, not how it cases them.
	// "text" is deliberately absent from this list — it appears in every swatch
	// label (--text-hi, --text-dim) whether or not the group heading renders,
	// so asserting on it would assert nothing.
	lower := strings.ToLower(got)
	for _, want := range []string{
		"surfaces", "borders", "semantic accents",
		"chakra petch", "jetbrains mono",
		// The keyframe demos are an explicit AC: the page must show them.
		"fpulse", "fblink", "fdash",
		"shadow-node", "shadow-modal",
	} {
		if !strings.Contains(lower, want) {
			t.Errorf("page does not mention %q", want)
		}
	}

	// Every palette hex must render, not just the accents — the page's job is
	// to show the whole palette.
	for token, hex := range web.Palette {
		if !strings.Contains(lower, strings.ToLower(hex)) {
			t.Errorf("page does not render %s (%s)", token, hex)
		}
	}
}
