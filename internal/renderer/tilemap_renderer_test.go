//go:build ebitengine

package renderer

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

type tileDrawRecorder struct{ sourceRects []image.Rectangle }

func (r *tileDrawRecorder) DrawImage(src *ebiten.Image, _ *ebiten.DrawImageOptions) {
	r.sourceRects = append(r.sourceRects, src.Bounds())
}

func TestTilemapRenderer_DrawsCurrentTileVisualFromDatabase(t *testing.T) {
	db := newTestDB(t)
	for _, stmt := range []string{
		`CREATE TABLE comp_tilelayer (entity_id INTEGER PRIMARY KEY, map_id TEXT, layer_order INTEGER, draw_order INTEGER)`,
		`CREATE TABLE comp_tilevisual (entity_id INTEGER PRIMARY KEY, image TEXT, source_x INTEGER,
			source_y INTEGER, source_w INTEGER, source_h INTEGER, dest_x INTEGER, dest_y INTEGER,
			flip_h INTEGER, flip_v INTEGER, flip_d INTEGER, alpha REAL, visible INTEGER)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	picture := image.NewRGBA(image.Rect(0, 0, 2, 1))
	picture.Set(0, 0, color.RGBA{R: 255, A: 255})
	picture.Set(1, 0, color.RGBA{B: 255, A: 255})
	path := filepath.Join(t.TempDir(), "tiles.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, picture); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	entity, err := db.Exec(`INSERT INTO entities (entity_type, created_tick) VALUES ('Tile', 0)`)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO comp_tilelayer VALUES (?, 'map', 0, 0)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO comp_tilevisual VALUES (?, ?, 0, 0, 1, 1, 0, 0, 0, 0, 0, 1, 1)`, id, path); err != nil {
		t.Fatal(err)
	}
	r, err := NewTilemapRenderer(db, "map", NewImageCache())
	if err != nil {
		t.Fatal(err)
	}
	assertSource := func(want ...image.Rectangle) {
		t.Helper()
		recorder := &tileDrawRecorder{}
		if err := r.drawTo(recorder); err != nil {
			t.Fatal(err)
		}
		if len(recorder.sourceRects) != len(want) {
			t.Fatalf("draw calls = %v, want %v", recorder.sourceRects, want)
		}
		for i, rect := range want {
			if recorder.sourceRects[i] != rect {
				t.Errorf("draw %d source = %v, want %v", i, recorder.sourceRects[i], rect)
			}
		}
	}
	assertSource(image.Rect(0, 0, 1, 1))
	if _, err := db.Exec(`UPDATE comp_tilevisual SET source_x = 1 WHERE entity_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	assertSource(image.Rect(1, 0, 2, 1))
	if _, err := db.Exec(`UPDATE comp_tilevisual SET visible = 0 WHERE entity_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	assertSource()
}
