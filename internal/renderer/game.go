//go:build ebitengine

package renderer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"os"
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
	imgCache    map[string]*ebiten.Image
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
}

func NewGame(p NewGameParams) *Game {
	t := newTicker(p.DB, p.Loader, p.Registry)
	t.SetInputHandler(p.Handler)

	al := p.AnimLoader
	if al == nil {
		al = NewAnimLoader()
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
		imgCache:    make(map[string]*ebiten.Image),
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
		screen.DrawImage(g.tr.Image(), nil)
	}

	rows, err := g.db.Query(`
		SELECT e.id, cp.x, cp.y, cs.sheet, cs.animation, cs.flip_x
		FROM entities e
		JOIN comp_position cp ON e.id = cp.entity_id
		JOIN comp_sprite   cs ON e.id = cs.entity_id`)
	if err != nil {
		return
	}
	defer rows.Close()

	ts := float64(g.tileSize)
	seen := make(map[int64]bool)

	for rows.Next() {
		var (
			id        int64
			px, py    float64
			sheet     string
			animation string
			flipX     int
		)
		if err := rows.Scan(&id, &px, &py, &sheet, &animation, &flipX); err != nil {
			continue
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

		worldX := px * ts
		worldY := py * ts

		def, hasDef := g.animLoader.Get(animation)
		sheetImg, hasImg := g.loadImage(sheet)

		if !hasDef || !hasImg {
			// Fallback: solid colour rectangle.
			if g.fallbackImg != nil {
				var op ebiten.DrawImageOptions
				op.GeoM.Translate(worldX, worldY)
				screen.DrawImage(g.fallbackImg, &op)
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

		var op ebiten.DrawImageOptions
		if flipX != 0 {
			op.GeoM.Scale(-1, 1)
			op.GeoM.Translate(ts, 0)
		}
		op.GeoM.Translate(worldX, worldY)
		screen.DrawImage(subImg, &op)
	}

	// GC animStates for deleted entities.
	for id := range g.animStates {
		if !seen[id] {
			delete(g.animStates, id)
		}
	}
}

func (g *Game) Layout(outsideW, outsideH int) (int, int) {
	return g.logicalW, g.logicalH
}

// loadImage returns a cached *ebiten.Image for path, loading and caching on first use.
// Returns nil, false if sheet is empty or the file cannot be loaded.
func (g *Game) loadImage(path string) (*ebiten.Image, bool) {
	if path == "" {
		return nil, false
	}
	if img, ok := g.imgCache[path]; ok {
		return img, true
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, false
	}
	img := ebiten.NewImageFromImage(src)
	g.imgCache[path] = img
	return img, true
}
