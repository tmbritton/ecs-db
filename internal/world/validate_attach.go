package world

import (
	"fmt"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// ValidateAttachComponent checks whether attaching a component to an
// existing entity satisfies the entity type contract.
//
// Checks (in order):
//  1. Entity type must exist in schema.
//  2. Component must be declared in schema.Components (always hard error).
//  3. Component must be allowed on the entity type.
//  4. Component must not already be attached (always hard error, no upsert).
//
// In "warning" mode, check (3) produces warnings instead of errors.
// Checks (1), (2), and (4) are always hard errors.
func ValidateAttachComponent(
	s *schema.DatabaseSchema,
	entityTypeName string,
	componentName string,
	isAlreadyAttached bool,
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

	// 2. Component must be declared.
	if _, ok := s.Components[componentName]; !ok {
		vr.Errors = append(vr.Errors,
			fmt.Sprintf("component %q is not declared in schema", componentName))
		return vr
	}

	// 3. Component must be allowed on the entity type.
	if !et.IsComponentAllowed(componentName) && !et.AllowExtraComponents {
		msg := fmt.Sprintf("component %q is not allowed for entity type %q", componentName, entityTypeName)
		if et.ValidationLevel == schema.ValidationWarning {
			vr.Warnings = append(vr.Warnings, msg)
		} else {
			vr.Errors = append(vr.Errors, msg)
		}
	}

	// 4. Component must not already be attached.
	if isAlreadyAttached {
		vr.Errors = append(vr.Errors,
			fmt.Sprintf("component %q is already attached to entity", componentName))
	}

	return vr
}
