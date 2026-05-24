package world

import (
	"fmt"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// ValidateDetachComponent checks whether detaching a component from an
// existing entity is permitted by the entity type contract.
//
// Checks (in order):
//  1. Entity type must exist in schema.
//  2. Component must be declared in schema.Components (always hard error).
//  3. Component must not be required on the entity type (always hard error,
//     even in warning mode — detaching required data has no safe path).
func ValidateDetachComponent(
	s *schema.DatabaseSchema,
	entityTypeName string,
	componentName string,
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

	// 3. Component must not be required. Required detach is always an error,
	// even in warning mode — removing required data has no safe "proceed anyway".
	if et.IsComponentRequired(componentName) {
		vr.Errors = append(vr.Errors,
			fmt.Sprintf("component %q is required for entity type %q and cannot be detached", componentName, entityTypeName))
	}

	return vr
}
