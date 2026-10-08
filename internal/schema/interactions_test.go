package schema

import (
	"os"
	"strings"
	"testing"
)

func TestRepoSchema_TileArtworkHasNoUniversalPassability(t *testing.T) {
	raw, err := os.ReadFile("../../schema.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := LoadSchema(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := s.Components["Tile"].Properties["passable"]; found {
		t.Fatal("the bundled Tile still declares a universal passable flag")
	}
	tileType := s.EntityTypes["Tile"]
	if !tileType.IsComponentRequired("Position") || !tileType.IsComponentRequired("TileReferences") {
		t.Fatal("placed Tiles must own Position and any number of entity references")
	}
	if tileType.IsComponentRequired("TileVisual") || tileType.IsComponentAllowed("TileVisual") {
		t.Fatal("artwork belongs to referenced entities, not the Tile")
	}
	for _, name := range []string{"Floor", "Wall", "River"} {
		entityType := s.EntityTypes[name]
		if !entityType.IsComponentAllowed("TileVisual") ||
			!entityType.IsComponentRequired("Passability") || !entityType.IsComponentRequired("Visibility") {
			t.Errorf("referenced %s must support artwork, passability and visibility", name)
		}
	}
	floor := s.EntityTypes["Floor"]
	if !floor.IsComponentRequired("TileVisual") {
		t.Error("art-only Floor must have a visual")
	}
	for _, name := range []string{"Wall", "River"} {
		restriction := s.EntityTypes[name]
		if restriction.IsComponentRequired("TileVisual") {
			t.Errorf("%s cannot act as a restriction-only shared reference", name)
		}
	}
	if _, ok := s.Components["TileReferences"]; !ok {
		t.Fatal("TileReferences is not declared in the bundled schema")
	}
	wallType, riverType := s.EntityTypes["Wall"], s.EntityTypes["River"]
	if wallType.IsComponentRequired("Position") || riverType.IsComponentRequired("Position") {
		t.Fatal("tile-referenced Wall/River should not need independent Position")
	}
	if err := ValidateSchema(s); err != nil {
		t.Fatal(err)
	}
}

func interactionFixture() DatabaseSchema {
	return DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]Component{
			"Passability": {Type: "object", Properties: map[string]Property{"kind": {Type: "string"}}},
			"Visibility":  {Type: "object", Properties: map[string]Property{"kind": {Type: "string"}}},
			"Flying":      {Type: "boolean"},
			"Phased":      {Type: "boolean"},
			"NightVision": {Type: "boolean"},
		},
		EntityTypes: map[string]EntityType{"Actor": {
			RequiredComponents: []string{"Passability"}, ValidationLevel: ValidationStrict,
		}},
		Interactions: map[string]map[string]InteractionRule{
			"Passability": {
				"solid": {Allows: []string{"Flying", "Phased"}},
				"open":  {Open: true},
			},
			"Visibility": {
				"opaque":   {},
				"darkness": {Allows: []string{"NightVision"}},
			},
		},
	}
}

func TestValidateSchema_InteractionCategoriesAndCapabilities(t *testing.T) {
	tests := []struct {
		name string
		edit func(*DatabaseSchema)
		want string
	}{
		{name: "valid policy"},
		{name: "missing rules for declared restriction", edit: func(s *DatabaseSchema) {
			delete(s.Interactions, "Passability")
		}, want: "Passability"},
		{name: "lowercase restriction component is not silently ignored", edit: func(s *DatabaseSchema) {
			comp := s.Components["Passability"]
			delete(s.Components, "Passability")
			s.Components["passability"] = comp
			s.EntityTypes["Actor"] = EntityType{RequiredComponents: []string{"passability"}, ValidationLevel: ValidationStrict}
			s.Interactions["passability"] = s.Interactions["Passability"]
			delete(s.Interactions, "Passability")
		}, want: "Passability"},
		{name: "lowercase footprint component is not silently ignored", edit: func(s *DatabaseSchema) {
			s.Components["occupiedcells"] = Component{Type: "array", Items: &Property{Type: "integer"}}
		}, want: "OccupiedCells"},
		{name: "footprint must be an array of offsets", edit: func(s *DatabaseSchema) {
			s.Components["OccupiedCells"] = Component{Type: "string"}
		}, want: "OccupiedCells"},
		{name: "footprint needs integer x and y", edit: func(s *DatabaseSchema) {
			s.Components["OccupiedCells"] = Component{Type: "array", Items: &Property{Type: "object", Properties: map[string]Property{
				"x": {Type: "number"}, "y": {Type: "integer"},
			}}}
		}, want: "OccupiedCells"},
		{name: "tile references require an entity-ref array", edit: func(s *DatabaseSchema) {
			s.Components["TileReferences"] = Component{Type: "array", Items: &Property{Type: "string"}}
		}, want: "TileReferences"},
		{name: "unknown restriction component", edit: func(s *DatabaseSchema) {
			s.Interactions["Missing"] = map[string]InteractionRule{"solid": {}}
		}, want: "Missing"},
		{name: "missing kind field", edit: func(s *DatabaseSchema) {
			s.Components["Passability"] = Component{Type: "object", Properties: map[string]Property{"other": {Type: "string"}}}
		}, want: "kind"},
		{name: "empty category", edit: func(s *DatabaseSchema) {
			s.Interactions["Passability"][""] = InteractionRule{}
		}, want: "category"},
		{name: "unknown capability", edit: func(s *DatabaseSchema) {
			s.Interactions["Passability"]["solid"] = InteractionRule{Allows: []string{"Teleported"}}
		}, want: "Teleported"},
		{name: "capability not boolean", edit: func(s *DatabaseSchema) {
			s.Components["Flying"] = Component{Type: "string"}
		}, want: "boolean"},
		{name: "duplicate capability", edit: func(s *DatabaseSchema) {
			s.Interactions["Passability"]["solid"] = InteractionRule{Allows: []string{"Flying", "Flying"}}
		}, want: "duplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := interactionFixture()
			if tt.edit != nil {
				tt.edit(&s)
			}
			err := ValidateSchema(s)
			if tt.want == "" && err != nil {
				t.Fatalf("valid policy refused: %v", err)
			}
			if tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("ValidateSchema = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestAllowsInteraction_IndependentBooleanAbilities(t *testing.T) {
	s := interactionFixture()
	s.Components["Burrowing"] = Component{Type: "boolean"}
	s.Interactions["Passability"]["solid"] = InteractionRule{Allows: []string{"Flying", "Phased", "Burrowing"}}
	if err := ValidateSchema(s); err != nil {
		t.Fatalf("mod-defined ability rejected: %v", err)
	}
	tests := []struct {
		name      string
		component string
		category  string
		abilities map[string]bool
		want      bool
		wantErr   bool
	}{
		{name: "wall blocks walker", component: "Passability", category: "solid"},
		{name: "flyer crosses wall", component: "Passability", category: "solid", abilities: map[string]bool{"Flying": true}, want: true},
		{name: "inactive flyer blocked", component: "Passability", category: "solid", abilities: map[string]bool{"Flying": false}},
		{name: "phased crosses wall", component: "Passability", category: "solid", abilities: map[string]bool{"Phased": true}, want: true},
		{name: "mod-defined ability crosses wall", component: "Passability", category: "solid", abilities: map[string]bool{"Burrowing": true}, want: true},
		{name: "open needs no ability", component: "Passability", category: "open", want: true},
		{name: "opaque blocks flying", component: "Visibility", category: "opaque", abilities: map[string]bool{"Flying": true}},
		{name: "darkness allows night vision", component: "Visibility", category: "darkness", abilities: map[string]bool{"NightVision": true}, want: true},
		{name: "phasing does not grant night vision", component: "Visibility", category: "darkness", abilities: map[string]bool{"Phased": true}},
		{name: "typo is an error", component: "Passability", category: "solidd", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.AllowsInteraction(tt.component, tt.category, tt.abilities)
			if (err != nil) != tt.wantErr || (!tt.wantErr && got != tt.want) {
				t.Fatalf("AllowsInteraction(%q,%q) = %v,%v; want %v,error=%v", tt.component, tt.category, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestMarshal_InteractionPolicySurvivesSchemaSave(t *testing.T) {
	s := interactionFixture()
	data, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSchema(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSchema(loaded); err != nil {
		t.Fatal(err)
	}
	if got, err := loaded.AllowsInteraction("Passability", "solid", map[string]bool{"Flying": true}); err != nil || !got {
		t.Fatalf("policy lost in schema save: %v,%v", got, err)
	}
	again, err := Marshal(loaded)
	if err != nil || string(again) != string(data) {
		t.Fatalf("policy save not byte-stable: %v\n%s\n%s", err, data, again)
	}
}

func TestLoadSchema_DuplicateInteractionKeysAreRefused(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"duplicate restriction", `{"schemaVersion":1,"components":{},"entityTypes":{},"interactions":{"Passability":{},"Passability":{}}}`, "Passability"},
		{"duplicate category", `{"schemaVersion":1,"components":{},"entityTypes":{},"interactions":{"Passability":{"solid":{},"solid":{}}}}`, "solid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadSchema([]byte(tt.raw))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadSchema = %v, want duplicate %q", err, tt.want)
			}
		})
	}
}
