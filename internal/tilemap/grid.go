package tilemap

import (
	"context"
	"database/sql"
	"fmt"
)

type TileGrid struct {
	Width, Height int
	passable      [][]bool
}

func NewTileGrid(width, height int) *TileGrid {
	p := make([][]bool, height)
	for i := range p {
		p[i] = make([]bool, width)
	}
	return &TileGrid{Width: width, Height: height, passable: p}
}

func (g *TileGrid) IsPassable(x, y int) bool {
	if x < 0 || y < 0 || x >= g.Width || y >= g.Height {
		return false
	}
	return g.passable[y][x]
}

func (g *TileGrid) SetPassable(x, y int, val bool) {
	if x < 0 || y < 0 || x >= g.Width || y >= g.Height {
		return
	}
	g.passable[y][x] = val
}

func (g *TileGrid) Rebuild(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx,
		`SELECT comp_tile.x, comp_tile.y, comp_tile.passable
		 FROM entities
		 JOIN comp_tile ON entities.id = comp_tile.entity_id
		 WHERE entities.entity_type = 'Tile'`)
	if err != nil {
		return fmt.Errorf("TileGrid.Rebuild: %w", err)
	}
	defer rows.Close()

	for row := range g.passable {
		for col := range g.passable[row] {
			g.passable[row][col] = false
		}
	}
	for rows.Next() {
		var x, y, p int
		if err := rows.Scan(&x, &y, &p); err != nil {
			return fmt.Errorf("TileGrid.Rebuild scan: %w", err)
		}
		g.SetPassable(x, y, p != 0)
	}
	return rows.Err()
}
