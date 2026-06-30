package tilemap

// ReachableTiles returns all passable grid cells reachable from start within
// maxSteps 4-directional steps, excluding start itself.
func ReachableTiles(grid *TileGrid, start Point, maxSteps int) []Point {
	if maxSteps <= 0 {
		return nil
	}

	type frontier struct {
		pt    Point
		steps int
	}

	visited := map[Point]bool{start: true}
	queue := []frontier{{start, 0}}
	var result []Point

	dirs := [4]Point{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		for _, d := range dirs {
			nb := Point{cur.pt.X + d.X, cur.pt.Y + d.Y}
			if visited[nb] || !grid.IsPassable(nb.X, nb.Y) {
				continue
			}
			visited[nb] = true
			result = append(result, nb)
			if cur.steps+1 < maxSteps {
				queue = append(queue, frontier{nb, cur.steps + 1})
			}
		}
	}
	return result
}
