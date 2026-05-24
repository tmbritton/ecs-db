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

// AttachComponent attaches a new component to an existing entity. Performs
// schema validation before inserting. Runs in its own transaction.
func (s *EntityService) AttachComponent(
	ctx context.Context,
	entityID int64,
	compName string,
	values map[string]interface{},
) error {
	// Look up the entity type.
	entityTypeName, err := s.store.GetEntityType(ctx, entityID)
	if err != nil {
		return fmt.Errorf("attaching component to entity %d: %w", entityID, err)
	}

	// Check if already attached.
	alreadyAttached, err := s.store.HasComponent(ctx, entityID, compName)
	if err != nil {
		return fmt.Errorf("attaching component to entity %d: %w", entityID, err)
	}

	// Validate the attach.
	vr := ValidateAttachComponent(s.schema, entityTypeName, compName, alreadyAttached)
	if !vr.Valid() {
		s.warnings = vr.Warnings
		return &ComponentMutationError{
			Action:   "attach",
			EntityID: entityID,
			Type:     entityTypeName,
			Errors:   vr.Errors,
			Warnings: vr.Warnings,
		}
	}

	s.warnings = make([]string, len(vr.Warnings))
	copy(s.warnings, vr.Warnings)

	// Begin transaction and attach.
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("attaching component to entity %d: %w", entityID, err)
	}

	if err := tx.AttachComponent(ctx, entityID, compName, values); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("attaching component to entity %d: %w", entityID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("attaching component to entity %d: %w", entityID, err)
	}

	return nil
}

// DetachComponent removes a component from an existing entity. Performs
// schema validation before deleting. Runs in its own transaction.
func (s *EntityService) DetachComponent(
	ctx context.Context,
	entityID int64,
	compName string,
) error {
	// Look up the entity type.
	entityTypeName, err := s.store.GetEntityType(ctx, entityID)
	if err != nil {
		return fmt.Errorf("detaching component from entity %d: %w", entityID, err)
	}

	// Validate the detach.
	vr := ValidateDetachComponent(s.schema, entityTypeName, compName)
	if !vr.Valid() {
		s.warnings = vr.Warnings
		return &ComponentMutationError{
			Action:   "detach",
			EntityID: entityID,
			Type:     entityTypeName,
			Errors:   vr.Errors,
			Warnings: vr.Warnings,
		}
	}

	s.warnings = vr.Warnings

	// Begin transaction and detach.
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("detaching component from entity %d: %w", entityID, err)
	}

	if err := tx.DetachComponent(ctx, entityID, compName); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("detaching component from entity %d: %w", entityID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("detaching component from entity %d: %w", entityID, err)
	}

	return nil
}

// ComponentMutationError is returned when attach/detach fails validation.
type ComponentMutationError struct {
	Action   string // "attach" or "detach"
	EntityID int64
	Type     string
	Errors   []string
	Warnings []string
}

func (e *ComponentMutationError) Error() string {
	if len(e.Errors) == 0 {
		return fmt.Sprintf("component %s validation failed for entity %d: no details", e.Action, e.EntityID)
	}
	return fmt.Sprintf("component %s validation failed for entity %d (type %q): %s",
		e.Action, e.EntityID, e.Type, e.Errors[0])
}
