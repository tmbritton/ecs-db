package schema

import "fmt"

// Supported property type values.
const (
	PropertyTypeString     = "string"
	PropertyTypeInteger    = "integer"
	PropertyTypeNumber     = "number"
	PropertyTypeBoolean    = "boolean"
	PropertyTypeObject     = "object"
	PropertyTypeArray      = "array"
	PropertyTypeEntityRef  = "entity-ref"
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
