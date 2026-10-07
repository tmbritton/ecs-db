package tiled

import (
	"fmt"
	"strconv"
	"strings"
)

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
	block := obj.firstChild("properties")
	if block != nil {
		var found *xelem
		for _, prop := range block.children("property") {
			if prop.attr("name") != name {
				continue
			}
			if found != nil {
				return fmt.Errorf("tiled: object %d has duplicate property %q", id, name)
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
			} else {
				// Tiled also accepts the element-content spelling. Retain it
				// instead of inventing a value attribute and leaving the old
				// text in the file as a second, conflicting value.
				found.setText(value.Value)
			}
			d.invalidate()
			return nil
		}
	} else {
		block = newElem("properties")
		obj.appendChild(block, layerIndentStep(obj))
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
	d.invalidate()
	return nil
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
