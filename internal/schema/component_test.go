package schema

import (
	"encoding/json"
	"testing"
)

func TestComponentUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
		// Extra checks after unmarshalling
		check func(*testing.T, *Component)
	}{
		{
			name: "valid object component",
			json: `{
				"type": "object",
				"properties": {
					"x": {"type": "number"},
					"y": {"type": "number"}
				}
			}`,
			wantErr: false,
			check: func(t *testing.T, c *Component) {
				if c.Type != ComponentTypeObject {
					t.Errorf("Type = %q, want %q", c.Type, ComponentTypeObject)
				}
				if len(c.Properties) != 2 {
					t.Errorf("Properties count = %d, want 2", len(c.Properties))
				}
			},
		},
		{
			name: "valid array component with entity-ref items",
			json: `{
				"type": "array",
				"items": {"type": "entity-ref"}
			}`,
			wantErr: false,
			check: func(t *testing.T, c *Component) {
				if c.Type != ComponentTypeArray {
					t.Errorf("Type = %q, want %q", c.Type, ComponentTypeArray)
				}
				if c.Items == nil || c.Items.Type != PropertyTypeEntityRef {
					t.Errorf("Items type = %v, want entity-ref", c.Items)
				}
			},
		},
		{
			name: "valid entity-ref component",
			json: `{"type": "entity-ref"}`,
			wantErr: false,
			check: func(t *testing.T, c *Component) {
				if c.Type != ComponentTypeEntityRef {
					t.Errorf("Type = %q, want %q", c.Type, ComponentTypeEntityRef)
				}
			},
		},
		{
			name: "valid string component",
			json: `{"type": "string"}`,
			wantErr: false,
			check: func(t *testing.T, c *Component) {
				if c.Type != ComponentTypeString {
					t.Errorf("Type = %q, want %q", c.Type, ComponentTypeString)
				}
			},
		},
		{
			name: "valid integer component",
			json: `{"type": "integer"}`,
			wantErr: false,
		},
		{
			name: "valid number component",
			json: `{"type": "number"}`,
			wantErr: false,
		},
		{
			name: "valid boolean component",
			json: `{"type": "boolean"}`,
			wantErr: false,
		},
		{
			name:    "missing type field",
			json:    `{"properties": {}}`,
			wantErr: true,
		},
		{
			name:    "unknown component type",
			json:    `{"type": "datetime"}`,
			wantErr: true,
		},
		{
			name:    "old text type",
			json:    `{"type": "text"}`,
			wantErr: true,
		},
		{
			name:    "old reference type",
			json:    `{"type": "reference"}`,
			wantErr: true,
		},
		{
			name: "object without properties",
			json: `{"type": "object"}`,
			wantErr: true,
		},
		{
			name: "object with empty properties",
			json: `{"type": "object", "properties": {}}`,
			wantErr: true,
		},
		{
			name: "object with invalid child property",
			json: `{
				"type": "object",
				"properties": {
					"bad": {"type": "url"}
				}
			}`,
			wantErr: true,
		},
		{
			name:    "array without items",
			json:    `{"type": "array"}`,
			wantErr: true,
		},
		{
			name: "array with invalid items type",
			json: `{"type": "array", "items": {"type": "email"}}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c Component
			err := json.Unmarshal([]byte(tt.json), &c)
			if (err != nil) != tt.wantErr {
				t.Errorf("Component.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, &c)
			}
		})
	}
}


