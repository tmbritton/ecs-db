package tilemap

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/tmbritton/ecs-db/internal/world"
)

type mapDef struct {
	Width  int      `toml:"width"`
	Height int      `toml:"height"`
	Rows   []string `toml:"rows"`
}

// LoadMap bootstraps Tile entities from a TOML map file and returns a
// populated TileGrid. Idempotent: skips entity creation if Tile entities
// already exist in the DB.
func LoadMap(ctx context.Context, svc *world.EntityService, db *sql.DB, path string, tileSize int) (*TileGrid, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("LoadMap: reading %q: %w", path, err)
	}
	var def mapDef
	if err := toml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("LoadMap: parsing %q: %w", path, err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&count); err != nil {
		return nil, fmt.Errorf("LoadMap: checking existing tiles: %w", err)
	}

	if count == 0 {
		for y, row := range def.Rows {
			for x, ch := range row {
				var passable bool
				var tileType string
				switch ch {
				case '.':
					passable, tileType = true, "floor"
				case '#':
					passable, tileType = false, "wall"
				default:
					continue
				}
				if _, err := svc.CreateEntity(ctx, "Tile", []world.EntityComponent{
					{Name: "Tile", Values: map[string]interface{}{
						"x": x, "y": y, "passable": passable, "tile_type": tileType,
					}},
				}); err != nil {
					return nil, fmt.Errorf("LoadMap: creating tile (%d,%d): %w", x, y, err)
				}
			}
		}
	}

	grid := NewTileGrid(def.Width, def.Height)
	if err := grid.Rebuild(ctx, db); err != nil {
		return nil, fmt.Errorf("LoadMap: rebuilding grid: %w", err)
	}
	return grid, nil
}
