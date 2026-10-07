package modes

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func TestMapContextMenu_TriggersAndActionsAreRendered(t *testing.T) {
	data := mapRegionFixture()
	data.Canvas.Layers = append(data.Canvas.Layers, mapcanvas.Layer{Index: 1, Name: "props"})
	data.ObjectGroups = []tiled.ObjectGroup{{Name: "spawns", Objects: []tiled.Object{{ID: 7, Type: "Goblin"}}}}
	for _, tc := range []struct {
		name string
		menu MapMenu
		want []string
	}{
		{
			"layer",
			MapMenu{Open: true, Kind: "layer", Path: "level.tmx", Layer: 1, LayerID: 3, ViewID: 3, LayerName: "props", CanMoveUp: true, CanDelete: true},
			[]string{`data-testid="map-context-menu"`, "Rename", "hideID3", "Move up", "Delete layer", "/forge/map/layer", "autofocus"},
		},
		{
			"spawn",
			MapMenu{Open: true, Kind: "spawn", Path: "level.tmx", ObjectID: 7},
			[]string{`data-testid="map-context-menu"`, "Duplicate", "Delete spawn", "/forge/map/spawn/duplicate", "/forge/map/spawn/delete"},
		},
		{
			"canvas",
			MapMenu{Open: true, Kind: "canvas", Path: "level.tmx", Tile: tiled.Tile{GID: 2, FlipH: true}},
			[]string{`data-testid="map-context-menu"`, "Pick tile", "$tile = 2", "$flipH = true", "/forge/map/menu?close=1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data.MapMenu = tc.menu
			got := render(t, MapHeadRegion(data))
			for _, fragment := range tc.want {
				if !strings.Contains(got, fragment) {
					t.Errorf("menu missing %q", fragment)
				}
			}
		})
	}
	data.MapMenu = MapMenu{}
	markup := render(t, MapHeadRegion(data)) + render(t, MapListRegion(data)) + render(t, MapCanvasRegion(data))
	for _, id := range []string{"layer-ground", "layer-props", "map-canvas", "spawn-7"} {
		if !strings.Contains(markup, `data-testid="`+id+`"`) {
			t.Errorf("browser selector %s missing", id)
		}
	}
	if n := strings.Count(markup, `data-on:contextmenu=`); n != 4 {
		t.Errorf("%d context-menu triggers, want two layers, canvas and spawn", n)
	}
}

func TestMapContextMenu_LegacyLayerDoesNotOfferEditsItCannotMake(t *testing.T) {
	menu := MapMenu{Open: true, Kind: "layer", Path: "old.tmx", LayerName: "ground", Layer: 0}
	got := render(t, mapContextMenu(Data{MapMenu: menu}))
	if strings.Contains(got, "Move up") || strings.Contains(got, "Move down") || strings.Contains(got, "Delete layer") {
		t.Errorf("legacy layer offered an action that must be refused: %s", got)
	}
	if !strings.Contains(got, "Rename") || !strings.Contains(got, "Show / hide") {
		t.Errorf("legacy layer lost its working menu items: %s", got)
	}
}
