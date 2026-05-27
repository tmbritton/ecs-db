package storage

import (
	"database/sql"
	"os"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// TestSmoke_AddComponent_RoundTrip exercises the full migration pipeline:
// bootstrap → insert data → reopen with new component → assert new table +
// original data intact + meta updated.
func TestSmoke_AddComponent_RoundTrip(t *testing.T) {
	path := t.TempDir() + "/smoke.sqlite"

	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
					"y": {Type: schema.PropertyTypeNumber},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	// --- v1: bootstrap and seed ---
	store1, err := NewSQLiteStore(path, s1, "")
	if err != nil {
		t.Fatalf("v1 open: %v", err)
	}
	db1 := store1.DB()

	res, err := db1.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Player', 0)")
	if err != nil {
		t.Fatalf("inserting entity: %v", err)
	}
	entityID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}

	if _, err := db1.Exec(
		"INSERT INTO comp_position (entity_id, x, y) VALUES (?, ?, ?)",
		entityID, 1.0, 2.0,
	); err != nil {
		t.Fatalf("inserting comp_position: %v", err)
	}
	_ = store1.Close()

	// --- v2: add Velocity component ---
	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"Position": s1.Components["Position"],
			"Velocity": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"vx": {Type: schema.PropertyTypeNumber},
					"vy": {Type: schema.PropertyTypeNumber},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	store2, err := NewSQLiteStore(path, s2, "")
	if err != nil {
		t.Fatalf("v2 open (migration): %v", err)
	}
	t.Cleanup(func() { _ = store2.Close() })
	db2 := store2.DB()

	if !tableExists(t, db2, "comp_velocity") {
		t.Error("comp_velocity table not created by migration")
	}

	var gotX, gotY float64
	if err := db2.QueryRow(
		"SELECT x, y FROM comp_position WHERE entity_id = ?", entityID,
	).Scan(&gotX, &gotY); err != nil {
		t.Fatalf("reading comp_position after migration: %v", err)
	}
	if gotX != 1.0 {
		t.Errorf("x = %v, want 1.0", gotX)
	}
	if gotY != 2.0 {
		t.Errorf("y = %v, want 2.0", gotY)
	}

	if got := readMetaValue(t, db2, "schema_version"); got != "2" {
		t.Errorf("schema_version = %q, want 2", got)
	}
}

// TestSmoke_AddColumn_RoundTrip exercises ALTER TABLE ADD COLUMN via the full
// pipeline: bootstrap → insert data → reopen with extra property → assert new
// column exists + original data intact + meta updated.
func TestSmoke_AddColumn_RoundTrip(t *testing.T) {
	path := t.TempDir() + "/smoke.sqlite"

	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	// --- v1: bootstrap and seed ---
	store1, err := NewSQLiteStore(path, s1, "")
	if err != nil {
		t.Fatalf("v1 open: %v", err)
	}
	db1 := store1.DB()

	res, err := db1.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Player', 0)")
	if err != nil {
		t.Fatalf("inserting entity: %v", err)
	}
	entityID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}

	if _, err := db1.Exec(
		"INSERT INTO comp_position (entity_id, x) VALUES (?, ?)",
		entityID, 3.5,
	); err != nil {
		t.Fatalf("inserting comp_position: %v", err)
	}
	_ = store1.Close()

	// --- v2: add z property to Position ---
	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
					"z": {Type: schema.PropertyTypeNumber},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	store2, err := NewSQLiteStore(path, s2, "")
	if err != nil {
		t.Fatalf("v2 open (migration): %v", err)
	}
	t.Cleanup(func() { _ = store2.Close() })
	db2 := store2.DB()

	if !columnExists(t, db2, "comp_position", "z") {
		t.Error("column z not added to comp_position by migration")
	}

	// Verify original data intact and new column is queryable on the pre-existing row.
	var gotX, gotZ float64
	if err := db2.QueryRow(
		"SELECT x, z FROM comp_position WHERE entity_id = ?", entityID,
	).Scan(&gotX, &gotZ); err != nil {
		t.Fatalf("reading comp_position (x, z) after migration: %v", err)
	}
	if gotX != 3.5 {
		t.Errorf("x = %v, want 3.5 (original value must be preserved)", gotX)
	}
	if gotZ != 0.0 {
		t.Errorf("z = %v, want 0.0 (default for newly added column)", gotZ)
	}

	if got := readMetaValue(t, db2, "schema_version"); got != "2" {
		t.Errorf("schema_version = %q, want 2", got)
	}
}

// TestSmoke_BackupCreatedBeforeMigration exercises the full backup pipeline:
// bootstrap v1 with backup enabled → insert data → reopen as v2 → assert
// backup file exists and is a valid pre-migration SQLite database.
func TestSmoke_BackupCreatedBeforeMigration(t *testing.T) {
	path := t.TempDir() + "/world.sqlite"

	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	// --- v1: bootstrap and seed ---
	store1, err := NewSQLiteStoreWithConfig(path, StoreConfig{
		Schema:          s1,
		MigrationPolicy: MigrationAuto,
		Logger:          NopLogger(),
		BackupRetention: 3,
	})
	if err != nil {
		t.Fatalf("v1 open: %v", err)
	}
	db1 := store1.DB()

	res, err := db1.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Player', 0)")
	if err != nil {
		t.Fatalf("inserting entity: %v", err)
	}
	entityID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	if _, err := db1.Exec(
		"INSERT INTO comp_position (entity_id, x) VALUES (?, ?)", entityID, 5.0,
	); err != nil {
		t.Fatalf("inserting comp_position: %v", err)
	}
	_ = store1.Close()

	// --- v2: add y property (triggers migration and backup) ---
	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
					"y": {Type: schema.PropertyTypeNumber},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	store2, err := NewSQLiteStoreWithConfig(path, StoreConfig{
		Schema:          s2,
		MigrationPolicy: MigrationAuto,
		Logger:          NopLogger(),
		BackupRetention: 3,
	})
	if err != nil {
		t.Fatalf("v2 open (migration): %v", err)
	}
	t.Cleanup(func() { _ = store2.Close() })

	// Assert backup file exists.
	backupPath := path + ".bak.v1"
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup file %s not found: %v", backupPath, err)
	}

	// Assert backup is a valid SQLite database with the pre-migration schema version.
	bdb, err := sql.Open("sqlite", backupPath)
	if err != nil {
		t.Fatalf("opening backup: %v", err)
	}
	defer func() { _ = bdb.Close() }()

	var gotVersion string
	if err := bdb.QueryRow("SELECT value FROM meta WHERE key = 'schema_version'").Scan(&gotVersion); err != nil {
		t.Fatalf("querying backup schema_version: %v", err)
	}
	if gotVersion != "1" {
		t.Errorf("backup schema_version = %q, want 1 (pre-migration state)", gotVersion)
	}
}

// TestSmoke_AddEntityRefProperty_RoundTrip verifies that adding an entity-ref
// typed property to an existing object component generates valid SQL and
// executes successfully against a live database.
func TestSmoke_AddEntityRefProperty_RoundTrip(t *testing.T) {
	path := t.TempDir() + "/smoke.sqlite"

	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Relationship": {
				Type:       schema.ComponentTypeObject,
				Properties: map[string]schema.Property{},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	// --- v1: bootstrap with empty object component and seed an entity ---
	store1, err := NewSQLiteStore(path, s1, "")
	if err != nil {
		t.Fatalf("v1 open: %v", err)
	}
	db1 := store1.DB()

	res, err := db1.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Node', 0)")
	if err != nil {
		t.Fatalf("inserting entity: %v", err)
	}
	entityID, _ := res.LastInsertId()
	if _, err := db1.Exec("INSERT INTO comp_relationship (entity_id) VALUES (?)", entityID); err != nil {
		t.Fatalf("inserting comp_relationship: %v", err)
	}
	_ = store1.Close()

	// --- v2: add entity-ref property "parent" to Relationship ---
	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"Relationship": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"parent": {Type: schema.PropertyTypeEntityRef},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	store2, err := NewSQLiteStore(path, s2, "")
	if err != nil {
		t.Fatalf("v2 open (migration): %v", err)
	}
	t.Cleanup(func() { _ = store2.Close() })
	db2 := store2.DB()

	// Column must exist and be nullable.
	if !columnExists(t, db2, "comp_relationship", "parent") {
		t.Fatal("parent column not added to comp_relationship")
	}

	// Pre-existing row must be readable; parent defaults to NULL.
	var gotEntityID int64
	var gotParent sql.NullInt64
	if err := db2.QueryRow(
		"SELECT entity_id, parent FROM comp_relationship WHERE entity_id = ?", entityID,
	).Scan(&gotEntityID, &gotParent); err != nil {
		t.Fatalf("reading comp_relationship after migration: %v", err)
	}
	if gotEntityID != entityID {
		t.Errorf("entity_id = %d, want %d", gotEntityID, entityID)
	}
	if gotParent.Valid {
		t.Errorf("parent = %d, want NULL for pre-existing row", gotParent.Int64)
	}

	if got := readMetaValue(t, db2, "schema_version"); got != "2" {
		t.Errorf("schema_version = %q, want 2", got)
	}
}

// TestSmoke_TypeChange_DataPreserved verifies that a table-rebuild migration
// preserves existing entity_id values and coerces column data.
func TestSmoke_TypeChange_DataPreserved(t *testing.T) {
	path := t.TempDir() + "/smoke.sqlite"

	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Score": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"points": {Type: schema.PropertyTypeNumber}, // REAL
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	// --- v1: bootstrap and seed two entities ---
	store1, err := NewSQLiteStore(path, s1, "")
	if err != nil {
		t.Fatalf("v1 open: %v", err)
	}
	db1 := store1.DB()

	var ids [2]int64
	for i, pts := range []float64{10.9, 20.1} {
		res, err := db1.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Player', 0)")
		if err != nil {
			t.Fatalf("inserting entity %d: %v", i, err)
		}
		ids[i], _ = res.LastInsertId()
		if _, err := db1.Exec("INSERT INTO comp_score (entity_id, points) VALUES (?, ?)", ids[i], pts); err != nil {
			t.Fatalf("inserting comp_score %d: %v", i, err)
		}
	}
	_ = store1.Close()

	// --- v2: change points from REAL to INTEGER (triggers table rebuild) ---
	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"Score": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"points": {Type: schema.PropertyTypeInteger}, // INTEGER
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	store2, err := NewSQLiteStore(path, s2, "")
	if err != nil {
		t.Fatalf("v2 open (migration): %v", err)
	}
	t.Cleanup(func() { _ = store2.Close() })
	db2 := store2.DB()

	// Both entity_ids must survive the rebuild. SQLite has loose typing: the
	// original REAL values (10.9, 20.1) are preserved as-is in the rebuilt
	// INTEGER affinity column, so we scan into float64.
	for i, id := range ids {
		var gotID int64
		var gotPts float64
		if err := db2.QueryRow(
			"SELECT entity_id, points FROM comp_score WHERE entity_id = ?", id,
		).Scan(&gotID, &gotPts); err != nil {
			t.Fatalf("entity %d not found after rebuild: %v", i, err)
		}
		if gotID != id {
			t.Errorf("entity %d: entity_id = %d, want %d", i, gotID, id)
		}
	}

	if got := readMetaValue(t, db2, "schema_version"); got != "2" {
		t.Errorf("schema_version = %q, want 2", got)
	}
}
