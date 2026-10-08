package modes

import (
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/tilelinks"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

func hasAuthoredTileLink(obj tiled.Object) bool {
	for name := range obj.Properties {
		if strings.EqualFold(name, "TileLink.layerID") || strings.EqualFold(name, "TileLink.cells") {
			return true
		}
	}
	return false
}

func invalidTileLinkID(counts map[int]int, group, index, id int) string {
	label := "tile-link-object-" + itoa(id)
	if counts[id] > 1 {
		return label + "-g" + itoa(group) + "-i" + itoa(index)
	}
	return label
}

type tileLinkOption struct {
	ID      int
	Type    string
	Summary string
	Blocked string
}

type tileLinkedView struct {
	Type, Visual, Passability, Visibility string
}

func tileLinkedObject(data Data, id int) tileLinkedView {
	for _, group := range data.ObjectGroups {
		for _, obj := range group.Objects {
			if obj.ID == id {
				return tileLinkedView{
					Type: obj.Type, Visual: tileProperty(obj.Properties, "TileVisual.image"),
					Passability: tileProperty(obj.Properties, "Passability.kind"),
					Visibility:  tileProperty(obj.Properties, "Visibility.kind"),
				}
			}
		}
	}
	return tileLinkedView{}
}

func tileProperty(props tiled.Properties, wanted string) string {
	var value string
	found := false
	for name, prop := range props {
		if strings.EqualFold(name, wanted) {
			if found {
				return "" // two spellings need repair; do not guess which one wins
			}
			value, found = prop.Value, true
		}
	}
	return value
}

func tileLinkCandidates(data Data) []tileLinkOption {
	if data.SelectedTile == nil || data.SelectedTile.Empty {
		return nil
	}
	counts := spawnIDCounts(data.ObjectGroups)
	linked := make(map[int]bool, len(data.SelectedTile.ObjectIDs))
	for _, id := range data.SelectedTile.ObjectIDs {
		linked[id] = true
	}
	m := &tiled.Map{
		Width: data.Canvas.Cols, Height: data.Canvas.Rows,
		TileWidth: data.Canvas.TileW, TileHeight: data.Canvas.TileH,
	}
	var artValidator *tilelinks.ArtValidator
	if data.MapPreview != nil {
		artValidator = tilelinks.NewArtValidator(data.MapPreview)
	}
	var options []tileLinkOption
	for _, group := range data.ObjectGroups {
		for _, obj := range group.Objects {
			otherLayer := false
			for name, prop := range obj.Properties {
				if strings.EqualFold(name, "TileLink.layerID") {
					parsed, err := strconv.Atoi(strings.TrimSpace(prop.Value))
					if err != nil || parsed != data.MapView.LayerID {
						otherLayer = true
					}
				}
			}
			if obj.Type == "" || obj.ID <= 0 || counts[obj.ID] != 1 || linked[obj.ID] || otherLayer {
				continue
			}
			verdict, err := tilemap.ValidateSpawn(&data.Schema, m, obj)
			if err != nil || !verdict.Valid() || tilemap.ValidateSpawnFields(&data.Schema, m, obj) != nil {
				continue
			}
			option := tileLinkOption{ID: obj.ID, Type: obj.Type, Summary: tileObjectSummary(obj.Properties)}
			if artValidator != nil {
				if err := artValidator.CanLink(obj.ID, data.MapView.LayerID, tilelinks.Cell{
					X: data.MapView.CellX, Y: data.MapView.CellY,
				}); err != nil {
					option.Blocked = err.Error()
				}
			}
			options = append(options, option)
		}
	}
	return options
}

func tileObjectSummary(props tiled.Properties) string {
	var parts []string
	for _, item := range []struct {
		field, label string
	}{
		{"TileVisual.image", "art"},
		{"Sprite.sheet", "sprite"},
		{"Passability.kind", "movement"},
		{"Visibility.kind", "sight"},
	} {
		if value := tileProperty(props, item.field); value != "" {
			parts = append(parts, item.label+" "+value)
		}
	}
	if len(parts) == 0 {
		return "no authored art or restriction"
	}
	return strings.Join(parts, " · ")
}

func tileLinkAction(data Data) string {
	return valueAction("/forge/map/tile/link", "id",
		"map", data.SelectedMap, "layer", itoa(data.MapView.LayerID),
		"x", itoa(data.MapView.CellX), "y", itoa(data.MapView.CellY), "action", "link")
}

func tileUnlinkAction(data Data, id int) string {
	return action("/forge/map/tile/link", "map", data.SelectedMap,
		"layer", itoa(data.MapView.LayerID), "x", itoa(data.MapView.CellX),
		"y", itoa(data.MapView.CellY), "action", "unlink", "id", itoa(id))
}
