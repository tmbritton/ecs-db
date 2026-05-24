package world

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

func TestValidateDetachComponent(t *testing.T) {
	tests := []struct {
		name            string
		schema          schema.DatabaseSchema
		entityType      string
		componentName   string
		wantErrors      int
		wantWarnings    int
		wantErrorSubstr string
	}{
		{
			name: "optional component detach",
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
			entityType:    "Goblin",
			componentName: "Velocity",
			wantErrors:    0,
			wantWarnings:  0,
		},
		{
			name: "required component detach",
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
			entityType:      "Goblin",
			componentName:   "Health",
			wantErrors:      1,
			wantErrorSubstr: `required`,
		},
		{
			name: "required component detach warning mode still errors",
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
						ValidationLevel:      schema.ValidationWarning,
					},
				},
			},
			entityType:      "Goblin",
			componentName:   "Position",
			wantErrors:      1,
			wantErrorSubstr: `required`,
		},
		{
			name: "undeclared component detach",
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
			componentName:   "MagicShield",
			wantErrors:      1,
			wantErrorSubstr: `"MagicShield" is not declared`,
		},
		{
			name: "unknown entity type",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{},
			},
			entityType:      "Dragon",
			componentName:   "Position",
			wantErrors:      1,
			wantErrorSubstr: `unknown entity type`,
		},
		{
			name: "detach extra on strict type is fine",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
					"Velocity": {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Goblin": {
						RequiredComponents:   []string{"Position"},
						AllowExtraComponents: false,
						ValidationLevel:      schema.ValidationStrict,
					},
				},
			},
			entityType:    "Goblin",
			componentName: "Velocity",
			wantErrors:    0,
			wantWarnings:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := ValidateDetachComponent(&tt.schema, tt.entityType, tt.componentName)

			if len(vr.Errors) != tt.wantErrors {
				t.Errorf("Errors count = %d, want %d: %v", len(vr.Errors), tt.wantErrors, vr.Errors)
			}
			if len(vr.Warnings) != tt.wantWarnings {
				t.Errorf("Warnings count = %d, want %d: %v", len(vr.Warnings), tt.wantWarnings, vr.Warnings)
			}
			if tt.wantErrorSubstr != "" {
				found := false
				for _, e := range vr.Errors {
					if strings.Contains(e, tt.wantErrorSubstr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q in %v", tt.wantErrorSubstr, vr.Errors)
				}
			}
		})
	}
}
