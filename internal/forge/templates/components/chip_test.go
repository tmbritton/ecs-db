package components

import "testing"

func TestChip(t *testing.T) {
	tests := []struct {
		name    string
		props   ChipProps
		want    []string
		notWant []string
	}{
		{
			// The lock is not decoration: it is the schema contract saying the
			// entity type requires this component.
			name:    "required is locked, not removable",
			props:   ChipProps{Name: "Position", Kind: ChipRequired},
			want:    []string{"🔒", "Position", "chip__lock"},
			notWant: []string{"✕", "chip__remove"},
		},
		{
			name:    "optional is detachable",
			props:   ChipProps{Name: "Velocity", Kind: ChipOptional},
			want:    []string{"✕", "chip__remove"},
			notWant: []string{"🔒", "chip__lock"},
		},
		{
			name:    "required ignores a remove action",
			props:   ChipProps{Name: "Position", Kind: ChipRequired, RemoveAction: "@post('/x')"},
			notWant: []string{"data-on", "chip__remove"},
		},
		{
			name:  "optional carries its remove action",
			props: ChipProps{Name: "Velocity", Kind: ChipOptional, RemoveAction: "@post('/forge/ents/detach')"},
			want:  []string{"data-on:click", "@post(&#39;/forge/ents/detach&#39;)"},
		},
		{
			name:  "context-seeded badge",
			props: ChipProps{Name: "hp", Kind: ChipOptional, ContextSeeded: true},
			want:  []string{"badge-ctx", "ƒ ctx"},
		},
		{
			name:    "not context-seeded",
			props:   ChipProps{Name: "hp", Kind: ChipOptional},
			notWant: []string{"badge-ctx"},
		},
		{
			// The zero ChipKind is Required. Nothing forces a caller to set
			// Kind, so which default they get is a design decision and is
			// pinned here: a lock withholds an affordance, where the other
			// default would offer a detach the schema forbids.
			name:    "zero value is required",
			props:   ChipProps{Name: "Position"},
			want:    []string{"🔒"},
			notWant: []string{"chip__remove"},
		},
		{
			name:  "remove control is named, not announced as a glyph",
			props: ChipProps{Name: "Velocity", Kind: ChipOptional},
			want:  []string{`aria-label="detach component"`, `aria-hidden="true"`},
		},
		{
			name:    "name is escaped",
			props:   ChipProps{Name: `<img src=x onerror=alert(1)>`},
			want:    []string{"&lt;img"},
			notWant: []string{"<img src=x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertOutput(t, render(t, Chip(tt.props)), tt.want, tt.notWant)
		})
	}
}
