package storage

import (
	"context"
	"database/sql"
	"errors"
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
		// ON DELETE CASCADE: the row is a live link and nothing else. It used
		// to be SET NULL, to record that an import had happened and stop a dead
		// spawn returning — which went with a create-once rule that has since
		// been replaced by the tile rule, where the file wins. Under that rule a
		// dead spawn *does* return, because the file still says there is a
		// goblin there, so a row whose entity is gone has nothing left to say.
		`CREATE TABLE IF NOT EXISTS spawns (
			map       TEXT NOT NULL,
			object_id INTEGER NOT NULL,
			entity_id INTEGER REFERENCES entities(id) ON DELETE CASCADE,
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
	if err := migrateSpawns(db); err != nil {
		return err
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("EnsureInterpreterTables: %w", err)
		}
	}
	return nil
}

// migrateSpawns rebuilds the spawns table when it was created with the foreign
// key it used to have.
//
// CREATE TABLE IF NOT EXISTS never runs again, so changing the DDL changes
// nothing about a database that already exists. The column went from ON DELETE
// SET NULL to ON DELETE CASCADE when the re-import rule changed, and a database
// left on the old one is not merely stale: a row whose entity died holds a NULL,
// the importer reads that as "not spawned", and its INSERT collides with the
// primary key the row still occupies — so the object is refused on every load,
// forever, with a SQLite constraint error naming an internal table.
//
// A row with a NULL entity is dropped rather than carried across. Under the rule
// that made this table, such a row said "this object has been spawned and its
// entity is gone, so do not spawn it again". Under the rule now, the file says
// there is a goblin there and there is not, so the object should spawn — and
// having no row is exactly how that is said.
func migrateSpawns(db *sql.DB) error {
	var onDelete string
	err := db.QueryRow(
		`SELECT on_delete FROM pragma_foreign_key_list('spawns') WHERE "from" = 'entity_id'`,
	).Scan(&onDelete)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // no table yet, or no such key: the CREATE below builds it
	}
	if err != nil {
		return fmt.Errorf("reading the spawns table's foreign key: %w", err)
	}
	if onDelete == "CASCADE" {
		return nil
	}

	// Foreign keys off for the rebuild, on the connection that runs it: the
	// pragma is per-connection and the pool would otherwise hand the
	// transaction a different one. The same reasoning as MigrationRunner's.
	conn, err := db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("acquiring a connection to rebuild spawns: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(context.Background(), "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disabling foreign keys to rebuild spawns: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON")
	}()

	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("beginning the spawns rebuild: %w", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE spawns_new (
			map       TEXT NOT NULL,
			object_id INTEGER NOT NULL,
			entity_id INTEGER REFERENCES entities(id) ON DELETE CASCADE,
			PRIMARY KEY (map, object_id)
		)`,
		`INSERT INTO spawns_new (map, object_id, entity_id)
			SELECT map, object_id, entity_id FROM spawns WHERE entity_id IS NOT NULL`,
		`DROP TABLE spawns`,
		`ALTER TABLE spawns_new RENAME TO spawns`,
	} {
		if _, err := tx.ExecContext(context.Background(), stmt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("rebuilding spawns: %s: %w", stmt, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing the spawns rebuild: %w", err)
	}
	return nil
}
