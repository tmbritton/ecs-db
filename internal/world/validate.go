package world

import (
	"fmt"
	"sort"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// ValidationResult carries the outcome of entity creation validation.
// Errors abort creation regardless of validation level. Warnings are
// non-fatal and are only produced when validationLevel is "warning".
type ValidationResult struct {
	Errors   []string
	Warnings []string
}

// Valid reports whether the result has no hard errors.
func (r ValidationResult) Valid() bool {
	return len(r.Errors) == 0
}

// ValidateEntityCreation checks whether the provided component set
// satisfies the entity type contract defined in the schema.
//
// Checks (in order):
//  1. Entity type must exist in schema.
//  2. All provided components must be declared in schema.Components
//     (always a hard error — no table exists for undeclared components).
//  3. All required components must be present.
//  4. No disallowed extra components when allowExtraComponents is false.
//
// In "warning" mode, violations of (3) and (4) become warnings.
// Checks (1) and (2) are always hard errors.
func ValidateEntityCreation(
	s *schema.DatabaseSchema,
	entityTypeName string,
	providedComponents []string,
) ValidationResult {
	var vr ValidationResult

	// 1. Entity type must exist.
	et, ok := s.EntityTypes[entityTypeName]
	if !ok {
		vr.Errors = append(vr.Errors,
			fmt.Sprintf("unknown entity type %q", entityTypeName))
		return vr
	}

	et.ApplyDefaults()

	providedSet := make(map[string]bool, len(providedComponents))
	for _, c := range providedComponents {
		providedSet[c] = true
	}

	// 2. All provided components must be declared.
	// Sort for deterministic error ordering.
	sorted := make([]string, 0, len(providedComponents))
	for _, c := range providedComponents {
		if _, ok := s.Components[c]; !ok {
			sorted = append(sorted, c)
		}
	}
	sort.Strings(sorted)
	for _, c := range sorted {
		vr.Errors = append(vr.Errors,
			fmt.Sprintf("component %q is not declared in schema", c))
	}
	// If any components are undeclared, return early — we can't proceed.
	if len(sorted) > 0 {
		return vr
	}

	// 3. Required components must be present.
	warningMode := et.ValidationLevel == schema.ValidationWarning
	for _, req := range et.RequiredComponents {
		if !providedSet[req] {
			msg := fmt.Sprintf("missing required component %q for entity type %q", req, entityTypeName)
			if warningMode {
				vr.Warnings = append(vr.Warnings, msg)
			} else {
				vr.Errors = append(vr.Errors, msg)
			}
		}
	}

	// 4. No disallowed extra components.
	if !et.AllowExtraComponents {
		for _, provided := range providedComponents {
			if !et.IsComponentAllowed(provided) {
				msg := fmt.Sprintf("component %q is not allowed for entity type %q", provided, entityTypeName)
				if warningMode {
					vr.Warnings = append(vr.Warnings, msg)
				} else {
					vr.Errors = append(vr.Errors, msg)
				}
			}
		}
	}

	return vr
}
