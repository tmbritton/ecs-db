package components

import "testing"

func TestSectionHeading(t *testing.T) {
	tests := []struct {
		name    string
		props   SectionHeadingProps
		want    []string
		notWant []string
	}{
		{
			name:    "label only",
			props:   SectionHeadingProps{Label: "LAYERS"},
			want:    []string{`class="section-heading"`, "LAYERS"},
			notWant: []string{"section-heading__source"},
		},
		{
			name:  "with live source suffix",
			props: SectionHeadingProps{Label: "ENTITIES", Source: "◂ world.sqlite"},
			want:  []string{`class="section-heading__source"`, "◂ world.sqlite"},
		},
		{
			name:  "label is escaped",
			props: SectionHeadingProps{Label: `<script>alert(1)</script>`},
			want:  []string{"&lt;script&gt;"},
			// The literal tag must never reach the document.
			notWant: []string{"<script>alert(1)</script>"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertOutput(t, render(t, SectionHeading(tt.props)), tt.want, tt.notWant)
		})
	}
}

func TestPanel(t *testing.T) {
	tests := []struct {
		name    string
		props   PanelProps
		want    []string
		notWant []string
	}{
		{
			name:    "no heading",
			props:   PanelProps{},
			want:    []string{"panel", "panel__body", "body content"},
			notWant: []string{"section-heading"},
		},
		{
			name:  "with heading",
			props: PanelProps{Heading: &SectionHeadingProps{Label: "TILESETS"}},
			want:  []string{"section-heading", "TILESETS"},
		},
		{
			name:    "zero width emits no style attribute",
			props:   PanelProps{},
			notWant: []string{"style"},
		},
		{
			name:  "explicit width",
			props: PanelProps{Width: 240},
			want:  []string{"width:240px"},
		},
		{
			// Width feeds an inline style, which templ.SafeCSS does not
			// sanitise — so the type must refuse anything but a plain px value.
			name:    "negative width is refused",
			props:   PanelProps{Width: -1},
			notWant: []string{"style"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertOutput(t, renderChildren(t, Panel(tt.props), "body content"), tt.want, tt.notWant)
		})
	}
}
