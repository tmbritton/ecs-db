package tilemap_test

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

func TestMapIssues_ReportsEveryEngineRefusalAgainstItsLayerAndCell(t *testing.T) {
	m := &tiled.Map{
		Name: "bad.tmx", Width: 2, Height: 1, TileWidth: 16, TileHeight: 16,
		Tilesets: []tiled.TilesetRef{{FirstGID: 10, Tileset: &tiled.Tileset{Name: "floor", TileCount: 2, Image: tiled.Image{Source: "fixture.png", Path: "fixture.png"}}}},
		Layers: []tiled.Layer{
			{Name: "ground", Width: 2, Height: 1, Data: []uint32{1, 99}},
			{Name: "small", Width: 1, Height: 1, Data: []uint32{0}},
		},
	}
	issues := tilemap.MapIssues(m)
	if len(issues) != 3 {
		t.Fatalf("got %d issues, want two cells and the short layer: %+v", len(issues), issues)
	}
	for i, tc := range []struct {
		layer, x, y int
		want        string
	}{
		{1, -1, -1, `layer "small" is 1×1 but the map is 2×1`},
		{0, 0, 0, "below the first tile"},
		{0, 1, 0, "does not hold it"},
	} {
		got := issues[i]
		if got.Layer != tc.layer || got.X != tc.x || got.Y != tc.y || !strings.Contains(got.Message, tc.want) {
			t.Errorf("issue %d = %+v, want layer %d cell %d,%d with %q", i, got, tc.layer, tc.x, tc.y, tc.want)
		}
	}
}

func TestMapIssues_CoveredBadTileIsNotAnEngineRefusal(t *testing.T) {
	m := &tiled.Map{
		Name: "stacked.tmx", Width: 1, Height: 1,
		Tilesets: []tiled.TilesetRef{{FirstGID: 1, Tileset: &tiled.Tileset{
			Name: "floor", TileCount: 1, Image: tiled.Image{Source: "floor.png"},
		}}},
		Layers: []tiled.Layer{
			{Name: "buried", Width: 1, Height: 1, Data: []uint32{99}},
			{Name: "top", Width: 1, Height: 1, Data: []uint32{1}},
		},
	}
	if issues := tilemap.MapIssues(m); len(issues) != 0 {
		t.Errorf("engine loads the valid top tile; buried bad gid is not a load refusal: %+v", issues)
	}
	m.Layers[1].Data[0] = 0
	issues := tilemap.MapIssues(m)
	if len(issues) != 1 || issues[0].Layer != 0 || !strings.Contains(issues[0].Message, "does not hold it") {
		t.Errorf("uncovering the bad tile did not attach its error to the buried layer: %+v", issues)
	}
}

func TestMapIssues_NoTilesetsReportsEveryAuthoredCell(t *testing.T) {
	m := &tiled.Map{Name: "empty.tmx", Width: 2, Height: 1, Layers: []tiled.Layer{{Name: "ground", Width: 2, Height: 1, Data: []uint32{1, 2}}}}
	issues := tilemap.MapIssues(m)
	if len(issues) != 2 || !strings.Contains(issues[0].Message, "no tilesets at all") ||
		!strings.Contains(issues[1].Message, "no tilesets at all") {
		t.Errorf("missing cell-specific engine refusals: %+v", issues)
	}
}

func TestMapIssues_NonOrthogonalMapUsesTheLoaderRefusal(t *testing.T) {
	m := &tiled.Map{Name: "hex.tmx", Width: 1, Height: 1, Orientation: "hexagonal"}
	issues := tilemap.MapIssues(m)
	if len(issues) == 0 || issues[0].Layer != -1 ||
		!strings.Contains(issues[0].Message, "square cells") {
		t.Errorf("engine orientation refusal missing: %+v", issues)
	}
}
