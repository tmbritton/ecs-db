// Package mapvalidation attaches the engine's map and spawn refusals to the
// authored cells, layers and objects Forge can mark while the map is edited.
package mapvalidation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/maps"
	"github.com/tmbritton/ecs-db/internal/forge/tilelinks"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

type Kind string

const (
	Map     Kind = "map"
	Tileset Kind = "tileset"
	Layer   Kind = "layer"
	Cell    Kind = "cell"
	Spawn   Kind = "spawn"
)

// Issue belongs to one owner, not just to a list. Group and ObjectIndex
// distinguish objects even when two of them claim the same Tiled ID.
type Issue struct {
	Kind        Kind
	Message     string
	Warning     bool
	Tileset     int
	Layer       int
	X, Y        int
	Group       int
	ObjectIndex int
	ObjectID    int
}

type Report struct {
	Issues []Issue
	// Owner lookups keep a 500x500 canvas from scanning every map finding
	// 250,000 times per stream render. Only Build fills these indexes; a
	// hand-built report in a template test falls back to the Issues slice.
	layers map[int][]Issue
	cells  map[[3]int][]Issue
	spawns map[[2]int][]Issue
}

func (r Report) LayerIssues(index int) []Issue {
	if r.layers != nil {
		return r.layers[index]
	}
	var out []Issue
	for _, issue := range r.Issues {
		if issue.Kind == Layer && issue.Layer == index {
			out = append(out, issue)
		}
	}
	return out
}

func (r Report) CellIssues(layer, x, y int) []Issue {
	if r.cells != nil {
		return r.cells[[3]int{layer, x, y}]
	}
	var out []Issue
	for _, issue := range r.Issues {
		if issue.Kind == Cell && issue.Layer == layer && issue.X == x && issue.Y == y {
			out = append(out, issue)
		}
	}
	return out
}

func (r Report) SpawnIssues(group, index int) []Issue {
	if r.spawns != nil {
		return r.spawns[[2]int{group, index}]
	}
	var out []Issue
	for _, issue := range r.Issues {
		if issue.Kind == Spawn && issue.Group == group && issue.ObjectIndex == index {
			out = append(out, issue)
		}
	}
	return out
}

type Identity struct{ Path, ID string }

// Build is deterministic: project map order, then tilesets and tile layers in
// authored order, then groups and objects in authored order with properties
// sorted by name. Re-rendering a map unchanged must not reshuffle its messages
// or make the page stream send an identical canvas again.
func Build(s *schema.DatabaseSchema, path string, m *tiled.Map, identities []Identity, refs []maps.TilesetProblem) Report {
	if m == nil || s == nil {
		return Report{}
	}
	report := Report{layers: map[int][]Issue{}, cells: map[[3]int][]Issue{}, spawns: map[[2]int][]Issue{}}
	add := func(issue Issue) {
		report.Issues = append(report.Issues, issue)
		switch issue.Kind {
		case Layer:
			report.layers[issue.Layer] = append(report.layers[issue.Layer], issue)
		case Cell:
			key := [3]int{issue.Layer, issue.X, issue.Y}
			report.cells[key] = append(report.cells[key], issue)
		case Spawn:
			key := [2]int{issue.Group, issue.ObjectIndex}
			report.spawns[key] = append(report.spawns[key], issue)
		}
	}
	if warning := tilemap.MapIdentityWarning(path, m); warning != "" {
		add(Issue{Kind: Map, Message: warning, Warning: true})
	} else {
		id := m.Properties.Get(tiled.PropMapID)
		var files []string
		for _, other := range identities {
			if other.ID == id {
				files = append(files, other.Path)
			}
		}
		if len(files) > 1 {
			sort.Strings(files)
			add(Issue{Kind: Map, Message: fmt.Sprintf("mapId %q is shared by maps %s; each map needs its own identity", id, strings.Join(files, ", "))})
		}
	}
	for _, problem := range refs {
		add(Issue{Kind: Tileset, Tileset: problem.Index, Message: problem.Err.Error()})
	}
	// The importer keys every Tile entity by a positive, unique layer ID.
	// The editor also needs those IDs: moving or deleting a layer changes its
	// index and could silently retarget an open tab's eye or paint selection.
	// Name both consequences on every affected row.
	byLayerID := make(map[int][]int, len(m.Layers))
	for i, layer := range m.Layers {
		if layer.ID > 0 {
			byLayerID[layer.ID] = append(byLayerID[layer.ID], i)
		}
	}
	for i, layer := range m.Layers {
		switch {
		case layer.ID <= 0:
			add(Issue{Kind: Layer, Layer: i, Message: fmt.Sprintf(
				"layer %q at row %d has no positive Tiled ID; the engine cannot import this map, and you cannot reorder or delete it until you assign a unique positive Tiled ID",
				layer.Name, i+1)})
		case len(byLayerID[layer.ID]) > 1:
			rows := byLayerID[layer.ID]
			add(Issue{Kind: Layer, Layer: i, Message: fmt.Sprintf(
				"tile layer ID %d is shared by %d layers (%q at row %d and %q at row %d); the engine cannot import this map, and you cannot reorder or delete these layers until each has a unique positive Tiled ID",
				layer.ID, len(rows), m.Layers[rows[0]].Name, rows[0]+1, m.Layers[rows[1]].Name, rows[1]+1)})
		}
	}
	for _, issue := range tilemap.MapIssues(m) {
		kind := Cell
		if issue.Layer < 0 {
			kind = Map
		} else if issue.X < 0 {
			kind = Layer
		}
		add(Issue{Kind: kind, Layer: issue.Layer, X: issue.X, Y: issue.Y, Message: issue.Message})
	}
	// Gather all claimants before reporting an ID collision: only marking
	// whichever appeared second leaves the first unmarked and unfindable.
	type owner struct {
		group, index    int
		name, groupName string
	}
	byID := map[int][]owner{}
	for g, group := range m.ObjectGroups {
		for i, obj := range group.Objects {
			byID[obj.ID] = append(byID[obj.ID], owner{g, i, obj.Name, group.Name})
		}
	}
	duplicates := map[int]string{}
	for id, owners := range byID {
		if len(owners) > 1 {
			duplicates[id] = fmt.Sprintf("object id %d belongs to another object in this map (%d claimants): %s, %s",
				id, len(owners), ownerName(owners[0].groupName, owners[0].group, owners[0].index, owners[0].name),
				ownerName(owners[1].groupName, owners[1].group, owners[1].index, owners[1].name))
		}
	}
	artValidator := tilelinks.NewArtValidator(m)
	for g, group := range m.ObjectGroups {
		for i, obj := range group.Objects {
			if description := duplicates[obj.ID]; description != "" {
				add(Issue{
					Kind: Spawn, Group: g, ObjectIndex: i, ObjectID: obj.ID,
					Message: description + "; this is " + ownerName(group.Name, g, i, obj.Name),
				})
			}
			if obj.Type == "" {
				for name := range obj.Properties {
					if strings.EqualFold(name, "TileLink.layerID") || strings.EqualFold(name, "TileLink.cells") {
						add(Issue{
							Kind: Spawn, Group: g, ObjectIndex: i, ObjectID: obj.ID,
							Message: fmt.Sprintf("TileLink object %d has no entity type; give it a class before importing", obj.ID),
						})
						break
					}
				}
				continue // an untyped Tiled object is not a spawn
			}
			spawnFindings := spawnIssues(s, m, obj)
			parseable := true
			for _, problem := range spawnFindings {
				if !problem.Warning {
					parseable = false
				}
				problem.Kind, problem.Group, problem.ObjectIndex, problem.ObjectID = Spawn, g, i, obj.ID
				add(problem)
			}
			for name := range obj.Properties {
				if !strings.EqualFold(name, "TileLink.layerID") && !strings.EqualFold(name, "TileLink.cells") {
					continue
				}
				if parseable {
					if err := tilemap.ValidateSpawnFields(s, m, obj); err != nil {
						add(Issue{Kind: Spawn, Group: g, ObjectIndex: i, ObjectID: obj.ID, Message: err.Error()})
					}
				}
				if err := artValidator.Validate(obj.ID); err != nil {
					add(Issue{Kind: Spawn, Group: g, ObjectIndex: i, ObjectID: obj.ID, Message: err.Error()})
				}
				break
			}
		}
	}
	return report
}

func ownerName(group string, groupIndex, index int, name string) string {
	if group == "" {
		group = "unnamed group"
	}
	if name == "" {
		name = "unnamed"
	}
	return fmt.Sprintf("%s object %d (group %d) %q", group, index+1, groupIndex+1, name)
}

func spawnIssues(s *schema.DatabaseSchema, m *tiled.Map, obj tiled.Object) []Issue {
	var issues []Issue
	blank := obj
	blank.Properties = nil
	if _, err := tilemap.ValidateSpawn(s, m, blank); err != nil {
		return []Issue{{Message: err.Error()}}
	}
	valid := tiled.Properties{}
	names := make([]string, 0, len(obj.Properties))
	for name := range obj.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		single := blank
		single.Properties = tiled.Properties{name: obj.Properties[name]}
		if _, err := tilemap.ValidateSpawn(s, m, single); err != nil {
			issues = append(issues, Issue{Message: err.Error()})
		} else {
			valid[name] = obj.Properties[name]
		}
	}
	blank.Properties = valid
	verdict, err := tilemap.ValidateSpawn(s, m, blank)
	if err != nil {
		issues = append(issues, Issue{Message: err.Error()})
		return issues
	}
	for _, msg := range verdict.Errors {
		issues = append(issues, Issue{Message: msg})
	}
	for _, msg := range verdict.Warnings {
		issues = append(issues, Issue{Message: msg, Warning: true})
	}
	return issues
}
