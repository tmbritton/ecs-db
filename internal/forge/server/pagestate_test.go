package server

import (
	"context"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What a page has selected is that page's business, not the server's.
//
// It used to live in the query string, which made selecting a statechart node a
// full page load — and the alternative, a signal, does not work here: the
// selection decides what the *inspector* renders, so the server has to know it,
// and a stream whose URL was fixed at page load would keep re-rendering the
// inspector for whatever was selected when the page opened.
func TestPageState_RemembersWhatEachPageHasSelected(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	a, b := s.newPage(), s.newPage()
	if a == b {
		t.Fatal("two pages were given the same id")
	}

	s.setPageSel(a, "state:idle")
	s.setPageSel(b, "edge:idle-GO-0")

	if got := s.pageSel(a); got != "state:idle" {
		t.Errorf("page a has %q selected", got)
	}
	if got := s.pageSel(b); got != "edge:idle-GO-0" {
		t.Errorf("page b has %q selected — one tab's selection reached another", got)
	}
}

func TestPageState_AnUnknownPageHasNothingSelected(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	if got := s.pageSel(""); got != "" {
		t.Errorf("the empty page id has %q selected", got)
	}
	if got := s.pageSel("never-issued"); got != "" {
		t.Errorf("an unissued page id has %q selected", got)
	}
	// And nothing recreates one. Both ways in refuse: recording a selection, so
	// a stale tab cannot grow the store back after its record expired; and
	// opening a stream, so that hitting the events route with an id off the end
	// of a keyboard does not put an entry in the store for it.
	s.setPageSel("never-issued", "state:idle")
	if got := s.pageSel("never-issued"); got != "" {
		t.Errorf("recording against an unissued id created it: %q", got)
	}
	s.streamOpened("never-issued")
	if got := s.streamsOn("never-issued"); got != 0 {
		t.Errorf("opening a stream on an unissued id created it: %d streams", got)
	}
	s.selections.mu.Lock()
	held := len(s.selections.m)
	s.selections.mu.Unlock()
	if held != 0 {
		t.Errorf("the store holds %d records for pages it never issued", held)
	}
}

// A stream closing is not a page ending, and treating it as one bricks the tab.
//
// Datastar opens its stream with openWhenHidden false, so switching to another
// browser tab aborts it and switching back reconnects with the same page id. An
// earlier version deleted the record on close: the reconnect then found
// nothing, every later selection was recorded against a page the server had
// forgotten, and clicking a node answered 204 and changed nothing for the life
// of the tab — with no error anywhere.
func TestPageState_SurvivesTheStreamGoingAwayAndComingBack(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	id := s.newPage()
	s.streamOpened(id)
	s.setPageSel(id, "state:idle")

	s.streamClosed(id) // the tab is hidden
	s.streamOpened(id) // and comes back

	if got := s.pageSel(id); got != "state:idle" {
		t.Errorf("the page lost its selection across a reconnect: %q", got)
	}
	s.setPageSel(id, "state:combat")
	if got := s.pageSel(id); got != "state:combat" {
		t.Errorf("the page stopped accepting selections after a reconnect: %q", got)
	}
}

// And a second reader of the subscription URL — a replayed request, a copy of
// the link in another tab — must not be able to take a live page's selection
// away by hanging up.
func TestPageState_SurvivesASecondReaderHangingUp(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	id := s.newPage()
	s.streamOpened(id)
	s.setPageSel(id, "state:idle")

	s.streamOpened(id) // somebody else opens the same URL
	s.streamClosed(id) // and lets go

	s.setPageSel(id, "state:combat")
	if got := s.pageSel(id); got != "state:combat" {
		t.Errorf("a second reader hanging up took the page's selection: %q", got)
	}
}

// A page whose stream never opens — a crawler, a tab closed during load —
// leaves its record behind, so the sweep has to be time-based as well.
func TestPageState_ForgetsPagesNobodyCameBackFor(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	stale := s.newPage()
	// With something selected, or this proves nothing: a record that was never
	// given a selection reads as "" whether it was swept or not, which is how
	// the first version of this test passed with the sweep deleted.
	s.setPageSel(stale, "state:idle")
	age(t, s, stale, 2*pageStateTTL)

	fresh := s.newPage() // the issue is what sweeps

	if got := s.pageSel(stale); got != "" {
		t.Errorf("a record older than the TTL survived with %q selected", got)
	}
	s.setPageSel(fresh, "state:idle")
	if got := s.pageSel(fresh); got != "state:idle" {
		t.Errorf("the sweep took the record that had just been issued: %q", got)
	}
}

// An editor left open all afternoon is not a crawler. The sweep is about pages
// that have gone, and a page holding a stream open has not gone however long it
// has sat there — an earlier version swept on age alone and refreshed the age
// only when the selection changed, so working in another window for an hour and
// then opening a second tab wiped the first one.
func TestPageState_KeepsAPageThatStillHasAStreamHoweverOldItIs(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	open := s.newPage()
	s.streamOpened(open)
	s.setPageSel(open, "state:idle")
	age(t, s, open, 10*pageStateTTL)

	s.newPage() // the sweep

	if got := s.pageSel(open); got != "state:idle" {
		t.Errorf("a page with a live stream was swept: %q", got)
	}
}

// The cap is the same rule under pressure: idle records go, live ones do not.
func TestPageState_TheCapTakesOnlyIdleRecords(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	open := s.newPage()
	s.streamOpened(open)
	s.setPageSel(open, "state:idle")

	for range pageStateCap * 2 {
		s.newPage()
	}

	if got := s.pageSel(open); got != "state:idle" {
		t.Errorf("the cap took a live page's selection: %q", got)
	}
}

// age backdates a record's clock, which is the only thing the sweep reads.
func age(t *testing.T, s *Server, id string, by time.Duration) {
	t.Helper()
	s.selections.mu.Lock()
	defer s.selections.mu.Unlock()
	s.selections.m[id].at = time.Now().Add(-by)
}

func TestPageState_IsBoundedByTheCap(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	for range pageStateCap * 3 {
		s.newPage()
	}

	s.selections.mu.Lock()
	held := len(s.selections.m)
	s.selections.mu.Unlock()

	if held > pageStateCap {
		t.Errorf("holding %d page records, cap is %d", held, pageStateCap)
	}
	// And it is still storing them, or this would pass by storing nothing.
	if held == 0 {
		t.Error("the store holds nothing at all")
	}
}

// The whole path, through HTTP: a page selects something, the record is the
// page's own, and closing the stream takes it with it.
func TestSelect_RecordsAgainstThePageAndEndsWithIt(t *testing.T) {
	srv, s, dir := machineServer(t)
	path := filepath.Join(dir, "core", "behaviors", "wander.json")

	_, body := get(t, srv, "/forge/agents?machine="+url.QueryEscape(path))
	sub := subscriptionOf(t, body)
	page := pageIDIn(t, sub)

	// A page is identified by its signals on an action, the way Datastar sends
	// them — in the body on a POST — not by anything the route asks for.
	if code := postSignals(t, srv, "/forge/agents/select?sel=state%3Aidle",
		`{"page":"`+page+`"}`); code != 204 {
		t.Fatalf("select = %d", code)
	}
	if got := s.pageSel(page); got != "state:idle" {
		t.Fatalf("the page has %q selected, not what it asked for", got)
	}

	// And the stream ending is *not* the page ending. Datastar aborts the
	// stream when the tab is hidden and reconnects when it comes back, so a
	// close that dropped the record would leave the tab unable to select
	// anything ever again — clicks answering 204 and changing nothing.
	ctx, cancel := context.WithCancel(context.Background())
	ch := frames(t, ctx, srv.URL+sub)
	awaitFrame(t, ch, "save-footer", "on connect")
	cancel()

	// Give the handler time to return and run its deferred close.
	deadline := time.After(2 * time.Second)
	for s.streamsOn(page) != 0 {
		select {
		case <-deadline:
			t.Fatal("the stream never let go")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if got := s.pageSel(page); got != "state:idle" {
		t.Errorf("the page lost its selection when its stream closed: %q", got)
	}

	// And it comes back and carries on.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	ch2 := frames(t, ctx2, srv.URL+sub)
	awaitFrame(t, ch2, "save-footer", "on reconnect")

	if code := postSignals(t, srv, "/forge/agents/select?sel=state%3Acombat",
		`{"page":"`+page+`"}`); code != 204 {
		t.Fatalf("select after reconnect = %d", code)
	}
	if got := s.pageSel(page); got != "state:combat" {
		t.Errorf("the page stopped accepting selections after a reconnect: %q", got)
	}
}

// Without the id in the signals an action cannot say which page it is, so every
// selection would be recorded against nothing and the chart would never move.
func TestPageID_TravelsInThePagesSignals(t *testing.T) {
	srv, _, dir := machineServer(t)
	path := filepath.Join(dir, "core", "behaviors", "wander.json")

	_, body := get(t, srv, "/forge/agents?machine="+url.QueryEscape(path))
	page := pageIDIn(t, dataInit(t, body))

	if !strings.Contains(body, `&#34;page&#34;:&#34;`+page+`&#34;`) {
		t.Errorf("the page's signals do not carry its id (%s):\n%.400s", page, body)
	}
}

// postSignals is post with a Datastar signal payload, which is how every action
// tells the server which page made it.
func postSignals(t *testing.T, srv *httptest.Server, path, signals string) int {
	t.Helper()
	resp, err := srv.Client().Post(srv.URL+path, "application/json", strings.NewReader(signals))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
