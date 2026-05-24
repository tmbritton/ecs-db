package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
	"github.com/tmbritton/ecs-db/internal/schema"
)

// SQLiteStore handles database connections and operations
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens or creates a SQLite database at the given path and
// initialises the fixed + generated tables from the schema.
func NewSQLiteStore(dbPath string, s schema.DatabaseSchema) (*SQLiteStore, error) {
	// Ensure directory exists
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Apply pragmas
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("applying pragma: %s: %w", pragma, err)
		}
	}

	// Create tables
	if err := createTables(db, s); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialise schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DB returns the underlying *sql.DB for adapters that need direct access.
func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

func createTables(db *sql.DB, s schema.DatabaseSchema) error {
	// Fixed tables
	fixed := `
	CREATE TABLE IF NOT EXISTS meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS world (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS entities (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		entity_type TEXT NOT NULL,
		created_tick INTEGER NOT NULL DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_entity_type ON entities(entity_type);

	CREATE TABLE IF NOT EXISTS event_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tick INTEGER NOT NULL,
		target_entity INTEGER,
		kind TEXT NOT NULL,
		payload TEXT NOT NULL DEFAULT '{}'
	);

	CREATE TABLE IF NOT EXISTS input_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		received_at_ms INTEGER NOT NULL,
		kind TEXT NOT NULL,
		payload TEXT NOT NULL DEFAULT '{}',
		consumed INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS transitions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tick INTEGER NOT NULL,
		wall_ms INTEGER NOT NULL,
		entity_id INTEGER NOT NULL,
		machine_id TEXT NOT NULL,
		from_state TEXT NOT NULL,
		to_state TEXT NOT NULL,
		event TEXT NOT NULL,
		guard_result TEXT,
		actions_run TEXT
	);

	-- Indexes on query-hot columns
	CREATE INDEX IF NOT EXISTS idx_event_queue_tick ON event_queue(tick);
	CREATE INDEX IF NOT EXISTS idx_input_events_consumed ON input_events(consumed);
	CREATE INDEX IF NOT EXISTS idx_transitions_entity_id ON transitions(entity_id);
	`
	if _, err := db.Exec(fixed); err != nil {
		return fmt.Errorf("creating fixed tables: %w", err)
	}

	// Record schema version in meta
	if _, err := db.Exec(
		"INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_version', ?)",
		fmt.Sprintf("%d", s.SchemaVersion),
	); err != nil {
		return fmt.Errorf("recording schema_version: %w", err)
	}

	// Generate component tables
	for name, comp := range s.Components {
		stmt, err := componentTableSQL(name, comp)
		if err != nil {
			return fmt.Errorf("building table for component %q: %w", name, err)
		}
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("creating table for component %q: %w", name, err)
		}
	}

	return nil
}
