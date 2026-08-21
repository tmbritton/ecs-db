package templates

import (
	"bytes"
	"context"
	"regexp"
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

// The gallery is the page a visual change is reviewed against, so a primitive
// missing from it is invisible until a mode ships it wrong. This renders the
// whole page and checks each primitive appears — and that both states of the
// ones whose two states carry different meaning are present.
func TestDevTokens_RendersEveryPrimitive(t *testing.T) {
	var buf bytes.Buffer
	if err := DevTokens(Surfaces, Borders, TextTones, Accents).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	// Each marker is chosen to be unique to its primitive. "✕" would not do:
	// it is drawn by the optional chip, an icon button and every modal close,
	// so a missing chip would still find it.
	for _, want := range []string{
		// one marker per primitive
		"class=\"panel\"", "section-heading__source", "list-row__label", "chip__name",
		"role=\"radiogroup\"", "segment__input", "dropdown__caret", `class="checkbox__box"`, "class=\"icon-btn",
		"ctx-menu__item", "modal-backdrop", "save-footer__actions",
		// states that mean different things
		"list-row--active", "list-row--danger",
		// both ListRow element forms: the activatable button and the inert
		// div, which forge.css deliberately styles differently
		`aria-current="true"`, `<div class="list-row`,
		"chip__lock", "chip__remove", "badge-ctx",
		"checkbox__box--on", "checkbox--on",
		"save-footer--dirty", "✓ saved", "● unsaved",
		"ctx-menu__item--danger", "ctx-menu__divider", "ctx-menu__submenu",
		// every lens accent, so all three are exercised on one page
		"segment--amber", "segment--cyan", "segment--violet",
		// every button variant
		"btn--primary", "btn--outline", "btn--ghost", "btn--dashed", "btn--danger",
		// the clean save footer, whose disabled state carries meaning
		"save-footer__save",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("gallery is missing %q", want)
		}
	}

	// A disabled control has to appear somewhere, since disabled is a state a
	// reviewer needs to see. Matched by pattern because the attribute follows
	// the button's accessible name, which is sample copy.
	if !regexp.MustCompile(`<button[^>]*icon-btn[^>]*disabled`).MatchString(out) {
		t.Error("gallery shows no disabled icon button")
	}
}
