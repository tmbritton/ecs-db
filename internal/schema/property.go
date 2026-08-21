package schema

import (
	"encoding/json"
	"fmt"

	"github.com/tmbritton/ecs-db/internal/jsonorder"
)

// Supported property type values.
const (
	PropertyTypeString    = "string"
	PropertyTypeInteger   = "integer"
	PropertyTypeNumber    = "number"
	PropertyTypeBoolean   = "boolean"
	PropertyTypeObject    = "object"
	PropertyTypeArray     = "array"
	PropertyTypeEntityRef = "entity-ref"
)

var supportedPropertyTypes = map[string]bool{
	PropertyTypeString:    true,
	PropertyTypeInteger:   true,
	PropertyTypeNumber:    true,
	PropertyTypeBoolean:   true,
	PropertyTypeObject:    true,
	PropertyTypeArray:     true,
	PropertyTypeEntityRef: true,
}

// Property defines a single field within a component.
//
// For object-type components the Properties map holds the nested field
// definitions. For array-type components the Items pointer describes
// the type of each element. Primitive types (string, integer, number,
// boolean) and entity-ref have no children.
type Property struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Items      *Property           `json:"items,omitempty"`

	// PropertyOrder records the authored order of Properties, on the same
	// terms as Component.PropertyOrder — see DatabaseSchema.ComponentOrder for
	// why order is kept at all. Recorded by UnmarshalJSON below, which is what
	// makes it work at any nesting depth.
	PropertyOrder []string `json:"-"`
}

// UnmarshalJSON decodes a property and records the order its own nested
// properties appeared in.
//
// It lives here rather than in LoadSchema's walk so that it recurses for free:
// a property nested inside an array's items inside another object records its
// order the same way the top level does. Without it, nested properties were
// alphabetised on save — a whole-block diff in a file nobody reordered, which
// is exactly what the order tracking exists to prevent, one level down.
func (p *Property) UnmarshalJSON(data []byte) error {
	type propertyAlias Property // avoids recursing into this method
	alias := (*propertyAlias)(p)
	if err := json.Unmarshal(data, alias); err != nil {
		return err
	}

	var section struct {
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(data, &section); err != nil {
		return err
	}
	order, err := jsonorder.Keys(section.Properties)
	if err != nil {
		return fmt.Errorf("reading property order: %w", err)
	}
	p.PropertyOrder = order
	return nil
}

// Validate returns a descriptive error if the property definition is
// structurally invalid (unknown type, object without properties, array
// without items, etc.). It recurses through nested objects/arrays.
func (p Property) Validate() error {
	if p.Type == "" {
		return fmt.Errorf("property type is required")
	}
	if !supportedPropertyTypes[p.Type] {
		return fmt.Errorf("unsupported property type %q: must be %s",
			p.Type, supportedTypeList())
	}
	switch p.Type {
	case PropertyTypeObject:
		if len(p.Properties) == 0 {
			return fmt.Errorf("property of type %q must have at least one nested property",
				PropertyTypeObject)
		}
		for name, child := range p.Properties {
			if err := child.Validate(); err != nil {
				return fmt.Errorf("property %q: %w", name, err)
			}
		}
	case PropertyTypeArray:
		if p.Items == nil {
			return fmt.Errorf("property of type %q must specify Items",
				PropertyTypeArray)
		}
		if err := p.Items.Validate(); err != nil {
			return fmt.Errorf("property %q items: %w", "array", err)
		}
	}
	return nil
}

// IsSupportedPropertyType reports whether t is a recognised property type.
func IsSupportedPropertyType(t string) bool {
	return supportedPropertyTypes[t]
}

func supportedTypeList() string {
	types := []string{
		PropertyTypeString,
		PropertyTypeInteger,
		PropertyTypeNumber,
		PropertyTypeBoolean,
		PropertyTypeObject,
		PropertyTypeArray,
		PropertyTypeEntityRef,
	}
	s := ""
	for i, t := range types {
		if i > 0 {
			s += ", "
		}
		s += t
	}
	return s
}
