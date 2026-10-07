package tiled

import (
	"fmt"
	"strconv"
	"strings"
)

// RenameLayer changes one tile layer's authored name, by flattened layer
// index. A second layer with the same name would make both the layer panel and
// per-cell data attributes ambiguous, so the editor refuses the collision.
func (d *Document) RenameLayer(index int, name string) error {
	layers := d.tileLayers()
	if index < 0 || index >= len(layers) {
		return fmt.Errorf("tiled: %s has no tile layer %d", d.name, index)
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("tiled: a layer needs a name")
	}
	for i, layer := range layers {
		if i != index && layer.attr("name") == name {
			return fmt.Errorf("tiled: layer %q already exists", name)
		}
	}
	layers[index].setAttr("name", name)
	d.invalidate()
	return nil
}

// CanMoveLayer reports whether the neighbor shares this layer's XML parent and
// the map has unique IDs with which the browser can track layers after a move.
func (d *Document) CanMoveLayer(index, direction int) bool {
	layers := d.tileLayers()
	if (direction != -1 && direction != 1) || index < 0 || index >= len(layers) ||
		index+direction < 0 || index+direction >= len(layers) ||
		layers[index].parent != layers[index+direction].parent {
		return false
	}
	return uniqueLayerIDs(layers)
}

func uniqueLayerIDs(layers []*xelem) bool {
	seen := make(map[int]bool, len(layers))
	for _, layer := range layers {
		id, err := strconv.Atoi(layer.attr("id"))
		if err != nil || id <= 0 || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// MoveLayer swaps two adjacent tile layers under one XML parent. Reparenting a
// layer across folders would discard that folder's authored meaning, so a
// cross-folder move is refused instead of quietly unpacking anything.
func (d *Document) MoveLayer(index, direction int) error {
	layers := d.tileLayers()
	if index < 0 || index >= len(layers) || (direction != -1 && direction != 1) ||
		index+direction < 0 || index+direction >= len(layers) {
		return fmt.Errorf("tiled: layer %d cannot move %d in %s", index, direction, d.name)
	}
	a, b := layers[index], layers[index+direction]
	if a.parent != b.parent {
		return fmt.Errorf("tiled: layer %q cannot move across a layer folder", a.attr("name"))
	}
	if !d.CanMoveLayer(index, direction) {
		return fmt.Errorf("tiled: layer %q cannot move without unique positive layer IDs to preserve view state", a.attr("name"))
	}
	parent := a.parent
	var ai, bi int
	for i, node := range parent.kids {
		if node.el == a {
			ai = i
		}
		if node.el == b {
			bi = i
		}
	}
	if ai > bi {
		ai, bi = bi, ai
	}
	// Comments and processing instructions immediately before a layer describe
	// that layer; move its entire leading run with it. Siblings between the two
	// tile layers (including image/object layers) remain in their own order.
	first, second := layerLead(parent.kids, ai), layerLead(parent.kids, bi)
	kids := parent.kids
	reordered := make([]xnode, 0, len(kids))
	reordered = append(reordered, kids[:first]...)
	reordered = append(reordered, kids[second:bi+1]...)
	reordered = append(reordered, kids[ai+1:second]...)
	reordered = append(reordered, kids[first:ai+1]...)
	reordered = append(reordered, kids[bi+1:]...)
	parent.kids = reordered
	parent.markDirty()
	d.invalidate()
	return nil
}

func layerLead(kids []xnode, at int) int {
	for at > 0 && kids[at-1].el == nil {
		at--
	}
	return at
}

// CanDeleteLayer reports whether deleting a layer can leave existing view
// signals attached to every surviving layer. Older maps without unique IDs
// still support paint and rename, but not index-changing edits.
func (d *Document) CanDeleteLayer(index int) bool {
	layers := d.tileLayers()
	return index >= 0 && index < len(layers) && uniqueLayerIDs(layers)
}

// DeleteLayer removes the tile layer with its preceding indentation and
// authored commentary, leaving its siblings and unrelated XML intact. Layers
// without unique IDs cannot be removed safely while the browser holds a paint
// selection for their old indices.
func (d *Document) DeleteLayer(index int) error {
	layers := d.tileLayers()
	if index < 0 || index >= len(layers) {
		return fmt.Errorf("tiled: %s has no tile layer %d", d.name, index)
	}
	if !d.CanDeleteLayer(index) {
		return fmt.Errorf("tiled: cannot delete a layer without unique positive layer IDs to preserve view state")
	}
	layer := layers[index]
	parent := layer.parent
	for i, node := range parent.kids {
		if node.el == layer {
			start := layerLead(parent.kids, i)
			parent.kids = append(parent.kids[:start], parent.kids[i+1:]...)
			parent.markDirty()
			break
		}
	}
	d.invalidate()
	return nil
}
