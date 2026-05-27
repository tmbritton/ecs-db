package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/tmbritton/ecs-db/internal/schema"
	_ "modernc.org/sqlite" // SQLite driver
)

func TestNewSQLiteStore_FreshDatabaseWritesSchemaVersionToMeta(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 4,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var value string
	err = store.db.QueryRow(
		"SELECT value FROM meta WHERE key = 'schema_version'",
	).Scan(&value)
	if err != nil {
		t.Fatalf("reading schema_version: %v", err)
	}
	if value != "4" {
		t.Errorf("schema_version = %q, want %q", value, "4")
	}
}

func TestNewSQLiteStore_FreshDatabaseWritesBuildTimeToMeta(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var value string
	err = store.db.QueryRow(
		"SELECT value FROM meta WHERE key = 'build_time'",
	).Scan(&value)
	if err != nil {
		t.Fatalf("reading build_time: %v", err)
	}
	// Verify it's a valid RFC 3339 timestamp.
	if _, parseErr := time.Parse(time.RFC3339, value); parseErr != nil {
		t.Errorf("build_time = %q is not valid RFC 3339: %v", value, parseErr)
	}
}

func TestNewSQLiteStore_FreshDatabaseWritesSchemaHashToMeta(t *testing.T) {
	fakeSchema := []byte(`{"schemaVersion":1,"components":{"Pos":{"type":"object","properties":{"x":{"type":"number"}}},"entityTypes":{"Player":{"requiredComponents":["Pos"]}}}`)
	hash := sha256.Sum256(fakeSchema)
	hashHex := hex.EncodeToString(hash[:])

	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Pos": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{
			"Player": {RequiredComponents: []string{"Pos"}},
		},
	}, hashHex)
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var value string
	err = store.db.QueryRow(
		"SELECT value FROM meta WHERE key = 'schema_hash'",
	).Scan(&value)
	if err != nil {
		t.Fatalf("reading schema_hash: %v", err)
	}
	if value != hashHex {
		t.Errorf("schema_hash = %q, want %q", value, hashHex)
	}
}

func TestNewSQLiteStore_FreshDatabaseSkipsSchemaHashWhenEmpty(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var count int
	err = store.db.QueryRow(
		"SELECT count(*) FROM meta WHERE key = 'schema_hash'",
	).Scan(&count)
	if err != nil {
		t.Fatalf("counting schema_hash: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no schema_hash row when hash is empty, got count=%d", count)
	}
}

func TestNewSQLiteStore_ExistingDatabaseMatchingVersionSucceeds(t *testing.T) {
	path := t.TempDir() + "/test.sqlite"

	// First open — creates the database.
	store, err := NewSQLiteStore(path, schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("first NewSQLiteStore error: %v", err)
	}
	// Insert something to verify it's preserved.
	_, err = store.db.Exec("INSERT INTO world (key, value) VALUES ('current_tick', '42')")
	if err != nil {
		t.Fatalf("inserting test data: %v", err)
	}
	_ = store.Close()

	// Second open — same version, should succeed.
	store2, err := NewSQLiteStore(path, schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("second NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store2.Close() })

	// Verify the data we inserted is still there (tables weren't recreated).
	var tick int64
	err = store2.db.QueryRow(
		"SELECT CAST(value AS INTEGER) FROM world WHERE key = 'current_tick'",
	).Scan(&tick)
	if err != nil {
		t.Fatalf("reading preserved data: %v", err)
	}
	if tick != 42 {
		t.Errorf("preserved tick = %d, want 42", tick)
	}

	// Verify meta has exactly one schema_version row (no duplicates from INSERT OR REPLACE).
	var metaCount int
	err = store2.db.QueryRow(
		"SELECT count(*) FROM meta WHERE key = 'schema_version'",
	).Scan(&metaCount)
	if err != nil {
		t.Fatalf("counting meta rows: %v", err)
	}
	if metaCount != 1 {
		t.Errorf("meta schema_version count = %d, want 1", metaCount)
	}
}

func TestNewSQLiteStore_ExistingDatabaseMismatchedVersionAutoMigrates(t *testing.T) {
	// NewSQLiteStore now auto-migrates on version mismatch (MigrationAuto).
	path := t.TempDir() + "/test.sqlite"

	// Create the database at version 1 with no components.
	store, err := NewSQLiteStore(path, schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("first NewSQLiteStore error: %v", err)
	}
	_ = store.Close()

	// Open with version 2 — should auto-migrate successfully.
	store2, err := NewSQLiteStore(path, schema.DatabaseSchema{
		SchemaVersion: 2,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("expected auto-migration to succeed, got: %v", err)
	}
	t.Cleanup(func() { _ = store2.Close() })

	// schema_version should now be 2.
	var value string
	if err := store2.db.QueryRow(
		"SELECT value FROM meta WHERE key = 'schema_version'",
	).Scan(&value); err != nil {
		t.Fatalf("reading schema_version: %v", err)
	}
	if value != "2" {
		t.Errorf("schema_version = %q, want 2", value)
	}
}

func TestNewSQLiteStore_FilenameHasNoVersionSuffix(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/ecs.db"

	store, err := NewSQLiteStore(path, schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// The file must be exactly at the requested path.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("database file not found at %q", path)
	}

	// There should be no extra files like "ecs.db-1" or "ecs.db-v1" in the directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading directory: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "ecs.db" && entry.Name() != "ecs.db-wal" && entry.Name() != "ecs.db-shm" {
			t.Errorf("unexpected file %q in database directory", entry.Name())
		}
	}
}

func TestNewSQLiteStore_MetaTableCreatedFirst(t *testing.T) {
	// We verify this indirectly: the initialization logic creates `meta`
	// before the DDL transaction. If meta didn't exist first, the
	// fresh-vs-existing detection (which queries for `meta`) would fail
	// to distinguish fresh from existing. The fact that the rest of this
	// test suite's tests pass (they all create fresh databases and then
	// read from meta) validates that meta is created before any other
	// table or component table.
	//
	// This test exists as documentation; the real verification is that
	// the initialization code explicitly creates meta in its own Exec
	// call before the transaction that creates other tables.
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Pos": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{
			"Player": {RequiredComponents: []string{"Pos"}},
		},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Verify meta exists and is queryable.
	var val string
	err = store.db.QueryRow(
		"SELECT value FROM meta WHERE key = 'schema_version'",
	).Scan(&val)
	if err != nil {
		t.Errorf("meta table not queryable: %v", err)
	}

	// Verify the component table also exists.
	var count int
	err = store.db.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='comp_pos'",
	).Scan(&count)
	if err != nil {
		t.Fatalf("checking comp_pos: %v", err)
	}
	if count != 1 {
		t.Error("component table comp_pos not found")
	}
}

func TestNewSQLiteStore_DoesNotRecreateTablesOnExistingDB(t *testing.T) {
	path := t.TempDir() + "/test.sqlite"

	// Create the database.
	store, err := NewSQLiteStore(path, schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("first NewSQLiteStore error: %v", err)
	}

	// Count sqlite_master entries before close.
	var beforeCount int
	err = store.db.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type IN ('table','index')",
	).Scan(&beforeCount)
	if err != nil {
		t.Fatalf("counting master before: %v", err)
	}
	_ = store.Close()

	// Reopen — same schema.
	store2, err := NewSQLiteStore(path, schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("reopen NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store2.Close() })

	// Count sqlite_master entries after reopen.
	var afterCount int
	err = store2.db.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type IN ('table','index')",
	).Scan(&afterCount)
	if err != nil {
		t.Fatalf("counting master after: %v", err)
	}

	if afterCount != beforeCount {
		t.Errorf("master entry count changed: before=%d, after=%d (tables were recreated)", beforeCount, afterCount)
	}
}

func TestNewSQLiteStore_ExistingDBMissingSchemaVersionReturnsError(t *testing.T) {
	path := t.TempDir() + "/test.sqlite"

	// Create a database with a meta table but no schema_version row.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening raw db: %v", err)
	}
	// Apply pragmas so it looks like a real initialized DB.
	_, _ = db.Exec("PRAGMA journal_mode = WAL")
	_, _ = db.Exec("PRAGMA foreign_keys = ON")
	_, err = db.Exec("CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)")
	if err != nil {
		t.Fatalf("creating meta: %v", err)
	}
	_ = db.Close()

	// Attempt to open — should error because schema_version is missing.
	_, err = NewSQLiteStore(path, schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err == nil {
		t.Fatal("expected error for missing schema_version, got nil")
	}

	// Should NOT be a SchemaVersionMismatchError — it's a different failure.
	var mismatch *SchemaVersionMismatchError
	if errors.As(err, &mismatch) {
		t.Errorf("expected a missing-schema-version error, got SchemaVersionMismatchError: %v", err)
	}
}
