// Package server serves the Forge content editor over HTTP. It owns transport
// and nothing else: no domain logic, no file authoring. Config and the asset
// filesystem are injected so handler tests run against a real httptest.Server
// with no package-level state to reset.
package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/forge/machines"
	"github.com/tmbritton/ecs-db/internal/forge/migration"
	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/forge/savereport"
	"github.com/tmbritton/ecs-db/internal/forge/session"
	"github.com/tmbritton/ecs-db/internal/forge/status"
	"github.com/tmbritton/ecs-db/internal/forge/templates"
	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
	"github.com/tmbritton/ecs-db/internal/forge/usage"
	"github.com/tmbritton/ecs-db/internal/forge/validation"
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
	// Machines are the behaviour machines the project resolved, for the binding
	// dropdowns and the context-seeds panel. Empty is a legitimate state — a
	// project may have none.
	//
	// The whole Machine, not just its ID: ENTS shows the bound machine's
	// context seeds, which live in its definition, and the mod each came from,
	// which is how two mods defining one ID are told apart.
	Machines []project.Machine
	// BehaviorDirs are the mods' behaviours directories, in load order. Inline
	// validation needs them to answer the engine's own question about a
	// behaviour binding — does a machine file of that name exist — which the
	// resolved machine list cannot answer, because a file that exists and was
	// rejected is absent from it for a different reason.
	BehaviorDirs []string
	// MachineSession is the editing session for the project's behaviour
	// machines. Nil when the project could not be opened, which leaves AGENTS
	// readable and the reason visible rather than failing the whole editor.
	//
	// When it is present it is also the source of truth for the resolved
	// machine set, replacing the Machines snapshot below: Epic 12 Story 7 had
	// to word its "no machine with this id is loaded" warning around a list
	// that was read once at startup, and this is the story that can change it.
	MachineSession *machines.Session
	// Problems are the project's loading failures. They are what turns "that
	// machine did not resolve" into a sentence naming the file and the reason,
	// instead of sending the user to the log to find out what the tool already
	// knows.
	Problems []project.Problem
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
	// Why the last edit was refused. Cleared by the next one that succeeds, and
	// by a full page load, so a stale explanation never outlives the state it
	// described or leaks into a tab that did nothing wrong.
	editProblem string
	// renamedTo follows components through renames.
	//
	// A page subscribes to its stream with the component it is showing, and
	// that URL cannot change afterwards. Renaming the component therefore left
	// the stream asking for a name that no longer exists, selectComponent fell
	// back to the first component, and the editor silently swapped to a
	// different one — while the address bar still named the old. The next edit
	// then hit whatever the editor had swapped to. Following the rename keeps
	// the page pointed at what the user is actually editing.
	// One map per namespace. A component and an entity type may share a name —
	// a tag component "Player" beside an entity type "Player" is an ordinary
	// ECS idiom — and a single map cannot tell them apart, so renaming the
	// component silently retargeted the ENTS editor onto whatever entity type
	// sorted first, delete button included.
	renamedTo        map[string]string
	renamedTypeTo    map[string]string
	renamedMachineTo map[string]string

	// held is the save that was stopped to ask first, or saveNone. It records
	// which save was asked for, not the plan it would run: the modal re-reads
	// the database on every render, because the plan depends on a database
	// another process is writing and a confirmation showing a stale list is
	// the one thing worse than no confirmation at all.
	//
	// Storing the intent rather than a bare "confirming" flag is what lets the
	// confirmed save be the one that was actually requested — Save and
	// SaveOverwriting differ in whether they check for a conflicting write,
	// and answering "yes" to one must not perform the other.
	held saveKind

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
	mux.HandleFunc("POST /forge/schema/save", sameOriginOnly(s.handleSchemaSave))
	mux.HandleFunc("POST /forge/schema/save/confirm", sameOriginOnly(s.handleSchemaSaveConfirm))
	mux.HandleFunc("POST /forge/schema/save/cancel", sameOriginOnly(s.handleSchemaSaveCancel))
	mux.HandleFunc("POST /forge/schema/discard", sameOriginOnly(s.handleSchemaDiscard))
	mux.HandleFunc("POST /forge/schema/reload", sameOriginOnly(s.handleSchemaReload))
	mux.HandleFunc("POST /forge/schema/overwrite", sameOriginOnly(s.handleSchemaOverwrite))
	s.registerSchemaEditRoutes(mux)
	s.registerMachineEditRoutes(mux)
	s.registerEntsEditRoutes(mux)
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
	// Rendered server-side on load so the page is never blank before the first
	// patch arrives; the stream takes over from there.
	// A full page load starts clean: an edit refused in another tab is not this
	// page's problem to report, and neither is a confirmation it never saw.
	s.setEditProblem("")
	s.hold(saveNone)
	data := s.modeData(r)
	s.render(w, r, templates.Shell(
		m, streamQuery(r, m.Slug, data), status.Check(s.cfg.Engine), s.saves.All(), s.footer(m.Slug, data),
		content(data), s.confirmation(data)))
}

// streamQuery is the selection the page's SSE subscription has to carry, so
// that the mode content it re-renders is the one on screen.
//
// One place names the selection parameters, because the shell must not know
// which one a given mode uses — it carried ?component= alone until AGENTS
// arrived selecting with ?machine=, and the stream then re-rendered every tick
// with nothing selected.
//
// The resolved values, not the raw query: Epic 12's rename-following means the
// component the page is showing may already differ from the one its URL names,
// and the stream should follow the editor rather than the address bar.
func streamQuery(r *http.Request, slug string, data modes.Data) string {
	q := url.Values{}
	if data.Selected != "" {
		// modeData resolves ?component= first and only falls back to ?type=, so
		// the key has to be chosen the same way round or a page carrying both
		// would subscribe under the wrong namespace and follow the wrong
		// rename map.
		key := "component"
		if r.URL.Query().Get("component") == "" && r.URL.Query().Get("type") != "" {
			key = "type"
		}
		q.Set(key, data.Selected)
	}
	// Only where it means something. machineData runs for every mode, so
	// without this every page — MAP, TILES, SCHEMA — put an absolute path from
	// the developer's filesystem into its subscription URL.
	if slug == "agents" && data.SelectedMachine != "" {
		q.Set("machine", data.SelectedMachine)
	}
	return q.Encode()
}

// modeData gathers what a mode needs to render. The schema is a deep copy from
// the session, so a template cannot reach the session through it.
func (s *Server) modeData(r *http.Request) modes.Data {
	// Which namespace the name is in, taken from the parameter it arrived
	// under rather than guessed from the mode: the ENTS page subscribes its
	// stream with ?component= (shell.go builds one selection parameter for
	// every mode), so the mode is not a reliable indicator and the parameter
	// is.
	selected, kind := r.URL.Query().Get("component"), renameComponentKind
	if selected == "" {
		if t := r.URL.Query().Get("type"); t != "" {
			selected, kind = t, renameTypeKind
		}
	}
	data := modes.Data{Selected: s.followRenames(selected, kind)}
	if s.cfg.Session == nil {
		return data
	}
	data.HasSession = true
	s.cfg.Session.Read(func(d schema.DatabaseSchema) { data.Schema = d })
	data.Machines, data.MachineProblems = s.resolvedMachines()
	// Computed for every mode, not just the two that edit the file: the save
	// footer is in the shell and is on screen everywhere, and a Save button
	// that only knows it would be refused while SCHEMA happens to be open is
	// worse than one that never knew.
	data.Validation = validation.Check(validation.Input{
		Schema:       data.Schema,
		BehaviorDirs: s.cfg.BehaviorDirs,
		Machines:     data.Machines,
		Problems:     data.MachineProblems,
	})
	data.SelectedMachine, data.DirtyMachines, data.ReformatMachines = s.machineData(r)
	slug := modeSlug(r)
	if s.cfg.MachineSession != nil {
		data.HasMachines = true
		data.MachineMods = s.cfg.MachineSession.ModsThatCanHold()
		// Only where they are read, like the migration preview and the usage
		// counts below. Validating a machine and proposing a free id both cost
		// a walk of the behaviours directories, and no mode but AGENTS renders
		// either.
		if slug == "agents" {
			// One call, and the definition comes out of it. Reading the
			// machine separately would take the session lock twice, and the
			// panel maps context keys from one read to components from the
			// other — so a key added in between would render with no component
			// beside it.
			data.Inspection = s.machineInspection(data.SelectedMachine)
			data.Machine = data.Inspection.Definition
			data.StrandedMachines = strandedSet(s.cfg.MachineSession)
			data.NewMachineID = s.cfg.MachineSession.FreeID("NewMachine")
		}
	}
	data.Problem = s.lastEditProblem()
	data.Confirming = s.isConfirming()
	// The preview costs a database open and a full introspection, so it is
	// computed only where it is read: the panel is SCHEMA's, and the
	// confirmation can be up on any mode.
	// Both cost a read-only open of the game's database, so they are computed
	// only where they are read: the migration panel is SCHEMA's, the
	// confirmation can be up on any mode, and the usage counts belong to the
	// two modes that edit schema.json.
	if slug == "schema" || data.Confirming {
		data.Migration = s.migrationPreview()
	}
	if slug == "schema" || slug == "ents" {
		// The component the panel is showing, so its table is counted in the
		// same visit rather than in a second one.
		component := ""
		if slug == "schema" {
			component = selectedComponent(data)
		}
		data.Counts = usage.Read(s.cfg.Engine.DBPath, component).
			AgainstVersion(data.Schema.SchemaVersion)
	}
	return data
}

// resolvedMachines is the project's machines as they are now.
//
// From the editing session when there is one, which re-resolves after every
// create, rename and delete — so a machine authored in Forge is bindable in ENTS
// immediately rather than after a restart. The Config snapshot is the fallback
// for a server built without a session, which is every handler test that does
// not care.
func (s *Server) resolvedMachines() ([]project.Machine, []project.Problem) {
	if s.cfg.MachineSession == nil {
		return s.cfg.Machines, s.cfg.Problems
	}
	// The session's list, not both: Config.Problems is the same list taken at
	// startup, so unioning them reported every broken file twice.
	return s.cfg.MachineSession.Machines(), s.cfg.MachineSession.Problems()
}

// selectedComponent resolves which component the SCHEMA panel is showing, so
// the count is taken for the same one the page renders.
func selectedComponent(data modes.Data) string {
	if _, ok := data.Schema.Components[data.Selected]; ok {
		return data.Selected
	}
	if names := modes.ComponentNames(data.Schema); len(names) > 0 {
		return names[0]
	}
	return ""
}

// modeSlug names the mode a request is for.
//
// PathValue is empty unless the request went through the route pattern that
// declared {mode}, which is true of every request in production and of none
// rendered directly — so the URL is the fallback, and the two agree.
func modeSlug(r *http.Request) string {
	if m := r.PathValue("mode"); m != "" {
		return m
	}
	rest := strings.TrimPrefix(r.URL.Path, "/forge/")
	slug, _, _ := strings.Cut(rest, "/")
	return slug
}

// migrationPreview asks what the engine would do to the database on its next
// start. Recomputed on every render and never cached — the database belongs to
// another process, and a warning that is out of date is a warning that is
// wrong.
func (s *Server) migrationPreview() migration.Preview {
	sess := s.cfg.Session
	if sess == nil {
		return migration.Preview{Reason: "no project is open"}
	}
	snapshot, err := sess.Snapshot()
	if err != nil {
		// The saved file no longer parses. Only the comparison against it is
		// lost — the diff against the database still holds — but the missing
		// half has to be reported as missing rather than as a version of zero,
		// which Stale() would read as "not stale" and quietly say nothing.
		//
		// Debug rather than Error: this runs on every render of every open
		// stream, and a broken schema.json would otherwise fill the log at the
		// poll interval.
		slog.Debug("parsing the last saved schema", "path", sess.Path(), "err", err)
	}
	var current schema.DatabaseSchema
	sess.Read(func(d schema.DatabaseSchema) { current = d })
	p := migration.Check(s.cfg.Engine.DBPath, current, snapshot)
	p.SnapshotUnknown = err != nil
	return p
}

// saveKind names which save is waiting on an answer.
type saveKind int

const (
	saveNone saveKind = iota
	saveNormal
	saveOverwrite
)

// hold records that a save is waiting to be confirmed.
func (s *Server) hold(k saveKind) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held = k
}

// takeHeld returns the waiting save and clears it in one step.
//
// One step, deliberately: a check followed by a separate clear lets two clicks
// on "Save anyway" both see the hold and both save, and lets a POST that
// arrives with nothing held save anyway. This is the gate the whole story
// rests on, so it cannot be a read and a write with a gap in between.
func (s *Server) takeHeld() saveKind {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.held
	s.held = saveNone
	return k
}

func (s *Server) isConfirming() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.held != saveNone
}

// handleModeEvents is the single SSE subscription a mode page opens.
// Everything that updates live on that page is pushed down this one stream —
// one per page, not one per widget, because browsers cap concurrent
// connections per origin.
//
// It holds the connection open and sends nothing yet. Story 6 pushes the
// engine-status readout through it; later epics add each mode's live data.
func (s *Server) handleModeEvents(w http.ResponseWriter, r *http.Request) {
	m, ok := mode.Lookup(r.PathValue("mode"))
	if !ok {
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

	var lastStatus, lastSaves, lastFooter, lastContent, lastConfirm string
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
		// Gathered once per tick and shared by the two regions that read it.
		// It costs a read-only open of the game's database — two, on SCHEMA —
		// and both regions asking separately doubled that on every tick of
		// every open stream.
		var gathered *modes.Data
		data := func() modes.Data {
			if gathered == nil {
				d := s.modeData(r)
				gathered = &d
			}
			return *gathered
		}

		for _, part := range []struct {
			name   string
			render func() (string, error)
			last   *string
		}{
			{"engine status", s.renderEngineStatus, &lastStatus},
			{"save reports", s.renderSaveReports, &lastSaves},
			{"save footer", func() (string, error) { return s.renderFooter(m.Slug, data()) }, &lastFooter},
			{"mode content", func() (string, error) { return s.renderModeContent(m, data()) }, &lastContent},
			{"save confirmation", func() (string, error) { return s.renderConfirmRegion(data()) }, &lastConfirm},
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
// footer builds the save footer for the mode on screen.
//
// Mode-aware because one Save button must do one thing: on AGENTS it saves
// machines, everywhere else it saves schema.json. Unsaved work in the other
// place is not hidden — it is reported alongside, so switching modes cannot
// make it disappear.
func (s *Server) footer(slug string, data modes.Data) templates.Component {
	if slug == "agents" && s.cfg.MachineSession != nil {
		return s.machinesFooter(data)
	}
	if s.cfg.Session == nil {
		return templates.NoFooter()
	}
	elsewhere := s.unsavedMachines()
	dirty, err := s.cfg.Session.Dirty()
	if err != nil {
		// A schema that will not serialise cannot be saved, and saying "clean"
		// would be worse than saying "dirty" — at least dirty prompts a look.
		slog.Error("computing dirty state", "path", s.cfg.Session.Path(), "err", err)
		dirty = true
	}
	return templates.SchemaFooter(
		filepath.Base(s.cfg.Session.Path()), dirty, data.Validation, elsewhere)
}

// unsavedMachines counts machine edits that this footer cannot save, so
// switching modes cannot hide them.
//
// Reformat-only differences are not counted: nobody edited those, and a footer
// that reported them as pending work would be crying wolf on every project
// whose files were not written by Forge.
func (s *Server) unsavedMachines() int {
	if s.cfg.MachineSession == nil {
		return 0
	}
	changes, err := s.cfg.MachineSession.Changes()
	if err != nil {
		return 0
	}
	n := 0
	for _, c := range changes {
		if !c.Reformatting {
			n++
		}
	}
	return n
}

// unsavedSchema reports whether schema.json has edits the AGENTS footer cannot
// save. Same reasoning in the other direction.
func (s *Server) unsavedSchema() bool {
	if s.cfg.Session == nil {
		return false
	}
	dirty, err := s.cfg.Session.Dirty()
	if err != nil {
		// A schema that will not serialise cannot be saved, and saying "clean"
		// would be worse than saying "dirty".
		return true
	}
	return dirty
}

// machinesFooter names how many machines are unsaved, and which.
func (s *Server) machinesFooter(data modes.Data) templates.Component {
	return templates.MachinesFooter(
		machinesFooterFile(data.DirtyMachines, data.ReformatMachines),
		len(data.DirtyMachines) > len(data.ReformatMachines),
		dirtyNames(data.DirtyMachines),
		s.unsavedSchema(),
	)
}

// confirmation renders the held-save dialog, or nil when nothing is held.
//
// nil rather than an empty component: the shell renders the region either way,
// and "is there a dialog" is what decides whether the rest of the shell is
// inert.
func (s *Server) confirmation(data modes.Data) templates.Component {
	if !data.Confirming || !data.Migration.Holds() {
		return nil
	}
	return components.MigrationConfirm(components.MigrationConfirmProps{
		Preview:      data.Migration,
		CancelAction: "@post('/forge/schema/save/cancel')",
		SaveAction:   "@post('/forge/schema/save/confirm')",
	})
}

// renderConfirmRegion renders the dialog's region for the SSE stream.
func (s *Server) renderConfirmRegion(data modes.Data) (string, error) {
	var buf bytes.Buffer
	c := templates.SaveConfirmRegion(s.confirmation(data))
	if err := c.Render(context.Background(), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (s *Server) renderFooter(slug string, data modes.Data) (string, error) {
	var buf bytes.Buffer
	c := templates.SaveFooterRegion(s.footer(slug, data))
	if err := c.Render(context.Background(), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderModeContent re-renders the open mode so an edit appears without a
// reload. The selection comes from the events request's own query string, which
// the page put there when it subscribed.
// renameKind names which of the two namespaces a rename belongs to.
type renameKind int

const (
	renameComponentKind renameKind = iota
	renameTypeKind
	// renameMachineKind follows a machine's *file*, which is what a page's
	// stream subscribes with. Renaming the file moves that path, and without a
	// trail the stream asks for one that no longer exists, the fallback picks
	// the first machine, and the editor silently swaps to a different one — the
	// same failure component renames had, in a third namespace.
	renameMachineKind
)

// renames returns the map for one namespace. Caller holds s.mu.
func (s *Server) renames(kind renameKind) map[string]string {
	switch kind {
	case renameTypeKind:
		return s.renamedTypeTo
	case renameMachineKind:
		return s.renamedMachineTo
	default:
		return s.renamedTo
	}
}

// followRenames resolves a name through any renames since the page subscribed.
// Chained renames follow all the way, with a bound so a cycle cannot spin.
func (s *Server) followRenames(name string, kind renameKind) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	renamed := s.renames(kind)
	for range len(renamed) + 1 {
		next, ok := renamed[name]
		if !ok || next == name {
			return name
		}
		name = next
	}
	return name
}

// recordRename notes that a name moved, so pages still naming the old one
// follow it rather than silently landing on someone else's.
//
// Per namespace: components and entity types are separate name spaces in
// schema.json and may collide, so one shared map made a component rename
// retarget the entity-type editor.
func (s *Server) recordRename(kind renameKind, from, to string) {
	if from == to {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch kind {
	case renameTypeKind:
		if s.renamedTypeTo == nil {
			s.renamedTypeTo = map[string]string{}
		}
		s.renamedTypeTo[from] = to
	case renameMachineKind:
		if s.renamedMachineTo == nil {
			s.renamedMachineTo = map[string]string{}
		}
		s.renamedMachineTo[from] = to
	default:
		if s.renamedTo == nil {
			s.renamedTo = map[string]string{}
		}
		s.renamedTo[from] = to
	}
}

// forgetRenames drops the rename trail.
//
// Taking what is on disk — discard or reload — undoes the renames themselves,
// so a trail still pointing at the new names would send every page to a name
// that no longer exists and from there to whatever sorts first.
func (s *Server) forgetRenames() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.renamedTo = nil
	s.renamedTypeTo = nil
	s.renamedMachineTo = nil
}

func (s *Server) renderModeContent(m mode.Mode, data modes.Data) (string, error) {
	build, ok := modes.Registry[m.Slug]
	if !ok {
		return "", fmt.Errorf("no content registered for mode %q", m.Slug)
	}
	var buf bytes.Buffer
	c := templates.ModeContentRegion(build(data))
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

// The schema actions. Each mutates the session and answers 204: the changed
// footer and the save report reach the page on the SSE stream it already holds,
// rather than as a body here. One push path, not two — the stream is already
// where every other live change arrives.
// handleSchemaSave writes unless the engine would destroy data doing so, in
// which case it writes nothing and puts the decision on screen.
//
// The check is here rather than in the button because a confirmation the
// client can skip is not a confirmation. Nothing has been written when this
// returns; the modal's own action is what saves.
func (s *Server) handleSchemaSave(w http.ResponseWriter, r *http.Request) {
	if s.holdForConfirmation(w, saveNormal) {
		return
	}
	s.runSchemaAction(w, r, func(sess *session.Session) error { return sess.Save() }, true)
}

// holdForConfirmation stops a save that has to be asked about first, and
// reports whether it did.
//
// Every route that writes goes through this. "Keep mine" resolving a conflict
// drops just as many columns as an ordinary save, and a check on one of three
// save routes is not a check.
func (s *Server) holdForConfirmation(w http.ResponseWriter, k saveKind) bool {
	if s.cfg.Session == nil || !s.migrationPreview().Holds() {
		s.hold(saveNone)
		return false
	}
	s.hold(k)
	// 204 with nothing patched: the page's stream re-renders the shell and the
	// dialog arrives there, the same way every other change does.
	w.WriteHeader(http.StatusNoContent)
	return true
}

// handleSchemaSaveConfirm is the answer to the confirmation, and the only way
// a destructive save happens. There is deliberately no "don't ask again":
// dropping a column is not a routine confirmation to train someone out of.
func (s *Server) handleSchemaSaveConfirm(w http.ResponseWriter, r *http.Request) {
	// The answer to a question nobody asked is not consent. Without this, a
	// POST straight to this route saves destructively having shown no dialog
	// at all — and, more likely in practice, the still-visible "Save anyway"
	// button saves after Cancel, in the window before the next poll removes
	// the dialog from the page.
	switch s.takeHeld() {
	case saveNormal:
		s.runSchemaAction(w, r, func(sess *session.Session) error { return sess.Save() }, true)
	case saveOverwrite:
		s.runSchemaAction(w, r, func(sess *session.Session) error { return sess.SaveOverwriting() }, true)
	default:
		// Nothing was waiting. Not an error the user needs to see: the likely
		// cause is a second click on a dialog that has already been answered.
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleSchemaSaveCancel writes nothing. Not "undoes the save" — the save
// never happened, and the working value is untouched.
func (s *Server) handleSchemaSaveCancel(w http.ResponseWriter, _ *http.Request) {
	s.takeHeld()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSchemaOverwrite(w http.ResponseWriter, r *http.Request) {
	if s.holdForConfirmation(w, saveOverwrite) {
		return
	}
	s.runSchemaAction(w, r, func(sess *session.Session) error { return sess.SaveOverwriting() }, true)
}

func (s *Server) handleSchemaDiscard(w http.ResponseWriter, r *http.Request) {
	s.runSchemaAction(w, r, func(sess *session.Session) error {
		if err := sess.Discard(); err != nil {
			return err
		}
		// The renames are undone, so the trail through them points at names
		// that no longer exist — and from there to whatever sorts first.
		s.forgetRenames()
		return nil
	}, false)
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
		s.forgetRenames()
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
	// The origin check is middleware — see sameOriginOnly.
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
