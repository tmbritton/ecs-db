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
	InsertComponent(ctx context.Context, entityID int64, compName string, values ComponentValues) error
	// AttachComponent inserts a component row for the given entity.
	// Returns errors.Is(err, ErrAlreadyAttached) if the component is already present.
	AttachComponent(ctx context.Context, entityID int64, compName string, values ComponentValues) error
	// DetachComponent deletes the component row for the given entity.
	DetachComponent(ctx context.Context, entityID int64, compName string) error
	// SetComponentValues updates the named fields of a component already
	// attached to the entity. Fields the caller does not name keep the value
	// they hold — this is a partial update, the plural of the agent runtime's
	// SetComponentValue and partial for the same reason.
	//
	// Updating a component the entity does not have is an error, the way
	// DetachComponent is. Tx has no reader, and HasComponent is on the store,
	// which is a different pooled connection and cannot see this transaction's
	// own writes — so asking first is not available inside a transaction and the
	// write itself has to answer.
	//
	// It is stricter than InsertComponent about where a value lives: a scalar
	// and an array component take theirs under "value" and nothing else, where
	// the insert path also accepts a lone key of any name. The insert cannot
	// tell a typo from a value; an update can, and a silent write to the wrong
	// component is worse than a refusal a caller reads once.
	SetComponentValues(ctx context.Context, entityID int64, compName string, values ComponentValues) error
	// DeleteEntity removes an entity, the component rows belonging to it, and
	// the interpreter state that would otherwise keep running without it —
	// behavior_components and event_queue, both of which the tick reads with no
	// join to entities.
	//
	// The component rows go explicitly rather than by ON DELETE CASCADE, which
	// is belt and braces now rather than the only mechanism: the cascade used
	// to be unenforced on every connection but one, because the engine set
	// PRAGMA foreign_keys once against a pooled *sql.DB. storage.DSN fixed
	// that. The explicit deletes stay because they also work on a database
	// opened by something that did not set it.
	//
	// An entity another entity *points at* is a different question, and the
	// database answers it rather than this method: an entity-ref column
	// declares ON DELETE CASCADE, so deleting the target removes the component
	// that pointed at it and leaves the entity that held it alone. It used to
	// declare no ON DELETE clause at all, which SQLite reads as a restrict — so
	// deleting an entity could fail because of some other entity's data.
	//
	// Two things that follow, and neither is obvious. The whole component goes,
	// not just the column that held the reference: a Holder with an owner and
	// an hp loses both. And this is the database's promise rather than this
	// method's, so it needs foreign keys enforced — on a connection opened
	// without them, the pointing component survives its target. storage.DSN is
	// what makes that true for every connection the engine opens; a database
	// opened by something else is that something else's problem.
	//
	// Rows in transitions are left: it is the audit log, it declares no foreign
	// key, and it is meant to outlive what it describes.
	DeleteEntity(ctx context.Context, entityID int64) error
	// RecordSpawn records that a map's object has become an entity, so a later
	// load of the same map does not create it again.
	//
	// On Tx rather than beside it, because it has to commit with the entity it
	// describes. The two were separate statements, and anything between them —
	// a duplicate object id, a killed process — left an entity that no spawn
	// row pointed at, which the next load duplicated and never mentioned.
	RecordSpawn(ctx context.Context, mapPath string, objectID int, entityID int64) error
	// SetSpawnComponents records the component set the map authored for this
	// object. A later import can detach a component removed from the file
	// without touching one attached only at runtime. It commits in the same
	// transaction as the entity's create or update.
	SetSpawnComponents(ctx context.Context, mapPath string, objectID int, names []string) error
	// SetTileArtComponents records the components authored by a painted tile's
	// owned reference, so re-import can detach removed authoring without
	// disturbing runtime-only components. It commits with the entity update.
	SetTileArtComponents(ctx context.Context, entityID int64, names []string) error
	// ForgetSpawn removes the record of a map's object having been spawned, for
	// an object the map no longer has. The entity goes with it — separately,
	// because a row may outlive nothing at all when the entity is already gone.
	ForgetSpawn(ctx context.Context, mapPath string, objectID int) error
	// Commit commits the transaction.
	Commit() error
	// Rollback rolls back the transaction.
	Rollback() error
}

// ErrAlreadyAttached is returned when an attach would duplicate a component.
var ErrAlreadyAttached = errors.New("component already attached")

// ErrNoSuchComponent is returned when an update names a component the entity
// does not have.
//
// A sentinel rather than a sentence, because a caller can act on it: the map
// re-import sets the components an object describes, and an object that has
// started describing a new one means "attach it", not "fail". Without this the
// only way to tell that case from a genuine failure is to match on the text of
// an error message.
var ErrNoSuchComponent = errors.New("entity does not have that component")

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
