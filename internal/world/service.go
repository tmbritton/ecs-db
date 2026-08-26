package world

import (
	"context"
	"fmt"
	"sync"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// EntityService orchestrates entity creation by validating the entity
// type contract and persisting the entity along with its components
// in a single transaction.
type EntityService struct {
	store    EntityStore
	schema   *schema.DatabaseSchema
	mu       sync.Mutex
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

// Schema is the schema this service validates against, or nil when none has
// been set.
//
// Read-only by convention and by the caller's need: the map loader has to know
// what type a component's property is before it can read a Tiled property as
// one, and the alternative is loading the schema a second time from a path the
// service already knows.
func (s *EntityService) Schema() *schema.DatabaseSchema {
	return s.schema
}

// Warnings returns warnings from the last CreateEntity call.
func (s *EntityService) Warnings() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	// Validated before a transaction is opened, not only inside one. The check
	// runs again in CreateEntityInTx, which is where it belongs; doing it here
	// as well is what keeps a ValidationError from being masked by a BeginTx
	// that failed, so a caller can still tell "your data is bad" from "the
	// database is busy".
	if err := s.validateCreation(entityTypeName, components); err != nil {
		return nil, err
	}

	var entity *Entity
	err := s.InTx(ctx, func(tx Tx) error {
		e, err := s.CreateEntityInTx(ctx, tx, entityTypeName, components)
		if err != nil {
			return err
		}
		entity = e
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entity, nil
}

// InTx runs fn inside a single transaction, committing when fn returns nil and
// rolling back otherwise.
//
// It exists for the callers that have to write many entities at once and cannot
// afford a transaction each: a map re-import that commits half its tiles is a
// map with a hole in it, and the engine reads the grid on the next tick.
func (s *EntityService) InTx(ctx context.Context, fn func(Tx) error) error {
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return err
	}
	// Deferred, because fn is an arbitrary closure supplied by the caller and a
	// panic inside it would otherwise unwind past both the rollback and the
	// commit, leaving the transaction and its pooled connection held for the
	// life of the process — and with SQLite in WAL, the write lock with them.
	done := false
	defer func() {
		if !done {
			_ = tx.Rollback()
		}
	}()

	if err := fn(tx); err != nil {
		return err
	}
	done = true
	return tx.Commit()
}

// CreateEntityInTx creates an entity inside a transaction the caller owns,
// with the same validation CreateEntity performs — CreateEntity is this
// function wrapped in InTx, so the two cannot drift apart.
//
// The caller commits. On error nothing is rolled back here, because the
// transaction is not this function's to end.
func (s *EntityService) CreateEntityInTx(
	ctx context.Context,
	tx Tx,
	entityTypeName string,
	components []EntityComponent,
) (*Entity, error) {
	if err := s.validateCreation(entityTypeName, components); err != nil {
		return nil, err
	}

	// Get current tick from world table.
	tick, err := s.store.GetCurrentTick(ctx)
	if err != nil {
		return nil, err
	}

	// Insert entity row → get entity ID.
	entityID, err := tx.InsertEntity(ctx, entityTypeName, tick)
	if err != nil {
		return nil, err
	}

	// Insert each component row.
	for _, comp := range components {
		if err := tx.InsertComponent(ctx, entityID, comp.Name, comp.Values); err != nil {
			return nil, err
		}
	}

	return &Entity{
		ID:          entityID,
		EntityType:  entityTypeName,
		CreatedTick: tick,
	}, nil
}

// validateCreation checks the entity type contract and records the warnings
// the last creation produced. It returns a *ValidationError, or nil.
func (s *EntityService) validateCreation(entityTypeName string, components []EntityComponent) error {
	names := make([]string, len(components))
	for i, c := range components {
		names[i] = c.Name
	}

	vr := ValidateEntityCreation(s.schema, entityTypeName, names)
	if !vr.Valid() {
		s.mu.Lock()
		s.warnings = vr.Warnings
		s.mu.Unlock()
		return &ValidationError{
			Type:     entityTypeName,
			Errors:   vr.Errors,
			Warnings: vr.Warnings,
		}
	}

	// Warnings may be non-empty in warning mode.
	s.mu.Lock()
	s.warnings = make([]string, len(vr.Warnings))
	copy(s.warnings, vr.Warnings)
	s.mu.Unlock()
	return nil
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
	values ComponentValues,
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
		s.mu.Lock()
		s.warnings = vr.Warnings
		s.mu.Unlock()
		return &ComponentMutationError{
			Action:   "attach",
			EntityID: entityID,
			Type:     entityTypeName,
			Errors:   vr.Errors,
			Warnings: vr.Warnings,
		}
	}

	s.mu.Lock()
	s.warnings = make([]string, len(vr.Warnings))
	copy(s.warnings, vr.Warnings)
	s.mu.Unlock()

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
		s.mu.Lock()
		s.warnings = vr.Warnings
		s.mu.Unlock()
		return &ComponentMutationError{
			Action:   "detach",
			EntityID: entityID,
			Type:     entityTypeName,
			Errors:   vr.Errors,
			Warnings: vr.Warnings,
		}
	}

	s.mu.Lock()
	s.warnings = vr.Warnings
	s.mu.Unlock()

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
