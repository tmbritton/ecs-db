package tilemap_test

import (
	"testing"

	"github.com/tmbritton/ecs-db/internal/tilemap"
)

func openGrid(w, h int) *tilemap.TileGrid {
	g := tilemap.NewTileGrid(w, h)
	for y := range h {
		for x := range w {
			g.SetPassable(x, y, true)
		}
	}
	return g
}

func TestAStar_StraightPath(t *testing.T) {
	grid := openGrid(5, 1)
	path := tilemap.AStar(grid, tilemap.Point{0, 0}, tilemap.Point{4, 0})
	if len(path) != 4 {
		t.Fatalf("path len = %d, want 4; path = %v", len(path), path)
	}
	if path[len(path)-1] != (tilemap.Point{4, 0}) {
		t.Errorf("last waypoint = %v, want {4 0}", path[len(path)-1])
	}
}

func TestAStar_AroundWall(t *testing.T) {
	// 3×3 grid: centre cell (1,1) is impassable.
	// start=(0,1) goal=(2,1) — must route via row 0 or row 2.
	grid := openGrid(3, 3)
	grid.SetPassable(1, 1, false)

	path := tilemap.AStar(grid, tilemap.Point{0, 1}, tilemap.Point{2, 1})
	if path == nil {
		t.Fatal("expected a path around the wall, got nil")
	}
	last := path[len(path)-1]
	if last != (tilemap.Point{2, 1}) {
		t.Errorf("last waypoint = %v, want {2 1}", last)
	}
}

func TestAStar_NoPath(t *testing.T) {
	// Start is surrounded by walls on all sides.
	grid := tilemap.NewTileGrid(3, 3)
	grid.SetPassable(1, 1, true) // only start is passable
	grid.SetPassable(2, 2, true) // goal is passable but unreachable

	path := tilemap.AStar(grid, tilemap.Point{1, 1}, tilemap.Point{2, 2})
	if path != nil {
		t.Errorf("expected nil path for enclosed start, got %v", path)
	}
}

func TestAStar_SameCell(t *testing.T) {
	grid := openGrid(3, 3)
	path := tilemap.AStar(grid, tilemap.Point{1, 1}, tilemap.Point{1, 1})
	if path != nil {
		t.Errorf("expected nil for start == goal, got %v", path)
	}
}

func TestAStar_ImpassableGoal(t *testing.T) {
	grid := openGrid(3, 3)
	grid.SetPassable(2, 2, false)
	path := tilemap.AStar(grid, tilemap.Point{0, 0}, tilemap.Point{2, 2})
	if path != nil {
		t.Errorf("expected nil for impassable goal, got %v", path)
	}
}
