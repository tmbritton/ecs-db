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
	if cfg.MigrationPolicy == "" {
		cfg.MigrationPolicy = MigrationAuto
	}

	// An empty path is not "the default database", it is a private temporary
	// one: SQLite opens "" as an unnamed on-disk database that is deleted when
	// the connection closes. Bootstrapping a whole schema into it succeeds and
	// then evaporates, which is a confusing way to lose a morning. Refused here
	// because every caller reaching this point got the path from config, and a
	// config with no database path is a config to fix rather than a default to
	// invent.
	if dbPath == "" {
		return nil, fmt.Errorf("no database path: SQLite would open a temporary database and discard it on close")
	}

	// Ensure directory exists
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection. The connection settings ride in the DSN — see
	// DSN for why they cannot be statements run after this.
	db, err := sql.Open("sqlite", DSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// journal_mode is the one that is not a connection setting: WAL is recorded
	// in the database file, so it is set once here and every connection opened
	// afterwards — by this process or another — reads it back. In the DSN it
	// would be a redundant statement on every connection, and a read-only
	// connection cannot set it at all.
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("applying pragma: PRAGMA journal_mode = WAL: %w", err)
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

// checkAndMigrate works out what the database needs and, if it needs anything,
// backs it up and does it.
//
// It used to return the moment the stored schema_version equalled the file's,
// without introspecting at all — which made schemaVersion the only signal that
// a migration was wanted, and it is the wrong one. The generator's output
// changes when the *engine* changes: Story 9 put ON DELETE CASCADE on every
// reference to an entity, and no schema file moved, so no database ever got it.
// The same gate is why a schema edit saved without a version bump did nothing,
// which Forge had to warn about rather than rely on.
//
// The version is still read first, for the clear errors it gives on a meta row
// that is missing or corrupt; it just no longer decides. What decides is the
// plan, which is empty for a database that already matches — so the ordinary
// open still does no work beyond introspecting.
func checkAndMigrate(db *sql.DB, dbPath string, cfg StoreConfig) error {
	if err := checkSchemaVersion(db, cfg.Schema.SchemaVersion); err != nil {
		// A version mismatch is a thing to migrate, not a thing to refuse.
		// Anything else — no meta row, an unparseable one — is fatal.
		var mismatch *SchemaVersionMismatchError
		if !errors.As(err, &mismatch) {
			return err
		}
	}

	runner := NewMigrationRunner(db, cfg.Schema, cfg.MigrationPolicy, cfg.Logger)
	plan, err := runner.Plan()
	if err != nil {
		return err
	}
	if plan.Empty() {
		return nil
	}

	// Back up before migration so the user has a restore point.
	if cfg.BackupRetention > 0 && !isMemoryDB(dbPath) {
		backupPath, backupErr := backupDatabase(db, dbPath, plan.FromVersion)
		if backupErr != nil {
			cfg.Logger.Warnf("backup failed (migration will proceed): %v", backupErr)
		} else {
			cfg.Logger.Infof("backup created: %s", backupPath)
			pruneBackups(dbPath, cfg.BackupRetention, cfg.Logger)
		}
	}

	return runner.Apply(plan)
}

// bootstrapDatabase creates every table and writes the initial meta rows in one
// transaction, so a failure leaves no database rather than half of one.
//
// This comment used to say that meta was created first and outside the
// transaction "as DDL auto-commits in SQLite". DDL does not auto-commit in
// SQLite — CREATE TABLE and CREATE INDEX roll back with everything else — and
// that mistaken belief is what produced the defect the body now explains.
func bootstrapDatabase(db *sql.DB, s schema.DatabaseSchema, schemaHash string) error {
	// Everything in one transaction, meta included.
	//
	// meta used to be created first and outside it, "so that tablesExist works
	// after partial failure" — but tablesExist asks whether meta is there, and
	// that is how the store tells a database it must migrate from one it must
	// build. A bootstrap that failed therefore left behind the single table
	// that makes the next open take the *migration* path, against a database
	// with no entities table and no recorded version. The half-built state was
	// not being detected by that ordering, it was being created by it.
	//
	// SQLite runs DDL inside a transaction, so a failure now rolls back to a
	// database with no tables at all — which tablesExist reads as "fresh", and
	// the next open builds it properly.
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	fixed := `
	CREATE TABLE meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

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
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		entity_id   INTEGER NOT NULL,
		machine_id  TEXT NOT NULL,
		event_type  TEXT NOT NULL,
		payload     TEXT,
		target_tick INTEGER NOT NULL
	);

	CREATE TABLE input_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		received_at_ms INTEGER NOT NULL,
		kind TEXT NOT NULL,
		payload TEXT NOT NULL DEFAULT '{}',
		consumed INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE transitions (
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
	);

	-- Indexes on query-hot columns
	CREATE INDEX idx_event_queue_target_tick ON event_queue(target_tick);
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
