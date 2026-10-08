//go:build ebitengine

package renderer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/game"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

// Game implements ebiten.Game. It runs the interpreter at ~20 Hz by calling
// Ticker.RunTick every (60/TicksPerSecond) frames.
type Game struct {
	db          *sql.DB
	ticker      *Ticker
	frameCount  int
	logicalW    int
	logicalH    int
	tileSize    int
	grid        *tilemap.TileGrid
	tr          *TilemapRenderer
	animLoader  *AnimLoader
	animStates  map[int64]*AnimState
	imageCache  *ImageCache
	fallbackImg *ebiten.Image
}

// NewGameParams holds all constructor arguments for Game.
type NewGameParams struct {
	DB         *sql.DB
	Loader     *agent.Loader
	Registry   *agent.Registry
	Width      int
	Height     int
	TileSize   int
	Grid       *tilemap.TileGrid
	Tilemap    *TilemapRenderer
	Handler    agent.InputHandler
	AnimLoader *AnimLoader
	ImageCache *ImageCache
}

func NewGame(p NewGameParams) *Game {
	t := newTicker(p.DB, p.Loader, p.Registry)
	t.SetInputHandler(p.Handler)
	if p.Grid != nil {
		t.SetMapID(p.Grid.MapID())
	}

	al := p.AnimLoader
	if al == nil {
		al = NewAnimLoader()
	}
	ic := p.ImageCache
	if ic == nil {
		ic = NewImageCache()
	}

	var fallback *ebiten.Image
	if p.TileSize > 0 {
		fallback = ebiten.NewImage(p.TileSize, p.TileSize)
		fallback.Fill(color.RGBA{R: 0, G: 200, B: 255, A: 255})
	}

	return &Game{
		db:          p.DB,
		ticker:      t,
		logicalW:    p.Width,
		logicalH:    p.Height,
		tileSize:    p.TileSize,
		grid:        p.Grid,
		tr:          p.Tilemap,
		animLoader:  al,
		animStates:  make(map[int64]*AnimState),
		imageCache:  ic,
		fallbackImg: fallback,
	}
}

var dirKeys = []struct {
	key  ebiten.Key
	name string
}{
	{ebiten.KeyArrowUp, game.KeyArrowUp},
	{ebiten.KeyArrowDown, game.KeyArrowDown},
	{ebiten.KeyArrowLeft, game.KeyArrowLeft},
	{ebiten.KeyArrowRight, game.KeyArrowRight},
	{ebiten.KeyW, game.KeyW},
	{ebiten.KeyA, game.KeyA},
	{ebiten.KeyS, game.KeyS},
	{ebiten.KeyD, game.KeyD},
}

func (g *Game) Update() error {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		return ebiten.Termination
	}

	// Sample held directional keys and write to input_events in a single transaction per frame.
	now := time.Now().UnixMilli()
	if inputTx, err := g.db.Begin(); err == nil {
		for _, k := range dirKeys {
			if ebiten.IsKeyPressed(k.key) {
				payload, _ := json.Marshal(map[string]string{"key": k.name})
				_, _ = inputTx.Exec(
					`INSERT INTO input_events (received_at_ms, kind, payload) VALUES (?, 'key_held', ?)`,
					now, string(payload),
				)
			}
		}
		_ = inputTx.Commit()
	}

	// Advance per-entity animation frame timers at 60 Hz.
	const dt = 1.0 / 60.0
	for _, st := range g.animStates {
		if def, ok := g.animLoader.Get(st.CurrentAnim); ok {
			st.Advance(def, dt)
		}
	}

	g.frameCount++
	if g.frameCount%(60/TicksPerSecond) == 0 {
		if err := g.ticker.RunTick(); err != nil {
			return fmt.Errorf("tick: %w", err)
		}
	}
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(color.Black)
	if g.tr != nil {
		if err := g.tr.Draw(screen); err != nil {
			log.Printf("tilemap: draw: %v", err)
		}
	}
	g.drawSprites(screen)
}

func (g *Game) drawSprites(dst imageDrawer) {
	hasReferences := g.grid != nil && g.grid.MapID() != "" && g.grid.HasComponent("TileReferences")
	linked := make(map[int64][]tilemap.Point)
	if hasReferences {
		rows, err := g.db.Query(`SELECT CAST(target.value AS INTEGER), p.x, p.y
			FROM comp_tilelayer layer JOIN comp_position p ON p.entity_id=layer.entity_id
			JOIN comp_tilereferences refs ON refs.entity_id=layer.entity_id
			JOIN json_each(refs.value) target WHERE layer.map_id=?
			ORDER BY layer.layer_order, layer.draw_order, layer.entity_id, CAST(target.key AS INTEGER)`, g.grid.MapID())
		if err != nil {
			log.Printf("reading linked sprite positions: %v", err)
			return
		}
		for rows.Next() {
			var id int64
			var x, y float64
			if err := rows.Scan(&id, &x, &y); err != nil {
				log.Printf("reading linked sprite positions: %v", err)
				_ = rows.Close()
				return
			}
			linked[id] = append(linked[id], tilemap.Point{X: int(x), Y: int(y)})
		}
		if err := rows.Err(); err != nil {
			log.Printf("reading linked sprite positions: %v", err)
			_ = rows.Close()
			return
		}
		_ = rows.Close()
	}
	query := `SELECT e.id, cp.x, cp.y, cs.sheet, cs.animation, cs.flip_x, NULL AS occupied
		FROM entities e JOIN comp_position cp ON e.id = cp.entity_id
		JOIN comp_sprite cs ON e.id = cs.entity_id`
	if hasReferences {
		query = `SELECT e.id, cp.x, cp.y, cs.sheet, cs.animation, cs.flip_x, NULL AS occupied
			FROM entities e LEFT JOIN comp_position cp ON e.id = cp.entity_id
			JOIN comp_sprite cs ON e.id = cs.entity_id`
	}
	if g.grid != nil && g.grid.HasComponent("OccupiedCells") {
		positionJoin := "JOIN"
		if hasReferences {
			positionJoin = "LEFT JOIN"
		}
		query = `SELECT e.id, cp.x, cp.y, cs.sheet, cs.animation, cs.flip_x, occupied.value
			FROM entities e ` + positionJoin + ` comp_position cp ON e.id = cp.entity_id
			JOIN comp_sprite cs ON e.id = cs.entity_id
			LEFT JOIN comp_occupiedcells occupied ON occupied.entity_id = e.id`
	}
	var args []any
	if g.grid != nil && g.grid.MapID() != "" {
		query += ` WHERE (EXISTS (SELECT 1 FROM comp_tilelayer tile WHERE tile.entity_id=e.id AND tile.map_id=?)
			OR EXISTS (SELECT 1 FROM comp_tileentityowner owner
				JOIN comp_tilelayer parent ON parent.entity_id=owner.target_entity_id
				WHERE owner.entity_id=e.id AND parent.map_id=?)`
		if hasReferences {
			query += ` OR EXISTS (SELECT 1 FROM comp_tilelayer parent
				JOIN comp_tilereferences refs ON refs.entity_id=parent.entity_id
				JOIN json_each(refs.value) target WHERE CAST(target.value AS INTEGER)=e.id AND parent.map_id=?
				AND (NOT EXISTS (SELECT 1 FROM spawns foreign_spawn WHERE foreign_spawn.entity_id=e.id)
				OR EXISTS (SELECT 1 FROM spawns local_spawn WHERE local_spawn.entity_id=e.id AND local_spawn.map=?))
				AND NOT EXISTS (SELECT 1 FROM comp_tileentityowner foreign_owner
				JOIN comp_tilelayer foreign_tile ON foreign_tile.entity_id=foreign_owner.target_entity_id
				WHERE foreign_owner.entity_id=e.id AND foreign_tile.map_id<>?))`
		}
		query += ` OR (NOT EXISTS (SELECT 1 FROM comp_tilelayer any_tile WHERE any_tile.entity_id=e.id)
				AND NOT EXISTS (SELECT 1 FROM comp_tileentityowner any_owner WHERE any_owner.entity_id=e.id)`
		if hasReferences {
			query += ` AND NOT EXISTS (SELECT 1 FROM comp_tilereferences any_refs
				JOIN comp_tilelayer linked_tile ON linked_tile.entity_id=any_refs.entity_id
				JOIN json_each(any_refs.value) any_target
				WHERE CAST(any_target.value AS INTEGER)=e.id AND linked_tile.map_id=?)
				AND (NOT EXISTS (SELECT 1 FROM comp_tilereferences any_refs
					JOIN json_each(any_refs.value) any_target WHERE CAST(any_target.value AS INTEGER)=e.id)
					OR EXISTS (SELECT 1 FROM spawns local_spawn WHERE local_spawn.entity_id=e.id AND local_spawn.map=?))`
		}
		query += ` AND (NOT EXISTS (SELECT 1 FROM spawns other WHERE other.entity_id=e.id)
				OR EXISTS (SELECT 1 FROM spawns current WHERE current.entity_id=e.id AND current.map=?))))`
		args = append(args, g.grid.MapID(), g.grid.MapID())
		if hasReferences {
			args = append(args, g.grid.MapID(), g.grid.MapID(), g.grid.MapID())
			args = append(args, g.grid.MapID(), g.grid.MapID())
		}
		args = append(args, g.grid.MapID())
	}
	query += " ORDER BY e.id"
	rows, err := g.db.Query(query, args...)
	if err != nil {
		return
	}
	defer rows.Close()

	ts := float64(g.tileSize)
	seen := make(map[int64]bool)

	for rows.Next() {
		var (
			id        int64
			px, py    sql.NullFloat64
			sheet     string
			animation string
			flipX     int
			occupied  sql.NullString
		)
		if err := rows.Scan(&id, &px, &py, &sheet, &animation, &flipX, &occupied); err != nil {
			continue
		}
		cells := []tilemap.Point{{}}
		if hasReferences && len(linked[id]) != 0 {
			cells = linked[id]
		} else if !px.Valid || !py.Valid {
			continue
		} else if occupied.Valid {
			cells, err = tilemap.ParseOccupiedCells(occupied.String,
				tilemap.Point{X: int(math.Floor(px.Float64)), Y: int(math.Floor(py.Float64))}, g.grid.Width, g.grid.Height)
			if err != nil {
				log.Printf("sprite %d: %v", id, err)
				continue
			}
		}
		seen[id] = true

		// Sync AnimState on first sight or animation change.
		st, exists := g.animStates[id]
		if !exists {
			st = &AnimState{CurrentAnim: animation}
			g.animStates[id] = st
		} else if animation != st.CurrentAnim {
			st.CurrentAnim = animation
			st.Frame = 0
			st.Elapsed = 0
		}

		worldX, worldY := px.Float64*ts, py.Float64*ts
		if hasReferences && len(linked[id]) != 0 {
			worldX, worldY = 0, 0 // linked cells are absolute map positions
		}

		def, hasDef := g.animLoader.Get(animation)
		sheetImg, hasImg := g.imageCache.Get(sheet)

		if !hasDef || !hasImg {
			// Fallback: solid colour rectangle.
			if g.fallbackImg != nil {
				drawSpriteFootprint(dst, g.fallbackImg, worldX, worldY, cells, false)
			}
			continue
		}

		frameIdx := st.Frame
		if frameIdx >= len(def.Frames) {
			frameIdx = len(def.Frames) - 1
		}
		col := def.Frames[frameIdx]
		subRect := image.Rect(col*g.tileSize, 0, (col+1)*g.tileSize, g.tileSize)
		subImg := sheetImg.SubImage(subRect).(*ebiten.Image)

		drawSpriteFootprint(dst, subImg, worldX, worldY, cells, flipX != 0)
	}

	// GC animStates for deleted entities.
	for id := range g.animStates {
		if !seen[id] {
			delete(g.animStates, id)
		}
	}
}

func drawSpriteFootprint(dst imageDrawer, frame *ebiten.Image, worldX, worldY float64, cells []tilemap.Point, flipX bool) {
	size := float64(frame.Bounds().Dx())
	for _, cell := range cells {
		var op ebiten.DrawImageOptions
		if flipX {
			op.GeoM.Scale(-1, 1)
			op.GeoM.Translate(size, 0)
		}
		op.GeoM.Translate(worldX+float64(cell.X)*size, worldY+float64(cell.Y)*size)
		dst.DrawImage(frame, &op)
	}
}

func (g *Game) Layout(outsideW, outsideH int) (int, int) {
	return g.logicalW, g.logicalH
}
