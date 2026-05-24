package schema

import (
	"encoding/json"
	"fmt"
)

// Supported component type values.
const (
	ComponentTypeObject    = "object"
	ComponentTypeArray     = "array"
	ComponentTypeEntityRef = "entity-ref"
	ComponentTypeString    = "string"
	ComponentTypeInteger   = "integer"
	ComponentTypeNumber    = "number"
	ComponentTypeBoolean   = "boolean"
)

var supportedComponentTypes = map[string]bool{
	ComponentTypeObject:    true,
	ComponentTypeArray:     true,
	ComponentTypeEntityRef: true,
	ComponentTypeString:    true,
	ComponentTypeInteger:   true,
	ComponentTypeNumber:    true,
	ComponentTypeBoolean:   true,
}

// Component represents one entry in the top-level "components" map of
// schema.json. It is unmarshalled polymorphically based on the "type" field.
type Component struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Items      *Property           `json:"items,omitempty"`
}

// UnmarshalJSON implements polymorphic decoding based on the "type" field.
// It validates that the type is recognised and that the structural fields
// (Properties for object, Items for array) are consistent.
func (c *Component) UnmarshalJSON(data []byte) error {
	// First pass: extract the raw type key.
	var raw struct {
		Type       string          `json:"type"`
		Properties json.RawMessage `json:"properties"`
		Items      json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type == "" {
		return fmt.Errorf("component has no %q field", "type")
	}
	if !supportedComponentTypes[raw.Type] {
		return fmt.Errorf("unknown component type %q: must be %s",
			raw.Type, supportedComponentTypeList())
	}
	c.Type = raw.Type

	// Second pass: decode into the full struct.
	type componentAlias Component
	alias := (*componentAlias)(c)
	if err := json.Unmarshal(data, alias); err != nil {
		return err
	}

	// Structural validation.
	switch c.Type {
	case ComponentTypeObject:
		if len(c.Properties) == 0 {
			return fmt.Errorf("component type %q must define %q with at least one property",
				ComponentTypeObject, "properties")
		}
		for name, prop := range c.Properties {
			if err := prop.Validate(); err != nil {
				return fmt.Errorf("component property %q: %w", name, err)
			}
		}
	case ComponentTypeArray:
		if c.Items == nil {
			return fmt.Errorf("component type %q must define %q",
				ComponentTypeArray, "items")
		}
		if err := c.Items.Validate(); err != nil {
			return fmt.Errorf("component %q items: %w", ComponentTypeArray, err)
		}
	}
	return nil
}

func supportedComponentTypeList() string {
	types := []string{
		ComponentTypeObject,
		ComponentTypeArray,
		ComponentTypeEntityRef,
		ComponentTypeString,
		ComponentTypeInteger,
		ComponentTypeNumber,
		ComponentTypeBoolean,
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
