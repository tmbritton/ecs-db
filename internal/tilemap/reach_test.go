package tilemap_test

import (
	"slices"
	"testing"

	"github.com/tmbritton/ecs-db/internal/tilemap"
)

func sortPoints(pts []tilemap.Point) []tilemap.Point {
	out := slices.Clone(pts)
	slices.SortFunc(out, func(a, b tilemap.Point) int {
		if a.Y != b.Y {
			return a.Y - b.Y
		}
		return a.X - b.X
	})
	return out
}

func TestReachableTiles_ZeroSteps(t *testing.T) {
	grid := openGrid(5, 5)
	got := tilemap.ReachableTiles(grid, tilemap.Point{2, 2}, 0)
	if len(got) != 0 {
		t.Errorf("maxSteps=0: got %v, want empty", got)
	}
}

func TestReachableTiles_OneStep(t *testing.T) {
	grid := openGrid(5, 5)
	got := sortPoints(tilemap.ReachableTiles(grid, tilemap.Point{2, 2}, 1))
	want := sortPoints([]tilemap.Point{{2, 1}, {2, 3}, {1, 2}, {3, 2}})
	if !slices.Equal(got, want) {
		t.Errorf("1 step from (2,2): got %v, want %v", got, want)
	}
}

func TestReachableTiles_ExcludesStart(t *testing.T) {
	grid := openGrid(5, 5)
	start := tilemap.Point{2, 2}
	for _, pt := range tilemap.ReachableTiles(grid, start, 3) {
		if pt == start {
			t.Errorf("result includes start point %v", start)
		}
	}
}

func TestReachableTiles_ExcludesImpassable(t *testing.T) {
	grid := openGrid(5, 5)
	wall := tilemap.Point{3, 2}
	grid.SetPassable(wall.X, wall.Y, false)

	for _, pt := range tilemap.ReachableTiles(grid, tilemap.Point{2, 2}, 3) {
		if pt == wall {
			t.Errorf("result includes impassable tile %v", wall)
		}
	}
}

func TestReachableTiles_WallLimitsReach(t *testing.T) {
	// 5×1 corridor, wall at x=2. Start=(0,0) maxSteps=5.
	// Only (1,0) is reachable; (2,0) is the wall, (3,0) and (4,0) are blocked.
	grid := tilemap.NewTileGrid(5, 1)
	grid.SetPassable(0, 0, true)
	grid.SetPassable(1, 0, true)
	// x=2 remains impassable

	got := tilemap.ReachableTiles(grid, tilemap.Point{0, 0}, 5)
	if len(got) != 1 || got[0] != (tilemap.Point{1, 0}) {
		t.Errorf("got %v, want [{1 0}]", got)
	}
}

func TestReachableTiles_CountOnOpenGrid(t *testing.T) {
	// On a large open grid, steps=2 from centre should reach the diamond of
	// Manhattan distance 1 and 2 (4 + 8 = 12 cells), none out of bounds.
	grid := openGrid(9, 9)
	got := tilemap.ReachableTiles(grid, tilemap.Point{4, 4}, 2)
	if len(got) != 12 {
		t.Errorf("2 steps on open grid: got %d tiles, want 12", len(got))
	}
}
