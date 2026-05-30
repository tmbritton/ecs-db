package agent

import (
	"fmt"
	"strconv"
	"time"
)

// ParseDurationMs converts an after-duration string to milliseconds.
// Accepts bare integer strings (XState ms shorthand) and time.Duration strings.
func ParseDurationMs(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration string")
	}
	// Bare integer → treat as milliseconds.
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	// Delegate to time.ParseDuration for "500ms", "1s", "1.5s", "2m", etc.
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid after duration %q: %w", s, err)
	}
	return d.Milliseconds(), nil
}

// DurationToTicks converts a millisecond count to ticks using ceiling division.
// tickDurationMs of 0 falls back to 1 (1ms per tick) to avoid division by zero.
func DurationToTicks(ms, tickDurationMs int64) int64 {
	if tickDurationMs <= 0 {
		tickDurationMs = 1
	}
	return (ms + tickDurationMs - 1) / tickDurationMs
}

// parseDurationTicks converts an after-duration string to a tick count.
func parseDurationTicks(duration string, tickDurationMs int64) int64 {
	ms, err := ParseDurationMs(duration)
	if err != nil {
		return 0
	}
	return DurationToTicks(ms, tickDurationMs)
}

// afterEventType returns the synthetic event type for an after-transition.
// Format matches XState v4: xstate.after(N).STATE_ID
func afterEventType(duration, stateID string) string {
	return "xstate.after(" + duration + ")." + stateID
}

// afterCandidates returns the After transitions for cur whose computed event type
// matches eventType. After keys are raw duration strings; the event type is
// "xstate.after(<duration>).<state-id>", so we compute and compare.
func afterCandidates(cur *StateNode, eventType string) []Transition {
	for duration, ts := range cur.After {
		if eventType == afterEventType(duration, cur.ID) {
			return ts
		}
	}
	return nil
}
