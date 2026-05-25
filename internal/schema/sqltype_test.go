package schema

import "testing"

func TestPropertySQLType(t *testing.T) {
	tests := []struct {
		name     string
		propType string
		want     string
	}{
		{name: "string", propType: PropertyTypeString, want: "TEXT"},
		{name: "integer", propType: PropertyTypeInteger, want: "INTEGER"},
		{name: "number", propType: PropertyTypeNumber, want: "REAL"},
		{name: "boolean", propType: PropertyTypeBoolean, want: "INTEGER"},
		{name: "entity-ref", propType: PropertyTypeEntityRef, want: "INTEGER"},
		{name: "object", propType: PropertyTypeObject, want: "TEXT"},
		{name: "array", propType: PropertyTypeArray, want: "TEXT"},
		{name: "unknown defaults to TEXT", propType: "unknown", want: "TEXT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Property{Type: tt.propType}
			got := PropertySQLType(p)
			if got != tt.want {
				t.Errorf("PropertySQLType(%s) = %q, want %q", tt.propType, got, tt.want)
			}
		})
	}
}
