package spawn_test

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/spawn"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

const level = `<map width="4" height="3" tilewidth="16" tileheight="16" nextobjectid="5"><objectgroup id="1" name="markers"><object id="4" x="0" y="0"/></objectgroup><objectgroup id="2" name="spawns"/></map>`

func types() *schema.DatabaseSchema {
	return &schema.DatabaseSchema{EntityTypes: map[string]schema.EntityType{
		"Goblin": {RequiredComponents: []string{"Position"}},
		"Player": {OptionalComponents: []string{"Position"}},
		"Ghost":  {RequiredComponents: []string{"Health"}},
		"Tile":   {RequiredComponents: []string{"Position"}},
	}}
}

func doc(t *testing.T) *tiled.Document {
	t.Helper()
	d, err := tiled.ParseDocument([]byte(level), "level.tmx")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPlace_WritesSpawnAtSelectedGroupAndCell(t *testing.T) {
	d := doc(t)
	id, err := spawn.Place(d, types(), 1, "Goblin", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if id != 5 || d.NextObjectID() != 6 {
		t.Fatalf("id %d, next %d", id, d.NextObjectID())
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.ObjectGroups[0].Objects) != 1 || len(m.ObjectGroups[1].Objects) != 1 {
		t.Fatalf("wrong group changed: %+v", m.ObjectGroups)
	}
	o := m.ObjectGroups[1].Objects[0]
	if o.ID != 5 || o.Type != "Goblin" || o.X != 32 || o.Y != 16 || o.GID != 0 {
		t.Errorf("spawn is %+v", o)
	}
}

func TestPlace_SeedsRequiredComponentsTheEngineWillInsert(t *testing.T) {
	s := types()
	s.EntityTypes["Goblin"] = schema.EntityType{RequiredComponents: []string{"Position", "Health"}}
	s.Components = map[string]schema.Component{
		"Health": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
			"hp": {Type: schema.PropertyTypeInteger}, "name": {Type: schema.PropertyTypeString},
		}},
	}
	d := doc(t)
	if _, err := spawn.Place(d, s, 1, "Goblin", 1, 2); err != nil {
		t.Fatal(err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	p := m.ObjectGroups[1].Objects[0].Properties
	if p["Health.hp"].Type != "int" || p.Get("Health.hp") != "0" ||
		p.Get("Health.name") != "" {
		t.Errorf("required Health fields were not seeded for engine import: %+v", p)
	}
}

func TestReason_RefusesATypeWhoseRequiredReferenceHasNoInitialTarget(t *testing.T) {
	s := types()
	s.EntityTypes["Goblin"] = schema.EntityType{RequiredComponents: []string{"Position", "Target"}}
	s.Components = map[string]schema.Component{"Target": {Type: schema.ComponentTypeEntityRef}}
	if reason := spawn.Reason(s, "Goblin"); !strings.Contains(reason, "Target") {
		t.Errorf("a required reference needs an authored target: %q", reason)
	}
}

func TestPlace_RefusesInvalidInputWithoutChangingDocument(t *testing.T) {
	for _, tc := range []struct {
		name, kind  string
		group, x, y int
	}{
		{"unknown type", "Nope", 1, 1, 1},
		{"no Position", "Ghost", 1, 1, 1},
		{"importer-owned Tile", "Tile", 1, 1, 1},
		{"missing group", "Goblin", 2, 1, 1},
		{"negative cell", "Goblin", 1, -1, 1},
		{"past map", "Goblin", 1, 4, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := doc(t)
			before := string(d.Bytes())
			if _, err := spawn.Place(d, types(), tc.group, tc.kind, tc.x, tc.y); err == nil {
				t.Fatal("invalid placement accepted")
			}
			if string(d.Bytes()) != before {
				t.Error("a refused placement mutated the map")
			}
		})
	}
}

func TestMoveAndDelete_PreserveObjectIdentityAndDoNotRecycleIDs(t *testing.T) {
	d := doc(t)
	id, err := spawn.Place(d, types(), 1, "Player", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := spawn.Move(d, id, 3, 2); err != nil {
		t.Fatal(err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	o := m.ObjectGroups[1].Objects[0]
	if o.ID != id || o.X != 48 || o.Y != 32 {
		t.Errorf("moved object is %+v", o)
	}
	if err := spawn.Delete(d, id); err != nil {
		t.Fatal(err)
	}
	second, err := spawn.Place(d, types(), 1, "Goblin", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if second != id+1 {
		t.Errorf("recycled deleted id %d as %d", id, second)
	}
}

func TestMove_TileObjectUsesBottomLeftOrigin(t *testing.T) {
	src := strings.Replace(level, `<objectgroup id="2" name="spawns"/>`,
		`<objectgroup id="2" name="spawns"><object id="5" type="Goblin" gid="1" x="0" y="16"/></objectgroup>`, 1)
	d, err := tiled.ParseDocument([]byte(src), "level.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := spawn.Move(d, 5, 2, 2); err != nil {
		t.Fatal(err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	o := m.ObjectGroups[1].Objects[0]
	if o.X != 32 || o.Y != 48 || o.GID != 1 || o.ID != 5 {
		t.Errorf("tile object's bottom edge was not preserved: %+v", o)
	}
}

func TestMoveAndDelete_RefuseNonSpawnAndOutsideMap(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(*tiled.Document) error
	}{
		{"move marker", func(d *tiled.Document) error { return spawn.Move(d, 4, 1, 1) }},
		{"delete marker", func(d *tiled.Document) error { return spawn.Delete(d, 4) }},
		{"move missing", func(d *tiled.Document) error { return spawn.Move(d, 99, 1, 1) }},
		{"delete missing", func(d *tiled.Document) error { return spawn.Delete(d, 99) }},
		{"move outside", func(d *tiled.Document) error { return spawn.Move(d, 4, 4, 1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := doc(t)
			before := string(d.Bytes())
			if err := tc.do(d); err == nil {
				t.Fatal("invalid edit accepted")
			}
			if got := string(d.Bytes()); got != before {
				t.Errorf("refusal mutated document: %s", strings.TrimPrefix(got, before))
			}
		})
	}
}

func TestMoveAndDelete_RefuseDuplicateObjectIDs(t *testing.T) {
	src := strings.Replace(level, `<objectgroup id="2" name="spawns"/>`,
		`<objectgroup id="2" name="spawns"><object id="4" type="Goblin" x="16" y="16"/></objectgroup>`, 1)
	for _, tc := range []struct {
		name string
		do   func(*tiled.Document) error
	}{
		{"move", func(d *tiled.Document) error { return spawn.Move(d, 4, 2, 2) }},
		{"delete", func(d *tiled.Document) error { return spawn.Delete(d, 4) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := tiled.ParseDocument([]byte(src), "duplicate.tmx")
			if err != nil {
				t.Fatal(err)
			}
			before := string(d.Bytes())
			if err := tc.do(d); err == nil || !strings.Contains(err.Error(), "duplicate") {
				t.Errorf("ambiguous id should be refused, got %v", err)
			}
			if string(d.Bytes()) != before {
				t.Error("refused edit changed a marker or a spawn")
			}
		})
	}
}
