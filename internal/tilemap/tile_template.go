package tilemap

import (
	"fmt"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/world"
)

// referencedTileComponents builds one authored entity from a tileset tile's
// entityType and schema-named component properties. The placed Tile supplies
// Position; its referenced Floor/Wall/River owns visual and interaction state.
func referencedTileComponents(s *schema.DatabaseSchema, tile LayerTile, ownerID int64) ([]world.EntityComponent, error) {
	entityType, found := s.EntityTypes[tile.State.EntityType]
	if !found {
		return nil, fmt.Errorf("tile at layer %d cell (%d,%d) references unknown entity type %q", tile.LayerID, tile.X, tile.Y, tile.State.EntityType)
	}
	m := &tiled.Map{
		Width: tile.MapWidth, Height: tile.MapHeight,
		TileWidth: tile.CellWidth, TileHeight: tile.CellHeight,
	}
	obj := tiled.Object{
		Type: tile.State.EntityType,
		X:    float64(tile.X * tile.CellWidth), Y: float64(tile.Y * tile.CellHeight),
		Properties: tile.TemplateProperties,
	}
	components, err := spawnComponents(s, m, obj)
	if err != nil {
		return nil, fmt.Errorf("tile at layer %d cell (%d,%d): %w", tile.LayerID, tile.X, tile.Y, err)
	}
	withoutPosition := components[:0]
	for _, component := range components {
		if component.Name == "Position" && !entityType.IsComponentRequired("Position") {
			continue
		}
		withoutPosition = append(withoutPosition, component)
	}
	components = append(withoutPosition,
		world.EntityComponent{Name: "TileVisual", Values: visualValues(tile)},
		world.EntityComponent{Name: "TileEntityOwner", Values: world.ComponentValues{"target_entity_id": ownerID}},
	)
	names := make([]string, len(components))
	for i, component := range components {
		names[i] = component.Name
	}
	result := world.ValidateEntityCreation(s, tile.State.EntityType, names)
	if !result.Valid() {
		return nil, fmt.Errorf("tile at layer %d cell (%d,%d): %s", tile.LayerID, tile.X, tile.Y, result.Errors[0])
	}
	return components, nil
}
