package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tmbritton/ecs-db/internal/schema"
	_ "modernc.org/sqlite" // SQLite driver
)

// SQLiteStore handles database connections and operations
type SQLiteStore struct {
	db     *sql.DB
	schema schema.DatabaseSchema
}

// StoreConfig holds all options for opening or creating a SQLite store.
type StoreConfig struct {
	// Schema is the current schema.json representation.
	Schema schema.DatabaseSchema
	// SchemaHash is an optional SHA-256 hex digest of the schema.json bytes.
	// Pass "" to omit it from meta.
	SchemaHash string
	// MigrationPolicy controls whether destructive migrations run automatically
	// (MigrationAuto, the default) or require confirmation (MigrationConfirm).
	MigrationPolicy MigrationPolicy
	// Logger receives structured migration events. Nil defaults to NopLogger.
	Logger MigrationLogger
	// BackupRetention is the number of versioned backups to keep before migration.
	// 0 (the default) disables backup. A positive value enables backup and retention.
	BackupRetention int
}

// NewSQLiteStore opens or creates a SQLite database at dbPath using the
// provided schema. On version mismatch the runner auto-migrates (MigrationAuto).
// This is the backward-compatible 3-argument form; use NewSQLiteStoreWithConfig
// for full control over migration policy and logging.
//
// schemaHash is an optional SHA-256 hex digest of the schema.json bytes.
func NewSQLiteStore(dbPath string, s schema.DatabaseSchema, schemaHash string) (*SQLiteStore, error) {
	return NewSQLiteStoreWithConfig(dbPath, StoreConfig{
		Schema:          s,
		SchemaHash:      schemaHash,
		MigrationPolicy: MigrationAuto,
		Logger:          NopLogger(),
	})
}

// NewSQLiteStoreWithConfig opens or creates a SQLite database at dbPath.
//
// On first run (no tables exist), it creates all tables and writes
// schema_version, build_time, and optionally schema_hash to the meta table.
//
// On subsequent opens (tables exist), it compares the stored schema_version
// against the schema in cfg. If versions match, existing data is preserved.
// If versions differ, the migration runner runs: introspect → diff → DDL →
// execute in one transaction → update meta. Returns an error only on failure
// or when cfg.MigrationPolicy = MigrationConfirm and destructive changes exist.
func NewSQLiteStoreWithConfig(dbPath string, cfg StoreConfig) (*SQLiteStore, error) {
	if cfg.Logger == nil {
		cfg.Logger = NopLogger()
	}

	// Ensure directory exists
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		_ = db.Close()
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
			_ = db.Close()
			return nil, fmt.Errorf("applying pragma: %s: %w", pragma, err)
		}
	}

	// Detect fresh vs existing database by checking if the meta table exists.
	existing, err := tablesExist(db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("checking for existing database: %w", err)
	}

	if existing {
		// Existing database — check version and migrate if needed.
		if err := checkAndMigrate(db, dbPath, cfg); err != nil {
			_ = db.Close()
			return nil, err
		}
	} else {
		// Fresh database — create all tables and write meta.
		if err := bootstrapDatabase(db, cfg.Schema, cfg.SchemaHash); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("bootstrapping database: %w", err)
		}
	}

	return &SQLiteStore{db: db, schema: cfg.Schema}, nil
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

// tablesExist checks whether the meta table exists in the database,
// as a proxy for "has this database been initialised before".
func tablesExist(db *sql.DB) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='meta'",
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("querying sqlite_master: %w", err)
	}
	return count > 0, nil
}

// checkSchemaVersion reads the stored schema_version from meta and
// compares it against the provided version. Returns *SchemaVersionMismatchError
// on mismatch, nil on match.
func checkSchemaVersion(db *sql.DB, currentVersion int) error {
	var stored string
	err := db.QueryRow(
		"SELECT value FROM meta WHERE key = 'schema_version'",
	).Scan(&stored)
	if err == sql.ErrNoRows {
		return fmt.Errorf("database exists but meta table is missing schema_version")
	}
	if err != nil {
		return fmt.Errorf("reading stored schema_version: %w", err)
	}

	dbVersion, err := strconv.Atoi(stored)
	if err != nil {
		return fmt.Errorf("corrupted schema_version in meta: %q", stored)
	}
	if dbVersion != currentVersion {
		return &SchemaVersionMismatchError{
			DBVersion:   dbVersion,
			FileVersion: currentVersion,
		}
	}
	return nil
}

// isMemoryDB reports whether dbPath refers to an in-memory SQLite database,
// for which file-based backup is not applicable.
func isMemoryDB(path string) bool {
	return path == "" || strings.Contains(path, ":memory:") || strings.Contains(path, "mode=memory")
}

// checkAndMigrate reads the stored version. If it matches the config schema
// version, it returns nil immediately. On mismatch, it optionally backs up
// the database (when cfg.BackupRetention > 0) then runs the migration runner.
func checkAndMigrate(db *sql.DB, dbPath string, cfg StoreConfig) error {
	err := checkSchemaVersion(db, cfg.Schema.SchemaVersion)
	if err == nil {
		return nil // versions match, nothing to do
	}

	// Only proceed if the error is a version mismatch; other errors are fatal.
	var mismatch *SchemaVersionMismatchError
	if !errors.As(err, &mismatch) {
		return err
	}

	// Back up before migration so the user has a restore point.
	if cfg.BackupRetention > 0 && !isMemoryDB(dbPath) {
		backupPath, backupErr := backupDatabase(db, dbPath, mismatch.DBVersion)
		if backupErr != nil {
			cfg.Logger.Warnf("backup failed (migration will proceed): %v", backupErr)
		} else {
			cfg.Logger.Infof("backup created: %s", backupPath)
			pruneBackups(dbPath, cfg.BackupRetention, cfg.Logger)
		}
	}

	// Run the migration pipeline.
	runner := NewMigrationRunner(db, cfg.Schema, cfg.MigrationPolicy, cfg.Logger)
	return runner.Run()
}

// bootstrapDatabase creates all tables and writes initial meta rows
// in a single transaction. The meta table is created first (outside the
// transaction, as DDL auto-commits in SQLite), then remaining tables
// and meta data are written inside a transaction.
func bootstrapDatabase(db *sql.DB, s schema.DatabaseSchema, schemaHash string) error {
	// Create meta first so that tablesExist works after partial failure.
	if _, err := db.Exec(`
		CREATE TABLE meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("creating meta table: %w", err)
	}

	// Begin transaction for the remaining DDL + meta writes.
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Remaining fixed tables.
	fixed := `
	CREATE TABLE world (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE entities (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		entity_type TEXT NOT NULL,
		created_tick INTEGER NOT NULL DEFAULT 0
	);

	CREATE INDEX idx_entity_type ON entities(entity_type);

	CREATE TABLE event_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tick INTEGER NOT NULL,
		target_entity INTEGER,
		kind TEXT NOT NULL,
		payload TEXT NOT NULL DEFAULT '{}'
	);

	CREATE TABLE input_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		received_at_ms INTEGER NOT NULL,
		kind TEXT NOT NULL,
		payload TEXT NOT NULL DEFAULT '{}',
		consumed INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE transitions (
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
	CREATE INDEX idx_event_queue_tick ON event_queue(tick);
	CREATE INDEX idx_input_events_consumed ON input_events(consumed);
	CREATE INDEX idx_transitions_entity_id ON transitions(entity_id);
	`
	if _, err := tx.Exec(fixed); err != nil {
		return fmt.Errorf("creating fixed tables: %w", err)
	}

	// Generate component tables.
	for name, comp := range s.Components {
		stmt, err := componentTableSQL(name, comp)
		if err != nil {
			return fmt.Errorf("building table for component %q: %w", name, err)
		}
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("creating table for component %q: %w", name, err)
		}
	}

	// Write meta rows.
	if _, err := tx.Exec(
		"INSERT INTO meta (key, value) VALUES ('schema_version', ?)",
		fmt.Sprintf("%d", s.SchemaVersion),
	); err != nil {
		return fmt.Errorf("recording schema_version: %w", err)
	}

	if _, err := tx.Exec(
		"INSERT INTO meta (key, value) VALUES ('build_time', ?)",
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("recording build_time: %w", err)
	}

	if schemaHash != "" {
		if _, err := tx.Exec(
			"INSERT INTO meta (key, value) VALUES ('schema_hash', ?)",
			schemaHash,
		); err != nil {
			return fmt.Errorf("recording schema_hash: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing bootstrap transaction: %w", err)
	}
	return nil
}
