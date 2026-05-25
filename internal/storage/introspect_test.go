package storage

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// ── ListComponentTables tests ───────────────────────────────────

func TestListComponentTables_EmptyDatabaseReturnsEmptySlice(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	tables, err := ListComponentTables(store.db)
	if err != nil {
		t.Fatalf("ListComponentTables error: %v", err)
	}

	if tables == nil {
		t.Fatal("ListComponentTables returned nil, want empty slice")
	}
	if len(tables) != 0 {
		t.Errorf("len(tables) = %d, want 0", len(tables))
	}
}

func TestListComponentTables_MultipleComponentsReturnsSorted(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Create tables in non-alphabetical order.
	for _, name := range []string{"comp_health", "comp_position", "comp_avatar"} {
		if _, err := db.Exec("CREATE TABLE " + name + " (entity_id INTEGER PRIMARY KEY)"); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	tables, err := ListComponentTables(db)
	if err != nil {
		t.Fatalf("ListComponentTables error: %v", err)
	}

	want := []string{"comp_avatar", "comp_health", "comp_position"}
	if len(tables) != len(want) {
		t.Fatalf("len(tables) = %d, want %d", len(tables), len(want))
	}
	for i, w := range want {
		if tables[i] != w {
			t.Errorf("tables[%d] = %q, want %q", i, tables[i], w)
		}
	}
}

func TestListComponentTables_IgnoresNonComponentTables(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, name := range []string{"foo", "data", "events", "comp_player", "comp_npc"} {
		if _, err := db.Exec("CREATE TABLE " + name + " (id INTEGER PRIMARY KEY)"); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	tables, err := ListComponentTables(db)
	if err != nil {
		t.Fatalf("ListComponentTables error: %v", err)
	}

	if len(tables) != 2 {
		t.Fatalf("len(tables) = %d, want 2", len(tables))
	}
	want := []string{"comp_npc", "comp_player"}
	for i, w := range want {
		if tables[i] != w {
			t.Errorf("tables[%d] = %q, want %q", i, tables[i], w)
		}
	}
}

// ── ReadSchemaVersion tests ─────────────────────────────────────

func TestReadSchemaVersion_ReturnsStoredVersion(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 5,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	v, err := ReadSchemaVersion(store.db)
	if err != nil {
		t.Fatalf("ReadSchemaVersion error: %v", err)
	}
	if v != 5 {
		t.Errorf("version = %d, want 5", v)
	}
}

func TestReadSchemaVersion_MetaMissingReturnsError(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = ReadSchemaVersion(db)
	if err == nil {
		t.Fatal("expected error for missing meta table, got nil")
	}
}

func TestReadSchemaVersion_KeyMissingReturnsError(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)")
	if err != nil {
		t.Fatalf("creating meta: %v", err)
	}

	_, err = ReadSchemaVersion(db)
	if err == nil {
		t.Fatal("expected error for missing schema_version key, got nil")
	}
}

func TestReadSchemaVersion_CorruptedValueReturnsError(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Overwrite with garbage.
	_, err = store.db.Exec("UPDATE meta SET value = 'not-a-number' WHERE key = 'schema_version'")
	if err != nil {
		t.Fatalf("overwriting meta: %v", err)
	}

	_, err = ReadSchemaVersion(store.db)
	if err == nil {
		t.Fatal("expected error for corrupted value, got nil")
	}
}

// ── IntrospectComponentTable tests ──────────────────────────────

func TestIntrospectComponentTable_ObjectComponent(t *testing.T) {
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
		},
		EntityTypes: map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	columns, err := IntrospectComponentTable(store.db, "comp_position")
	if err != nil {
		t.Fatalf("IntrospectComponentTable error: %v", err)
	}

	if len(columns) != 3 {
		t.Fatalf("len(columns) = %d, want 3", len(columns))
	}

	checkCol := func(idx int, name, sqlType string, isPK bool) {
		t.Helper()
		c := columns[idx]
		if c.Name != name {
			t.Errorf("columns[%d].Name = %q, want %q", idx, c.Name, name)
		}
		if c.SQLType != sqlType {
			t.Errorf("columns[%d].SQLType = %q, want %q", idx, c.SQLType, sqlType)
		}
		if c.IsPK != isPK {
			t.Errorf("columns[%d].IsPK = %v, want %v", idx, c.IsPK, isPK)
		}
	}

	checkCol(0, "entity_id", "INTEGER", true)
	checkCol(1, "x", "REAL", false)
	checkCol(2, "y", "REAL", false)
}

func TestIntrospectComponentTable_EntityRefComponent(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Target": {Type: schema.ComponentTypeEntityRef},
		},
		EntityTypes: map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	columns, err := IntrospectComponentTable(store.db, "comp_target")
	if err != nil {
		t.Fatalf("IntrospectComponentTable error: %v", err)
	}

	if len(columns) != 2 {
		t.Fatalf("len(columns) = %d, want 2", len(columns))
	}
	if columns[0].Name != "entity_id" || !columns[0].IsPK {
		t.Errorf("first column should be entity_id PK")
	}
	if columns[1].Name != "target_entity_id" || columns[1].SQLType != "INTEGER" {
		t.Errorf("second column: %s %s, want target_entity_id INTEGER", columns[1].Name, columns[1].SQLType)
	}
}

func TestIntrospectComponentTable_ScalarTypes(t *testing.T) {
	tests := []struct {
		name      string
		component schema.Component
		wantCol   string // data column name
		wantType  string // expected SQL type
		wantDflt  string // expected default value
	}{
		{
			name:      "string",
			component: schema.Component{Type: "string"},
			wantCol:   "value",
			wantType:  "TEXT",
			wantDflt:  "''",
		},
		{
			name:      "integer",
			component: schema.Component{Type: "integer"},
			wantCol:   "value",
			wantType:  "INTEGER",
			wantDflt:  "0",
		},
		{
			name:      "number",
			component: schema.Component{Type: "number"},
			wantCol:   "value",
			wantType:  "REAL",
			wantDflt:  "0.0",
		},
		{
			name:      "boolean",
			component: schema.Component{Type: "boolean"},
			wantCol:   "value",
			wantType:  "BOOLEAN",
			wantDflt:  "0",
		},
		{
			name:      "array",
			component: schema.Component{Type: "array"},
			wantCol:   "value",
			wantType:  "TEXT",
			wantDflt:  "'[]'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
				SchemaVersion: 1,
				Components:    map[string]schema.Component{tt.name: tt.component},
				EntityTypes:   map[string]schema.EntityType{},
			}, "")
			if err != nil {
				t.Fatalf("NewSQLiteStore error: %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })

			columns, err := IntrospectComponentTable(store.db, "comp_"+tt.name)
			if err != nil {
				t.Fatalf("IntrospectComponentTable error: %v", err)
			}

			if len(columns) != 2 {
				t.Fatalf("len(columns) = %d, want 2", len(columns))
			}
			if columns[0].Name != "entity_id" || !columns[0].IsPK {
				t.Errorf("first column should be entity_id PK, got %s pk=%v", columns[0].Name, columns[0].IsPK)
			}

			col := columns[1]
			if col.Name != tt.wantCol {
				t.Errorf("col.Name = %q, want %q", col.Name, tt.wantCol)
			}
			if col.SQLType != tt.wantType {
				t.Errorf("col.SQLType = %q, want %q", col.SQLType, tt.wantType)
			}
			if col.Default != tt.wantDflt {
				t.Errorf("col.Default = %q, want %q", col.Default, tt.wantDflt)
			}
		})
	}
}

func TestIntrospectComponentTable_EmptyObjectComponent(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Marker": {
				Type:       "object",
				Properties: map[string]schema.Property{},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	columns, err := IntrospectComponentTable(store.db, "comp_marker")
	if err != nil {
		t.Fatalf("IntrospectComponentTable error: %v", err)
	}

	if len(columns) != 1 {
		t.Fatalf("len(columns) = %d, want 1", len(columns))
	}
	if columns[0].Name != "entity_id" || !columns[0].IsPK || columns[0].SQLType != "INTEGER" {
		t.Errorf("expected entity_id INTEGER PK, got %+v", columns[0])
	}
}

func TestIntrospectComponentTable_NonExistentTableReturnsEmpty(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	columns, err := IntrospectComponentTable(store.db, "comp_doesnotexist")
	if err != nil {
		t.Fatalf("IntrospectComponentTable error: %v", err)
	}

	if len(columns) != 0 {
		t.Errorf("expected empty columns for non-existent table, got %d columns", len(columns))
	}
}

// ── InferComponentType tests ────────────────────────────────────

func TestInferComponentType(t *testing.T) {
	tests := []struct {
		name string
		cols []DomainColumn
		want string
	}{
		{
			name: "empty_object",
			cols: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}},
			want: "object",
		},
		{
			name: "object_with_properties",
			cols: []DomainColumn{
				{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
				{Name: "x", SQLType: "REAL"},
				{Name: "y", SQLType: "REAL"},
			},
			want: "object",
		},
		{
			name: "string",
			cols: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "TEXT"}},
			want: "string",
		},
		{
			name: "integer",
			cols: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "INTEGER"}},
			want: "integer",
		},
		{
			name: "number",
			cols: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "REAL"}},
			want: "number",
		},
		{
			name: "boolean",
			cols: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "BOOLEAN"}},
			want: "boolean",
		},
		{
			name: "array",
			cols: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "TEXT", Default: "[]"}},
			want: "array",
		},
		{
			name: "entity_ref",
			cols: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "target_entity_id", SQLType: "INTEGER"}},
			want: "entity-ref",
		},
		{
			name: "scalar_with_entity_id_not_first",
			cols: []DomainColumn{{Name: "value", SQLType: "TEXT", IsPK: false}},
			want: "string", // no entity_id at all → no stripping → still matches
		},
		{
			name: "zero_columns",
			cols: []DomainColumn{},
			want: "object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferComponentType(tt.cols)
			if got != tt.want {
				t.Errorf("InferComponentType(%s) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// ── IntrospectAll tests ─────────────────────────────────────────

func TestIntrospectAll_EmptySchema(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 7,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ds, err := IntrospectAll(store.db)
	if err != nil {
		t.Fatalf("IntrospectAll error: %v", err)
	}

	if ds == nil {
		t.Fatal("IntrospectAll returned nil")
	}
	if ds.SchemaVersion != 7 {
		t.Errorf("SchemaVersion = %d, want 7", ds.SchemaVersion)
	}
	if ds.Components == nil {
		t.Fatal("Components map is nil, want empty map")
	}
	if len(ds.Components) != 0 {
		t.Errorf("len(Components) = %d, want 0", len(ds.Components))
	}
	if ds.EntityTypeNames == nil {
		t.Fatal("EntityTypeNames map is nil")
	}
	if len(ds.EntityTypeNames) != 0 {
		t.Errorf("len(EntityTypeNames) = %d, want 0", len(ds.EntityTypeNames))
	}
}

func TestIntrospectAll_FullSchema(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 3,
		Components: map[string]schema.Component{
			"Position": {
				Type: "object",
				Properties: map[string]schema.Property{
					"x": {Type: "number"},
					"y": {Type: "number"},
				},
			},
			"Health": {Type: "integer"},
			"Target": {Type: "entity-ref"},
			"Inventory": {
				Type:  "array",
				Items: &schema.Property{Type: "entity-ref"},
			},
		},
		EntityTypes: map[string]schema.EntityType{
			"Player": {RequiredComponents: []string{"Position", "Health"}},
		},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ds, err := IntrospectAll(store.db)
	if err != nil {
		t.Fatalf("IntrospectAll error: %v", err)
	}

	if ds.SchemaVersion != 3 {
		t.Errorf("SchemaVersion = %d, want 3", ds.SchemaVersion)
	}

	wantComponents := []string{"position", "health", "target", "inventory"}
	if len(ds.Components) != len(wantComponents) {
		t.Fatalf("len(Components) = %d, want %d", len(ds.Components), len(wantComponents))
	}
	for _, name := range wantComponents {
		if _, ok := ds.Components[name]; !ok {
			t.Errorf("Components missing %q", name)
		}
	}

	// Verify none of the fixed tables leaked in.
	fixedTables := []string{"meta", "world", "entities", "event_queue", "input_events", "transitions"}
	for _, name := range fixedTables {
		if _, ok := ds.Components[name]; ok {
			t.Errorf("fixed table %q should not appear in Components", name)
		}
	}

	// Verify specific shapes.
	pos := ds.Components["position"]
	if pos.Type != "object" {
		t.Errorf("position.Type = %q, want object", pos.Type)
	}
	if len(pos.Columns) != 3 {
		t.Errorf("position column count = %d, want 3", len(pos.Columns))
	}

	health := ds.Components["health"]
	if health.Type != "integer" {
		t.Errorf("health.Type = %q, want integer", health.Type)
	}
	if len(health.Columns) != 2 {
		t.Errorf("health column count = %d, want 2", len(health.Columns))
	}
}

func TestIntrospectAll_WithEntityTypes(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes: map[string]schema.EntityType{
			"Player": {RequiredComponents: []string{}},
			"Enemy":  {RequiredComponents: []string{}},
		},
	}, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Insert entities of different types.
	_, err = store.db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Player', 0)")
	if err != nil {
		t.Fatalf("inserting entity: %v", err)
	}
	_, err = store.db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Player', 1)")
	if err != nil {
		t.Fatalf("inserting entity: %v", err)
	}
	_, err = store.db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Enemy', 2)")
	if err != nil {
		t.Fatalf("inserting entity: %v", err)
	}

	ds, err := IntrospectAll(store.db)
	if err != nil {
		t.Fatalf("IntrospectAll error: %v", err)
	}

	if !ds.EntityTypeNames["Player"] {
		t.Error("EntityTypeNames missing 'Player'")
	}
	if !ds.EntityTypeNames["Enemy"] {
		t.Error("EntityTypeNames missing 'Enemy'")
	}
	// Duplicates should not cause extra keys.
	if len(ds.EntityTypeNames) != 2 {
		t.Errorf("len(EntityTypeNames) = %d, want 2", len(ds.EntityTypeNames))
	}
}

func TestIntrospectAll_MetaMissingReturnsError(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// No meta table exists.
	_, err = IntrospectAll(db)
	if err == nil {
		t.Fatal("expected error for missing meta, got nil")
	}
}

// ── IntBool tests ───────────────────────────────────────────────

func TestIntBool_Scan(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    IntBool
		wantErr bool
	}{
		{name: "nil → 0", input: nil, want: 0},
		{name: "int64 one → 1", input: int64(1), want: 1},
		{name: "int64 zero → 0", input: int64(0), want: 0},
		{name: "float64 one → 1", input: float64(1), want: 1},
		{name: "float64 zero → 0", input: float64(0), want: 0},
		{name: "[]byte non-zero → 1", input: []byte("1"), want: 1},
		{name: "[]byte zero → 0", input: []byte("0"), want: 0},
		{name: "[]byte empty → 0", input: []byte{}, want: 0},
		{name: "string non-zero → 1", input: "1", want: 1},
		{name: "string zero → 0", input: "0", want: 0},
		{name: "unknown type → 0", input: true, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b IntBool
			err := b.Scan(tt.input)
			if err != nil && !tt.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if b != tt.want {
				t.Errorf("IntBool = %d, want %d", b, tt.want)
			}
		})
	}
}

// ── Round-trip integration tests ────────────────────────────────

func TestIntrospectAll_RoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		components map[string]schema.Component
		wantTypes  map[string]string // component name → expected inferred type
	}{
		{
			name:       "empty schema",
			components: map[string]schema.Component{},
			wantTypes:  map[string]string{},
		},
		{
			name: "single object property",
			components: map[string]schema.Component{
				"Position": {
					Type: "object",
					Properties: map[string]schema.Property{
						"x": {Type: "number"},
					},
				},
			},
			wantTypes: map[string]string{"position": "object"},
		},
		{
			name: "three mixed properties",
			components: map[string]schema.Component{
				"Stats": {
					Type: "object",
					Properties: map[string]schema.Property{
						"hp":     {Type: "integer"},
						"name":   {Type: "string"},
						"active": {Type: "boolean"},
					},
				},
			},
			wantTypes: map[string]string{"stats": "object"},
		},
		{
			name: "all scalar types",
			components: map[string]schema.Component{
				"Name":      {Type: "string"},
				"Count":     {Type: "integer"},
				"Weight":    {Type: "number"},
				"Active":    {Type: "boolean"},
				"Target":    {Type: "entity-ref"},
				"Inventory": {Type: "array", Items: &schema.Property{Type: "entity-ref"}},
			},
			wantTypes: map[string]string{
				"name":      "string",
				"count":     "integer",
				"weight":    "number",
				"active":    "boolean",
				"target":    "entity-ref",
				"inventory": "array",
			},
		},
		{
			name: "empty object component",
			components: map[string]schema.Component{
				"Marker": {Type: "object", Properties: map[string]schema.Property{}},
			},
			wantTypes: map[string]string{"marker": "object"},
		},
		{
			name: "multi-component schema",
			components: map[string]schema.Component{
				"Position": {
					Type: "object",
					Properties: map[string]schema.Property{
						"x": {Type: "number"},
						"y": {Type: "number"},
					},
				},
				"Health": {Type: "integer"},
				"Sprite": {Type: "string"},
			},
			wantTypes: map[string]string{
				"position": "object",
				"health":   "integer",
				"sprite":   "string",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewSQLiteStore(t.TempDir()+"/test.sqlite", schema.DatabaseSchema{
				SchemaVersion: 1,
				Components:    tt.components,
				EntityTypes:   map[string]schema.EntityType{},
			}, "")
			if err != nil {
				t.Fatalf("NewSQLiteStore error: %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })

			ds, err := IntrospectAll(store.db)
			if err != nil {
				t.Fatalf("IntrospectAll error: %v", err)
			}

			if len(ds.Components) != len(tt.wantTypes) {
				t.Fatalf("len(Components) = %d, want %d", len(ds.Components), len(tt.wantTypes))
			}

			for compName, wantType := range tt.wantTypes {
				comp, ok := ds.Components[compName]
				if !ok {
					t.Errorf("Components missing %q", compName)
					continue
				}
				if comp.Type != wantType {
					t.Errorf("Components[%q].Type = %q, want %q", compName, comp.Type, wantType)
				}

				// Verify column counts by counting properties in the original schema.
				origFound := false
				for origName, compDef := range tt.components {
					if strings.ToLower(origName) == compName {
						wantColCount := 1 // entity_id always
						if compDef.Type == "object" {
							wantColCount += len(compDef.Properties)
						} else {
							// All non-object types add exactly 1 data column.
							wantColCount++
						}
						if len(comp.Columns) != wantColCount {
							t.Errorf("Components[%q].Columns len = %d, want %d", compName, len(comp.Columns), wantColCount)
						}
						origFound = true
						break
					}
				}
				if !origFound {
					t.Errorf("no matching component definition found for %q", compName)
				}

				// entity_id must always be first and PK.
				if comp.Columns[0].Name != "entity_id" || !comp.Columns[0].IsPK {
					t.Errorf("Components[%q] first column should be entity_id PK, got %+v", compName, comp.Columns[0])
				}
			}
		})
	}
}
