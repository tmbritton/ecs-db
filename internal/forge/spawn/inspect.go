package spawn

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/tilemap"
	"github.com/tmbritton/ecs-db/internal/world"
)

// Names is the component set the map actually describes, including Position
// from the object's coordinates. The importer makes precisely this set.
func Names(obj tiled.Object) []string {
	seen := map[string]bool{"Position": true}
	for key := range obj.Properties {
		if comp, _, ok := strings.Cut(key, "."); ok {
			seen[comp] = true
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func candidate(d *tiled.Document, id int) (*tiled.Map, tiled.Object, error) {
	if d == nil {
		return nil, tiled.Object{}, fmt.Errorf("there is no map to edit")
	}
	m, err := d.Map()
	if err != nil {
		return nil, tiled.Object{}, err
	}
	obj, err := object(m, id)
	if err != nil {
		return nil, tiled.Object{}, err
	}
	copy := make(tiled.Properties, len(obj.Properties))
	for name, value := range obj.Properties {
		copy[name] = value
	}
	obj.Properties = copy
	return m, obj, nil
}

func check(s *schema.DatabaseSchema, m *tiled.Map, obj tiled.Object) (world.ValidationResult, error) {
	if s == nil {
		return world.ValidationResult{}, fmt.Errorf("there is no schema to validate this spawn against")
	}
	verdict, err := tilemap.ValidateSpawn(s, m, obj)
	if err != nil {
		return verdict, err
	}
	if !verdict.Valid() {
		return verdict, fmt.Errorf("%s", strings.Join(verdict.Errors, "; "))
	}
	return verdict, nil
}

// SetProperty changes one field only after the engine parser and entity-type
// validator agree on the prospective object. The document remains unchanged on
// every refusal; the original authored property key spelling is kept.
func SetProperty(d *tiled.Document, s *schema.DatabaseSchema, id int, component, field, raw string) (world.ValidationResult, error) {
	m, obj, err := candidate(d, id)
	if err != nil {
		return world.ValidationResult{}, err
	}
	if s == nil {
		return world.ValidationResult{}, fmt.Errorf("there is no schema")
	}
	decl, canonical := schema.ComponentByName(s, component)
	if canonical == "" {
		return world.ValidationResult{}, fmt.Errorf("the schema has no component %q", component)
	}
	if strings.EqualFold(canonical, "Position") {
		return world.ValidationResult{}, fmt.Errorf("position comes from where the object sits; move it instead")
	}
	if strings.EqualFold(canonical, "Sprite") && strings.EqualFold(field, "sheet") {
		return world.ValidationResult{}, fmt.Errorf("animations.toml sets Sprite.sheet at startup")
	}
	var column string
	kind := decl.Type
	if schema.StorageLayout(decl.Type) == schema.LayoutColumns {
		var prop schema.Property
		prop, column = schema.PropertyByName(decl.Properties, field)
		if column == "" {
			return world.ValidationResult{}, fmt.Errorf("%s has no field %q", canonical, field)
		}
		kind = prop.Type
	} else {
		column = schema.LayoutColumnName(schema.StorageLayout(decl.Type))
		if !strings.EqualFold(field, column) {
			return world.ValidationResult{}, fmt.Errorf("%s stores its value in %s, not %s", canonical, column, field)
		}
	}
	key := canonical + "." + column
	matches := 0
	for existing := range obj.Properties {
		if strings.EqualFold(existing, key) {
			key = existing
			matches++
		}
	}
	if matches > 1 {
		return world.ValidationResult{}, fmt.Errorf("%s.%s is ambiguous: the object has several spellings of that property", canonical, column)
	}
	if matches == 0 {
		attached := false
		for _, name := range Names(obj) {
			if strings.EqualFold(name, canonical) {
				attached = true
				break
			}
		}
		if !attached {
			return world.ValidationResult{}, fmt.Errorf("%s is not attached to this spawn; add it first", canonical)
		}
	}
	p, err := typedProperty(kind, raw)
	if err != nil {
		return world.ValidationResult{}, err
	}
	obj.Properties[key] = p
	verdict, err := check(s, m, obj)
	if err != nil {
		return verdict, err
	}
	return verdict, d.SetObjectProperty(id, key, p)
}

func typedProperty(kind, raw string) (tiled.Property, error) {
	if kind == schema.PropertyTypeEntityRef {
		if _, err := positiveTarget(raw); err != nil {
			return tiled.Property{}, err
		}
		return tiled.Property{Type: "int", Value: raw}, nil
	}
	p, err := initialValue(kind)
	if err == nil {
		p.Value = raw
	}
	return p, err
}

func positiveTarget(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("an entity reference needs an explicit positive target entity id")
	}
	return id, nil
}

func initialProperties(name string, comp schema.Component) (tiled.Properties, error) {
	return initialPropertiesWithTargets(name, comp, nil)
}

func initialPropertiesWithTargets(name string, comp schema.Component, targets map[string]string) (tiled.Properties, error) {
	props := tiled.Properties{}
	if comp.Type == schema.ComponentTypeObject {
		names := make([]string, 0, len(comp.Properties))
		for field := range comp.Properties {
			names = append(names, field)
		}
		sort.Strings(names)
		for _, field := range names {
			kind := comp.Properties[field].Type
			var p tiled.Property
			var err error
			if kind == schema.PropertyTypeEntityRef {
				var id int64
				id, err = positiveTarget(targets[field])
				if err == nil {
					p = tiled.Property{Type: "int", Value: strconv.FormatInt(id, 10)}
				}
			} else {
				p, err = initialValue(kind)
			}
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", name, field, err)
			}
			props[name+"."+field] = p
		}
		return props, nil
	}
	if comp.Type == schema.ComponentTypeEntityRef {
		id, err := positiveTarget(targets["target_entity_id"])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		props[name+".target_entity_id"] = tiled.Property{Type: "int", Value: strconv.FormatInt(id, 10)}
		return props, nil
	}
	p, err := initialValue(comp.Type)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	props[name+"."+schema.LayoutColumnName(schema.StorageLayout(comp.Type))] = p
	return props, nil
}

// AddComponent supplies the fields of one permitted component with typed
// initial values. Required components absent from an imported file may also be
// repaired. Warnings come from the engine contract and are not discarded.
func AddComponent(d *tiled.Document, s *schema.DatabaseSchema, id int, name string) (world.ValidationResult, error) {
	return AddComponentWithTargets(d, s, id, name, nil)
}

// AddComponentWithTarget is AddComponent with an explicit target for a
// reference component. An absent reference cannot be invented as zero: SQLite
// would reject that FK when the engine imports the map.
func AddComponentWithTarget(d *tiled.Document, s *schema.DatabaseSchema, id int, name, target string) (world.ValidationResult, error) {
	return AddComponentWithTargets(d, s, id, name, map[string]string{"target_entity_id": target})
}

// AddComponentWithTargets accepts one target per entity-reference field of an
// object component (or target_entity_id for a scalar reference). All targets
// are checked before any property is written, so a half-filled form cannot
// leave the document with half an object component.
func AddComponentWithTargets(d *tiled.Document, s *schema.DatabaseSchema, id int, name string, targets map[string]string) (world.ValidationResult, error) {
	m, obj, err := candidate(d, id)
	if err != nil {
		return world.ValidationResult{}, err
	}
	if s == nil {
		return world.ValidationResult{}, fmt.Errorf("there is no schema")
	}
	comp, canonical := schema.ComponentByName(s, name)
	if canonical == "" {
		return world.ValidationResult{}, fmt.Errorf("the schema has no component %q", name)
	}
	if strings.EqualFold(canonical, "Position") {
		return world.ValidationResult{}, fmt.Errorf("position comes from the object's coordinates")
	}
	for _, attached := range Names(obj) {
		if strings.EqualFold(attached, canonical) {
			return world.ValidationResult{}, fmt.Errorf("%s is already attached", canonical)
		}
	}
	props, err := initialPropertiesWithTargets(canonical, comp, targets)
	if err != nil {
		return world.ValidationResult{}, err
	}
	for key, p := range props {
		obj.Properties[key] = p
	}
	verdict, err := check(s, m, obj)
	if err != nil {
		return verdict, err
	}
	return verdict, writeProperties(d, id, props)
}

func writeProperties(d *tiled.Document, id int, props tiled.Properties) error {
	// The keys passed here were absent from the object before validation.
	// Writing them in sorted order gives the map a stable diff and leaves no
	// existing nested XML to refuse halfway through an edit.
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		p := props[key]
		if err := d.SetObjectProperty(id, key, p); err != nil {
			return err
		}
	}
	return nil
}

// RepairRequired seeds every completely missing required component together.
// Doing one at a time would fail the engine's strict contract while any other
// required component was still absent, making the map impossible to repair.
func RepairRequired(d *tiled.Document, s *schema.DatabaseSchema, id int) (world.ValidationResult, error) {
	m, obj, err := candidate(d, id)
	if err != nil {
		return world.ValidationResult{}, err
	}
	if s == nil {
		return world.ValidationResult{}, fmt.Errorf("there is no schema")
	}
	et, ok := s.EntityTypes[obj.Type]
	if !ok {
		return world.ValidationResult{}, fmt.Errorf("unknown entity type %q", obj.Type)
	}
	attached := map[string]bool{}
	for _, name := range Names(obj) {
		attached[strings.ToLower(name)] = true
	}
	props := tiled.Properties{}
	for _, name := range et.RequiredComponents {
		if attached[strings.ToLower(name)] {
			continue
		}
		comp, ok := s.Components[name]
		if !ok {
			return world.ValidationResult{}, fmt.Errorf("required component %q is not declared", name)
		}
		initial, err := initialProperties(name, comp)
		if err != nil {
			return world.ValidationResult{}, err
		}
		for key, value := range initial {
			props[key] = value
			obj.Properties[key] = value
		}
	}
	verdict, err := check(s, m, obj)
	if err != nil {
		return verdict, err
	}
	return verdict, writeProperties(d, id, props)
}

// DetachComponent removes the named optional/extra component, never Position
// or a required component. Validation is of the resulting map, not a guess.
func DetachComponent(d *tiled.Document, s *schema.DatabaseSchema, id int, name string) (world.ValidationResult, error) {
	m, obj, err := candidate(d, id)
	if err != nil {
		return world.ValidationResult{}, err
	}
	if s == nil {
		return world.ValidationResult{}, fmt.Errorf("there is no schema")
	}
	if strings.EqualFold(name, "Position") {
		return world.ValidationResult{}, fmt.Errorf("position comes from the object's coordinates")
	}
	et, ok := s.EntityTypes[obj.Type]
	if !ok {
		return world.ValidationResult{}, fmt.Errorf("unknown entity type %q", obj.Type)
	}
	if et.IsComponentRequired(name) {
		return world.ValidationResult{}, fmt.Errorf("%s is required by %s and cannot be detached", name, obj.Type)
	}
	found := false
	for key := range obj.Properties {
		if comp, _, ok := strings.Cut(key, "."); ok && strings.EqualFold(comp, name) {
			delete(obj.Properties, key)
			found = true
		}
	}
	if !found {
		return world.ValidationResult{}, fmt.Errorf("%s is not attached", name)
	}
	verdict, err := check(s, m, obj)
	if err != nil {
		return verdict, err
	}
	return verdict, d.RemoveObjectComponent(id, name)
}
