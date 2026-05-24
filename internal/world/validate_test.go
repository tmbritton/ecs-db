package world

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

func TestValidateEntityCreation(t *testing.T) {
	tests := []struct {
		name             string
		schema           schema.DatabaseSchema
		entityType       string
		provided         []string
		wantErrors       int
		wantWarnings     int
		wantErrorSubstr  string
		wantWarningSubstr string
	}{
		{
			name: "valid entity with all required and one optional",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
					"Health":   {Type: schema.ComponentTypeObject},
					"Velocity": {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Goblin": {
						RequiredComponents:   []string{"Position", "Health"},
						OptionalComponents:   []string{"Velocity"},
						AllowExtraComponents: false,
						ValidationLevel:      schema.ValidationStrict,
					},
				},
			},
			entityType: "Goblin",
			provided:   []string{"Position", "Health", "Velocity"},
			wantErrors:   0,
			wantWarnings: 0,
		},
		{
			name: "valid entity with only required",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
					"Health":   {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Goblin": {
						RequiredComponents:   []string{"Position", "Health"},
						AllowExtraComponents: false,
						ValidationLevel:      schema.ValidationStrict,
					},
				},
			},
			entityType: "Goblin",
			provided:   []string{"Position", "Health"},
			wantErrors:   0,
			wantWarnings: 0,
		},
		{
			name: "missing required component strict mode",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
					"Health":   {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Goblin": {
						RequiredComponents:   []string{"Position", "Health"},
						AllowExtraComponents: false,
						ValidationLevel:      schema.ValidationStrict,
					},
				},
			},
			entityType:      "Goblin",
			provided:        []string{"Position"},
			wantErrors:      1,
			wantErrorSubstr: `missing required component "Health"`,
		},
		{
			name: "missing multiple required components",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
					"Health":   {Type: schema.ComponentTypeObject},
					"Sprite":   {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Goblin": {
						RequiredComponents:   []string{"Position", "Health", "Sprite"},
						AllowExtraComponents: false,
						ValidationLevel:      schema.ValidationStrict,
					},
				},
			},
			entityType: "Goblin",
			provided:   []string{"Position"},
			wantErrors: 2,
		},
		{
			name: "extra component disallowed strict mode",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
					"Velocity": {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Goblin": {
						RequiredComponents:   []string{"Position"},
						OptionalComponents:   []string{},
						AllowExtraComponents: false,
						ValidationLevel:      schema.ValidationStrict,
					},
				},
			},
			entityType:      "Goblin",
			provided:        []string{"Position", "Velocity"},
			wantErrors:      1,
			wantErrorSubstr: `component "Velocity" is not allowed`,
		},
		{
			name: "extra component allowed when allowExtraComponents true",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
					"Velocity": {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Particle": {
						RequiredComponents:   []string{"Position"},
						OptionalComponents:   []string{},
						AllowExtraComponents: true,
						ValidationLevel:      schema.ValidationStrict,
					},
				},
			},
			entityType: "Particle",
			provided:   []string{"Position", "Velocity"},
			wantErrors:   0,
			wantWarnings: 0,
		},
		{
			name: "warning mode missing required becomes warning",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
					"Health":   {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Particle": {
						RequiredComponents:   []string{"Position", "Health"},
						AllowExtraComponents: false,
						ValidationLevel:      schema.ValidationWarning,
					},
				},
			},
			entityType:       "Particle",
			provided:         []string{"Position"},
			wantErrors:       0,
			wantWarnings:     1,
			wantWarningSubstr: `missing required component "Health"`,
		},
		{
			name: "warning mode extra component becomes warning",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
					"Velocity": {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Particle": {
						RequiredComponents:   []string{"Position"},
						AllowExtraComponents: false,
						ValidationLevel:      schema.ValidationWarning,
					},
				},
			},
			entityType:       "Particle",
			provided:         []string{"Position", "Velocity"},
			wantErrors:       0,
			wantWarnings:     1,
			wantWarningSubstr: `component "Velocity" is not allowed`,
		},
		{
			name: "unknown entity type",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Goblin": {RequiredComponents: []string{"Position"}},
				},
			},
			entityType:      "Dragon",
			provided:        []string{"Position"},
			wantErrors:      1,
			wantErrorSubstr: `unknown entity type "Dragon"`,
		},
		{
			name: "undeclared component always hard error",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Goblin": {
						RequiredComponents:   []string{"Position"},
						AllowExtraComponents: true,
						ValidationLevel:      schema.ValidationWarning,
					},
				},
			},
			entityType:      "Goblin",
			provided:        []string{"Position", "FakeComponent"},
			wantErrors:      1,
			wantErrorSubstr: `"FakeComponent" is not declared in schema`,
		},
		{
			name: "empty entity type with no required components",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components:    map[string]schema.Component{},
				EntityTypes: map[string]schema.EntityType{
					"Empty": {
						RequiredComponents:   []string{},
						AllowExtraComponents: true,
						ValidationLevel:      schema.ValidationStrict,
					},
				},
			},
			entityType: "Empty",
			provided:   []string{},
			wantErrors:   0,
			wantWarnings: 0,
		},
		{
			name: "validationLevel defaults to strict",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
					"Health":   {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Goblin": {
						RequiredComponents: []string{"Position", "Health"},
						// ValidationLevel omitted → defaults to strict
					},
				},
			},
			entityType: "Goblin",
			provided:   []string{"Position"},
			wantErrors: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := ValidateEntityCreation(&tt.schema, tt.entityType, tt.provided)

			if len(vr.Errors) != tt.wantErrors {
				t.Errorf("got %d errors, want %d: %v", len(vr.Errors), tt.wantErrors, vr.Errors)
			}
			if tt.wantErrorSubstr != "" {
				found := false
				for _, e := range vr.Errors {
					if e == tt.wantErrorSubstr || strings.Contains(e, tt.wantErrorSubstr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("want error containing %q, got: %v", tt.wantErrorSubstr, vr.Errors)
				}
			}
			if len(vr.Warnings) != tt.wantWarnings {
				t.Errorf("got %d warnings, want %d: %v", len(vr.Warnings), tt.wantWarnings, vr.Warnings)
			}
			if tt.wantWarningSubstr != "" {
				found := false
				for _, w := range vr.Warnings {
					if w == tt.wantWarningSubstr || strings.Contains(w, tt.wantWarningSubstr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("want warning containing %q, got: %v", tt.wantWarningSubstr, vr.Warnings)
				}
			}

			// Also verify Valid() consistency.
			if vr.Valid() != (tt.wantErrors == 0) {
				t.Errorf("Valid()=%v but wantErrors=%d", vr.Valid(), tt.wantErrors)
			}
		})
	}
}


