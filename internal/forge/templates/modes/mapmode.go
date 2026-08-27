package modes

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
)

// cellStyle places one tile: its own size, the slice of the sheet it is, and
// the matrix that puts it where it goes — flips included, because the
// translation is part of the same matrix.
//
// CSS matrix(a,b,c,d,e,f) is column-major — x' = a·x + c·y + e — and the
// engine's is written the other way round, so b and c swap crossing over.
// Getting that wrong flips every rotated tile about the wrong axis, which looks
// like art authored badly rather than a transposed pair of numbers.
func cellStyle(c mapcanvas.Cell) templ.SafeCSS {
	var b strings.Builder
	b.WriteString("width:" + itoa(c.SW) + "px;height:" + itoa(c.SH) + "px;")
	b.WriteString("transform:matrix(" +
		ftoa(c.A) + "," + ftoa(c.C) + "," + ftoa(c.B) + "," + ftoa(c.D) + "," +
		ftoa(c.TX) + "," + ftoa(c.TY) + ");")
	if c.Image != "" {
		b.WriteString("background-image:url(" + c.Image + ");")
		b.WriteString("background-position:" + itoa(-c.SX) + "px " + itoa(-c.SY) + "px;")
	}
	if c.Alpha < 1 {
		b.WriteString("opacity:" + ftoa(c.Alpha) + ";")
	}
	return templ.SafeCSS(b.String())
}

// paletteStyle shows one tile of a sheet at its own size.
func paletteStyle(t mapcanvas.PaletteTile) templ.SafeCSS {
	return templ.SafeCSS(
		"width:" + itoa(t.SW) + "px;height:" + itoa(t.SH) + "px;" +
			"background-image:url(" + t.Image + ");" +
			"background-size:" + itoa(t.SheetW) + "px " + itoa(t.SheetH) + "px;" +
			"background-position:" + itoa(-t.SX) + "px " + itoa(-t.SY) + "px")
}

// canvasVars are the map's own dimensions, in map pixels. The size on screen
// is these times --map-zoom, computed in CSS, so changing zoom moves no bytes.
func canvasVars(c mapcanvas.Canvas) templ.SafeCSS {
	return templ.SafeCSS(
		"--map-w:" + itoa(c.W) + ";--map-h:" + itoa(c.H) +
			";--tile-w:" + itoa(c.TileW) + ";--tile-h:" + itoa(c.TileH))
}

// ftoa is a coordinate written as a number at every magnitude — never in
// exponent notation, which no CSS parser reads.
func ftoa(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// cellID names a cell: the layer it is in and where it sits.
//
// Not unique, and it cannot be — Tiled permits two layers with one name, and
// then two cells share an id. Good enough for a test to point at and for a
// person to read; Story 5 needs to key pointer state on the layer *index*,
// which is what the panel and the paint routes already address a layer by.
func cellID(c mapcanvas.Cell) string {
	return c.Layer + ":" + itoa(c.X) + "," + itoa(c.Y)
}

func gidLabel(gid uint32) string { return strconv.FormatUint(uint64(gid), 10) }

func layerCount(c mapcanvas.Canvas) string {
	if len(c.Layers) == 1 {
		return "1 layer"
	}
	return itoa(len(c.Layers)) + " layers"
}

// layerTitle says what a row's state means, because several are possible and
// only one of them is about the file.
// layerTitleExpr is the eye's tooltip, chosen in the browser because what it
// describes is.
//
// It used to be a Go function over Layer.Hidden — and when visibility became a
// signal, Build stopped setting that field while the function went on reading
// it. Every eye then said "drawn", including the ones that were not: server-
// rendered view state, frozen at page load, which is the exact thing this mode
// stopped doing everywhere else.
func layerTitleExpr(l mapcanvas.Layer) string {
	hidden, shown := "not drawn in Forge. The file still says visible, and the engine imports it either way", "drawn"
	if l.HiddenInFile {
		hidden = "not drawn — the map's own visible attribute hides it. Click to look at it anyway"
		shown = "drawn here, though the map's visible attribute hides it"
	} else if l.Transparent {
		shown = "drawn, and invisible: the map sets this layer's opacity to 0"
	}
	return hideSignal(l) + " ? " + quoteJS(hidden) + " : " + quoteJS(shown)
}

// layerTitleSeed is the same tooltip for first paint, before Datastar has run.
func layerTitleSeed(l mapcanvas.Layer) string {
	if l.HiddenInFile {
		return "not drawn — the map's own visible attribute hides it. Click to look at it anyway"
	}
	if l.Transparent {
		return "drawn, and invisible: the map sets this layer's opacity to 0"
	}
	return "drawn"
}

// The text these controls carry before Datastar has run, and if it never does.
//
// data-text replaces an element's contents on hydration, so seeding them costs
// nothing and buys a first paint that is not a column of blank boxes and three
// missing status fields. The seed is the same value the signal is seeded from,
// so the two cannot disagree.
func eyeSeed(l mapcanvas.Layer) string {
	if l.HiddenInFile {
		return "—"
	}
	return "👁"
}

func zoomLabelSeed(c mapcanvas.Canvas) string {
	n := mapcanvas.InitialScale(c)
	return itoa(n) + "× · " + itoa(n*c.TileW) + "px cells"
}

func activeLayerSeed(c mapcanvas.Canvas) string {
	for _, l := range c.Layers {
		if !l.HiddenInFile {
			return "painting " + l.Name
		}
	}
	return "painting nothing"
}

// The Datastar expressions MAP's view controls are wired with.
//
// Written here rather than inline in the template for the reason every other
// expression in this package is: a Datastar attribute whose plugin name does
// not resolve is skipped in silence, so the expressions are worth having in one
// place where a test can read them.

// eq is the expression for "this signal currently holds this value".
func eq(signal string, v uint32) string {
	return signal + " === " + strconv.FormatUint(uint64(v), 10)
}

// flag is eq as the string "true" or "false".
//
// Datastar's attr plugin writes a bare attribute for a boolean — true becomes
// `aria-pressed=""` and false removes it — which is right for `disabled` and
// wrong for everything here. ARIA states are enumerated strings, and the data
// attributes the browser suite selects on are asserted in both states, which
// an absent attribute cannot express.
func flag(expr string) string { return "(" + expr + ") ? 'true' : 'false'" }

// toggleSignal sets a signal to v, or back to zero if it is already v. Clicking
// the tile in hand puts it down.
func toggleSignal(signal string, v uint32) string {
	n := strconv.FormatUint(uint64(v), 10)
	return signal + " = " + signal + " === " + n + " ? 0 : " + n
}

// selectedWhen is a data-class object literal adding one class while a signal
// holds a value.
func selectedWhen(signal string, v uint32, class string) string {
	return "{'" + class + "': " + eq(signal, v) + "}"
}

// hideSignalName is the signal carrying one layer's visibility. One per layer
// rather than an array: Datastar tracks a signal, and mutating an element of an
// array signal is not a change it can see.
func hideSignalName(l mapcanvas.Layer) string { return "hide" + itoa(l.Index) }

func hideSignal(l mapcanvas.Layer) string { return "$" + hideSignalName(l) }

// layerRowClasses is the row's own state: which layer a stroke lands on, and
// whether this one is being drawn.
func layerRowClasses(l mapcanvas.Layer) string {
	return "{'layer-row--active': " + eq("$layer", uint32(l.Index)) +
		", 'layer-row--muted': " + hideSignal(l) + "}"
}

// zoomLabelExpr is the status line's "3× · 48px cells", computed in the browser
// because the zoom it describes is.
func zoomLabelExpr(c mapcanvas.Canvas) string {
	return "$zoom + '× · ' + ($zoom * " + itoa(c.TileW) + ") + 'px cells'"
}

// activeLayerExpr names the layer a stroke lands on. A lookup table rather than
// a computation: the layer names are the server's to know and the choice is the
// browser's, so the expression carries the one and reads the other.
func activeLayerExpr(c mapcanvas.Canvas) string {
	if len(c.Layers) == 0 {
		return "'no layer'"
	}
	names := make([]string, 0, len(c.Layers))
	for _, l := range c.Layers {
		names = append(names, quoteJS(l.Name))
	}
	return "'painting ' + ([" + strings.Join(names, ",") + "][$layer] ?? 'nothing')"
}

// quoteJS writes a string as a JavaScript single-quoted literal. Layer names
// come from a .tmx that anyone may have written, so a name containing a quote
// or a backslash must not be able to end the literal and become expression.
func quoteJS(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\\', '\'':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case ' ':
			b.WriteString("\\u2028")
		case ' ':
			b.WriteString("\\u2029")
		case '@':
			// Not a JavaScript concern. Datastar rewrites `@name(` into an
			// action call *after* it has finished protecting string literals,
			// so the rewrite reaches inside them: a layer called "@post(x)"
			// makes the status line read `__action("post",evt,x)`. Corruption
			// of someone's layer name rather than execution of it — literals
			// are protected from the $signal pass, which is the one that could
			// matter — but a name is a name and should arrive as it was written.
			b.WriteString("\\x40")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// cellsOfLayer is the cells belonging to one layer, in draw order. Build groups
// them once; this is the lookup.
func cellsOfLayer(c mapcanvas.Canvas, index int) []mapcanvas.Cell {
	return c.CellsByLayer[index]
}

// Steps re-exports the zoom levels the control offers, so the template does not
// reach into mapcanvas for a list it only renders.
func Steps() []int { return mapcanvas.Steps() }

// MapView is which map is being edited, and nothing else.
//
// It used to carry the zoom, the tile in hand, the active layer and which
// layers were drawn, all of them in the query string. They are signals now, for
// a reason that is not preference: a page's SSE subscription is built when it
// loads and cannot change afterwards, so anything the server renders from it is
// frozen at that moment — and the next unrelated event would re-render the view
// as it was and undo whatever had happened since. What the server does not
// render, a re-render cannot clobber.
//
// The map itself stays, because it is not view state: it says which file is
// being edited, it belongs in a URL somebody can paste, and changing it is a
// page load rather than a repaint.
type MapView struct {
	Path string
}

// Href is the page for this view.
func (v MapView) Href() string {
	if v.Path == "" {
		return "/forge/map"
	}
	return "/forge/map?" + url.Values{"map": {v.Path}}.Encode()
}

// WithMap is this view pointed at another map. Nothing carries over, because
// nothing else is left to carry: what used to survive this call — a layer
// index, a tile id — meant different things in a different map anyway.
func (v MapView) WithMap(path string) MapView { return MapView{Path: path} }

// hideSeed declares one layer's visibility signal, seeded from the file, and
// only if the page does not already have it.
//
// The signals a page opens with are written once on <main>, which is never
// patched — that is what makes them survive a re-render. But a map reloaded
// from disk with an extra layer produces a patched canvas referring to a signal
// the page never declared, and Datastar auto-creates a missing signal as the
// empty string: falsy, so the new layer would come up *drawn* even when the
// .tmx hides it, silently contradicting the one rule this seeding exists for.
//
// __ifmissing is what makes re-declaring safe. Without it every patch would
// reset visibility to what the file says and undo the eye on each push.
//
// What this does not fix is a reload that *reorders* layers: index 3 then means
// a different layer and keeps the old value. That is visible rather than silent
// — the row shows which layers are hidden, and clicking corrects it — and the
// alternative is a stable per-layer identity, which .tmx does not give us: names
// are not unique and ids are not on tile layers.
func hideSeed(l mapcanvas.Layer) string {
	return `{"` + hideSignalName(l) + `":` + strconv.FormatBool(l.HiddenInFile) + `}`
}
