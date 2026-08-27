package server

import (
	"testing"
	"time"
)

// The stamp a page carries has to mean "this page load, once" and not "these
// bytes, ever".
//
// Datastar re-issues the data-init expression when a dropped stream reconnects,
// so the same stamp arrives a second time — by which point the browser's DOM
// holds whatever was last patched, not what the page loaded with. If the
// server's render had meanwhile changed and changed back, a stamp that still
// matched would suppress the send and leave the browser showing the wrong
// thing, with nothing to correct it until the next unrelated edit.
func TestPageRenders_AStampIsGoodOnce(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	stamp := s.rememberPage("abc123")

	got, ok := s.takePage(stamp)
	if !ok || got != "abc123" {
		t.Fatalf("first take = (%q, %v), want (\"abc123\", true)", got, ok)
	}

	if _, ok := s.takePage(stamp); ok {
		t.Error("the same stamp was accepted twice — a reconnect would be told to skip")
	}
}

func TestPageRenders_AnUnknownStampIsRefused(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	if _, ok := s.takePage(""); ok {
		t.Error("an empty stamp was accepted")
	}
	if _, ok := s.takePage("never-issued"); ok {
		t.Error("a stamp the server never issued was accepted")
	}
}

func TestPageRenders_StampsAreDistinct(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	seen := make(map[string]bool)
	for range 100 {
		stamp := s.rememberPage("same-bytes")
		if seen[stamp] {
			t.Fatalf("stamp %q was issued twice", stamp)
		}
		seen[stamp] = true
	}
}

// A page whose stream never opens — a crawler, a tab closed during load —
// leaves its stamp behind. Without eviction that is a map that only grows, in a
// process meant to run for days.
func TestPageRenders_ForgetsStampsNobodyCameBackFor(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	stale := s.rememberPage("old")
	s.pages.mu.Lock()
	s.pages.m[stale] = pageRender{sum: "old", at: time.Now().Add(-2 * pageRenderTTL)}
	s.pages.mu.Unlock()

	fresh := s.rememberPage("new") // the insert is what sweeps

	if _, ok := s.takePage(stale); ok {
		t.Error("a stamp older than the TTL was still honoured")
	}
	if _, ok := s.takePage(fresh); !ok {
		t.Error("the sweep took the stamp that had just been issued")
	}
}

// The sweep is time-based, so a burst of page loads inside one TTL is not
// bounded by it. The cap is what keeps a process that is being hammered from
// holding every stamp; dropping them all only costs a redundant send.
func TestPageRenders_IsBoundedByTheCap(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())

	for range pageRenderCap * 3 {
		s.rememberPage("x")
	}

	s.pages.mu.Lock()
	held := len(s.pages.m)
	s.pages.mu.Unlock()

	if held > pageRenderCap {
		t.Errorf("holding %d stamps, cap is %d", held, pageRenderCap)
	}
}
