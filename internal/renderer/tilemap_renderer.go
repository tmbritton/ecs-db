//go:build ebitengine

package renderer

import (
	"context"
	"database/sql"
	"fmt"
	"image"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

// TilemapRenderer draws the current TileVisual components in their database
// layer order. Only decoded PNGs are cached; no map-file snapshot is kept.
type TilemapRenderer struct {
	db      *sql.DB
	mapID   string
	images  *ImageCache
	missing map[string]bool
}

func NewTilemapRenderer(db *sql.DB, mapID string, images *ImageCache) (*TilemapRenderer, error) {
	if db == nil || mapID == "" || images == nil {
		return nil, fmt.Errorf("renderer: tilemap requires a database, map identity and image cache")
	}
	return &TilemapRenderer{db: db, mapID: mapID, images: images, missing: make(map[string]bool)}, nil
}

// Draw queries entity components each frame, so edits and deletions made by the
// game are visible on the next draw without re-reading TMX or rebuilding art.
func (r *TilemapRenderer) Draw(screen *ebiten.Image) error {
	return r.drawTo(screen)
}

type imageDrawer interface {
	DrawImage(*ebiten.Image, *ebiten.DrawImageOptions)
}

func (r *TilemapRenderer) drawTo(screen imageDrawer) error {
	draws, err := tilemap.ReadTileDraws(context.Background(), r.db, r.mapID)
	if err != nil {
		return err
	}
	for _, d := range draws {
		sheet, ok := r.images.Get(d.Image)
		if !ok {
			if !r.missing[d.Image] {
				log.Printf("tilemap: image %q would not load; tile not drawn", d.Image)
				r.missing[d.Image] = true
			}
			continue
		}
		delete(r.missing, d.Image)
		m := d.Transform()
		op := &ebiten.DrawImageOptions{}
		if d.Alpha < 1 {
			op.ColorScale.ScaleAlpha(float32(d.Alpha))
		}
		op.GeoM.SetElement(0, 0, m.A)
		op.GeoM.SetElement(0, 1, m.B)
		op.GeoM.SetElement(0, 2, m.TX)
		op.GeoM.SetElement(1, 0, m.C)
		op.GeoM.SetElement(1, 1, m.D)
		op.GeoM.SetElement(1, 2, m.TY)
		screen.DrawImage(sheet.SubImage(image.Rect(d.SX, d.SY, d.SX+d.SW, d.SY+d.SH)).(*ebiten.Image), op)
	}
	return nil
}
