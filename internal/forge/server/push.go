package server

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tmbritton/ecs-db/internal/forge/eventbus"
)

// The kinds of change Forge publishes.
//
// The stream treats all of them the same today — any one means "re-render and
// diff" — so the types buy nothing yet. They are recorded from the start
// because Epic 18 is where they start mattering: a world_version tick arriving
// several times a second must not drag a schema re-render along with it, and a
// bus carrying one undifferentiated "something happened" cannot be told to.
const (
	EventSchema   = "forge.schema"
	EventMachines = "forge.machines"
	EventMaps     = "forge.maps"
	EventSaves    = "forge.saves"
	EventEngine   = "forge.engine"
	// EventChanged is the type a mutation route gets when no prefix claims it.
	//
	// A fallback rather than a panic or a silent drop: the failure mode of
	// guessing wrong here is a control that appears to do nothing, and a mode
	// added in a later epic should update the page before anyone remembers to
	// come back and name its events.
	EventChanged = "forge.changed"
)

// routeEvents names the change each family of mutation routes makes.
//
// A table rather than a switch because forgeEvents is derived from it: a mode
// added in a later epic gets a line here and is subscribed to automatically. A
// hand-kept list of what streams listen for is a list that eventually omits
// something, and the symptom of omitting one is a whole mode that silently
// stops updating.
var routeEvents = []struct{ prefix, event string }{
	// ENTS and SCHEMA are two halves of schema.json, and an edit in either
	// changes what the other renders.
	{"/forge/schema/", EventSchema},
	{"/forge/ents/", EventSchema},
	{"/forge/agents/", EventMachines},
	{"/forge/map/", EventMaps},
}

// nonRouteEvents are the kinds published from outside a mutation route: a save
// being recorded, and the engine poller noticing the game come or go.
// EventChanged is here because it is what a route no prefix claims falls back
// to, and what loading a page publishes.
var nonRouteEvents = []string{EventSaves, EventEngine, EventChanged}

// forgeEvents is what a mode stream listens for: everything a route can produce
// plus everything published outside one.
var forgeEvents = func() []string {
	seen := make(map[string]bool, len(routeEvents)+len(nonRouteEvents))
	out := make([]string, 0, len(routeEvents)+len(nonRouteEvents))
	add := func(e string) {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	for _, e := range nonRouteEvents {
		add(e)
	}
	for _, r := range routeEvents {
		add(r.event)
	}
	return out
}()

// eventTypeFor names the change a mutation route makes, from its path.
//
// Derived from the path rather than declared per handler so that the publish
// stays a single call site in sameOriginOnly. Twenty-four handlers each
// remembering to publish is twenty-four chances to forget; a prefix table is
// one place to be wrong, and being wrong here costs a more precise event type
// rather than an update.
func eventTypeFor(path string) string {
	for _, r := range routeEvents {
		if strings.HasPrefix(path, r.prefix) {
			return r.event
		}
	}
	return EventChanged
}

// publish tells every open page that something changed.
func (s *Server) publish(eventType string) {
	// Non-blocking: this runs inside the HTTP handler for a mutation, and a
	// browser tab that has stopped reading must not be able to stall someone
	// else's save. A stream that is behind does not need the event it missed —
	// it needs to know something changed, which the next event also says.
	s.cfg.Bus.PublishNonBlocking(eventbus.Event{Type: eventType})
}

// renderCount is how many times a stream has rendered its regions. It exists so
// a test can prove a burst of events collapses into one pass rather than one
// pass per event — which is not observable from the wire, because the diff
// suppresses the duplicates either way and the waste is exactly what is hidden.
func (s *Server) renderCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.renders
}

func (s *Server) countRender() {
	s.mu.Lock()
	s.renders++
	s.mu.Unlock()
}

// enginePollStarted brings the engine poller up for the first open stream and
// takes it down with the last.
//
// The engine's database is written by another process, so it is the one thing
// here that genuinely has to be asked rather than told. Everything else is
// state this server changed and therefore already knows about.
//
// Refcounted on the stream count rather than started in New for two reasons:
// with no page open there is nobody to tell, so a Forge sitting in a terminal
// does no work at all; and a poller tied to the process would outlive every
// test that builds a server, holding a database handle open past the temp
// directory it lives in.
//
// Callers hold s.mu.
func (s *Server) enginePollStartedLocked() {
	if s.pollCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(s.streamsCtx)
	s.pollCancel = cancel
	go s.pollEngine(ctx)
}

// Callers hold s.mu.
func (s *Server) enginePollStoppedLocked() {
	if s.pollCancel == nil {
		return
	}
	s.pollCancel()
	s.pollCancel = nil
}

// pollEngine watches what Forge cannot be told about: the game's database,
// written by another process, and the project's files, which anything on the
// machine may rewrite.
//
// It publishes on change, not on tick. Before this, every open tab opened the
// database on its own timer — so the cost of watching scaled with the number of
// browser tabs, and every tab paid it again whether or not anything had moved.
//
// A timer rather than fsnotify, which the engine already uses in
// internal/agent/watcher.go. Two seconds is the latency this had before, so
// matching it is not a regression; fsnotify would make it instant and is worth
// doing when something needs instant.
func (s *Server) pollEngine(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	// Seeded before the loop, not left empty: the page that opened this stream
	// already rendered current state, so a first tick that "found a change"
	// would wake every stream to send bytes nobody needs.
	lastEngine, err := s.renderEngineStatus()
	if err != nil {
		slog.ErrorContext(ctx, "rendering engine status", "err", err)
	}
	lastFiles := s.fingerprintExternal()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if cur, err := s.renderEngineStatus(); err != nil {
			slog.ErrorContext(ctx, "rendering engine status", "err", err)
		} else if cur != lastEngine {
			lastEngine = cur
			s.publish(EventEngine)
		}

		if cur := s.fingerprintExternal(); cur != lastFiles {
			lastFiles = cur
			s.publish(EventChanged)
		}
	}
}

// drain empties a subscription so that a burst of events causes one re-render
// rather than one per event. The render reads current state, so a second pass
// over the same state produces the same bytes — which the diff would suppress
// anyway, having already paid for the render.
func drain(ch <-chan eventbus.Event) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// fingerprintExternal summarises the project files on disk, cheaply enough to
// do on a timer.
//
// The two-second poll used to give Forge a file watcher by accident: every
// stream re-rendered on a tick whatever had happened, and re-rendering re-reads
// disk — MapSession.Refresh lists the map directory, the footer compares the
// session against the file, the migration preview re-reads schema.json. So a
// map added by Tiled, or a schema.json rewritten by a git checkout, appeared
// within a tick without anyone having designed for it.
//
// Removing the poll removed that, and nothing said so. This puts it back
// deliberately and once for the whole server, rather than once per open tab.
//
// Names, sizes and modification times — never contents. The point is to notice
// that something changed, not what; the render that follows does the reading.
func (s *Server) fingerprintExternal() string {
	sum := fnv.New64a()
	note := func(parts ...any) { _, _ = fmt.Fprintln(sum, parts...) }

	// A file that has gone contributes nothing, which is itself the change:
	// only consecutive fingerprints are ever compared, so an entry
	// disappearing moves the sum exactly as an entry changing does.
	stat := func(path string) {
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		note(path, info.Size(), info.ModTime().UnixNano())
	}

	// A directory's own mtime moves when an entry is added or removed, and each
	// entry's when it is written — so both are needed to tell "a new map
	// appeared" from "an open one was edited".
	walk := func(dir string) {
		if dir == "" {
			return
		}
		stat(dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		// ReadDir sorts by filename, so this is stable without sorting again.
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			stat(filepath.Join(dir, e.Name()))
		}
	}

	if s.cfg.Session != nil {
		stat(s.cfg.Session.Path())
	}
	for _, dir := range s.cfg.BehaviorDirs {
		walk(dir)
	}
	if s.cfg.MapSession != nil {
		walk(s.cfg.MapSession.Root())
	}
	return strconv.FormatUint(sum.Sum64(), 16)
}
