package tilemap

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/tmbritton/ecs-db/internal/schema"
)

type TileGrid struct {
	Width, Height int
	entityIDs     map[Point]int64
	db            *sql.DB
	rules         schema.DatabaseSchema
	mapID         string
}

// SpaceFromSnapshot uses the current tick rather than a second database
// connection, which cannot see an uncommitted wall move.
func (g *TileGrid) SpaceFromSnapshot(reader SpaceQuerier) (Space, error) {
	return ReadSpace(context.Background(), reader, g.rules, g.Width, g.Height, g.mapID)
}

// NewTileGridForWorld binds the map bounds to the current database and its
// schema-declared interaction policy.
func NewTileGridForWorld(width, height int, db *sql.DB, rules schema.DatabaseSchema) *TileGrid {
	g := NewTileGrid(width, height)
	g.db, g.rules = db, rules
	return g
}

// NewTileGridForMap additionally scopes authored occupants and actor lookups
// to this map. Runtime entities with no spawn owner remain in the snapshot.
func NewTileGridForMap(width, height int, db *sql.DB, rules schema.DatabaseSchema, mapID string) *TileGrid {
	g := NewTileGridForWorld(width, height, db, rules)
	g.mapID = mapID
	return g
}

// SpaceFrom reads the current world, using a tick transaction when provided
// so movement sees walls placed earlier in the same tick. With nil source it
// reads the committed database (outside an active tick transaction).
func (g *TileGrid) SpaceFrom(ctx context.Context, source SpaceQuerier) (Space, error) {
	if source == nil {
		source = g.db
	}
	if source == nil {
		return Space{}, fmt.Errorf("TileGrid has no world database")
	}
	return ReadSpace(ctx, source, g.rules, g.Width, g.Height, g.mapID)
}

// HasComponent answers whether this map's schema declares a component table.
// The renderer uses it to retain anchor-only sprites for projects that do not
// declare multi-cell occupancy.
func (g *TileGrid) HasComponent(name string) bool {
	_, declared := g.rules.Components[name]
	return declared
}

// MapID is the identity used to scope authored occupants and actor lookups.
func (g *TileGrid) MapID() string { return g.mapID }

func NewTileGrid(width, height int) *TileGrid {
	return &TileGrid{Width: width, Height: height, entityIDs: make(map[Point]int64)}
}

// EntityAt returns the entity ID for the tile at (x, y), and whether it exists.
func (g *TileGrid) EntityAt(x, y int) (int64, bool) {
	id, ok := g.entityIDs[Point{X: x, Y: y}]
	return id, ok
}

// SetEntityID records the entity ID for the tile at (x, y).
func (g *TileGrid) SetEntityID(x, y int, id int64) {
	g.entityIDs[Point{X: x, Y: y}] = id
}

// RebuildMap indexes the active map's art Tile IDs. This index never decides
// traversal or sight; those query the current occupants through SpaceFrom.
func (g *TileGrid) RebuildMap(ctx context.Context, db *sql.DB, mapID string) error {
	rows, err := db.QueryContext(ctx, `SELECT e.id, t.x, t.y FROM entities e
			 JOIN comp_tile t ON e.id = t.entity_id
			 JOIN comp_tilelayer l ON l.entity_id = e.id
			 WHERE e.entity_type = 'Tile' AND l.map_id = ?
			 ORDER BY l.layer_order, l.draw_order, e.id`, mapID)
	return g.rebuildRows(rows, err)
}

func (g *TileGrid) rebuildRows(rows *sql.Rows, err error) error {
	if err != nil {
		return fmt.Errorf("TileGrid.Rebuild: %w", err)
	}
	defer rows.Close()

	for k := range g.entityIDs {
		delete(g.entityIDs, k)
	}
	for rows.Next() {
		var id int64
		var x, y int
		if err := rows.Scan(&id, &x, &y); err != nil {
			return fmt.Errorf("TileGrid.Rebuild scan: %w", err)
		}
		g.entityIDs[Point{X: x, Y: y}] = id
	}
	return rows.Err()
}
