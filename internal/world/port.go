package world

import "context"

// Tx is the domain-side interface for a database transaction. The storage
// adapter implements this, keeping domain code free of SQL details.
type Tx interface {
	// InsertEntity inserts a row into the entities table with the given
	// entity type and created tick, returning the auto-assigned id.
	InsertEntity(ctx context.Context, entityType string, createdTick int64) (int64, error)
	// InsertComponent inserts a row into the correct comp_* table for the
	// named component with the given values and entity_id.
	InsertComponent(ctx context.Context, entityID int64, compName string, values map[string]interface{}) error
	// Commit commits the transaction.
	Commit() error
	// Rollback rolls back the transaction.
	Rollback() error
}

// EntityStore is the port the entity service uses for persistence.
// The SQLite adapter implements this interface.
type EntityStore interface {
	// BeginTx starts a new transaction and returns the Tx wrapper.
	BeginTx(ctx context.Context) (Tx, error)
	// GetCurrentTick reads the current tick from the world table.
	// Returns 0 if no tick has been recorded yet.
	GetCurrentTick(ctx context.Context) (int64, error)
}
