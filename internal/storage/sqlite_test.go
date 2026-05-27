package storage

import (
	"errors"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// ── NewSQLiteStoreWithConfig integration tests ────────────────────────────────

func TestNewSQLiteStore_VersionMatch_NoMigration(t *testing.T) {
	path := t.TempDir() + "/test.sqlite"

	s := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeNumber},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	// First open — bootstrap.
	store1, err := NewSQLiteStoreWithConfig(path, StoreConfig{Schema: s})
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_ = store1.Close()

	// Second open — same version, no migration should run.
	store2, err := NewSQLiteStoreWithConfig(path, StoreConfig{Schema: s})
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { _ = store2.Close() })

	var sv string
	_ = store2.db.QueryRow("SELECT value FROM meta WHERE key = 'schema_version'").Scan(&sv)
	if sv != "1" {
		t.Errorf("schema_version = %q, want 1", sv)
	}
}

func TestNewSQLiteStore_VersionMismatch_MigratesAndReturnsStore(t *testing.T) {
	path := t.TempDir() + "/test.sqlite"

	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}
	store1, err := NewSQLiteStoreWithConfig(path, StoreConfig{Schema: s1})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	_ = store1.Close()

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"tag": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	store2, err := NewSQLiteStoreWithConfig(path, StoreConfig{Schema: s2})
	if err != nil {
		t.Fatalf("migrated open: %v", err)
	}
	t.Cleanup(func() { _ = store2.Close() })

	// Store should be usable and migrated.
	if store2.DB() == nil {
		t.Error("DB() returned nil after migration")
	}
	var sv string
	_ = store2.db.QueryRow("SELECT value FROM meta WHERE key = 'schema_version'").Scan(&sv)
	if sv != "2" {
		t.Errorf("schema_version = %q, want 2", sv)
	}
	// New component table should exist.
	var count int
	_ = store2.db.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='comp_tag'",
	).Scan(&count)
	if count != 1 {
		t.Error("comp_tag table not created by migration")
	}
}

func TestNewSQLiteStore_MigrationFails_ReturnsError(t *testing.T) {
	path := t.TempDir() + "/test.sqlite"

	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeNumber},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	store1, err := NewSQLiteStoreWithConfig(path, StoreConfig{Schema: s1})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	// Poison the DB to make a rebuild fail.
	if _, err := store1.db.Exec("CREATE TABLE comp_position_new (entity_id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("poisoning: %v", err)
	}
	_ = store1.Close()

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	_, err = NewSQLiteStoreWithConfig(path, StoreConfig{Schema: s2})
	if err == nil {
		t.Fatal("expected migration error, got nil")
	}
	var migErr *SchemaMigrationError
	if !errors.As(err, &migErr) {
		t.Fatalf("expected *SchemaMigrationError, got %T: %v", err, err)
	}
}

func TestNewSQLiteStore_ConfirmPolicy_DestructiveReturnsConfirmationError(t *testing.T) {
	path := t.TempDir() + "/test.sqlite"

	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"droppable": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	store1, err := NewSQLiteStoreWithConfig(path, StoreConfig{Schema: s1})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	_ = store1.Close()

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}

	_, err = NewSQLiteStoreWithConfig(path, StoreConfig{
		Schema:          s2,
		MigrationPolicy: MigrationConfirm,
	})
	if err == nil {
		t.Fatal("expected *MigrationRequiresConfirmation, got nil")
	}
	var conf *MigrationRequiresConfirmation
	if !errors.As(err, &conf) {
		t.Fatalf("expected *MigrationRequiresConfirmation, got %T: %v", err, err)
	}
}

func TestNewSQLiteStore_BackwardCompatible_ThreeArgSignature(t *testing.T) {
	// Verify the old 3-arg NewSQLiteStore still works and auto-migrates.
	path := t.TempDir() + "/test.sqlite"

	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}
	store1, err := NewSQLiteStore(path, s1, "")
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_ = store1.Close()

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"name": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	store2, err := NewSQLiteStore(path, s2, "")
	if err != nil {
		t.Fatalf("migrated open: %v", err)
	}
	t.Cleanup(func() { _ = store2.Close() })

	if store2.DB() == nil {
		t.Error("DB() returned nil")
	}
}

func TestNewSQLiteStoreWithConfig_NilLoggerDefaultsToNop(t *testing.T) {
	// Passing nil Logger should not panic.
	path := t.TempDir() + "/test.sqlite"
	s := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}
	store, err := NewSQLiteStoreWithConfig(path, StoreConfig{
		Schema: s,
		Logger: nil, // should default to NopLogger
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
}
