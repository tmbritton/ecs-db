package eventbus

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

// The four tests below are carried over from pkg/eventbus in
// tmbritton/fancykaraoke-go, which this package is a copy of. They are kept as
// they are so that a divergence in behaviour shows up here rather than when the
// two are reconciled into a shared library.

func TestEventBus_SubscribeAndPublish(t *testing.T) {
	eb := New(slog.Default())

	topic := "test-topic"
	ch := eb.Subscribe(topic)

	event := Event{
		Type:    topic,
		Payload: "test-payload",
	}

	eb.Publish(event)

	select {
	case received := <-ch:
		if received.Type != event.Type {
			t.Errorf("Expected event type %s, got %s", event.Type, received.Type)
		}
		if received.Payload != event.Payload {
			t.Errorf("Expected payload %v, got %v", event.Payload, received.Payload)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timed out waiting for event")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	eb := New(slog.Default())

	topic := "multi-topic"
	ch1 := eb.Subscribe(topic)
	ch2 := eb.Subscribe(topic)

	eb.Publish(Event{Type: topic, Payload: "broadcast"})

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case <-ch:
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Subscriber %d timed out", i+1)
		}
	}
}

func TestEventBus_IndependentTopics(t *testing.T) {
	eb := New(slog.Default())

	chA := eb.Subscribe("topic-A")
	chB := eb.Subscribe("topic-B")

	eb.Publish(Event{Type: "topic-A", Payload: "A"})

	select {
	case <-chA:
	case <-time.After(100 * time.Millisecond):
		t.Error("Topic A subscriber timed out")
	}

	select {
	case <-chB:
		t.Error("Topic B subscriber received event for Topic A")
	default:
	}
}

// Rewritten from upstream's version, which started 50 subscribers ranging over
// a channel that is never closed and never waited for them — so it leaked 50
// goroutines per run and could not have observed a race in the unsubscribe path
// because there was none to observe. Unsubscribe is what lets a subscriber
// finish, so the goroutines can now be joined and the -race detector gets to
// see subscribe, publish and unsubscribe overlapping.
func TestEventBus_Concurrency(t *testing.T) {
	eb := New(slog.Default())

	const topic, numSubs, numPubs = "concurrent-topic", 50, 50

	var subs sync.WaitGroup
	for range numSubs {
		subs.Add(1)
		go func() {
			defer subs.Done()
			ch := eb.Subscribe(topic)
			defer eb.Unsubscribe(ch)
			// Drain whatever arrives while the publishers are running, then
			// leave. A subscriber that blocked until it had seen every event
			// would deadlock against a bus that is allowed to drop.
			deadline := time.After(200 * time.Millisecond)
			for {
				select {
				case <-ch:
				case <-deadline:
					return
				}
			}
		}()
	}

	var pubs sync.WaitGroup
	for range numPubs {
		pubs.Add(1)
		go func() {
			defer pubs.Done()
			eb.PublishNonBlocking(Event{Type: topic, Payload: "data"})
		}()
	}

	pubs.Wait()
	subs.Wait()

	if got := eb.Subscribers(topic); got != 0 {
		t.Errorf("after every subscriber unsubscribed, %d were left", got)
	}
}

// --- Forge's additions. See docs/stories/epic-15/00-push-not-poll.md. ---

// Forge subscribes once per SSE stream, which is once per page load, so a bus
// that cannot forget a subscriber leaks one per page load. It does not stay a
// leak either: once a dead subscriber's buffer fills, Publish waits
// PublishTimeout on it, so N abandoned tabs put 5N seconds on every save.
func TestUnsubscribe_RemovesTheSubscriber(t *testing.T) {
	eb := New(slog.Default())

	ch := eb.Subscribe("t")
	if got := eb.Subscribers("t"); got != 1 {
		t.Fatalf("Subscribers after Subscribe = %d, want 1", got)
	}

	eb.Unsubscribe(ch)

	if got := eb.Subscribers("t"); got != 0 {
		t.Errorf("Subscribers after Unsubscribe = %d, want 0", got)
	}
}

// Exactly one: the bus holds a slice per type, and removing by value rather
// than by identity would take every subscriber with the same buffer state out
// with it.
func TestUnsubscribe_LeavesOtherSubscribersAlone(t *testing.T) {
	eb := New(slog.Default())

	going := eb.Subscribe("t")
	staying := eb.Subscribe("t")

	eb.Unsubscribe(going)

	if got := eb.Subscribers("t"); got != 1 {
		t.Fatalf("Subscribers after one Unsubscribe = %d, want 1", got)
	}

	eb.Publish(Event{Type: "t"})

	select {
	case <-staying:
	case <-time.After(100 * time.Millisecond):
		t.Error("the subscriber that stayed did not receive the event")
	}
}

func TestUnsubscribe_OfAChannelTheBusDoesNotHoldIsIgnored(t *testing.T) {
	eb := New(slog.Default())
	kept := eb.Subscribe("t")

	stranger := make(chan Event)
	eb.Unsubscribe(stranger)
	eb.Unsubscribe(kept)
	eb.Unsubscribe(kept) // twice: a deferred Unsubscribe can run after an explicit one

	if got := eb.Subscribers("t"); got != 0 {
		t.Errorf("Subscribers = %d, want 0", got)
	}
}

// One channel across several types, so a caller listening for six kinds of
// change does not grow a select case per kind.
func TestSubscribe_IsVariadicAcrossTypes(t *testing.T) {
	eb := New(slog.Default())

	ch := eb.Subscribe("a", "b", "c")

	for _, typ := range []string{"a", "b", "c"} {
		eb.Publish(Event{Type: typ})
		select {
		case got := <-ch:
			if got.Type != typ {
				t.Errorf("received %q, want %q", got.Type, typ)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("no event received for type %q", typ)
		}
	}
}

// And unsubscribing takes it off all of them, not just the first.
func TestUnsubscribe_RemovesAVariadicSubscriberFromEveryType(t *testing.T) {
	eb := New(slog.Default())

	ch := eb.Subscribe("a", "b", "c")
	eb.Unsubscribe(ch)

	for _, typ := range []string{"a", "b", "c"} {
		if got := eb.Subscribers(typ); got != 0 {
			t.Errorf("Subscribers(%q) = %d, want 0", typ, got)
		}
	}
}

// Forge calls Publish synchronously inside the HTTP handler for a mutation, so
// a subscriber that has stopped reading must not be able to stall someone's
// save. A stream that is behind does not need the event it missed — it needs to
// know something changed, which the next event also says.
func TestPublishNonBlocking_DropsRatherThanWaitingOnAFullSubscriber(t *testing.T) {
	eb := New(slog.Default())

	ch := eb.Subscribe("t")
	for range ChannelBuffer {
		eb.PublishNonBlocking(Event{Type: "t"})
	}
	if len(ch) != ChannelBuffer {
		t.Fatalf("buffer holds %d events, want %d", len(ch), ChannelBuffer)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		eb.PublishNonBlocking(Event{Type: "t"})
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("PublishNonBlocking blocked on a full subscriber")
	}
}

// A dropped event must not cost the subscriber the ones it already had. The
// obvious wrong fix — evicting the oldest to make room — loses an event the
// subscriber was going to act on in order to deliver one that says the same
// thing.
func TestPublishNonBlocking_ADroppedEventDoesNotDisturbTheBuffer(t *testing.T) {
	eb := New(slog.Default())

	ch := eb.Subscribe("t")
	for i := range ChannelBuffer {
		eb.PublishNonBlocking(Event{Type: "t", Payload: i})
	}
	eb.PublishNonBlocking(Event{Type: "t", Payload: "dropped"})

	if got := <-ch; got.Payload != 0 {
		t.Errorf("first buffered event has payload %v, want 0", got.Payload)
	}
	if len(ch) != ChannelBuffer-1 {
		t.Errorf("buffer holds %d events after one read, want %d", len(ch), ChannelBuffer-1)
	}
}

// The race the stream loop depends on: it subscribes, renders, then selects. An
// event published while it is rendering has to be waiting at the select rather
// than lost, or an edit made during a render never reaches the browser.
func TestPublish_DuringASubscribersWorkIsStillDelivered(t *testing.T) {
	eb := New(slog.Default())

	ch := eb.Subscribe("t")

	rendering := make(chan struct{})
	go func() {
		<-rendering // stands in for the subscriber being busy
		eb.PublishNonBlocking(Event{Type: "t", Payload: "mid-render"})
	}()

	close(rendering)
	time.Sleep(20 * time.Millisecond) // the publish lands while nobody is receiving

	select {
	case got := <-ch:
		if got.Payload != "mid-render" {
			t.Errorf("received %v, want mid-render", got.Payload)
		}
	case <-time.After(time.Second):
		t.Error("an event published while the subscriber was busy was lost")
	}
}

func TestNew_WithoutALoggerDoesNotPanic(t *testing.T) {
	eb := New(nil)
	ch := eb.Subscribe("t")
	eb.PublishNonBlocking(Event{Type: "t"})

	select {
	case <-ch:
	case <-time.After(100 * time.Millisecond):
		t.Error("a bus built with no logger did not deliver")
	}
}

// Upstream's Publish resets its shared timer with the pre-Go-1.23 dance:
//
//	if !timeout.Stop() { <-timeout.C }
//	timeout.Reset(PublishTimeout)
//
// When the previous iteration timed out, the select already took the value from
// timeout.C. Stop then reports false because the timer has expired, and the
// drain waits for a second value that is never sent — so Publish hangs forever
// on the subscriber *after* a slow one, inside the HTTP handler that called it.
//
// Go 1.23 made timer channels unbuffered and Stop/Reset safe without draining,
// and this module is go 1.26, so the fix is to drop the dance rather than to
// repair it.
func TestPublish_DoesNotHangOnTheSubscriberAfterASlowOne(t *testing.T) {
	eb := New(slog.Default())
	eb.publishTimeout = 10 * time.Millisecond

	slow := eb.Subscribe("t")
	for range ChannelBuffer {
		eb.PublishNonBlocking(Event{Type: "t"})
	}
	if len(slow) != ChannelBuffer {
		t.Fatalf("the slow subscriber holds %d events, want %d", len(slow), ChannelBuffer)
	}
	after := eb.Subscribe("t")

	done := make(chan struct{})
	go func() {
		defer close(done)
		eb.Publish(Event{Type: "t", Payload: "x"})
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish hung after timing out on a slow subscriber")
	}

	select {
	case got := <-after:
		if got.Payload != "x" {
			t.Errorf("the subscriber after the slow one received %v, want x", got.Payload)
		}
	default:
		t.Error("the subscriber after the slow one received nothing")
	}
}

// Publish and Unsubscribe overlap by design: Publish takes its list of
// subscribers under a read lock and then sends outside it, because a send may
// block and holding the lock across one would let a single stalled subscriber
// freeze every other caller. So a subscriber can leave while a publish is
// walking the list.
//
// Publish walking a copy is what makes that safe; Unsubscribe compacting into a
// fresh slice rather than over the array it was given is the second layer.
// Either alone is enough, which is why this test only goes red when *both* are
// removed — and it is worth having for that case, because the failure is not a
// lost event, it is a send to whichever channel the shuffle moved into the
// slot.
//
// Run under -race, which is where it earns its keep.
func TestUnsubscribeDuringPublishIsSafe(t *testing.T) {
	eb := New(slog.Default())

	const topic = "churn"
	subs := make([]<-chan Event, 0, 64)
	for range cap(subs) {
		subs = append(subs, eb.Subscribe(topic))
	}

	done := make(chan struct{})
	var publishing sync.WaitGroup
	publishing.Add(1)
	go func() {
		defer publishing.Done()
		for {
			select {
			case <-done:
				return
			default:
				eb.PublishNonBlocking(Event{Type: topic, Payload: "x"})
			}
		}
	}()

	// Leaving while the publisher is mid-walk, which is the overlap that
	// matters — the existing concurrency test unsubscribes only once every
	// publish has finished, so it never exercises this at all.
	var leaving sync.WaitGroup
	for _, ch := range subs {
		leaving.Add(1)
		go func() {
			defer leaving.Done()
			eb.Unsubscribe(ch)
		}()
	}
	leaving.Wait()
	close(done)
	publishing.Wait()

	if got := eb.Subscribers(topic); got != 0 {
		t.Errorf("%d subscribers left after every one unsubscribed", got)
	}
}
