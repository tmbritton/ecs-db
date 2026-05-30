package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
)

type sqliteMachineWriter struct {
	tx *sql.Tx
}

// NewMachineWriter wraps a *sql.Tx to produce an agent.MachineWriter.
func NewMachineWriter(tx *sql.Tx) agent.MachineWriter {
	return &sqliteMachineWriter{tx: tx}
}

func (w *sqliteMachineWriter) SetMachineState(entityID int64, machineID string, states []string, tick int64) error {
	data, err := json.Marshal(states)
	if err != nil {
		return fmt.Errorf("SetMachineState: marshal states: %w", err)
	}
	_, err = w.tx.Exec(
		`INSERT INTO behavior_components (entity_id, machine_id, current_states, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(entity_id, machine_id) DO UPDATE SET
		   current_states = excluded.current_states,
		   updated_at     = excluded.updated_at`,
		entityID, machineID, string(data), tick,
	)
	if err != nil {
		return fmt.Errorf("SetMachineState: %w", err)
	}
	return nil
}

func (w *sqliteMachineWriter) AppendTransition(rec agent.TransitionRecord) error {
	fromJSON, err := json.Marshal(rec.FromStates)
	if err != nil {
		return fmt.Errorf("AppendTransition: marshal from_states: %w", err)
	}
	toJSON, err := json.Marshal(rec.ToStates)
	if err != nil {
		return fmt.Errorf("AppendTransition: marshal to_states: %w", err)
	}
	actionsJSON, err := json.Marshal(rec.ActionsRun)
	if err != nil {
		return fmt.Errorf("AppendTransition: marshal actions_run: %w", err)
	}
	var condResult interface{}
	if rec.CondResult != nil {
		if *rec.CondResult {
			condResult = 1
		} else {
			condResult = 0
		}
	}
	_, err = w.tx.Exec(
		`INSERT INTO transitions
		   (tick, wall_ms, entity_id, machine_id, from_states, to_states, event, cond_result, actions_run)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.Tick, rec.WallMs, rec.EntityID, rec.MachineID,
		string(fromJSON), string(toJSON), rec.Event, condResult, string(actionsJSON),
	)
	if err != nil {
		return fmt.Errorf("AppendTransition: %w", err)
	}
	return nil
}

func (w *sqliteMachineWriter) ScheduleAfterEvent(entityID int64, machineID, eventType string, targetTick int64) error {
	_, err := w.tx.Exec(
		`INSERT INTO event_queue (entity_id, machine_id, event_type, target_tick) VALUES (?, ?, ?, ?)`,
		entityID, machineID, eventType, targetTick,
	)
	if err != nil {
		return fmt.Errorf("ScheduleAfterEvent: %w", err)
	}
	return nil
}

func (w *sqliteMachineWriter) CancelAfterEvents(entityID int64, machineID string, stateIDs []string) error {
	for _, stateID := range stateIDs {
		// The literal ")." in the pattern anchors the boundary between duration
		// and state ID, preventing spurious matches on sibling states.
		pattern := "xstate.after(%)." + escapeForLIKE(stateID)
		if _, err := w.tx.Exec(
			`DELETE FROM event_queue WHERE entity_id = ? AND machine_id = ? AND event_type LIKE ? ESCAPE '\'`,
			entityID, machineID, pattern,
		); err != nil {
			return fmt.Errorf("CancelAfterEvents %q: %w", stateID, err)
		}
	}
	return nil
}

// escapeForLIKE escapes SQLite LIKE wildcards in s so state IDs containing
// '%' or '_' do not act as pattern wildcards.
func escapeForLIKE(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

var _ agent.MachineWriter = (*sqliteMachineWriter)(nil)
