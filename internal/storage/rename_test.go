package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/world"
)

// seeded is a database with one entity carrying a Position and a Holder, so a
// rename has something to lose.
func seeded(t *testing.T, path string) int64 {
	t.Helper()
	store, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := tx.InsertEntity(ctx, "Thing", 0)
	if err != nil {
		t.Fatalf("InsertEntity: %v", err)
	}
	mustInsert(t, ctx, tx, id, "Position", world.ComponentValues{"x": 42})
	mustInsert(t, ctx, tx, id, "Holder", world.ComponentValues{"owner": id, "hp": 7})
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
	return id
}

// A component rename moves the table instead of dropping it.
func TestMigration_ARenamedComponentKeepsItsTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	id := seeded(t, path)

	next := refSchema()
	next.Components["Placement"] = schema.Component{
		RenamedFrom: "Position",
		Type:        schema.ComponentTypeObject,
		Properties:  map[string]schema.Property{"x": {Type: schema.PropertyTypeInteger}},
	}
	delete(next.Components, "Position")
	et := next.EntityTypes["Thing"]
	et.RequiredComponents = []string{"Placement"}
	next.EntityTypes["Thing"] = et

	migrated, err := NewSQLiteStore(path, next, "")
	if err != nil {
		t.Fatalf("renaming a component: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	var x int
	if err := migrated.DB().QueryRow(
		`SELECT x FROM comp_placement WHERE entity_id = ?`, id).Scan(&x); err != nil {
		t.Fatalf("reading the renamed table: %v", err)
	}
	if x != 42 {
		t.Errorf("x = %d, want 42 — the rename lost the data", x)
	}
	if got := foreignKeys(t, migrated.DB(), "comp_placement"); !contains(got, "entity_id ON DELETE CASCADE") {
		t.Errorf("comp_placement has %v — the rename dropped the foreign key", got)
	}
	var old int
	if err := migrated.DB().QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE name = 'comp_position'`).Scan(&old); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if old != 0 {
		t.Error("the old table is still there")
	}
}

// A property rename moves the column.
func TestMigration_ARenamedPropertyKeepsItsColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	id := seeded(t, path)

	next := refSchema()
	pos := next.Components["Position"]
	pos.Properties = map[string]schema.Property{
		"col_x": {Type: schema.PropertyTypeInteger, RenamedFrom: "x"},
	}
	next.Components["Position"] = pos

	migrated, err := NewSQLiteStore(path, next, "")
	if err != nil {
		t.Fatalf("renaming a property: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	var x int
	if err := migrated.DB().QueryRow(
		`SELECT col_x FROM comp_position WHERE entity_id = ?`, id).Scan(&x); err != nil {
		t.Fatalf("reading the renamed column: %v", err)
	}
	if x != 42 {
		t.Errorf("col_x = %d, want 42 — the rename lost the data", x)
	}
}

// Renaming a property that carries a foreign key keeps the foreign key, on the
// renamed column.
func TestMigration_ARenamedReferencePropertyKeepsItsForeignKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	seeded(t, path)

	next := refSchema()
	holder := next.Components["Holder"]
	holder.Properties = map[string]schema.Property{
		"keeper": {Type: schema.PropertyTypeEntityRef, RenamedFrom: "owner"},
		"hp":     {Type: schema.PropertyTypeInteger},
	}
	next.Components["Holder"] = holder

	migrated, err := NewSQLiteStore(path, next, "")
	if err != nil {
		t.Fatalf("renaming a reference property: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	if got := foreignKeys(t, migrated.DB(), "comp_holder"); !contains(got, "keeper ON DELETE CASCADE") {
		t.Errorf("comp_holder has %v, want the cascade on the renamed column", got)
	}
}

// An entity type rename is rows rather than DDL.
func TestMigration_ARenamedEntityTypeMovesItsEntities(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	id := seeded(t, path)

	next := refSchema()
	next.EntityTypes["Creature"] = schema.EntityType{
		RenamedFrom:        "Thing",
		RequiredComponents: []string{"Position"},
		OptionalComponents: []string{"Carrier", "Holder"},
		ValidationLevel:    "strict",
	}
	delete(next.EntityTypes, "Thing")

	migrated, err := NewSQLiteStore(path, next, "")
	if err != nil {
		t.Fatalf("renaming an entity type: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	var got string
	if err := migrated.DB().QueryRow(
		`SELECT entity_type FROM entities WHERE id = ?`, id).Scan(&got); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got != "Creature" {
		t.Errorf("the entity is still filed as %q", got)
	}
}

// A rename and another change to the same thing, in one migration.
func TestMigration_ARenameAndAnAdditionTogether(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	id := seeded(t, path)

	next := refSchema()
	next.Components["Placement"] = schema.Component{
		RenamedFrom: "Position",
		Type:        schema.ComponentTypeObject,
		Properties: map[string]schema.Property{
			"col_x": {Type: schema.PropertyTypeInteger, RenamedFrom: "x"},
			"y":     {Type: schema.PropertyTypeInteger},
		},
	}
	delete(next.Components, "Position")
	et := next.EntityTypes["Thing"]
	et.RequiredComponents = []string{"Placement"}
	next.EntityTypes["Thing"] = et

	migrated, err := NewSQLiteStore(path, next, "")
	if err != nil {
		t.Fatalf("renaming and adding at once: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	var x, y int
	if err := migrated.DB().QueryRow(
		`SELECT col_x, y FROM comp_placement WHERE entity_id = ?`, id).Scan(&x, &y); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if x != 42 {
		t.Errorf("col_x = %d, want 42", x)
	}
	if y != 0 {
		t.Errorf("y = %d, want the added column's default 0", y)
	}
}

// Running it twice does nothing, so renamedFrom may stay in the file.
func TestMigration_ARenameThatAlreadyRanIsANoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	seeded(t, path)

	next := refSchema()
	pos := next.Components["Position"]
	pos.Properties = map[string]schema.Property{
		"col_x": {Type: schema.PropertyTypeInteger, RenamedFrom: "x"},
	}
	next.Components["Position"] = pos

	first, err := NewSQLiteStore(path, next, "")
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_ = first.Close()

	second, err := NewSQLiteStore(path, next, "")
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer func() { _ = second.Close() }()

	plan, err := NewMigrationRunner(second.DB(), next, MigrationAuto, NopLogger()).Plan()
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	if !plan.Empty() {
		t.Errorf("the second open still plans %d statements: %+v", len(plan.Statements), plan.Statements)
	}
}

// A rename keeps the data, so it is not destructive and MigrationConfirm has no
// reason to stop it.
func TestMigration_ARenameIsNotDestructive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	seeded(t, path)

	next := refSchema()
	pos := next.Components["Position"]
	pos.Properties = map[string]schema.Property{
		"col_x": {Type: schema.PropertyTypeInteger, RenamedFrom: "x"},
	}
	next.Components["Position"] = pos

	store, err := NewSQLiteStoreWithConfig(path, StoreConfig{
		Schema:          next,
		MigrationPolicy: MigrationConfirm,
		Logger:          NopLogger(),
	})
	if err != nil {
		t.Fatalf("a rename was refused as destructive: %v", err)
	}
	_ = store.Close()
}

// ── The safety net ───────────────────────────────────────────────────

// A rename nobody declared cannot be applied, but it can be recognised. This is
// the only warning anybody gets before the column goes.
func TestGenerate_AnUndeclaredRenameSaysSo(t *testing.T) {
	file := &schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"Position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"col_x": {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	domain := &DomainSchema{
		SchemaVersion: 1,
		Components: map[string]DomainComponent{"position": {Columns: []DomainColumn{
			{Name: "entity_id", SQLType: "INTEGER", References: schema.EntityReference, IsPK: true},
			{Name: "x", SQLType: "INTEGER"},
		}}},
		EntityTypeNames: map[string]bool{},
	}

	stmts := NewGenerator(file, domain, Config{StrictDrop: true}).
		Generate(schema.Diff(domain.ToDiffSchema(), file, nil))

	var said string
	for _, s := range stmts {
		said += s.Description + "\n"
	}
	for _, want := range []string{"x", "col_x", "renamedFrom"} {
		if !strings.Contains(said, want) {
			t.Errorf("nothing in the migration mentions %q:\n%s", want, said)
		}
	}
}

// A column that is simply gone says what it takes, without claiming a rename
// that is not there to claim.
func TestGenerate_ADroppedColumnSaysWhatItTakes(t *testing.T) {
	file := &schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"Position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	domain := &DomainSchema{
		SchemaVersion: 1,
		Components: map[string]DomainComponent{"position": {Columns: []DomainColumn{
			{Name: "entity_id", SQLType: "INTEGER", References: schema.EntityReference, IsPK: true},
			{Name: "x", SQLType: "INTEGER"},
			{Name: "gone", SQLType: "TEXT"},
		}}},
		EntityTypeNames: map[string]bool{},
	}

	stmts := NewGenerator(file, domain, Config{StrictDrop: true}).
		Generate(schema.Diff(domain.ToDiffSchema(), file, nil))

	var said string
	for _, s := range stmts {
		said += s.Description + "\n"
	}
	if !strings.Contains(said, "gone") {
		t.Errorf("the migration does not name the column it drops:\n%s", said)
	}
	if strings.Contains(said, "renamedFrom") {
		t.Errorf("a plain drop was reported as a possible rename:\n%s", said)
	}
}

// ── The survivors of the mutation battery ────────────────────────────

// A component rename keeps every row, so there is nothing for MigrationConfirm
// to stop. The property form has its own statement kind and its own chance to be
// marked destructive.
func TestMigration_ARenamedComponentIsNotDestructive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	seeded(t, path)

	next := refSchema()
	next.Components["Placement"] = schema.Component{
		RenamedFrom: "Position",
		Type:        schema.ComponentTypeObject,
		Properties:  map[string]schema.Property{"x": {Type: schema.PropertyTypeInteger}},
	}
	delete(next.Components, "Position")
	et := next.EntityTypes["Thing"]
	et.RequiredComponents = []string{"Placement"}
	next.EntityTypes["Thing"] = et

	store, err := NewSQLiteStoreWithConfig(path, StoreConfig{
		Schema:          next,
		MigrationPolicy: MigrationConfirm,
		Logger:          NopLogger(),
	})
	if err != nil {
		t.Fatalf("a component rename was refused as destructive: %v", err)
	}
	_ = store.Close()
}

// An entity type name is a value in a column, not an identifier, so nothing
// validates it as one and it can hold a quote. Interpolating it unescaped would
// end the string literal early.
func TestMigration_AnEntityTypeNameWithAQuote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")

	before := refSchema()
	before.EntityTypes["Mon'ster"] = schema.EntityType{
		RequiredComponents: []string{"Position"},
		ValidationLevel:    "strict",
	}
	store, err := NewSQLiteStore(path, before, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if _, err := store.DB().Exec(
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, ?, 0)`, "Mon'ster"); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	_ = store.Close()

	after := refSchema()
	after.EntityTypes["Creature's"] = schema.EntityType{
		RenamedFrom:        "Mon'ster",
		RequiredComponents: []string{"Position"},
		ValidationLevel:    "strict",
	}

	migrated, err := NewSQLiteStore(path, after, "")
	if err != nil {
		t.Fatalf("renaming an entity type whose name holds a quote: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	var got string
	if err := migrated.DB().QueryRow(
		`SELECT entity_type FROM entities WHERE id = 1`).Scan(&got); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got != "Creature's" {
		t.Errorf("the entity is filed as %q, want %q", got, "Creature's")
	}
}

// A dropped table says what it takes, and offers the rename it might have been.
func TestGenerate_ADroppedTableSaysWhatItTakes(t *testing.T) {
	file := &schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"Placement": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	domain := &DomainSchema{
		SchemaVersion: 1,
		Components: map[string]DomainComponent{"position": {Columns: []DomainColumn{
			{Name: "entity_id", SQLType: "INTEGER", References: schema.EntityReference, IsPK: true},
			{Name: "x", SQLType: "INTEGER"},
		}}},
		EntityTypeNames: map[string]bool{},
	}

	stmts := NewGenerator(file, domain, Config{StrictDrop: true}).
		Generate(schema.Diff(domain.ToDiffSchema(), file, nil))

	var said string
	for _, s := range stmts {
		said += s.Description + "\n"
	}
	for _, want := range []string{"comp_position", "every row in it", "renamedFrom", "placement"} {
		if !strings.Contains(said, want) {
			t.Errorf("the dropped table does not mention %q:\n%s", want, said)
		}
	}
}

// ── The review's findings ────────────────────────────────────────────

// A rename and a rebuild on the same component, which is where the two halves
// of the migration used to disagree about what the table was called.
//
// The diff renames its own copy of the introspected schema; the generator was
// handed the original. So the rebuild looked for the table under its new name,
// did not find it, and produced an error statement — which the runner returns
// from every subsequent open. The story claimed this case worked, on the
// strength of a test that only covered an added column.
func TestMigration_ARenameAndARebuildTogether(t *testing.T) {
	cases := map[string]func(schema.DatabaseSchema) schema.DatabaseSchema{
		"a property dropped": func(s schema.DatabaseSchema) schema.DatabaseSchema {
			s.Components["Keeper"] = schema.Component{
				RenamedFrom: "Holder",
				Type:        schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"owner": {Type: schema.PropertyTypeEntityRef},
				},
			}
			return s
		},
		"a property retyped": func(s schema.DatabaseSchema) schema.DatabaseSchema {
			s.Components["Keeper"] = schema.Component{
				RenamedFrom: "Holder",
				Type:        schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"owner": {Type: schema.PropertyTypeEntityRef},
					"hp":    {Type: schema.PropertyTypeString},
				},
			}
			return s
		},
		"a constraint changed": func(s schema.DatabaseSchema) schema.DatabaseSchema {
			s.Components["Keeper"] = schema.Component{
				RenamedFrom: "Holder",
				Type:        schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"owner": {Type: schema.PropertyTypeInteger},
					"hp":    {Type: schema.PropertyTypeInteger},
				},
			}
			return s
		},
	}

	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "w.sqlite")
			id := seeded(t, path)

			next := refSchema()
			delete(next.Components, "Holder")
			next = edit(next)
			et := next.EntityTypes["Thing"]
			et.OptionalComponents = []string{"Carrier", "Keeper"}
			next.EntityTypes["Thing"] = et

			migrated, err := NewSQLiteStore(path, next, "")
			if err != nil {
				t.Fatalf("renaming a component that also needs rebuilding: %v", err)
			}
			defer func() { _ = migrated.Close() }()

			var owner int
			if err := migrated.DB().QueryRow(
				`SELECT owner FROM comp_keeper WHERE entity_id = ?`, id).Scan(&owner); err != nil {
				t.Fatalf("reading the renamed table: %v", err)
			}
			if owner != int(id) {
				t.Errorf("owner = %d, want %d — the rebuild lost the data", owner, id)
			}
		})
	}
}

// A property renamed and retyped at once. The rebuild's copy has to substitute a
// value for the NULLs the old column was allowed to hold, and it looked for the
// column under its new name in a snapshot that still had the old one — so it
// emitted no COALESCE and failed on the NULL, reopening the wedge Story 12 shut.
func TestMigration_ARenamedPropertyStillGetsItsSubstitutedValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	wedgeStore(t, path)

	next := refSchema()
	holder := next.Components["Holder"]
	holder.Properties = map[string]schema.Property{
		"keeper": {Type: schema.PropertyTypeInteger, RenamedFrom: "owner"},
		"hp":     {Type: schema.PropertyTypeInteger},
	}
	next.Components["Holder"] = holder

	migrated, err := NewSQLiteStore(path, next, "")
	if err != nil {
		t.Fatalf("renaming and retyping a reference property at once: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	var keeper int
	if err := migrated.DB().QueryRow(
		`SELECT keeper FROM comp_holder WHERE entity_id = 1`).Scan(&keeper); err != nil {
		t.Fatalf("reading the renamed column: %v", err)
	}
	if keeper != 0 {
		t.Errorf("keeper = %d, want the substituted 0", keeper)
	}
	var rows int
	if err := migrated.DB().QueryRow(`SELECT COUNT(*) FROM comp_holder`).Scan(&rows); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 2 {
		t.Errorf("comp_holder has %d rows, want 2", rows)
	}
}

// An entity type rename merges into a name that already has entities. A table
// cannot be renamed onto another table, but an UPDATE moving rows from one type
// string to another is safe whether or not the destination already has some.
func TestMigration_AnEntityTypeRenameMergesIntoAnExistingName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")

	before := refSchema()
	before.EntityTypes["Monster"] = schema.EntityType{
		RequiredComponents: []string{"Position"}, ValidationLevel: "strict",
	}
	before.EntityTypes["Creature"] = schema.EntityType{
		RequiredComponents: []string{"Position"}, ValidationLevel: "strict",
	}
	store, err := NewSQLiteStore(path, before, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	for i, et := range map[int]string{1: "Monster", 2: "Creature"} {
		if _, err := store.DB().Exec(
			`INSERT INTO entities (id, entity_type, created_tick) VALUES (?, ?, 0)`, i, et); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	_ = store.Close()

	after := refSchema()
	after.EntityTypes["Creature"] = schema.EntityType{
		RenamedFrom:        "Monster",
		RequiredComponents: []string{"Position"},
		ValidationLevel:    "strict",
	}

	migrated, err := NewSQLiteStore(path, after, "")
	if err != nil {
		t.Fatalf("merging an entity type rename: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	var stranded int
	if err := migrated.DB().QueryRow(
		`SELECT COUNT(*) FROM entities WHERE entity_type = 'Monster'`).Scan(&stranded); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if stranded != 0 {
		t.Errorf("%d entities are still filed under a type the schema no longer declares", stranded)
	}
	var creatures int
	if err := migrated.DB().QueryRow(
		`SELECT COUNT(*) FROM entities WHERE entity_type = 'Creature'`).Scan(&creatures); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if creatures != 2 {
		t.Errorf("%d entities are Creatures, want 2", creatures)
	}
}

// withRenames returns a new schema and leaves the one it was given alone.
//
// Nothing today reads the original afterwards — Plan replaces it — so this is a
// contract rather than a bug report. It is worth pinning because the method
// reads as pure: without the copy it edits the caller's column slice in place,
// and the next reader of that snapshot sees names for a rename that has not run.
func TestWithRenames_LeavesTheOriginalAlone(t *testing.T) {
	original := &DomainSchema{
		SchemaVersion: 1,
		Components: map[string]DomainComponent{"position": {Columns: []DomainColumn{
			{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
			{Name: "x", SQLType: "INTEGER"},
		}}},
		EntityTypeNames: map[string]bool{"Thing": true},
	}

	renamed := original.withRenames([]schema.Change{
		{Kind: schema.ChangeRenamedComponent, Component: "placement", OldName: "position"},
		{Kind: schema.ChangeRenamedProperty, Component: "placement", Property: "col_x", OldName: "x"},
		{Kind: schema.ChangeRenamedEntityType, ETName: "Creature", OldName: "Thing"},
	})

	// The copy has the new names.
	got, ok := renamed.Components["placement"]
	if !ok {
		t.Fatalf("the renamed schema has no comp_placement: %v", renamed.Components)
	}
	if got.Columns[1].Name != "col_x" {
		t.Errorf("the renamed column is %q, want %q", got.Columns[1].Name, "col_x")
	}
	if !renamed.EntityTypeNames["Creature"] || renamed.EntityTypeNames["Thing"] {
		t.Errorf("the renamed entity types are %v", renamed.EntityTypeNames)
	}

	// The original has the old ones.
	before, ok := original.Components["position"]
	if !ok {
		t.Fatalf("the original lost comp_position: %v", original.Components)
	}
	if before.Columns[1].Name != "x" {
		t.Errorf("the original's column is now %q — withRenames edited it in place", before.Columns[1].Name)
	}
	if !original.EntityTypeNames["Thing"] || original.EntityTypeNames["Creature"] {
		t.Errorf("the original's entity types are now %v", original.EntityTypeNames)
	}
}
