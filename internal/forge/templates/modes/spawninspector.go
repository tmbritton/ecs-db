package modes

import (
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/spawn"
	"github.com/tmbritton/ecs-db/internal/jsonorder"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/world"
)

type spawnFieldView struct {
	Name, Value, Type string
	ContextSeeded     bool
	Sheet             bool
}

type spawnComponentView struct {
	Name, Position string
	Required       bool
	Present        bool
	Seeded         bool
	Fields         []spawnFieldView
}

// spawnComponentViews combines the file's actual properties with the type's
// contract. Required components are visible even if a hand-authored object
// omitted them, so the omission is fixable rather than invisible.
func spawnComponentViews(data Data) []spawnComponentView {
	if data.SelectedSpawn == nil {
		return nil
	}
	obj := *data.SelectedSpawn
	et := data.Schema.EntityTypes[obj.Type]
	attached := map[string]bool{}
	for _, name := range canonicalSpawnNames(data.Schema, obj) {
		attached[name] = true
	}
	ordered := []string{"Position"}
	seen := map[string]bool{"Position": true}
	for _, name := range et.RequiredComponents {
		if !seen[name] {
			ordered = append(ordered, name)
			seen[name] = true
		}
	}
	for _, name := range componentNames(data.Schema) {
		if attached[name] && !seen[name] {
			ordered = append(ordered, name)
			seen[name] = true
		}
	}
	// Unknown components in an externally authored map are not dropped from
	// the inspector. They are where its engine refusal came from.
	var unknown []string
	for name := range attached {
		if !seen[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	ordered = append(ordered, unknown...)

	_, seeds := seedsFor(data, et)
	out := make([]spawnComponentView, 0, len(ordered))
	for _, name := range ordered {
		view := spawnComponentView{Name: name, Present: attached[name], Required: et.IsComponentRequired(name)}
		if name == "Position" {
			view.Required = true
			view.Position = spawnCellLabel(obj, data.Canvas.TileW, data.Canvas.TileH)
		} else if comp, ok := data.Schema.Components[name]; ok {
			view.Fields = spawnFields(obj.Properties, name, comp, seeds)
			for _, f := range view.Fields {
				view.Seeded = view.Seeded || f.ContextSeeded
			}
		}
		out = append(out, view)
	}
	return out
}

func spawnFields(properties tiled.Properties, name string, comp schema.Component, seeds []Seed) []spawnFieldView {
	var fields []string
	types := map[string]string{}
	if schema.StorageLayout(comp.Type) == schema.LayoutColumns {
		fields = jsonorder.Apply(comp.PropertyOrder, comp.Properties)
		for _, field := range fields {
			types[field] = comp.Properties[field].Type
		}
	} else {
		field := schema.LayoutColumnName(schema.StorageLayout(comp.Type))
		fields = []string{field}
		types[field] = comp.Type
	}
	out := make([]spawnFieldView, 0, len(fields))
	for _, field := range fields {
		view := spawnFieldView{Name: field, Type: types[field], Sheet: name == "Sprite" && field == "sheet"}
		for key, p := range properties {
			if strings.EqualFold(key, name+"."+field) {
				view.Value = p.Value
				break
			}
		}
		for _, seed := range seeds {
			if seed.Component == name && seed.Key == field {
				view.ContextSeeded = true
			}
		}
		out = append(out, view)
	}
	return out
}

func spawnAddable(data Data) []string {
	if data.SelectedSpawn == nil {
		return nil
	}
	obj := data.SelectedSpawn
	et := data.Schema.EntityTypes[obj.Type]
	attached := map[string]bool{}
	for _, name := range canonicalSpawnNames(data.Schema, *obj) {
		attached[name] = true
	}
	out := []string{}
	for _, name := range componentNames(data.Schema) {
		if name == "Position" || attached[name] || et.IsComponentRequired(name) {
			continue
		}
		if et.IsComponentOptional(name) || et.AllowExtraComponents {
			out = append(out, name)
		}
	}
	return out
}

func spawnMissingRequired(data Data) []string {
	var out []string
	for _, component := range spawnComponentViews(data) {
		if component.Required && !component.Present {
			out = append(out, component.Name)
		}
	}
	return out
}

func canonicalSpawnNames(current schema.DatabaseSchema, obj tiled.Object) []string {
	seen := map[string]bool{}
	for _, name := range spawn.Names(obj) {
		if _, canonical := schema.ComponentByName(&current, name); canonical != "" {
			name = canonical
		}
		seen[name] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func spawnAddableRegular(data Data) []string {
	var out []string
	for _, name := range spawnAddable(data) {
		if len(spawnReferenceFields(data.Schema.Components[name])) == 0 {
			out = append(out, name)
		}
	}
	return out
}

func spawnAddableReferences(data Data) []string {
	var out []string
	for _, name := range spawnAddable(data) {
		if len(spawnReferenceFields(data.Schema.Components[name])) > 0 {
			out = append(out, name)
		}
	}
	return out
}

func spawnReferenceFields(comp schema.Component) []string {
	if comp.Type == schema.ComponentTypeEntityRef {
		return []string{"target_entity_id"}
	}
	if comp.Type != schema.ComponentTypeObject {
		return nil
	}
	var out []string
	for _, name := range jsonorder.Apply(comp.PropertyOrder, comp.Properties) {
		if comp.Properties[name].Type == schema.PropertyTypeEntityRef {
			out = append(out, name)
		}
	}
	return out
}

func spawnReferenceTestID(name string, comp schema.Component, field string) string {
	if comp.Type == schema.ComponentTypeEntityRef {
		return "spawn-target-" + name
	}
	return "spawn-target-" + name + "." + field
}

func spawnContextProblems(data Data) []string {
	if data.SelectedSpawn == nil {
		return nil
	}
	et := data.Schema.EntityTypes[data.SelectedSpawn.Type]
	_, seeds := seedsFor(data, et)
	var out []string
	seen := map[string]bool{}
	for _, seed := range seeds {
		if seed.Component == "" || seen[seed.Component] {
			continue
		}
		seen[seed.Component] = true
		verdict := world.ValidateAttachComponent(&data.Schema, data.SelectedSpawn.Type, seed.Component, false)
		if len(verdict.Errors) > 0 {
			out = append(out, "This type forbids "+seed.Component+", which its machine seeds; the engine will refuse the binding.")
		} else if len(verdict.Warnings) > 0 {
			out = append(out, "This type forbids "+seed.Component+", which its machine seeds; validationLevel warning means the binding proceeds with a warning.")
		}
	}
	return out
}

func spawnFieldAction(data Data, component, field string) string {
	return valueAction("/forge/map/spawn/property", "value", "map", data.SelectedMap,
		"id", itoa(data.SelectedSpawn.ID), "component", component, "field", field)
}

func spawnAddAction(data Data) string {
	return valueAction("/forge/map/spawn/component", "add", "map", data.SelectedMap,
		"id", itoa(data.SelectedSpawn.ID))
}

func spawnDetachAction(data Data, component string) string {
	return action("/forge/map/spawn/component", "map", data.SelectedMap,
		"id", itoa(data.SelectedSpawn.ID), "detach", component)
}

func spawnReferenceAction(data Data, name string) string {
	base := action("/forge/map/spawn/component", "map", data.SelectedMap,
		"id", itoa(data.SelectedSpawn.ID), "add", name)
	inner := strings.TrimSuffix(strings.TrimPrefix(base, "@post('"), "')")
	expr := "@post('" + inner + "'"
	for i, field := range spawnReferenceFields(data.Schema.Components[name]) {
		expr += " + '&target." + urlValue(field) + "=' + encodeURIComponent(evt.target.parentElement.querySelectorAll('[data-ref-field]')[" + itoa(i) + "].value)"
	}
	return expr + ")"
}

func spawnRepairAction(data Data) string {
	return action("/forge/map/spawn/component", "map", data.SelectedMap,
		"id", itoa(data.SelectedSpawn.ID), "repair", "required")
}
