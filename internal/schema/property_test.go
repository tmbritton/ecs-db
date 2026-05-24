package schema

import "testing"

func TestIsSupportedPropertyType(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"string", true},
		{"integer", true},
		{"number", true},
		{"boolean", true},
		{"object", true},
		{"array", true},
		{"entity-ref", true},
		{"text", false},
		{"datetime", false},
		{"email", false},
		{"url", false},
		{"reference", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsSupportedPropertyType(tt.input); got != tt.want {
				t.Errorf("IsSupportedPropertyType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestPropertyValidate(t *testing.T) {
	tests := []struct {
		name    string
		prop    Property
		wantErr bool
	}{
		{
			name:    "empty type",
			prop:    Property{Type: ""},
			wantErr: true,
		},
		{
			name:    "unknown type",
			prop:    Property{Type: "datetime"},
			wantErr: true,
		},
		{
			name: "valid string",
			prop: Property{Type: PropertyTypeString},
		},
		{
			name: "valid integer",
			prop: Property{Type: PropertyTypeInteger},
		},
		{
			name: "valid number",
			prop: Property{Type: PropertyTypeNumber},
		},
		{
			name: "valid boolean",
			prop: Property{Type: PropertyTypeBoolean},
		},
		{
			name: "valid entity-ref",
			prop: Property{Type: PropertyTypeEntityRef},
		},
		{
			name: "valid object with children",
			prop: Property{
				Type: PropertyTypeObject,
				Properties: map[string]Property{
					"x": {Type: PropertyTypeNumber},
					"y": {Type: PropertyTypeNumber},
				},
			},
		},
		{
			name:    "object without properties",
			prop:    Property{Type: PropertyTypeObject},
			wantErr: true,
		},
		{
			name: "object with invalid child",
			prop: Property{
				Type: PropertyTypeObject,
				Properties: map[string]Property{
					"bad": {Type: "datetime"},
				},
			},
			wantErr: true,
		},
		{
			name: "valid array with items",
			prop: Property{
				Type:  PropertyTypeArray,
				Items: &Property{Type: PropertyTypeEntityRef},
			},
		},
		{
			name:    "array without items",
			prop:    Property{Type: PropertyTypeArray},
			wantErr: true,
		},
		{
			name: "array with invalid items type",
			prop: Property{
				Type:  PropertyTypeArray,
				Items: &Property{Type: "unknown"},
			},
			wantErr: true,
		},
		{
			name: "deeply nested object",
			prop: Property{
				Type: PropertyTypeObject,
				Properties: map[string]Property{
					"pos": {
						Type: PropertyTypeObject,
						Properties: map[string]Property{
							"x": {Type: PropertyTypeNumber},
						},
					},
				},
			},
		},
		{
			name: "deeply nested object with invalid leaf",
			prop: Property{
				Type: PropertyTypeObject,
				Properties: map[string]Property{
					"pos": {
						Type: PropertyTypeObject,
						Properties: map[string]Property{
							"x": {Type: "url"},
						},
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.prop.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Property.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
