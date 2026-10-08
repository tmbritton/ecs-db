package tilemap

// ReachableFor evaluates the same mover/occupant policy as AStarFor, excluding
// the start cell and staying within maxSteps four-directional moves.
func ReachableFor(space Space, mover SpatialEntity, start Point, maxSteps int) ([]Point, error) {
	return reachableWithAllowed(start, maxSteps, func(at Point) (bool, error) {
		return space.CanEnter(mover, at)
	})
}

func reachableWithAllowed(start Point, maxSteps int, allowed func(Point) (bool, error)) ([]Point, error) {
	if maxSteps <= 0 {
		return nil, nil
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
			if visited[nb] {
				continue
			}
			pass, err := allowed(nb)
			if err != nil {
				return nil, err
			}
			if !pass {
				continue
			}
			visited[nb] = true
			result = append(result, nb)
			if cur.steps+1 < maxSteps {
				queue = append(queue, frontier{nb, cur.steps + 1})
			}
		}
	}
	return result, nil
}
