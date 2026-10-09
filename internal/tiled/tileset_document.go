package tiled

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// TilesetDocument is a TSX open for editing. The reading model is derived from
// its source bytes; edits invalidate that model rather than maintaining a
// second parsed copy alongside the XML tree.
type TilesetDocument struct {
	name, dir string
	tree      *xtree

	cached    *Tileset
	cachedErr error
	valid     bool
	metaValid bool
	metadata  []string
	ambiguous []uint32
}

// ParseTilesetDocument accepts external XML tilesets only. JSON tilesets can
// be read by ParseTileset, but writing one from the reading model would lose
// metadata that parser deliberately does not hold.
func ParseTilesetDocument(data []byte, name, dir string) (*TilesetDocument, error) {
	if !strings.EqualFold(filepath.Ext(name), ".tsx") {
		return nil, fmt.Errorf("tiled: %s is not an external .tsx file; embedded and JSON tilesets are read-only", name)
	}
	if firstMeaningfulByte(data) == '{' {
		return nil, fmt.Errorf("tiled: %s is a .tsj; Forge writes only .tsx tilesets", name)
	}
	tree, err := parseTree(data)
	if err != nil {
		return nil, err
	}
	if tree.root.name != "tileset" {
		return nil, fmt.Errorf("tiled: %s has <%s> where a tileset's <tileset> should be", name, tree.root.name)
	}
	d := &TilesetDocument{name: name, dir: dir, tree: tree}
	if _, err := d.Tileset(); err != nil {
		return nil, err
	}
	return d, nil
}

// Bytes is the exact original TSX when nothing has been edited. Each change
// renders only the affected ancestors, leaving unknown sibling XML verbatim.
func (d *TilesetDocument) Bytes() []byte { return d.tree.render() }

// Tileset is the engine's reading model for these working bytes. Callers must
// not mutate it or hold it across document edits.
func (d *TilesetDocument) Tileset() (*Tileset, error) {
	if !d.valid {
		d.cached, d.cachedErr = ParseTileset(d.Bytes(), d.name, d.dir)
		d.valid = true
	}
	return d.cached, d.cachedErr
}

// MetadataFeatures names authored Tiled-only XML which this game's renderer,
// traversal and sprite animator do not consume. The writer retains it, but the
// editor must not imply that saving it changes gameplay. The parsed tree keeps
// comments out of the answer and survives surgical tile edits.
func (d *TilesetDocument) MetadataFeatures() []string {
	d.metadataSnapshot()
	return append([]string(nil), d.metadata...)
}

// ClassAmbiguities returns IDs whose XML declares both spellings. The reading
// model chooses one to display, but an edit must not silently discard the
// other: SetTileClass refuses those tiles until the ambiguity is fixed.
func (d *TilesetDocument) ClassAmbiguities() []uint32 {
	d.metadataSnapshot()
	return append([]uint32(nil), d.ambiguous...)
}

// metadataSnapshot scans the lossless tree once per working document version.
// The number of authored tiles can be large, but the header never holds more
// than 16 per-tile labels plus a summary and the two tileset-wide features.
func (d *TilesetDocument) metadataSnapshot() {
	if d.metaValid {
		return
	}
	d.metadata, d.ambiguous = nil, nil
	var extra int
	var wang, terrains bool
	add := func(label string) {
		if len(d.metadata) < 16 {
			d.metadata = append(d.metadata, label)
		} else {
			extra++
		}
	}
	for _, kid := range d.tree.root.kids {
		if kid.el == nil {
			continue
		}
		if kid.el.name == "wangsets" {
			wang = true
		}
		if kid.el.name == "terraintypes" {
			terrains = true
		}
		if kid.el.name != "tile" {
			continue
		}
		tile := kid.el
		id := tile.attr("id")
		hasType, hasClass := false, false
		for _, attr := range tile.attrs {
			hasType = hasType || attr.name == "type"
			hasClass = hasClass || attr.name == "class"
		}
		if hasType && hasClass {
			if parsed, err := strconv.ParseUint(id, 10, 32); err == nil {
				d.ambiguous = append(d.ambiguous, uint32(parsed))
			}
		}
		var collision, animation bool
		for _, child := range tile.kids {
			if child.el != nil {
				collision = collision || child.el.name == "objectgroup"
				animation = animation || child.el.name == "animation"
			}
		}
		if collision {
			add("tile " + id + " collision objects")
		}
		if animation {
			add("tile " + id + " animation")
		}
	}
	if extra > 0 {
		d.metadata = append(d.metadata, fmt.Sprintf("%d more tile metadata declarations preserved", extra))
	}
	if wang {
		d.metadata = append(d.metadata, "wangsets")
	}
	if terrains {
		d.metadata = append(d.metadata, "terrains")
	}
	sort.Slice(d.ambiguous, func(i, j int) bool { return d.ambiguous[i] < d.ambiguous[j] })
	d.metaValid = true
}

func (d *TilesetDocument) invalidate() { d.valid, d.metaValid = false, false }

// SetTileProperty edits an explicitly authored tile without reconstructing
// its animation, collision shapes or unknown children from the reading model.
// A sheet's implicit tiles are introduced only by the separate add path.
func (d *TilesetDocument) SetTileProperty(local uint32, name string, value Property) error {
	if name == "" {
		return fmt.Errorf("tiled: a tile property needs a name")
	}
	for _, text := range []string{name, value.Type, value.PropertyType, value.Value} {
		if err := validateXMLValue("tile property", text); err != nil {
			return err
		}
	}
	tile, err := d.tileForEdit(local)
	if err != nil {
		return err
	}
	if err := setElementProperty(tile, fmt.Sprintf("tile %d", local), name, value); err != nil {
		return err
	}
	d.invalidate()
	return nil
}

// SetTileClass edits one tile's class, preserving the file's existing
// class-versus-type spelling. A previously implicit sheet tile gains an
// authored <tile> element; collection IDs are never invented.
func (d *TilesetDocument) SetTileClass(local uint32, class string) error {
	if err := validateXMLValue("tile class", class); err != nil {
		return err
	}
	if tile := d.tileElement(local); tile != nil {
		hasType, hasClass := false, false
		for _, attr := range tile.attrs {
			hasType = hasType || attr.name == "type"
			hasClass = hasClass || attr.name == "class"
		}
		if hasType && hasClass {
			return fmt.Errorf("tiled: tile %d declares both type and class; choose one in Tiled before editing it", local)
		}
	}
	if class == "" && d.tileElement(local) == nil {
		set, err := d.Tileset()
		if err != nil {
			return err
		}
		if set.Collection() || local >= uint32(set.TileCount) {
			return fmt.Errorf("tiled: %s has no tile %d to edit", d.name, local)
		}
		return nil // the implicit sheet tile already has no class
	}
	tile, err := d.tileForEdit(local)
	if err != nil {
		return err
	}
	attr := "type" // Tiled 1.10's spelling for new metadata
	for _, a := range tile.attrs {
		if a.name == "type" || a.name == "class" {
			attr = a.name
			break
		}
	}
	if class == "" {
		tile.removeAttr(attr)
	} else {
		tile.setAttr(attr, class)
	}
	d.invalidate()
	return nil
}

func (d *TilesetDocument) tileElement(local uint32) *xelem {
	for _, tile := range d.tree.root.children("tile") {
		if n, err := strconv.ParseUint(tile.attr("id"), 10, 32); err == nil && uint32(n) == local {
			return tile
		}
	}
	return nil
}

// A sheet declares implicit tiles 0..tilecount-1; a collection does not.
// Insert a first metadata element for a sheet tile before the next authored
// tile or the terrain/wang metadata, without rewriting any existing sibling.
func (d *TilesetDocument) tileForEdit(local uint32) (*xelem, error) {
	set, err := d.Tileset()
	if err != nil {
		return nil, err
	}
	if !set.Collection() && local >= uint32(set.TileCount) {
		return nil, fmt.Errorf("tiled: %s has no tile %d to edit", d.name, local)
	}
	if tile := d.tileElement(local); tile != nil {
		return tile, nil
	}
	if set.Collection() {
		return nil, fmt.Errorf("tiled: %s has no tile %d to edit", d.name, local)
	}
	tile := newElem("tile", xattr{"id", strconv.FormatUint(uint64(local), 10)})
	root := d.tree.root
	for i, kid := range root.kids {
		if kid.el == nil {
			continue
		}
		insert := false
		switch kid.el.name {
		case "tile":
			other, _ := strconv.ParseUint(kid.el.attr("id"), 10, 32)
			insert = uint32(other) > local
		case "wangsets", "terraintypes":
			insert = true
		}
		if insert {
			tile.parent = root
			indent := "\n" + root.childIndent(layerIndentStep(root))
			root.kids = append(root.kids[:i], append([]xnode{{el: tile}, {raw: []byte(indent)}}, root.kids[i:]...)...)
			root.markDirty()
			return tile, nil
		}
	}
	root.appendChild(tile, layerIndentStep(root))
	return tile, nil
}
