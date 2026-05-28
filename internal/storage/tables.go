package storage

import (
	"database/sql"
	"fmt"
)

// EnsureInterpreterTables creates the three interpreter-managed tables if they
// do not already exist. CREATE TABLE IF NOT EXISTS makes each call idempotent,
// so it is safe to call on both fresh and existing databases.
//
// Call this after NewSQLiteStore has bootstrapped or migrated the schema-managed
// tables — behavior_components references entities(id) which must exist first.
func EnsureInterpreterTables(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS behavior_components (
			entity_id      INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
			machine_id     TEXT NOT NULL,
			current_states TEXT NOT NULL,
			updated_at     INTEGER NOT NULL,
			PRIMARY KEY (entity_id, machine_id)
		)`,
		`CREATE TABLE IF NOT EXISTS transitions (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			tick        INTEGER NOT NULL,
			wall_ms     INTEGER NOT NULL,
			entity_id   INTEGER NOT NULL,
			machine_id  TEXT NOT NULL,
			from_states TEXT NOT NULL,
			to_states   TEXT NOT NULL,
			event       TEXT NOT NULL,
			cond_result INTEGER,
			actions_run TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS event_queue (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_id   INTEGER NOT NULL,
			machine_id  TEXT NOT NULL,
			event_type  TEXT NOT NULL,
			payload     TEXT,
			target_tick INTEGER NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("EnsureInterpreterTables: %w", err)
		}
	}
	return nil
}
