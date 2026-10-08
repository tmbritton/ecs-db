// Package tilelinks edits the authored TMX references that the map importer
// resolves to TileReferences. Object IDs belong to the file, not the database.
package tilelinks

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/spawn"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

type Cell struct{ X, Y int }

func linkProperty(props tiled.Properties, wanted string) (tiled.Property, string, bool, error) {
	var value tiled.Property
	var spelling string
	for name, prop := range props {
		if strings.EqualFold(name, wanted) {
			if spelling != "" {
				return tiled.Property{}, "", false, fmt.Errorf("duplicate %s properties with different casing", wanted)
			}
			value, spelling = prop, name
		}
	}
	return value, spelling, spelling != "", nil
}

func linkLayer(props tiled.Properties) (int, bool, error) {
	prop, _, found, err := linkProperty(props, "TileLink.layerID")
	if err != nil || !found {
		return 0, found, err
	}
	id, err := strconv.Atoi(strings.TrimSpace(prop.Value))
	if err != nil || id <= 0 {
		return 0, true, fmt.Errorf("invalid TileLink.layerID %q", prop.Value)
	}
	return id, true, nil
}

// Inspection describes a painted layer cell and the authored object IDs that
// link to it. The owned template is a type declaration, not a spawned DB ID.
type Inspection struct {
	LayerID, X, Y int
	GID           uint32
	EntityType    string
	Empty         bool
	ObjectIDs     []int
}

func Inspect(m *tiled.Map, layerID int, at Cell) (Inspection, error) {
	if m == nil {
		return Inspection{}, fmt.Errorf("there is no map to inspect")
	}
	if at.X < 0 || at.Y < 0 || at.X >= m.Width || at.Y >= m.Height {
		return Inspection{}, fmt.Errorf("cell (%d,%d) is outside the map", at.X, at.Y)
	}
	var selected *tiled.Layer
	for i := range m.Layers {
		if layerID != m.Layers[i].ID || layerID <= 0 {
			continue
		}
		if selected != nil {
			return Inspection{}, fmt.Errorf("tile layer ID %d is duplicated", layerID)
		}
		selected = &m.Layers[i]
	}
	if selected == nil {
		return Inspection{}, fmt.Errorf("tile layer ID %d does not exist", layerID)
	}
	result := Inspection{LayerID: layerID, X: at.X, Y: at.Y, GID: selected.TileAt(at.X, at.Y).GID}
	if result.GID == 0 {
		result.Empty = true
		return result, nil
	}
	ref, local, found := m.TilesetFor(result.GID)
	if !found || ref.Tileset == nil {
		return Inspection{}, fmt.Errorf("tile layer %d cell (%d,%d) cannot resolve its tileset", layerID, at.X, at.Y)
	}
	result.EntityType = ref.Tileset.Tiles[local].Properties.Get("entityType")
	if result.EntityType == "" {
		result.EntityType = ref.Tileset.Properties.Get("entityType")
	}
	if result.EntityType == "" {
		return Inspection{}, fmt.Errorf("tile layer %d cell (%d,%d) has no entityType template", layerID, at.X, at.Y)
	}
	seen := make(map[int]bool)
	for _, group := range m.ObjectGroups {
		for _, obj := range group.Objects {
			linkedLayer, found, err := linkLayer(obj.Properties)
			if err != nil {
				continue // the object gets its own MAP validation mark
			}
			if !found || linkedLayer != layerID {
				continue
			}
			_, _, cells, err := footprint(m, obj)
			if err != nil {
				continue // bad object metadata must not hide a valid Tile
			}
			for _, cell := range cells {
				if cell == at {
					if seen[obj.ID] {
						return Inspection{}, fmt.Errorf("object ID %d is duplicated; the Tile reference is ambiguous", obj.ID)
					}
					seen[obj.ID] = true
					result.ObjectIDs = append(result.ObjectIDs, obj.ID)
				}
			}
		}
	}
	return result, nil
}

// ValidateSharedArt catches a TileLink edit that would substitute one object's
// visual for differently painted cells. A restriction-only object may instead
// link Tiles with separate per-cell visual references.
func ValidateSharedArt(m *tiled.Map, objectID int) error {
	return NewArtValidator(m).Validate(objectID)
}

type artKey struct {
	layerID int
	cell    Cell
	typ     string
}

type placementKey struct {
	layerIndex int
	cell       Cell
}

// ArtValidator shares its visual-provider and placement indexes across all
// linked objects in one map preview, rather than rescanning the entire map
// separately for every object on each SSE render.
type ArtValidator struct {
	m             *tiled.Map
	objects       map[int]tiled.Object
	counts        map[int]int
	visualOwners  map[artKey]bool
	placements    map[placementKey]tiled.Placement
	indexedVisual bool
}

func NewArtValidator(m *tiled.Map) *ArtValidator {
	v := &ArtValidator{m: m, objects: make(map[int]tiled.Object), counts: make(map[int]int)}
	if m != nil {
		for _, group := range m.ObjectGroups {
			for _, obj := range group.Objects {
				v.objects[obj.ID] = obj
				v.counts[obj.ID]++
			}
		}
	}
	return v
}

func (v *ArtValidator) hasVisualProvider(layerID int, at Cell, typ string) bool {
	if !v.indexedVisual {
		v.indexedVisual = true
		v.visualOwners = make(map[artKey]bool)
		for _, group := range v.m.ObjectGroups {
			for _, obj := range group.Objects {
				_, _, hasVisual, err := linkProperty(obj.Properties, "TileVisual.image")
				if err != nil || !hasVisual {
					continue
				}
				layer, _, cells, err := footprint(v.m, obj)
				if err != nil || layer == 0 {
					continue
				}
				for _, cell := range cells {
					v.visualOwners[artKey{layerID: layer, cell: cell, typ: obj.Type}] = true
				}
			}
		}
	}
	return v.visualOwners[artKey{layerID: layerID, cell: at, typ: typ}]
}

func (v *ArtValidator) placement(layerIndex int, at Cell) (tiled.Placement, bool) {
	if v.placements == nil {
		v.placements = make(map[placementKey]tiled.Placement)
		for _, placed := range v.m.AllPlacements() {
			v.placements[placementKey{layerIndex: placed.LayerIndex, cell: Cell{X: placed.X, Y: placed.Y}}] = placed
		}
	}
	placed, found := v.placements[placementKey{layerIndex: layerIndex, cell: at}]
	return placed, found
}

func (v *ArtValidator) Validate(objectID int) error {
	m := v.m
	if m == nil {
		return fmt.Errorf("there is no map to validate")
	}
	obj := v.objects[objectID]
	if objectID <= 0 {
		return fmt.Errorf("TileLink object ID %d must be positive", objectID)
	}
	if v.counts[objectID] != 1 || obj.Type == "" {
		return fmt.Errorf("object ID %d is missing, duplicated or untyped", objectID)
	}
	layerID, _, cells, err := footprint(m, obj)
	if err != nil || layerID == 0 {
		return err
	}
	return v.validateCells(obj, layerID, cells)
}

func (v *ArtValidator) validateCells(obj tiled.Object, layerID int, cells []Cell) error {
	m := v.m
	objectID := obj.ID
	layerIndex := -1
	for i, layer := range m.Layers {
		if layer.ID == layerID {
			if layerIndex != -1 {
				return fmt.Errorf("tile layer ID %d is duplicated", layerID)
			}
			layerIndex = i
		}
	}
	if layerIndex == -1 {
		return fmt.Errorf("tile layer ID %d does not exist", layerID)
	}
	_, _, hasVisual, err := linkProperty(obj.Properties, "TileVisual.image")
	if err != nil {
		return err
	}
	var first *tiled.Draw
	for _, cell := range cells {
		gid := m.Layers[layerIndex].TileAt(cell.X, cell.Y).GID
		if gid == 0 {
			return fmt.Errorf("object %d TileLink targets empty layer %d cell (%d,%d)", objectID, layerID, cell.X, cell.Y)
		}
		ref, local, found := m.TilesetFor(gid)
		if !found || ref.Tileset == nil {
			return fmt.Errorf("layer %d cell (%d,%d) cannot resolve its painted tileset", layerID, cell.X, cell.Y)
		}
		entityType := ref.Tileset.Tiles[local].Properties.Get("entityType")
		if entityType == "" {
			entityType = ref.Tileset.Properties.Get("entityType")
		}
		if entityType != obj.Type {
			continue
		}
		if !hasVisual && !v.hasVisualProvider(layerID, cell, obj.Type) {
			return fmt.Errorf("object %d replacing painted %s in layer %d needs TileVisual", objectID, obj.Type, layerID)
		}
		if !hasVisual {
			continue // another object supplies the painted type's visual
		}
		placed, found := v.placement(layerIndex, cell)
		if !found || placed.Problem != "" {
			return fmt.Errorf("layer %d cell (%d,%d) cannot resolve its artwork: %s", layerID, cell.X, cell.Y, placed.Problem)
		}
		draw := placed.Draw
		draw.DX -= cell.X * m.TileWidth
		draw.DY -= cell.Y * m.TileHeight
		draw.Alpha = 1 // TileLayer owns opacity, not the referenced visual.
		if first != nil && *first != draw {
			return fmt.Errorf("object %d shares one TileVisual across distinct artwork; use per-cell art references and one shared restriction", objectID)
		}
		first = &draw
	}
	return nil
}

func target(m *tiled.Map, objectID, layerID int, at Cell) (tiled.Object, error) {
	if m == nil || m.TileWidth <= 0 || m.TileHeight <= 0 {
		return tiled.Object{}, fmt.Errorf("there is no valid map to link")
	}
	var object tiled.Object
	objects := 0
	for _, group := range m.ObjectGroups {
		for _, candidate := range group.Objects {
			if candidate.ID == objectID {
				objects++
				object = candidate
			}
		}
	}
	if objects != 1 || object.Type == "" {
		return tiled.Object{}, fmt.Errorf("TileLink object id %d is missing, untyped or duplicated", objectID)
	}
	if err := validTargetCell(m, layerID, at); err != nil {
		return tiled.Object{}, err
	}
	return object, nil
}

func validTargetCell(m *tiled.Map, layerID int, at Cell) error {
	if at.X < 0 || at.Y < 0 || at.X >= m.Width || at.Y >= m.Height {
		return fmt.Errorf("cell (%d,%d) is outside the map", at.X, at.Y)
	}
	layers := 0
	for _, layer := range m.Layers {
		if layer.ID == layerID && layerID > 0 {
			layers++
			if layer.TileAt(at.X, at.Y).GID == 0 {
				return fmt.Errorf("layer %d cell (%d,%d) is empty", layerID, at.X, at.Y)
			}
		}
	}
	if layers != 1 {
		return fmt.Errorf("tile layer ID %d is missing or duplicated", layerID)
	}
	return nil
}

func footprint(m *tiled.Map, obj tiled.Object) (int, Cell, []Cell, error) {
	layerID, found, err := linkLayer(obj.Properties)
	if err != nil {
		return 0, Cell{}, nil, fmt.Errorf("object %d: %w", obj.ID, err)
	}
	if !found {
		if _, _, partial, err := linkProperty(obj.Properties, "TileLink.cells"); err != nil || partial {
			return 0, Cell{}, nil, fmt.Errorf("object %d has TileLink.cells without a layer ID", obj.ID)
		}
		return 0, Cell{}, nil, nil
	}
	x, y := obj.X/float64(m.TileWidth), obj.Y/float64(m.TileHeight)
	if obj.GID != 0 {
		y-- // Tiled tile objects use their bottom-left corner as their anchor.
	}
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || math.Trunc(x) != x || math.Trunc(y) != y {
		return 0, Cell{}, nil, fmt.Errorf("object %d is not anchored to a grid cell", obj.ID)
	}
	anchor := Cell{X: int(x), Y: int(y)}
	if prop, _, explicit, err := linkProperty(obj.Properties, "TileLink.cells"); err != nil {
		return 0, Cell{}, nil, err
	} else if explicit {
		offsets, err := tilemap.ParseOccupiedCells(prop.Value, tilemap.Point{X: anchor.X, Y: anchor.Y}, m.Width, m.Height)
		if err != nil {
			return 0, Cell{}, nil, fmt.Errorf("object %d TileLink.cells: %w", obj.ID, err)
		}
		cells := make([]Cell, 0, len(offsets))
		for _, offset := range offsets {
			cells = append(cells, Cell{anchor.X + offset.X, anchor.Y + offset.Y})
		}
		return layerID, anchor, cells, nil
	}
	return layerID, anchor, []Cell{anchor}, nil
}

// Link connects an existing typed object to a painted Tile on one stable layer.
// Repeating a link is a byte-identical no-op.
func Link(d *tiled.Document, objectID, layerID int, at Cell) (bool, error) {
	if d == nil {
		return false, fmt.Errorf("there is no map to link")
	}
	m, err := d.Map()
	if err != nil {
		return false, err
	}
	obj, err := target(m, objectID, layerID, at)
	if err != nil {
		return false, err
	}
	currentLayer, anchor, cells, err := footprint(m, obj)
	if err != nil {
		return false, err
	}
	if currentLayer != 0 && currentLayer != layerID {
		return false, fmt.Errorf("object %d already links layer %d; one object cannot link two layers", objectID, currentLayer)
	}
	if currentLayer != 0 {
		for _, cell := range cells {
			for _, layer := range m.Layers {
				if layer.ID == currentLayer && layer.TileAt(cell.X, cell.Y).GID == 0 {
					return false, fmt.Errorf("object %d already links empty layer %d cell (%d,%d)", objectID, currentLayer, cell.X, cell.Y)
				}
			}
		}
	}
	if currentLayer == 0 {
		// Even a newly linked object may sit away from the target cell.
		x, y := obj.X/float64(m.TileWidth), obj.Y/float64(m.TileHeight)
		if obj.GID != 0 {
			y--
		}
		if math.Trunc(x) != x || math.Trunc(y) != y || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
			return false, fmt.Errorf("object %d is not anchored to a grid cell", objectID)
		}
		anchor = Cell{int(x), int(y)}
	}
	for _, cell := range cells {
		if cell == at {
			return false, nil
		}
	}
	cells = append(cells, at)
	return setFootprint(d, obj, layerID, anchor, cells)
}

// CanLink previews the same footprint and shared-art contract without editing
// a Document. MAP uses it to explain an incompatible picker target *before*
// offering an action the POST must refuse.
func CanLink(m *tiled.Map, objectID, layerID int, at Cell) error {
	return NewArtValidator(m).CanLink(objectID, layerID, at)
}

// CanLink reuses resolved painted placements across candidates offered by one
// inspector render; only the single object's prospective links change.
func (v *ArtValidator) CanLink(objectID, layerID int, at Cell) error {
	m := v.m
	if m == nil || m.TileWidth <= 0 || m.TileHeight <= 0 {
		return fmt.Errorf("there is no valid map to link")
	}
	obj := v.objects[objectID]
	if v.counts[objectID] != 1 || obj.Type == "" {
		return fmt.Errorf("TileLink object id %d is missing, untyped or duplicated", objectID)
	}
	if err := validTargetCell(m, layerID, at); err != nil {
		return err
	}
	currentLayer, _, cells, err := footprint(m, obj)
	if err != nil {
		return err
	}
	if currentLayer != 0 && currentLayer != layerID {
		return fmt.Errorf("object %d already links layer %d", objectID, currentLayer)
	}
	if currentLayer == 0 {
		x, y := obj.X/float64(m.TileWidth), obj.Y/float64(m.TileHeight)
		if obj.GID != 0 {
			y--
		}
		if math.Trunc(x) != x || math.Trunc(y) != y || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
			return fmt.Errorf("object %d is not anchored to a grid cell", objectID)
		}
	}
	for _, cell := range cells {
		if cell == at {
			return nil
		}
	}
	cells = append(cells, at)
	return v.validateCells(obj, layerID, cells)
}

// Unlink removes one target; removing the last one leaves the typed object as
// an independent spawn, with its other authored properties and ID untouched.
func Unlink(d *tiled.Document, objectID, layerID int, at Cell) (bool, error) {
	if d == nil {
		return false, fmt.Errorf("there is no map to unlink")
	}
	m, err := d.Map()
	if err != nil {
		return false, err
	}
	obj, err := target(m, objectID, layerID, at)
	if err != nil {
		return false, err
	}
	currentLayer, anchor, cells, err := footprint(m, obj)
	if err != nil {
		return false, err
	}
	if currentLayer == 0 || currentLayer != layerID {
		return false, nil
	}
	remaining := make([]Cell, 0, len(cells))
	for _, cell := range cells {
		if cell != at {
			remaining = append(remaining, cell)
		}
	}
	if len(remaining) == len(cells) {
		return false, nil
	}
	if len(remaining) == 0 {
		return true, d.RemoveObjectComponent(objectID, "TileLink")
	}
	return setFootprint(d, obj, layerID, anchor, remaining)
}

func setFootprint(d *tiled.Document, obj tiled.Object, layerID int, anchor Cell, cells []Cell) (bool, error) {
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].Y != cells[j].Y {
			return cells[i].Y < cells[j].Y
		}
		return cells[i].X < cells[j].X
	})
	type offset struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	var offsets []offset
	for _, cell := range cells {
		offsets = append(offsets, offset{X: cell.X - anchor.X, Y: cell.Y - anchor.Y})
	}
	raw, err := json.Marshal(offsets)
	if err != nil {
		return false, err
	}
	// Preflight both writes against an identical document before touching the
	// session's own tree; Document validates one property at a time.
	trial, err := tiled.ParseDocument(d.Bytes(), "link-preview.tmx")
	if err != nil {
		return false, err
	}
	_, layerProperty, layerPresent, err := linkProperty(obj.Properties, "TileLink.layerID")
	if err != nil {
		return false, err
	}
	if !layerPresent {
		layerProperty = "TileLink.layerID"
	}
	_, cellsProperty, hadExplicitCells, err := linkProperty(obj.Properties, "TileLink.cells")
	if err != nil {
		return false, err
	}
	if !hadExplicitCells {
		cellsProperty = "TileLink.cells"
	}
	write := func(doc *tiled.Document) error {
		if err := doc.SetObjectProperty(obj.ID, layerProperty, tiled.Property{Type: "int", Value: strconv.Itoa(layerID)}); err != nil {
			return err
		}
		if len(cells) == 1 && cells[0] == anchor && !hadExplicitCells {
			return nil
		}
		return doc.SetObjectProperty(obj.ID, cellsProperty, tiled.Property{Value: string(raw)})
	}
	if err := write(trial); err != nil {
		return false, err
	}
	return true, write(d)
}

// SetLayerData applies a paint result while removing links to erased Tiles.
// Preview the entire edit on a detached document before changing the working
// map: otherwise the first removed link could commit ahead of a later refusal.
func SetLayerData(d *tiled.Document, layerIndex int, next []uint32) error {
	if d == nil {
		return fmt.Errorf("there is no map to paint")
	}
	m, err := d.Map()
	if err != nil {
		return err
	}
	if layerIndex < 0 || layerIndex >= len(m.Layers) {
		return fmt.Errorf("there is no tile layer %d", layerIndex)
	}
	layer := m.Layers[layerIndex]
	if len(next) != len(layer.Data) {
		return fmt.Errorf("tile layer %d needs %d cells, got %d", layer.ID, len(layer.Data), len(next))
	}
	removed := make(map[Cell]bool)
	for i, raw := range next {
		if tiled.TileOf(layer.Data[i]).GID != 0 && tiled.TileOf(raw).GID == 0 {
			removed[Cell{X: i % layer.Width, Y: i / layer.Width}] = true
		}
	}
	type removal struct {
		id   int
		cell Cell
	}
	var links []removal
	if len(removed) != 0 {
		for _, group := range m.ObjectGroups {
			for _, obj := range group.Objects {
				linkedLayer, found, err := linkLayer(obj.Properties)
				if err != nil {
					return err
				}
				if !found || linkedLayer != layer.ID {
					continue
				}
				_, _, cells, err := footprint(m, obj)
				if err != nil {
					return err
				}
				for _, cell := range cells {
					if removed[cell] {
						links = append(links, removal{id: obj.ID, cell: cell})
					}
				}
			}
		}
	}
	apply := func(doc *tiled.Document) error {
		for _, link := range links {
			if _, err := Unlink(doc, link.id, layer.ID, link.cell); err != nil {
				return err
			}
		}
		return doc.SetLayerData(layerIndex, next)
	}
	if len(links) > 0 {
		trial, err := tiled.ParseDocument(d.Bytes(), "paint-preview.tmx")
		if err != nil {
			return err
		}
		if err := apply(trial); err != nil {
			return err
		}
	}
	return apply(d)
}

// Move relocates an object anchor without dragging its linked Tiles with it.
// Unlinked spawns retain the ordinary MAP move behavior.
func Move(d *tiled.Document, id, x, y int) error {
	if d == nil {
		return fmt.Errorf("there is no map to move an object on")
	}
	m, err := d.Map()
	if err != nil {
		return err
	}
	var obj tiled.Object
	count := 0
	for _, group := range m.ObjectGroups {
		for _, candidate := range group.Objects {
			if candidate.ID == id {
				obj = candidate
				count++
			}
		}
	}
	if count != 1 || obj.Type == "" {
		return spawn.Move(d, id, x, y)
	}
	layerID, anchor, cells, err := footprint(m, obj)
	if err != nil {
		return err
	}
	if layerID == 0 {
		return spawn.Move(d, id, x, y)
	}
	if anchor == (Cell{X: x, Y: y}) {
		return nil
	}
	type offset struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	var offsets []offset
	for _, cell := range cells {
		offsets = append(offsets, offset{X: cell.X - x, Y: cell.Y - y})
	}
	raw, err := json.Marshal(offsets)
	if err != nil {
		return err
	}
	_, cellsProperty, found, err := linkProperty(obj.Properties, "TileLink.cells")
	if err != nil {
		return err
	}
	if !found {
		cellsProperty = "TileLink.cells"
	}
	apply := func(doc *tiled.Document) error {
		if err := spawn.Move(doc, id, x, y); err != nil {
			return err
		}
		return doc.SetObjectProperty(id, cellsProperty, tiled.Property{Value: string(raw)})
	}
	trial, err := tiled.ParseDocument(d.Bytes(), "move-preview.tmx")
	if err != nil {
		return err
	}
	if err := apply(trial); err != nil {
		return err
	}
	return apply(d)
}

// DeleteLayer removes references into a deleted tile layer while retaining
// each object as an independent spawn. A candidate verifies every object ID
// and layer edit before any of the working map's XML is changed.
func DeleteLayer(d *tiled.Document, index int) error {
	if d == nil {
		return fmt.Errorf("there is no map to delete a layer from")
	}
	m, err := d.Map()
	if err != nil {
		return err
	}
	if index < 0 || index >= len(m.Layers) {
		return fmt.Errorf("there is no tile layer %d", index)
	}
	layerID := m.Layers[index].ID
	var linked []int
	for _, group := range m.ObjectGroups {
		for _, obj := range group.Objects {
			linkLayerID, found, err := linkLayer(obj.Properties)
			if err != nil {
				return fmt.Errorf("object %d: %w", obj.ID, err)
			}
			if found && linkLayerID == layerID {
				linked = append(linked, obj.ID)
			}
		}
	}
	if len(linked) == 0 {
		return d.DeleteLayer(index)
	}
	apply := func(doc *tiled.Document) error {
		for _, id := range linked {
			if err := doc.RemoveObjectComponent(id, "TileLink"); err != nil {
				return err
			}
		}
		return doc.DeleteLayer(index)
	}
	trial, err := tiled.ParseDocument(d.Bytes(), "layer-preview.tmx")
	if err != nil {
		return err
	}
	if err := apply(trial); err != nil {
		return err
	}
	return apply(d)
}
