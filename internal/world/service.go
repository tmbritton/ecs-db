package world

import (
	"context"
	"fmt"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// EntityService orchestrates entity creation by validating the entity
// type contract and persisting the entity along with its components
// in a single transaction.
type EntityService struct {
	store    EntityStore
	schema   *schema.DatabaseSchema
	warnings []string
}

// NewEntityService creates a service with the given store. Set the schema
// with SetSchema before calling CreateEntity.
func NewEntityService(store EntityStore) *EntityService {
	return &EntityService{store: store}
}

// SetSchema sets the database schema the service validates against.
func (s *EntityService) SetSchema(ds schema.DatabaseSchema) {
	s.schema = &ds
}

// Warnings returns warnings from the last CreateEntity call.
func (s *EntityService) Warnings() []string {
	return s.warnings
}

// CreateEntity creates a new entity of the given type with the provided
// components. Validation is performed against the loaded schema.
//
// On success, returns the created entity and nil error.
// On validation failure (or any storage error), returns nil and an error.
// The operation is fully transactional — no partial data is written.
func (s *EntityService) CreateEntity(
	ctx context.Context,
	entityTypeName string,
	components []EntityComponent,
) (*Entity, error) {
	// Extract component names for validation.
	names := make([]string, len(components))
	for i, c := range components {
		names[i] = c.Name
	}

	// Validate against schema.
	vr := ValidateEntityCreation(s.schema, entityTypeName, names)
	if !vr.Valid() {
		s.warnings = vr.Warnings
		return nil, &ValidationError{
			Type:     entityTypeName,
			Errors:   vr.Errors,
			Warnings: vr.Warnings,
		}
	}

	// Collect warnings (may be non-empty in warning mode).
	s.warnings = make([]string, len(vr.Warnings))
	copy(s.warnings, vr.Warnings)

	// Begin transaction.
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return nil, err
	}

	// Get current tick from world table.
	tick, err := s.store.GetCurrentTick(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	// Insert entity row → get entity ID.
	entityID, err := tx.InsertEntity(ctx, entityTypeName, tick)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	// Insert each component row.
	for _, comp := range components {
		if err := tx.InsertComponent(ctx, entityID, comp.Name, comp.Values); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}

	// Commit.
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &Entity{
		ID:          entityID,
		EntityType:  entityTypeName,
		CreatedTick: tick,
	}, nil
}

// ValidationError is returned when entity creation fails validation.
type ValidationError struct {
	Type     string
	Errors   []string
	Warnings []string
}

func (e *ValidationError) Error() string {
	if len(e.Errors) == 0 {
		return "entity creation validation failed: no details"
	}
	return fmt.Sprintf("entity creation validation failed for type %q: %s",
		e.Type, e.Errors[0])
}
