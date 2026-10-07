package tiled

import (
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// DuplicateObject copies the whole XML subtree so shapes, comments, templates
// and future Tiled additions survive. The new id is allocated only after the
// object and destination have been validated.
func (d *Document) DuplicateObject(id int, x, y float64) (int, error) {
	original, err := d.objectElement(id)
	if err != nil {
		return 0, err
	}
	m, err := d.Map()
	if err != nil {
		return 0, err
	}
	maxX, maxY := float64(m.Width*m.TileWidth), float64(m.Height*m.TileHeight)
	tile := original.attr("gid") != "" && original.attr("gid") != "0"
	if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) ||
		x < 0 || x >= maxX || (tile && (y <= 0 || y > maxY)) ||
		(!tile && (y < 0 || y >= maxY)) {
		return 0, fmt.Errorf("tiled: duplicate of object %d is outside the %dx%d map", id, m.Width, m.Height)
	}
	newID := d.allocateObjectID()
	clone := cloneElement(original)
	clone.setAttr("id", strconv.Itoa(newID))
	clone.setAttr("x", formatCoord(x))
	clone.setAttr("y", formatCoord(y))
	group := original.parent
	group.appendChild(clone, layerIndentStep(group))
	d.invalidate()
	return newID, nil
}

func cloneElement(src *xelem) *xelem {
	clone := *src
	clone.parent = nil
	clone.attrs = append([]xattr(nil), src.attrs...)
	clone.kids = make([]xnode, len(src.kids))
	for i, node := range src.kids {
		clone.kids[i] = node
		if node.el != nil {
			child := cloneElement(node.el)
			child.parent = &clone
			clone.kids[i].el = child
		}
	}
	return &clone
}

// objectElement resolves an id before any mutation: duplicate ids cannot be
// edited by picking whichever object happens to come first in file order.
func (d *Document) objectElement(id int) (*xelem, error) {
	var found *xelem
	want := strconv.Itoa(id)
	for _, group := range d.objectGroups() {
		for _, obj := range group.children("object") {
			if obj.attr("id") != want {
				continue
			}
			if found != nil {
				return nil, fmt.Errorf("tiled: duplicate object id %d in %s", id, d.name)
			}
			found = obj
		}
	}
	if found == nil {
		return nil, fmt.Errorf("tiled: %s holds no object %d", d.name, id)
	}
	return found, nil
}

// SetObjectProperty adds or replaces one custom property, preserving every
// other object child and attribute verbatim. Validation of whether the engine
// accepts the value belongs above this file-level mutator.
func (d *Document) SetObjectProperty(id int, name string, value Property) error {
	component, field, ok := strings.Cut(name, ".")
	if !ok || component == "" || field == "" {
		return fmt.Errorf("tiled: property %q must be Component.property", name)
	}
	obj, err := d.objectElement(id)
	if err != nil {
		return err
	}
	if err := setElementProperty(obj, fmt.Sprintf("object %d", id), name, value); err != nil {
		return err
	}
	d.invalidate()
	return nil
}

// setElementProperty is shared by TMX object and TSX tile edits. Both formats
// use the same Tiled <properties> vocabulary; keeping the byte-fidelity and
// nested-value refusals in one place prevents the two writers drifting.
func setElementProperty(owner *xelem, label, name string, value Property) error {
	for _, text := range []string{name, value.Type, value.PropertyType, value.Value} {
		if err := validateXMLValue(label+" property", text); err != nil {
			return err
		}
	}
	blocks := owner.children("properties")
	if len(blocks) > 1 {
		return fmt.Errorf("tiled: %s has %d <properties> blocks; this edit cannot choose one", label, len(blocks))
	}
	var block *xelem
	if len(blocks) == 1 {
		block = blocks[0]
	}
	if block != nil {
		var found *xelem
		for _, prop := range block.children("property") {
			if prop.attr("name") != name {
				continue
			}
			if found != nil {
				return fmt.Errorf("tiled: %s has duplicate property %q", label, name)
			}
			found = prop
		}
		if found != nil {
			hasValueAttr := false
			for _, attr := range found.attrs {
				if attr.name == "value" {
					hasValueAttr = true
				}
			}
			for _, child := range found.kids {
				if child.el != nil {
					return fmt.Errorf("tiled: property %q has nested content this edit cannot preserve", name)
				}
				if !hasValueAttr && strings.HasPrefix(strings.TrimSpace(string(child.raw)), "<") {
					return fmt.Errorf("tiled: property %q has markup inside a text value this edit cannot preserve", name)
				}
			}
			// type="string" is Tiled's default. Preserve an authored
			// explicit spelling on an unchanged value, but drop an obsolete
			// int/bool/float when the field changes to string.
			if value.Type == "" || value.Type == "string" {
				if found.attr("type") != "string" {
					found.removeAttr("type")
				}
			} else {
				found.setAttr("type", value.Type)
			}
			if value.PropertyType == "" {
				found.removeAttr("propertytype")
			} else {
				found.setAttr("propertytype", value.PropertyType)
			}
			if hasValueAttr {
				// Children such as comments are unrelated to the attribute
				// value, so keep them exactly as they arrived.
				found.setAttr("value", value.Value)
			} else if !sameTextPropertyValue(found, value.Value) {
				// Tiled also accepts the element-content spelling. Retain it
				// instead of inventing a value attribute and leaving the old
				// text in the file as a second, conflicting value.
				found.setText(value.Value)
			}
			return nil
		}
	} else {
		block = newElem("properties")
		// A tile's properties precede its image, collision objects and
		// animation in a TSX. Keep that order when adding the first block;
		// an object keeps the existing append behavior.
		inserted := false
		if owner.name == "tile" {
			for i, child := range owner.kids {
				if child.el == nil {
					continue
				}
				switch child.el.name {
				case "image", "objectgroup", "animation":
					at := i
					for at > 0 && owner.kids[at-1].el == nil {
						at--
					}
					block.parent = owner
					indent := "\n" + owner.childIndent(layerIndentStep(owner))
					owner.kids = append(owner.kids[:at], append([]xnode{{raw: []byte(indent)}, {el: block}}, owner.kids[at:]...)...)
					owner.markDirty()
					inserted = true
				}
				if inserted {
					break
				}
			}
		}
		if !inserted {
			owner.appendChild(block, layerIndentStep(owner))
		}
	}
	attrs := []xattr{{"name", name}}
	if value.Type != "" && value.Type != "string" {
		attrs = append(attrs, xattr{"type", value.Type})
	}
	if value.PropertyType != "" {
		attrs = append(attrs, xattr{"propertytype", value.PropertyType})
	}
	attrs = append(attrs, xattr{"value", value.Value})
	block.appendChild(newElem("property", attrs...), layerIndentStep(block))
	return nil
}

// sameTextPropertyValue compares the *meaning* of element content. Rewriting
// `a&#38;b` as `a&amp;b` for the very same value makes a no-op an unrelated XML
// diff in both TMX and TSX, even though either spelling parses to `a&b`.
func sameTextPropertyValue(prop *xelem, want string) bool {
	var body strings.Builder
	for _, child := range prop.kids {
		body.Write(child.raw)
	}
	var decoded struct {
		Value string `xml:",chardata"`
	}
	if err := xml.Unmarshal([]byte("<value>"+body.String()+"</value>"), &decoded); err != nil {
		return false
	}
	return decoded.Value == want
}

// RemoveObjectComponent removes only the named component's custom properties.
// Position is not a property: the caller must move the object instead.
func (d *Document) RemoveObjectComponent(id int, name string) error {
	if name == "" || strings.Contains(name, ".") {
		return fmt.Errorf("tiled: %q is not a component name", name)
	}
	obj, err := d.objectElement(id)
	if err != nil {
		return err
	}
	block := obj.firstChild("properties")
	if block == nil {
		return nil
	}
	changed := false
	for _, prop := range block.children("property") {
		comp, _, ok := strings.Cut(prop.attr("name"), ".")
		if ok && strings.EqualFold(comp, name) {
			block.removeChild(prop)
			changed = true
		}
	}
	if changed {
		if len(block.children("property")) == 0 && len(block.kids) == 0 {
			obj.removeChild(block)
		}
		d.invalidate()
	}
	return nil
}
