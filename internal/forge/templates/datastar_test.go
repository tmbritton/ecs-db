package templates

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/web/dstest"
)

// The shell's one Datastar attribute is the page's SSE subscription. If it
// names a plugin the bundle does not register, the page loads, looks correct,
// and never connects to its stream — no error anywhere.
func TestShell_DatastarAttributesResolve(t *testing.T) {
	plugins := dstest.Plugins(t)
	seen := dstest.AssertAttrs(t, plugins, renderShell(t, mode.Default))
	dstest.RequireSeen(t, seen, "init")
}

// Datastar v1.0.2 registers no `on-load` plugin — `data-on-load` is skipped in
// silence, and the page never subscribes. `data-init` is the attribute that
// runs an expression once when the element is set up.
func TestShell_SubscribesWithInitNotOnLoad(t *testing.T) {
	got := renderShell(t, mode.Default)
	if strings.Contains(got, "data-on-load") || strings.Contains(got, "data-on:load") {
		t.Errorf("the subscription uses a load event; no such plugin exists\n%s", got)
	}
	if !strings.Contains(got, "data-init=") {
		t.Errorf("want data-init for the SSE subscription\n%s", got)
	}
}
