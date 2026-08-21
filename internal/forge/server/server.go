// Package server serves the Forge content editor over HTTP. It owns transport
// and nothing else: no domain logic, no file authoring. Config and the asset
// filesystem are injected so handler tests run against a real httptest.Server
// with no package-level state to reset.
package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/forge/templates"
)

// Config is the subset of application config the HTTP layer needs.
type Config struct {
	Addr string
}

type Server struct {
	cfg    Config
	static fs.FS
	http   *http.Server

	mu sync.Mutex
	ln net.Listener
}

func New(cfg Config, static fs.FS) *Server {
	// An empty Addr would make net/http listen on :80 across every interface.
	// config.Load defaults this already; guarding here too means the boundary
	// that actually binds cannot be wrong, whatever constructed the Config.
	if cfg.Addr == "" {
		cfg.Addr = config.DefaultForgeAddr
	}
	s := &Server{cfg: cfg, static: static}
	s.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", s.staticHandler()))
	mux.HandleFunc("GET /dev/tokens", s.handleDevTokens)
	mux.HandleFunc("POST /dev/noop", s.handleDevNoop)
	mux.HandleFunc("GET /", s.handleIndex)
	return mux
}

// handleDevNoop is the sink the /dev/tokens gallery posts to. The gallery
// wires real Datastar actions rather than decorative ones — a handler that
// only looks wired is how the whole package shipped with `data-on-click`,
// which parses as a plugin named "on-click" and is silently ignored — so the
// actions need somewhere to land that is not a 405 in the console.
//
// 204: nothing to patch, which is a valid Datastar response.
func (s *Server) handleDevNoop(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// staticHandler serves the embedded asset tree. Assets are immutable for the
// lifetime of a binary, so they can be cached hard.
func (s *Server) staticHandler() http.Handler {
	files := http.FileServerFS(s.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't serve (or year-cache) an autoindex of the asset tree. After
		// StripPrefix, "/static/" arrives as "" and "/static/css/" as "css/",
		// so both shapes need checking.
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(w, r)
	})
}

// handleDevTokens renders the design-token reference. It is deliberately not
// reachable from the app shell — it is a development aid.
func (s *Server) handleDevTokens(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, templates.DevTokens(
		templates.Surfaces, templates.Borders, templates.TextTones, templates.Accents))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// "GET /" is a catch-all in net/http's pattern syntax, so anything that
	// matched no other route lands here. Only the root is a real page.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, templates.Layout("Forge"))
}

// render writes a templ component as a complete HTML document.
func (s *Server) render(w http.ResponseWriter, r *http.Request, c templates.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		// The response is already partially written, so there is no status code
		// left to send. Log and let the truncated body signal the failure.
		slog.ErrorContext(r.Context(), "rendering page", "path", r.URL.Path, "err", err)
	}
}

// Listen binds the configured address. It is separate from Serve so a caller
// can fail fast on a bind error, and can report the address actually bound —
// which differs from the configured one whenever the port is 0.
func (s *Server) Listen() error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	return nil
}

// Serve blocks until the server stops. A clean Shutdown is not an error.
// Listen must have been called first.
func (s *Server) Serve() error {
	s.mu.Lock()
	ln := s.ln
	s.mu.Unlock()
	if ln == nil {
		return errors.New("forge/server: Serve called before Listen")
	}
	err := s.http.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

// Addr reports the address actually bound once Listen has run, falling back to
// the configured address before that.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	return s.cfg.Addr
}
