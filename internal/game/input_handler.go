package game

import (
	"encoding/json"
	"fmt"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

// PlayerInputHandler processes drained input_events rows and moves the player entity.
type PlayerInputHandler struct {
	playerID int64
	grid     *tilemap.TileGrid
}

// NewPlayerInputHandler returns a handler bound to the given player entity and tile grid.
// Panics if grid is nil.
func NewPlayerInputHandler(playerID int64, grid *tilemap.TileGrid) *PlayerInputHandler {
	if grid == nil {
		panic("game: NewPlayerInputHandler: grid must not be nil")
	}
	return &PlayerInputHandler{playerID: playerID, grid: grid}
}

type direction struct{ dx, dy int }

// Handle implements agent.InputHandler.
// It collapses all key_held events into a single direction decision (last wins),
// checks passability, then updates comp_position and comp_sprite within the tick transaction.
func (h *PlayerInputHandler) Handle(events []agent.InputEvent, world agent.WorldWriter, reader agent.WorldReader) error {
	var dir *direction
	flipLeft := false

	for _, e := range events {
		if e.Kind != "key_held" {
			continue
		}
		var payload struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal([]byte(e.Payload), &payload); err != nil {
			continue
		}
		switch payload.Key {
		case "ArrowRight", "D":
			dir = &direction{1, 0}
			flipLeft = false
		case "ArrowLeft", "A":
			dir = &direction{-1, 0}
			flipLeft = true
		case "ArrowDown", "S":
			dir = &direction{0, 1}
		case "ArrowUp", "W":
			dir = &direction{0, -1}
		}
	}

	moved := false
	if dir != nil {
		xVal, err := reader.GetComponentValue(h.playerID, "Position", "x")
		if err != nil {
			return fmt.Errorf("player input: read x: %w", err)
		}
		yVal, err := reader.GetComponentValue(h.playerID, "Position", "y")
		if err != nil {
			return fmt.Errorf("player input: read y: %w", err)
		}

		cx := int(toFloat(xVal))
		cy := int(toFloat(yVal))
		tx := cx + dir.dx
		ty := cy + dir.dy

		if h.grid.IsPassable(tx, ty) {
			if err := world.SetComponentValue(h.playerID, "Position", "x", float64(tx)); err != nil {
				return fmt.Errorf("player input: set x: %w", err)
			}
			if err := world.SetComponentValue(h.playerID, "Position", "y", float64(ty)); err != nil {
				return fmt.Errorf("player input: set y: %w", err)
			}
			moved = true
		}
	}

	anim := "player_idle"
	if moved {
		anim = "player_walk"
	}
	if err := world.SetComponentValue(h.playerID, "Sprite", "animation", anim); err != nil {
		return fmt.Errorf("player input: set animation: %w", err)
	}
	flip := btoi(moved && flipLeft)
	if err := world.SetComponentValue(h.playerID, "Sprite", "flip_x", flip); err != nil {
		return fmt.Errorf("player input: set flip_x: %w", err)
	}

	return nil
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
