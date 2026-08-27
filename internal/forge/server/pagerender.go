package server

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	// pageRenderTTL is how long a stamp is worth honouring. A page opens its
	// stream in the same breath as loading, so anything still here after a
	// minute belongs to a page whose stream never came — a crawler, a tab
	// closed mid-load — and is only taking up room.
	pageRenderTTL = time.Minute
	// pageRenderCap bounds the map when page loads arrive faster than the TTL
	// sweeps them. Dropping stamps costs a redundant send and nothing else,
	// which is the right way round for a cache to fail.
	pageRenderCap = 1024
)

// pageRender is what one page load shipped to the browser.
type pageRender struct {
	sum string
	at  time.Time
}

// pageRenders remembers what each page load sent, so the stream that follows it
// can recognise that the browser already holds those bytes and skip re-sending
// them.
//
// Keyed by a stamp that is good exactly once, rather than by the content hash
// itself. The distinction matters: Datastar re-runs the data-init expression
// when a dropped stream reconnects, so the same URL — and the same stamp —
// arrives a second time. By then the browser's DOM holds whatever was last
// patched rather than what the page loaded with, so "these bytes match what the
// page shipped" is no longer a reason to stay quiet. A stamp that is consumed
// on first use says the narrower and true thing: *this page load's first
// stream* already has this. Everything else — a reconnect, a second
// subscription, a client that built its own URL — gets the full send, which is
// the safe direction to be wrong in.
type pageRenders struct {
	mu sync.Mutex
	m  map[string]pageRender
}

// rememberPage records a page's rendered content and returns the stamp its
// stream should present.
func (s *Server) rememberPage(sum string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// Only reachable if the system entropy source is broken. An empty
		// stamp is never honoured by takePage, so the page simply gets the
		// pre-existing behaviour of being sent its own content once.
		return ""
	}
	stamp := hex.EncodeToString(raw[:])

	s.pages.mu.Lock()
	defer s.pages.mu.Unlock()
	if s.pages.m == nil {
		s.pages.m = make(map[string]pageRender)
	}

	// Swept on insert rather than on a timer: there is no work to do when
	// nobody is loading pages, and a goroutine that wakes to find an empty map
	// is a goroutine that should not exist.
	cutoff := time.Now().Add(-pageRenderTTL)
	for k, v := range s.pages.m {
		if v.at.Before(cutoff) {
			delete(s.pages.m, k)
		}
	}
	if len(s.pages.m) >= pageRenderCap {
		clear(s.pages.m)
	}

	s.pages.m[stamp] = pageRender{sum: sum, at: time.Now()}
	return stamp
}

// takePage consumes a stamp and reports what the page it belongs to shipped.
// A stamp is good once; see pageRenders.
//
// The empty stamp — from a subscription the shell did not build, or from a page
// whose rememberPage could not read the system entropy source — is refused by
// the lookup like any other stamp that was never issued, because nothing is
// ever stored under it.
func (s *Server) takePage(stamp string) (string, bool) {
	s.pages.mu.Lock()
	defer s.pages.mu.Unlock()

	got, ok := s.pages.m[stamp]
	if !ok {
		return "", false
	}
	delete(s.pages.m, stamp)
	return got.sum, true
}
