package web

// Palette is the complete set of colours Forge is allowed to use, mirroring
// tokens.css. The design system is explicit that each hue means exactly one
// thing and that nothing outside this set may be introduced: "If something
// needs emphasis, reach for weight, border, or the active-row fill — not a new
// color." Adding an entry here is a design decision, not a code change.
var Palette = map[string]string{
	// surfaces
	"--void":   "#1a1714",
	"--ink":    "#201d1a",
	"--inset":  "#1c1916",
	"--panel":  "#252220",
	"--raised": "#282420",
	"--canvas": "#242019",
	"--active": "#332e28",
	"--hover":  "#2a2622",
	// borders
	"--border":        "#3d372f",
	"--border-dashed": "#4a4337",
	"--border-hi":     "#5a5142",
	// text
	"--text-hi":        "#f2ead9",
	"--text":           "#d8d2c8",
	"--text-secondary": "#b8ae9e",
	"--text-dim":       "#9a8f7d",
	"--text-faint":     "#6e675c",
	// semantic accents
	"--amber":    "#ffb454",
	"--amber-hi": "#ffc678",
	"--green":    "#9ece6a",
	"--cyan":     "#56c5d0",
	"--violet":   "#c8a4ff",
	"--red":      "#e06c60",
}
