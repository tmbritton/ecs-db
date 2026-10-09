package templates

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"path/filepath"
	"strings"
	"testing"
)

func TestTilesetFooter_QuotesNamesInsideJavaScriptConfirmations(t *testing.T) {
	for _, name := range []string{"O'Reilly.tsx", `x');globalThis.exposed=1;--.tsx`, "line\nbreak.tsx"} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			path := filepath.Join("tiles", name)
			if err := TilesetFooter(path, name, true, Elsewhere{}).Render(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			markup := html.UnescapeString(out.String())
			for _, message := range []string{
				"Reload " + name + " from disk? Unsaved changes will be lost.",
				"Discard unsaved changes to " + name + "?",
			} {
				quoted, err := json.Marshal(message)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(markup, "confirm("+string(quoted)+")") {
					t.Errorf("confirmation %q is not a safe JavaScript string in %s", message, markup)
				}
			}
		})
	}
}
