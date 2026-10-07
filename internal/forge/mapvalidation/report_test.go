package mapvalidation_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/maps"
	"github.com/tmbritton/ecs-db/internal/forge/mapvalidation"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func validationSchema(level schema.ValidationLevel) schema.DatabaseSchema {
	return schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeInteger}, "y": {Type: schema.PropertyTypeInteger},
			}},
			"Health": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"hp": {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{
			"Goblin": {RequiredComponents: []string{"Position", "Health"}, ValidationLevel: level},
		},
	}
}

func TestBuild_ReportsEveryPropertyMistakeAndTheMissingComponentOnItsSpawn(t *testing.T) {
	m := &tiled.Map{
		Name: "level.tmx", Width: 2, Height: 1, TileWidth: 16, TileHeight: 16,
		ObjectGroups: []tiled.ObjectGroup{{Name: "spawns", Objects: []tiled.Object{
			{ID: 7, Type: "Goblin", Properties: tiled.Properties{
				"hp":         {Value: "4"},
				"Mystery.hp": {Value: "7"},
				"Position.x": {Value: "1"},
			}},
		}}},
	}
	for _, tc := range []struct {
		level       schema.ValidationLevel
		wantWarning bool
	}{
		{schema.ValidationStrict, false},
		{schema.ValidationWarning, true},
	} {
		t.Run(string(tc.level), func(t *testing.T) {
			s := validationSchema(tc.level)
			report := mapvalidation.Build(&s, "level.tmx", m, nil, nil)
			for _, want := range []string{
				`property "hp" names no component; write it as Component.hp`,
				`property "Mystery.hp" names component "Mystery"`,
				`property "Position.x" sets Position`,
				`missing required component "Health" for entity type "Goblin"`,
			} {
				found := false
				for _, issue := range report.Issues {
					if issue.Kind == mapvalidation.Spawn && issue.ObjectID == 7 && issue.Group == 0 && issue.ObjectIndex == 0 &&
						strings.Contains(issue.Message, want) {
						found = true
						if strings.Contains(want, "missing required") && issue.Warning != tc.wantWarning {
							t.Errorf("wrong strict/warning severity for %q: %+v", want, issue)
						}
					}
				}
				if !found {
					t.Errorf("missing %q among %+v", want, report.Issues)
				}
			}
		})
	}
}

func TestBuild_DuplicateMapAndSpawnIdentitiesNameBothOwners(t *testing.T) {
	m := &tiled.Map{
		Name: "a.tmx", Width: 2, Height: 1, TileWidth: 16, TileHeight: 16,
		Properties: tiled.Properties{tiled.PropMapID: {Value: "shared"}},
		ObjectGroups: []tiled.ObjectGroup{
			{Name: "actors", Objects: []tiled.Object{{ID: 8, Name: "first", Type: "Goblin"}}},
			{Name: "extras", Objects: []tiled.Object{{ID: 8, Name: "second", Type: "Missing"}}},
		},
	}
	s := validationSchema(schema.ValidationStrict)
	report := mapvalidation.Build(&s, "a.tmx", m,
		[]mapvalidation.Identity{{Path: "a.tmx", ID: "shared"}, {Path: "b.tmx", ID: "shared"}}, nil)
	var mapProblem, first, second, unknown bool
	for _, issue := range report.Issues {
		switch {
		case issue.Kind == mapvalidation.Map && strings.Contains(issue.Message, "a.tmx") && strings.Contains(issue.Message, "b.tmx"):
			mapProblem = true
		case issue.Kind == mapvalidation.Spawn && issue.Group == 0 && issue.ObjectIndex == 0 && strings.Contains(issue.Message, "object id 8"):
			first = true
		case issue.Kind == mapvalidation.Spawn && issue.Group == 1 && issue.ObjectIndex == 0 && strings.Contains(issue.Message, "object id 8"):
			second = true
		case issue.Kind == mapvalidation.Spawn && issue.Group == 1 && strings.Contains(issue.Message, `unknown entity type "Missing"`):
			unknown = true
		}
	}
	if !mapProblem || !first || !second || !unknown {
		t.Errorf("not all identity/type errors attached: %+v", report.Issues)
	}
}

func TestBuild_DuplicateUnnamedObjectsCanBeFoundByGroupAndIndex(t *testing.T) {
	m := &tiled.Map{
		Name: "level.tmx", Width: 2, Height: 1, TileWidth: 16, TileHeight: 16,
		Properties: tiled.Properties{tiled.PropMapID: {Value: "level"}},
		ObjectGroups: []tiled.ObjectGroup{{Name: "spawns", Objects: []tiled.Object{
			{ID: 1, Type: "Goblin"}, {ID: 1, Type: "Goblin", X: 16},
		}}},
	}
	s := validationSchema(schema.ValidationStrict)
	report := mapvalidation.Build(&s, "level.tmx", m, nil, nil)
	for _, i := range []int{0, 1} {
		issues := report.SpawnIssues(0, i)
		if len(issues) == 0 || !strings.Contains(issues[0].Message, "spawns object 1") ||
			!strings.Contains(issues[0].Message, "spawns object 2") {
			t.Errorf("duplicate unnamed owner %d was not findable: %+v", i, issues)
		}
	}
}

func TestBuild_ManyDuplicateIDsKeepPerOwnerMessageBounded(t *testing.T) {
	objects := make([]tiled.Object, 1000)
	for i := range objects {
		objects[i].ID = 0
	}
	m := &tiled.Map{
		Name: "many.tmx", Width: 1, Height: 1, TileWidth: 16, TileHeight: 16,
		Properties:   tiled.Properties{tiled.PropMapID: {Value: "many"}},
		ObjectGroups: []tiled.ObjectGroup{{Name: "notes", Objects: objects}},
	}
	s := validationSchema(schema.ValidationStrict)
	report := mapvalidation.Build(&s, "many.tmx", m, nil, nil)
	if len(report.Issues) != 1000 {
		t.Fatalf("1000 claimants produced %d findings", len(report.Issues))
	}
	for _, index := range []int{0, 999} {
		issue := report.SpawnIssues(0, index)[0]
		if len(issue.Message) > 300 || !strings.Contains(issue.Message, "notes object "+strconv.Itoa(index+1)) {
			t.Errorf("claimant %d has an unbounded or unfindable message: %.350s", index, issue.Message)
		}
	}
}

func TestBuild_UnnamedGroupsStillIdentifyEachDuplicateObject(t *testing.T) {
	m := &tiled.Map{
		Name: "unnamed.tmx", Width: 1, Height: 1, TileWidth: 16, TileHeight: 16,
		Properties: tiled.Properties{tiled.PropMapID: {Value: "unnamed"}},
		ObjectGroups: []tiled.ObjectGroup{
			{Objects: []tiled.Object{{ID: 4, Type: "Goblin"}}},
			{Objects: []tiled.Object{{ID: 4, Type: "Goblin"}}},
		},
	}
	s := validationSchema(schema.ValidationStrict)
	report := mapvalidation.Build(&s, "unnamed.tmx", m, nil, nil)
	for _, group := range []int{0, 1} {
		issues := report.SpawnIssues(group, 0)
		if len(issues) == 0 || !strings.Contains(issues[0].Message, "group 1") ||
			!strings.Contains(issues[0].Message, "group 2") {
			t.Errorf("unnamed groups are not distinguishable: %+v", issues)
		}
	}
}

func TestBuild_MissingMapIDWarnsInTheEnginesOwnTerms(t *testing.T) {
	m := &tiled.Map{Name: "level.tmx", Width: 1, Height: 1, TileWidth: 16, TileHeight: 16}
	s := validationSchema(schema.ValidationStrict)
	report := mapvalidation.Build(&s, "maps/level.tmx", m, nil,
		[]maps.TilesetProblem{{Index: 0, Source: "lost.tsx", Err: errTest("tiled: level.tmx: reading tileset lost.tsx: file missing")}})
	var identity, tileset bool
	for _, issue := range report.Issues {
		if issue.Kind == mapvalidation.Map && issue.Warning && strings.Contains(issue.Message, "spawns are filed under its path") {
			identity = true
		}
		if issue.Kind == mapvalidation.Tileset && issue.Tileset == 0 && strings.Contains(issue.Message, "lost.tsx") {
			tileset = true
		}
	}
	if !identity || !tileset {
		t.Errorf("no engine mapId warning or named tileset error: %+v", report.Issues)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestBuild_OwnerLookupsKeepCellsAndSpawnsDistinct(t *testing.T) {
	m := &tiled.Map{
		Name: "bad.tmx", Width: 2, Height: 1, TileWidth: 16, TileHeight: 16,
		Layers:       []tiled.Layer{{Name: "ground", Width: 2, Height: 1, Data: []uint32{1, 0}}},
		ObjectGroups: []tiled.ObjectGroup{{Objects: []tiled.Object{{ID: 3, Type: "Absent"}}}},
	}
	s := validationSchema(schema.ValidationStrict)
	r := mapvalidation.Build(&s, "bad.tmx", m, nil, nil)
	if got := r.CellIssues(0, 0, 0); len(got) != 1 || !strings.Contains(got[0].Message, "no tilesets") {
		t.Errorf("bad cell issue missing: %+v", got)
	}
	if got := r.CellIssues(0, 1, 0); len(got) != 0 {
		t.Errorf("issue leaked into empty cell: %+v", got)
	}
	if got := r.SpawnIssues(0, 0); len(got) != 1 || !strings.Contains(got[0].Message, "unknown entity type") {
		t.Errorf("spawn issue not attributed: %+v", got)
	}
}

func TestReport_LookupsDoNotLeakBetweenOwners(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report mapvalidation.Report
	}{
		{"hand-built", mapvalidation.Report{Issues: []mapvalidation.Issue{
			{Kind: mapvalidation.Layer, Layer: 0, Message: "layer"},
			{Kind: mapvalidation.Cell, Layer: 0, X: 1, Y: 0, Message: "cell"},
			{Kind: mapvalidation.Spawn, Group: 1, ObjectIndex: 0, Message: "spawn"},
		}}},
		{"indexed", func() mapvalidation.Report {
			m := &tiled.Map{
				Name: "m.tmx", Width: 2, Height: 1, TileWidth: 16, TileHeight: 16,
				Layers: []tiled.Layer{{Name: "short", Width: 1, Height: 1, Data: []uint32{1}}},
			}
			s := validationSchema(schema.ValidationStrict)
			return mapvalidation.Build(&s, "m.tmx", m, nil, nil)
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.report.LayerIssues(0); len(got) != 1 || got[0].Kind != mapvalidation.Layer {
				t.Errorf("layer owner: %+v", got)
			}
			if got := tc.report.LayerIssues(1); len(got) != 0 {
				t.Errorf("unrelated layer inherited a problem: %+v", got)
			}
			if tc.name == "hand-built" {
				if got := tc.report.CellIssues(0, 1, 0); len(got) != 1 || got[0].Message != "cell" {
					t.Errorf("cell owner: %+v", got)
				}
				if got := tc.report.SpawnIssues(1, 0); len(got) != 1 || got[0].Message != "spawn" {
					t.Errorf("spawn owner: %+v", got)
				}
			}
			if got := tc.report.CellIssues(9, 9, 9); len(got) != 0 {
				t.Errorf("unrelated cell inherited a problem: %+v", got)
			}
			if got := tc.report.SpawnIssues(9, 9); len(got) != 0 {
				t.Errorf("unrelated spawn inherited a problem: %+v", got)
			}
		})
	}
}

func TestBuild_MissingInputsDoNotInventFindings(t *testing.T) {
	s := validationSchema(schema.ValidationStrict)
	if got := mapvalidation.Build(&s, "", nil, nil, nil); len(got.Issues) != 0 {
		t.Errorf("nil map produced findings: %+v", got)
	}
	if got := mapvalidation.Build(nil, "", &tiled.Map{}, nil, nil); len(got.Issues) != 0 {
		t.Errorf("nil schema produced findings: %+v", got)
	}
}

func BenchmarkBuild_FourMapsWith50x50Tiles(b *testing.B) {
	s := validationSchema(schema.ValidationStrict)
	maps := make([]*tiled.Map, 4)
	ids := make([]mapvalidation.Identity, len(maps))
	for i := range maps {
		path := "level" + string(rune('A'+i)) + ".tmx"
		data := make([]uint32, 50*50)
		for cell := range data {
			data[cell] = 1
		}
		maps[i] = &tiled.Map{
			Name: path, Width: 50, Height: 50, TileWidth: 16, TileHeight: 16,
			Properties: tiled.Properties{tiled.PropMapID: {Value: path}},
			Tilesets: []tiled.TilesetRef{{FirstGID: 1, Tileset: &tiled.Tileset{
				Name: "fixture", TileCount: 1, Image: tiled.Image{Source: "tiles.png", Path: "tiles.png"},
			}}},
			Layers: []tiled.Layer{{Name: "ground", Width: 50, Height: 50, Data: data}},
		}
		ids[i] = mapvalidation.Identity{Path: path, ID: path}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j, m := range maps {
			if report := mapvalidation.Build(&s, ids[j].Path, m, ids, nil); len(report.Issues) != 0 {
				b.Fatalf("healthy map has %d validation findings", len(report.Issues))
			}
		}
	}
}
