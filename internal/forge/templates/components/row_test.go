package components

import "testing"

func TestListRow(t *testing.T) {
	tests := []struct {
		name    string
		props   ListRowProps
		want    []string
		notWant []string
	}{
		{
			name:    "inactive",
			props:   ListRowProps{Label: "Props"},
			want:    []string{"list-row", "Props"},
			notWant: []string{"list-row--active"},
		},
		{
			name:  "active",
			props: ListRowProps{Label: "Terrain", Active: true},
			want:  []string{"list-row--active"},
		},
		{
			name:  "leading glyph and trailing value",
			props: ListRowProps{Label: "Terrain", Glyph: "👁", Value: "128"},
			want:  []string{"list-row__glyph", "👁", "list-row__value", "128"},
		},
		{
			name:    "no glyph, no glyph element",
			props:   ListRowProps{Label: "Terrain"},
			notWant: []string{"list-row__glyph", "list-row__value"},
		},
		{
			name:  "muted tone",
			props: ListRowProps{Label: "hidden", Tone: ToneMuted},
			want:  []string{"list-row--muted"},
		},
		{
			// A query layer backed by a SQL predicate reads red and mono —
			// it is a debug overlay, not an authored layer.
			name:  "danger tone renders mono",
			props: ListRowProps{Label: "hp < 20% · pulse", Tone: ToneDanger, Mono: true},
			want:  []string{"list-row--danger", "mono", "hp &lt; 20%"},
		},
		{
			// A div with a click handler is unreachable by keyboard. Every
			// layer, machine, entity and component list routes through this
			// row, so the defect would multiply across all six modes.
			name:  "activatable rows are real buttons",
			props: ListRowProps{Label: "Terrain", Action: "@get('/forge/map/layer/2')"},
			want:  []string{"<button", `type="button"`, "data-on:click", "@get(&#39;/forge/map/layer/2&#39;)"},
		},
		{
			name:  "selection is announced, not just coloured",
			props: ListRowProps{Label: "Terrain", Active: true, Action: "@get('/x')"},
			want:  []string{`aria-current="true"`},
		},
		{
			name:    "unselected row says so",
			props:   ListRowProps{Label: "Props", Action: "@get('/x')"},
			want:    []string{`aria-current="false"`},
			notWant: []string{`aria-current="true"`},
		},
		{
			// A row with nothing to do is not a control. forge.css keys the
			// pointer cursor and hover fill off `button.list-row`, so the
			// element name is load-bearing, not incidental.
			name:    "inert rows are divs, not buttons",
			props:   ListRowProps{Label: "Terrain"},
			want:    []string{"<div class=\"list-row"},
			notWant: []string{"<button", "aria-current"},
		},
		{
			name:    "no action, no handler attribute",
			props:   ListRowProps{Label: "Terrain"},
			notWant: []string{"data-on"},
		},
		{
			// The two element forms must not drift apart visually.
			name:  "button and div forms share the class list",
			props: ListRowProps{Label: "Terrain", Active: true, Tone: ToneMuted, Action: "@get('/x')"},
			want:  []string{"list-row--active", "list-row--muted"},
		},
		{
			// templ escapes by default; this pins it so a future refactor to
			// raw HTML output is caught rather than shipped.
			name:    "label is escaped",
			props:   ListRowProps{Label: `<script>alert(1)</script>`},
			want:    []string{"&lt;script&gt;"},
			notWant: []string{"<script>alert(1)</script>"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertOutput(t, render(t, ListRow(tt.props)), tt.want, tt.notWant)
		})
	}
}
