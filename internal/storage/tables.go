package storage

import (
	"database/sql"
	"fmt"
)

// EnsureInterpreterTables creates the tables the engine owns itself if they
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
		// spawns records which of a map's objects have already become entities.
		//
		// An object has no natural identity: the entity it made has moved, taken
		// damage or died by the time anything re-reads the file, so re-import
		// cannot match it by position the way tiles are matched by their cell.
		// What it can match is the object id, which is stable in the file.
		//
		// An engine table rather than a component, for the reason
		// behavior_components is one: every entity type in a real schema.json
		// sets allowExtraComponents false, so a Spawn component would have to be
		// declared optional on every type anybody ever spawns — the engine's
		// bookkeeping written into the author's file, in a place where
		// forgetting it is a refused spawn.
		//
		// ON DELETE SET NULL and not CASCADE: the row is a fact about the
		// import, and stays true after the goblin dies. Cascading would delete
		// it and spawn the goblin again on the next run, which is a level reset
		// dressed up as a restart. The null says the spawn happened and its
		// entity is gone, which is what is true.
		`CREATE TABLE IF NOT EXISTS spawns (
			map       TEXT NOT NULL,
			object_id INTEGER NOT NULL,
			entity_id INTEGER REFERENCES entities(id) ON DELETE SET NULL,
			PRIMARY KEY (map, object_id)
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
