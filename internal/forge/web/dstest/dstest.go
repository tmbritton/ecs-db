// Package dstest checks rendered markup against the Datastar bundle that
// actually ships.
//
// Datastar attributes fail silently. An attribute whose plugin name does not
// resolve is skipped with no error, no console warning and no visual
// difference — the control simply never does anything. That is how every
// handler in this project once shipped inert: v1.0.2 separates a plugin from
// its key with a colon (`data-on:click`), so `data-on-click` parses as a
// plugin named "on-click", which is not registered. Nothing caught it: not the
// compiler, not the linter, not a screenshot.
//
// So the vendored bundle is the source of truth. Any package that renders
// Datastar attributes uses this from its tests to prove each one names a
// plugin the bundle registers.
package dstest

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/web"
)

// The bundle registers plugins as `{name:"on",…}`. Minified, but the shape is
// stable across builds, and a rename would surface here as a failure rather
// than as silently dead UI.
//
// Hyphens are matched because several plugin names contain them
// (`on-intersect`, `on-interval`, `on-signal-patch`, `json-signals`). That
// also admits the two SSE event-type constants, which are declared with the
// same `name:` shape — harmless, since nothing would write
// `data-datastar-patch-elements`, and excluding them would mean rejecting the
// four real hyphenated plugins.
var pluginRE = regexp.MustCompile(`\{name:"([a-zA-Z0-9-]+)"`)

var dataAttrRE = regexp.MustCompile(`\sdata-([a-zA-Z0-9:._-]+)=`)

// Plain HTML data attributes that are deliberately ours, not Datastar's.
// Datastar ignores an attribute whose plugin it does not recognise, which is
// precisely what makes these safe — and precisely what makes a typo in a real
// Datastar attribute invisible. So the exemption is an explicit list rather
// than a pattern: adding one is a decision someone has to write down, and
// anything not on it is still treated as a misspelled plugin.
var nonDatastarAttrs = map[string]bool{
	"testid": true, // e2e selector of record; see e2e/README.md
	"active": true, // mirrors an --active class so a test can assert both states
	// Mirrors the destructive styling on a migration statement, so a test can
	// assert which statements are marked without parsing class lists.
	"destructive": true,
	// The component an editor panel is showing, for the same reason.
	"component": true,
	// The entity type an editor panel is showing.
	"type": true,
	// Whether the save footer is refusing to save, so a test can assert the
	// state rather than infer it from a disabled attribute that also means
	// "nothing to save".
	"blocked": true,
	// Whether a fields-table row has a validation message under it.
	"problem": true,

	// ── MAP ──────────────────────────────────────────────────────────────────
	//
	// Which map game.toml names, so a test can assert the "loaded" mark without
	// reading the badge's wording.
	"configured": true,
	// Which cell of which layer a tile div is, which Story 5's pointer handling
	// reads to work out what was painted on.
	"cell": true,

	// ── AGENTS: the canvas and the two inspectors ────────────────────────────
	//
	// The statechart carries most of its state in plain data attributes,
	// because a browser test cannot read a class list usefully and canvas.js
	// reads several of them to work out what was dragged onto what.
	//
	// Whether a machine in the list has unsaved work, and which file it is.
	"dirty":   true,
	"machine": true,
	// Whether the working machine would load, so a test can assert the readout
	// rather than the wording beside it.
	"valid": true,
	// What the canvas has selected, in one place, so a test does not have to
	// find the marked element to find out.
	"selection": true,
	// A node's dotted path and a state's kind — canvas.js reads the first to
	// say what was dragged, and both are how a test addresses a node.
	"path": true,
	"kind": true,
	// Whether a node is the one its container enters, and whether it is
	// selected: both are drawn as marks that a test cannot read back.
	"initial":  true,
	"selected": true,
	// An edge's guard, its resolved target, and whether it resolves at all.
	// The first two are asserted directly when an edit is expected to change
	// them without a reload.
	"guard":    true,
	"to":       true,
	"dangling": true,
	// The action an inspector row is for, and the registry type its parameter
	// input was generated from — the claim "this control came from the
	// registry's schema" is otherwise unassertable.
	"action":     true,
	"param-type": true,
	// Which row of a transition order is the selected one.
	"current": true,
	// Whether a problem stops the save, which is the difference the badge and
	// the hue carry and which a test should not have to read off a class.
	"blocking": true,
	// Whether a node or an edge carries a validation error. The mark is a hue
	// and a glyph; this is how a test asks the question without reading either.
	"invalid": true,
}

// Plugins reads the plugin names the vendored bundle registers.
func Plugins(t *testing.T) map[string]bool {
	t.Helper()
	src, err := fs.ReadFile(web.Static, "js/vendor/datastar.js")
	if err != nil {
		t.Fatalf("read vendored datastar: %v", err)
	}
	names := map[string]bool{}
	for _, m := range pluginRE.FindAllStringSubmatch(string(src), -1) {
		names[m[1]] = true
	}
	// A parser that matched nothing would make every assertion vacuous.
	// "on-intersect" is in this list specifically to prove hyphenated names
	// are being picked up.
	for _, required := range []string{"on", "bind", "text", "signals", "init", "on-intersect"} {
		if !names[required] {
			t.Fatalf("bundle exposes no %q plugin — the parser is out of date, found %v",
				required, SortedKeys(names))
		}
	}
	return names
}

// PluginOf mirrors the bundle's own parse: strip __modifiers, then split the
// plugin from its key on the first colon.
func PluginOf(attr string) string {
	base, _, _ := strings.Cut(attr, "__")
	name, _, _ := strings.Cut(base, ":")
	return name
}

// AssertAttrs checks every data-* attribute in the rendered markup against the
// bundle, and returns the set of attribute names it found so a caller can
// guard against a scan that had nothing to scan.
func AssertAttrs(t *testing.T, plugins map[string]bool, rendered ...string) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	for _, out := range rendered {
		for _, m := range dataAttrRE.FindAllStringSubmatch(out, -1) {
			attr, plugin := m[1], PluginOf(m[1])
			seen[attr] = true
			if nonDatastarAttrs[plugin] {
				continue
			}
			if !plugins[plugin] {
				t.Errorf("data-%s names plugin %q, which the vendored bundle does not register (it has %v)",
					attr, plugin, SortedKeys(plugins))
			}
		}
	}
	return seen
}

// RequireSeen fails unless each attribute was actually emitted. Without it, a
// template that stopped emitting handlers would pass by having nothing left to
// check.
func RequireSeen(t *testing.T, seen map[string]bool, attrs ...string) {
	t.Helper()
	for _, want := range attrs {
		if !seen[want] {
			t.Errorf("nothing emitted data-%s; the scan proves nothing (saw %v)", want, SortedKeys(seen))
		}
	}
}

func SortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
