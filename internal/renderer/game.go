//go:build ebitengine

package renderer

import (
	"database/sql"
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/tmbritton/ecs-db/internal/agent"
)

// Game implements ebiten.Game. It runs the interpreter at ~20 Hz by calling
// Ticker.RunTick every (60/TicksPerSecond) frames.
type Game struct {
	ticker     *Ticker
	frameCount int
	logicalW   int
	logicalH   int
}

func NewGame(db *sql.DB, loader *agent.Loader, registry *agent.Registry, w, h int) *Game {
	return &Game{
		ticker:   newTicker(db, loader, registry),
		logicalW: w,
		logicalH: h,
	}
}

func (g *Game) Update() error {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		return ebiten.Termination
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
}

func (g *Game) Layout(outsideW, outsideH int) (int, int) {
	return g.logicalW, g.logicalH
}
