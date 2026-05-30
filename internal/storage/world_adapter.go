package storage

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
)

// txWorldWriter implements agent.WorldWriter using a live *sql.Tx.
// Table names are "comp_" + lowercase(compName); column names are lowercase(field).
// No schema reference is required — callers supply valid component and field names.
type txWorldWriter struct{ tx *sql.Tx }

// NewTxWorldWriter wraps tx to produce an agent.WorldWriter.
func NewTxWorldWriter(tx *sql.Tx) agent.WorldWriter { return &txWorldWriter{tx: tx} }

func (w *txWorldWriter) SpawnEntity(entityType string) (int64, error) {
	res, err := w.tx.Exec(
		"INSERT INTO entities (entity_type, created_tick) VALUES (?, 0)", entityType,
	)
	if err != nil {
		return 0, fmt.Errorf("SpawnEntity: %w", err)
	}
	return res.LastInsertId()
}

func (w *txWorldWriter) AttachComponent(entityID int64, compName string, values map[string]any) error {
	table := "comp_" + strings.ToLower(compName)
	cols := []string{"entity_id"}
	args := []any{entityID}
	for col, val := range values {
		cols = append(cols, strings.ToLower(col))
		args = append(args, val)
	}
	ph := make([]string, len(cols))
	for i := range ph {
		ph[i] = "?"
	}
	_, err := w.tx.Exec(
		fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(cols, ","), strings.Join(ph, ",")),
		args...,
	)
	if err != nil {
		return fmt.Errorf("AttachComponent %q: %w", compName, err)
	}
	return nil
}

func (w *txWorldWriter) DetachComponent(entityID int64, compName string) error {
	table := "comp_" + strings.ToLower(compName)
	_, err := w.tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE entity_id = ?", table), entityID)
	if err != nil {
		return fmt.Errorf("DetachComponent %q: %w", compName, err)
	}
	return nil
}

func (w *txWorldWriter) SetComponentValue(entityID int64, compName, field string, value any) error {
	table := "comp_" + strings.ToLower(compName)
	col := strings.ToLower(field)
	_, err := w.tx.Exec(
		fmt.Sprintf("UPDATE %s SET %s = ? WHERE entity_id = ?", table, col),
		value, entityID,
	)
	if err != nil {
		return fmt.Errorf("SetComponentValue %q.%q: %w", compName, field, err)
	}
	return nil
}

// txWorldReader implements agent.WorldReader using a live *sql.Tx.
// Reads within the same transaction see uncommitted writes from the same tx.
type txWorldReader struct{ tx *sql.Tx }

// NewTxWorldReader wraps tx to produce an agent.WorldReader.
func NewTxWorldReader(tx *sql.Tx) agent.WorldReader { return &txWorldReader{tx: tx} }

func (r *txWorldReader) GetComponentValue(entityID int64, compName, field string) (any, error) {
	table := "comp_" + strings.ToLower(compName)
	col := strings.ToLower(field)
	var val any
	err := r.tx.QueryRow(
		fmt.Sprintf("SELECT %s FROM %s WHERE entity_id = ?", col, table), entityID,
	).Scan(&val)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetComponentValue %q.%q: %w", compName, field, err)
	}
	return val, nil
}

func (r *txWorldReader) HasComponent(entityID int64, compName string) (bool, error) {
	table := "comp_" + strings.ToLower(compName)
	var n int
	err := r.tx.QueryRow(
		fmt.Sprintf("SELECT 1 FROM %s WHERE entity_id = ? LIMIT 1", table), entityID,
	).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("HasComponent %q: %w", compName, err)
	}
	return true, nil
}

func (r *txWorldReader) FindEntityByType(entityType string) (int64, error) {
	var id int64
	err := r.tx.QueryRow(
		"SELECT id FROM entities WHERE entity_type = ? LIMIT 1", entityType,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("no entity of type %q", entityType)
	}
	if err != nil {
		return 0, fmt.Errorf("FindEntityByType %q: %w", entityType, err)
	}
	return id, nil
}

// Compile-time interface checks.
var (
	_ agent.WorldWriter = (*txWorldWriter)(nil)
	_ agent.WorldReader = (*txWorldReader)(nil)
)
