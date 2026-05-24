package world

import (
	"context"
	"errors"
	"fmt"
)

// Tx is the domain-side interface for a database transaction. The storage
// adapter implements this, keeping domain code free of SQL details.
type Tx interface {
	// InsertEntity inserts a row into the entities table with the given
	// entity type and created tick, returning the auto-assigned id.
	InsertEntity(ctx context.Context, entityType string, createdTick int64) (int64, error)
	// InsertComponent inserts a row into the correct comp_* table for the
	// named component with the given values and entity_id.
	InsertComponent(ctx context.Context, entityID int64, compName string, values map[string]interface{}) error
	// AttachComponent inserts a component row for the given entity.
	// Returns errors.Is(err, ErrAlreadyAttached) if the component is already present.
	AttachComponent(ctx context.Context, entityID int64, compName string, values map[string]interface{}) error
	// DetachComponent deletes the component row for the given entity.
	DetachComponent(ctx context.Context, entityID int64, compName string) error
	// Commit commits the transaction.
	Commit() error
	// Rollback rolls back the transaction.
	Rollback() error
}

// ErrAlreadyAttached is returned when an attach would duplicate a component.
var ErrAlreadyAttached = errors.New("component already attached")

// EntityStore is the port the entity service uses for persistence.
// The SQLite adapter implements this interface.
type EntityStore interface {
	// BeginTx starts a new transaction and returns the Tx wrapper.
	BeginTx(ctx context.Context) (Tx, error)
	// GetCurrentTick reads the current tick from the world table.
	// Returns 0 if no tick has been recorded yet.
	GetCurrentTick(ctx context.Context) (int64, error)
	// GetEntityType returns the entity type for the given entity ID.
	// Returns an error if the entity does not exist.
	GetEntityType(ctx context.Context, entityID int64) (string, error)
	// HasComponent returns true if the entity has the named component attached.
	HasComponent(ctx context.Context, entityID int64, compName string) (bool, error)
}

// IsAlreadyAttached reports whether an error is the ErrAlreadyAttached sentinel.
func IsAlreadyAttached(err error) bool {
	return errors.Is(err, ErrAlreadyAttached)
}

// EntityNotFoundError is returned when an entity ID does not exist.
type EntityNotFoundError struct {
	ID int64
}

func (e *EntityNotFoundError) Error() string {
	return fmt.Sprintf("entity %d not found", e.ID)
}
