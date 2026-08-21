// Package mode enumerates Forge's six top-level editors.
//
// One table drives everything: the rail buttons, the route set, the page
// titles and the tests. There is no second list to keep in sync, so a mode
// cannot exist in the navigation without also being routable, or the reverse.
package mode

// Mode is one of Forge's top-level editors. The zero value is not a mode;
// obtain one from All or Lookup.
type Mode struct {
	Slug    string // URL segment under /forge/ and the stable identifier
	Caption string // 10px mono caption under the rail glyph
	Glyph   string // rail icon
	Title   string // document-title suffix
}

// All is the rail order, top to bottom.
var All = []Mode{
	{Slug: "map", Caption: "MAP", Glyph: "🗺", Title: "Map"},
	{Slug: "tiles", Caption: "TILES", Glyph: "▦", Title: "Tiles"},
	{Slug: "ents", Caption: "ENTS", Glyph: "♟", Title: "Entity Types"},
	{Slug: "schema", Caption: "SCHEMA", Glyph: "⛃", Title: "Schema"},
	{Slug: "agents", Caption: "AGENTS", Glyph: "◉→◉", Title: "Agents"},
	{Slug: "sprites", Caption: "SPRT", Glyph: "🧍", Title: "Sprites"},
}

// Default is the mode "/" redirects to.
var Default = All[0]

// Lookup resolves a URL segment to its mode. The second result is false for
// anything not in All, which is what makes an unknown /forge/{mode} a 404.
func Lookup(slug string) (Mode, bool) {
	for _, m := range All {
		if m.Slug == slug {
			return m, true
		}
	}
	return Mode{}, false
}

// Path is the route a rail button links to.
func (m Mode) Path() string { return "/forge/" + m.Slug }
