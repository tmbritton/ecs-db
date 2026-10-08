package schema

import (
	"fmt"
	"strings"
)

// InteractionRule describes what lets an entity pass through an occupant or
// see past it. Capabilities are separate boolean components, not enum modes.
type InteractionRule struct {
	Open   bool     `json:"open"`
	Allows []string `json:"allows"`
}

// AllowsInteraction applies one schema-declared category to the active
// capabilities of one mover or observer. Unknown labels are errors, not open.
func (s DatabaseSchema) AllowsInteraction(component, category string, abilities map[string]bool) (bool, error) {
	rules, ok := s.Interactions[component]
	if !ok {
		return false, fmt.Errorf("no interaction rules for %q", component)
	}
	rule, ok := rules[category]
	if !ok {
		return false, fmt.Errorf("unknown %s category %q", component, category)
	}
	if rule.Open {
		return true, nil
	}
	for _, name := range rule.Allows {
		if abilities[name] {
			return true, nil
		}
	}
	return false, nil
}

func validateInteractions(s DatabaseSchema) error {
	for name := range s.Components {
		for _, canonical := range []string{"Passability", "Visibility", "OccupiedCells", "TileReferences"} {
			if strings.EqualFold(name, canonical) && name != canonical {
				return fmt.Errorf("spatial component %q must use canonical name %q to match its database interaction", name, canonical)
			}
		}
	}
	if footprint, declared := s.Components["OccupiedCells"]; declared {
		if footprint.Type != ComponentTypeArray || footprint.Items == nil ||
			footprint.Items.Type != PropertyTypeObject || len(footprint.Items.Properties) != 2 ||
			footprint.Items.Properties["x"].Type != PropertyTypeInteger ||
			footprint.Items.Properties["y"].Type != PropertyTypeInteger {
			return fmt.Errorf("OccupiedCells must be an array of objects with integer x and y offsets")
		}
	}
	if references, declared := s.Components["TileReferences"]; declared {
		if references.Type != ComponentTypeArray || references.Items == nil || references.Items.Type != PropertyTypeEntityRef {
			return fmt.Errorf("TileReferences must be an array of entity-ref IDs")
		}
	}
	for _, restriction := range []string{"Passability", "Visibility"} {
		if _, declared := s.Components[restriction]; declared {
			if _, hasRules := s.Interactions[restriction]; !hasRules {
				return fmt.Errorf("component %q has no interaction categories", restriction)
			}
		}
	}
	for restriction, categories := range s.Interactions {
		comp, found := s.Components[restriction]
		if !found {
			return fmt.Errorf("interactions names undeclared component %q", restriction)
		}
		kind, found := comp.Properties["kind"]
		if comp.Type != ComponentTypeObject || !found || kind.Type != PropertyTypeString {
			return fmt.Errorf("interaction component %q must have a string kind property", restriction)
		}
		if len(categories) == 0 {
			return fmt.Errorf("interaction component %q must declare a category", restriction)
		}
		for category, rule := range categories {
			if strings.TrimSpace(category) == "" {
				return fmt.Errorf("interaction component %q has an empty category", restriction)
			}
			seen := make(map[string]bool, len(rule.Allows))
			for _, name := range rule.Allows {
				if seen[name] {
					return fmt.Errorf("interaction category %q has duplicate capability %q", category, name)
				}
				seen[name] = true
				ability, found := s.Components[name]
				if !found {
					return fmt.Errorf("interaction category %q references undeclared capability %q", category, name)
				}
				if ability.Type != ComponentTypeBoolean {
					return fmt.Errorf("interaction capability %q must be a boolean component", name)
				}
			}
		}
	}
	return nil
}
