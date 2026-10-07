package tiled

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Document is a map open for editing: the file's own bytes, and the edits made
// to them.
//
// It exists because Map cannot be written back. Map is a reading model — it
// keeps what the engine needs and drops nextobjectid, image layers, layer
// folders, object shapes, editor settings and more — so a writer built on it
// hands an author back a file with those parts deleted. A Document keeps the
// whole document and rewrites only the elements an edit touched, so anything
// this package does not model survives without this package knowing it exists.
//
// **Only .tmx is written.** Reading a .tmj still works and always will; editing
// one is refused by name. Writing JSON with the same guarantee means a second
// document implementation with its own fidelity tests, and converting a .tmj to
// .tmx on save would silently change a project's file layout and break the
// game.toml that names it.
//
// Not safe for concurrent use, on editable.File's terms: the value that owns a
// Document is responsible for serialising access to it.
type Document struct {
	name string
	tree *xtree

	// The reading model, derived from the bytes rather than kept beside them —
	// a parallel parsed copy is a second thing that can disagree. Invalidated
	// by every edit, which is why a caller must not hold on to what Map
	// returned. Memoised because the canvas asks on every render.
	cached    *Map
	cachedErr error
	valid     bool
}

// ParseDocument opens a map for editing.
//
// It parses twice, deliberately: once into the tree that will be written back,
// and once through Parse, so a Document is always a map that reads. The second
// is what turns "this file has a typo in it" into a refusal here rather than a
// surprise when something later asks for its layers.
func ParseDocument(data []byte, name string) (*Document, error) {
	if firstMeaningfulByte(data) == '{' {
		return nil, fmt.Errorf("tiled: %s is a .tmj, and Forge writes only .tmx; "+
			"in Tiled, File ▸ Save As ▸ Tiled map files (*.tmx)", name)
	}
	tree, err := parseTree(data)
	if err != nil {
		return nil, err
	}
	if tree.root.name != "map" {
		return nil, fmt.Errorf("tiled: %s has <%s> where a map's <map> should be", name, tree.root.name)
	}
	doc := &Document{name: name, tree: tree}
	if _, err := doc.Map(); err != nil {
		return nil, err
	}
	return doc, nil
}

// NewMapSpec is everything a map has to say about itself before it has anything
// in it.
type NewMapSpec struct {
	// MapID is what the spawns table keys this map's entities by. Required:
	// Epic 14 Story 9 made a map without one a trap that only springs when
	// somebody renames the file, and a map Forge wrote without one would be
	// Forge shipping that trap.
	MapID                 string
	Width, Height         int
	TileWidth, TileHeight int
}

// NewDocument writes a map from nothing.
//
// It comes with one empty tile layer and one object group, because a map with
// neither cannot be painted on or spawned into, and adding them is the first
// thing anybody would do. Both are named the way the shipped level names them.
func NewDocument(name string, spec NewMapSpec) (*Document, error) {
	if spec.MapID == "" {
		return nil, fmt.Errorf("tiled: a new map needs a %q, which is what its spawns are keyed by", PropMapID)
	}
	if spec.Width <= 0 || spec.Height <= 0 {
		return nil, fmt.Errorf("tiled: %s is %dx%d tiles, which is not a size", name, spec.Width, spec.Height)
	}
	if spec.TileWidth <= 0 || spec.TileHeight <= 0 {
		return nil, fmt.Errorf("tiled: %s says its tiles are %dx%d pixels, which is not a size",
			name, spec.TileWidth, spec.TileHeight)
	}
	src := `<?xml version="1.0" encoding="UTF-8"?>
<map version="1.10" orientation="orthogonal" renderorder="right-down"` +
		` width="` + strconv.Itoa(spec.Width) + `" height="` + strconv.Itoa(spec.Height) +
		`" tilewidth="` + strconv.Itoa(spec.TileWidth) + `" tileheight="` + strconv.Itoa(spec.TileHeight) +
		`" infinite="0" nextlayerid="3" nextobjectid="1">
 <properties>
  <property name="` + PropMapID + `" value="` + attrText(spec.MapID) + `"/>
 </properties>
 <layer id="1" name="ground" width="` + strconv.Itoa(spec.Width) + `" height="` + strconv.Itoa(spec.Height) + `">
  <data encoding="csv">` + encodeCSV(make([]uint32, spec.Width*spec.Height), spec.Width) + `</data>
 </layer>
 <objectgroup id="2" name="spawns"/>
</map>
`
	return ParseDocument([]byte(src), name)
}

// Bytes is the document as it would be written.
//
// Byte-identical to what was read when nothing has been edited, which is what
// makes editable.File's dirty comparison mean something here.
func (d *Document) Bytes() []byte { return d.tree.render() }

// Map is what this document says, read through the ordinary parser.
//
// **The result belongs to the document.** Mutating it changes no file and is
// discarded by the next edit — including tileset resolution, so a caller that
// resolved this map's tilesets has to resolve again after any edit. Edits go
// through the document.
func (d *Document) Map() (*Map, error) {
	if !d.valid {
		d.cached, d.cachedErr = Parse(d.Bytes(), d.name)
		d.valid = true
	}
	return d.cached, d.cachedErr
}

// invalidate is called by every edit. Forgetting it in a new edit method would
// leave Map answering from before the change, which is the failure mode this
// whole type exists to avoid one level up.
func (d *Document) invalidate() { d.valid = false }

// SetMapID changes the map-level identity property without rewriting any
// unrelated TMX. An absent <properties> block is created, and an existing
// value keeps its attribute-vs-element spelling and any adjacent comments.
func (d *Document) SetMapID(id string) error {
	if err := validateXMLValue("mapId", id); err != nil {
		return err
	}
	root := d.tree.root
	blocks := root.children("properties")
	if len(blocks) > 1 {
		return fmt.Errorf("tiled: map has %d <properties> blocks; mapId cannot be edited unambiguously", len(blocks))
	}
	var block *xelem
	if len(blocks) == 1 {
		block = blocks[0]
	}
	if block != nil {
		var found *xelem
		for _, prop := range block.children("property") {
			if prop.attr("name") != PropMapID {
				continue
			}
			if found != nil {
				return fmt.Errorf("tiled: map has duplicate %q properties", PropMapID)
			}
			found = prop
		}
		if found != nil {
			hasValue := false
			for _, attr := range found.attrs {
				if attr.name == "value" {
					hasValue = true
				}
			}
			for _, kid := range found.kids {
				if kid.el != nil {
					return fmt.Errorf("tiled: mapId has nested content this edit cannot preserve")
				}
				if !hasValue && strings.HasPrefix(strings.TrimSpace(string(kid.raw)), "<") {
					return fmt.Errorf("tiled: mapId has markup inside a text value this edit cannot preserve")
				}
			}
			found.removeAttr("type")
			found.removeAttr("propertytype")
			if hasValue {
				found.setAttr("value", id)
			} else {
				found.setText(id)
			}
			d.invalidate()
			return nil
		}
	} else {
		block = newElem("properties")
		// Tiled writes map properties before layers and tilesets. Insert them
		// there instead of appending after the object groups, where another
		// editor may not treat them as map metadata at all.
		at := -1
		for i, kid := range root.kids {
			if kid.el == nil {
				continue
			}
			switch kid.el.name {
			case "tileset", "layer", "group", "imagelayer", "objectgroup":
				at = i
			}
			if at >= 0 {
				break
			}
		}
		if at < 0 {
			root.appendChild(block, layerIndentStep(root))
		} else {
			block.parent = root
			indent := "\n" + root.childIndent(layerIndentStep(root))
			root.kids = append(root.kids[:at], append([]xnode{{el: block}, {raw: []byte(indent)}}, root.kids[at:]...)...)
			root.markDirty()
		}
	}
	block.appendChild(newElem("property", xattr{"name", PropMapID}, xattr{"value", id}), layerIndentStep(block))
	d.invalidate()
	return nil
}

// validateXMLValue guards mutations before they touch the source tree. The
// attribute writer escapes markup, but escaping cannot represent a NUL or an
// invalid UTF-8 sequence in XML, and a save would otherwise write a file the
// same package can no longer parse.
func validateXMLValue(subject, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("tiled: %s must be valid UTF-8 to be written as XML", subject)
	}
	for _, r := range value {
		if r != '\t' && r != '\n' && r != '\r' &&
			(r < ' ' || r >= 0xD800 && r <= 0xDFFF || r == 0xFFFE || r == 0xFFFF) {
			return fmt.Errorf("tiled: %s contains U+%04X, which XML cannot hold", subject, r)
		}
	}
	return nil
}

// tileLayers and objectGroups are the elements behind Map's Layers and
// ObjectGroups, in the same order.
//
// The flattening has to match collectXML's exactly: a <group> is a layer folder
// and its layers count in document order, so an index that only counted
// top-level elements would address the wrong layer in any map whose author had
// organised it — and the wrong layer is the one you did not mean to paint.
func (d *Document) tileLayers() []*xelem { return collectElems(d.tree.root, "layer") }

func (d *Document) objectGroups() []*xelem { return collectElems(d.tree.root, "objectgroup") }

func collectElems(parent *xelem, want string) []*xelem {
	var out []*xelem
	for _, k := range parent.kids {
		if k.el == nil {
			continue
		}
		switch k.el.name {
		case want:
			out = append(out, k.el)
		case "group":
			out = append(out, collectElems(k.el, want)...)
		}
	}
	return out
}

// SetLayerData replaces every tile in a layer.
//
// index is the index into Map().Layers, which is the flattened order — see
// tileLayers. gids are raw, flags and all, exactly as Layer.Data holds them.
//
// Whole-layer rather than per-cell because that is what makes a stroke one edit:
// a drag across forty cells is one call, one rewritten <data> element and one
// entry in whatever is counting changes.
func (d *Document) SetLayerData(index int, gids []uint32) error {
	layers := d.tileLayers()
	if index < 0 || index >= len(layers) {
		return fmt.Errorf("tiled: %s has no layer %d", d.name, index)
	}
	layer := layers[index]
	w, err := intAttr(layer, "width")
	if err != nil {
		return fmt.Errorf("tiled: %s: layer %q: %w", d.name, layer.attr("name"), err)
	}
	h, err := intAttr(layer, "height")
	if err != nil {
		return fmt.Errorf("tiled: %s: layer %q: %w", d.name, layer.attr("name"), err)
	}
	if len(gids) != w*h {
		return fmt.Errorf("tiled: %s: layer %q is %d cells and was given %d tiles",
			d.name, layer.attr("name"), w*h, len(gids))
	}
	data := layer.firstChild("data")
	if data == nil {
		return fmt.Errorf("tiled: %s: layer %q holds no <data> element", d.name, layer.attr("name"))
	}

	encoding := data.attr("encoding")
	if encoding == "" {
		// One <tile> element per cell. Written back the same way rather than
		// converted: the encoding a file uses is a setting somebody chose, and
		// a save that changed it is an unreviewable diff in their repository.
		step := layerIndentStep(layer)
		kids := make([]xnode, 0, 2*len(gids)+1)
		inner := "\n" + data.indent() + step
		for _, gid := range gids {
			kids = append(kids, xnode{raw: []byte(inner)},
				xnode{el: newElem("tile", xattr{"gid", strconv.FormatUint(uint64(gid), 10)})})
		}
		kids = append(kids, xnode{raw: []byte("\n" + data.indent())})
		for i := range kids {
			if kids[i].el != nil {
				kids[i].el.parent = data
			}
		}
		data.kids = kids
		data.selfClose = false
		data.markDirty()
		d.invalidate()
		return nil
	}

	text, err := encodeLayerData(encoding, data.attr("compression"), gids, w)
	if err != nil {
		return fmt.Errorf("tiled: %s: layer %q: %w", d.name, layer.attr("name"), err)
	}
	data.setText(text)
	d.invalidate()
	return nil
}

// layerIndentStep is one level of indentation, taken from the file rather than
// assumed, so a tab-indented map stays tab-indented.
func layerIndentStep(layer *xelem) string {
	outer := layer.indent()
	if inner := layer.childIndent(""); len(inner) > len(outer) {
		return inner[len(outer):]
	}
	return " "
}

// NextObjectID is the id the next object placed in this map will take.
//
// The higher of the map's own nextobjectid and one past the highest object
// already in it. **Not simply the attribute**, which is the version this shipped
// with and which hands out an id that is already in use the moment the counter
// falls behind its objects.
//
// A counter can fall behind without anyone hand-editing anything. Two branches
// each place a spawn; the objects land on different lines and merge cleanly,
// while nextobjectid is one line and git takes one side of it. The file then
// says 3 with an object 4 in it, the next spawn is created as another object 3,
// and because spawns is keyed (map, object_id) the engine does not create an
// entity for it — it moves the existing one, machine and transitions and all.
//
// So the objects are scanned every time. It costs a walk of a list the document
// is holding anyway, and it makes the guarantee a property of the file rather
// than of the file's bookkeeping being right.
func (d *Document) NextObjectID() int {
	highest := 0
	for _, g := range d.objectGroups() {
		for _, o := range g.children("object") {
			if id, err := strconv.Atoi(o.attr("id")); err == nil && id > highest {
				highest = id
			}
		}
	}
	next := highest + 1
	if v, err := strconv.Atoi(d.tree.root.attr("nextobjectid")); err == nil && v > next {
		next = v
	}
	return next
}

// allocateObjectID hands out an id and moves the counter past it.
//
// **The counter never goes down and an id is never handed out twice**, which is
// not tidiness. The spawns table is keyed (map, object_id), so an id a deleted
// object once held does not create an entity when it comes round again — it
// retargets the entity the old object made, moving a live goblin to wherever
// the new object sits and taking its machine and its transitions with it. There
// is deliberately no API that renumbers objects.
func (d *Document) allocateObjectID() int {
	id := d.NextObjectID()
	d.tree.root.setAttr("nextobjectid", strconv.Itoa(id+1))
	return id
}

// There is no layer-id allocator here. nextlayerid is preserved verbatim like
// every other attribute, which is all this story needs of it; handing one out
// is the business of the story that adds a layer, and an exported function with
// no caller is a promise nobody has tested.

// AddObject puts a spawn in an object group and returns the id it was given.
//
// The id is always allocated here and obj.ID is ignored, so no caller can
// choose one — see allocateObjectID for why that would be a way to move a live
// entity onto the wrong spawn.
//
// obj.Visible is not read either, and objects are created visible. Tiled's
// default is visible and it writes no attribute for it, so honouring the field
// would mean every caller building an Object literal had to remember to set it
// — and a zero-valued Object is a request for an ordinary spawn, not an
// invisible one. Hiding an object is not something Forge offers yet; when it
// does, it is an edit to an object that exists rather than an argument here.
func (d *Document) AddObject(group int, obj Object) (int, error) {
	groups := d.objectGroups()
	if group < 0 || group >= len(groups) {
		return 0, fmt.Errorf("tiled: %s has no object group %d", d.name, group)
	}
	id := d.allocateObjectID()
	el := newElem("object", xattr{"id", strconv.Itoa(id)})
	if obj.Name != "" {
		el.attrs = append(el.attrs, xattr{"name", obj.Name})
	}
	if obj.Type != "" {
		// "type", not "class": Tiled 1.9 renamed it and 1.10 renamed it back,
		// so the current spelling is the one to write, and the reader takes
		// either.
		el.attrs = append(el.attrs, xattr{"type", obj.Type})
	}
	el.attrs = append(el.attrs,
		xattr{"x", formatCoord(obj.X)},
		xattr{"y", formatCoord(obj.Y)})
	for _, opt := range []struct {
		name string
		v    float64
	}{{"width", obj.Width}, {"height", obj.Height}, {"rotation", obj.Rotation}} {
		if opt.v != 0 {
			el.attrs = append(el.attrs, xattr{opt.name, formatCoord(opt.v)})
		}
	}
	if obj.GID != 0 {
		el.attrs = append(el.attrs, xattr{"gid", strconv.FormatUint(uint64(obj.GID), 10)})
	}
	if obj.Template != "" {
		el.attrs = append(el.attrs, xattr{"template", obj.Template})
	}

	step := layerIndentStep(groups[group])
	groups[group].appendChild(el, step)
	addProperties(el, obj.Properties, step)
	d.invalidate()
	return id, nil
}

// RemoveObject deletes an object wherever in the map it is.
//
// Its id does not come back — see allocateObjectID.
func (d *Document) RemoveObject(id int) error {
	o, err := d.objectElement(id)
	if err != nil {
		return err
	}
	o.parent.removeChild(o)
	d.invalidate()
	return nil
}

// MoveObject changes only an existing object's coordinates. Its id, attributes,
// shape, properties and children stay where they were in the file: replacing it
// with a new object would retarget the entity the engine keys by its id.
func (d *Document) MoveObject(id int, x, y float64) error {
	m, err := d.Map()
	if err != nil {
		return err
	}
	o, err := d.objectElement(id)
	if err != nil {
		return err
	}
	maxX := float64(m.Width * m.TileWidth)
	maxY := float64(m.Height * m.TileHeight)
	// A tile object is located by its bottom edge, which may sit exactly on
	// the map's bottom border; a point object cannot.
	tile := o.attr("gid") != "" && o.attr("gid") != "0"
	if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) ||
		x < 0 || x >= maxX || (tile && (y <= 0 || y > maxY)) ||
		(!tile && (y < 0 || y >= maxY)) {
		return fmt.Errorf("tiled: object %d at (%v,%v) is outside the %dx%d map", id, x, y, m.Width, m.Height)
	}
	if o.attr("x") == formatCoord(x) && o.attr("y") == formatCoord(y) {
		return nil
	}
	o.setAttr("x", formatCoord(x))
	o.setAttr("y", formatCoord(y))
	d.invalidate()
	return nil
}

// addProperties puts a <properties> block inside an element, or nothing at all
// when there is nothing to put in one — Tiled writes no empty block and neither
// does this.
//
// The block is attached to its parent *before* its properties go in, which is
// not an ordering preference. Indentation is read from where an element sits, so
// filling the block first indented every property against a block that was not
// anywhere yet — and wrote them, and the closing tag, hard against the left
// margin of a file indented three levels in.
//
// Sorted by name, because Properties is a map and an unsorted save would
// reshuffle a file's properties on every write for no reason visible in a diff.
func addProperties(parent *xelem, props Properties, step string) {
	if len(props) == 0 {
		return
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)

	block := newElem("properties")
	parent.appendChild(block, step)
	for _, name := range names {
		p := props[name]
		attrs := []xattr{{"name", name}}
		// Tiled omits the type for strings, which is its default, and a
		// property written with type="string" reads as a file some other tool
		// produced.
		if p.Type != "" && p.Type != "string" {
			attrs = append(attrs, xattr{"type", p.Type})
		}
		if p.PropertyType != "" {
			attrs = append(attrs, xattr{"propertytype", p.PropertyType})
		}
		attrs = append(attrs, xattr{"value", p.Value})
		block.appendChild(newElem("property", attrs...), step)
	}
}

// formatCoord writes a coordinate the way Tiled writes one: whole numbers with
// no decimal point, and everything else at the shortest spelling that reads
// back identically.
//
// 'f' rather than 'g', which switches to exponent notation at a million and
// would write a coordinate as "1e+06". Out of reach for any map this engine
// draws, and the point of a format function is that it does not have a range
// outside which it means something else.
func formatCoord(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func intAttr(el *xelem, name string) (int, error) {
	raw := el.attr(name)
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s is %q, which is not a number", name, raw)
	}
	return v, nil
}
