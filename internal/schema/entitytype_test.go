package schema

import "testing"

func TestEntityTypeApplyDefaults(t *testing.T) {
	tests := []struct {
		name      string
		input     EntityType
		wantLevel ValidationLevel
	}{
		{
			name:      "empty level defaults to strict",
			input:     EntityType{},
			wantLevel: ValidationStrict,
		},
		{
			name:      "explicit strict stays strict",
			input:     EntityType{ValidationLevel: ValidationStrict},
			wantLevel: ValidationStrict,
		},
		{
			name:      "explicit warning stays warning",
			input:     EntityType{ValidationLevel: ValidationWarning},
			wantLevel: ValidationWarning,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.ApplyDefaults()
			if tt.input.ValidationLevel != tt.wantLevel {
				t.Errorf("ValidationLevel = %q, want %q",
					tt.input.ValidationLevel, tt.wantLevel)
			}
		})
	}
}

func TestEntityTypeIsComponentRequired(t *testing.T) {
	et := EntityType{RequiredComponents: []string{"Position", "Health"}}
	tests := []struct {
		name      string
		component string
		want      bool
	}{
		{"Position is required", "Position", true},
		{"Health is required", "Health", true},
		{"Sprite not required", "Sprite", false},
		{"empty not required", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := et.IsComponentRequired(tt.component); got != tt.want {
				t.Errorf("IsComponentRequired(%q) = %v, want %v", tt.component, got, tt.want)
			}
		})
	}
}

func TestEntityTypeIsComponentOptional(t *testing.T) {
	et := EntityType{OptionalComponents: []string{"Velocity", "Behavior"}}
	tests := []struct {
		name      string
		component string
		want      bool
	}{
		{"Velocity is optional", "Velocity", true},
		{"Behavior is optional", "Behavior", true},
		{"Position not optional", "Position", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := et.IsComponentOptional(tt.component); got != tt.want {
				t.Errorf("IsComponentOptional(%q) = %v, want %v", tt.component, got, tt.want)
			}
		})
	}
}

func TestEntityTypeIsComponentAllowed(t *testing.T) {
	tests := []struct {
		name      string
		et        EntityType
		component string
		want      bool
	}{
		{
			name:      "allowed via required",
			et:        EntityType{RequiredComponents: []string{"Position"}},
			component: "Position",
			want:      true,
		},
		{
			name:      "allowed via optional",
			et:        EntityType{OptionalComponents: []string{"Velocity"}},
			component: "Velocity",
			want:      true,
		},
		{
			name:      "allowed via extra components",
			et:        EntityType{AllowExtraComponents: true},
			component: "Anything",
			want:      true,
		},
		{
			name: "not allowed when strict and not listed",
			et: EntityType{
				RequiredComponents:   []string{"Position"},
				AllowExtraComponents: false,
			},
			component: "Velocity",
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.et.IsComponentAllowed(tt.component); got != tt.want {
				t.Errorf("IsComponentAllowed(%q) = %v, want %v", tt.component, got, tt.want)
			}
		})
	}
}
