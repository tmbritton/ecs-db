// The bundle-scanning machinery lives in web/dstest so the shell templates can
// use the same guard. See that package for why this exists at all.
package components

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/web/dstest"
)

func TestDatastarAttributes_NamePluginsTheBundleRegisters(t *testing.T) {
	plugins := dstest.Plugins(t)

	// Every primitive that can emit a data-* attribute, wired as a caller would.
	rendered := []string{
		render(t, ListRow(ListRowProps{Label: "Terrain", Action: "@get('/x')"})),
		render(t, Chip(ChipProps{Name: "Velocity", Kind: ChipOptional, RemoveAction: "@post('/x')"})),
		render(t, SegmentedControl(SegmentedControlProps{
			Label: "lens", Signal: "lens", Action: "@post('/x')",
			Options: []SegmentOption{{Value: "a", Label: "A"}},
		})),
		render(t, Dropdown(DropdownProps{Signal: "behavior", Action: "@post('/x')", Options: []Option{{Value: "a", Label: "A"}}})),
		render(t, Checkbox(CheckboxProps{Label: "Snap", Signal: "snap", Action: "@post('/x')"})),
		render(t, IconButton(IconButtonProps{Glyph: "▶", Title: "preview", Action: "@post('/x')"})),
		render(t, ContextMenu(ContextMenuProps{Title: "T", Items: []MenuItem{{Label: "Cut", Action: "@post('/x')"}}})),
		renderChildren(t, ModalShell(ModalShellProps{Title: "T", CloseAction: "@post('/x')"}), "body"),
		render(t, SaveFooter(SaveFooterProps{Dirty: true, SaveAction: "@post('/s')", DiscardAction: "@post('/d')"})),
	}

	seen := dstest.AssertAttrs(t, plugins, rendered...)

	// Guard the guard: if the primitives stopped emitting handlers entirely,
	// every check above would pass by having nothing to check.
	dstest.RequireSeen(t, seen, "on:click", "on:change", "bind")
}

// The event name has to survive the plugin's kebab conversion. `data-on:click`
// is right; `data-on-click` is a plugin name, not an event, and is ignored.
func TestDatastarAttributes_EventKeyUsesColon(t *testing.T) {
	out := render(t, ListRow(ListRowProps{Label: "Terrain", Action: "@get('/x')"}))
	if strings.Contains(out, "data-on-click") {
		t.Errorf("data-on-click parses as a plugin named \"on-click\" and is silently ignored\n%s", out)
	}
	if !strings.Contains(out, "data-on:click=") {
		t.Errorf("want data-on:click\n%s", out)
	}
}
