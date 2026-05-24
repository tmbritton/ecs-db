package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/world"
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

func (t *sqliteTx) InsertComponent(ctx context.Context, entityID int64, compName string, values map[string]interface{}) error {
	comp, ok := t.schema.Components[compName]
	if !ok {
		return fmt.Errorf("component %q not declared in schema", compName)
	}

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
	values map[string]interface{},
) error {
	// Build column list in sorted order for deterministic SQL.
	cols := make([]string, 0, len(comp.Properties)+1)
	args := make([]interface{}, 0, len(comp.Properties)+1)
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
	values map[string]interface{},
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
	values map[string]interface{},
) error {
	// Arrays are stored as JSON in a single "value" column.
	var raw interface{}
	if v, ok := values["value"]; ok {
		raw = v
	} else if len(values) > 0 {
		// Caller passed raw array items as the map.
		// Convert to a slice.
		items := make([]interface{}, 0, len(values))
		for _, v := range values {
			items = append(items, v)
		}
		raw = items
	} else {
		raw = []interface{}{}
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
	values map[string]interface{},
) error {
	var val interface{}
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
	err := s.db.QueryRowContext(ctx,
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

// Compile-time interface checks.
var (
	_ world.Tx         = (*sqliteTx)(nil)
	_ world.EntityStore = (*SQLiteStore)(nil)
)
