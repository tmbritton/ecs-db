// Package eventbus is a typed publish/subscribe bus.
//
// It is a copy of pkg/eventbus in tmbritton/fancykaraoke-go, kept
// source-compatible on purpose: the intention is to extract that package into a
// library shared by every Go application here, at which point this one is
// deleted and the import swapped. Keeping the whole API rather than trimming it
// to what Forge happens to use is what makes that swap a one-line change.
//
// Three things differ from upstream, all of them found by being the first
// consumer whose subscribers come and go, and all of them worth taking back:
//
//   - Unsubscribe exists. Forge subscribes once per SSE stream, which is once
//     per page load, so a bus that cannot forget a subscriber leaks one per page
//     load — and it does not stay a leak, see PublishNonBlocking below.
//   - Subscribe is variadic, so one channel can carry several types and a
//     listener does not grow a select case per type it cares about.
//   - PublishNonBlocking exists, and Publish no longer hangs on the subscriber
//     after a slow one (see the comment on Publish).
//
// Also: upstream logs subscriber churn at Info. Here that is Debug, because
// Forge gains and loses a subscriber on every page load and a development tool
// that prints a line each time is a tool whose log nobody reads.
//
// What is NOT here is upstream's global.go. AGENTS.md forbids package-level
// singletons; Forge takes its bus through server.Config the same way it takes
// its sessions.
package eventbus

import (
	"log/slog"
	"sync"
	"time"
)

const (
	// ChannelBuffer is the buffer size for event bus subscriber channels.
	// Larger buffers reduce the chance of blocking publishers, but increase memory usage.
	// A buffer of 100 allows bursts of events without blocking.
	ChannelBuffer = 100

	// PublishTimeout is how long to wait when sending an event to a slow subscriber
	// before giving up. This prevents one slow subscriber from blocking all events.
	PublishTimeout = 5 * time.Second
)

// Event is one notification. Type is what happened; Payload is optional detail,
// and subscribers that only need to know *that* something happened leave it nil.
type Event struct {
	Type    string
	Payload any
}

type EventBus struct {
	subscribers map[string][]chan Event
	mutex       sync.RWMutex
	logger      *slog.Logger

	// publishTimeout is PublishTimeout, overridden only by tests — waiting five
	// real seconds to prove a timeout is not a test anyone runs twice.
	publishTimeout time.Duration
}

// New creates a new EventBus instance
func New(logger *slog.Logger) *EventBus {
	if logger == nil {
		logger = slog.Default()
	}
	eb := &EventBus{
		subscribers:    make(map[string][]chan Event),
		logger:         logger,
		publishTimeout: PublishTimeout,
	}
	logger.Info("event bus initialized")
	return eb
}

// Subscribe returns a channel carrying every event of the given types.
//
// One channel for all of them, rather than one per type: a listener watching
// six kinds of change wants one select case, not six. Passing a single type is
// the upstream signature and behaves identically.
//
// The channel is buffered, which is what makes it safe to subscribe before
// starting work rather than after: an event published while the subscriber is
// busy is waiting when it next looks, instead of being missed.
//
// Every caller must Unsubscribe. The bus never closes a subscriber's channel —
// see Unsubscribe for why — so ranging over it will not terminate on its own.
func (eb *EventBus) Subscribe(eventTypes ...string) <-chan Event {
	eb.mutex.Lock()
	defer eb.mutex.Unlock()

	ch := make(chan Event, ChannelBuffer)
	for _, eventType := range eventTypes {
		eb.subscribers[eventType] = append(eb.subscribers[eventType], ch)
	}

	eb.logger.Debug("event bus subscriber added",
		slog.Any("event_types", eventTypes),
		slog.Int("channel_buffer", ChannelBuffer))

	return ch
}

// Unsubscribe removes a channel from every type it was subscribed to.
//
// Removing by identity, not by value: two subscribers to the same type are two
// channels with the same element type and the same contents, and taking out
// "one that looks like this" would take out both.
//
// The channel is deliberately not closed. Publish copies the subscriber slice
// under a read lock and then sends outside it, so a channel closed here could
// be one a publisher is about to send on — which panics. The caller knows it is
// finished, because it is the one that said so.
//
// Unsubscribing a channel the bus does not hold is a no-op, so an explicit call
// followed by a deferred one is safe.
func (eb *EventBus) Unsubscribe(ch <-chan Event) {
	eb.mutex.Lock()
	defer eb.mutex.Unlock()

	removed := 0
	for eventType, subs := range eb.subscribers {
		// A fresh slice, not subs[:0].
		//
		// In-place compaction is safe as the package stands, because
		// subscribersOf copies the channels out before Publish sends. That
		// makes this belt and braces — but the belt is one line in another
		// function, and removing it is the sort of tidy-up that looks free:
		// a live slice plus in-place compaction is a data race that delivers
		// to whichever channel the shuffle moved into the slot, which is a
		// much worse symptom than a dropped event.
		kept := make([]chan Event, 0, len(subs))
		for _, sub := range subs {
			if (<-chan Event)(sub) == ch {
				removed++
				continue
			}
			kept = append(kept, sub)
		}
		if len(kept) == 0 {
			// Not just tidiness: Subscribers reports a count per type, and a
			// bus that accumulates an empty slice per type a page ever
			// subscribed to grows without bound in a long-running Forge.
			delete(eb.subscribers, eventType)
			continue
		}
		eb.subscribers[eventType] = kept
	}

	eb.logger.Debug("event bus subscriber removed", slog.Int("types", removed))
}

// Subscribers reports how many channels are listening for a type. It exists so
// that a test can prove a stream let go of its subscription — a leaked
// subscriber does not break anything visibly, it just slowly turns Publish into
// a wait.
func (eb *EventBus) Subscribers(eventType string) int {
	eb.mutex.RLock()
	defer eb.mutex.RUnlock()
	return len(eb.subscribers[eventType])
}

// Publish delivers an event to every subscriber of its type, waiting up to
// PublishTimeout on each before giving up on it.
//
// Prefer PublishNonBlocking from anything holding a request open; see there.
//
// Upstream reset its shared timer with the pre-Go-1.23 dance —
// `if !timeout.Stop() { <-timeout.C }` — which hangs forever on the iteration
// after a timeout: the select has already taken the value, Stop reports false
// because the timer expired, and the drain waits for a second value that is
// never sent. Go 1.23 made timer channels unbuffered and Stop/Reset safe
// without draining, so the dance is gone rather than repaired.
func (eb *EventBus) Publish(event Event) {
	subs := eb.subscribersOf(event.Type)

	// One timer for the whole publish rather than a time.After per subscriber,
	// which would spawn a goroutine each.
	timeout := time.NewTimer(eb.publishTimeout)
	defer timeout.Stop()

	successCount, timeoutCount := 0, 0
	for i, ch := range subs {
		if i > 0 {
			timeout.Reset(eb.publishTimeout)
		}

		select {
		case ch <- event:
			successCount++
		case <-timeout.C:
			timeoutCount++
			eb.logger.Error("event delivery timed out - subscriber is slow or blocked",
				slog.String("event_type", event.Type),
				slog.Int("channel_capacity", cap(ch)),
				slog.Int("channel_length", len(ch)),
				slog.Duration("timeout", eb.publishTimeout))
		}
	}

	if timeoutCount > 0 {
		eb.logger.Error("some events timed out during publish",
			slog.String("event_type", event.Type),
			slog.Int("timed_out", timeoutCount),
			slog.Int("successful", successCount))
	}
}

// PublishNonBlocking delivers an event to every subscriber that has room for
// it, and drops it for any that does not.
//
// This is what a publisher inside a request handler wants. Forge publishes from
// the middleware wrapping every mutation route, so a subscriber that has
// stopped reading must not be able to stall someone's save — and a subscriber
// that is behind does not need the event it missed. It needs to know something
// changed, which the next event also says.
//
// Dropping the new event rather than evicting the oldest is deliberate: making
// room would discard an event the subscriber was going to act on in order to
// deliver one that says the same thing.
func (eb *EventBus) PublishNonBlocking(event Event) {
	// The read lock is held across the sends, where Publish takes a copy and
	// releases it first. It can be, because a non-blocking send cannot block:
	// there is no subscriber that can hold the lock open, so Unsubscribe waits
	// on a bounded loop of channel writes rather than on a browser. That saves
	// the copy — a slice allocated on every publish for the life of the
	// process — and removes any question of the two overlapping.
	eb.mutex.RLock()
	delivered, dropped := 0, 0
	for _, ch := range eb.subscribers[event.Type] {
		select {
		case ch <- event:
			delivered++
		default:
			dropped++
		}
	}
	eb.mutex.RUnlock()

	if dropped > 0 {
		eb.logger.Warn("event bus dropped an event for full subscribers",
			slog.String("event_type", event.Type),
			slog.Int("dropped", dropped),
			slog.Int("delivered", delivered))
	}
}

// Nothing is logged on a publish that worked.
//
// Two reasons, and the second is the one that decided it. A line per publish is
// not information — the normal case is not worth reporting, and the signals
// that are (a drop, a timeout) are reported. And slog builds its variadic args
// at the call site whatever the level is set to, so a Debug call on a hot path
// allocates whether or not it ever prints; guarding inside a helper does not
// help, because the slice is already built by the time the helper is entered.
// Forge publishes once per edit and would never notice, but a bus shared with
// something publishing per frame would, and this is meant to be shared.

// subscribersOf is a copy taken under the lock, so that sending — which may
// block — happens outside it. Holding the lock across a blocking send would let
// one stalled subscriber block Subscribe and Unsubscribe for every other
// caller, for up to PublishTimeout each.
//
// Publish's alone. PublishNonBlocking cannot stall, so it holds the lock
// instead and skips the copy.
func (eb *EventBus) subscribersOf(eventType string) []chan Event {
	eb.mutex.RLock()
	defer eb.mutex.RUnlock()

	subs := make([]chan Event, len(eb.subscribers[eventType]))
	copy(subs, eb.subscribers[eventType])
	return subs
}
