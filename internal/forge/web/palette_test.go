package web

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// namedColours are CSS colour keywords. `color: white` is one keystroke and
// would otherwise sail past a hex-only scan. Not the full 148-entry list — the
// common ones plus every keyword close to a palette hue.
var namedColours = map[string]bool{
	"aqua": true, "aquamarine": true, "azure": true, "beige": true, "black": true,
	"blue": true, "brown": true, "chartreuse": true, "chocolate": true, "coral": true,
	"crimson": true, "cyan": true, "darkblue": true, "darkgray": true, "darkgrey": true,
	"darkgreen": true, "darkorange": true, "darkred": true, "dimgray": true, "dimgrey": true,
	"firebrick": true, "fuchsia": true, "gold": true, "goldenrod": true, "gray": true,
	"green": true, "grey": true, "hotpink": true, "indigo": true, "ivory": true,
	"khaki": true, "lavender": true, "lightblue": true, "lightgray": true, "lightgrey": true,
	"lightgreen": true, "lime": true, "magenta": true, "maroon": true, "navy": true,
	"olive": true, "orange": true, "orangered": true, "orchid": true, "pink": true,
	"plum": true, "purple": true, "red": true, "salmon": true, "sienna": true,
	"silver": true, "skyblue": true, "slategray": true, "slategrey": true, "snow": true,
	"tan": true, "teal": true, "tomato": true, "turquoise": true, "violet": true,
	"wheat": true, "white": true, "yellow": true,
}

type finding struct {
	line   int
	text   string
	reason string
}

func (f finding) String() string {
	return fmt.Sprintf("%d: %s (%s)", f.line, f.text, f.reason)
}

// stripComments removes /* ... */ while respecting string literals, so a `/*`
// inside content:"…" cannot swallow the rest of the file. Newlines are kept so
// reported line numbers stay accurate.
func stripComments(css string) string {
	var b strings.Builder
	var quote byte
	inComment := false

	for i := 0; i < len(css); i++ {
		c := css[i]
		switch {
		case inComment:
			if c == '*' && i+1 < len(css) && css[i+1] == '/' {
				inComment = false
				i++
			} else if c == '\n' {
				b.WriteByte('\n')
			}
		case quote != 0:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(css) {
				i++
				b.WriteByte(css[i])
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
			b.WriteByte(c)
		case c == '/' && i+1 < len(css) && css[i+1] == '*':
			inComment = true
			i++
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// declValueRE matches `property: value` up to the terminating ; or }. Scanning
// values rather than whole lines keeps #id selectors out of the results.
var declValueRE = regexp.MustCompile(`(?s)[-a-zA-Z]+\s*:\s*([^;{}]*)`)

var (
	hexRE      = regexp.MustCompile(`#[0-9a-fA-F]+\b`)
	funcRE     = regexp.MustCompile(`\b(rgba?|hsla?|hwb|lab|lch|oklab|oklch|color|color-mix)\s*\(`)
	gradientRE = regexp.MustCompile(`\b[-a-z]*gradient\s*\(`)
	wordRE     = regexp.MustCompile(`[a-zA-Z-]{3,}`)
)

// paletteRGB is the palette as (r,g,b) triples, for validating translucent
// overlays expressed in rgba().
func paletteRGB() map[[3]int]bool {
	out := map[[3]int]bool{
		{0, 0, 0}:       true, // pure black — hard shadows
		{255, 255, 255}: true, // pure white
	}
	for _, hex := range Palette {
		r, _ := strconv.ParseInt(hex[1:3], 16, 0)
		g, _ := strconv.ParseInt(hex[3:5], 16, 0)
		b, _ := strconv.ParseInt(hex[5:7], 16, 0)
		out[[3]int{int(r), int(g), int(b)}] = true
	}
	return out
}

func hexInPalette(h string) bool {
	h = strings.ToLower(h)
	// 8-digit is a palette colour with alpha; compare the RGB half.
	if len(h) == 9 {
		h = h[:7]
	}
	if len(h) != 7 {
		return false
	}
	for _, p := range Palette {
		if strings.EqualFold(p, h) {
			return true
		}
	}
	return false
}

// checkFunc validates a colour function call starting at the given index.
// Only translucent overlays of a palette hue (or pure black/white) are allowed;
// everything else is a new hue by another name.
func checkFunc(name, args string, rgb map[[3]int]bool) (ok bool, reason string) {
	if name != "rgb" && name != "rgba" {
		return false, name + "() is not an allowed colour syntax"
	}
	// Accept both rgba(r,g,b,a) and CSS Color 4 rgb(r g b / a).
	norm := strings.ReplaceAll(args, "/", ",")
	norm = strings.ReplaceAll(norm, "  ", " ")
	fields := strings.FieldsFunc(norm, func(r rune) bool { return r == ',' || r == ' ' })
	if len(fields) < 3 {
		return false, "unparseable colour"
	}
	var triple [3]int
	for i := 0; i < 3; i++ {
		v, err := strconv.Atoi(strings.TrimSpace(fields[i]))
		if err != nil {
			return false, "non-numeric channel " + fields[i]
		}
		triple[i] = v
	}
	if !rgb[triple] {
		return false, "RGB triple is not a palette colour"
	}
	if len(fields) < 4 {
		return false, "opaque — use the token instead"
	}
	alphaStr := strings.TrimSuffix(strings.TrimSpace(fields[3]), "%")
	alpha, err := strconv.ParseFloat(alphaStr, 64)
	if err != nil {
		return false, "unparseable alpha " + fields[3]
	}
	if strings.HasSuffix(strings.TrimSpace(fields[3]), "%") {
		alpha /= 100
	}
	if alpha >= 1 {
		return false, "opaque — use the token instead"
	}
	return true, ""
}

func scanCSS(css string) []finding {
	var out []finding
	rgb := paletteRGB()
	clean := stripComments(css)

	for lineNo, line := range strings.Split(clean, "\n") {
		for _, m := range declValueRE.FindAllStringSubmatch(line, -1) {
			val := m[1]

			if loc := gradientRE.FindString(val); loc != "" {
				out = append(out, finding{line: lineNo + 1, text: loc, reason: "gradients are not part of the design language"})
			}
			for _, h := range hexRE.FindAllString(val, -1) {
				if !hexInPalette(h) {
					out = append(out, finding{line: lineNo + 1, text: h, reason: "not a palette colour"})
				}
			}
			for _, fm := range funcRE.FindAllStringSubmatchIndex(val, -1) {
				name := val[fm[2]:fm[3]]
				rest := val[fm[1]:]
				end := strings.IndexByte(rest, ')')
				if end < 0 {
					end = len(rest)
				}
				if ok, why := checkFunc(name, rest[:end], rgb); !ok {
					out = append(out, finding{line: lineNo + 1, text: name + "(" + rest[:end] + ")", reason: why})
				}
			}
			for _, w := range wordRE.FindAllString(val, -1) {
				if namedColours[strings.ToLower(w)] {
					out = append(out, finding{line: lineNo + 1, text: w, reason: "named colour"})
				}
			}
		}
	}
	return out
}

// cssFiles walks the whole embedded tree, so a stylesheet added outside css/
// or in a subdirectory is still scanned.
func cssFiles(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := fs.WalkDir(Static, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".css" {
			return err
		}
		data, err := fs.ReadFile(Static, p)
		if err != nil {
			return err
		}
		out[p] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walking embedded assets: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("no stylesheets found — the embed is broken and this test would pass vacuously")
	}
	return out
}

func TestStylesheets_UseOnlyPaletteColours(t *testing.T) {
	for name, css := range cssFiles(t) {
		t.Run(name, func(t *testing.T) {
			for _, f := range scanCSS(css) {
				t.Errorf("%s:%s", name, f)
			}
		})
	}
}

// The scanner is the guard for the entire design language, so it gets its own
// tests. Each case here is a leak that a previous version let through.
func TestScanCSS(t *testing.T) {
	tests := []struct {
		name    string
		css     string
		wantHit bool
	}{
		{"palette hex", `a { color: #ffb454; }`, false},
		{"palette hex uppercase", `a { color: #FFB454; }`, false},
		{"stray hex", `a { color: #ff00ff; }`, true},
		{"hex hidden after a quoted /*", `a { content: "/*"; color: #ff00ff; }`, true},
		{"hex in a comment is ignored", `a { /* #ff00ff */ color: #ffb454; }`, false},
		{"id selector is not a colour", `#beef { color: #ffb454; }`, false},
		{"palette hex with alpha", `a { color: #ffb45480; }`, false},
		{"translucent black shadow", `a { box-shadow: 4px 4px 0 rgba(0, 0, 0, 0.25); }`, false},
		{"translucent palette tint", `a { background: rgba(200, 164, 255, 0.12); }`, false},
		{"translucent stray hue", `a { background: rgba(255, 0, 255, 0.5); }`, true},
		{"opaque rgba spelled 1.00", `a { color: rgba(0, 0, 0, 1.00); }`, true},
		{"opaque rgb", `a { color: rgb(1, 2, 3); }`, true},
		{"css color 4 space syntax", `a { color: rgb(0 0 0 / 50%); }`, false},
		{"named colour", `a { color: red; }`, true},
		{"named colour white", `a { background: white; }`, true},
		{"hsl is not allowed", `a { color: hsl(300 100% 50%); }`, true},
		{"gradient of palette colours", `a { background: linear-gradient(#ffb454, #9ece6a); }`, true},
		{"var reference is fine", `a { color: var(--amber); }`, false},
		{"transparent keyword is fine", `a { background: transparent; }`, false},
		{"property names are not colours", `a { border-color: var(--border); }`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanCSS(tt.css)
			if tt.wantHit && len(got) == 0 {
				t.Errorf("expected a finding, got none")
			}
			if !tt.wantHit && len(got) > 0 {
				t.Errorf("expected no findings, got %v", got)
			}
		})
	}
}

func TestStripComments_PreservesLineNumbers(t *testing.T) {
	css := "a{}\n/* line two\n   line three */\nb { color: #ff00ff; }\n"
	got := scanCSS(css)
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %v", got)
	}
	if got[0].line != 4 {
		t.Errorf("finding reported at line %d, want 4", got[0].line)
	}
}

// A palette entry nobody declares is a stale entry.
func TestTokensCSS_DeclaresEveryPaletteEntry(t *testing.T) {
	data, err := fs.ReadFile(Static, "css/tokens.css")
	if err != nil {
		t.Fatalf("reading tokens.css: %v", err)
	}
	css := strings.ToLower(string(data))
	for token, hex := range Palette {
		t.Run(token, func(t *testing.T) {
			if !strings.Contains(css, token+":") {
				t.Errorf("tokens.css does not declare %s", token)
			}
			if !strings.Contains(css, strings.ToLower(hex)) {
				t.Errorf("tokens.css does not contain %s for %s", hex, token)
			}
		})
	}
}

var (
	reducedMotionRE = regexp.MustCompile(`(?s)@media \(prefers-reduced-motion: reduce\) \{(.*?)\n\}`)
	classRE         = regexp.MustCompile(`\.[a-zA-Z][\w-]*`)
	animationRE     = regexp.MustCompile(`animation:\s*([a-zA-Z][\w-]*)`)
)

// The reduced-motion block originally selected [class*='fdash'] — but fdash is
// a keyframes name, not a class, so it matched nothing and the marching ants
// kept animating. Every selector in that block must name a class that exists.
func TestReducedMotion_SelectorsMatchRealClasses(t *testing.T) {
	css := cssFiles(t)["css/forge.css"]
	m := reducedMotionRE.FindStringSubmatch(css)
	if m == nil {
		t.Fatal("no prefers-reduced-motion block in forge.css")
	}
	outside := strings.Replace(css, m[0], "", 1)

	selectors := classRE.FindAllString(m[1], -1)
	if len(selectors) == 0 {
		t.Fatal("reduced-motion block selects no classes")
	}
	for _, sel := range selectors {
		t.Run(sel, func(t *testing.T) {
			if !strings.Contains(outside, sel) {
				t.Errorf("%s is selected under reduced-motion but defined nowhere else", sel)
			}
		})
	}
}

// Every animated element must be covered by the reduced-motion block. Adding an
// animation without disabling it is the same bug from the other direction.
func TestReducedMotion_CoversEveryAnimatedClass(t *testing.T) {
	css := cssFiles(t)["css/forge.css"]
	block := reducedMotionRE.FindStringSubmatch(css)
	if block == nil {
		t.Fatal("no prefers-reduced-motion block in forge.css")
	}
	covered := map[string]bool{}
	for _, sel := range classRE.FindAllString(block[1], -1) {
		covered[sel] = true
	}

	// Find rules that set a named animation, and the class they belong to.
	for _, rule := range strings.Split(stripComments(css), "}") {
		if !animationRE.MatchString(rule) || strings.Contains(rule, "prefers-reduced-motion") {
			continue
		}
		head, _, ok := strings.Cut(rule, "{")
		if !ok {
			continue
		}
		for _, sel := range classRE.FindAllString(head, -1) {
			if !covered[sel] {
				t.Errorf("%s animates but is not disabled under prefers-reduced-motion", sel)
			}
		}
	}
}

// The list a native <select> opens is painted by the browser, not by the page:
// it takes the control's colour and its own white ground, so the catalogue
// rendered as pale cream on white and could not be read. Nothing about the
// closed control shows it, and no screenshot of the page catches it either —
// the popup only exists while it is open.
//
// Pinned because it is a one-line rule that looks like tidying and is not.
func TestStylesheets_PaintTheNativeSelectPopup(t *testing.T) {
	// color-scheme is what makes the browser paint its own widgets dark, and it
	// is the half that works everywhere: an option rule is honoured by some
	// engines and ignored by others.
	if !strings.Contains(stripComments(cssFiles(t)["css/tokens.css"]), "color-scheme: dark") {
		t.Error("nothing tells the browser to paint its own widgets dark, so a select's list opens white")
	}

	body := stripComments(cssFiles(t)["css/forge.css"])
	i := strings.Index(body, "option,")
	if i < 0 {
		i = strings.Index(body, "option {")
	}
	if i < 0 {
		t.Fatal("nothing styles option, so a select's list is left to the browser alone")
	}
	rule := body[i:]
	if j := strings.Index(rule, "}"); j >= 0 {
		rule = rule[:j]
	}
	for _, want := range []string{"background:", "color:"} {
		if !strings.Contains(rule, want) {
			t.Errorf("the option rule declares no %s, so half the popup stays browser-default: %s", want, rule)
		}
	}
}
