package modes

import (
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/mapvalidation"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func mapIDAction(path string) string {
	return "@post('/forge/map/identity?map=" + urlValue(path) + "&id=' + encodeURIComponent(evt.target.value))"
}

func validationID(issue mapvalidation.Issue, id string) string {
	switch issue.Kind {
	case mapvalidation.Map:
		if issue.Warning && id == "" {
			return "map-id-warning"
		}
		return "map-identity-conflict"
	case mapvalidation.Tileset:
		return "map-tileset-problem-" + itoa(issue.Tileset)
	default:
		return "map-validation-problem"
	}
}

func mapLayerProblem(report mapvalidation.Report, index int) bool {
	return len(report.LayerIssues(index)) > 0
}

func mapCellProblem(report mapvalidation.Report, layer, x, y int) bool {
	return len(report.CellIssues(layer, x, y)) > 0
}

func mapSpawnProblem(report mapvalidation.Report, group, index int) (invalid, warning bool) {
	for _, issue := range report.SpawnIssues(group, index) {
		if issue.Warning {
			warning = true
		} else {
			invalid = true
		}
	}
	return invalid, warning
}

func mapSpawnInvalid(report mapvalidation.Report, group, index int) bool {
	bad, _ := mapSpawnProblem(report, group, index)
	return bad
}

func mapSpawnWarning(report mapvalidation.Report, group, index int) bool {
	_, warn := mapSpawnProblem(report, group, index)
	return warn
}

func mapProblemTitle(report mapvalidation.Report, kind mapvalidation.Kind, layer, x, y, group, index int) string {
	var issues []mapvalidation.Issue
	switch kind {
	case mapvalidation.Layer:
		issues = report.LayerIssues(layer)
	case mapvalidation.Cell:
		issues = report.CellIssues(layer, x, y)
	case mapvalidation.Spawn:
		issues = report.SpawnIssues(group, index)
	}
	var messages []string
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	return strings.Join(messages, "\n")
}

func mapCellTitle(report mapvalidation.Report, layer, x, y int, fallback string) string {
	if reason := mapProblemTitle(report, mapvalidation.Cell, layer, x, y, 0, 0); reason != "" {
		return reason
	}
	return fallback
}

func spawnIDCounts(groups []tiled.ObjectGroup) map[int]int {
	counts := make(map[int]int)
	for _, g := range groups {
		for _, obj := range g.Objects {
			counts[obj.ID]++
		}
	}
	return counts
}

func spawnMarkerID(counts map[int]int, group, index, id int) string {
	if counts[id] > 1 {
		return "spawn-" + itoa(id) + "-g" + itoa(group) + "-i" + itoa(index)
	}
	return "spawn-" + itoa(id)
}
