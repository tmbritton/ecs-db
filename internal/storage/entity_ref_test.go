package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/world"
)

// refSchema has entity-ref in both of the shapes the schema allows: a component
// whose type is entity-ref, and a property of an object component. They are
// meant to mean the same thing and used not to.
func refSchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeInteger},
			}},
			"Carrier": {Type: schema.ComponentTypeEntityRef},
			"Holder": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"owner": {Type: schema.PropertyTypeEntityRef},
				"hp":    {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{
			"Thing": {
				RequiredComponents:   []string{"Position"},
				OptionalComponents:   []string{"Carrier", "Holder"},
				ValidationLevel:      "strict",
				AllowExtraComponents: false,
			},
		},
	}
}

// foreignKeys reports every foreign key on a table as "column ON DELETE action",
// sorted, so a test can state the constraint rather than the DDL text.
func foreignKeys(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf(
		`SELECT "from", on_delete FROM pragma_foreign_key_list(%q)`, table))
	if err != nil {
		t.Fatalf("reading foreign keys of %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var from, onDelete string
		if err := rows.Scan(&from, &onDelete); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		out = append(out, from+" ON DELETE "+onDelete)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	sort.Strings(out)
	return out
}

func refStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "w.sqlite"), refSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// Every foreign key the generator emits cascades. entity_id always did; the two
// entity-ref forms are this story.
func TestCreateTable_EveryEntityReferenceCascades(t *testing.T) {
	store := refStore(t)

	for table, want := range map[string][]string{
		"comp_carrier": {"entity_id ON DELETE CASCADE", "target_entity_id ON DELETE CASCADE"},
		"comp_holder":  {"entity_id ON DELETE CASCADE", "owner ON DELETE CASCADE"},
	} {
		got := foreignKeys(t, store.DB(), table)
		if len(got) != len(want) {
			t.Errorf("%s has %v, want %v", table, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s has %v, want %v", table, got, want)
				break
			}
		}
	}
}

// The decision, in the behaviour it produces: the target goes, the pointing
// component goes with it, and the entity that held the pointer is still there.
func TestDeleteEntity_TakesThePointingComponentAndLeavesItsHolder(t *testing.T) {
	store := refStore(t)
	ctx := context.Background()

	var target, holder int64
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	target, err = tx.InsertEntity(ctx, "Thing", 0)
	if err != nil {
		t.Fatalf("InsertEntity: %v", err)
	}
	mustInsert(t, ctx, tx, target, "Position", world.ComponentValues{"x": 1})
	holder, err = tx.InsertEntity(ctx, "Thing", 0)
	if err != nil {
		t.Fatalf("InsertEntity: %v", err)
	}
	mustInsert(t, ctx, tx, holder, "Position", world.ComponentValues{"x": 2})
	mustInsert(t, ctx, tx, holder, "Carrier", world.ComponentValues{"target_entity_id": target})
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	tx2, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := tx2.DeleteEntity(ctx, target); err != nil {
		t.Fatalf("deleting an entity another entity points at: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var carriers int
	if err := store.DB().QueryRow(
		`SELECT COUNT(*) FROM comp_carrier WHERE entity_id = ?`, holder).Scan(&carriers); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if carriers != 0 {
		t.Error("the component pointing at the deleted entity is still there")
	}

	var alive int
	if err := store.DB().QueryRow(
		`SELECT COUNT(*) FROM entities WHERE id = ?`, holder).Scan(&alive); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if alive != 1 {
		t.Error("the entity that held the pointer went with it")
	}
	var pos int
	if err := store.DB().QueryRow(
		`SELECT COUNT(*) FROM comp_position WHERE entity_id = ?`, holder).Scan(&pos); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if pos != 1 {
		t.Error("the holder lost a component that had nothing to do with the reference")
	}
}

// The same for the property form, including the part of the decision worth
// being explicit about: the whole component goes, not just the column, so a
// Holder loses its hp along with its owner.
func TestDeleteEntity_APropertyReferenceTakesItsWholeComponent(t *testing.T) {
	store := refStore(t)
	ctx := context.Background()

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	target, err := tx.InsertEntity(ctx, "Thing", 0)
	if err != nil {
		t.Fatalf("InsertEntity: %v", err)
	}
	mustInsert(t, ctx, tx, target, "Position", world.ComponentValues{"x": 1})
	holder, err := tx.InsertEntity(ctx, "Thing", 0)
	if err != nil {
		t.Fatalf("InsertEntity: %v", err)
	}
	mustInsert(t, ctx, tx, holder, "Position", world.ComponentValues{"x": 2})
	mustInsert(t, ctx, tx, holder, "Holder", world.ComponentValues{"owner": target, "hp": 10})
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	tx2, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := tx2.DeleteEntity(ctx, target); err != nil {
		t.Fatalf("deleting the target of an entity-ref property: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var holders int
	if err := store.DB().QueryRow(
		`SELECT COUNT(*) FROM comp_holder WHERE entity_id = ?`, holder).Scan(&holders); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if holders != 0 {
		t.Error("the component holding the reference survived its target")
	}
	var alive int
	if err := store.DB().QueryRow(
		`SELECT COUNT(*) FROM entities WHERE id = ?`, holder).Scan(&alive); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if alive != 1 {
		t.Error("the entity that held the component went with it")
	}
}

// The gap this file used to pin — an existing database keeping a constraint
// only the generator had changed — is closed by Story 11, whose tests are in
// reference_test.go. TestMigration_DoesNotNoticeAConstraintThatOnlyTheGenerator
// Changed lived here and said to delete it when this became true.

// A table that arrives by migration has to carry what a created one carries.
// These are the paths where the constraint used to go missing: a rebuild
// dropped the foreign key an ALTER had added, and neither cascaded.
func TestMigration_AnAddedEntityRefPropertyCascades(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")

	// Holder exists already and has no reference in it, so adding one is an
	// ALTER TABLE ADD COLUMN on a table that is already there — the path that
	// used to be the only one emitting a foreign key at all, and that emitted
	// it without a cascade.
	before := refSchema()
	before.Components["Holder"] = schema.Component{
		Type:       schema.ComponentTypeObject,
		Properties: map[string]schema.Property{"hp": {Type: schema.PropertyTypeInteger}},
	}
	store, err := NewSQLiteStore(path, before, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if fks := foreignKeys(t, store.DB(), "comp_holder"); contains(fks, "owner ON DELETE CASCADE") {
		t.Fatalf("comp_holder already has the column this test is about to add: %v", fks)
	}
	_ = store.Close()

	after := refSchema()
	after.SchemaVersion = 2
	migrated, err := NewSQLiteStore(path, after, "")
	if err != nil {
		t.Fatalf("migrating: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	// The column really arrived by ALTER TABLE ADD COLUMN rather than by a
	// rebuild, and a column count would not say so — a rebuild produces three
	// columns too. ADD COLUMN is the only path that has to give the column a
	// DEFAULT, so a recorded default is what tells them apart.
	var dflt sql.NullString
	if err := migrated.DB().QueryRow(
		`SELECT dflt_value FROM pragma_table_info('comp_holder') WHERE name = 'owner'`).Scan(&dflt); err != nil {
		t.Fatalf("reading the column: %v", err)
	}
	if !dflt.Valid {
		t.Fatal("owner has no default, so it did not arrive through ALTER TABLE ADD COLUMN")
	}

	got := foreignKeys(t, migrated.DB(), "comp_holder")
	if !contains(got, "owner ON DELETE CASCADE") {
		t.Errorf("comp_holder has %v, want it to include the cascade", got)
	}
}

func TestMigration_ARebuiltTableKeepsTheCascade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")

	before := refSchema()
	store, err := NewSQLiteStore(path, before, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	// A row, so the rebuild has something to carry across. A drop-and-recreate
	// would produce the same column count and the same constraint, and only
	// this tells the two apart.
	if _, err := store.DB().Exec(
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`); err != nil {
		t.Fatalf("seeding an entity: %v", err)
	}
	if _, err := store.DB().Exec(
		`INSERT INTO comp_holder (entity_id, owner, hp) VALUES (1, 1, 9)`); err != nil {
		t.Fatalf("seeding a row: %v", err)
	}
	_ = store.Close()

	// Removing a property from an object component is what forces a rebuild:
	// SQLite cannot drop a column in place in the general case, so the
	// generator emits create-copy-drop-rename.
	after := refSchema()
	after.SchemaVersion = 2
	holder := after.Components["Holder"]
	holder.Properties = map[string]schema.Property{"owner": {Type: schema.PropertyTypeEntityRef}}
	after.Components["Holder"] = holder

	migrated, err := NewSQLiteStore(path, after, "")
	if err != nil {
		t.Fatalf("migrating: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	// The rebuild really happened.
	var cols int
	if err := migrated.DB().QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('comp_holder')`).Scan(&cols); err != nil {
		t.Fatalf("counting columns: %v", err)
	}
	if cols != 2 {
		t.Fatalf("comp_holder has %d columns, want 2 — the rebuild did not run", cols)
	}
	var owner int64
	if err := migrated.DB().QueryRow(
		`SELECT owner FROM comp_holder WHERE entity_id = 1`).Scan(&owner); err != nil {
		t.Fatalf("the row did not survive the rebuild: %v", err)
	}
	if owner != 1 {
		t.Errorf("owner = %d after the rebuild, want 1", owner)
	}

	got := foreignKeys(t, migrated.DB(), "comp_holder")
	if !contains(got, "owner ON DELETE CASCADE") {
		t.Errorf("comp_holder has %v — the rebuild dropped the cascade", got)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// The sequence that used to leave a database nobody could open again.
//
// Add an entity-ref property to a component that already has rows: ALTER TABLE
// cannot backfill, so the column is nullable and the existing row's value is
// NULL. Then change any *other* property of that component: the rebuild
// declared the entity-ref column NOT NULL, so its INSERT...SELECT hit a NOT
// NULL constraint on the row the ALTER had left. The migration rolls back
// cleanly — and NewSQLiteStore returns the error, so every subsequent open
// failed the same way, with nothing able to repair it: introspection does not
// read notnull, so the diff could never see that the column was nullable.
//
// The three paths agreeing is what makes this work, which is why it is a test
// and not a comment.
func TestMigration_AnAddedReferenceSurvivesALaterRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")

	v1 := refSchema()
	v1.Components["Holder"] = schema.Component{
		Type:       schema.ComponentTypeObject,
		Properties: map[string]schema.Property{"hp": {Type: schema.PropertyTypeInteger}},
	}
	store, err := NewSQLiteStore(path, v1, "")
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	if _, err := store.DB().Exec(
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`); err != nil {
		t.Fatalf("seeding an entity: %v", err)
	}
	if _, err := store.DB().Exec(
		`INSERT INTO comp_holder (entity_id, hp) VALUES (1, 7)`); err != nil {
		t.Fatalf("seeding a row: %v", err)
	}
	_ = store.Close()

	// v2 adds owner. The existing row has no owner and cannot be given one.
	v2 := refSchema()
	v2.SchemaVersion = 2
	store2, err := NewSQLiteStore(path, v2, "")
	if err != nil {
		t.Fatalf("v2 (adding the reference): %v", err)
	}
	_ = store2.Close()

	// v3 removes hp, which forces a rebuild of the table holding that NULL.
	v3 := refSchema()
	v3.SchemaVersion = 3
	v3.Components["Holder"] = schema.Component{
		Type:       schema.ComponentTypeObject,
		Properties: map[string]schema.Property{"owner": {Type: schema.PropertyTypeEntityRef}},
	}
	store3, err := NewSQLiteStore(path, v3, "")
	if err != nil {
		t.Fatalf("v3 (the rebuild): %v", err)
	}
	defer func() { _ = store3.Close() }()

	// The row survived, and the constraint is still what it should be.
	var rows int
	if err := store3.DB().QueryRow(`SELECT COUNT(*) FROM comp_holder`).Scan(&rows); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 1 {
		t.Errorf("%d rows after the rebuild, want the one that was there", rows)
	}
	if got := foreignKeys(t, store3.DB(), "comp_holder"); !contains(got, "owner ON DELETE CASCADE") {
		t.Errorf("comp_holder has %v, want the cascade", got)
	}

	// And the database opens again, which is the part that used to be false.
	_ = store3.Close()
	again, err := NewSQLiteStore(path, v3, "")
	if err != nil {
		t.Fatalf("reopening after the rebuild: %v", err)
	}
	_ = again.Close()
}

// The three paths declare the same nullability, which is what the test above
// rests on. Asserted directly, because a foreign-key list does not show it and
// every other test here reads only the foreign keys.
func TestEntityRefColumns_AgreeAboutNullabilityWhicheverWayTheyArrive(t *testing.T) {
	notNull := func(t *testing.T, db *sql.DB, table, column string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(fmt.Sprintf(
			`SELECT "notnull" FROM pragma_table_info(%q) WHERE name = %q`, table, column)).Scan(&n); err != nil {
			t.Fatalf("reading %s.%s: %v", table, column, err)
		}
		return n
	}

	// Created.
	created := refStore(t)
	if got := notNull(t, created.DB(), "comp_holder", "owner"); got != 0 {
		t.Errorf("created: owner notnull = %d, want 0", got)
	}
	// A component that *is* a reference is the other way, and deliberately:
	// the row is nothing else.
	if got := notNull(t, created.DB(), "comp_carrier", "target_entity_id"); got != 1 {
		t.Errorf("created: target_entity_id notnull = %d, want 1", got)
	}

	// Added by ALTER, and then rebuilt.
	path := filepath.Join(t.TempDir(), "w.sqlite")
	v1 := refSchema()
	v1.Components["Holder"] = schema.Component{
		Type:       schema.ComponentTypeObject,
		Properties: map[string]schema.Property{"hp": {Type: schema.PropertyTypeInteger}},
	}
	s1, err := NewSQLiteStore(path, v1, "")
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	_ = s1.Close()

	v2 := refSchema()
	v2.SchemaVersion = 2
	s2, err := NewSQLiteStore(path, v2, "")
	if err != nil {
		t.Fatalf("v2: %v", err)
	}
	if got := notNull(t, s2.DB(), "comp_holder", "owner"); got != 0 {
		t.Errorf("altered: owner notnull = %d, want 0", got)
	}
	_ = s2.Close()

	v3 := refSchema()
	v3.SchemaVersion = 3
	v3.Components["Holder"] = schema.Component{
		Type:       schema.ComponentTypeObject,
		Properties: map[string]schema.Property{"owner": {Type: schema.PropertyTypeEntityRef}},
	}
	s3, err := NewSQLiteStore(path, v3, "")
	if err != nil {
		t.Fatalf("v3: %v", err)
	}
	defer func() { _ = s3.Close() }()
	if got := notNull(t, s3.DB(), "comp_holder", "owner"); got != 0 {
		t.Errorf("rebuilt: owner notnull = %d, want 0", got)
	}
}

// The rebuild's entity-ref-component branch, at the generator rather than
// through a migration.
//
// It is reachable — schema.Diff reports a retyped *scalar* component as a
// changed property named "value", and a changed property is what sends the
// generator to genRebuild, so turning a string component into an entity-ref
// lands here. It is not reachable end to end: the rebuild's INSERT...SELECT
// then fails because the old table has no target_entity_id to copy from, which
// is a column-rename problem this story does not touch. So the CREATE the
// branch emits is asserted where it is produced.
func TestGenRebuild_AnEntityRefComponentIsRebuiltWithTheCascade(t *testing.T) {
	file := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Carrier": {Type: schema.ComponentTypeEntityRef},
		},
	}
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"carrier": {
				Type: "string",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "value", SQLType: "TEXT"},
				},
			},
		},
	}
	g := NewGenerator(file, domain, Config{StrictDrop: true})

	stmts := g.Generate([]schema.Change{{
		Kind:      schema.ChangedPropertyType,
		Component: "carrier",
		Property:  "value",
		OldType:   "TEXT",
		NewType:   "INTEGER",
	}})
	if len(stmts) == 0 {
		t.Fatal("a retyped scalar component produced no statements")
	}
	create := stmts[0].SQL
	// The target column specifically. entity_id is in the same statement and
	// has always cascaded, so asserting the clause appears anywhere in the SQL
	// passes whatever target_entity_id says.
	if !strings.Contains(create, "target_entity_id INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE") {
		t.Errorf("the rebuilt table's own reference does not cascade:\n%s", create)
	}
}

// A database built before entity references cascaded still refuses the delete,
// and SQLite's own words for that are "FOREIGN KEY constraint failed (787)" —
// which sends someone to look at their schema, where the cascade is plainly
// declared, rather than at their database, where it is not.
func TestDeleteEntity_SaysWhyALegacyDatabaseRefuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	store, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// The old shape, which is what this version would never build.
	for _, stmt := range []string{
		`DROP TABLE comp_carrier`,
		`CREATE TABLE comp_carrier (
			entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
			target_entity_id INTEGER NOT NULL REFERENCES entities(id))`,
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`,
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (2, 'Thing', 0)`,
		`INSERT INTO comp_carrier (entity_id, target_entity_id) VALUES (2, 1)`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatalf("building the old shape: %v", err)
		}
	}

	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.DeleteEntity(ctx, 1)
	if err == nil {
		t.Fatal("a legacy restrict let the delete through")
	}
	for _, want := range []string{"still references it", "predates"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to say %q", err, want)
		}
	}
}

// The two migrations that used to fail, and then fail on every open afterwards.
//
// entity-ref's column is target_entity_id and every other scalar's is value, so
// changing a component between them is a change of shape. It was reported as a
// changed property called "value", the generator answered with a rebuild, and
// the rebuild's copy read a column the old table did not have. Nothing was
// lost — the transaction rolled back — but NewSQLiteStore returned the error,
// so the only way forward was to edit schema.json back to what the database
// already had.
func TestMigration_AComponentCanChangeBetweenAReferenceAndAValue(t *testing.T) {
	mk := func(v int, comp schema.Component) schema.DatabaseSchema {
		return schema.DatabaseSchema{
			SchemaVersion: v,
			Components:    map[string]schema.Component{"X": comp},
			EntityTypes: map[string]schema.EntityType{
				"T": {RequiredComponents: []string{"X"}, ValidationLevel: "strict"},
			},
		}
	}

	for _, tc := range []struct {
		name       string
		from, to   schema.Component
		seed       string
		wantColumn string
	}{
		{
			"a string becomes a reference",
			schema.Component{Type: schema.ComponentTypeString},
			schema.Component{Type: schema.ComponentTypeEntityRef},
			`INSERT INTO comp_x (entity_id, value) VALUES (1, 'anything')`,
			"target_entity_id",
		},
		{
			"a reference becomes a string",
			schema.Component{Type: schema.ComponentTypeEntityRef},
			schema.Component{Type: schema.ComponentTypeString},
			`INSERT INTO comp_x (entity_id, target_entity_id) VALUES (1, 1)`,
			"value",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "w.sqlite")
			s1, err := NewSQLiteStore(path, mk(1, tc.from), "")
			if err != nil {
				t.Fatalf("v1: %v", err)
			}
			if _, err := s1.DB().Exec(
				`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'T', 0)`); err != nil {
				t.Fatalf("seeding an entity: %v", err)
			}
			if _, err := s1.DB().Exec(tc.seed); err != nil {
				t.Fatalf("seeding a row: %v", err)
			}
			_ = s1.Close()

			s2, err := NewSQLiteStore(path, mk(2, tc.to), "")
			if err != nil {
				t.Fatalf("the migration failed: %v", err)
			}
			defer func() { _ = s2.Close() }()

			// The table has the new shape.
			var n int
			if err := s2.DB().QueryRow(
				`SELECT COUNT(*) FROM pragma_table_info('comp_x') WHERE name = ?`, tc.wantColumn).Scan(&n); err != nil {
				t.Fatalf("reading columns: %v", err)
			}
			if n != 1 {
				t.Errorf("comp_x has no %s column after the change", tc.wantColumn)
			}

			// And the row is gone, which is the deliberate half. Neither
			// direction has a conversion — a string is not an entity id — so
			// the table is dropped and rebuilt empty. Asserted rather than
			// assumed, because a later change that silently started keeping
			// rows would be keeping values that mean nothing.
			var rows int
			if err := s2.DB().QueryRow(`SELECT COUNT(*) FROM comp_x`).Scan(&rows); err != nil {
				t.Fatalf("counting rows: %v", err)
			}
			if rows != 0 {
				t.Errorf("%d rows survived a change with no conversion behind it", rows)
			}

			// And it opens again, which is the part that used to be false.
			_ = s2.Close()
			again, err := NewSQLiteStore(path, mk(2, tc.to), "")
			if err != nil {
				t.Fatalf("reopening: %v", err)
			}
			_ = again.Close()
		})
	}
}

// A shape change cannot carry its data — there is no conversion from a string
// to an entity id — so it has to be refusable rather than silent. The confirm
// policy is what refuses it, and it can only do that if the statements are
// marked destructive.
func TestMigration_AShapeChangeIsOfferedForConfirmation(t *testing.T) {
	mk := func(v int, comp schema.Component) schema.DatabaseSchema {
		return schema.DatabaseSchema{
			SchemaVersion: v,
			Components:    map[string]schema.Component{"X": comp},
			EntityTypes: map[string]schema.EntityType{
				"T": {RequiredComponents: []string{"X"}, ValidationLevel: "strict"},
			},
		}
	}
	path := filepath.Join(t.TempDir(), "w.sqlite")
	s1, err := NewSQLiteStore(path, mk(1, schema.Component{Type: schema.ComponentTypeString}), "")
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	_ = s1.Close()

	_, err = NewSQLiteStoreWithConfig(path, StoreConfig{
		Schema:          mk(2, schema.Component{Type: schema.ComponentTypeEntityRef}),
		MigrationPolicy: MigrationConfirm,
		Logger:          NopLogger(),
	})
	if err == nil {
		t.Fatal("a change that throws a table away ran without being offered for confirmation")
	}
	var confirm *MigrationRequiresConfirmation
	if !errors.As(err, &confirm) {
		t.Fatalf("error = %v (%T), want MigrationRequiresConfirmation", err, err)
	}
	var sawDrop bool
	for _, stmt := range confirm.DestructiveStatements {
		if strings.Contains(stmt.SQL, "DROP TABLE") && strings.Contains(stmt.SQL, "comp_x") {
			sawDrop = true
		}
	}
	if !sawDrop {
		t.Errorf("destructive statements = %v, want the drop of comp_x among them", confirm.DestructiveStatements)
	}
}

// A drop reads the same whether somebody deleted a component or changed its
// shape, and only one of those is a surprise. The person being asked to confirm
// is entitled to know which.
func TestMigration_TheConfirmationSaysWhyTheTableIsGoing(t *testing.T) {
	mk := func(v int, comp schema.Component) schema.DatabaseSchema {
		return schema.DatabaseSchema{
			SchemaVersion: v,
			Components:    map[string]schema.Component{"X": comp},
			EntityTypes: map[string]schema.EntityType{
				"T": {RequiredComponents: []string{"X"}, ValidationLevel: "strict"},
			},
		}
	}
	path := filepath.Join(t.TempDir(), "w.sqlite")
	s1, err := NewSQLiteStore(path, mk(1, schema.Component{Type: schema.ComponentTypeString}), "")
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	_ = s1.Close()

	_, err = NewSQLiteStoreWithConfig(path, StoreConfig{
		Schema:          mk(2, schema.Component{Type: schema.ComponentTypeEntityRef}),
		MigrationPolicy: MigrationConfirm,
		Logger:          NopLogger(),
	})
	var confirm *MigrationRequiresConfirmation
	if !errors.As(err, &confirm) {
		t.Fatalf("error = %v, want MigrationRequiresConfirmation", err)
	}
	var explained bool
	for _, stmt := range confirm.DestructiveStatements {
		if strings.Contains(stmt.Description, "shape changed") {
			explained = true
		}
	}
	if !explained {
		var got []string
		for _, stmt := range confirm.DestructiveStatements {
			got = append(got, stmt.Description)
		}
		t.Errorf("descriptions = %v, want one saying the component's shape changed", got)
	}
}
