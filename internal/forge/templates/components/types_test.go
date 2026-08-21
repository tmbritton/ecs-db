package components

import (
	"strings"
	"testing"
)

func TestClasses(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"drops empties", []string{"list-row", "", "mono"}, "list-row mono"},
		{"drops whitespace-only", []string{"panel", "   "}, "panel"},
		{"trims", []string{" panel ", "panel--rail"}, "panel panel--rail"},
		{"all empty", []string{"", ""}, ""},
		{"none", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classes(tt.in...); got != tt.want {
				t.Errorf("classes(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWhen(t *testing.T) {
	if got := when(true, "x"); got != "x" {
		t.Errorf("when(true) = %q, want %q", got, "x")
	}
	if got := when(false, "x"); got != "" {
		t.Errorf("when(false) = %q, want empty", got)
	}
}

func TestPxWidth(t *testing.T) {
	tests := []struct {
		px   int
		want string
	}{
		{240, "width:240px"},
		{1, "width:1px"},
		{0, ""},
		{-1, ""},
	}
	for _, tt := range tests {
		if got := string(pxWidth(tt.px)); got != tt.want {
			t.Errorf("pxWidth(%d) = %q, want %q", tt.px, got, tt.want)
		}
	}
}

func TestAccentClass(t *testing.T) {
	tests := []struct {
		accent Accent
		want   string
	}{
		{AccentNone, ""},
		{AccentAmber, "segment--amber"},
		{AccentGreen, "segment--green"},
		{AccentCyan, "segment--cyan"},
		{AccentViolet, "segment--violet"},
		{AccentRed, "segment--red"},
		// Out of range must degrade to the inherited appearance, not emit a
		// class no stylesheet has.
		{Accent(42), ""},
	}
	for _, tt := range tests {
		if got := tt.accent.class("segment"); got != tt.want {
			t.Errorf("Accent(%d).class = %q, want %q", tt.accent, got, tt.want)
		}
	}
}

func TestToneClass(t *testing.T) {
	tests := []struct {
		tone Tone
		want string
	}{
		{ToneDefault, ""},
		{ToneMuted, "list-row--muted"},
		{ToneDanger, "list-row--danger"},
		{Tone(9), ""},
	}
	for _, tt := range tests {
		if got := tt.tone.class("list-row"); got != tt.want {
			t.Errorf("Tone(%d).class = %q, want %q", tt.tone, got, tt.want)
		}
	}
}

// The lens/hue mapping is the design system's central claim — authored is
// files, live is world.sqlite, replay is the transitions log — so it is pinned
// rather than left to the call site.
func TestLensSourceAccent(t *testing.T) {
	tests := []struct {
		lens LensSource
		want Accent
	}{
		{LensAuthored, AccentAmber},
		{LensLive, AccentCyan},
		{LensReplay, AccentViolet},
		{LensSource("nonsense"), AccentAmber},
	}
	for _, tt := range tests {
		if got := tt.lens.Accent(); got != tt.want {
			t.Errorf("%q.Accent() = %d, want %d", tt.lens, got, tt.want)
		}
	}
}

func TestBoolAttr(t *testing.T) {
	if boolAttr(true) != "true" || boolAttr(false) != "false" {
		t.Error("boolAttr must spell both states out; aria-pressed is tri-state")
	}
}

// Every accent modifier a component can emit needs a rule in forge.css. This
// checks the Go half is self-consistent; palette_test.go checks the CSS half.
func TestAccentClasses_AreDistinct(t *testing.T) {
	seen := map[string]Accent{}
	for _, a := range []Accent{AccentAmber, AccentGreen, AccentCyan, AccentViolet, AccentRed} {
		c := a.class("segment")
		if !strings.HasPrefix(c, "segment--") {
			t.Errorf("Accent(%d) produced %q, want a segment-- modifier", a, c)
		}
		if prev, dup := seen[c]; dup {
			t.Errorf("Accent(%d) and Accent(%d) share the class %q", prev, a, c)
		}
		seen[c] = a
	}
}
