package components

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/web"
)

// Datastar attributes fail silently. An attribute whose plugin name does not
// resolve is skipped with no error, no console warning and no visual
// difference — the control simply never does anything. That is exactly how
// every handler in this package shipped inert: v1.0.2 separates a plugin from
// its key with a colon (`data-on:click`), and `data-on-click` parses as a
// plugin named "on-click", which does not exist.
//
// So the vendored bundle is the source of truth. This renders every primitive,
// extracts the data-* attributes, and checks each one names a plugin the
// bundle actually registers.

// The bundle registers plugins as `{name:"on",…}`. Minified, but the shape is
// stable across builds and a rename would surface here as a failure rather
// than as silently dead UI.
var pluginRE = regexp.MustCompile(`\{name:"([a-zA-Z]+)"`)

func bundlePlugins(t *testing.T) map[string]bool {
	t.Helper()
	src, err := fs.ReadFile(web.Static, "js/vendor/datastar.js")
	if err != nil {
		t.Fatalf("read vendored datastar: %v", err)
	}
	names := map[string]bool{}
	for _, m := range pluginRE.FindAllStringSubmatch(string(src), -1) {
		names[m[1]] = true
	}
	// A bundle that yielded nothing would make every assertion below vacuous.
	for _, required := range []string{"on", "bind", "text", "signals"} {
		if !names[required] {
			t.Fatalf("bundle exposes no %q plugin — parser is out of date, found %v", required, sortedKeys(names))
		}
	}
	return names
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var dataAttrRE = regexp.MustCompile(`\sdata-([a-zA-Z0-9:._-]+)=`)

// pluginOf mirrors the bundle's own parse: strip __modifiers, then split the
// plugin from its key on the first colon.
func pluginOf(attr string) string {
	base, _, _ := strings.Cut(attr, "__")
	name, _, _ := strings.Cut(base, ":")
	return name
}

func TestDatastarAttributes_NamePluginsTheBundleRegisters(t *testing.T) {
	plugins := bundlePlugins(t)

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

	seen := map[string]bool{}
	for _, out := range rendered {
		for _, m := range dataAttrRE.FindAllStringSubmatch(out, -1) {
			attr, plugin := m[1], pluginOf(m[1])
			seen[attr] = true
			if !plugins[plugin] {
				t.Errorf("data-%s names plugin %q, which the vendored bundle does not register (it has %v)",
					attr, plugin, sortedKeys(plugins))
			}
		}
	}

	// Guard the guard: if the primitives stopped emitting handlers entirely,
	// every check above would pass by having nothing to check.
	for _, want := range []string{"on:click", "on:change", "bind"} {
		if !seen[want] {
			t.Errorf("no primitive emitted data-%s; the scan proves nothing", want)
		}
	}
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
