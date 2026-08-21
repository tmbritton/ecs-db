package mode

import "testing"

func TestLookup(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantOK  bool
		wantCap string
	}{
		{name: "first mode", slug: "map", wantOK: true, wantCap: "MAP"},
		{name: "last mode", slug: "sprites", wantOK: true, wantCap: "SPRT"},
		{name: "middle mode", slug: "agents", wantOK: true, wantCap: "AGENTS"},
		{name: "unknown slug", slug: "nope", wantOK: false},
		{name: "empty slug", slug: "", wantOK: false},
		// The rail captions are display text, not identifiers. Looking one up
		// must miss, or a URL like /forge/SPRT would resolve.
		{name: "caption is not a slug", slug: "SPRT", wantOK: false},
		{name: "settings is not a mode", slug: "settings", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Lookup(tt.slug)
			if ok != tt.wantOK {
				t.Fatalf("Lookup(%q) ok = %v, want %v", tt.slug, ok, tt.wantOK)
			}
			if !tt.wantOK {
				if got != (Mode{}) {
					t.Errorf("Lookup(%q) = %+v on miss, want the zero Mode", tt.slug, got)
				}
				return
			}
			if got.Caption != tt.wantCap {
				t.Errorf("Lookup(%q).Caption = %q, want %q", tt.slug, got.Caption, tt.wantCap)
			}
			if got.Slug != tt.slug {
				t.Errorf("Lookup(%q).Slug = %q, want it to round-trip", tt.slug, got.Slug)
			}
		})
	}
}

// All drives the rail, the routes and every test. A duplicate or blank field
// would produce a rail button that navigates to the wrong mode, or a route
// that shadows another — both silent.
func TestAll_IsAWellFormedTable(t *testing.T) {
	if len(All) != 6 {
		t.Fatalf("len(All) = %d, want the 6 modes the design specifies", len(All))
	}
	seenSlug := map[string]bool{}
	seenCaption := map[string]bool{}
	for i, m := range All {
		if m.Slug == "" || m.Caption == "" || m.Glyph == "" || m.Title == "" {
			t.Errorf("All[%d] = %+v has an empty field", i, m)
		}
		if seenSlug[m.Slug] {
			t.Errorf("All[%d]: duplicate slug %q", i, m.Slug)
		}
		if seenCaption[m.Caption] {
			t.Errorf("All[%d]: duplicate caption %q", i, m.Caption)
		}
		seenSlug[m.Slug] = true
		seenCaption[m.Caption] = true
	}
}

// Slugs are URL segments. Anything needing escaping would make the href the
// rail renders differ from the path the router matches.
func TestAll_SlugsAreURLSafe(t *testing.T) {
	for _, m := range All {
		for _, r := range m.Slug {
			if (r < 'a' || r > 'z') && r != '-' {
				t.Errorf("slug %q contains %q; slugs must be lowercase ASCII or '-'", m.Slug, r)
			}
		}
	}
}

// "/" redirects to Default. If it ever named a mode that is not in the table,
// the redirect would land on a 404.
func TestDefault_IsAMember(t *testing.T) {
	got, ok := Lookup(Default.Slug)
	if !ok {
		t.Fatalf("Default %+v is not in All", Default)
	}
	if got != Default {
		t.Errorf("Lookup(Default.Slug) = %+v, want %+v", got, Default)
	}
}

// Path builds every rail href and every route the server registers. The two
// have to agree exactly, so the shape is pinned here rather than inferred from
// whichever caller happens to be under test.
func TestPath(t *testing.T) {
	for _, m := range All {
		t.Run(m.Slug, func(t *testing.T) {
			want := "/forge/" + m.Slug
			if got := m.Path(); got != want {
				t.Errorf("Path() = %q, want %q", got, want)
			}
		})
	}
}
