// Package modes holds the per-mode content components the shell renders.
//
// Modes register themselves here rather than the shell knowing about them,
// which is what keeps the shell free of mode-specific knowledge.
package modes

import "github.com/a-h/templ"

// A Region is one independently patchable area of a mode's page: an element id
// and how to render it.
//
// The SSE stream renders each region, compares it with what it last sent, and
// patches only the ones that moved. So the split decides what an edit costs:
// MAP's canvas is fifteen times the size of everything else on the page put
// together, and while it shared a region with the rest, choosing a tile to
// paint with re-sent three hundred cells to change one CSS class.
//
// Two rules make that work, both checked in regions_test.go:
//
//   - Regions must be disjoint. One nested inside another is dirtied whenever
//     the inner one changes, so both get sent and the split buys nothing.
//   - Every region's id must be present in every state the mode can be in. A
//     patch addresses an element by id, and an id that comes and goes is a
//     patch that silently lands nowhere for the life of the tab.
type Region struct {
	ID     string
	Render func(Data) templ.Component
}

// Content is a mode: its patchable regions, and how a whole page is assembled
// from them.
//
// Page takes the regions already rendered, in Regions order, rather than the
// Data they came from. That is what stops a page and the stream that follows it
// from being two renders that merely ought to agree — the page is built out of
// the very bytes the stream would send, so "the browser already has this" is a
// fact rather than a hope.
type Content struct {
	Regions []Region
	Page    func(rendered []string) templ.Component
	// Signals is the client-owned view state this mode declares, as a JSON
	// object literal, or empty for a mode that has none.
	//
	// It is emitted on `<main id="mode-content">`, which a page load renders and
	// the stream never patches — the regions inside it are what get patched. So
	// a signal declared here survives every patch, which is the whole point:
	// what the server does not render, a re-render cannot clobber.
	//
	// Seeded from Data because the *initial* value is the server's to know — a
	// layer the map file hides must come up hidden. After that the browser owns
	// it and the server never hears about it again, except when a request that
	// needs it carries the signals along.
	Signals func(Data) string
}

// Registry maps a mode slug to its content.
//
// A lookup rather than a switch: the handler does not grow a branch per mode,
// and registry_test.go can check it against the mode table in both directions.
var Registry = map[string]Content{
	"map": {
		Regions: []Region{
			{"mode-list", MapListRegion},
			// The right-hand side is three regions, not one, and the middle one
			// is the reason: the canvas is ~70KB of a ~75KB page, so a refusal
			// banner or a zoom readout sharing a region with it costs three
			// hundred cells to change a line of text.
			{"map-head", MapHeadRegion},
			// map-canvas-region, not map-canvas: the element inside it already
			// answers to data-testid="map-canvas", and two different elements
			// both called "map-canvas" is a name that has to be disambiguated
			// every time it is read.
			{"map-canvas-region", MapCanvasRegion},
			{"map-inspector", MapInspectorRegion},
			{"map-foot", MapFootRegion},
		},
		Page:    MapPage,
		Signals: MapSignals,
	},
	"tiles": {
		Regions: []Region{
			{"mode-list", TilesListRegion},
			{"tileset-head", TilesHeadRegion},
			{"tileset-grid-region", TilesGridRegion},
			{"tileset-inspector-region", TilesInspectorRegion},
		},
		Page: TilesPage,
		Signals: func(Data) string {
			return `{"tilePropertyName":"","tilePropertyType":"string","tilePropertyValue":""}`
		},
	},
	"ents":   {Regions: twoPane(EntsListRegion, EntsMainRegion), Page: TwoPanePage("ents-mode")},
	"schema": {Regions: twoPane(SchemaListRegion, SchemaMainRegion), Page: TwoPanePage("schema-mode")},
	"agents": {Regions: twoPane(AgentsListRegion, AgentsMainRegion), Page: TwoPanePage("agents-mode")},
	"sprites": {
		Regions: twoPane(SpritesListRegion, SpritesMainRegion), Page: TwoPanePage("sprites-mode"),
		Signals: func(Data) string {
			return `{"spriteCreateName":"","spriteCreateSheet":"","spriteCreateFrames":"0","spriteCreateFPS":"8","spriteCreateLoop":"true","spriteBindingName":"","spriteBindingSheet":""}`
		},
	},
}

// twoPane is the shape every mode but MAP has: a list rail and an editor panel.
func twoPane(list, main func(Data) templ.Component) []Region {
	return []Region{{"mode-list", list}, {"mode-main", main}}
}
