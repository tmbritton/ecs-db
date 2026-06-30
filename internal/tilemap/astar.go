package tilemap

import "container/heap"

// Point is a tile-grid coordinate.
type Point struct{ X, Y int }

// AStar returns the shortest path from start to goal on grid, exclusive of start
// and inclusive of goal. Returns nil if no path exists or start == goal.
func AStar(grid *TileGrid, start, goal Point) []Point {
	if start == goal {
		return nil
	}
	if !grid.IsPassable(goal.X, goal.Y) {
		return nil
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
			return reconstructPath(parent, start, goal)
		}
		if closed[cur] {
			continue
		}
		closed[cur] = true

		for _, d := range dirs {
			nb := Point{cur.X + d.X, cur.Y + d.Y}
			if !grid.IsPassable(nb.X, nb.Y) || closed[nb] {
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
	return nil
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
