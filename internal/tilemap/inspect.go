package tilemap

import (
	"fmt"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

// MapIssue is an engine refusal projected onto the authored layer/cell that
// caused it. Coordinates are -1 when the refusal belongs to a whole layer or
// map; Layer is -1 for a map-level problem.
type MapIssue struct {
	Layer   int
	X, Y    int
	Message string
}

// MapIssues collects cell and shape refusals for every authored layer tile.
// The importer no longer ignores a bad tile just because another layer covers
// it. Unlike the loader, one bad cell does not hide the next.
func MapIssues(m *tiled.Map) []MapIssue {
	if m == nil {
		return nil
	}
	var issues []MapIssue
	if err := checkOrientation(m); err != nil {
		issues = append(issues, MapIssue{Layer: -1, X: -1, Y: -1, Message: err.Error()})
	}
	base := *m
	base.Layers = nil
	if err := checkShape(&base); err != nil {
		return append(issues, MapIssue{Layer: -1, X: -1, Y: -1, Message: err.Error()})
	}
	for i, layer := range m.Layers {
		one := *m
		one.Layers = []tiled.Layer{layer}
		if err := checkShape(&one); err != nil {
			issues = append(issues, MapIssue{Layer: i, X: -1, Y: -1, Message: err.Error()})
		}
	}
	type cell struct{ layer, x, y int }
	drawProblems := make(map[cell]string)
	for _, placement := range m.AllPlacements() {
		if placement.Problem != "" {
			drawProblems[cell{placement.LayerIndex, placement.X, placement.Y}] = placement.Problem
		}
	}
	for i, layer := range m.Layers {
		for y := 0; y < m.Height; y++ {
			for x := 0; x < m.Width; x++ {
				gid := layer.TileAt(x, y).GID
				if gid == 0 {
					continue
				}
				where := fmt.Sprintf("tilemap: %s: layer %q: tile at (%d,%d) has global id %d", m.Name, layer.Name, x, y, gid)
				ts, local, err := tileOf(m, where, gid)
				if err == nil {
					_, err = stateOf(ts, local, where)
				}
				if err == nil {
					if why := drawProblems[cell{i, x, y}]; why != "" {
						err = fmt.Errorf("%s: %s", where, why)
					}
				}
				if err != nil {
					issues = append(issues, MapIssue{Layer: i, X: x, Y: y, Message: err.Error()})
				}
			}
		}
	}
	return issues
}
