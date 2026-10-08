package tilemap

import "container/heap"

// Point is a tile-grid coordinate.
type Point struct{ X, Y int }

// AStarFor uses the same occupant decision as direct movement, with this
// mover's actual schema-declared capabilities and destination footprint.
func AStarFor(space Space, mover SpatialEntity, start, goal Point) ([]Point, error) {
	return astarWithAllowed(start, goal, func(at Point) (bool, error) {
		return space.CanEnter(mover, at)
	})
}

func astarWithAllowed(start, goal Point, allowed func(Point) (bool, error)) ([]Point, error) {
	if start == goal {
		return nil, nil
	}
	canGoal, err := allowed(goal)
	if err != nil {
		return nil, err
	}
	if !canGoal {
		return nil, nil
	}

	open := &pQueue{{pt: start, f: manhattan(start, goal)}}
	heap.Init(open)

	gScore := map[Point]int{start: 0}
	parent := map[Point]Point{}
	closed := map[Point]bool{}

	dirs := [4]Point{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

	for open.Len() > 0 {
		cur := heap.Pop(open).(pqItem).pt

		if cur == goal {
			return reconstructPath(parent, start, goal), nil
		}
		if closed[cur] {
			continue
		}
		closed[cur] = true

		for _, d := range dirs {
			nb := Point{cur.X + d.X, cur.Y + d.Y}
			if closed[nb] {
				continue
			}
			pass, err := allowed(nb)
			if err != nil {
				return nil, err
			}
			if !pass {
				continue
			}
			tentative := gScore[cur] + 1
			if prev, ok := gScore[nb]; !ok || tentative < prev {
				gScore[nb] = tentative
				parent[nb] = cur
				heap.Push(open, pqItem{pt: nb, f: tentative + manhattan(nb, goal)})
			}
		}
	}
	return nil, nil
}

func reconstructPath(parent map[Point]Point, start, goal Point) []Point {
	var path []Point
	for p := goal; p != start; p = parent[p] {
		path = append(path, p)
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

func manhattan(a, b Point) int {
	dx := a.X - b.X
	if dx < 0 {
		dx = -dx
	}
	dy := a.Y - b.Y
	if dy < 0 {
		dy = -dy
	}
	return dx + dy
}

// pqItem is a node in the A* open set.
type pqItem struct {
	pt Point
	f  int // f = g + h
}

// pQueue is a min-heap of pqItems ordered by f score.
type pQueue []pqItem

func (pq pQueue) Len() int           { return len(pq) }
func (pq pQueue) Less(i, j int) bool { return pq[i].f < pq[j].f }
func (pq pQueue) Swap(i, j int)      { pq[i], pq[j] = pq[j], pq[i] }
func (pq *pQueue) Push(x any)        { *pq = append(*pq, x.(pqItem)) }
func (pq *pQueue) Pop() any {
	old := *pq
	n := len(old)
	x := old[n-1]
	*pq = old[:n-1]
	return x
}
