package modes

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/paint"
	"github.com/tmbritton/ecs-db/internal/tiled"

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

// HideSignal is the legacy index-keyed visibility signal for layers without an
// ID and direct callers. Real Tiled layers use LayerHideSignal instead. One
// signal per layer rather than an array: Datastar tracks a signal, and mutating
// an element of an array signal is not a change it can see.
//
// Exported because the paint route reads the same name back off a request, to
// refuse a stroke aimed at a layer the view is hiding. That name is shared
// across a package boundary, and if the two halves ever disagreed the lookup
// would return the zero value, the refusal would quietly stop happening, and
// nothing would say so — strokes would land on an invisible layer. One
// implementation, so they cannot disagree.
func HideSignal(layerIndex int) string { return "hide" + itoa(layerIndex) }

// LayerHideSignal uses Tiled's stable layer ID when present. A page keeps view
// signals across SSE patches; after a move, the same index names another layer.
// Older files with no IDs retain index-based signals, and cannot be reordered.
func LayerHideSignal(id, index int) string {
	if id > 0 {
		return "hideID" + itoa(id)
	}
	return HideSignal(index)
}

func hideSignalName(l mapcanvas.Layer) string { return LayerHideSignal(l.ID, l.Index) }

func hideSignal(l mapcanvas.Layer) string { return "$" + hideSignalName(l) }

// layerRowClasses is the row's own state: which layer a stroke lands on, and
// whether this one is being drawn.
func layerRowClasses(l mapcanvas.Layer) string {
	return "{'layer-row--active': " + selectedLayerExpr(l) +
		", 'layer-row--muted': " + hideSignal(l) + "}"
}

func selectedLayerExpr(l mapcanvas.Layer) string {
	if l.ID > 0 {
		return eq("$layerID", uint32(l.ID))
	}
	return eq("$layer", uint32(l.Index))
}

func selectLayerAction(l mapcanvas.Layer) string {
	return "$layer = " + itoa(l.Index) + "; $layerID = " + itoa(l.ID)
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
		names = append(names, selectedLayerExpr(l)+" ? "+quoteJS(l.Name))
	}
	return "'painting ' + (" + strings.Join(names, " : ") + " : 'nothing')"
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
	Path  string
	Spawn int
}

// Href is the page for this view.
func (v MapView) Href() string {
	if v.Path == "" {
		return "/forge/map"
	}
	query := url.Values{"map": {v.Path}}
	if v.Spawn > 0 {
		query.Set("spawn", strconv.Itoa(v.Spawn))
	}
	return "/forge/map?" + query.Encode()
}

// WithMap is this view pointed at another map. Nothing carries over, because
// nothing else is left to carry: what used to survive this call — a layer
// index, a tile id — meant different things in a different map anyway.
func (v MapView) WithMap(path string) MapView { return MapView{Path: path} }

// WithSpawn selects one object without changing the map it belongs to.
func (v MapView) WithSpawn(id int) MapView { return MapView{Path: v.Path, Spawn: id} }

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
// Tiled's positive layer IDs keep these signals attached to the authored layer
// when its position changes. Old files without IDs keep index signals and
// cannot reorder layers until Tiled gives them IDs.
func hideSeed(l mapcanvas.Layer) string {
	return `{"` + hideSignalName(l) + `":` + strconv.FormatBool(l.HiddenInFile) + `}`
}

// A Tool is one of the three things a stroke can do.
type Tool struct {
	Kind  string
	Glyph string
	Title string
}

// Tools are the tools the toolbar offers, in the prototype's order. The kinds
// are paint.Stamp, paint.Fill and paint.Erase — named as strings here because
// the template writes them into a signal, and the server parses that signal
// back into a paint.Kind. paint's own switch is what refuses anything else.
func Tools() []Tool {
	return []Tool{
		{Kind: "stamp", Glyph: "🖌", Title: "paint the tile in hand"},
		{Kind: "fill", Glyph: "▧", Title: "fill a rectangle with the tile in hand"},
		{Kind: "erase", Glyph: "⌫", Title: "clear a cell"},
	}
}

// turnKey is the index the browser computes for a stamp's three flags. It has
// to agree with turnTables below, and nothing else depends on the ordering.
const turnKey = "(($flipD?4:0)+($flipH?2:0)+($flipV?1:0))"

// turnTables enumerates paint.RotateCW over all eight flag combinations and
// returns the three JS array literals the browser indexes with turnKey.
//
// Generated from paint.RotateCW rather than transcribed from it, because the
// rotation cycle is not guessable by eye — a quarter turn changes two flags,
// not one — and a hand-written copy would drift silently: the browser would
// send flags the server accepts as valid, so nothing would report the
// disagreement except a tile pointing the wrong way on screen.
func turnTables() (d, h, v string) {
	var db, hb, vb strings.Builder
	db.WriteByte('[')
	hb.WriteByte('[')
	vb.WriteByte('[')
	for key := 0; key < 8; key++ {
		before := tiled.Tile{GID: 1, FlipD: key&4 != 0, FlipH: key&2 != 0, FlipV: key&1 != 0}
		after := paint.RotateCW(before)
		if key > 0 {
			db.WriteByte(',')
			hb.WriteByte(',')
			vb.WriteByte(',')
		}
		db.WriteString(strconv.FormatBool(after.FlipD))
		hb.WriteString(strconv.FormatBool(after.FlipH))
		vb.WriteString(strconv.FormatBool(after.FlipV))
	}
	db.WriteByte(']')
	hb.WriteByte(']')
	vb.WriteByte(']')
	return db.String(), hb.String(), vb.String()
}

// turnAction advances the stamp a quarter turn clockwise.
//
// The rotation is the browser's — the server is told the flags with the stroke
// rather than remembering which way the stamp is facing, for the same reason
// every other view control on this page is a signal. All three flags are read
// before any is written, or the second assignment would rotate a state the
// first one just changed.
func turnAction() string {
	d, h, v := turnTables()
	return "const k = " + turnKey + ";" +
		"$flipD = " + d + "[k];" +
		"$flipH = " + h + "[k];" +
		"$flipV = " + v + "[k]"
}

// turnLabelList names each of the eight flag combinations, indexed by turnKey.
//
// Walked with RotateCW rather than written out, for the same reason the tables
// are: a hand-written map had two of them swapped, and nothing could see it —
// eight distinct non-empty strings is all a test of a literal map can check.
//
// The eight symmetries of a square fall into two orbits under a quarter turn:
// the unmirrored one starting at upright, and the mirrored one starting at a
// horizontal mirror. Every state is therefore a mirror-or-not plus an angle,
// which is what these say. Note that a plain vertical mirror lands on
// "mirrored 180°" — a top-to-bottom mirror is a left-to-right one turned half
// way round, and naming it after the button that reaches it is not possible:
// each reflected state is reachable by both buttons, from different angles.
func turnLabelList() []string {
	names := make([]string, 8)
	for _, orbit := range []struct {
		start  tiled.Tile
		labels [4]string
	}{
		{tiled.Tile{GID: 1}, [4]string{"upright", "90°", "180°", "270°"}},
		{tiled.Tile{GID: 1, FlipH: true}, [4]string{"mirrored", "mirrored 90°", "mirrored 180°", "mirrored 270°"}},
	} {
		at := orbit.start
		for _, label := range orbit.labels {
			names[turnKeyOf(at)] = label
			at = paint.RotateCW(at)
		}
	}
	return names
}

// turnLabels is that list as a JavaScript array literal.

func turnLabels() string { return jsStringArray(turnLabelList()) }

// jsStringArray writes strings as a JavaScript array literal, each element
// through quoteJS like every other string this file puts in an expression.
//
// Separate from turnLabels so the escaping can be tested with a string that
// actually needs escaping. None of the eight orientation labels does, so a test
// going through turnLabels cannot tell quoteJS from raw quotes — which is what
// a mutation over the real labels showed.
func jsStringArray(items []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(quoteJS(s))
	}
	b.WriteByte(']')
	return b.String()
}

// turnKeyOf is turnKey's arithmetic in Go: D is the 4s bit, H the 2s, V the 1s.
func turnKeyOf(t tiled.Tile) int {
	key := 0
	if t.FlipD {
		key |= 4
	}
	if t.FlipH {
		key |= 2
	}
	if t.FlipV {
		key |= 1
	}
	return key
}

// turnLabelExpr says which way the stamp is facing, in words, because three
// booleans do not read as an angle.
func turnLabelExpr() string {
	return "'stamp ' + " + turnLabels() + "[" + turnKey + "]"
}

// turnLabelSeed is what the note reads before Datastar hydrates. The signals
// start the stamp upright, so this is that orientation's label — taken from the
// same list the expression indexes, so the two cannot disagree.
func turnLabelSeed() string {
	return "stamp " + turnLabelList()[turnKeyOf(tiled.Tile{})]
}

// The pointer surface's half of the boundary with static/js/paint.js.
//
// The module dispatches one `paintstroke` event carrying where a gesture
// started, where it ended, and every cell it covered. Which of those three the
// server is meant to act on is the tool's business, and the tool is a signal —
// so the choice is made here, in an expression, rather than in a module that is
// not allowed to know what a tool is.

// strokeAction turns a completed gesture into a request.
//
// The map is named rather than left to mapShown's default. The default is the
// map the page resolved and would be right, but a second tab editing another
// map is exactly the case where "whatever the server would pick" and "what you
// were looking at" come apart.
func strokeAction(data Data) string {
	where := "'/forge/map/paint?map=" + urlValue(data.SelectedMap) +
		"&x=' + evt.detail.x + '&y=' + evt.detail.y"
	return "$tool === '" + string(paint.Fill) + "' " +
		"? @post(" + where + " + '&x2=' + evt.detail.x2 + '&y2=' + evt.detail.y2) " +
		": @post(" + where + " + '&cells=' + evt.detail.cells)"
}

// ghostNoteExpr is the label beside the ghost stamp: what a click would do,
// right now, in words.
//
// The tool as well as the orientation, because "stamp 90°" printed beside an
// eraser describes a stroke that is not the one about to happen. Erase has no
// orientation to report — the empty cell has no facing — so it says only what
// it is.
func ghostNoteExpr() string {
	return "$tool === '" + string(paint.Erase) + "' ? 'erase' : " +
		"($tool + ' ' + " + turnLabels() + "[" + turnKey + "])"
}

// marqueeClass shows the drag rectangle for the one tool that means a
// rectangle.
//
// paint.js sets the rectangle's coordinates and a class saying a drag is under
// way; it does not know which tool is chosen and must not. So the two halves of
// the condition come from opposite sides — the pointer says "dragging", the
// signal says "fill" — and CSS is where they meet.
//
// Deliberately not shown for the stamp and the eraser. Those follow the pointer,
// so a rectangle drawn round a curved drag would promise cells the stroke is not
// going to paint.
func marqueeClass() string {
	return "{'map-marquee--on': $tool === '" + string(paint.Fill) + "'}"
}

// The grid, as a signal like every other view control on this page.

func gridToggle() string { return "$grid = !$grid" }

func gridClass() string { return "{'map-canvas__grid--off': !$grid}" }

func gridTitleExpr() string {
	return "$grid ? 'hide the grid' : 'show the grid'"
}

// shortcutAction is the prototype's tool shortcuts: B stamp, R rect fill, E
// eraser, Q turn, X mirror, G grid.
//
// The prototype also lists Select (M). There is no select tool — no paint.Kind,
// no route, nothing a selection could mean to the server — and a shortcut that
// chooses a tool which does not exist is worse than no shortcut.
//
// data-on compiles its expression as a function body rather than an expression,
// so this can be statements and can return early. That is what makes the guards
// readable, and they are the important part: without them every letter typed
// into a name field would also change the tool.
//
// No regexp literal. Datastar rewrites $name and @name( in the expression text
// before compiling it, and a literal is not a place it stops looking.
func shortcutAction() string {
	d, h, v := turnTables()
	return "if (evt.ctrlKey || evt.metaKey || evt.altKey) return;" +
		"const t = evt.target;" +
		"if (t && (t.isContentEditable || ['INPUT','TEXTAREA','SELECT'].includes(t.tagName))) return;" +
		"const key = evt.key.toLowerCase();" +
		"if (key === 'b') $tool = '" + string(paint.Stamp) + "';" +
		"else if (key === 'r') $tool = '" + string(paint.Fill) + "';" +
		"else if (key === 'e') $tool = '" + string(paint.Erase) + "';" +
		// The same tables the turn button uses, not a second copy of the cycle:
		// two spellings of a rotation that is not guessable by eye is two
		// spellings that will drift, and nothing would report it.
		"else if (key === 'q') { const k = " + turnKey + ";" +
		"$flipD = " + d + "[k]; $flipH = " + h + "[k]; $flipV = " + v + "[k]; }" +
		"else if (key === 'x') $flipH = !$flipH;" +
		"else if (key === 'g') " + gridToggle() + ";"
}
