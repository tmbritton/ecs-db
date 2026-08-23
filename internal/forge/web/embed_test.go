package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// Forge must work with no network. These tests are the regression guard for
// that: they fail if a //go:embed directive breaks, an asset is deleted, or
// fonts.css references a file that isn't there.
func TestStatic_RequiredAssetsArePresent(t *testing.T) {
	required := []string{
		"js/vendor/datastar.js",
		"js/vendor/VERSION",
		"js/canvas.js",
		"js/autofocus.js",
		"css/fonts.css",
		"css/forge.css",
		"fonts/chakra-petch-400.woff2",
		"fonts/chakra-petch-500.woff2",
		"fonts/chakra-petch-600.woff2",
		"fonts/jetbrains-mono-400.woff2",
		"fonts/jetbrains-mono-500.woff2",
	}
	for _, name := range required {
		t.Run(name, func(t *testing.T) {
			info, err := fs.Stat(Static, name)
			if err != nil {
				t.Fatalf("fs.Stat(%q): %v", name, err)
			}
			if info.Size() == 0 {
				t.Errorf("%q is empty", name)
			}
		})
	}
}

var fontSrcRE = regexp.MustCompile(`url\('([^']+)'\)`)

// fonts.css and the vendored files must not drift apart. A missing woff2 shows
// up as a silent fallback to a system font, which is easy to miss by eye.
func TestFontsCSS_ReferencesOnlyEmbeddedFiles(t *testing.T) {
	data, err := fs.ReadFile(Static, "css/fonts.css")
	if err != nil {
		t.Fatalf("reading fonts.css: %v", err)
	}
	matches := fontSrcRE.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatal("fonts.css declares no font sources")
	}
	for _, m := range matches {
		ref := m[1]
		t.Run(ref, func(t *testing.T) {
			// Served under /static/, stored at the FS root.
			name := strings.TrimPrefix(ref, "/static/")
			if name == ref {
				t.Fatalf("font src %q is not served from /static/", ref)
			}
			if _, err := fs.Stat(Static, name); err != nil {
				t.Errorf("fonts.css references %q, which is not embedded: %v", ref, err)
			}
		})
	}
}

// The whole point of self-hosting is that nothing is fetched at runtime.
func TestStylesheets_MakeNoExternalRequests(t *testing.T) {
	entries, err := fs.ReadDir(Static, "css")
	if err != nil {
		t.Fatalf("reading css dir: %v", err)
	}
	for _, e := range entries {
		t.Run(e.Name(), func(t *testing.T) {
			data, err := fs.ReadFile(Static, "css/"+e.Name())
			if err != nil {
				t.Fatalf("reading %s: %v", e.Name(), err)
			}
			for _, line := range strings.Split(string(data), "\n") {
				code := line
				if i := strings.Index(code, "*"); i >= 0 {
					code = code[:i] // ignore comments, which cite the source URL
				}
				if strings.Contains(code, "http://") || strings.Contains(code, "https://") {
					t.Errorf("external reference in %s: %s", e.Name(), strings.TrimSpace(line))
				}
			}
		})
	}
}
