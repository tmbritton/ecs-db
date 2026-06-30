package tilemap_test

import (
	"testing"

	"github.com/tmbritton/ecs-db/internal/tilemap"
)

func TestLineOfSight_ClearLine(t *testing.T) {
	grid := openGrid(5, 1)
	if !tilemap.LineOfSight(grid, tilemap.Point{X: 0}, tilemap.Point{X: 4}) {
		t.Error("expected true for clear 5-cell corridor")
	}
}

func TestLineOfSight_WallInMiddle(t *testing.T) {
	grid := openGrid(5, 1)
	grid.SetPassable(2, 0, false)
	if tilemap.LineOfSight(grid, tilemap.Point{X: 0}, tilemap.Point{X: 4}) {
		t.Error("expected false when wall at (2,0) blocks ray")
	}
}

func TestLineOfSight_SameCell(t *testing.T) {
	grid := openGrid(3, 3)
	if !tilemap.LineOfSight(grid, tilemap.Point{X: 1, Y: 1}, tilemap.Point{X: 1, Y: 1}) {
		t.Error("expected true for same cell")
	}
}

func TestLineOfSight_ImpassableEnd(t *testing.T) {
	grid := openGrid(5, 1)
	grid.SetPassable(4, 0, false)
	if tilemap.LineOfSight(grid, tilemap.Point{X: 0}, tilemap.Point{X: 4}) {
		t.Error("expected false when end cell is impassable")
	}
}

func TestLineOfSight_DiagonalBlocked(t *testing.T) {
	// 3×3 open grid; wall at (1,1); diagonal ray from (0,0) to (2,2).
	grid := openGrid(3, 3)
	grid.SetPassable(1, 1, false)
	if tilemap.LineOfSight(grid, tilemap.Point{X: 0, Y: 0}, tilemap.Point{X: 2, Y: 2}) {
		t.Error("expected false when diagonal blocked by wall at (1,1)")
	}
}
