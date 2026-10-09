package modes

import (
	"bytes"
	"context"
	"regexp"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/tilelinks"
	"github.com/tmbritton/ecs-db/internal/forge/tilesets"
	"github.com/tmbritton/ecs-db/internal/forge/tilesurface"
	"github.com/tmbritton/ecs-db/internal/forge/validation"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

var testIDRE = regexp.MustCompile(`data-testid="([^"]+)"`)

// 05-app-shell.spec.js reads the stub through these, so a rename here is a
// broken browser suite. Catching it in `go test` costs seconds; catching it in
// Playwright costs a timeout and a screenshot.
func TestStubs_ExposeTheTestIDsTheSuiteSelectsOn(t *testing.T) {
	for _, m := range mode.All {
		if m.Slug == "tiles" {
			continue // TILES now has a real file list and inspector.
		}
		t.Run(m.Slug, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Render(m.Slug, Data{}).Render(context.Background(), &buf); err != nil {
				t.Fatalf("render: %v", err)
			}
			seen := map[string]int{}
			for _, match := range testIDRE.FindAllStringSubmatch(buf.String(), -1) {
				seen[match[1]]++
			}
			for _, id := range []string{"mode-stub", "mode-stub-caption", "mode-stub-epic"} {
				if seen[id] != 1 {
					t.Errorf("data-testid=%q appears %d times, want exactly 1", id, seen[id])
				}
			}
		})
	}
}

func TestTilesMode_ExposesTheTestIDsTheSuiteSelectsOn(t *testing.T) {
	var buf bytes.Buffer
	data := Data{
		Tilesets:        []tilesets.Entry{{Path: "shared.tsx", Writable: true, Maps: []string{"a.tmx"}}, {Path: "legacy.tsj", Problem: "read-only"}},
		SelectedTileset: "legacy.tsj",
	}
	if err := Render("tiles", data).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	assertModeTestIDs(t, buf.String(), []string{
		"tiles-mode", "tileset-list", "tileset-panel", "tileset-legacy.tsj", "tileset-shared.tsx",
		"tileset-title", "tileset-path", "tileset-readonly", "tileset-maps", "tileset-problem",
	})
}

func TestTilesMode_DistinctPathsWithSameBasenameHaveUniqueRowTestIDs(t *testing.T) {
	var buf bytes.Buffer
	data := Data{Tilesets: []tilesets.Entry{{Path: "one/shared.tsx"}, {Path: "two/shared.tsx"}}}
	if err := Render("tiles", data).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	assertModeTestIDs(t, buf.String(), []string{"tileset-shared.tsx", "tileset-shared.tsx-2"})
}

func TestTilesMode_ReadSurfaceIDsAreUniqueAndBrowserPinned(t *testing.T) {
	tile := tilesurface.Tile{
		ID: 13, Class: "hallway", ImageURL: "/forge/asset?path=art.png", SW: 16, SH: 16, SheetW: 32, SheetH: 32,
		Properties: []tilesurface.Property{{Name: "kind", Type: "class", PropertyType: "Terrain", Value: "water"}},
	}
	data := Data{
		Tilesets: []tilesets.Entry{{Path: "tiles.tsx", Writable: true}}, SelectedTileset: "tiles.tsx",
		Tileset:  &tiled.Tileset{Name: "sheet", TileCount: 1, TileWidth: 16, TileHeight: 16},
		TileView: tilesurface.View{Tiles: []tilesurface.Tile{tile}, Selected: &tile, TileCount: 1, Pages: 3, Page: 1},
		Problem:  "a refused tile edit",
	}
	var buf bytes.Buffer
	if err := Render("tiles", data).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	assertModeTestIDs(t, buf.String(), []string{
		"tile-grid", "tile-inspector", "tile-selected", "tile-13", "tile-grid-art-13",
		"tile-inspector-art-13", "tile-class", "tile-properties", "tileset-metadata",
		"tile-page-next", "tile-page-previous",
		"tile-class-input", "tile-property-name", "tile-property-type", "tile-property-value", "tile-property-submit",
		"tile-grid-class-13", "tile-property-editor", "tileset-edit-problem",
	})
	if !bytes.Contains(buf.Bytes(), []byte("Terrain")) {
		t.Error("selected tile's authored custom property type was omitted")
	}
	if !bytes.Contains(buf.Bytes(), []byte(`role="listitem"`)) {
		t.Error("the tile grid advertised a list without accessible list items")
	}
}

func TestMAPTileInspector_NamesTheWorkingArtworkClass(t *testing.T) {
	data := Data{MapView: MapView{LayerID: 1}, SelectedTile: &tilelinks.Inspection{GID: 15, EntityType: "Floor", ArtworkClass: "hallway"}}
	var buf bytes.Buffer
	if err := tileInspector(data).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`data-testid="tile-artwork-class"`)) || !bytes.Contains(buf.Bytes(), []byte("hallway")) {
		t.Fatal("MAP's selected Tile concealed the working TSX artwork class")
	}
}

// 12-inline-validation.spec.js selects on these, and unlike the ids above they
// only render when something is wrong — so a rename would not show up in any
// other test either. AGENTS.md asks for the pin for exactly this reason: the
// failure arrives in `go test` in seconds instead of as a browser timeout.
func TestValidation_ExposesTheTestIDsTheSuiteSelectsOn(t *testing.T) {
	// One report carrying every shape of problem at once, so a single render of
	// each mode has to produce all of them.
	report := validation.Report{
		Partial: true,
		Problems: []validation.Problem{
			{Message: "a file-wide problem", Blocking: true},
			{
				Owner:    validation.Owner{Kind: validation.OwnerComponent, Name: "Health"},
				Message:  "a problem about the component itself",
				Blocking: true,
			},
			{
				Owner:   validation.Owner{Kind: validation.OwnerComponent, Name: "Health"},
				Field:   validation.PropertyField("hp"),
				Message: "a problem about a field",
			},
			{
				Owner:    validation.Owner{Kind: validation.OwnerEntityType, Name: "Player"},
				Message:  "a problem about the entity type itself",
				Blocking: true,
			},
		},
	}

	tests := []struct {
		mode     string
		selected string
		render   func(Data) string
		want     []string
	}{
		{
			mode: "schema", selected: "Health",
			render: func(d Data) string { return renderMode(t, d) },
			want: []string{
				"file-problems", "problems-partial", "component-problems",
				"field-problems-hp", "row-problem-Health",
			},
		},
		{
			mode: "ents", selected: "Player",
			render: func(d Data) string { return renderEnts(t, d) },
			want:   []string{"file-problems", "problems-partial", "type-problems", "row-problem-Player"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			data := modeFixture()
			data.Selected = tc.selected
			data.Validation = report

			seen := map[string]int{}
			for _, match := range testIDRE.FindAllStringSubmatch(tc.render(data), -1) {
				seen[match[1]]++
			}
			for _, id := range tc.want {
				if seen[id] != 1 {
					t.Errorf("data-testid=%q appears %d times, want exactly 1", id, seen[id])
				}
			}
		})
	}
}

// 13-state-inspector.spec.js selects on these. AGENTS.md asks for the pin so a
// rename fails in seconds rather than as a browser timeout minutes later, and
// the parameterised ones — built from a kind, an index and a parameter name —
// are the easiest to break without noticing.
func TestInspector_ExposesTheTestIDsTheSuiteSelectsOn(t *testing.T) {
	data := inspectorFixture(t, "state:idle")
	data.SelectedState.Entry = []agent.ActionSpec{{Type: "dealDamage"}}
	data.SelectedStateWarning = "Delete state idle? Nothing transitions into it."

	var buf bytes.Buffer
	if err := Render("agents", data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	assertModeTestIDs(t, buf.String(), []string{
		"state-inspector", "state-name", "state-rename-note", "already-initial",
		"delete-state", "entry-actions", "exit-actions", "catalogue-is-closed",
		"add-entry-action", "add-exit-action",
		"entry-action-0", "remove-entry-0", "entry-desc-0",
		"param-entry-0-amount", "input-entry-0-amount", "problem-entry-0-amount",
		"param-entry-0-target", "input-entry-0-target",
	})

	// The ones that only render in the other states, which no other test would
	// notice going missing either.
	var empty bytes.Buffer
	if err := Render("agents", inspectorFixture(t, "")).Render(context.Background(), &empty); err != nil {
		t.Fatalf("render: %v", err)
	}
	assertModeTestIDs(t, empty.String(), []string{"inspector-empty"})

	// 13-transition-inspector.spec.js selects on these.
	transition := transitionFixture(t, guardedGo)
	var edge bytes.Buffer
	if err := Render("agents", transition).Render(context.Background(), &edge); err != nil {
		t.Fatalf("render: %v", err)
	}
	assertModeTestIDs(t, edge.String(), []string{
		"transition-inspector", "transition-route", "transition-event",
		"transition-target", "transition-guard", "delete-transition",
		"param-guard-distance", "input-guard-distance", "problem-guard-target",
		"taction-actions", "add-taction-action",
		"transition-order", "order-note", "order-0", "order-1",
		"move-up-0", "move-down-0", "move-up-1", "move-down-1",
	})

	notInitial := inspectorFixture(t, "state:combat")
	var other bytes.Buffer
	if err := Render("agents", notInitial).Render(context.Background(), &other); err != nil {
		t.Fatalf("render: %v", err)
	}
	assertModeTestIDs(t, other.String(), []string{"set-initial"})
}

// assertModeTestIDs is the shared check: present, and present exactly once — a
// duplicate makes a Playwright locator match two elements and surfaces as a
// strict-mode violation a long way from its cause.
func assertModeTestIDs(t *testing.T, markup string, want []string) {
	t.Helper()
	seen := map[string]int{}
	for _, match := range testIDRE.FindAllStringSubmatch(markup, -1) {
		seen[match[1]]++
	}
	for _, id := range want {
		switch seen[id] {
		case 1:
		case 0:
			t.Errorf("data-testid=%q is no longer rendered; e2e/specs select on it", id)
		default:
			t.Errorf("data-testid=%q is rendered %d times; it must be unique", id, seen[id])
		}
	}
}
