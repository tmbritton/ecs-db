package world

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

func TestValidateAttachComponent(t *testing.T) {
	tests := []struct {
		name              string
		schema            schema.DatabaseSchema
		entityType        string
		componentName     string
		alreadyAttached   bool
		wantErrors        int
		wantWarnings      int
		wantErrorSubstr   string
		wantWarningSubstr string
	}{
		{
			name: "valid optional component not attached",
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
			componentName:   "Velocity",
			alreadyAttached: false,
			wantErrors:      0,
			wantWarnings:    0,
		},
		{
			name: "valid required component not attached",
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
			alreadyAttached: false,
			wantErrors:      0,
			wantWarnings:    0,
		},
		{
			name: "undeclared component name",
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
			alreadyAttached: false,
			wantErrors:      1,
			wantErrorSubstr: `"MagicShield" is not declared`,
		},
		{
			name: "disallowed extra component strict",
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
			entityType:      "Goblin",
			componentName:   "Velocity",
			alreadyAttached: false,
			wantErrors:      1,
			wantErrorSubstr: `"Velocity" is not allowed`,
		},
		{
			name: "disallowed extra warning mode",
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
						ValidationLevel:      schema.ValidationWarning,
					},
				},
			},
			entityType:        "Goblin",
			componentName:     "Velocity",
			alreadyAttached:   false,
			wantErrors:        0,
			wantWarnings:      1,
			wantWarningSubstr: `"Velocity" is not allowed`,
		},
		{
			name: "already attached",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Goblin": {
						RequiredComponents:   []string{"Position"},
						AllowExtraComponents: false,
						ValidationLevel:      schema.ValidationStrict,
					},
				},
			},
			entityType:      "Goblin",
			componentName:   "Position",
			alreadyAttached: true,
			wantErrors:      1,
			wantErrorSubstr: `already attached`,
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
			alreadyAttached: false,
			wantErrors:      1,
			wantErrorSubstr: `unknown entity type`,
		},
		{
			name: "allowed extra with allowExtra true",
			schema: schema.DatabaseSchema{
				SchemaVersion: 1,
				Components: map[string]schema.Component{
					"Position": {Type: schema.ComponentTypeObject},
					"Velocity": {Type: schema.ComponentTypeObject},
				},
				EntityTypes: map[string]schema.EntityType{
					"Goblin": {
						RequiredComponents:   []string{"Position"},
						AllowExtraComponents: true,
						ValidationLevel:      schema.ValidationStrict,
					},
				},
			},
			entityType:      "Goblin",
			componentName:   "Velocity",
			alreadyAttached: false,
			wantErrors:      0,
			wantWarnings:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := ValidateAttachComponent(&tt.schema, tt.entityType, tt.componentName, tt.alreadyAttached)

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
			if tt.wantWarningSubstr != "" {
				found := false
				for _, w := range vr.Warnings {
					if strings.Contains(w, tt.wantWarningSubstr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected warning containing %q in %v", tt.wantWarningSubstr, vr.Warnings)
				}
			}
		})
	}
}
