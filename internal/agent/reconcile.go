package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Reconciler checks entities running a reloaded machine and resets any whose
// current states no longer exist in the new definition.
type Reconciler struct{}

// Reconcile queries behavior_components for all rows where machine_id = machineID.
// For each entity: if all current states are in validStates, it is skipped;
// otherwise current_states is reset to [initial] and any pending event_queue
// rows for that entity+machine are deleted. All writes run in one transaction.
// If the transaction fails it is rolled back and the error is returned.
func (r *Reconciler) Reconcile(ctx context.Context, db *sql.DB, machineID string, validStates map[string]bool, initial string) error {
	rows, err := db.QueryContext(
		ctx,
		`SELECT entity_id, current_states FROM behavior_components WHERE machine_id = ?`,
		machineID,
	)
	if err != nil {
		return fmt.Errorf("reconcile %q: query: %w", machineID, err)
	}
	defer rows.Close()

	type entry struct {
		entityID int64
		states   []string
	}
	var toReset []entry

	for rows.Next() {
		var entityID int64
		var raw string
		if err := rows.Scan(&entityID, &raw); err != nil {
			return fmt.Errorf("reconcile %q: scan: %w", machineID, err)
		}
		var states []string
		if err := json.Unmarshal([]byte(raw), &states); err != nil {
			return fmt.Errorf("reconcile %q: parse current_states for entity %d: %w", machineID, entityID, err)
		}
		if len(removedStates(states, validStates)) == 0 {
			fmt.Printf("[hot-reload] entity %d machine %q state valid, no reset\n", entityID, machineID)
			continue
		}
		toReset = append(toReset, entry{entityID, states})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reconcile %q: rows: %w", machineID, err)
	}

	if len(toReset) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reconcile %q: begin tx: %w", machineID, err)
	}

	resetJSON, _ := json.Marshal([]string{initial})
	for _, e := range toReset {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE behavior_components SET current_states = ? WHERE entity_id = ? AND machine_id = ?`,
			string(resetJSON), e.entityID, machineID,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("reconcile %q: reset entity %d: %w", machineID, e.entityID, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`DELETE FROM event_queue WHERE entity_id = ? AND machine_id = ?`,
			e.entityID, machineID,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("reconcile %q: cancel events for entity %d: %w", machineID, e.entityID, err)
		}
		removed := removedStates(e.states, validStates)
		fmt.Printf("[hot-reload] entity %d machine %q reset to %q (removed states: [%s])\n",
			e.entityID, machineID, initial, strings.Join(removed, ", "))
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reconcile %q: commit: %w", machineID, err)
	}
	return nil
}

// removedStates returns state names from current that are absent from valid.
func removedStates(current []string, valid map[string]bool) []string {
	var out []string
	for _, s := range current {
		if !valid[s] {
			out = append(out, s)
		}
	}
	return out
}
