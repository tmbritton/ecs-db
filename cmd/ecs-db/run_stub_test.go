//go:build !ebitengine

package main

import (
	"errors"
	"strings"
	"testing"
)

// The headless binary must still expose `run`, so the CLI surface is identical
// in both builds and the failure is an explanation rather than "unknown command".
func TestRunCmd_RegisteredInHeadlessBuild(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"run"})
	if err != nil {
		t.Fatalf("finding run command: %v", err)
	}
	if cmd.Name() != "run" {
		t.Fatalf("got command %q, want %q", cmd.Name(), "run")
	}
}

func TestRunCmd_WithoutEbitengine_ReturnsError(t *testing.T) {
	// Through Execute, not runCmd.RunE directly, so this covers the wiring a
	// user actually hits — including PersistentPreRunE loading config first.
	_, err := execute(t, "run", "--config", writeProject(t, validSchema))
	if !errors.Is(err, errNoEbitengine) {
		t.Fatalf("got %v, want errNoEbitengine", err)
	}
	// The message has to name the missing tag, or the user has no way to act on it.
	if !strings.Contains(err.Error(), "ebitengine") {
		t.Errorf("error %q does not mention the ebitengine build tag", err)
	}
}
