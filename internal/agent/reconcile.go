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

// ReconcileOnReload adapts a Reconciler to the callback the loader invokes
// after a successful hot-reload.
//
// The two do not fit directly: ReconcileFunc is handed a machine ID and its
// valid states, while Reconcile also needs a database, a context, and the
// machine's initial state to reset to. The initial state is recovered from the
// loader, which by then holds the newly loaded definition.
//
// Errors are logged rather than returned, because there is nobody to return
// them to — this runs on a timer goroutine the watcher starts, after the file
// has already been accepted. An unrecovered panic there kills the process, not
// merely the watcher, so the awkward cases are handled rather than assumed
// away.
//
// A nil Reconciler yields a nil callback, which the loader treats as "no
// reconciliation", rather than a callback that panics on first use.
func ReconcileOnReload(ctx context.Context, db *sql.DB, loader *Loader, r *Reconciler) ReconcileFunc {
	if r == nil || db == nil || loader == nil {
		return nil
	}
	return func(machineID string, validStates map[string]bool) {
		def, ok := loader.Get(machineID)
		if !ok {
			// Reloaded but not retrievable: nothing to reset toward.
			fmt.Printf("[hot-reload] reconcile skipped: machine %q is not loaded\n", machineID)
			return
		}
		// A machine with no states at all validates — validateInitial only
		// requires an initial when there are children — so def.Initial can be
		// empty, and resetting to it would write [""] into current_states.
		// That leaves the entity pointing at a state that does not exist,
		// which is the corruption this whole callback exists to prevent.
		// Leaving it where it is is strictly better than moving it nowhere.
		if def.Initial == "" || !validStates[def.Initial] {
			fmt.Printf("[hot-reload] reconcile skipped: machine %q has no usable initial state\n", machineID)
			return
		}
		if err := r.Reconcile(ctx, db, machineID, validStates, def.Initial); err != nil {
			fmt.Printf("[hot-reload] reconcile failed for %q: %v\n", machineID, err)
		}
	}
}

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
	type entry struct {
		entityID int64
		states   []string
	}
	var toReset []entry

	for rows.Next() {
		var entityID int64
		var raw string
		if err := rows.Scan(&entityID, &raw); err != nil {
			rows.Close()
			return fmt.Errorf("reconcile %q: scan: %w", machineID, err)
		}
		var states []string
		if err := json.Unmarshal([]byte(raw), &states); err != nil {
			rows.Close()
			return fmt.Errorf("reconcile %q: parse current_states for entity %d: %w", machineID, entityID, err)
		}
		if len(removedStates(states, validStates)) == 0 {
			fmt.Printf("[hot-reload] entity %d machine %q state valid, no reset\n", entityID, machineID)
			continue
		}
		toReset = append(toReset, entry{entityID, states})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("reconcile %q: rows: %w", machineID, err)
	}
	rows.Close()

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
