package agent

import (
	"testing"
)

func TestParseDurationMs_BareInt(t *testing.T) {
	ms, err := ParseDurationMs("500")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms != 500 {
		t.Errorf("got %d, want 500", ms)
	}
}

func TestParseDurationMs_MillisecondSuffix(t *testing.T) {
	ms, err := ParseDurationMs("500ms")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms != 500 {
		t.Errorf("got %d, want 500", ms)
	}
}

func TestParseDurationMs_SecondSuffix(t *testing.T) {
	ms, err := ParseDurationMs("1s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms != 1000 {
		t.Errorf("got %d, want 1000", ms)
	}
}

func TestParseDurationMs_FractionalSecond(t *testing.T) {
	ms, err := ParseDurationMs("1.5s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms != 1500 {
		t.Errorf("got %d, want 1500", ms)
	}
}

func TestParseDurationMs_MinuteSuffix(t *testing.T) {
	ms, err := ParseDurationMs("2m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms != 120000 {
		t.Errorf("got %d, want 120000", ms)
	}
}

func TestParseDurationMs_Invalid(t *testing.T) {
	cases := []string{"bad", "1x", "", "abc123"}
	for _, s := range cases {
		_, err := ParseDurationMs(s)
		if err == nil {
			t.Errorf("ParseDurationMs(%q): expected error, got nil", s)
		}
	}
}

func TestDurationToTicks_ExactDivision(t *testing.T) {
	ticks := DurationToTicks(500, 50)
	if ticks != 10 {
		t.Errorf("got %d, want 10", ticks)
	}
}

func TestDurationToTicks_CeilRemainder(t *testing.T) {
	if got := DurationToTicks(100, 50); got != 2 {
		t.Errorf("100ms/50ms = %d, want 2", got)
	}
	if got := DurationToTicks(75, 50); got != 2 {
		t.Errorf("75ms/50ms = %d, want 2", got)
	}
}

func TestDurationToTicks_DefaultTickDuration(t *testing.T) {
	if got := DurationToTicks(1000, 50); got != 20 {
		t.Errorf("1000ms/50ms = %d, want 20", got)
	}
}

func TestDurationToTicks_ZeroTickDuration(t *testing.T) {
	if got := DurationToTicks(500, 0); got != 500 {
		t.Errorf("DurationToTicks(500,0) = %d, want 500", got)
	}
}
