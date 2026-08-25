package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// ── Introspection ────────────────────────────────────────────────────

func columnsOf(t *testing.T, store *SQLiteStore, table string) map[string]DomainColumn {
	t.Helper()
	cols, err := IntrospectComponentTable(store.DB(), table)
	if err != nil {
		t.Fatalf("introspecting %s: %v", table, err)
	}
	out := map[string]DomainColumn{}
	for _, c := range cols {
		out[c.Name] = c
	}
	return out
}

func TestIntrospect_ReadsForeignKeys(t *testing.T) {
	store := refStore(t)

	cols := columnsOf(t, store, "comp_holder")
	if got := cols["entity_id"].References; got != schema.EntityReference {
		t.Errorf("entity_id references %q, want %q", got, schema.EntityReference)
	}
	if got := cols["owner"].References; got != schema.EntityReference {
		t.Errorf("owner references %q, want %q", got, schema.EntityReference)
	}
	if got := cols["hp"].References; got != schema.NoReference {
		t.Errorf("hp references %q, want nothing", got)
	}
}

// "No ON DELETE clause" and "no foreign key" are different answers, and the
// whole diff turns on telling them apart: the first is a refusal, the second is
// a dangling reference.
func TestIntrospect_NoActionIsNotTheSameAsNoForeignKey(t *testing.T) {
	store := refStore(t)
	for _, stmt := range []string{
		`DROP TABLE comp_holder`,
		`CREATE TABLE comp_holder (
			entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
			owner INTEGER REFERENCES entities(id),
			hp INTEGER NOT NULL)`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatalf("building the old shape: %v", err)
		}
	}

	cols := columnsOf(t, store, "comp_holder")
	if got := cols["owner"].References; got != "entities(id) ON DELETE NO ACTION" {
		t.Errorf("a reference with no ON DELETE clause reads as %q", got)
	}
	if got := cols["hp"].References; got != schema.NoReference {
		t.Errorf("a column with no reference reads as %q", got)
	}
}

// A reference written without naming a column. No generated table has one; a
// hand-edited database might, and it must not read as the canonical form.
func TestIntrospect_AReferenceWithNoNamedColumn(t *testing.T) {
	store := refStore(t)
	for _, stmt := range []string{
		`DROP TABLE comp_holder`,
		`CREATE TABLE comp_holder (
			entity_id INTEGER PRIMARY KEY REFERENCES entities ON DELETE CASCADE,
			hp INTEGER NOT NULL)`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatalf("building it: %v", err)
		}
	}
	// Asserted exactly, not just "neither of the two answers that would be
	// wrong" — that weaker form passes for any garbage string, including one a
	// bug produced.
	got := columnsOf(t, store, "comp_holder")["entity_id"].References
	if want := "entities ON DELETE CASCADE"; got != want {
		t.Errorf("a reference that names no column reads as %q, want %q", got, want)
	}
}

// ── Agreement ────────────────────────────────────────────────────────

// agreementSchema declares a component of every component type, plus an object
// carrying a property of every property type — so the tables built below are
// every table the generator can build.
func agreementSchema(t *testing.T) (schema.DatabaseSchema, map[string]schema.Component) {
	t.Helper()
	comps := map[string]schema.Component{}
	for _, ct := range schema.ComponentTypes() {
		c := schema.Component{Type: ct}
		switch ct {
		case schema.ComponentTypeArray:
			c.Items = &schema.Property{Type: schema.PropertyTypeString}
		case schema.ComponentTypeObject:
			props := map[string]schema.Property{}
			for _, pt := range schema.PropertyTypes() {
				p := schema.Property{Type: pt}
				if pt == schema.PropertyTypeArray {
					p.Items = &schema.Property{Type: schema.PropertyTypeString}
				}
				props["p_"+strings.ReplaceAll(pt, "-", "_")] = p
			}
			c.Properties = props
		}
		comps["c_"+strings.ReplaceAll(ct, "-", "_")] = c
	}
	s := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    comps,
		EntityTypes: map[string]schema.EntityType{
			"Thing": {ValidationLevel: "strict"},
		},
	}
	return s, comps
}

// What the diff expects a column to reference is what the generator actually
// emits, for every type there is. Both sides are enumerated rather than listed,
// so a type added later has to be given an answer instead of passing quietly.
func TestReferences_TheDiffExpectsWhatTheGeneratorEmits(t *testing.T) {
	s, comps := agreementSchema(t)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "w.sqlite"), s, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	for name, comp := range comps {
		table := "comp_" + strings.ToLower(name)
		built := columnsOf(t, store, table)
		want := schema.ColumnReferences(comp)

		if len(built) != len(want) {
			t.Errorf("%s has columns %v, the diff expects %v", table, keysOf(built), keysOf(want))
			continue
		}
		for col, wantRef := range want {
			got, ok := built[col]
			if !ok {
				t.Errorf("%s has no column %q, which the diff expects", table, col)
				continue
			}
			if got.References != wantRef {
				t.Errorf("%s.%s references %q, the diff expects %q", table, col, got.References, wantRef)
			}
		}
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// A database the generator just built diffs to nothing. Without this the story
// trades a constraint that is never repaired for every table being rebuilt on
// every open.
func TestReferences_AFreshDatabaseHasNothingToMigrate(t *testing.T) {
	s, _ := agreementSchema(t)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "w.sqlite"), s, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	plan, err := NewMigrationRunner(store.DB(), s, MigrationAuto, NopLogger()).Plan()
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	if !plan.Empty() {
		t.Errorf("a freshly built database plans %d statements: %+v", len(plan.Statements), plan.Statements)
	}
}

// ── Repair ───────────────────────────────────────────────────────────

// oldShapeStore builds the database Story 9 would have left: a component of
// type entity-ref whose reference refuses instead of cascading, and a reference
// property with no foreign key at all. It seeds a row in each, because a repair
// that lost the data would not be one.
func oldShapeStore(t *testing.T, path string) {
	t.Helper()
	store, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	for _, stmt := range []string{
		`DROP TABLE comp_carrier`,
		`CREATE TABLE comp_carrier (
			entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
			target_entity_id INTEGER NOT NULL REFERENCES entities(id))`,
		`DROP TABLE comp_holder`,
		`CREATE TABLE comp_holder (
			entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
			owner INTEGER NOT NULL,
			hp INTEGER NOT NULL)`,
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`,
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (2, 'Thing', 0)`,
		`INSERT INTO comp_carrier (entity_id, target_entity_id) VALUES (2, 1)`,
		`INSERT INTO comp_holder (entity_id, owner, hp) VALUES (2, 1, 7)`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatalf("building the old shape (%s): %v", stmt, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
}

// The replacement for TestMigration_DoesNotNoticeAConstraintThatOnlyTheGenerator
// Changed: both forms are repaired, and the rows come across.
func TestMigration_RepairsAConstraintTheGeneratorChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	oldShapeStore(t, path)

	// The same schema, at the same version. Nothing about the file changed —
	// only the engine did, which is the case that used to be invisible.
	migrated, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	if got := foreignKeys(t, migrated.DB(), "comp_carrier"); !contains(got, "target_entity_id ON DELETE CASCADE") {
		t.Errorf("comp_carrier has %v, want target_entity_id to cascade", got)
	}
	if got := foreignKeys(t, migrated.DB(), "comp_holder"); !contains(got, "owner ON DELETE CASCADE") {
		t.Errorf("comp_holder has %v, want owner to cascade", got)
	}

	for _, q := range []struct {
		what string
		sql  string
		want int
	}{
		{"the carrier row", `SELECT target_entity_id FROM comp_carrier WHERE entity_id = 2`, 1},
		{"the holder's owner", `SELECT owner FROM comp_holder WHERE entity_id = 2`, 1},
		{"the holder's hp", `SELECT hp FROM comp_holder WHERE entity_id = 2`, 7},
	} {
		var got int
		if err := migrated.DB().QueryRow(q.sql).Scan(&got); err != nil {
			t.Fatalf("reading %s: %v", q.what, err)
		}
		if got != q.want {
			t.Errorf("%s = %d, want %d — the repair lost data", q.what, got, q.want)
		}
	}
}

// And the repair means what it says: deleting the target now takes the
// pointing component with it, on a database that used to refuse.
func TestMigration_TheRepairedConstraintCascades(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	oldShapeStore(t, path)

	migrated, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	if _, err := migrated.DB().Exec(`DELETE FROM entities WHERE id = 1`); err != nil {
		t.Fatalf("deleting the target: %v", err)
	}
	for _, table := range []string{"comp_carrier", "comp_holder"} {
		var n int
		if err := migrated.DB().QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM %s`, table)).Scan(&n); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s still has %d row(s) pointing at a deleted entity", table, n)
		}
	}
}

// Repairing once is enough: the second open must find nothing to do, or the
// engine rebuilds every table on every start.
func TestMigration_RepairsOnceAndThenStops(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	oldShapeStore(t, path)

	first, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_ = first.Close()

	second, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer func() { _ = second.Close() }()

	plan, err := NewMigrationRunner(second.DB(), refSchema(), MigrationAuto, NopLogger()).Plan()
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	if !plan.Empty() {
		t.Errorf("the second open still plans %d statements: %+v", len(plan.Statements), plan.Statements)
	}
}

// The version gate, in the case that is not about constraints at all: a schema
// edit saved without bumping schemaVersion used to do nothing, which Forge had
// to warn about rather than rely on.
func TestMigration_RunsWhenTheShapeChangedAndTheVersionDidNot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	store, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	_ = store.Close()

	// A property added, and the version left where it was.
	next := refSchema()
	pos := next.Components["Position"]
	pos.Properties = map[string]schema.Property{
		"x": {Type: schema.PropertyTypeInteger},
		"y": {Type: schema.PropertyTypeInteger},
	}
	next.Components["Position"] = pos

	migrated, err := NewSQLiteStore(path, next, "")
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	var n int
	if err := migrated.DB().QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('comp_position') WHERE name = 'y'`).Scan(&n); err != nil {
		t.Fatalf("looking for the column: %v", err)
	}
	if n != 1 {
		t.Error("the column the file declares was never added")
	}
}

// ── One rebuild per component ────────────────────────────────────────

// genRebuild ignores the change it is given — it builds the whole table from
// the file schema — so two changes that both want a rebuild produced two
// identical sequences. A constraint change and a property change on the same
// component is exactly what an old database has.
func TestGenerate_OneRebuildPerComponent(t *testing.T) {
	file := &schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"Holder": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"owner": {Type: schema.PropertyTypeEntityRef},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	domain := &DomainSchema{
		SchemaVersion: 1,
		Components: map[string]DomainComponent{"holder": {Columns: []DomainColumn{
			{Name: "entity_id", SQLType: "INTEGER", References: schema.EntityReference, IsPK: true},
			{Name: "owner", SQLType: "INTEGER", References: schema.NoReference},
			{Name: "gone", SQLType: "TEXT"},
		}}},
		EntityTypeNames: map[string]bool{},
	}

	changes := schema.Diff(domain.ToDiffSchema(), file, nil)
	var wanted int
	for _, c := range changes {
		if c.Kind == schema.ChangeChangedConstraint || c.Kind == schema.ChangeRemovedProperty {
			wanted++
		}
	}
	if wanted < 2 {
		t.Fatalf("this test needs two rebuild-causing changes, got %+v", changes)
	}

	stmts := NewGenerator(file, domain, Config{StrictDrop: true}).Generate(changes)
	var creates int
	for _, s := range stmts {
		if s.Kind == "rebuild_table" && strings.HasPrefix(s.SQL, "CREATE TABLE") {
			creates++
		}
	}
	if creates != 1 {
		t.Errorf("%d rebuilds of comp_holder, want 1:\n%s", creates, sqlOf(stmts))
	}
}

func sqlOf(stmts []Statement) string {
	var b strings.Builder
	for _, s := range stmts {
		fmt.Fprintf(&b, "  [%s] %s\n", s.Kind, strings.ReplaceAll(s.SQL, "\n", " "))
	}
	return b.String()
}

// ── The survivors of the mutation battery ────────────────────────────

// SQLite column names are case-insensitive and the generator writes them
// lowercase, but a hand-edited table can carry any casing and it is still the
// same column. A comparison keyed on the database's casing would find no column
// called "owner", report nothing, and leave the reference unrepaired.
func TestMigration_RepairsAReferenceWhateverTheColumnCasingIs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	store, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	for _, stmt := range []string{
		`DROP TABLE comp_holder`,
		`CREATE TABLE comp_holder (
			entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
			Owner INTEGER,
			hp INTEGER NOT NULL)`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatalf("building the old shape: %v", err)
		}
	}
	_ = store.Close()

	migrated, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	if got := foreignKeys(t, migrated.DB(), "comp_holder"); !contains(got, "owner ON DELETE CASCADE") {
		t.Errorf("comp_holder has %v, want owner to cascade", got)
	}
}

// What a rebuild says for itself, which is the whole content of the
// confirmation dialog it appears in. Two columns drift in opposite directions,
// so there are two things to say and one rebuild to say them in.
func TestGenerate_ARebuildSaysWhyEveryColumnWantedIt(t *testing.T) {
	file := &schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Holder": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"owner":  {Type: schema.PropertyTypeEntityRef}, // gains one
				"keeper": {Type: schema.PropertyTypeInteger},   // loses one
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	domain := &DomainSchema{
		SchemaVersion: 1,
		Components: map[string]DomainComponent{"holder": {Columns: []DomainColumn{
			{Name: "entity_id", SQLType: "INTEGER", References: schema.EntityReference, IsPK: true},
			{Name: "owner", SQLType: "INTEGER", References: schema.NoReference},
			{Name: "keeper", SQLType: "INTEGER", References: schema.EntityReference},
		}}},
		EntityTypeNames: map[string]bool{},
	}

	changes := schema.Diff(domain.ToDiffSchema(), file, nil)
	stmts := NewGenerator(file, domain, Config{StrictDrop: true}).Generate(changes)

	var created string
	for _, s := range stmts {
		if s.Kind == "rebuild_table" && strings.HasPrefix(s.SQL, "CREATE TABLE") {
			created = s.Description
		}
	}
	if created == "" {
		t.Fatalf("no rebuild was generated:\n%s", sqlOf(stmts))
	}
	for _, want := range []string{
		"owner gains a foreign key it never had",
		"keeper loses its foreign key to " + schema.EntityReference,
	} {
		if !strings.Contains(created, want) {
			t.Errorf("the rebuild is described as %q, which does not say %q", created, want)
		}
	}
}

// An empty plan is not applied. Applying one commits nothing, so the cost is
// not visible in the database — it is a backup file, written on every open of
// a database that had nothing wrong with it.
func TestOpen_AMatchingDatabaseIsNotBackedUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "w.sqlite")

	cfg := StoreConfig{
		Schema:          refSchema(),
		MigrationPolicy: MigrationAuto,
		Logger:          NopLogger(),
		BackupRetention: 3,
	}
	store, err := NewSQLiteStoreWithConfig(path, cfg)
	if err != nil {
		t.Fatalf("NewSQLiteStoreWithConfig: %v", err)
	}
	_ = store.Close()

	again, err := NewSQLiteStoreWithConfig(path, cfg)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer func() { _ = again.Close() }()

	backups, err := filepath.Glob(path + ".bak.v*")
	if err != nil {
		t.Fatalf("looking for backups: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("opening a database that already matched wrote %v", backups)
	}
}

// And the other half: a database that does need repairing is backed up first.
func TestOpen_ARepairIsBackedUpFirst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "w.sqlite")
	oldShapeStore(t, path)

	store, err := NewSQLiteStoreWithConfig(path, StoreConfig{
		Schema:          refSchema(),
		MigrationPolicy: MigrationAuto,
		Logger:          NopLogger(),
		BackupRetention: 3,
	})
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer func() { _ = store.Close() }()

	backups, err := filepath.Glob(path + ".bak.v*")
	if err != nil {
		t.Fatalf("looking for backups: %v", err)
	}
	if len(backups) != 1 {
		t.Errorf("a repair wrote %v, want one backup", backups)
	}
}

// ── The review's findings ────────────────────────────────────────────

// A table whose name merely begins with "comp" is not a component table.
//
// SQL LIKE treats "_" as a single-character wildcard, so 'comp_%' matched
// "compact_things" too. It was introspected as a component, TrimPrefix left the
// name alone because it does not start with "comp_", and the diff asked for
// "comp_compact_things" — which does not exist, so the DROP succeeded and
// changed nothing, and the next plan was the same one. With the version gate
// open that repeats on every start, backup and all, forever.
func TestOpen_ATableThatMerelyStartsWithCompIsLeftAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	store, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if _, err := store.DB().Exec(
		`CREATE TABLE compact_things (id INTEGER PRIMARY KEY, note TEXT)`); err != nil {
		t.Fatalf("creating the neighbouring table: %v", err)
	}
	if _, err := store.DB().Exec(
		`INSERT INTO compact_things (id, note) VALUES (1, 'keep me')`); err != nil {
		t.Fatalf("seeding it: %v", err)
	}

	if tables, err := ListComponentTables(store.DB()); err != nil {
		t.Fatalf("listing: %v", err)
	} else if contains(tables, "compact_things") {
		t.Errorf("ListComponentTables = %v, which includes a table that is not a component", tables)
	}

	plan, err := NewMigrationRunner(store.DB(), refSchema(), MigrationAuto, NopLogger()).Plan()
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	if !plan.Empty() {
		t.Errorf("a neighbouring table makes the engine plan %d statement(s) on every open: %+v",
			len(plan.Statements), plan.Statements)
	}

	var note string
	if err := store.DB().QueryRow(`SELECT note FROM compact_things WHERE id = 1`).Scan(&note); err != nil {
		t.Fatalf("the neighbouring table did not survive: %v", err)
	}
	_ = store.Close()
}

// A column carrying the right foreign key *and* a stray one is not correct.
// Keeping only one of them read as correct and was never repaired.
func TestIntrospect_AColumnWithTwoForeignKeys(t *testing.T) {
	store := refStore(t)
	for _, stmt := range []string{
		`DROP TABLE comp_holder`,
		`CREATE TABLE comp_holder (
			entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
			owner INTEGER REFERENCES entities(id) ON DELETE CASCADE
			                 REFERENCES entities(id) ON DELETE SET NULL,
			hp INTEGER NOT NULL)`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatalf("building it: %v", err)
		}
	}

	got := columnsOf(t, store, "comp_holder")["owner"].References
	if got == schema.EntityReference {
		t.Fatalf("a column with two foreign keys reads as %q — the stray one is invisible", got)
	}
	if !strings.Contains(got, "ON DELETE CASCADE") || !strings.Contains(got, "ON DELETE SET NULL") {
		t.Errorf("owner reads as %q, want both of its keys", got)
	}

	plan, err := NewMigrationRunner(store.DB(), refSchema(), MigrationAuto, NopLogger()).Plan()
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	if plan.Empty() {
		t.Error("a column with a stray foreign key is not repaired")
	}
}

// Two repairs at one schemaVersion keep two restore points. The backup used to
// be named by version alone and deleted before writing, so the second repair
// destroyed what the first had saved — and retention could not help, because it
// counted version-named files and there was only ever one.
func TestOpen_ASecondRepairDoesNotDestroyTheFirstsBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	oldShapeStore(t, path)

	cfg := StoreConfig{
		Schema:          refSchema(),
		MigrationPolicy: MigrationAuto,
		Logger:          NopLogger(),
		BackupRetention: 5,
	}
	first, err := NewSQLiteStoreWithConfig(path, cfg)
	if err != nil {
		t.Fatalf("first repair: %v", err)
	}
	// A second thing to repair, at the same version: another table put back the
	// way an older engine built it.
	if _, err := first.DB().Exec(`DROP TABLE comp_position`); err != nil {
		t.Fatalf("dropping: %v", err)
	}
	_ = first.Close()

	second, err := NewSQLiteStoreWithConfig(path, cfg)
	if err != nil {
		t.Fatalf("second repair: %v", err)
	}
	defer func() { _ = second.Close() }()

	backups, err := filepath.Glob(path + ".bak.v*")
	if err != nil {
		t.Fatalf("looking for backups: %v", err)
	}
	if len(backups) != 2 {
		t.Fatalf("two repairs at one version left %v, want two backups", backups)
	}

	// The first backup still holds the database as it was before anything was
	// repaired, which is the whole point of having it.
	sort.Strings(backups)
	if got := foreignKeys(t, openBackup(t, backups[0]), "comp_carrier"); !contains(got, "target_entity_id ON DELETE NO ACTION") {
		t.Errorf("the oldest backup has %v — it no longer holds the pre-repair state", got)
	}
}

func openBackup(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("opening backup %s: %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// A constraint change is a modification, and modifications run after additions.
// Ordering it with them instead would emit the rebuild before the ALTER TABLE
// that adds a column, and the rebuild's copy would read a column that is not
// there yet — the same failure Story 10 fixed, arriving by a different route.
//
// The added column's name sorts after the drifted one, so within a phase the
// rebuild would come first.
func TestMigration_AConstraintChangeIsAppliedAfterAnAddedColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	store, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	// comp_holder as an older engine built it: owner with no foreign key.
	for _, stmt := range []string{
		`DROP TABLE comp_holder`,
		`CREATE TABLE comp_holder (
			entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
			owner INTEGER,
			hp INTEGER NOT NULL)`,
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`,
		`INSERT INTO comp_holder (entity_id, owner, hp) VALUES (1, NULL, 4)`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatalf("building the old shape: %v", err)
		}
	}
	_ = store.Close()

	// And the file gains a property, whose name sorts after "owner".
	next := refSchema()
	holder := next.Components["Holder"]
	holder.Properties = map[string]schema.Property{
		"owner": {Type: schema.PropertyTypeEntityRef},
		"hp":    {Type: schema.PropertyTypeInteger},
		"zzz":   {Type: schema.PropertyTypeInteger},
	}
	next.Components["Holder"] = holder

	migrated, err := NewSQLiteStore(path, next, "")
	if err != nil {
		t.Fatalf("migrating a component that both gained a column and drifted: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	if got := foreignKeys(t, migrated.DB(), "comp_holder"); !contains(got, "owner ON DELETE CASCADE") {
		t.Errorf("comp_holder has %v, want owner to cascade", got)
	}
	var hp int
	if err := migrated.DB().QueryRow(`SELECT hp FROM comp_holder WHERE entity_id = 1`).Scan(&hp); err != nil {
		t.Fatalf("the row did not survive: %v", err)
	}
	if hp != 4 {
		t.Errorf("hp = %d, want 4", hp)
	}
}

// The failure that leaves a database unopenable says what to do about it. Until
// Story 11 opened the version gate this needed a schemaVersion bump to reach;
// it is now one save away.
func TestMigration_TheUnrepairableFailureSaysWhichFileToEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	store, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`,
		`INSERT INTO comp_holder (entity_id, owner, hp) VALUES (1, NULL, 4)`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	_ = store.Close()

	// Retyping the one property kind that may be NULL to one that may not.
	next := refSchema()
	holder := next.Components["Holder"]
	holder.Properties = map[string]schema.Property{
		"owner": {Type: schema.PropertyTypeInteger},
		"hp":    {Type: schema.PropertyTypeInteger},
	}
	next.Components["Holder"] = holder

	_, err = NewSQLiteStore(path, next, "")
	if err == nil {
		t.Fatal("the rebuild succeeded — if NULLs are now carried across, this test should go")
	}
	var migErr *SchemaMigrationError
	if !errors.As(err, &migErr) {
		t.Fatalf("got %T, want *SchemaMigrationError", err)
	}
	if migErr.Hint == "" {
		t.Fatal("the error that makes a database unopenable offers no hint")
	}
	for _, want := range []string{"schema.json", "holder"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q:\n%s", want, err)
		}
	}
}

// The hint is advice, and advice attached to the wrong failure is worse than
// none: it tells somebody to revert a schema change on a database that is not
// wedged and has some other problem entirely.
func TestMigrationHint_OnlyTheFailureThatWedges(t *testing.T) {
	cases := []struct {
		name     string
		stmt     Statement
		err      error
		wantHint bool
	}{
		{
			name:     "a rebuild's copy refusing a NULL",
			stmt:     Statement{Kind: "rebuild_table", Component: "holder"},
			err:      errors.New("NOT NULL constraint failed: comp_holder_new.owner (1299)"),
			wantHint: true,
		},
		{
			name:     "a rebuild failing for some other reason",
			stmt:     Statement{Kind: "rebuild_table", Component: "holder"},
			err:      errors.New("SQL logic error: no such column: zzz (1)"),
			wantHint: false,
		},
		{
			// A different kind of statement, so a different thing to say.
			name:     "an ALTER refusing a NOT NULL column",
			stmt:     Statement{Kind: "alter_add_column", Component: "holder"},
			err:      errors.New("NOT NULL constraint failed: comp_holder.owner (1299)"),
			wantHint: false,
		},
		{
			name:     "a table being dropped",
			stmt:     Statement{Kind: "drop_table", Component: "holder"},
			err:      errors.New("database is locked (5)"),
			wantHint: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := migrationHint(tc.err, tc.stmt)
			if (got != "") != tc.wantHint {
				t.Errorf("hint = %q, want a hint: %v", got, tc.wantHint)
			}
		})
	}
}
