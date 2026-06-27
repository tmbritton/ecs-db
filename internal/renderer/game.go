//go:build ebitengine

package renderer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"image/color"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

// Game implements ebiten.Game. It runs the interpreter at ~20 Hz by calling
// Ticker.RunTick every (60/TicksPerSecond) frames.
type Game struct {
	db         *sql.DB
	ticker     *Ticker
	frameCount int
	logicalW   int
	logicalH   int
	tileSize   int
	grid       *tilemap.TileGrid
	tr         *TilemapRenderer
	playerID   int64
	playerImg  *ebiten.Image
}

// NewGameParams holds all constructor arguments for Game.
type NewGameParams struct {
	DB       *sql.DB
	Loader   *agent.Loader
	Registry *agent.Registry
	Width    int
	Height   int
	TileSize int
	Grid     *tilemap.TileGrid
	Tilemap  *TilemapRenderer
	PlayerID int64
	Handler  agent.InputHandler
}

func NewGame(p NewGameParams) *Game {
	t := newTicker(p.DB, p.Loader, p.Registry)
	t.SetInputHandler(p.Handler)

	var playerImg *ebiten.Image
	if p.TileSize > 0 {
		playerImg = ebiten.NewImage(p.TileSize, p.TileSize)
		playerImg.Fill(color.RGBA{R: 0, G: 200, B: 255, A: 255})
	}

	return &Game{
		db:        p.DB,
		ticker:    t,
		logicalW:  p.Width,
		logicalH:  p.Height,
		tileSize:  p.TileSize,
		grid:      p.Grid,
		tr:        p.Tilemap,
		playerID:  p.PlayerID,
		playerImg: playerImg,
	}
}

var dirKeys = []struct {
	key  ebiten.Key
	name string
}{
	{ebiten.KeyArrowUp, "ArrowUp"},
	{ebiten.KeyArrowDown, "ArrowDown"},
	{ebiten.KeyArrowLeft, "ArrowLeft"},
	{ebiten.KeyArrowRight, "ArrowRight"},
	{ebiten.KeyW, "W"},
	{ebiten.KeyA, "A"},
	{ebiten.KeyS, "S"},
	{ebiten.KeyD, "D"},
}

func (g *Game) Update() error {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		return ebiten.Termination
	}

	// Sample held directional keys and write to input_events every frame.
	now := time.Now().UnixMilli()
	for _, k := range dirKeys {
		if ebiten.IsKeyPressed(k.key) {
			payload, _ := json.Marshal(map[string]string{"key": k.name})
			_, _ = g.db.Exec(
				`INSERT INTO input_events (received_at_ms, kind, payload) VALUES (?, 'key_held', ?)`,
				now, string(payload),
			)
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
	if g.playerID > 0 && g.playerImg != nil {
		var px, py float64
		if err := g.db.QueryRow(
			"SELECT x, y FROM comp_position WHERE entity_id = ?", g.playerID,
		).Scan(&px, &py); err == nil {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(px*float64(g.tileSize), py*float64(g.tileSize))
			screen.DrawImage(g.playerImg, op)
		}
	}
}

func (g *Game) Layout(outsideW, outsideH int) (int, int) {
	return g.logicalW, g.logicalH
}
