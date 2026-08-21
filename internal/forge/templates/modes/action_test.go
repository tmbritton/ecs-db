package modes

import (
	"net/url"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// action, valueAction, urlValue, jsString and componentHref build URLs and
// JavaScript by hand, from names the user types. None of them had a test:
// replacing urlValue with the identity, dropping encodeURIComponent, always
// emitting "?" or always "&", and swapping jsString for naive quoting all
// passed the whole suite.

func TestAction(t *testing.T) {
	tests := []struct {
		name string
		path string
		args []string
		want string
	}{
		{name: "no arguments", path: "/x", want: "@post('/x')"},
		{name: "one pair", path: "/x", args: []string{"a", "1"}, want: "@post('/x?a=1')"},
		{
			name: "two pairs use ? then &",
			path: "/x",
			args: []string{"a", "1", "b", "2"},
			want: "@post('/x?a=1&b=2')",
		},
		{
			name: "values are escaped",
			path: "/x",
			args: []string{"rename", "My Component"},
			want: "@post('/x?rename=My%20Component')",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := action(tt.path, tt.args...); got != tt.want {
				t.Errorf("action = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValueAction(t *testing.T) {
	t.Run("appends with ? when there are no other arguments", func(t *testing.T) {
		got := valueAction("/x", "to")
		want := "@post('/x?to=' + encodeURIComponent(evt.target.value))"
		if got != want {
			t.Errorf("valueAction = %q, want %q", got, want)
		}
	})

	t.Run("appends with & when there are", func(t *testing.T) {
		got := valueAction("/x", "to", "rename", "A")
		want := "@post('/x?rename=A&to=' + encodeURIComponent(evt.target.value))"
		if got != want {
			t.Errorf("valueAction = %q, want %q", got, want)
		}
	})

	// The value has to be escaped in the browser too, or a field named "a&b"
	// becomes two parameters.
	t.Run("escapes the value at runtime", func(t *testing.T) {
		if !strings.Contains(valueAction("/x", "to"), "encodeURIComponent(evt.target.value)") {
			t.Error("the control's value is spliced in unescaped")
		}
	})
}

// A component name may contain anything a JSON key may, and it is spliced into
// a single-quoted JavaScript string inside an HTML attribute. A raw quote or
// parenthesis would end the expression early.
func TestUrlValue_EscapesEverythingDangerous(t *testing.T) {
	awkward := []string{
		`quote'single`, `quote"double`, `back\slash`, `paren)close`,
		`amp&and`, `hash#frag`, `question?q`, `percent%41`, `space here`,
		"new\nline", "tab\there", "unicode·é", `<angle>`, `equals=sign`,
	}
	for _, name := range awkward {
		t.Run(name, func(t *testing.T) {
			got := urlValue(name)

			for _, forbidden := range []string{"'", `"`, "\\", ")", "&", "#", "?", "=", " ", "\n", "\t", "<", ">"} {
				if strings.Contains(got, forbidden) {
					t.Errorf("urlValue(%q) = %q still contains %q", name, got, forbidden)
				}
			}
			// And it must decode back to what went in, or the server acts on a
			// different name than the user typed.
			back, err := url.QueryUnescape(got)
			if err != nil {
				t.Fatalf("urlValue(%q) = %q does not decode: %v", name, got, err)
			}
			if back != name {
				t.Errorf("urlValue(%q) decodes to %q", name, back)
			}
		})
	}
}

// The confirmation text is built from a user-supplied name and evaluated as
// JavaScript.
func TestJSString_CannotBreakOut(t *testing.T) {
	for _, in := range []string{
		`plain`, `it's`, `say "hi"`, `back\slash`, "new\nline",
		`</script>`, " line separator", `${injected}`,
	} {
		t.Run(in, func(t *testing.T) {
			got := jsString(in)
			if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
				t.Fatalf("jsString(%q) = %q is not a quoted string", in, got)
			}
			// A bare quote or backslash inside would end the literal early.
			inner := got[1 : len(got)-1]
			for i := 0; i < len(inner); i++ {
				if inner[i] == '\\' {
					i++ // escaped pair, skip both
					continue
				}
				if inner[i] == '"' {
					t.Errorf("jsString(%q) = %q has an unescaped quote", in, got)
				}
			}
			if strings.ContainsAny(inner, "\n\r") {
				t.Errorf("jsString(%q) = %q contains a raw newline", in, got)
			}
		})
	}
}

func TestConfirmAction_GuardsTheAction(t *testing.T) {
	got := confirmAction(`Delete "X"?`, "@post('/x')")
	if !strings.HasPrefix(got, "confirm(") {
		t.Errorf("confirmAction = %q, does not confirm first", got)
	}
	if !strings.Contains(got, "&& @post('/x')") {
		t.Errorf("confirmAction = %q, the action is not guarded by the answer", got)
	}
	if strings.Contains(got, `("Delete "X"?")`) {
		t.Errorf("confirmAction = %q, the question is not escaped", got)
	}
}

func TestComponentHref_EscapesTheName(t *testing.T) {
	got := componentHref(`Odd Name&x`)
	if strings.ContainsAny(strings.TrimPrefix(got, "/forge/schema?component="), " &") {
		t.Errorf("componentHref = %q, the name is not escaped", got)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("componentHref = %q is not a valid URL: %v", got, err)
	}
	if u.Query().Get("component") != `Odd Name&x` {
		t.Errorf("componentHref = %q decodes to %q", got, u.Query().Get("component"))
	}
}

func TestSelectComponent(t *testing.T) {
	data := modeFixture()
	tests := []struct {
		name, want string
	}{
		{name: "Health", want: "Health"},
		{name: "", want: "Position"},
		{name: "Ghost", want: "Position"},
	}
	for _, tt := range tests {
		if got := selectComponent(data.Schema, tt.name); got != tt.want {
			t.Errorf("selectComponent(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}

	// An empty schema has nothing to select, and must not panic reaching for it.
	if got := selectComponent(emptySchema(), "anything"); got != "" {
		t.Errorf("selectComponent on an empty schema = %q, want empty", got)
	}
}

func emptySchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{SchemaVersion: 1}
}
