//go:build ebitengine

package renderer

import (
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"log"
	"maps"
	"slices"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/tmbritton/ecs-db/internal/tiled"
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

	// src is the parsed map, and nil when there is none — the character format,
	// or no map at all. Where the *appearance* comes from: comp_tile holds one
	// row per cell by design, so the database can say what a cell is and never
	// what is stacked on it, and a stack is most of what a tileset is for.
	src *tiled.Map
	// images loads a tileset's picture once and keeps it. Shared with the
	// sprite renderer rather than a second cache of the same PNGs.
	images *ImageCache
}

// NewTilemapRenderer builds the static map image.
//
// src may be nil, and is nil for a map this engine's original character format
// describes. Then the tiles are coloured by tile_type from the database, which
// is what the renderer did for every map before tilesets existed.
func NewTilemapRenderer(db *sql.DB, src *tiled.Map, images *ImageCache, w, h, tileSize int) (*TilemapRenderer, error) {
	// Not "make one if there isn't one". There is exactly one image cache in
	// the process, because it is also what hot-reload evicts from — a second
	// one would decode the same PNGs twice and see half the evictions. A caller
	// with nothing to pass has made a mistake worth hearing about.
	if images == nil {
		return nil, fmt.Errorf("renderer: a tilemap renderer needs the process's image cache")
	}
	r := &TilemapRenderer{db: db, src: src, images: images, w: w, h: h, tileSize: tileSize}
	if err := r.rebuild(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *TilemapRenderer) Image() *ebiten.Image { return r.img }

// Invalidate rebuilds the static image.
//
// Nothing calls it. The comment here used to say setTilePassable did, and that
// action writes comp_tile.passable and the grid and never touches the renderer
// — so an opened door has never redrawn, and would not have even if it were
// called, because the old fallback colours by tile_type and that action does
// not write one.
//
// On the drawn path it is dead by construction rather than by oversight: the
// picture comes from the map file, which is read once at startup and never
// changed, so a rebuild is guaranteed to produce the same pixels. Making a
// tile's appearance change at run time means giving the database something the
// renderer reads, which is a story and not a comment.
func (r *TilemapRenderer) Invalidate() {
	if err := r.rebuild(); err != nil {
		log.Printf("TilemapRenderer.Invalidate: %v", err)
	}
}

func (r *TilemapRenderer) rebuild() error {
	if r.src != nil && r.src.Drawable() {
		r.img = r.drawTiles()
		return nil
	}
	return r.drawColours()
}

// drawTiles paints every tile of every visible layer from its tileset.
//
// The arithmetic is all in tiled.DrawList, which is untagged and tested: which
// layer covers which, which tileset owns an id, where a tile taller than its
// cell goes. What is left here is opening files and calling DrawImage, which is
// the part no test without a display can see.
func (r *TilemapRenderer) drawTiles() *ebiten.Image {
	img := ebiten.NewImage(r.w, r.h)
	draws, problems := r.src.DrawList()

	// Each picture once, and each one that will not open named once — not per
	// tile, and not per frame. A map is three hundred cells and this rebuilds
	// on every Invalidate.
	loaded := map[string]*ebiten.Image{}
	missing := map[string]int{}
	for _, d := range draws {
		if _, tried := loaded[d.Image]; tried {
			continue
		}
		if _, gone := missing[d.Image]; gone {
			continue
		}
		if sheet, ok := r.images.Get(d.Image); ok {
			loaded[d.Image] = sheet
		} else {
			missing[d.Image] = 0
		}
	}

	for _, d := range draws {
		sheet, ok := loaded[d.Image]
		if !ok {
			missing[d.Image]++
			continue
		}
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
		img.DrawImage(sheet.SubImage(image.Rect(d.SX, d.SY, d.SX+d.SW, d.SY+d.SH)).(*ebiten.Image), op)
	}

	for _, path := range slices.Sorted(maps.Keys(missing)) {
		log.Printf("tilemap: %s would not load; %d tile(s) not drawn", path, missing[path])
	}
	for _, p := range problems {
		log.Printf("tilemap: %s: %s", r.src.Name, p)
	}
	return img
}

// drawColours is the map before tilesets: a filled rectangle per tile, grey for
// a wall and lighter grey for anything else. Kept as the path a map with no
// tileset takes, which is the character format and every test fixture.
func (r *TilemapRenderer) drawColours() error {
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
		vector.FillRect(img,
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
