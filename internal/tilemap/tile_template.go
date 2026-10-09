package tilemap

import (
	"fmt"
	"sort"
	"strings"

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

// ValidateTileTemplateComponents checks the components that importing this
// artwork would create against its entity type's required/allowed contract.
// Position is only attached to a referenced entity when that type requires it;
// the positioned Tile always owns its own Position. TileVisual and
// TileEntityOwner are added by the importer, not by the TSX author.
func ValidateTileTemplateComponents(s *schema.DatabaseSchema, ts *tiled.Tileset, local uint32) world.ValidationResult {
	if s == nil || ts == nil {
		return world.ValidationResult{Errors: []string{"no schema or tileset is available to validate"}}
	}
	typeName := ts.Tiles[local].Properties.Get("entityType")
	if typeName == "" {
		typeName = ts.Properties.Get("entityType")
	}
	typeSpec, known := s.EntityTypes[typeName]
	if !known {
		return world.ValidateEntityCreation(s, typeName, nil)
	}
	names := map[string]bool{"TileVisual": true, "TileEntityOwner": true}
	if typeSpec.IsComponentRequired("Position") {
		names["Position"] = true
	}
	for name := range tileTemplateProperties(ts, local) {
		component, _, _ := strings.Cut(name, ".")
		if _, canonical := schema.ComponentByName(s, component); canonical != "" {
			names[canonical] = true
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	return world.ValidateEntityCreation(s, typeName, ordered)
}
