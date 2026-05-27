package storage

import (
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
