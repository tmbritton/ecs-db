// Package spawn applies map-object edits in the same coordinate and identity
// conventions the engine uses when it imports a Tiled map.
package spawn

import (
	"fmt"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

// Reason says why an entity type cannot be placed on the map, or is empty
// when it can. The palette and the write route use this same rule.
func Reason(s *schema.DatabaseSchema, name string) string {
	if s == nil {
		return "there is no schema open"
	}
	et, ok := s.EntityTypes[name]
	if !ok {
		return fmt.Sprintf("%q is not an entity type in this schema", name)
	}
	if name == "Tile" {
		return "Tile is owned by the map's tile importer"
	}
	if !et.IsComponentRequired("Position") && !et.IsComponentOptional("Position") {
		return "this type does not declare Position"
	}
	for _, required := range et.RequiredComponents {
		if required == "Position" {
			continue
		}
		comp, ok := s.Components[required]
		if !ok {
			return fmt.Sprintf("required component %q is not declared", required)
		}
		if comp.Type == schema.ComponentTypeEntityRef {
			return fmt.Sprintf("%s needs a target entity before it can be placed", required)
		}
		for _, field := range comp.Properties {
			if field.Type == schema.PropertyTypeEntityRef {
				return fmt.Sprintf("%s needs a target entity before it can be placed", required)
			}
		}
	}
	return ""
}

func cell(m *tiled.Map, x, y int) error {
	if m == nil || m.TileWidth <= 0 || m.TileHeight <= 0 ||
		x < 0 || y < 0 || x >= m.Width || y >= m.Height {
		return fmt.Errorf("cell %d,%d is outside the map", x, y)
	}
	return nil
}

// Place adds one point object to the explicitly chosen object group. Its id is
// allocated by Document, never by the browser or a caller.
func Place(d *tiled.Document, s *schema.DatabaseSchema, group int, kind string, x, y int) (int, error) {
	if d == nil {
		return 0, fmt.Errorf("there is no map to place a spawn on")
	}
	if reason := Reason(s, kind); reason != "" {
		return 0, fmt.Errorf("cannot place %s: %s", kind, reason)
	}
	m, err := d.Map()
	if err != nil {
		return 0, err
	}
	if group < 0 || group >= len(m.ObjectGroups) {
		return 0, fmt.Errorf("there is no object group %d", group)
	}
	if err := cell(m, x, y); err != nil {
		return 0, err
	}
	props := tiled.Properties{}
	for _, name := range s.EntityTypes[kind].RequiredComponents {
		if name == "Position" {
			continue
		}
		comp, exists := s.Components[name]
		if !exists {
			return 0, fmt.Errorf("required component %q is not declared", name)
		}
		initial, err := initialProperties(name, comp)
		if err != nil {
			return 0, err
		}
		for key, value := range initial {
			props[key] = value
		}
	}
	return d.AddObject(group, tiled.Object{
		Type: kind, X: float64(x * m.TileWidth), Y: float64(y * m.TileHeight), Properties: props,
	})
}

// initialValue is the same zero-value convention SQLite's generated columns
// use. A reference has no valid zero target, so the palette cannot guess one.
func initialValue(kind string) (tiled.Property, error) {
	switch kind {
	case schema.PropertyTypeInteger:
		return tiled.Property{Type: "int", Value: "0"}, nil
	case schema.PropertyTypeNumber:
		return tiled.Property{Type: "float", Value: "0"}, nil
	case schema.PropertyTypeString:
		return tiled.Property{Value: ""}, nil
	case schema.PropertyTypeBoolean:
		return tiled.Property{Type: "bool", Value: "false"}, nil
	case schema.PropertyTypeArray:
		return tiled.Property{Type: "string", Value: "[]"}, nil
	case schema.PropertyTypeObject:
		return tiled.Property{Type: "string", Value: "{}"}, nil
	default:
		return tiled.Property{}, fmt.Errorf("a %s field needs an authored value before it can be spawned", kind)
	}
}

func object(m *tiled.Map, id int) (tiled.Object, error) {
	var selected tiled.Object
	count := 0
	for _, group := range m.ObjectGroups {
		for _, o := range group.Objects {
			if o.ID == id {
				count++
				selected = o
			}
		}
	}
	if count > 1 {
		return tiled.Object{}, fmt.Errorf("duplicate object id %d: which object to edit is ambiguous", id)
	}
	if count == 0 || selected.Type == "" {
		return tiled.Object{}, fmt.Errorf("there is no spawn with object id %d", id)
	}
	return selected, nil
}

// Move keeps the object id and all authored properties. Tile objects are
// bottom-left anchored in Tiled; point objects use top-left coordinates.
func Move(d *tiled.Document, id, x, y int) error {
	if d == nil {
		return fmt.Errorf("there is no map to move a spawn on")
	}
	m, err := d.Map()
	if err != nil {
		return err
	}
	if err := cell(m, x, y); err != nil {
		return err
	}
	o, err := object(m, id)
	if err != nil {
		return err
	}
	px, py := float64(x*m.TileWidth), float64(y*m.TileHeight)
	if o.GID != 0 {
		py += float64(m.TileHeight)
	}
	return d.MoveObject(id, px, py)
}

// Delete removes the authored spawn. The engine will remove its entity on the
// next map import; Document never recycles the id it held.
func Delete(d *tiled.Document, id int) error {
	if d == nil {
		return fmt.Errorf("there is no map to delete a spawn from")
	}
	m, err := d.Map()
	if err != nil {
		return err
	}
	if _, err := object(m, id); err != nil {
		return err
	}
	return d.RemoveObject(id)
}
