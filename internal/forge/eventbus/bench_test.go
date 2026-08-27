package eventbus

import (
	"io"
	"log/slog"
	"testing"
)

// A publish should not allocate.
//
// Forge publishes once per edit and would never notice if it did, but this
// package is meant to be extracted and shared, and the next thing to use it may
// publish per frame. The two allocations this started with were both invisible:
// a slice copied out of the map on every publish, and slog's variadic args,
// which are built at the call site whatever the level is set to — so a Debug
// line nobody had switched on still cost something on every publish.
//
// Run with -benchmem. The interesting column is allocs/op, not ns/op.
func BenchmarkPublishNonBlocking(b *testing.B) {
	// Info level, so every Debug call in the publish path is a no-op. The
	// question this asks is what the path costs anyway.
	eb := New(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ch := eb.Subscribe("t")
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ch:
			case <-done:
				return
			}
		}
	}()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		eb.PublishNonBlocking(Event{Type: "t"})
	}
	b.StopTimer()
	eb.Unsubscribe(ch)
}
