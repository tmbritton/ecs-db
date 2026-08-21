package templates

import (
	"fmt"

	"github.com/tmbritton/ecs-db/internal/forge/mode"
)

// menuLabels is the menu bar, left to right. accentedMenu carries the amber
// accent because it owns the engine-connection actions.
var menuLabels = []string{"File", "Edit", "View", "Map", "Engine", "Help"}

const accentedMenu = "Engine"

// eventStream is the one SSE subscription a page opens. Everything that
// changes after the page lands — the engine-status readout, and later a mode's
// live data — is pushed down this single stream as HTML patches. One stream
// per page, not one per widget: browsers cap concurrent connections per
// origin, and a stream per component spends that budget for nothing.
//
// `data-init` is the attribute that runs an expression once when the element
// is set up. Datastar v1.0.2 registers no `on-load` plugin, and an attribute
// naming a plugin that is not registered is skipped in silence — see
// components/datastar_test.go, which checks every attribute this package
// emits against the plugins the vendored bundle actually registers.
func eventStream(m mode.Mode) string {
	return fmt.Sprintf("@get('%s/events')", m.Path())
}

// boolAttr renders a tri-state-safe attribute value. `data-active` is written
// on every rail button rather than only the active one, so a test can assert
// "this one is active AND the others are not" from a single attribute instead
// of inferring absence from a missing class.
func boolAttr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
