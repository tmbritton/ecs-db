package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/world"
	"modernc.org/sqlite"
	sqliteLib "modernc.org/sqlite/lib"
)

// sqliteTx wraps *sql.Tx and the schema to implement the world.Tx port.
type sqliteTx struct {
	tx     *sql.Tx
	schema schema.DatabaseSchema
}

func (t *sqliteTx) InsertEntity(ctx context.Context, entityType string, createdTick int64) (int64, error) {
	res, err := t.tx.ExecContext(ctx,
		"INSERT INTO entities (entity_type, created_tick) VALUES (?, ?)",
		entityType, createdTick)
	if err != nil {
		return 0, fmt.Errorf("inserting entity: %w", err)
	}
	return res.LastInsertId()
}

func (t *sqliteTx) InsertComponent(ctx context.Context, entityID int64, compName string, values world.ComponentValues) error {
	return t.insertComponent(ctx, entityID, compName, values)
}

func (t *sqliteTx) AttachComponent(ctx context.Context, entityID int64, compName string, values world.ComponentValues) error {
	err := t.insertComponent(ctx, entityID, compName, values)
	// On UNIQUE constraint violation, return the domain sentinel.
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqliteLib.SQLITE_CONSTRAINT_UNIQUE {
			return world.ErrAlreadyAttached
		}
	}
	return err
}

// insertComponent is the shared implementation for both InsertComponent
// and AttachComponent — they do the same SQL operation.
func (t *sqliteTx) insertComponent(ctx context.Context, entityID int64, compName string, values world.ComponentValues) error {
	comp := t.schema.Components[compName]

	tableName := "comp_" + strings.ToLower(compName)

	switch comp.Type {
	case schema.ComponentTypeObject:
		return t.insertObjectComponent(ctx, tableName, entityID, comp, values)
	case schema.ComponentTypeEntityRef:
		return t.insertEntityRefComponent(ctx, tableName, entityID, values)
	case schema.ComponentTypeArray:
		return t.insertArrayComponent(ctx, tableName, entityID, values)
	case schema.ComponentTypeString, schema.ComponentTypeInteger,
		schema.ComponentTypeNumber, schema.ComponentTypeBoolean:
		return t.insertScalarComponent(ctx, tableName, entityID, comp.Type, values)
	default:
		return fmt.Errorf("unsupported component type %q for insert", comp.Type)
	}
}

func (t *sqliteTx) insertObjectComponent(
	ctx context.Context,
	tableName string,
	entityID int64,
	comp schema.Component,
	values world.ComponentValues,
) error {
	// Build column list in sorted order for deterministic SQL.
	cols := make([]string, 0, len(comp.Properties)+1)
	args := make([]any, 0, len(comp.Properties)+1)
	cols = append(cols, "entity_id")
	args = append(args, entityID)

	// Sort property names for deterministic INSERT column order.
	propNames := make([]string, 0, len(comp.Properties))
	for name := range comp.Properties {
		propNames = append(propNames, name)
	}
	sort.Strings(propNames)

	for _, propName := range propNames {
		cols = append(cols, strings.ToLower(propName))
		val, ok := values[propName]
		if !ok {
			val = nil
		}
		args = append(args, val)
	}

	placeholders := make([]string, len(cols))
	for i := range placeholders {
		placeholders[i] = "?"
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)
	if _, err := t.tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("inserting into %s: %w", tableName, err)
	}
	return nil
}

func (t *sqliteTx) insertEntityRefComponent(
	ctx context.Context,
	tableName string,
	entityID int64,
	values world.ComponentValues,
) error {
	targetID, ok := values["target_entity_id"]
	if !ok {
		targetID = values["target"]
	}
	if targetID == nil {
		return fmt.Errorf("entity-ref component %s: target_entity_id is nil", tableName)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (entity_id, target_entity_id) VALUES (?, ?)",
		tableName,
	)
	if _, err := t.tx.ExecContext(ctx, query, entityID, targetID); err != nil {
		return fmt.Errorf("inserting into %s: %w", tableName, err)
	}
	return nil
}

func (t *sqliteTx) insertArrayComponent(
	ctx context.Context,
	tableName string,
	entityID int64,
	values world.ComponentValues,
) error {
	// Arrays are stored as JSON in a single "value" column.
	var raw any
	if v, ok := values["value"]; ok {
		raw = v
	} else if len(values) > 0 {
		// Caller passed raw array items as the map.
		// Convert to a slice.
		items := make([]any, 0, len(values))
		for _, v := range values {
			items = append(items, v)
		}
		raw = items
	} else {
		raw = []any{}
	}

	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("encoding array component %s as JSON: %w", tableName, err)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (entity_id, value) VALUES (?, ?)",
		tableName,
	)
	if _, err := t.tx.ExecContext(ctx, query, entityID, string(jsonBytes)); err != nil {
		return fmt.Errorf("inserting into %s: %w", tableName, err)
	}
	return nil
}

func (t *sqliteTx) insertScalarComponent(
	ctx context.Context,
	tableName string,
	entityID int64,
	compType string,
	values world.ComponentValues,
) error {
	var val any
	if v, ok := values["value"]; ok {
		val = v
	} else if len(values) == 1 {
		// Caller passed a single-key map with the value under an arbitrary key.
		for _, v := range values {
			val = v
		}
	} else {
		val = nil
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (entity_id, value) VALUES (?, ?)",
		tableName,
	)
	if _, err := t.tx.ExecContext(ctx, query, entityID, val); err != nil {
		return fmt.Errorf("inserting into %s: %w", tableName, err)
	}
	return nil
}

func (t *sqliteTx) Commit() error {
	return t.tx.Commit()
}

func (t *sqliteTx) Rollback() error {
	return t.tx.Rollback()
}

func (t *sqliteTx) DetachComponent(ctx context.Context, entityID int64, compName string) error {
	_, ok := t.schema.Components[compName]
	if !ok {
		return fmt.Errorf("component %q not declared in schema", compName)
	}

	tableName := "comp_" + strings.ToLower(compName)
	query := fmt.Sprintf("DELETE FROM %s WHERE entity_id = ?", tableName)
	result, err := t.tx.ExecContext(ctx, query, entityID)
	if err != nil {
		return fmt.Errorf("deleting from %s: %w", tableName, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected on delete from %s: %w", tableName, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("entity %d has no %s component to detach", entityID, compName)
	}
	return nil
}

// SetComponentValues updates the named fields of an attached component.
// Implements world.Tx.
//
// Partial by contract: a field the caller does not name keeps its stored value,
// which is what lets a re-import rewrite passability without knowing every
// column the component happens to have.
func (t *sqliteTx) SetComponentValues(ctx context.Context, entityID int64, compName string, values world.ComponentValues) error {
	comp, ok := t.schema.Components[compName]
	if !ok {
		return fmt.Errorf("component %q not declared in schema", compName)
	}
	if len(values) == 0 {
		return nil
	}

	tableName := "comp_" + strings.ToLower(compName)

	switch comp.Type {
	case schema.ComponentTypeObject:
		return t.updateColumns(ctx, tableName, entityID, comp, values)
	case schema.ComponentTypeEntityRef:
		target, ok := values["target_entity_id"]
		if !ok {
			target, ok = values["target"]
		}
		if !ok {
			return fmt.Errorf("updating %s: entity-ref component takes target_entity_id", tableName)
		}
		if target == nil {
			return fmt.Errorf("updating %s: target_entity_id is nil", tableName)
		}
		return t.updateSingleColumn(ctx, tableName, "target_entity_id", entityID, target)
	case schema.ComponentTypeArray:
		raw, ok := values["value"]
		if !ok {
			return fmt.Errorf("updating %s: array component takes its items under \"value\"", tableName)
		}
		jsonBytes, err := json.Marshal(raw)
		if err != nil {
			return fmt.Errorf("encoding array component %s as JSON: %w", tableName, err)
		}
		return t.updateSingleColumn(ctx, tableName, "value", entityID, string(jsonBytes))
	case schema.ComponentTypeString, schema.ComponentTypeInteger,
		schema.ComponentTypeNumber, schema.ComponentTypeBoolean:
		val, ok := values["value"]
		if !ok {
			return fmt.Errorf("updating %s: scalar component takes its value under \"value\"", tableName)
		}
		return t.updateSingleColumn(ctx, tableName, "value", entityID, val)
	default:
		return fmt.Errorf("unsupported component type %q for update", comp.Type)
	}
}

// updateColumns writes the named properties of an object component.
//
// Property names are sorted so the SQL is the same statement every time — the
// same reason insertObjectComponent sorts, and it is what makes a failing
// statement reproducible rather than a function of map iteration.
func (t *sqliteTx) updateColumns(
	ctx context.Context,
	tableName string,
	entityID int64,
	comp schema.Component,
	values world.ComponentValues,
) error {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)

	assignments := make([]string, 0, len(names))
	args := make([]any, 0, len(names)+1)
	for _, name := range names {
		if _, ok := comp.Properties[name]; !ok {
			return fmt.Errorf("updating %s: %q is not a property of the component", tableName, name)
		}
		col := strings.ToLower(name)
		if err := validateIdentifier(col, "SetComponentValues field"); err != nil {
			return err
		}
		assignments = append(assignments, col+" = ?")
		args = append(args, values[name])
	}
	args = append(args, entityID)

	query := fmt.Sprintf("UPDATE %s SET %s WHERE entity_id = ?", tableName, strings.Join(assignments, ", "))
	return t.exactlyOneRow(ctx, tableName, entityID, query, args...)
}

func (t *sqliteTx) updateSingleColumn(ctx context.Context, tableName, column string, entityID int64, value any) error {
	if err := validateIdentifier(column, "SetComponentValues field"); err != nil {
		return err
	}
	query := fmt.Sprintf("UPDATE %s SET %s = ? WHERE entity_id = ?", tableName, column)
	return t.exactlyOneRow(ctx, tableName, entityID, query, value, entityID)
}

// exactlyOneRow runs an update and refuses one that matched nothing.
//
// An update to a component the entity does not have is a caller who believes
// they are writing and are not, and Tx has no reader to let them check first —
// HasComponent is on the store, which is a different pooled connection and
// cannot see this transaction's own writes. So the write itself has to say so,
// the way DetachComponent already does.
func (t *sqliteTx) exactlyOneRow(ctx context.Context, tableName string, entityID int64, query string, args ...any) error {
	res, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("updating %s: %w", tableName, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected on update of %s: %w", tableName, err)
	}
	if n == 0 {
		return fmt.Errorf("entity %d has no %s component to update", entityID, tableName)
	}
	return nil
}

// DeleteEntity removes an entity, the component rows belonging to it, and the
// interpreter state that would otherwise go on running without it.
// Implements world.Tx.
//
// Every component table the schema declares is cleared, not only the ones the
// entity is known to hold: an entity type with allowExtraComponents can carry
// any declared component, and a DELETE that matches nothing costs nothing.
//
// It does not rely on ON DELETE CASCADE. The cascade is in the DDL, but
// PRAGMA foreign_keys is per-connection and the store issues it once against a
// pooled *sql.DB — see world.Tx for the measurement.
//
// behavior_components and event_queue are live state, not history, and neither
// is a comp_ table so neither is reached by the loop above. tick.go reads
// behavior_components with no join to entities and delivers TICK to every row
// it finds, so a machine left behind runs its actions against components that
// are gone — quietly, on every tick, forever. event_queue is drained by tick
// the same way. Only transitions is left: it is the audit log, and it is meant
// to outlive what it describes, which is why it declares no foreign key.
func (t *sqliteTx) DeleteEntity(ctx context.Context, entityID int64) error {
	names := make([]string, 0, len(t.schema.Components))
	for name := range t.schema.Components {
		names = append(names, name)
	}
	sort.Strings(names)

	tables := make([]string, 0, len(names)+2)
	for _, name := range names {
		table := "comp_" + strings.ToLower(name)
		if err := validateIdentifier(strings.TrimPrefix(table, "comp_"), "DeleteEntity component"); err != nil {
			return err
		}
		tables = append(tables, table)
	}
	// Only if the interpreter has been set up at all: EnsureInterpreterTables is
	// a separate call, and a store used purely for entities never made them.
	for _, table := range []string{"behavior_components", "event_queue"} {
		exists, err := t.tableExists(ctx, table)
		if err != nil {
			return err
		}
		if exists {
			tables = append(tables, table)
		}
	}

	for _, table := range tables {
		if _, err := t.tx.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE entity_id = ?", table), entityID,
		); err != nil {
			return fmt.Errorf("deleting from %s: %w", table, err)
		}
	}

	if _, err := t.tx.ExecContext(ctx, "DELETE FROM entities WHERE id = ?", entityID); err != nil {
		return fmt.Errorf("deleting entity %d: %w", entityID, err)
	}
	return nil
}

func (t *sqliteTx) tableExists(ctx context.Context, name string) (bool, error) {
	var found string
	err := t.tx.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("looking for table %s: %w", name, err)
	}
	return true, nil
}

// BeginTx starts a new transaction and returns it wrapped as a world.Tx.
// Implements world.EntityStore.
func (s *SQLiteStore) BeginTx(ctx context.Context) (world.Tx, error) {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	return &sqliteTx{tx: sqlTx, schema: s.schema}, nil
}

// GetCurrentTick reads the current tick from the world table.
// Returns 0 if no tick has been recorded yet.
// Implements world.EntityStore.
func (s *SQLiteStore) GetCurrentTick(ctx context.Context) (int64, error) {
	var tick int64
	err := s.db.QueryRowContext(
		ctx,
		"SELECT CAST(value AS INTEGER) FROM world WHERE key='current_tick'",
	).Scan(&tick)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("reading current tick: %w", err)
	}
	return tick, nil
}

// GetEntityType returns the entity type for the given entity ID.
// Implements world.EntityStore.
func (s *SQLiteStore) GetEntityType(ctx context.Context, entityID int64) (string, error) {
	var entityType string
	err := s.db.QueryRowContext(
		ctx,
		"SELECT entity_type FROM entities WHERE id = ?",
		entityID,
	).Scan(&entityType)
	if err == sql.ErrNoRows {
		return "", &world.EntityNotFoundError{ID: entityID}
	}
	if err != nil {
		return "", fmt.Errorf("reading entity type for id %d: %w", entityID, err)
	}
	return entityType, nil
}

// HasComponent returns true if the entity has the named component attached.
// Implements world.EntityStore.
func (s *SQLiteStore) HasComponent(ctx context.Context, entityID int64, compName string) (bool, error) {
	table := "comp_" + strings.ToLower(compName)
	var placeholder int
	err := s.db.QueryRowContext(
		ctx,
		fmt.Sprintf("SELECT 1 FROM %s WHERE entity_id = ? LIMIT 1", table),
		entityID,
	).Scan(&placeholder)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking component %s for entity %d: %w", compName, entityID, err)
	}
	return true, nil
}

// Compile-time interface checks.
var (
	_ world.Tx          = (*sqliteTx)(nil)
	_ world.EntityStore = (*SQLiteStore)(nil)
)
