package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tmbritton/ecs-db/internal/forge/eventbus"
	"github.com/tmbritton/ecs-db/internal/forge/maps"
	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/session"
	"github.com/tmbritton/ecs-db/internal/forge/status"
)

// pushServer is a server whose engine poller will not fire during the test.
//
// An hour, not a short interval: these tests are about the push path, and one
// that only passed because a tick happened to arrive would be a test of the
// thing this story removes.
func pushServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(sessionSchema), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	sess, err := session.Open(path)
	if err != nil {
		t.Fatalf("session.Open: %v", err)
	}
	s := New(Config{
		Addr:         "127.0.0.1:0",
		Session:      sess,
		PollInterval: time.Hour,
		Engine:       status.Config{},
	}, testFS())
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	return srv, s
}

// The point of the whole story. Before this, a save reached the browser when the
// 2-second ticker next fired — ~1s on average — and the only reason the UI ever
// updated was that a poll came round.
func TestStream_AnEditArrivesWithoutWaitingForATick(t *testing.T) {
	srv, _ := pushServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events")

	// Drain the connect burst so the frame asserted below can only be the edit.
	awaitFrame(t, ch, "save-footer", "on connect")
	drainFrames(ch, 200*time.Millisecond)

	start := time.Now()
	if code := post(t, srv, "/forge/schema/version"); code != 204 {
		t.Fatalf("bumping the version = %d, want 204", code)
	}

	awaitFrameWithin(t, ch, "● unsaved", "after an edit", 2*time.Second)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("the edit took %v to reach the stream — that is a poll, not a push", elapsed)
	}
}

// A stream that emits every tick forever makes the browser's EventStream log
// useless for debugging the traffic that actually matters, and it was doing
// exactly that before the first identical-patch suppression landed. Now there
// is no tick at all, so silence should be total.
func TestStream_SendsNothingWhileNothingChanges(t *testing.T) {
	srv, _ := pushServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events")

	awaitFrame(t, ch, "save-footer", "on connect")
	drainFrames(ch, 200*time.Millisecond)

	select {
	case f, ok := <-ch:
		if ok {
			t.Errorf("an idle stream sent a frame: %.120s", f)
		}
	case <-time.After(700 * time.Millisecond):
	}
}

// Forge subscribes once per SSE stream, which is once per page load. A bus that
// keeps a subscriber for a tab that closed does not fail visibly — it turns
// every later save into a wait, once the abandoned channel's buffer fills.
func TestStream_ReleasesItsSubscriptionWhenTheClientGoesAway(t *testing.T) {
	srv, s := pushServer(t)

	for range 5 {
		ctx, cancel := context.WithCancel(context.Background())
		ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events")
		awaitFrame(t, ch, "save-footer", "on connect")
		cancel()
	}

	deadline := time.After(2 * time.Second)
	for {
		if s.cfg.Bus.Subscribers(EventSchema) == 0 && s.openStreams() == 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("after 5 closed streams the bus still holds %d subscribers and %d streams are open",
				s.cfg.Bus.Subscribers(EventSchema), s.openStreams())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// One re-render for a burst, not one per event. The render reads current state,
// so a second pass over the same state can only produce the same bytes — which
// the diff would suppress anyway, having already paid for the render.
func TestStream_CoalescesABurstIntoOneRender(t *testing.T) {
	srv, s := pushServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events")
	awaitFrame(t, ch, "save-footer", "on connect")
	drainFrames(ch, 200*time.Millisecond)

	before := s.renderCount()
	for range 20 {
		s.publish(EventSchema)
	}

	time.Sleep(300 * time.Millisecond)
	if got := s.renderCount() - before; got > 3 {
		t.Errorf("20 events caused %d render passes, want a handful", got)
	}
}

// Every mutation route goes through sameOriginOnly and nothing else does, which
// is why the publish lives there: a route added later cannot forget to push
// without also forgetting the origin check.
func TestEventTypeForPath(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"/forge/schema/field", EventSchema},
		{"/forge/schema/save", EventSchema},
		{"/forge/ents/component", EventSchema},
		{"/forge/agents/state", EventMachines},
		{"/forge/agents/save", EventMachines},
		{"/forge/map/save", EventMaps},
		{"/forge/map/discard", EventMaps},
		// A route under a prefix nobody has taught this function about still
		// wakes every stream, because the alternative is a control that
		// silently stops updating the page.
		{"/forge/sprites/slice", EventChanged},
		{"/forge/tiles/collision", EventChanged},
	} {
		if got := eventTypeFor(tc.path); got != tc.want {
			t.Errorf("eventTypeFor(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// The stream must listen for every type the server can publish, or a whole mode
// stops updating — silently, which is this app's characteristic failure.
//
// Asserted against what eventTypeFor can actually return rather than against a
// list written out here. A test that keeps its own copy of the answer is a
// third place to forget, and it goes on passing while the thing it is named for
// is broken.
func TestStreamSubscribesToEveryEventTypeTheServerPublishes(t *testing.T) {
	subscribed := make(map[string]bool, len(forgeEvents))
	for _, typ := range forgeEvents {
		subscribed[typ] = true
	}

	for _, r := range routeEvents {
		// Through eventTypeFor, so a prefix the table lists but the function
		// cannot reach is caught too.
		if got := eventTypeFor(r.prefix + "anything"); !subscribed[got] {
			t.Errorf("routes under %q publish %q, which no stream subscribes to", r.prefix, got)
		}
	}
	if got := eventTypeFor("/forge/a-mode-nobody-has-written-yet/edit"); !subscribed[got] {
		t.Errorf("an unclaimed route publishes %q, which no stream subscribes to", got)
	}
	for _, typ := range nonRouteEvents {
		if !subscribed[typ] {
			t.Errorf("%q is published outside a request, and no stream subscribes to it", typ)
		}
	}
}

// Every existing test builds a Config without one, and a nil bus would panic on
// the first mutation rather than on the line that forgot it.
func TestNew_DefaultsTheBus(t *testing.T) {
	if s := New(Config{Addr: "127.0.0.1:0"}, testFS()); s.cfg.Bus == nil {
		t.Error("a Config with no Bus produced a server with no bus")
	}
}

// A bus supplied by the caller is the one used, because cmd/ecs-db builds it and
// later epics will have more than one consumer of it.
func TestNew_KeepsTheBusItWasGiven(t *testing.T) {
	bus := eventbus.New(nil)
	if s := New(Config{Addr: "127.0.0.1:0", Bus: bus}, testFS()); s.cfg.Bus != bus {
		t.Error("New replaced the bus it was given")
	}
}

func drainFrames(ch <-chan string, within time.Duration) {
	deadline := time.After(within)
	for {
		select {
		case <-ch:
		case <-deadline:
			return
		}
	}
}

// The poller exists to notice the engine coming and going. It must not mistake
// "I have not looked before" for "something changed" — an unseeded poller
// publishes on its first tick, which wakes every open stream to render and diff
// a page nothing has touched.
func TestEnginePoller_SaysNothingWhileTheEngineIsUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(sessionSchema), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	sess, err := session.Open(path)
	if err != nil {
		t.Fatalf("session.Open: %v", err)
	}
	// Short enough that several tick inside the window below, so silence means
	// the poller looked and stayed quiet rather than that it never ran.
	s := New(Config{Addr: "127.0.0.1:0", Session: sess, PollInterval: 20 * time.Millisecond}, testFS())
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)

	// Watched on the bus, not on the wire. A spurious publish does not reach
	// the browser — the stream wakes, re-renders, finds the same bytes and
	// suppresses the patch — so asserting on frames would pass while the
	// server woke every open stream on a timer, which is the thing this story
	// removed.
	watch := s.cfg.Bus.Subscribe(EventEngine)
	defer s.cfg.Bus.Unsubscribe(watch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events") // starts the poller
	awaitFrame(t, ch, "save-footer", "on connect")

	select {
	case e := <-watch:
		t.Errorf("the poller published %q with the engine unchanged", e.Type)
	case <-time.After(600 * time.Millisecond):
	}
}

// Nobody watching, nothing to watch for. The poller opens the game's database
// on every tick, so one left running after the last tab closed is a file handle
// and a wakeup bought for an audience of none.
func TestEnginePoller_StopsWithTheLastStream(t *testing.T) {
	srv, s := pushServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events")
	awaitFrame(t, ch, "save-footer", "on connect")

	s.mu.Lock()
	running := s.pollCancel != nil
	s.mu.Unlock()
	if !running {
		t.Fatal("no poller was started for an open stream")
	}

	cancel()

	deadline := time.After(2 * time.Second)
	for {
		s.mu.Lock()
		stopped := s.pollCancel == nil
		s.mu.Unlock()
		if stopped {
			return
		}
		select {
		case <-deadline:
			t.Fatal("the poller outlived the last stream")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// Loading a page is a mutation, because three of the things it resets are
// per-server and every open page renders them: the refusal banner, the
// right-click menu, and the save held for confirmation.
//
// It is a GET, so sameOriginOnly does not wrap it and publishes nothing on its
// behalf. Under the poll that was invisible — every stream re-rendered on a
// timer regardless — so this is a way for the change to have made things worse
// rather than better, and the failure is silent and permanent: tab A's save
// confirmation stays on screen with buttons that no longer do anything.
func TestPageLoad_TellsTheOtherTabsItResetTheirSharedState(t *testing.T) {
	srv, s := pushServer(t)

	// SCHEMA rather than the default mode: pushServer opens a schema session and
	// no map session, so MAP renders its "no project" stub and has nowhere to
	// show a refusal.
	const at = "/forge/schema"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := frames(t, ctx, srv.URL+at+"/events")
	awaitFrame(t, ch, "save-footer", "on connect")
	drainFrames(ch, 200*time.Millisecond)

	// Something on screen in this tab that a page load elsewhere will clear.
	// Set and published the way a refusing route does it — the route records
	// the problem and sameOriginOnly publishes on the way out.
	s.setEditProblem("the engine will not have that")
	s.publish(EventSchema)
	awaitFrame(t, ch, "the engine will not have that", "the refusal")

	// Another tab opens.
	if _, body := get(t, srv, at); body == "" {
		t.Fatal("the second page rendered nothing")
	}

	f := awaitFrameWithin(t, ch, `id="mode-main"`, "after a page load elsewhere", 2*time.Second)
	if strings.Contains(f, "the engine will not have that") {
		t.Error("the refusal is still on the page after a load elsewhere cleared it server-side")
	}
}

// The two-second poll gave Forge a file watcher by accident: every stream
// re-rendered on a tick whatever had happened, and re-rendering re-reads disk.
// So a map added by Tiled, or a schema.json rewritten by a git checkout, showed
// up within a tick without anyone having designed for it.
//
// Removing the poll removed that too, and nothing said so — an idle Forge would
// have gone on showing a project that no longer existed. This is the deliberate
// version, and it is here because the accidental one had no test to inherit.
func TestExternalChange_AFileRewrittenOnDiskReachesAnIdlePage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(sessionSchema), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	sess, err := session.Open(path)
	if err != nil {
		t.Fatalf("session.Open: %v", err)
	}
	s := New(Config{Addr: "127.0.0.1:0", Session: sess, PollInterval: 20 * time.Millisecond}, testFS())
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)

	watch := s.cfg.Bus.Subscribe(EventChanged)
	defer s.cfg.Bus.Unsubscribe(watch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := frames(t, ctx, srv.URL+"/forge/schema/events") // starts the poller
	awaitFrame(t, ch, "save-footer", "on connect")

	// Somebody else's editor, or a git checkout.
	changed := strings.Replace(sessionSchema, `"schemaVersion": 3`, `"schemaVersion": 9`, 1)
	if changed == sessionSchema {
		t.Fatal("the fixture no longer contains the version this rewrites")
	}
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatalf("rewriting: %v", err)
	}

	select {
	case <-watch:
	case <-time.After(3 * time.Second):
		t.Error("a file rewritten on disk never reached the open page")
	}
}

// And the fingerprint must not report a change that did not happen, or the
// poller is a two-second tick wearing a different name.
func TestExternalChange_IsSilentWhileNothingOnDiskMoves(t *testing.T) {
	srv, s := pushServer(t)
	_ = srv

	first := s.fingerprintExternal()
	for range 5 {
		if got := s.fingerprintExternal(); got != first {
			t.Fatalf("the fingerprint moved with nothing touched: %q then %q", first, got)
		}
	}
}

// net/http recovers a panicking handler, so a handler that mutated and then
// panicked would leave the change made and nobody told — permanently, now that
// there is no poll behind it to clean up after.
func TestSameOriginOnly_TellsThePagesEvenWhenTheHandlerPanics(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	watch := s.cfg.Bus.Subscribe(EventSchema)
	defer s.cfg.Bus.Unsubscribe(watch)

	h := s.sameOriginOnly(func(http.ResponseWriter, *http.Request) {
		panic("a handler that got halfway")
	})
	func() {
		// Standing in for net/http's own recovery, so the panic does not take
		// the test with it.
		defer func() { _ = recover() }()
		h(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/forge/schema/field", nil))
	}()

	select {
	case <-watch:
	default:
		t.Error("a handler that panicked after mutating told no open page")
	}
}

// Every place the fingerprint has to look, asserted one at a time. Watching
// only schema.json would leave a Forge that never notices Tiled adding a map —
// which is the case the accidental file-watching covered and the one most
// likely to be missed when re-adding it deliberately.
func TestFingerprintExternal_NoticesEveryPlaceAProjectLives(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(sessionSchema), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	sess, err := session.Open(schemaPath)
	if err != nil {
		t.Fatalf("session.Open: %v", err)
	}

	behaviors := filepath.Join(dir, "behaviors")
	mapRoot := filepath.Join(dir, "maps")
	for _, d := range []string{behaviors, mapRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	s := New(Config{
		Addr:         "127.0.0.1:0",
		Session:      sess,
		BehaviorDirs: []string{behaviors},
		MapSession:   maps.Open(maps.Config{Root: mapRoot}),
	}, testFS())

	for _, tc := range []struct {
		name string
		do   func(t *testing.T)
	}{
		{"schema.json rewritten", func(t *testing.T) {
			body := strings.Replace(sessionSchema, `"schemaVersion": 3`, `"schemaVersion": 9`, 1)
			if body == sessionSchema {
				t.Fatal("the fixture no longer contains the version this rewrites")
			}
			if err := os.WriteFile(schemaPath, []byte(body), 0o600); err != nil {
				t.Fatalf("rewriting: %v", err)
			}
		}},
		{"a behaviour appears", func(t *testing.T) {
			write(t, filepath.Join(behaviors, "new.json"), `{"id":"new","initial":"idle","states":{"idle":{}}}`)
		}},
		{"a behaviour is edited", func(t *testing.T) {
			write(t, filepath.Join(behaviors, "new.json"), `{"id":"new","initial":"go","states":{"go":{}}}`)
		}},
		{"a map appears", func(t *testing.T) {
			write(t, filepath.Join(mapRoot, "level2.tmx"), "<map></map>")
		}},
		{"a map is removed", func(t *testing.T) {
			if err := os.Remove(filepath.Join(mapRoot, "level2.tmx")); err != nil {
				t.Fatalf("removing: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := s.fingerprintExternal()
			tc.do(t)
			if after := s.fingerprintExternal(); after == before {
				t.Errorf("the fingerprint did not move when %s", tc.name)
			}
		})
	}
}

// A project's worth of machines saved at once is one change, not one per
// machine. Publishing per machine puts a hundred events into every open
// stream's hundred-slot buffer to say a thing one event says — overflowing it,
// and logging dropped events, for an ordinary Save All.
func TestReportSaves_IsOneEventForTheWholeSet(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	watch := s.cfg.Bus.Subscribe(EventSaves)
	defer s.cfg.Bus.Unsubscribe(watch)

	paths := make([]string, 20)
	for i := range paths {
		paths[i] = "/p/machine" + strconv.Itoa(i) + ".json"
	}
	s.ReportSaves(paths, make([]error, len(paths)))

	if got := len(watch); got != 1 {
		t.Errorf("saving %d machines published %d events, want 1", len(paths), got)
	}
}

func TestClearSaveReports_IsOneEventForTheWholeSet(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	watch := s.cfg.Bus.Subscribe(EventSaves)
	defer s.cfg.Bus.Unsubscribe(watch)

	s.ClearSaveReports([]string{"/p/a.json", "/p/b.json", "/p/c.json"})

	if got := len(watch); got != 1 {
		t.Errorf("clearing 3 reports published %d events, want 1", got)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
