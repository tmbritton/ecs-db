package storage

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// ── componentTableSQL tests ───────────────────────────────────

func TestComponentTableSQL_Object(t *testing.T) {
	comp := schema.Component{
		Type: schema.ComponentTypeObject,
		Properties: map[string]schema.Property{
			"x": {Type: schema.PropertyTypeNumber},
			"y": {Type: schema.PropertyTypeNumber},
		},
	}
	sql, err := componentTableSQL("Position", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, sql, "CREATE TABLE IF NOT EXISTS comp_position")
	assertContains(t, sql, "x REAL NOT NULL")
	assertContains(t, sql, "y REAL NOT NULL")
	assertContains(t, sql, "entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE")
}

func TestComponentTableSQL_ObjectWithMixedProperties(t *testing.T) {
	comp := schema.Component{
		Type: schema.ComponentTypeObject,
		Properties: map[string]schema.Property{
			"imageId": {Type: schema.PropertyTypeString},
			"frame":   {Type: schema.PropertyTypeInteger},
		},
	}
	sql, err := componentTableSQL("Sprite", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, sql, "imageid TEXT NOT NULL")
	assertContains(t, sql, "frame INTEGER NOT NULL")
}

func TestComponentTableSQL_EntityRef(t *testing.T) {
	comp := schema.Component{Type: schema.ComponentTypeEntityRef}
	sql, err := componentTableSQL("Wielder", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, sql, "target_entity_id INTEGER NOT NULL REFERENCES entities(id)")
}

func TestComponentTableSQL_Array(t *testing.T) {
	comp := schema.Component{
		Type:  schema.ComponentTypeArray,
		Items: &schema.Property{Type: schema.PropertyTypeEntityRef},
	}
	sql, err := componentTableSQL("Inventory", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, sql, "value TEXT NOT NULL DEFAULT '[]'")
}

func TestComponentTableSQL_String(t *testing.T) {
	comp := schema.Component{Type: schema.ComponentTypeString}
	sql, err := componentTableSQL("Name", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, sql, "value TEXT NOT NULL DEFAULT ''")
}

func TestComponentTableSQL_Integer(t *testing.T) {
	comp := schema.Component{Type: schema.ComponentTypeInteger}
	sql, err := componentTableSQL("Count", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, sql, "value INTEGER NOT NULL DEFAULT 0")
}

func TestComponentTableSQL_Number(t *testing.T) {
	comp := schema.Component{Type: schema.ComponentTypeNumber}
	sql, err := componentTableSQL("Weight", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, sql, "value REAL NOT NULL DEFAULT 0.0")
}

func TestComponentTableSQL_Boolean(t *testing.T) {
	comp := schema.Component{Type: schema.ComponentTypeBoolean}
	sql, err := componentTableSQL("Active", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, sql, "value BOOLEAN NOT NULL DEFAULT 0")
}

func TestComponentTableSQL_LowercaseTableName(t *testing.T) {
	comp := schema.Component{Type: schema.ComponentTypeString}
	sql, err := componentTableSQL("HealthBar", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, sql, "comp_healthbar")
}

// ── propertySQLType tests ─────────────────────────────────────

func TestPropertySQLType(t *testing.T) {
	tests := []struct {
		name  string
		prop  schema.Property
		want  string
	}{
		{"string", schema.Property{Type: schema.PropertyTypeString}, "TEXT"},
		{"integer", schema.Property{Type: schema.PropertyTypeInteger}, "INTEGER"},
		{"number", schema.Property{Type: schema.PropertyTypeNumber}, "REAL"},
		{"boolean", schema.Property{Type: schema.PropertyTypeBoolean}, "INTEGER"},
		{"entity-ref", schema.Property{Type: schema.PropertyTypeEntityRef}, "INTEGER"},
		{"object as JSON", schema.Property{Type: schema.PropertyTypeObject}, "TEXT"},
		{"array as JSON", schema.Property{Type: schema.PropertyTypeArray}, "TEXT"},
		{"unknown falls back to TEXT", schema.Property{Type: "weird"}, "TEXT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := propertySQLType(tt.prop)
			if got != tt.want {
				t.Errorf("propertySQLType(%s) = %q, want %q", tt.prop.Type, got, tt.want)
			}
		})
	}
}

// ── NewSQLiteStore integration tests ──────────────────────────

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestNewSQLiteStore_CreatesFixedTables(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	defer store.Close()

	// Verify all fixed tables exist
	tables := []string{"meta", "world", "entities", "event_queue", "input_events", "transitions"}
	for _, name := range tables {
		var count int
		err := store.db.QueryRow(
			"SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?",
			name,
		).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %q not found (count=%d)", name, count)
		}
	}
}

func TestNewSQLiteStore_RecordsSchemaVersion(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 3,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	defer store.Close()

	var value string
	err = store.db.QueryRow(
		"SELECT value FROM meta WHERE key = 'schema_version'",
	).Scan(&value)
	if err != nil {
		t.Fatalf("reading schema_version: %v", err)
	}
	if value != "3" {
		t.Errorf("schema_version = %q, want %q", value, "3")
	}
}

func TestNewSQLiteStore_CreatesComponentTables(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
					"y": {Type: schema.PropertyTypeNumber},
				},
			},
			"Name": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{
			"Player": {RequiredComponents: []string{"Position", "Name"}},
		},
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	defer store.Close()

	for _, name := range []string{"comp_position", "comp_name"} {
		var count int
		err := store.db.QueryRow(
			"SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?",
			name,
		).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("component table %q not found", name)
		}
	}
}

func TestNewSQLiteStore_EntityRefComponentCreatesFK(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Target": {Type: schema.ComponentTypeEntityRef},
		},
		EntityTypes: map[string]schema.EntityType{
			"Aim": {RequiredComponents: []string{"Target"}},
		},
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	defer store.Close()

	// Check the column exists
	rows, err := store.db.Query("PRAGMA table_info(comp_target)")
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk, dfltVal interface{}
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltVal, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "target_entity_id" {
			found = true
		}
	}
	if !found {
		t.Error("comp_target table missing target_entity_id column")
	}
}

func TestNewSQLiteStore_CascadeDelete(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Health": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"hp": {Type: schema.PropertyTypeInteger},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{
			"Goblin": {RequiredComponents: []string{"Health"}},
		},
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	defer store.Close()

	// Insert entity + component
	res, err := store.db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Goblin', 1)")
	if err != nil {
		t.Fatal(err)
	}
	entityID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (?, 50)", entityID)
	if err != nil {
		t.Fatal(err)
	}

	// Delete entity — should cascade to component
	_, err = store.db.Exec("DELETE FROM entities WHERE id = ?", entityID)
	if err != nil {
		t.Fatalf("cascade delete failed: %v", err)
	}

	var count int
	err = store.db.QueryRow("SELECT count(*) FROM comp_health").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows after cascade, got %d", count)
	}
}

func TestNewSQLiteStore_MissingDirectory(t *testing.T) {
	_, err := NewSQLiteStore("/no/such/dir/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
	})
	if err == nil {
		t.Fatal("expected error for missing directory, got nil")
	}
}

func TestStore_Close_NilDB(t *testing.T) {
	store := &SQLiteStore{db: nil}
	if err := store.Close(); err != nil {
		t.Errorf("Close() on nil db returned error: %v", err)
	}
}

func TestStore_DB(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if store.DB() == nil {
		t.Error("DB() returned nil")
	}
}

func TestStore_DB_AfterClose(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	// Should still return a non-nil pointer even after close
	if store.DB() == nil {
		t.Error("DB() returned nil after Close")
	}
}

func TestMigrateComponent_AllValidTypes(t *testing.T) {
	comps := map[string]schema.Component{
		"object":     {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{"x": {Type: schema.PropertyTypeNumber}}},
		"entity-ref": {Type: schema.ComponentTypeEntityRef},
		"array":      {Type: schema.ComponentTypeArray, Items: &schema.Property{Type: schema.PropertyTypeEntityRef}},
		"string":     {Type: schema.ComponentTypeString},
		"integer":    {Type: schema.ComponentTypeInteger},
		"number":     {Type: schema.ComponentTypeNumber},
		"boolean":    {Type: schema.ComponentTypeBoolean},
	}
	for name, comp := range comps {
		t.Run(name, func(t *testing.T) {
			_, err := MigrateComponent("Test", comp)
			if err != nil {
				t.Errorf("MigrateComponent(%q) unexpected error: %v", name, err)
			}
		})
	}
}

func TestComponentTableSQL_UnknownComponentType(t *testing.T) {
	comp := schema.Component{Type: "bogus"}
	_, err := componentTableSQL("Bad", comp)
	if err == nil {
		t.Error("expected error for unknown component type, got nil")
	}
}

// ── Helper ────────────────────────────────────────────────────

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("SQL does not contain %q:\n%s", substr, s)
	}
}
