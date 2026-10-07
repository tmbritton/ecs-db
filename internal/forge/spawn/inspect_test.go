package spawn_test

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/spawn"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func inspectSchema() *schema.DatabaseSchema {
	s := types()
	s.Components = map[string]schema.Component{
		"Position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
			"x": {Type: schema.PropertyTypeInteger}, "y": {Type: schema.PropertyTypeInteger},
		}},
		"Health": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
			"hp": {Type: schema.PropertyTypeInteger}, "maxHp": {Type: schema.PropertyTypeInteger},
		}},
		"Label": {Type: schema.ComponentTypeString},
	}
	s.EntityTypes["Goblin"] = schema.EntityType{
		RequiredComponents: []string{"Position", "Health"}, OptionalComponents: []string{"Label"},
		ValidationLevel: schema.ValidationStrict,
	}
	return s
}

func inspectedGoblin(t *testing.T, s *schema.DatabaseSchema) (*tiled.Document, int) {
	t.Helper()
	d := doc(t)
	id, err := spawn.Place(d, s, 1, "Goblin", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	return d, id
}

func TestSetProperty_UsesSchemaTypeAndImporterVerdict(t *testing.T) {
	s := inspectSchema()
	d, id := inspectedGoblin(t, s)
	verdict, err := spawn.SetProperty(d, s, id, "Health", "hp", "7")
	if err != nil || !verdict.Valid() {
		t.Fatalf("set hp: %+v, %v", verdict, err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	p := m.ObjectGroups[1].Objects[0].Properties["Health.hp"]
	if p.Type != "int" || p.Value != "7" {
		t.Errorf("the engine will see %+v, want typed Health.hp=7", p)
	}
	before := string(d.Bytes())
	if _, err := spawn.SetProperty(d, s, id, "Health", "hp", "plenty"); err == nil {
		t.Fatal("engine would refuse this integer, so Forge must refuse it")
	}
	if got := string(d.Bytes()); got != before {
		t.Error("refused edit dirtied the document")
	}
}

func TestSetProperty_ScalarUsesStorageColumnAndPositionIsReadOnly(t *testing.T) {
	s := inspectSchema()
	d, id := inspectedGoblin(t, s)
	if _, err := spawn.AddComponent(d, s, id, "Label"); err != nil {
		t.Fatal(err)
	}
	if _, err := spawn.SetProperty(d, s, id, "Label", "value", "watcher"); err != nil {
		t.Fatal(err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ObjectGroups[1].Objects[0].Properties.Get("Label.value"); got != "watcher" {
		t.Errorf("scalar stored under %q, want Label.value", got)
	}
	for _, tc := range []struct{ component, field string }{
		{"Label", "text"}, {"Position", "x"}, {"Sprite", "sheet"},
	} {
		if _, err := spawn.SetProperty(d, s, id, tc.component, tc.field, "new"); err == nil {
			t.Errorf("%s.%s was editable", tc.component, tc.field)
		}
	}
}

func TestAddAndDetach_RespectContractAndPreserveRequiredData(t *testing.T) {
	s := inspectSchema()
	d, id := inspectedGoblin(t, s)
	if _, err := spawn.AddComponent(d, s, id, "Label"); err != nil {
		t.Fatal(err)
	}
	if _, err := spawn.AddComponent(d, s, id, "Label"); err == nil {
		t.Fatal("attached optional component twice")
	}
	if _, err := spawn.DetachComponent(d, s, id, "Health"); err == nil {
		t.Fatal("detached a required component")
	}
	if _, err := spawn.DetachComponent(d, s, id, "Label"); err != nil {
		t.Fatal(err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	p := m.ObjectGroups[1].Objects[0].Properties
	if p.Has("Label.value") || !p.Has("Health.hp") {
		t.Errorf("detaching optional changed required data: %+v", p)
	}
	if _, err := spawn.AddComponent(d, s, id, "Unknown"); err == nil {
		t.Fatal("attached an undeclared component")
	}
}

func TestEdit_WarningLevelKeepsTheEngineWarningAndAllowsTheEdit(t *testing.T) {
	s := inspectSchema()
	d, id := inspectedGoblin(t, s)
	s.EntityTypes["Goblin"] = schema.EntityType{
		RequiredComponents: []string{"Position", "Health"},
		ValidationLevel:    schema.ValidationWarning,
	}
	verdict, err := spawn.AddComponent(d, s, id, "Label")
	if err != nil || len(verdict.Warnings) == 0 || !strings.Contains(verdict.Warnings[0], "not allowed") {
		t.Fatalf("warning-level extra component: %+v, %v", verdict, err)
	}
	if !strings.Contains(string(d.Bytes()), `name="Label.value"`) {
		t.Error("warning-level edit was refused")
	}
}

func TestSetProperty_RefusesAmbiguousCaseFoldedNamesWithoutChangingBytes(t *testing.T) {
	s := inspectSchema()
	d, id := inspectedGoblin(t, s)
	if err := d.SetObjectProperty(id, "health.hp", tiled.Property{Type: "int", Value: "2"}); err != nil {
		t.Fatal(err)
	}
	before := string(d.Bytes())
	if _, err := spawn.SetProperty(d, s, id, "Health", "hp", "7"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous property refusal, got %v", err)
	}
	if string(d.Bytes()) != before {
		t.Error("refused edit picked an arbitrary matching spelling")
	}
}

func TestSetProperty_CanFillAMissingFieldOfAnAttachedComponent(t *testing.T) {
	s := inspectSchema()
	d := doc(t)
	id, err := d.AddObject(1, tiled.Object{
		Type: "Goblin", X: 16, Y: 16,
		Properties: tiled.Properties{"Health.hp": {Type: "int", Value: "5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spawn.SetProperty(d, s, id, "Health", "maxHp", "8"); err != nil {
		t.Fatal(err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ObjectGroups[1].Objects[0].Properties.Get("Health.maxHp"); got != "8" {
		t.Errorf("new Health.maxHp = %q, want 8", got)
	}
}

func TestAddComponent_EntityReferenceNeedsAnExplicitTarget(t *testing.T) {
	s := inspectSchema()
	s.Components["Target"] = schema.Component{Type: schema.ComponentTypeEntityRef}
	et := s.EntityTypes["Goblin"]
	et.OptionalComponents = append(et.OptionalComponents, "Target")
	s.EntityTypes["Goblin"] = et
	for _, tc := range []struct {
		name, target string
		wantError    bool
	}{
		{"missing", "", true},
		{"not a number", "many", true},
		{"no entity zero", "0", true},
		{"explicit id", "17", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, id := inspectedGoblin(t, s)
			before := string(d.Bytes())
			_, err := spawn.AddComponentWithTarget(d, s, id, "Target", tc.target)
			if tc.wantError {
				if err == nil {
					t.Fatal("an unusable reference target was accepted")
				}
				if string(d.Bytes()) != before {
					t.Error("refusal changed the map")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			m, err := d.Map()
			if err != nil {
				t.Fatal(err)
			}
			p := m.ObjectGroups[1].Objects[0].Properties["Target.target_entity_id"]
			if p.Type != "int" || p.Value != "17" {
				t.Errorf("target = %+v, want typed reference to 17", p)
			}
		})
	}
}

func TestSetProperty_EntityReferenceMustNamePositiveTarget(t *testing.T) {
	s := inspectSchema()
	s.Components["Target"] = schema.Component{Type: schema.ComponentTypeEntityRef}
	et := s.EntityTypes["Goblin"]
	et.OptionalComponents = append(et.OptionalComponents, "Target")
	s.EntityTypes["Goblin"] = et
	for _, raw := range []string{"0", "-2", "many"} {
		t.Run(raw, func(t *testing.T) {
			d, id := inspectedGoblin(t, s)
			if _, err := spawn.AddComponentWithTarget(d, s, id, "Target", "17"); err != nil {
				t.Fatal(err)
			}
			before := string(d.Bytes())
			if _, err := spawn.SetProperty(d, s, id, "Target", "target_entity_id", raw); err == nil {
				t.Fatal("accepted an entity id SQLite cannot reference")
			}
			if string(d.Bytes()) != before {
				t.Error("refused reference edit changed the map")
			}
		})
	}
}

func TestAddComponent_ObjectWithTwoReferencesNeedsBothTargets(t *testing.T) {
	s := inspectSchema()
	s.Components["Owner"] = schema.Component{Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
		"first":  {Type: schema.PropertyTypeEntityRef},
		"second": {Type: schema.PropertyTypeEntityRef},
		"label":  {Type: schema.PropertyTypeString},
	}}
	et := s.EntityTypes["Goblin"]
	et.OptionalComponents = append(et.OptionalComponents, "Owner")
	s.EntityTypes["Goblin"] = et
	for _, tc := range []struct {
		name    string
		targets map[string]string
		valid   bool
	}{
		{"both", map[string]string{"first": "1", "second": "2"}, true},
		{"missing second", map[string]string{"first": "1"}, false},
		{"invalid second", map[string]string{"first": "1", "second": "0"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, id := inspectedGoblin(t, s)
			before := string(d.Bytes())
			_, err := spawn.AddComponentWithTargets(d, s, id, "Owner", tc.targets)
			if !tc.valid {
				if err == nil || string(d.Bytes()) != before {
					t.Fatalf("invalid reference map changed the document: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			m, err := d.Map()
			if err != nil {
				t.Fatal(err)
			}
			p := m.ObjectGroups[1].Objects[0].Properties
			if p.Get("Owner.first") != "1" || p.Get("Owner.second") != "2" || !p.Has("Owner.label") {
				t.Errorf("object references not attached: %+v", p)
			}
		})
	}
}

func TestRepairRequired_AttachesAllMissingComponentsAsOneValidatedEdit(t *testing.T) {
	s := inspectSchema()
	et := s.EntityTypes["Goblin"]
	et.RequiredComponents = []string{"Position", "Health", "Label"}
	et.OptionalComponents = nil
	s.EntityTypes["Goblin"] = et
	d := doc(t)
	id, err := d.AddObject(1, tiled.Object{Type: "Goblin", X: 16, Y: 16})
	if err != nil {
		t.Fatal(err)
	}
	verdict, err := spawn.RepairRequired(d, s, id)
	if err != nil || !verdict.Valid() {
		t.Fatalf("repair refused a candidate with every missing component: %+v, %v", verdict, err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	p := m.ObjectGroups[1].Objects[0].Properties
	if p.Get("Health.hp") != "0" || p.Get("Health.maxHp") != "0" || !p.Has("Label.value") {
		t.Errorf("required repair left %+v", p)
	}
	s.Components["Target"] = schema.Component{Type: schema.ComponentTypeEntityRef}
	et.RequiredComponents = append(et.RequiredComponents, "Target")
	s.EntityTypes["Goblin"] = et
	before := string(d.Bytes())
	if _, err := spawn.RepairRequired(d, s, id); err == nil {
		t.Fatal("reference target cannot be invented")
	}
	if string(d.Bytes()) != before {
		t.Error("a refused multi-component repair partially changed the map")
	}
}
