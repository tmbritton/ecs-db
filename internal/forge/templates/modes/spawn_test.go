package modes

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

// Browser selectors have a Go-side owner, so changing a test id fails in
// seconds rather than timing out only after starting Chromium.
func TestSpawnPage_PinsBrowserTestIDs(t *testing.T) {
	obj := tiled.Object{ID: 1, Type: "TestGoblin", X: 16, Y: 16}
	data := Data{
		Schema: schema.DatabaseSchema{EntityTypes: map[string]schema.EntityType{
			"TestGoblin": {RequiredComponents: []string{"Position"}},
			"Marker":     {RequiredComponents: []string{"Health"}},
			"Tile":       {RequiredComponents: []string{"Position"}},
		}},
		Canvas:        mapcanvas.Canvas{TileW: 16, TileH: 16},
		ObjectGroups:  []tiled.ObjectGroup{{Name: "spawns", Objects: []tiled.Object{obj}}},
		SelectedSpawn: &obj,
		SelectedMap:   "level.tmx", MapView: MapView{Path: "level.tmx"},
	}
	markup := render(t, spawnPalette(data)) + render(t, mapCanvas(data)) + render(t, spawnInspector(data))
	for _, id := range []string{
		"spawn-type-TestGoblin", "spawn-type-Marker", "object-group-spawns",
		"spawn-canvas", "spawn-1", "spawn-selected", "spawn-delete",
	} {
		if n := strings.Count(markup, `data-testid="`+id+`"`); n != 1 {
			t.Errorf("data-testid=%q occurs %d times, want 1", id, n)
		}
	}
	if !strings.Contains(markup, "does not declare Position") {
		t.Error("unspawnable type has no visible reason")
	}
	if strings.Contains(markup, `data-testid="spawn-type-Tile"`) {
		t.Error("the tile importer's own type is not a spawn to offer")
	}
	data.SelectedSpawn = nil
	data.MissingSpawn = 1
	markup = render(t, spawnInspector(data))
	for _, id := range []string{"spawn-missing", "spawn-clear-selection"} {
		if !strings.Contains(markup, `data-testid="`+id+`"`) {
			t.Errorf("a deleted selection needs %s", id)
		}
	}
}
