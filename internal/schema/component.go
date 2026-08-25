package schema

import (
	"encoding/json"
	"fmt"
	"sort"
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

// ComponentTypes is every component type a schema may declare, sorted.
//
// Exported so that a caller which has to handle all of them can enumerate them
// rather than keep a second list — storage's layout-agreement test builds a
// real table for each, and a type added here fails that test until the layout
// knows what shape it is.
func ComponentTypes() []string {
	out := make([]string, 0, len(supportedComponentTypes))
	for t := range supportedComponentTypes {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// PropertyTypes is every property type a schema may declare, sorted.
//
// Exported for the same reason ComponentTypes is: a caller that has to answer
// for all of them enumerates rather than keeps a second list. See
// ColumnReference, whose test would otherwise pass for a type nobody had
// thought about.
func PropertyTypes() []string {
	out := make([]string, 0, len(supportedPropertyTypes))
	for t := range supportedPropertyTypes {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

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
	Behavior   string              `json:"behavior,omitempty"`
	Properties map[string]Property `json:"properties,omitempty"`
	Items      *Property           `json:"items,omitempty"`

	// PropertyOrder records the authored order of Properties, on the same terms
	// as DatabaseSchema.ComponentOrder. See that field for why.
	PropertyOrder []string `json:"-"`
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
