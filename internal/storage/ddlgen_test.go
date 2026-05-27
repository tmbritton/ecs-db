package storage

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// ── Helper ────────────────────────────────────────────────────

func assertContainsDDL(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("SQL does not contain %q:\n%s", substr, s)
	}
}

func assertNotContainsDDL(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("SQL unexpectedly contains %q:\n%s", substr, s)
	}
}

// ── NewGenerator tests ──────────────────────────────────────────────

func TestNewGenerator_NilFilePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewGenerator(nil, nil, Config{}) did not panic")
		}
	}()
	NewGenerator(nil, nil, Config{})
}

func TestNewGenerator_Valid(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: make(map[string]schema.Component),
	}
	g := NewGenerator(file, nil, Config{})
	if g == nil {
		t.Fatal("NewGenerator returned nil")
	}
}

func TestNewGenerator_DefaultConfig(t *testing.T) {
	file := &schema.DatabaseSchema{}
	g := NewGenerator(file, nil, Config{})
	if g.config.StrictDrop != false {
		t.Errorf("default StrictDrop = %v, want false", g.config.StrictDrop)
	}
}

// ── Generate: empty / skipped changes ───────────────────────────────

func TestGenerate_EmptyDiff(t *testing.T) {
	file := &schema.DatabaseSchema{Components: make(map[string]schema.Component)}
	g := NewGenerator(file, nil, Config{})
	stmts := g.Generate(nil)
	if len(stmts) != 0 {
		t.Errorf("len(stmts) = %d, want 0", len(stmts))
	}
}

func TestGenerate_EmptyDiffSlice(t *testing.T) {
	file := &schema.DatabaseSchema{Components: make(map[string]schema.Component)}
	g := NewGenerator(file, nil, Config{})
	stmts := g.Generate([]schema.Change{})
	if len(stmts) != 0 {
		t.Errorf("len(stmts) = %d, want 0", len(stmts))
	}
}

func TestGenerate_EntityTypeChangesSkipped(t *testing.T) {
	file := &schema.DatabaseSchema{Components: make(map[string]schema.Component)}
	g := NewGenerator(file, nil, Config{})

	changes := []schema.Change{
		{Kind: schema.ChangeAddedEntityType, ETName: "Player"},
		{Kind: schema.ChangeRemovedEntityType, ETName: "NPC"},
		{Kind: schema.ChangeChangedEntityType, ETName: "Enemy"},
	}

	stmts := g.Generate(changes)
	if len(stmts) != 0 {
		t.Errorf("entity type changes produced %d statements, want 0", len(stmts))
	}
}

func TestGenerate_UnknownChangeKindSkipped(t *testing.T) {
	file := &schema.DatabaseSchema{Components: make(map[string]schema.Component)}
	g := NewGenerator(file, nil, Config{})

	changes := []schema.Change{{Kind: schema.ChangeKind("bogus")}}
	stmts := g.Generate(changes)
	if len(stmts) != 0 {
		t.Errorf("unknown change kind produced %d statements, want 0", len(stmts))
	}
}

// ── genAddComponent tests ───────────────────────────────────────────

func TestGenAddComponent_Object(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
					"y": {Type: schema.PropertyTypeNumber},
				},
			},
		},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedComponent,
		Component: "position",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	s := stmts[0]
	assertContainsDDL(t, s.SQL, "CREATE TABLE IF NOT EXISTS comp_position")
	assertContainsDDL(t, s.SQL, "x REAL NOT NULL")
	assertContainsDDL(t, s.SQL, "y REAL NOT NULL")
	assertContainsDDL(t, s.SQL, "entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE")
	if s.Kind != "create_table" {
		t.Errorf("Kind = %q, want %q", s.Kind, "create_table")
	}
	if s.Destructive {
		t.Error("Destructive = true, want false")
	}
	if s.Component != "position" {
		t.Errorf("Component = %q, want %q", s.Component, "position")
	}
}

func TestGenAddComponent_Scalar(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Health": {Type: schema.ComponentTypeInteger},
		},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedComponent,
		Component: "health",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "CREATE TABLE IF NOT EXISTS comp_health")
	assertContainsDDL(t, stmts[0].SQL, "value INTEGER NOT NULL DEFAULT 0")
}

func TestGenAddComponent_EntityRef(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Target": {Type: schema.ComponentTypeEntityRef},
		},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedComponent,
		Component: "target",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "target_entity_id INTEGER NOT NULL REFERENCES entities(id)")
}

func TestGenAddComponent_Array(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Inventory": {
				Type:  schema.ComponentTypeArray,
				Items: &schema.Property{Type: schema.PropertyTypeEntityRef},
			},
		},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedComponent,
		Component: "inventory",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "value TEXT NOT NULL DEFAULT '[]'")
}

func TestGenAddComponent_Boolean(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Active": {Type: schema.ComponentTypeBoolean},
		},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedComponent,
		Component: "active",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "CREATE TABLE IF NOT EXISTS comp_active")
	assertContainsDDL(t, stmts[0].SQL, "value INTEGER NOT NULL DEFAULT 0")
}

func TestGenAddComponent_UnknownComponent(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedComponent,
		Component: "missing",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	if !strings.Contains(stmts[0].Description, "unknown component") {
		t.Errorf("Description = %q, want 'unknown component'", stmts[0].Description)
	}
}

func TestGenAddComponent_UnsupportedType(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Bad": {Type: "bogus-type"},
		},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedComponent,
		Component: "bad",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	if !strings.Contains(stmts[0].Description, "ERROR:") {
		t.Errorf("Description = %q, want 'ERROR:'", stmts[0].Description)
	}
}

// ── genAddProperty tests ────────────────────────────────────────────

func TestGenAddProperty_String(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Sprite": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"imageId": {Type: schema.PropertyTypeString},
				},
			},
		},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedProperty,
		Component: "sprite",
		Property:  "imageid",
		NewType:   "TEXT",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "ALTER TABLE comp_sprite ADD COLUMN imageid TEXT NOT NULL DEFAULT ''")
	if stmts[0].Kind != "alter_add_column" {
		t.Errorf("Kind = %q, want %q", stmts[0].Kind, "alter_add_column")
	}
	if stmts[0].Destructive {
		t.Error("Destructive = true, want false")
	}
}

func TestGenAddProperty_Number(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"z": {Type: schema.PropertyTypeNumber},
				},
			},
		},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedProperty,
		Component: "position",
		Property:  "z",
		NewType:   "REAL",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "ALTER TABLE comp_position ADD COLUMN z REAL NOT NULL DEFAULT 0.0")
}

func TestGenAddProperty_Integer(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Flags": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"count": {Type: schema.PropertyTypeInteger},
				},
			},
		},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedProperty,
		Component: "flags",
		Property:  "count",
		NewType:   "INTEGER",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "ALTER TABLE comp_flags ADD COLUMN count INTEGER NOT NULL DEFAULT 0")
}

func TestGenAddProperty_Boolean(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Flags": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"active": {Type: schema.PropertyTypeBoolean},
				},
			},
		},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedProperty,
		Component: "flags",
		Property:  "active",
		NewType:   "INTEGER",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "ALTER TABLE comp_flags ADD COLUMN active INTEGER NOT NULL DEFAULT 0")
}

func TestGenAddProperty_EntityRef(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Rel": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"parent": {Type: schema.PropertyTypeEntityRef},
				},
			},
		},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedProperty,
		Component: "rel",
		Property:  "parent",
		NewType:   "INTEGER",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "ALTER TABLE comp_rel ADD COLUMN parent INTEGER NOT NULL DEFAULT NULL")
	assertContainsDDL(t, stmts[0].SQL, "REFERENCES entities(id)")
}

func TestGenAddProperty_UnknownPropertySkips(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Test": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
				},
			},
		},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedProperty,
		Component: "test",
		Property:  "missing",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	if !strings.Contains(stmts[0].Description, "unknown property") {
		t.Errorf("Description = %q, want 'unknown property'", stmts[0].Description)
	}
}

func TestGenAddProperty_NonObjectComponentReturnsError(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Health": {Type: schema.ComponentTypeInteger},
		},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedProperty,
		Component: "health",
		Property:  "bonus",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	if !strings.Contains(stmts[0].Description, "unknown property or non-object") {
		t.Errorf("Description = %q, want 'unknown property or non-object'", stmts[0].Description)
	}
}

func TestGenAddProperty_UnknownComponentReturnsError(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{},
	}
	g := NewGenerator(file, nil, Config{})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeAddedProperty,
		Component: "missing",
		Property:  "x",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	if !strings.Contains(stmts[0].Description, "unknown property or non-object") {
		t.Errorf("Description = %q, want 'unknown property or non-object'", stmts[0].Description)
	}
}

// ── genRemoveComponent tests ────────────────────────────────────────

func TestGenRemoveComponent_Simple(t *testing.T) {
	file := &schema.DatabaseSchema{Components: make(map[string]schema.Component)}
	g := NewGenerator(file, nil, Config{StrictDrop: true})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeRemovedComponent,
		Component: "legacy",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	s := stmts[0]
	if s.SQL != "DROP TABLE IF EXISTS comp_legacy" {
		t.Errorf("SQL = %q, want %q", s.SQL, "DROP TABLE IF EXISTS comp_legacy")
	}
	if s.Kind != "drop_table" {
		t.Errorf("Kind = %q, want %q", s.Kind, "drop_table")
	}
	if !s.Destructive {
		t.Error("Destructive = false, want true")
	}
	if s.Component != "legacy" {
		t.Errorf("Component = %q, want %q", s.Component, "legacy")
	}
}

// ── genRemoveProperty (table rebuild) tests ─────────────────────────

func TestGenRemoveProperty_RebuildSequence(t *testing.T) {
	// File: Position has x and y. DB has x, y, and z — z is being removed.
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
					"y": {Type: schema.PropertyTypeNumber},
				},
			},
		},
	}
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"position": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "x", SQLType: "REAL"},
					{Name: "y", SQLType: "REAL"},
					{Name: "z", SQLType: "REAL"},
				},
			},
		},
	}
	g := NewGenerator(file, domain, Config{StrictDrop: true})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeRemovedProperty,
		Component: "position",
		Property:  "z",
	}})

	// PRAGMAs are no longer in the generator output — they are handled by
	// MigrationRunner outside the transaction (SQLite ignores them inside one).
	if len(stmts) != 4 {
		t.Fatalf("got %d statements, want 4 (CREATE, INSERT, DROP, RENAME)", len(stmts))
	}

	// Statement 0: CREATE TABLE comp_position_new
	assertContainsDDL(t, stmts[0].SQL, "CREATE TABLE")
	assertContainsDDL(t, stmts[0].SQL, "comp_position_new")
	// Should have x and y but NOT z.
	assertContainsDDL(t, stmts[0].SQL, "x")
	assertContainsDDL(t, stmts[0].SQL, "y")
	assertNotContainsDDL(t, stmts[0].SQL, "z")

	// Statement 1: INSERT preserves entity_id and all retained columns.
	assertContainsDDL(t, stmts[1].SQL, "INSERT INTO comp_position_new")
	assertContainsDDL(t, stmts[1].SQL, "entity_id")
	assertContainsDDL(t, stmts[1].SQL, "x")
	assertContainsDDL(t, stmts[1].SQL, "y")
	// z was removed — must not appear in SELECT.
	assertNotContainsDDL(t, stmts[1].SQL, "z")
	assertContainsDDL(t, stmts[1].SQL, "FROM comp_position")

	// Statement 2: DROP TABLE comp_position
	assertContainsDDL(t, stmts[2].SQL, "DROP TABLE comp_position")

	// Statement 3: RENAME
	assertContainsDDL(t, stmts[3].SQL, "ALTER TABLE comp_position_new RENAME TO comp_position")

	// All statements should be destructive.
	for i, s := range stmts {
		if !s.Destructive {
			t.Errorf("stmt %d Destructive = false, want true", i)
		}
	}
}

func TestGenRemoveProperty_MissingDomainReturnsError(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Test": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
				},
			},
		},
	}
	// Domain is nil — cannot rebuild without knowing current columns.
	g := NewGenerator(file, nil, Config{StrictDrop: true})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeRemovedProperty,
		Component: "test",
		Property:  "y",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	if !strings.Contains(stmts[0].Description, "cannot rebuild") {
		t.Errorf("Description = %q, want 'cannot rebuild'", stmts[0].Description)
	}
	if !stmts[0].Destructive {
		t.Error("Destructive = false, want true")
	}
}

func TestGenRemoveProperty_MissingFileComponentReturnsError(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{},
	}
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"test": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "x", SQLType: "REAL"},
					{Name: "y", SQLType: "REAL"},
				},
			},
		},
	}
	g := NewGenerator(file, domain, Config{StrictDrop: true})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeRemovedProperty,
		Component: "test",
		Property:  "y",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	if !strings.Contains(stmts[0].Description, "unknown component") {
		t.Errorf("Description = %q, want 'unknown component'", stmts[0].Description)
	}
}

// ── genChangePropertyType (table rebuild) tests ─────────────────────

func TestGenChangePropertyType_RebuildSequence(t *testing.T) {
	// File: Position.x changed from TEXT to REAL.
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
					"y": {Type: schema.PropertyTypeNumber},
				},
			},
		},
	}
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"position": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "x", SQLType: "TEXT"},
					{Name: "y", SQLType: "REAL"},
				},
			},
		},
	}
	g := NewGenerator(file, domain, Config{StrictDrop: true})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangedPropertyType,
		Component: "position",
		Property:  "x",
		OldType:   "TEXT",
		NewType:   "REAL",
	}})

	if len(stmts) != 4 {
		t.Fatalf("got %d statements, want 4 (CREATE, INSERT, DROP, RENAME)", len(stmts))
	}
	// The new table should have x as REAL, not TEXT.
	assertContainsDDL(t, stmts[0].SQL, "x REAL NOT NULL")
	assertContainsDDL(t, stmts[0].SQL, "y REAL NOT NULL")
	// INSERT must include entity_id.
	assertContainsDDL(t, stmts[1].SQL, "entity_id")
	// All statements should be destructive.
	for i, s := range stmts {
		if !s.Destructive {
			t.Errorf("stmt %d Destructive = false, want true", i)
		}
	}
}

func TestGenChangePropertyType_MissingDomainReturnsError(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Test": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
				},
			},
		},
	}
	g := NewGenerator(file, nil, Config{StrictDrop: true})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangedPropertyType,
		Component: "test",
		Property:  "x",
		OldType:   "TEXT",
		NewType:   "REAL",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	if !strings.Contains(stmts[0].Description, "cannot rebuild") {
		t.Errorf("Description = %q, want 'cannot rebuild'", stmts[0].Description)
	}
}

// ── Structural change reorder tests ─────────────────────────────────

func TestGenerate_StructuralChangeDropBeforeCreate(t *testing.T) {
	// File: "data" changed from scalar to object.
	// Diff might emit add before remove (phase-based ordering);
	// the generator must ensure DROP comes before CREATE.
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Data": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"key": {Type: schema.PropertyTypeString},
				},
			},
		},
	}
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"data": {
				Type:    "integer",
				Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "INTEGER"}},
			},
		},
	}
	g := NewGenerator(file, domain, Config{StrictDrop: true})

	changes := []schema.Change{
		{Kind: schema.ChangeAddedComponent, Component: "data"},
		{Kind: schema.ChangeRemovedComponent, Component: "data"},
	}

	stmts := g.Generate(changes)
	if len(stmts) != 2 {
		t.Fatalf("got %d statements, want 2", len(stmts))
	}
	if stmts[0].SQL != "DROP TABLE IF EXISTS comp_data" {
		t.Errorf("stmts[0].SQL = %q, want DROP TABLE IF EXISTS comp_data", stmts[0].SQL)
	}
	assertContainsDDL(t, stmts[1].SQL, "CREATE TABLE")
}

func TestGenerate_StructuralChangeAlreadyOrdered(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Data": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"key": {Type: schema.PropertyTypeString},
				},
			},
		},
	}
	g := NewGenerator(file, nil, Config{StrictDrop: true})

	changes := []schema.Change{
		{Kind: schema.ChangeRemovedComponent, Component: "data"},
		{Kind: schema.ChangeAddedComponent, Component: "data"},
	}

	stmts := g.Generate(changes)
	if len(stmts) != 2 {
		t.Fatalf("got %d statements, want 2", len(stmts))
	}
	if stmts[0].SQL != "DROP TABLE IF EXISTS comp_data" {
		t.Errorf("stmts[0].SQL = %q, want DROP TABLE first", stmts[0].SQL)
	}
}

// ── Destructive flag tests ──────────────────────────────────────────

func TestGenerate_DestructiveFlags(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
				},
			},
		},
	}
	g := NewGenerator(file, nil, Config{StrictDrop: true})

	changes := []schema.Change{
		{Kind: schema.ChangeAddedComponent, Component: "position"},
		{Kind: schema.ChangeRemovedComponent, Component: "legacy"},
	}

	stmts := g.Generate(changes)
	if len(stmts) != 2 {
		t.Fatalf("got %d statements, want 2", len(stmts))
	}
	if stmts[0].Destructive {
		t.Error("add_component should not be destructive")
	}
	if !stmts[1].Destructive {
		t.Error("removed_component should be destructive")
	}
}

func TestGenerate_NonDestructiveClear(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"NewComp": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"a": {Type: schema.PropertyTypeString},
				},
			},
		},
	}
	g := NewGenerator(file, nil, Config{})

	changes := []schema.Change{
		{Kind: schema.ChangeAddedComponent, Component: "newcomp"},
		{Kind: schema.ChangeAddedProperty, Component: "newcomp", Property: "b", NewType: "TEXT"},
	}

	stmts := g.Generate(changes)
	for _, s := range stmts {
		if s.Destructive {
			t.Errorf("%s should not be destructive, Kind=%q", s.Component, s.Kind)
		}
	}
}

// ── StrictDrop filter tests ─────────────────────────────────────────

func TestGenerate_StrictDropFalse_FiltersDestructive(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"New": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"v": {Type: schema.PropertyTypeString},
				},
			},
		},
	}
	g := NewGenerator(file, nil, Config{StrictDrop: false})

	changes := []schema.Change{
		{Kind: schema.ChangeAddedComponent, Component: "new"},
		{Kind: schema.ChangeRemovedComponent, Component: "old"},
	}

	stmts := g.Generate(changes)
	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "CREATE TABLE")
}

func TestGenerate_StrictDropTrue_IncludesDestructive(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{},
	}
	g := NewGenerator(file, nil, Config{StrictDrop: true})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeRemovedComponent,
		Component: "old",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "DROP TABLE")
}

func TestGenerate_StrictDropFalse_FiltersRebuild(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Data": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
				},
			},
		},
	}
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"data": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "x", SQLType: "REAL"},
					{Name: "y", SQLType: "REAL"},
				},
			},
		},
	}
	g := NewGenerator(file, domain, Config{StrictDrop: false})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeRemovedProperty,
		Component: "data",
		Property:  "y",
	}})

	if len(stmts) != 0 {
		t.Errorf("expected 0 statements with StrictDrop=false, got %d", len(stmts))
	}
}

// ── Rebuild scalar component type tests ─────────────────────────────

func TestGenChangePropertyType_ScalarRebuild_EntityRef(t *testing.T) {
	// Rebuild a scalar entity-ref component to a different entity-ref.
	// This exercises the EntityRef branch of buildNewColumns.
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Target": {Type: schema.ComponentTypeEntityRef},
		},
	}
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"target": {
				Type:    "integer",
				Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "INTEGER"}},
			},
		},
	}
	g := NewGenerator(file, domain, Config{StrictDrop: true})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangedPropertyType,
		Component: "target",
		Property:  "value",
		OldType:   "INTEGER",
		NewType:   "INTEGER",
	}})

	// Even if types match, changed_type change produces a rebuild.
	if len(stmts) != 4 {
		t.Fatalf("got %d statements, want 4", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "target_entity_id")
}

func TestGenChangePropertyType_ScalarRebuild_String(t *testing.T) {
	// Exercises the String branch of buildNewColumns.
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Name": {Type: schema.ComponentTypeString},
		},
	}
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"name": {
				Type:    "string",
				Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "TEXT"}},
			},
		},
	}
	g := NewGenerator(file, domain, Config{StrictDrop: true})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangedPropertyType,
		Component: "name",
		Property:  "value",
		OldType:   "TEXT",
		NewType:   "TEXT",
	}})

	if len(stmts) != 4 {
		t.Fatalf("got %d statements, want 4", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "value TEXT NOT NULL DEFAULT ''")
}

func TestGenChangePropertyType_ScalarRebuild_Array(t *testing.T) {
	// Exercises the Array branch of buildNewColumns.
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Tags": {Type: schema.ComponentTypeArray},
		},
	}
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"tags": {
				Type:    "array",
				Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "TEXT"}},
			},
		},
	}
	g := NewGenerator(file, domain, Config{StrictDrop: true})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangedPropertyType,
		Component: "tags",
		Property:  "value",
		OldType:   "TEXT",
		NewType:   "TEXT",
	}})

	if len(stmts) != 4 {
		t.Fatalf("got %d statements, want 4", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "value TEXT NOT NULL DEFAULT '[]'")
}

func TestGenChangePropertyType_ScalarRebuild_Number(t *testing.T) {
	// Exercises the Number branch of buildNewColumns.
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Temp": {Type: schema.ComponentTypeNumber},
		},
	}
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"temp": {
				Type:    "integer",
				Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "INTEGER"}},
			},
		},
	}
	g := NewGenerator(file, domain, Config{StrictDrop: true})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangedPropertyType,
		Component: "temp",
		Property:  "value",
		OldType:   "INTEGER",
		NewType:   "REAL",
	}})

	if len(stmts) != 4 {
		t.Fatalf("got %d statements, want 4", len(stmts))
	}
	assertContainsDDL(t, stmts[0].SQL, "value REAL NOT NULL DEFAULT 0.0")
}

// ── Mixed scenario test ─────────────────────────────────────────────

func TestGenerate_MixedChanges(t *testing.T) {
	file := &schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
					"y": {Type: schema.PropertyTypeNumber},
					"z": {Type: schema.PropertyTypeNumber},
				},
			},
			"Health": {Type: schema.ComponentTypeInteger},
		},
	}
	domain := &DomainSchema{
		SchemaVersion: 1,
		Components: map[string]DomainComponent{
			"position": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "x", SQLType: "REAL"},
					{Name: "y", SQLType: "REAL"},
				},
			},
			"velocity": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "dx", SQLType: "REAL"},
					{Name: "dy", SQLType: "REAL"},
				},
			},
		},
	}
	g := NewGenerator(file, domain, Config{StrictDrop: true})

	changes := []schema.Change{
		{Kind: schema.ChangeAddedComponent, Component: "health"},
		{Kind: schema.ChangeAddedProperty, Component: "position", Property: "z", NewType: "REAL"},
		{Kind: schema.ChangeRemovedComponent, Component: "velocity"},
	}

	stmts := g.Generate(changes)
	if len(stmts) < 3 {
		t.Fatalf("expected at least 3 statements, got %d", len(stmts))
	}

	// Verify non-destructive and destructive statements exist.
	var added, altered, dropped bool
	for _, s := range stmts {
		if s.Kind == "create_table" && s.Component == "health" {
			added = true
		}
		if s.Kind == "alter_add_column" && s.Component == "position" {
			altered = true
		}
		if s.Kind == "drop_table" && s.Component == "velocity" {
			dropped = true
		}
	}
	if !added {
		t.Error("expected CREATE TABLE for health")
	}
	if !altered {
		t.Error("expected ALTER TABLE for position")
	}
	if !dropped {
		t.Error("expected DROP TABLE for velocity")
	}
}

// ── defaultValueForProperty tests ───────────────────────────────────

func TestDefaultValueForProperty_String(t *testing.T) {
	d := defaultValueForProperty(schema.Property{Type: schema.PropertyTypeString})
	if d != "''" {
		t.Errorf("string default = %q, want ''''", d)
	}
}

func TestDefaultValueForProperty_Integer(t *testing.T) {
	d := defaultValueForProperty(schema.Property{Type: schema.PropertyTypeInteger})
	if d != "0" {
		t.Errorf("integer default = %q, want 0", d)
	}
}

func TestDefaultValueForProperty_Number(t *testing.T) {
	d := defaultValueForProperty(schema.Property{Type: schema.PropertyTypeNumber})
	if d != "0.0" {
		t.Errorf("number default = %q, want 0.0", d)
	}
}

func TestDefaultValueForProperty_Boolean(t *testing.T) {
	d := defaultValueForProperty(schema.Property{Type: schema.PropertyTypeBoolean})
	if d != "0" {
		t.Errorf("boolean default = %q, want 0", d)
	}
}

func TestDefaultValueForProperty_EntityRef(t *testing.T) {
	d := defaultValueForProperty(schema.Property{Type: schema.PropertyTypeEntityRef})
	if d != "NULL" {
		t.Errorf("entity-ref default = %q, want NULL", d)
	}
}

func TestDefaultValueForProperty_ObjectArray(t *testing.T) {
	d := defaultValueForProperty(schema.Property{Type: schema.PropertyTypeObject})
	if d != "'{}'" {
		t.Errorf("object default = %q, want '{}''", d)
	}
	d = defaultValueForProperty(schema.Property{Type: schema.PropertyTypeArray})
	if d != "'{}'" {
		t.Errorf("array default = %q, want '{}''", d)
	}
}

func TestDefaultValueForProperty_Unknown(t *testing.T) {
	d := defaultValueForProperty(schema.Property{Type: "bogus"})
	if d != "NULL" {
		t.Errorf("unknown default = %q, want NULL", d)
	}
}

// ── buildCreateTable tests ───────────────────────────────────────────

func TestBuildCreateTable_NoIfExists(t *testing.T) {
	// Rebuild temp tables must not use IF NOT EXISTS; if the temp
	// table somehow exists we want the command to fail loudly rather
	// than silently producing an empty table and corrupting data.
	cols := []string{
		"entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE",
		"x REAL NOT NULL",
	}
	sql := buildCreateTable("comp_position_new", "position", cols)
	assertNotContainsDDL(t, sql, "IF NOT EXISTS")
	assertContainsDDL(t, sql, "CREATE TABLE comp_position_new")
	assertContainsDDL(t, sql, "entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE")
	assertContainsDDL(t, sql, "x REAL NOT NULL")
}

// ── buildNewColumns: unknown component type ───────────────────────────

func TestBuildNewColumns_UnknownComponentType(t *testing.T) {
	comp := schema.Component{
		Type: "bogus",
	}
	cols := buildNewColumns(comp)
	// Only the PK column should exist; no data column for unknown type.
	if len(cols) != 1 {
		t.Errorf("got %d columns, want 1 (entity_id only)", len(cols))
	}
	assertContainsDDL(t, cols[0], "entity_id")
	assertContainsDDL(t, cols[0], "REFERENCES entities(id)")
}

func TestBuildNewColumns_Integer(t *testing.T) {
	comp := schema.Component{Type: schema.ComponentTypeInteger}
	cols := buildNewColumns(comp)
	if len(cols) != 2 {
		t.Fatalf("got %d columns, want 2", len(cols))
	}
	assertContainsDDL(t, cols[1], "value INTEGER NOT NULL DEFAULT 0")
}

func TestBuildNewColumns_Boolean(t *testing.T) {
	comp := schema.Component{Type: schema.ComponentTypeBoolean}
	cols := buildNewColumns(comp)
	if len(cols) != 2 {
		t.Fatalf("got %d columns, want 2", len(cols))
	}
	assertContainsDDL(t, cols[1], "value INTEGER NOT NULL DEFAULT 0")
}

func TestBuildNewColumns_ObjectPropertyNamesLowerCase(t *testing.T) {
	// buildNewColumns must lowercase property names to match
	// componentTableSQL's behavior; otherwise rebuilds produce
	// inconsistent column casing.
	comp := schema.Component{
		Type: schema.ComponentTypeObject,
		Properties: map[string]schema.Property{
			"ImageId": {Type: schema.PropertyTypeString},
			"ScaleX":  {Type: schema.PropertyTypeNumber},
		},
	}
	cols := buildNewColumns(comp)
	// Should have entity_id + 2 property columns = 3 total.
	if len(cols) != 3 {
		t.Fatalf("got %d columns, want 3", len(cols))
	}
	// Columns must be lowercase even though file schema uses mixed case.
	// After alphabetical sort: imageid at [1], scalex at [2].
	assertContainsDDL(t, cols[1], "imageid TEXT NOT NULL")
	assertContainsDDL(t, cols[2], "scalex REAL NOT NULL")
}

// ── genRebuild: component missing from domain map ─────────────────────

func TestGenRebuild_ComponentMissingFromDomainMap(t *testing.T) {
	// g.domain is non-nil but the specific component is absent from
	// its Components map — a different path than g.domain == nil.
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Test": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
				},
			},
		},
	}
	domain := &DomainSchema{
		Components: map[string]DomainComponent{}, // "test" not present
	}
	g := NewGenerator(file, domain, Config{StrictDrop: true})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangeRemovedProperty,
		Component: "test",
		Property:  "y",
	}})

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1", len(stmts))
	}
	if !strings.Contains(stmts[0].Description, "not found in domain schema") {
		t.Errorf("Description = %q, want 'not found in domain schema'", stmts[0].Description)
	}
}
