package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/starfederation/datastar-go/datastar"
)

const (
	// pageStateTTL is how long a record with no stream on it survives.
	//
	// Only records with no stream: a page holding one open is alive by
	// definition, however long it has sat there. An earlier version swept on
	// age alone and refreshed the age only when the selection *changed*, which
	// made an editor left open all afternoon indistinguishable from a crawler —
	// and the next page load anywhere swept it.
	pageStateTTL = time.Hour
	// pageStateCap bounds the store when pages are loaded faster than the TTL
	// sweeps them. Only idle records are dropped: taking a live page's
	// selection because a thousand crawlers went past is not a trade worth
	// making.
	pageStateCap = 1024
)

// pageState is what one open page has chosen, as opposed to what the project
// contains.
type pageState struct {
	// sel is the statechart node or edge this page has selected, in chart's
	// own notation. Empty for nothing selected.
	sel string
	// streams is how many SSE connections are open for this page — normally
	// one, briefly two while a reconnect overlaps, and zero for a page that has
	// gone away or is between reconnects.
	streams int
	// at is when this page last had a stream on it. Only read for records with
	// no stream; see pageStateTTL.
	at time.Time
}

// pageStates is per-tab view state the server has to know.
//
// Most view state does not belong here — MAP's zoom, its tile in hand and its
// layer visibility are signals the browser owns outright, precisely so that a
// re-render cannot undo them. This is the case that cannot be done that way:
// the statechart's selection decides what the *inspector* renders, so the
// server has to know it to render at all, and a stream whose URL was fixed when
// the page loaded would go on rendering the inspector for whatever was selected
// then.
//
// Per page rather than per server, which is what makes it an improvement on
// editProblem and canvasMenu rather than more of the same: those are shared by
// every open tab, and two browsers on one Forge see each other's right-click
// menu. A page carries its id in its signals, so every request it makes says
// which page it is.
type pageStates struct {
	mu sync.Mutex
	m  map[string]*pageState
}

// newPage issues an id for a page that is about to render.
func (s *Server) newPage() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// Only reachable if the system entropy source is broken. An empty id is
		// never recorded against, so the page falls back to the behaviour it
		// had before it had an id: its selection comes from its URL and does
		// not survive.
		return ""
	}
	id := hex.EncodeToString(raw[:])

	s.selections.mu.Lock()
	defer s.selections.mu.Unlock()
	if s.selections.m == nil {
		s.selections.m = make(map[string]*pageState)
	}

	// Swept on issue rather than on a timer: there is no work to do when nobody
	// is loading pages. Only records with no stream on them — a page that still
	// holds one is a tab someone has open.
	cutoff := time.Now().Add(-pageStateTTL)
	for k, v := range s.selections.m {
		if v.streams == 0 && v.at.Before(cutoff) {
			delete(s.selections.m, k)
		}
	}
	if len(s.selections.m) >= pageStateCap {
		for k, v := range s.selections.m {
			if v.streams == 0 {
				delete(s.selections.m, k)
			}
		}
	}

	s.selections.m[id] = &pageState{at: time.Now()}
	return id
}

// pageSel is what this page has selected, or "" for a page the server does not
// know — which is every page before it has selected anything, and every request
// that arrived without an id.
func (s *Server) pageSel(id string) string {
	if id == "" {
		return ""
	}
	s.selections.mu.Lock()
	defer s.selections.mu.Unlock()
	if p := s.selections.m[id]; p != nil {
		return p.sel
	}
	return ""
}

// setPageSel records what a page has selected.
//
// Only for a page that was issued an id. A request naming one the server has
// forgotten — a tab left open across a restart — is ignored rather than
// recreating it, so a stale page cannot grow the store back after its stream
// closed and its record was dropped.
func (s *Server) setPageSel(id, sel string) {
	if id == "" {
		return
	}
	s.selections.mu.Lock()
	defer s.selections.mu.Unlock()
	if p := s.selections.m[id]; p != nil {
		p.sel = sel
	}
}

// streamOpened and streamClosed mark a page as having a live connection.
//
// A stream closing is emphatically *not* a page ending, which is what the first
// version of this assumed — and it was wrong in the most ordinary way possible.
// Datastar opens its stream with openWhenHidden false, so switching to another
// browser tab aborts it and switching back reconnects with the same page id. If
// the close had deleted the record, the reconnect would find nothing, every
// later selection would be recorded against a page the server had forgotten,
// and the tab would go quietly dead: clicks answering 204 and changing nothing,
// for the life of the tab, with no error anywhere.
//
// Counted rather than a flag, because a reconnect can overlap the close it is
// replacing, and because anything else that reads the subscription URL — a
// second tab, a replayed request from the network panel — must not be able to
// take a live page's selection away by hanging up.
func (s *Server) streamOpened(id string) { s.markStream(id, +1) }

func (s *Server) streamClosed(id string) { s.markStream(id, -1) }

func (s *Server) markStream(id string, delta int) {
	if id == "" {
		return
	}
	s.selections.mu.Lock()
	defer s.selections.mu.Unlock()

	p := s.selections.m[id]
	if p == nil {
		return
	}
	p.streams += delta
	if p.streams < 0 {
		p.streams = 0
	}
	// The clock starts when the last stream lets go, so the TTL measures how
	// long a page has been gone rather than how long it has been quiet.
	p.at = time.Now()
}

// pageIDOf is the page a request came from.
//
// Two places, because a request reaches the server two ways. The stream names
// it in its URL, which is fixed when the page opens and is exactly right for
// something that identifies the page. Everything else is a Datastar action,
// and Datastar sends the page's signals with every one of those — so the id
// rides along without any route having to ask for it.
func pageIDOf(r *http.Request) string {
	if id := r.URL.Query().Get("page"); id != "" {
		return id
	}
	var signals struct {
		Page string `json:"page"`
	}
	if err := datastar.ReadSignals(r, &signals); err != nil {
		return ""
	}
	return signals.Page
}

// streamsOn is how many connections a page currently holds. It exists so a test
// can wait for a close to have actually happened rather than sleeping past it.
func (s *Server) streamsOn(id string) int {
	s.selections.mu.Lock()
	defer s.selections.mu.Unlock()
	if p := s.selections.m[id]; p != nil {
		return p.streams
	}
	return 0
}
