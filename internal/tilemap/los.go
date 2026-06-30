package tilemap

// LineOfSight returns true when the straight line from start to end contains no
// impassable cells. Uses a Bresenham walk: start is excluded from the
// passability check; end is included.
func LineOfSight(grid *TileGrid, start, end Point) bool {
	if start == end {
		return true
	}
	x, y := start.X, start.Y
	dx, dy := iabs(end.X-x), iabs(end.Y-y)
	sx, sy := isign(end.X-x), isign(end.Y-y)
	err := dx - dy
	for {
		if x == end.X && y == end.Y {
			return grid.IsPassable(x, y)
		}
		if (x != start.X || y != start.Y) && !grid.IsPassable(x, y) {
			return false
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x += sx
		}
		if e2 < dx {
			err += dx
			y += sy
		}
	}
}

func iabs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func isign(n int) int {
	if n > 0 {
		return 1
	}
	if n < 0 {
		return -1
	}
	return 0
}
