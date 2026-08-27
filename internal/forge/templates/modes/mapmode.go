package modes

import (
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
)

// MapView is the URL's view state for MAP mode: which map, which layers Forge
// is hiding, and which tile the palette has selected.
//
// A struct rather than three parameters because every link in the mode has to
// carry all of it — a layer toggle that dropped the tile selection would
// deselect your brush every time you looked at a different layer.
type MapView struct {
	Path string
	// Hidden is the complete set of layers Forge is not drawing, seeded from the
	// file's own visible attributes when the URL says nothing. Complete, so a
	// layer the file hides can be turned on — the URL is the answer, not one
	// half of an OR with the file.
	Hidden map[int]bool
	// Active is the layer a stroke would land on. Story 4 paints into it.
	Active      int
	SelectedGID uint32
}

// Href is the link to this view.
func (v MapView) Href() string {
	q := url.Values{}
	if v.Path != "" {
		q.Set("map", v.Path)
	}
	if hide := hiddenList(v.Hidden); hide != "" {
		q.Set("hide", hide)
	}
	if v.Active != 0 {
		q.Set("layer", strconv.Itoa(v.Active))
	}
	if v.SelectedGID != 0 {
		q.Set("tile", strconv.FormatUint(uint64(v.SelectedGID), 10))
	}
	if len(q) == 0 {
		return "/forge/map"
	}
	return "/forge/map?" + q.Encode()
}

// WithMap is this view pointed at another map, keeping nothing else: a layer
// index and a tile id mean different things in a different map, so carrying
// them over would hide a layer nobody asked about and select a tile that is not
// there.
func (v MapView) WithMap(path string) MapView { return MapView{Path: path} }

// WithLayerToggled flips one layer's visibility.
func (v MapView) WithLayerToggled(i int) MapView {
	next := MapView{Path: v.Path, Active: v.Active, SelectedGID: v.SelectedGID, Hidden: map[int]bool{}}
	for k, on := range v.Hidden {
		if on {
			next.Hidden[k] = true
		}
	}
	if next.Hidden[i] {
		delete(next.Hidden, i)
	} else {
		next.Hidden[i] = true
	}
	return next
}

// WithTile is this view with another tile selected, or with none when the tile
// given is the one already selected — so clicking the current tile clears it.
// WithActiveLayer is this view painting into another layer.
func (v MapView) WithActiveLayer(i int) MapView {
	return MapView{Path: v.Path, Hidden: v.Hidden, Active: i, SelectedGID: v.SelectedGID}
}

func (v MapView) WithTile(gid uint32) MapView {
	next := MapView{Path: v.Path, Hidden: v.Hidden, Active: v.Active, SelectedGID: gid}
	if v.SelectedGID == gid {
		next.SelectedGID = 0
	}
	return next
}

// ParseHidden reads the hide parameter. Anything that is not a layer index is
// ignored rather than refused: a hand-edited or stale URL should show you the
// map, not an error page.
func ParseHidden(raw string) map[int]bool {
	if raw == "" {
		return nil
	}
	out := map[int]bool{}
	for _, part := range strings.Split(raw, ",") {
		if i, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && i >= 0 {
			out[i] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ParseLayer reads the layer parameter, on ParseHidden's terms.
func ParseLayer(raw string) int {
	i, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || i < 0 {
		return 0
	}
	return i
}

// ParseGID reads the tile parameter, on the same terms.
func ParseGID(raw string) uint32 {
	v, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(v)
}

// hiddenList renders the set in ascending order, so one view has one URL and
// the page stream's identical-patch suppression is not defeated by map
// iteration order.
func hiddenList(hidden map[int]bool) string {
	if len(hidden) == 0 {
		return ""
	}
	idx := make([]int, 0, len(hidden))
	for i, on := range hidden {
		if on {
			idx = append(idx, i)
		}
	}
	sort.Ints(idx)
	parts := make([]string, len(idx))
	for i, v := range idx {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

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

func canvasBoxStyle(w, h int) templ.SafeCSS {
	return templ.SafeCSS("width:" + itoa(w) + "px;height:" + itoa(h) + "px")
}

// gridStyle draws the cell grid with a repeating gradient rather than an
// element per cell: the grid is decoration, and 300 more elements to look at
// lines is 300 more things for a patch to diff.
func gridStyle(c mapcanvas.Canvas) templ.SafeCSS {
	return templ.SafeCSS(
		"background-size:" + itoa(c.TileW*c.Scale) + "px " + itoa(c.TileH*c.Scale) + "px")
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
func layerTitle(l mapcanvas.Layer) string {
	switch {
	case l.Hidden && l.HiddenInFile:
		return "not drawn — the map's own visible attribute hides it. Click to look at it anyway"
	case l.Hidden:
		return "not drawn in Forge. The file still says visible, and the engine imports it either way"
	case l.HiddenInFile:
		return "drawn here, though the map's visible attribute hides it"
	case l.Transparent:
		return "drawn, and invisible: the map sets this layer's opacity to 0"
	default:
		return "drawn"
	}
}

// activeLayerName is what the status line calls the layer a stroke would land
// on, or says when there is none to land on.
func activeLayerName(c mapcanvas.Canvas) string {
	for _, l := range c.Layers {
		if l.Active {
			return "painting " + l.Name
		}
	}
	return "no layer"
}
