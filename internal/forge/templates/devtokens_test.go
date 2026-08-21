package templates

import (
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/tmbritton/ecs-db/internal/forge/web"
)

// The swatch lists and web.Palette are separate copies of the same data. This
// is the test that stops them drifting — --amber-hi was already missing from
// the page while sitting in both tokens.css and the palette.
func TestSwatchGroups_CoverEveryToken(t *testing.T) {
	want := web.Palette

	got := map[string]string{}
	for _, g := range [][]Swatch{Surfaces, Borders, TextTones, Accents} {
		for _, s := range g {
			if prev, dup := got[s.Token]; dup {
				t.Errorf("%s appears twice (%s and %s)", s.Token, prev, s.Hex)
			}
			got[s.Token] = s.Hex
		}
	}

	for token, hex := range want {
		switch g, ok := got[token]; {
		case !ok:
			t.Errorf("%s is in the palette but not rendered on /dev/tokens", token)
		case !strings.EqualFold(g, hex):
			t.Errorf("%s renders as %s, palette says %s", token, g, hex)
		}
	}
	for token := range got {
		if _, ok := want[token]; !ok {
			t.Errorf("/dev/tokens renders %s, which is not in the palette", token)
		}
	}
}

func TestSwatchGroups_HaveMeanings(t *testing.T) {
	for _, g := range [][]Swatch{Surfaces, Borders, TextTones, Accents} {
		for _, s := range g {
			if strings.TrimSpace(s.Meaning) == "" {
				t.Errorf("%s has no stated meaning — the page exists to say what each hue means", s.Token)
			}
		}
	}
}

// swatchStyle returns templ.SafeCSS, which bypasses sanitisation. Anything that
// is not a plain 6-digit hex must produce nothing at all.
func TestSwatchStyle(t *testing.T) {
	tests := []struct {
		name, hex string
		want      templ.SafeCSS
	}{
		{"valid lowercase", "#ffb454", "background:#ffb454"},
		{"valid uppercase", "#FFB454", "background:#FFB454"},
		{"missing hash", "ffb454", ""},
		{"short hex", "#fb4", ""},
		{"eight digit", "#ffb45480", ""},
		{"css injection", "#fff;position:fixed;top:0", ""},
		{"expression", "red;background:url(javascript:alert(1))", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := swatchStyle(tt.hex); got != tt.want {
				t.Errorf("swatchStyle(%q) = %q, want %q", tt.hex, got, tt.want)
			}
		})
	}
}
