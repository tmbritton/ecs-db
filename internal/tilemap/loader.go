package tilemap

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/world"
)

// LoadMap reads a Tiled map, brings the Tile entities and the object layers'
// spawns in the database in line with it, and returns a populated TileGrid.
//
// It used to be a one-time bootstrap: it counted Tile entities and created none
// if any existed, which made the database the source of truth after the first
// run and the file decoration. A map edited afterwards loaded without error and
// changed nothing. Loading now diffs — see SyncLayerTiles for what the file owns and
// what survives it.
//
// One format. The engine started with a bespoke TOML of '.' and '#' characters,
// and Story 7 migrated the one map that used it and deleted the reader with the
// file: a second way to load a map is a second thing to keep working, and Forge
// writes Tiled. A file that is not a Tiled map is refused by name, by the
// parser, rather than tried as something else.
//
// It needs storage.EnsureInterpreterTables to have run: the spawns table is how
// an object that has left the map takes its entity with it.
//
// The second result is the parsed map for startup metadata such as tile size
// and object spawns. Game tile drawing reads the entities, not this map.
func LoadMap(ctx context.Context, svc *world.EntityService, db *sql.DB, path string) (*TileGrid, *tiled.Map, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("LoadMap: reading %q: %w", path, err)
	}
	file, err := readTiled(path, data)
	if err != nil {
		return nil, nil, err
	}
	mapID := MapID(path, file.Source)
	links, err := tileLinksOfMap(file.Source)
	if err != nil {
		return nil, nil, fmt.Errorf("LoadMap: Tile references in %q: %w", path, err)
	}
	linkedObjects := make(map[int]bool, len(links))
	shared := make(map[layerKey]map[string]tileLink)
	for _, link := range links {
		linkedObjects[link.objectID] = true
		for _, at := range link.cells {
			key := layerKey{mapID: mapID, layerID: link.layerID, x: at.X, y: at.Y}
			if shared[key] == nil {
				shared[key] = make(map[string]tileLink)
			}
			combined := link
			combined.providesVisual = link.providesVisual || shared[key][link.entityType].providesVisual
			shared[key][link.entityType] = combined
		}
	}
	for i := range file.LayerTiles {
		tile := &file.LayerTiles[i]
		if link, found := shared[tile.key()][tile.State.EntityType]; found {
			if !link.providesVisual {
				return nil, nil, fmt.Errorf("LoadMap: TileLink object %d replacing painted %s in layer %d at (%d,%d) needs TileVisual", link.objectID, link.entityType, tile.LayerID, tile.X, tile.Y)
			}
			tile.SharedReference = true
		}
	}
	painted := make(map[layerKey]LayerTile, len(file.LayerTiles))
	for _, tile := range file.LayerTiles {
		painted[tile.key()] = tile
	}
	for _, link := range links {
		var first *LayerTile
		for _, at := range link.cells {
			key := layerKey{mapID: mapID, layerID: link.layerID, x: at.X, y: at.Y}
			tile, found := painted[key]
			if !found || tile.State.EntityType != link.entityType || !link.providesVisual {
				continue
			}
			if first != nil && first.Visual != tile.Visual {
				return nil, nil, fmt.Errorf("LoadMap: TileLink object %d shares a single %s TileVisual across distinct artwork in layer %d; use separate per-cell art references with one shared restriction entity", link.objectID, link.entityType, link.layerID)
			}
			first = &tile
		}
	}
	// A malformed spatial restriction must not commit the layer tiles and then
	// leave a missing wall/river behind. Other spawn refusals keep their existing
	// per-object reporting contract; these fields define the walkable world.
	for _, group := range file.Source.ObjectGroups {
		for _, object := range group.Objects {
			spatial := false
			for name := range object.Properties {
				component, _, _ := strings.Cut(name, ".")
				_, canonical := schema.ComponentByName(svc.Schema(), component)
				if canonical == "OccupiedCells" || len(svc.Schema().Interactions[canonical]) > 0 ||
					strings.EqualFold(component, "TileLink") {
					spatial = true
				}
			}
			if entityType, found := svc.Schema().EntityTypes[object.Type]; found {
				for _, required := range entityType.RequiredComponents {
					if required == "OccupiedCells" || len(svc.Schema().Interactions[required]) > 0 {
						spatial = true
					}
				}
			}
			if !spatial {
				continue
			}
			validation, err := ValidateSpawn(svc.Schema(), file.Source, object)
			if err != nil {
				return nil, nil, fmt.Errorf("LoadMap: spatial object %d in %q: %w", object.ID, path, err)
			}
			if !validation.Valid() {
				return nil, nil, fmt.Errorf("LoadMap: spatial object %d in %q: %s", object.ID, path, strings.Join(validation.Errors, "; "))
			}
			if linkedObjects[object.ID] {
				components, err := spawnComponents(svc.Schema(), file.Source, object)
				if err != nil {
					return nil, nil, fmt.Errorf("LoadMap: TileLink object %d: %w", object.ID, err)
				}
				for _, component := range components {
					declared := svc.Schema().Components[component.Name]
					if schema.StorageLayout(declared.Type) != schema.LayoutColumns {
						continue
					}
					for name, property := range declared.Properties {
						if _, present := component.Values[name]; !present && !schema.PropertyNullable(property.Type) {
							return nil, nil, fmt.Errorf("LoadMap: TileLink object %d: %s.%s is required to spawn its entity", object.ID, component.Name, name)
						}
					}
				}
			}
		}
	}

	if _, err := SyncLayerTiles(ctx, svc, db, mapID, filepath.Clean(path), file.LayerTiles); err != nil {
		return nil, nil, fmt.Errorf("LoadMap: importing %q: %w", path, err)
	}

	// Spawns come from the same file as the tiles and are loaded with them: a
	// caller that wanted one without the other would be asking for half a map.
	// Refusals are returned rather than raised — one object with a typo in its
	// type should not stop the other nineteen, and the caller decides how loud
	// to be about it.
	spawns, err := SyncSpawns(ctx, svc, db, filepath.Clean(path), file.Source)
	if err != nil {
		return nil, nil, fmt.Errorf("LoadMap: spawning from %q: %w", path, err)
	}
	if err := syncTileReferences(ctx, svc, db, mapID, links); err != nil {
		return nil, nil, fmt.Errorf("LoadMap: linking Tiles in %q: %w", path, err)
	}
	for _, refused := range spawns.Refused {
		log.Printf("tilemap: %s: %s", path, refused)
	}
	// Warnings are entities that were created and should not have been — a type
	// at validationLevel "warning" missing a required component. Louder than
	// nothing and quieter than a refusal, which is what "warning" asked for.
	for _, warning := range spawns.Warnings {
		log.Printf("tilemap: %s: created anyway: %s", path, warning)
	}

	grid := NewTileGridForMap(file.Width, file.Height, db, *svc.Schema(), mapID)
	if err := grid.RebuildMap(ctx, db, mapID); err != nil {
		return nil, nil, fmt.Errorf("LoadMap: rebuilding grid: %w", err)
	}
	return grid, file.Source, nil
}

// loaded is what a map file yields.
//
// A struct rather than four returns and an error: the third and fourth were
// already a width and a height nobody could tell apart at a call site, and the
// parsed map makes five.
type loaded struct {
	LayerTiles []LayerTile
	Width      int
	Height     int
	// Source is the parsed map; its layers have already been projected into
	// LayerTiles, and it is used here only for spawns and map metadata.
	Source *tiled.Map
}
