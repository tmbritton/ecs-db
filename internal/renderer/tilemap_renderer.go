//go:build ebitengine

package renderer

import (
	"context"
	"database/sql"
	"image/color"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// TilemapRenderer builds a static *ebiten.Image from the tile entities in the
// DB. The image is rebuilt on construction and again whenever Invalidate is
// called. This avoids re-querying the DB every frame for tiles that almost
// never change.
type TilemapRenderer struct {
	db       *sql.DB
	img      *ebiten.Image
	w, h     int
	tileSize int
}

func NewTilemapRenderer(db *sql.DB, w, h, tileSize int) (*TilemapRenderer, error) {
	r := &TilemapRenderer{db: db, w: w, h: h, tileSize: tileSize}
	if err := r.rebuild(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *TilemapRenderer) Image() *ebiten.Image { return r.img }

// Invalidate rebuilds the static image. Called by the setTilePassable action.
func (r *TilemapRenderer) Invalidate() {
	if err := r.rebuild(); err != nil {
		log.Printf("TilemapRenderer.Invalidate: %v", err)
	}
}

func (r *TilemapRenderer) rebuild() error {
	img := ebiten.NewImage(r.w, r.h)
	rows, err := r.db.QueryContext(context.Background(),
		`SELECT comp_tile.x, comp_tile.y, comp_tile.tile_type
		 FROM entities
		 JOIN comp_tile ON entities.id = comp_tile.entity_id
		 WHERE entities.entity_type = 'Tile'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var x, y int
		var tileType string
		if err := rows.Scan(&x, &y, &tileType); err != nil {
			return err
		}
		var c color.RGBA
		switch tileType {
		case "wall":
			c = color.RGBA{R: 60, G: 60, B: 60, A: 255}
		default: // floor
			c = color.RGBA{R: 180, G: 180, B: 180, A: 255}
		}
		vector.DrawFilledRect(img,
			float32(x*r.tileSize), float32(y*r.tileSize),
			float32(r.tileSize), float32(r.tileSize),
			c, false)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	r.img = img
	return nil
}
