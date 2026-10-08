package renderer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/storage"
)

const (
	TicksPerSecond = 20
	tickDurationMs = int64(1000 / TicksPerSecond)
)

// Ticker executes the interpreter tick sequence against the SQLite database.
// It is separated from Game so it can be tested without an Ebitengine window.
type Ticker struct {
	db           *sql.DB
	loader       *agent.Loader
	registry     *agent.Registry
	inputHandler agent.InputHandler
	mapID        string
}

func newTicker(db *sql.DB, loader *agent.Loader, registry *agent.Registry) *Ticker {
	return &Ticker{db: db, loader: loader, registry: registry}
}

// SetInputHandler registers a handler that is called each tick with drained input_events rows.
func (t *Ticker) SetInputHandler(h agent.InputHandler) {
	t.inputHandler = h
}

// SetMapID scopes authored entity lookups to the active map during a tick.
func (t *Ticker) SetMapID(id string) { t.mapID = id }

// RunTick executes one interpreter tick in a single SQLite transaction:
//  1. Read current_tick from world table.
//  2. Drain due event_queue rows and deliver them via SendEvent.
//  3. Deliver TICK to every entity with an active behavior_components row.
//  4. Advance current_tick and world_version.
func (t *Ticker) RunTick() error {
	tx, err := t.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	hasReferences := false
	if t.mapID != "" {
		hasReferences, err = storage.HasTileReferences(context.Background(), tx)
		if err != nil {
			return err
		}
	}

	// 1. Read current tick.
	var currentTick int64
	err = tx.QueryRow(`SELECT CAST(value AS INTEGER) FROM world WHERE key = 'current_tick'`).Scan(&currentTick)
	if err == sql.ErrNoRows {
		currentTick = 0
	} else if err != nil {
		return fmt.Errorf("reading current_tick: %w", err)
	}

	world := storage.NewTxWorldWriter(tx)
	reader := storage.NewTxWorldReader(tx)
	if t.mapID != "" {
		reader = storage.NewTxWorldReaderForMap(tx, t.mapID)
	}
	mw := storage.NewMachineWriter(tx)

	// 1.5. Drain and dispatch input_events.
	{
		iRows, err := tx.Query(`SELECT id, kind, payload FROM input_events WHERE consumed = 0 ORDER BY id ASC`)
		if err != nil {
			return fmt.Errorf("querying input_events: %w", err)
		}
		var inputEvents []agent.InputEvent
		for iRows.Next() {
			var e agent.InputEvent
			if err := iRows.Scan(&e.ID, &e.Kind, &e.Payload); err != nil {
				iRows.Close()
				return fmt.Errorf("scanning input_events: %w", err)
			}
			inputEvents = append(inputEvents, e)
		}
		iRows.Close()
		if err := iRows.Err(); err != nil {
			return fmt.Errorf("iterating input_events: %w", err)
		}
		if t.inputHandler != nil {
			if err := t.inputHandler.Handle(inputEvents, world, reader); err != nil {
				return fmt.Errorf("tick: input handler: %w", err)
			}
		}
		if _, err := tx.Exec(`UPDATE input_events SET consumed = 1 WHERE consumed = 0`); err != nil {
			return fmt.Errorf("marking input events consumed: %w", err)
		}
	}

	// 2. Drain due event_queue rows.
	type dueEvent struct {
		entityID  int64
		machineID string
		eventType string
	}
	dueQuery := `SELECT entity_id, machine_id, event_type FROM event_queue WHERE target_tick <= ?`
	dueArgs := []any{currentTick}
	if t.mapID != "" {
		dueQuery += " AND " + storage.ActiveMapEntityClause("event_queue.entity_id", hasReferences)
		dueArgs = append(dueArgs, storage.ActiveMapEntityArgs(t.mapID, hasReferences)...)
	}
	dueRows, err := tx.Query(dueQuery, dueArgs...)
	if err != nil {
		return fmt.Errorf("querying event_queue: %w", err)
	}
	var due []dueEvent
	for dueRows.Next() {
		var e dueEvent
		if err := dueRows.Scan(&e.entityID, &e.machineID, &e.eventType); err != nil {
			dueRows.Close()
			return fmt.Errorf("scanning event_queue: %w", err)
		}
		due = append(due, e)
	}
	dueRows.Close()
	if err := dueRows.Err(); err != nil {
		return fmt.Errorf("iterating event_queue: %w", err)
	}
	deleteQuery := `DELETE FROM event_queue WHERE target_tick <= ?`
	deleteArgs := []any{currentTick}
	if t.mapID != "" {
		deleteQuery += " AND " + storage.ActiveMapEntityClause("event_queue.entity_id", hasReferences)
		deleteArgs = append(deleteArgs, storage.ActiveMapEntityArgs(t.mapID, hasReferences)...)
	}
	if _, err := tx.Exec(deleteQuery, deleteArgs...); err != nil {
		return fmt.Errorf("deleting due events: %w", err)
	}

	// 3. Deliver due events.
	for _, e := range due {
		a, err := t.loadAgentFromDB(tx, e.entityID, e.machineID)
		if err != nil || a == nil {
			continue
		}
		if err := agent.SendEvent(a, agent.Event{Type: e.eventType}, currentTick, t.registry, world, reader, mw); err != nil {
			log.Printf("tick: deliver %q to entity %d: %v", e.eventType, e.entityID, err)
		}
	}

	// 4. Deliver TICK to all active behavior_components.
	behaviorQuery := `SELECT entity_id, machine_id, current_states FROM behavior_components`
	var behaviorArgs []any
	if t.mapID != "" {
		behaviorQuery += " WHERE " + storage.ActiveMapEntityClause("behavior_components.entity_id", hasReferences)
		behaviorArgs = append(behaviorArgs, storage.ActiveMapEntityArgs(t.mapID, hasReferences)...)
	}
	bcRows, err := tx.Query(behaviorQuery, behaviorArgs...)
	if err != nil {
		return fmt.Errorf("querying behavior_components: %w", err)
	}
	type bcRow struct {
		entityID      int64
		machineID     string
		currentStates string
	}
	var bcs []bcRow
	for bcRows.Next() {
		var r bcRow
		if err := bcRows.Scan(&r.entityID, &r.machineID, &r.currentStates); err != nil {
			bcRows.Close()
			return fmt.Errorf("scanning behavior_components: %w", err)
		}
		bcs = append(bcs, r)
	}
	bcRows.Close()
	if err := bcRows.Err(); err != nil {
		return fmt.Errorf("iterating behavior_components: %w", err)
	}

	for _, r := range bcs {
		var stateIDs []string
		if err := json.Unmarshal([]byte(r.currentStates), &stateIDs); err != nil {
			log.Printf("tick: parse states for entity %d: %v", r.entityID, err)
			continue
		}
		def, ok := t.loader.Get(r.machineID)
		if !ok {
			continue
		}
		a := agent.LoadAgent(def, r.entityID, stateIDs, tickDurationMs)
		if err := agent.SendEvent(a, agent.Event{Type: "TICK"}, currentTick, t.registry, world, reader, mw); err != nil {
			log.Printf("tick: TICK for entity %d: %v", r.entityID, err)
		}
	}

	// 5. Advance world state.
	if _, err := tx.Exec(
		`INSERT INTO world (key, value) VALUES ('current_tick', ?)
		 ON CONFLICT(key) DO UPDATE SET value = ?`,
		currentTick+1, currentTick+1,
	); err != nil {
		return fmt.Errorf("advancing current_tick: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO world (key, value) VALUES ('world_version', '1')
		 ON CONFLICT(key) DO UPDATE SET value = CAST(value AS INTEGER) + 1`,
	); err != nil {
		return fmt.Errorf("advancing world_version: %w", err)
	}

	log.Printf("[tick %d] done", currentTick+1)
	return tx.Commit()
}

// loadAgentFromDB reconstructs an Agent from behavior_components for the given entity/machine pair.
// Returns nil, nil if the machine definition is not loaded or no row exists.
func (t *Ticker) loadAgentFromDB(tx *sql.Tx, entityID int64, machineID string) (*agent.Agent, error) {
	def, ok := t.loader.Get(machineID)
	if !ok {
		return nil, nil
	}
	var raw string
	err := tx.QueryRow(
		`SELECT current_states FROM behavior_components WHERE entity_id = ? AND machine_id = ?`,
		entityID, machineID,
	).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loadAgentFromDB: %w", err)
	}
	var stateIDs []string
	if err := json.Unmarshal([]byte(raw), &stateIDs); err != nil {
		return nil, fmt.Errorf("loadAgentFromDB: parse states: %w", err)
	}
	return agent.LoadAgent(def, entityID, stateIDs, tickDurationMs), nil
}
