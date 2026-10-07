package tilemap

import "github.com/tmbritton/ecs-db/internal/tiled"

// MapIssue is an engine refusal projected onto the authored layer/cell that
// caused it. Coordinates are -1 when the refusal belongs to a whole layer or
// map; Layer is -1 for a map-level problem.
type MapIssue struct {
	Layer   int
	X, Y    int
	Message string
}

// MapIssues collects cell and shape refusals with the engine's own checkShape
// and cellAt. Like the loader, it considers only the topmost non-empty tile at
// each cell: an invalid lower tile hidden under a valid one does not stop the
// engine loading. Unlike the loader, one bad cell does not hide the next.
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
	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			owner := -1
			for i := len(m.Layers) - 1; i >= 0; i-- {
				if m.Layers[i].TileAt(x, y).GID != 0 {
					owner = i
					break
				}
			}
			if owner < 0 {
				continue
			}
			if _, _, err := cellAt(m, x, y); err != nil {
				issues = append(issues, MapIssue{Layer: owner, X: x, Y: y, Message: err.Error()})
			}
		}
	}
	return issues
}
