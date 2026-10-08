package modes

import (
	"regexp"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
	"github.com/tmbritton/ecs-db/internal/forge/mapvalidation"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func TestMapValidation_RendersMessagesAndMarksTheirOwners(t *testing.T) {
	data := mapRegionFixture()
	data.Canvas.CellsByLayer = map[int][]mapcanvas.Cell{0: data.Canvas.Cells}
	obj := tiled.Object{ID: 7, Type: "Goblin"}
	data.ObjectGroups = []tiled.ObjectGroup{{Name: "spawns", Objects: []tiled.Object{obj}}}
	data.MapValidation = mapvalidation.Report{Issues: []mapvalidation.Issue{
		{Kind: mapvalidation.Map, Message: "missing mapId", Warning: true},
		{Kind: mapvalidation.Layer, Layer: 0, Message: "wrong shape"},
		{Kind: mapvalidation.Cell, Layer: 0, X: 0, Y: 0, Message: "gid outside tileset"},
		{Kind: mapvalidation.Spawn, Group: 0, ObjectIndex: 0, ObjectID: 7, Message: "unknown entity type"},
	}}
	markup := render(t, MapHeadRegion(data)) + render(t, MapListRegion(data)) + render(t, MapCanvasRegion(data))
	for _, want := range []string{
		`data-testid="map-id"`, `data-testid="map-id-warning"`, `data-testid="map-validation"`,
		"wrong shape", "gid outside tileset", "unknown entity type",
		`data-testid="layer-ground"`, `data-testid="spawn-7"`, `data-cell="ground:0,0"`,
		`data-invalid="true"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing owner mark or feedback %q", want)
		}
	}
	for _, id := range []string{"map-layer-0", "map-validation-count"} {
		if n := strings.Count(markup, `data-testid="`+id+`"`); n != 1 {
			t.Errorf("browser selector %s occurs %d times, want one", id, n)
		}
	}
}

func TestMapValidation_PinsConflictTilesetAndDuplicateSpawnBrowserSelectors(t *testing.T) {
	data := mapRegionFixture()
	data.MapID = "shared"
	data.ObjectGroups = []tiled.ObjectGroup{{Name: "spawns", Objects: []tiled.Object{
		{ID: 7, Type: "Goblin"}, {ID: 7, Type: "Goblin", X: 16},
	}}}
	data.MapValidation = mapvalidation.Report{Issues: []mapvalidation.Issue{
		{Kind: mapvalidation.Map, Message: "maps share an id"},
		{Kind: mapvalidation.Tileset, Tileset: 0, Message: "missing tileset"},
		{Kind: mapvalidation.Spawn, Group: 0, ObjectIndex: 0, ObjectID: 7, Message: "duplicate"},
		{Kind: mapvalidation.Spawn, Group: 0, ObjectIndex: 1, ObjectID: 7, Message: "duplicate"},
	}}
	markup := render(t, MapHeadRegion(data)) + render(t, MapCanvasRegion(data))
	for _, id := range []string{"map-identity-conflict", "map-tileset-problem-0", "map-validation-count", "spawn-7-g0-i0", "spawn-7-g0-i1"} {
		if n := strings.Count(markup, `data-testid="`+id+`"`); n != 1 {
			t.Errorf("browser selector %s occurs %d times, want one", id, n)
		}
	}
	if regexp.MustCompile(`<a[^>]*data-testid="spawn-7-g0-i[01]"`).MatchString(markup) ||
		strings.Contains(markup, `draggable="true" data-on:dragstart=`) {
		t.Error("ambiguous IDs expose a navigation or drag that edits the wrong claimant")
	}
}
