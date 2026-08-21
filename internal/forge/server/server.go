// Package server serves the Forge content editor over HTTP. It owns transport
// and nothing else: no domain logic, no file authoring. Config and the asset
// filesystem are injected so handler tests run against a real httptest.Server
// with no package-level state to reset.
package server

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/savereport"
	"github.com/tmbritton/ecs-db/internal/forge/session"
	"github.com/tmbritton/ecs-db/internal/forge/status"
	"github.com/tmbritton/ecs-db/internal/forge/templates"
	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// Config is the subset of application config the HTTP layer needs.
type Config struct {
	Addr string
	// Session is the editable schema.json both SCHEMA and ENTS work on. Nil
	// when the project could not be opened, which leaves the modes readable
	// and the reason visible rather than failing the whole editor.
	Session *session.Session
	// Engine describes the game database to report on. Zero-valued in tests
	// that do not care, which reports offline — the honest default.
	Engine status.Config
	// PollInterval is how often the engine status is re-checked. Zero falls
	// back rather than panicking time.NewTicker.
	PollInterval time.Duration
}

type Server struct {
	cfg    Config
	static fs.FS
	http   *http.Server

	// Shutdown cancels this, which releases every open SSE stream. An SSE
	// handler blocks for the life of its connection and http.Server.Shutdown
	// waits for in-flight requests, so without it, stopping Forge would block
	// for as long as a browser tab held a stream open.
	//
	// It is deliberately scoped to streams rather than wired through
	// http.Server.BaseContext. A base context would cancel *every* in-flight
	// request, and templ checks ctx.Err() at each component boundary — so an
	// ordinary page render caught by Ctrl-C would be abandoned mid-write and
	// the client would receive a truncated 200. That turns graceful shutdown
	// into abortive shutdown for the whole server.
	streamsCtx    context.Context
	cancelStreams context.CancelFunc

	mu      sync.Mutex
	ln      net.Listener
	streams int

	// The latest save outcome per file, pushed down the page stream alongside
	// the engine status. Its own type carries the locking.
	saves *savereport.Set
}

func New(cfg Config, static fs.FS) *Server {
	// An empty Addr would make net/http listen on :80 across every interface.
	// config.Load defaults this already; guarding here too means the boundary
	// that actually binds cannot be wrong, whatever constructed the Config.
	if cfg.Addr == "" {
		cfg.Addr = config.DefaultForgeAddr
	}
	// A zero interval panics time.NewTicker. config defaults it already;
	// guarding here too means the boundary that actually ticks cannot be wrong,
	// whatever constructed the Config.
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = config.DefaultForgePollSeconds * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		cfg: cfg, static: static,
		streamsCtx: ctx, cancelStreams: cancel,
		saves: savereport.NewSet(),
	}
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
	mux.HandleFunc("POST /dev/schema/bump", s.handleDevSchemaBump)
	mux.HandleFunc("POST /forge/schema/save", s.handleSchemaSave)
	mux.HandleFunc("POST /forge/schema/discard", s.handleSchemaDiscard)
	mux.HandleFunc("POST /forge/schema/reload", s.handleSchemaReload)
	mux.HandleFunc("POST /forge/schema/overwrite", s.handleSchemaOverwrite)
	mux.HandleFunc("GET /forge/{mode}", s.handleMode)
	mux.HandleFunc("GET /forge/{mode}/events", s.handleModeEvents)
	mux.HandleFunc("GET /", s.handleIndex)
	return mux
}

// handleMode serves one mode as a complete page. That is the only
// representation there is: Datastar renders a whole document, the page
// subscribes to an SSE stream, and every later change arrives as an HTML patch
// pushed down it. There is no fragment form of a mode and nothing to
// content-negotiate — switching modes is ordinary navigation, which is what
// makes deep links, bookmarks and the back button work for free.
func (s *Server) handleMode(w http.ResponseWriter, r *http.Request) {
	m, ok := mode.Lookup(r.PathValue("mode"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	content, ok := modes.Registry[m.Slug]
	if !ok {
		// mode.All and modes.Registry are checked against each other in
		// modes/registry_test.go, so this is unreachable — but rendering a nil
		// component panics, and a 500 is a better failure than a dead process.
		slog.ErrorContext(r.Context(), "no content registered for mode", "mode", m.Slug)
		http.Error(w, "mode not available", http.StatusInternalServerError)
		return
	}
	// Rendered server-side on load so the menu bar is never blank before the
	// first patch arrives; the stream takes over from there.
	s.render(w, r, templates.Shell(m, status.Check(s.cfg.Engine), s.saves.All(), s.footer(), content()))
}

// handleModeEvents is the single SSE subscription a mode page opens.
// Everything that updates live on that page is pushed down this one stream —
// one per page, not one per widget, because browsers cap concurrent
// connections per origin.
//
// It holds the connection open and sends nothing yet. Story 6 pushes the
// engine-status readout through it; later epics add each mode's live data.
func (s *Server) handleModeEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := mode.Lookup(r.PathValue("mode")); !ok {
		http.NotFound(w, r)
		return
	}
	// The stream ends when the client goes away *or* when the server is
	// shutting down, and nothing else in the process is affected by the latter.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer context.AfterFunc(s.streamsCtx, cancel)()

	sse := datastar.NewSSE(w, r, datastar.WithContext(ctx))

	s.mu.Lock()
	s.streams++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.streams--
		s.mu.Unlock()
	}()

	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	var lastStatus, lastSaves, lastFooter string
	for {
		// Everything that changes on the page goes down this one connection.
		// Engine status shows on every mode, so it is pushed whatever m is;
		// save reports join it here, and Epic 18's world_version traffic will
		// too. Each is diffed separately so one changing does not re-patch the
		// other.
		//
		// Suppressing identical patches is not just economy: a stream that
		// emits every tick forever makes the browser's EventStream log useless
		// for debugging the busier traffic that lands on it later.
		for _, part := range []struct {
			name   string
			render func() (string, error)
			last   *string
		}{
			{"engine status", s.renderEngineStatus, &lastStatus},
			{"save reports", s.renderSaveReports, &lastSaves},
			{"save footer", s.renderFooter, &lastFooter},
		} {
			cur, err := part.render()
			if err != nil {
				slog.ErrorContext(ctx, "rendering "+part.name, "err", err)
				continue
			}
			if cur == *part.last {
				continue
			}
			if err := sse.PatchElements(cur); err != nil {
				return // client gone
			}
			*part.last = cur
		}

		select {
		case <-sse.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

// ReportSave records what became of a save and lets every open page know.
//
// The report reaches the browser on the next poll of the page-level stream
// rather than through a channel of its own: one stream per page is the
// architecture, and a second notification path would be a second thing to get
// right. Epic 12's save button is the caller.
func (s *Server) ReportSave(path string, saveErr error) {
	s.saves.Record(savereport.Observe(path, saveErr, status.Check(s.cfg.Engine).State))
}

// ClearSaveReport removes a file's report, for a caller that wants to dismiss
// it — closing the file, say.
func (s *Server) ClearSaveReport(path string) { s.saves.Clear(path) }

// footer renders the save footer from the session's real state. A project that
// failed to open has no session and therefore nothing to save.
func (s *Server) footer() templates.Component {
	if s.cfg.Session == nil {
		return templates.NoFooter()
	}
	dirty, err := s.cfg.Session.Dirty()
	if err != nil {
		// A schema that will not serialise cannot be saved, and saying "clean"
		// would be worse than saying "dirty" — at least dirty prompts a look.
		slog.Error("computing dirty state", "path", s.cfg.Session.Path(), "err", err)
		dirty = true
	}
	return templates.SchemaFooter(filepath.Base(s.cfg.Session.Path()), dirty)
}

func (s *Server) renderFooter() (string, error) {
	var buf bytes.Buffer
	c := templates.SaveFooterRegion(s.footer())
	if err := c.Render(context.Background(), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (s *Server) renderSaveReports() (string, error) {
	var buf bytes.Buffer
	c := components.SaveReports(components.SaveReportsProps{Reports: s.saves.All()})
	if err := c.Render(context.Background(), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderEngineStatus checks the database and renders the readout to a string.
// The first iteration of the stream loop runs before any tick, so a client that
// reconnects sees current state immediately rather than after a poll interval.
func (s *Server) renderEngineStatus() (string, error) {
	var buf bytes.Buffer
	c := components.EngineStatus(components.EngineStatusProps{Status: status.Check(s.cfg.Engine)})
	if err := c.Render(context.Background(), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// openStreams reports how many SSE connections are currently held open. It
// exists so the shutdown test can prove a stream was actually open rather than
// passing because the request never arrived.
func (s *Server) openStreams() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streams
}

// sameOrigin refuses a write initiated by another site.
//
// Binding to loopback is not a security boundary: a POST with no custom headers
// is a "simple request", so any page the user visits can fire one at
// 127.0.0.1:7777 with no preflight and no consent, and the side effect lands.
// These endpoints write to the user's schema.json, so that matters.
//
// Sec-Fetch-Site is sent by every browser that can make the attacking request
// in the first place. A missing header means a non-browser client — curl, a
// test — which is allowed: this is a same-origin check, not authentication.
func sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	default:
		return false
	}
}

// handleDevSchemaBump makes one real edit to the session — bumping
// schemaVersion — so the editing path can be driven end to end before Story 2
// puts controls on the page.
//
// It exercises production code rather than standing in for it: the same
// Session.Edit every real control will use. It disappears when SCHEMA mode has
// a version badge to click, and until then a browser test of the session would
// otherwise have to mock the thing under test.
func (s *Server) handleDevSchemaBump(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin write refused", http.StatusForbidden)
		return
	}
	if s.cfg.Session == nil {
		http.Error(w, "no project is open", http.StatusConflict)
		return
	}
	if err := s.cfg.Session.Edit(func(d *schema.DatabaseSchema) error {
		d.SchemaVersion++
		return nil
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// The schema actions. Each mutates the session and answers 204: the changed
// footer and the save report reach the page on the SSE stream it already holds,
// rather than as a body here. One push path, not two — the stream is already
// where every other live change arrives.
func (s *Server) handleSchemaSave(w http.ResponseWriter, r *http.Request) {
	s.runSchemaAction(w, r, func(sess *session.Session) error { return sess.Save() }, true)
}

func (s *Server) handleSchemaOverwrite(w http.ResponseWriter, r *http.Request) {
	s.runSchemaAction(w, r, func(sess *session.Session) error { return sess.SaveOverwriting() }, true)
}

func (s *Server) handleSchemaDiscard(w http.ResponseWriter, r *http.Request) {
	s.runSchemaAction(w, r, func(sess *session.Session) error { return sess.Discard() }, false)
}

func (s *Server) handleSchemaReload(w http.ResponseWriter, r *http.Request) {
	s.runSchemaAction(w, r, func(sess *session.Session) error {
		if err := sess.Reload(); err != nil {
			return err
		}
		// Taking what is on disk makes the last save outcome moot — whether it
		// succeeded or was refused, it describes a version of the file that is
		// no longer the one being edited. Leaving it up would be reporting on
		// something that no longer exists.
		s.ClearSaveReport(sess.Path())
		return nil
	}, false)
}

// runSchemaAction is the shape every editing action takes. Actions that write
// record a save report; discard and reload do not, because nothing was saved
// and reporting one would be a claim about the file that is not true.
func (s *Server) runSchemaAction(
	w http.ResponseWriter, r *http.Request,
	do func(*session.Session) error,
	reports bool,
) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin write refused", http.StatusForbidden)
		return
	}
	sess := s.cfg.Session
	if sess == nil {
		http.Error(w, "no project is open", http.StatusConflict)
		return
	}
	err := do(sess)
	if reports {
		s.ReportSave(sess.Path(), err)
	} else if err != nil {
		slog.ErrorContext(r.Context(), "schema action", "path", sess.Path(), "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
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
	// matched no other route lands here.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// The root is not a page of its own — it lands you in the default mode, so
	// there stays exactly one canonical URL per mode.
	http.Redirect(w, r, mode.Default.Path(), http.StatusFound)
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

// Shutdown stops the server. Releasing the SSE streams first is what makes it
// return: they would otherwise keep Shutdown waiting for the life of every
// open browser tab. Ordinary requests are left to finish normally.
func (s *Server) Shutdown(ctx context.Context) error {
	s.cancelStreams()
	return s.http.Shutdown(ctx)
}

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
