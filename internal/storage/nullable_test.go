package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// wedgeStore is the database the wedge needs: a Holder whose owner — the one
// column the generator declares nullable — is actually NULL, alongside a row
// that has one, so a repair that dropped rows or flattened them all would show.
func wedgeStore(t *testing.T, path string) {
	t.Helper()
	store, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`,
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (2, 'Thing', 0)`,
		`INSERT INTO comp_holder (entity_id, owner, hp) VALUES (1, NULL, 4)`,
		`INSERT INTO comp_holder (entity_id, owner, hp) VALUES (2, 1, 7)`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatalf("seeding (%s): %v", stmt, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
}

// retyped is refSchema with Holder.owner changed from entity-ref to some other
// type — the edit that used to make the database refuse to open forever.
func retyped(propType string) schema.DatabaseSchema {
	s := refSchema()
	h := s.Components["Holder"]
	h.Properties = map[string]schema.Property{
		"owner": {Type: propType},
		"hp":    {Type: schema.PropertyTypeInteger},
	}
	s.Components["Holder"] = h
	return s
}

// The wedge, gone: the migration runs and both rows come across.
func TestMigration_RetypingAReferencePropertyCarriesItsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	wedgeStore(t, path)

	migrated, err := NewSQLiteStore(path, retyped(schema.PropertyTypeInteger), "")
	if err != nil {
		t.Fatalf("retyping a reference property that holds NULLs: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	var rows int
	if err := migrated.DB().QueryRow(`SELECT COUNT(*) FROM comp_holder`).Scan(&rows); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 2 {
		t.Errorf("comp_holder has %d rows, want 2 — the repair lost data", rows)
	}

	// The row that had a value keeps it; the row that had none gets the value
	// ALTER TABLE ADD COLUMN would have given it.
	for _, q := range []struct {
		entity int
		want   int
		what   string
	}{
		{2, 1, "the row that had an owner"},
		{1, 0, "the row that had none"},
	} {
		var got int
		if err := migrated.DB().QueryRow(
			`SELECT owner FROM comp_holder WHERE entity_id = ?`, q.entity).Scan(&got); err != nil {
			t.Fatalf("reading %s: %v", q.what, err)
		}
		if got != q.want {
			t.Errorf("%s has owner = %d, want %d", q.what, got, q.want)
		}
	}

	var hp int
	if err := migrated.DB().QueryRow(
		`SELECT hp FROM comp_holder WHERE entity_id = 1`).Scan(&hp); err != nil {
		t.Fatalf("reading hp: %v", err)
	}
	if hp != 4 {
		t.Errorf("hp = %d, want 4 — the other columns did not come across", hp)
	}
}

// The substituted value is the one ALTER TABLE ADD COLUMN uses, for every type a
// property can be retyped to. Adding a NOT NULL property and retyping one into
// a NOT NULL property are the same question, and used to have opposite answers:
// one invented a value, the other refused to open the database again.
func TestMigration_TheSubstitutedValueIsTheOneAddingAColumnWouldUse(t *testing.T) {
	for _, pt := range schema.PropertyTypes() {
		if pt == schema.PropertyTypeEntityRef {
			continue // stays nullable; nothing to substitute
		}
		t.Run(pt, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "w.sqlite")
			wedgeStore(t, path)

			migrated, err := NewSQLiteStore(path, retyped(pt), "")
			if err != nil {
				t.Fatalf("retyping to %s: %v", pt, err)
			}
			defer func() { _ = migrated.Close() }()

			var got string
			if err := migrated.DB().QueryRow(
				`SELECT CAST(owner AS TEXT) FROM comp_holder WHERE entity_id = 1`).Scan(&got); err != nil {
				t.Fatalf("reading the substituted value: %v", err)
			}
			// What ALTER TABLE ADD COLUMN would have written, with its SQL
			// quoting removed — the same source, so the two cannot drift.
			want := strings.Trim(defaultValueForProperty(schema.Property{Type: pt}), "'")
			if got != want {
				t.Errorf("a NULL %s became %q, want %q — the value differs from the one adding the column uses",
					pt, got, want)
			}
		})
	}
}

// Loud, because it invents data. Under MigrationAuto — the default, and what the
// engine runs — the log is the only place this is ever mentioned.
func TestMigration_SubstitutingAValueIsLogged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	wedgeStore(t, path)

	log := &recordingLogger{}
	store, err := NewSQLiteStoreWithConfig(path, StoreConfig{
		Schema:          retyped(schema.PropertyTypeInteger),
		MigrationPolicy: MigrationAuto,
		Logger:          log,
	})
	if err != nil {
		t.Fatalf("migrating: %v", err)
	}
	defer func() { _ = store.Close() }()

	said := strings.Join(log.warnings, "\n")
	// "1 row is", not "1 row" — the shorter needle is a substring of the
	// ungrammatical "1 rows are" that dropping the singular would produce, so
	// asserting it would not notice.
	for _, want := range []string{"comp_holder", "owner", "1 row is", "0"} {
		if !strings.Contains(said, want) {
			t.Errorf("the warnings do not mention %q:\n%s", want, said)
		}
	}
}

// And the plural, so the singular is a branch rather than the only wording.
func TestMigration_SubstitutingValuesCountsThemAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	store, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	for i := 1; i <= 3; i++ {
		if _, err := store.DB().Exec(
			`INSERT INTO entities (id, entity_type, created_tick) VALUES (?, 'Thing', 0)`, i); err != nil {
			t.Fatalf("seeding: %v", err)
		}
		if _, err := store.DB().Exec(
			`INSERT INTO comp_holder (entity_id, owner, hp) VALUES (?, NULL, 1)`, i); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	_ = store.Close()

	log := &recordingLogger{}
	migrated, err := NewSQLiteStoreWithConfig(path, StoreConfig{
		Schema:          retyped(schema.PropertyTypeInteger),
		MigrationPolicy: MigrationAuto,
		Logger:          log,
	})
	if err != nil {
		t.Fatalf("migrating: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	said := strings.Join(log.warnings, "\n")
	if !strings.Contains(said, "3 rows are") {
		t.Errorf("three substituted rows were reported as:\n%s", said)
	}
}

type recordingLogger struct {
	warnings []string
	infos    []string
}

func (l *recordingLogger) Warnf(format string, args ...interface{}) {
	l.warnings = append(l.warnings, fmt.Sprintf(format, args...))
}

func (l *recordingLogger) Infof(format string, args ...interface{}) {
	l.infos = append(l.infos, fmt.Sprintf(format, args...))
}

// An ordinary rebuild's SQL is unchanged: the substitution appears only where a
// NULL can actually be. Forge previews this text, and a COALESCE around every
// column would be noise in every migration anyone ever reads.
func TestGenerate_NoSubstitutionWhereNoNullCanBe(t *testing.T) {
	file := &schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"Holder": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"hp": {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	// hp is NOT NULL already; the rebuild is for the dropped column.
	domain := &DomainSchema{
		SchemaVersion: 1,
		Components: map[string]DomainComponent{"holder": {Columns: []DomainColumn{
			{Name: "entity_id", SQLType: "INTEGER", References: schema.EntityReference, IsPK: true},
			{Name: "hp", SQLType: "INTEGER"},
			{Name: "gone", SQLType: "TEXT"},
		}}},
		EntityTypeNames: map[string]bool{},
	}

	stmts := NewGenerator(file, domain, Config{StrictDrop: true}).
		Generate(schema.Diff(domain.ToDiffSchema(), file, nil))

	for _, s := range stmts {
		if strings.Contains(s.SQL, "COALESCE") {
			t.Errorf("a rebuild with no nullable column emitted %q", s.SQL)
		}
	}
	if len(stmts) == 0 {
		t.Fatal("nothing was generated, so this proves nothing")
	}
}

// ── Nullability is part of the shape ─────────────────────────────────

func TestIntrospect_ReadsNullability(t *testing.T) {
	store := refStore(t)
	cols := columnsOf(t, store, "comp_holder")

	if !cols["owner"].Nullable {
		t.Error("owner is a reference property and reads as NOT NULL")
	}
	if cols["hp"].Nullable {
		t.Error("hp reads as nullable")
	}
	if got := columnsOf(t, store, "comp_carrier")["target_entity_id"]; got.Nullable {
		t.Error("a component that is a reference reads as nullable")
	}
}

// What the diff expects is what the generator emits, for every type there is —
// built as a real table rather than asserted from the same source twice.
func TestNullability_TheDiffExpectsWhatTheGeneratorEmits(t *testing.T) {
	s, comps := agreementSchema(t)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "w.sqlite"), s, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	for name, comp := range comps {
		table := "comp_" + strings.ToLower(name)
		built := columnsOf(t, store, table)
		want := schema.ColumnNullable(comp)
		// The primary key is absent from the map by design, so the built table
		// has exactly one more column than the map has entries. Without this
		// the loop below would pass over an empty map, which is what a
		// ColumnNullable that answered for nothing would produce.
		if len(built) != len(want)+1 {
			t.Errorf("%s has columns %v, the diff has expectations for %v",
				table, keysOf(built), keysOf(want))
			continue
		}
		for col, wantNullable := range want {
			got, ok := built[col]
			if !ok {
				t.Errorf("%s has no column %q, which the diff expects", table, col)
				continue
			}
			if got.Nullable != wantNullable {
				t.Errorf("%s.%s is nullable=%v, the diff expects %v",
					table, col, got.Nullable, wantNullable)
			}
		}
	}
}

// A hand-edited nullable column is repaired, and the rows it holds survive it.
func TestMigration_RepairsAColumnThatShouldRefuseNulls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	store, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	for _, stmt := range []string{
		`DROP TABLE comp_holder`,
		`CREATE TABLE comp_holder (
			entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
			owner INTEGER REFERENCES entities(id) ON DELETE CASCADE,
			hp INTEGER)`,
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`,
		`INSERT INTO comp_holder (entity_id, owner, hp) VALUES (1, NULL, NULL)`,
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

	var notNull int
	if err := migrated.DB().QueryRow(
		`SELECT "notnull" FROM pragma_table_info('comp_holder') WHERE name = 'hp'`).Scan(&notNull); err != nil {
		t.Fatalf("reading the column: %v", err)
	}
	if notNull != 1 {
		t.Error("hp still accepts NULLs")
	}
	// owner is a reference property, so it stays nullable.
	var ownerNotNull int
	if err := migrated.DB().QueryRow(
		`SELECT "notnull" FROM pragma_table_info('comp_holder') WHERE name = 'owner'`).Scan(&ownerNotNull); err != nil {
		t.Fatalf("reading the column: %v", err)
	}
	if ownerNotNull != 0 {
		t.Error("a reference property was made NOT NULL, which is the wedge Story 9 found")
	}

	var hp int
	var owner sql.NullInt64
	if err := migrated.DB().QueryRow(
		`SELECT hp, owner FROM comp_holder WHERE entity_id = 1`).Scan(&hp, &owner); err != nil {
		t.Fatalf("the row did not survive: %v", err)
	}
	if hp != 0 {
		t.Errorf("hp = %d, want the substituted 0", hp)
	}
	if owner.Valid {
		t.Errorf("owner = %v, want it left NULL", owner.Int64)
	}
}

// Repairing once is enough.
func TestMigration_NullabilityRepairsOnceAndThenStops(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	wedgeStore(t, path)

	first, err := NewSQLiteStore(path, retyped(schema.PropertyTypeInteger), "")
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_ = first.Close()

	second, err := NewSQLiteStore(path, retyped(schema.PropertyTypeInteger), "")
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer func() { _ = second.Close() }()

	plan, err := NewMigrationRunner(second.DB(), retyped(schema.PropertyTypeInteger), MigrationAuto, NopLogger()).Plan()
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	if !plan.Empty() {
		t.Errorf("the second open still plans %d statements: %+v", len(plan.Statements), plan.Statements)
	}
}

// ── The survivors of the mutation battery ────────────────────────────

// copyExpression's contract, branch by branch. Some of these are not reachable
// from any schema the generator can build — an entity-ref property is the only
// nullable column and its default is NULL, so two of the guards hold for the
// same reason today. They are separate conditions because they answer separate
// questions, and a column that is nullable in the new table needs no
// substitution whatever its default turns out to be.
func TestCopyExpression(t *testing.T) {
	nullableOld := []DomainColumn{{Name: "owner", Nullable: true}}

	cases := []struct {
		name string
		col  rebuildColumn
		old  []DomainColumn
		want string
	}{
		{
			name: "a NOT NULL column reading from a nullable one",
			col:  rebuildColumn{Name: "owner", NotNull: true, Default: "0"},
			old:  nullableOld,
			want: "COALESCE(owner, 0)",
		},
		{
			name: "the same column when the old one already refused NULL",
			col:  rebuildColumn{Name: "owner", NotNull: true, Default: "0"},
			old:  []DomainColumn{{Name: "owner", Nullable: false}},
			want: "owner",
		},
		{
			name: "a column that still accepts NULL needs nothing substituted",
			col:  rebuildColumn{Name: "owner", NotNull: false, Default: "0"},
			old:  nullableOld,
			want: "owner",
		},
		{
			name: "a NOT NULL column with no default has nothing to substitute",
			col:  rebuildColumn{Name: "owner", NotNull: true, Default: ""},
			old:  nullableOld,
			want: "owner",
		},
		{
			name: "a default of NULL is not a value",
			col:  rebuildColumn{Name: "owner", NotNull: true, Default: "NULL"},
			old:  nullableOld,
			want: "owner",
		},
		{
			name: "a column the old table does not have",
			col:  rebuildColumn{Name: "owner", NotNull: true, Default: "0"},
			old:  []DomainColumn{{Name: "hp", Nullable: true}},
			want: "owner",
		},
		{
			name: "the old column's casing does not matter",
			col:  rebuildColumn{Name: "owner", NotNull: true, Default: "0"},
			old:  []DomainColumn{{Name: "Owner", Nullable: true}},
			want: "COALESCE(owner, 0)",
		},
		{
			// SQLite reports every INTEGER PRIMARY KEY as nullable, so without
			// this the primary key would be wrapped in every rebuild.
			name: "the primary key is never substituted into",
			col:  rebuildColumn{Name: "entity_id", NotNull: true, Default: "0"},
			old:  []DomainColumn{{Name: "entity_id", Nullable: true, IsPK: true}},
			want: "entity_id",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := copyExpression(tc.col, tc.old); got != tc.want {
				t.Errorf("copyExpression = %q, want %q", got, tc.want)
			}
		})
	}
}

// The scalar shape, which has its own column builder and so its own chance to
// forget the default. A hand-edited value column that accepts NULL is repaired
// the same way an object's property is.
func TestMigration_RepairsAScalarValueColumnThatShouldRefuseNulls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	s := refSchema()
	s.Components["Label"] = schema.Component{Type: schema.ComponentTypeString}
	et := s.EntityTypes["Thing"]
	et.OptionalComponents = append(et.OptionalComponents, "Label")
	s.EntityTypes["Thing"] = et

	store, err := NewSQLiteStore(path, s, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	for _, stmt := range []string{
		`DROP TABLE comp_label`,
		`CREATE TABLE comp_label (
			entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
			value TEXT)`,
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`,
		`INSERT INTO comp_label (entity_id, value) VALUES (1, NULL)`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatalf("building the hand-edited shape: %v", err)
		}
	}
	_ = store.Close()

	migrated, err := NewSQLiteStore(path, s, "")
	if err != nil {
		t.Fatalf("repairing a nullable value column: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	var notNull int
	if err := migrated.DB().QueryRow(
		`SELECT "notnull" FROM pragma_table_info('comp_label') WHERE name = 'value'`).Scan(&notNull); err != nil {
		t.Fatalf("reading the column: %v", err)
	}
	if notNull != 1 {
		t.Error("value still accepts NULL")
	}
	var value string
	if err := migrated.DB().QueryRow(
		`SELECT value FROM comp_label WHERE entity_id = 1`).Scan(&value); err != nil {
		t.Fatalf("the row did not survive: %v", err)
	}
	if value != "" {
		t.Errorf("value = %q, want the substituted empty string", value)
	}
}

// The warning is about rows that exist. A rebuild that could have substituted
// but found nothing to substitute must not report inventing data, or the notice
// stops meaning anything the one time it matters.
func TestMigration_NothingIsSaidWhenThereAreNoNullsToReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	store, err := NewSQLiteStore(path, refSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	// Every row has an owner, so retyping the property invents nothing.
	for _, stmt := range []string{
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`,
		`INSERT INTO comp_holder (entity_id, owner, hp) VALUES (1, 1, 4)`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	_ = store.Close()

	log := &recordingLogger{}
	migrated, err := NewSQLiteStoreWithConfig(path, StoreConfig{
		Schema:          retyped(schema.PropertyTypeInteger),
		MigrationPolicy: MigrationAuto,
		Logger:          log,
	})
	if err != nil {
		t.Fatalf("migrating: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	for _, w := range log.warnings {
		if strings.Contains(w, "no longer accepts NULL") {
			t.Errorf("a rebuild with no NULLs to replace reported: %s", w)
		}
	}
	// And the migration really did run, so this is not passing by doing nothing.
	var owner int
	if err := migrated.DB().QueryRow(
		`SELECT owner FROM comp_holder WHERE entity_id = 1`).Scan(&owner); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if owner != 1 {
		t.Errorf("owner = %d, want 1", owner)
	}
}

// A rebuild says why, and a nullability change is one of the reasons it can
// have. This is the sentence somebody reads before allowing a table to be
// rebuilt, and "rows that have none take a value" is the part worth knowing.
func TestGenerate_ARebuildSaysWhenNullabilityIsWhy(t *testing.T) {
	file := &schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Holder": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"hp": {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	domain := &DomainSchema{
		SchemaVersion: 1,
		Components: map[string]DomainComponent{"holder": {Columns: []DomainColumn{
			{Name: "entity_id", SQLType: "INTEGER", References: schema.EntityReference, IsPK: true},
			{Name: "hp", SQLType: "INTEGER", Nullable: true},
		}}},
		EntityTypeNames: map[string]bool{},
	}

	stmts := NewGenerator(file, domain, Config{StrictDrop: true}).
		Generate(schema.Diff(domain.ToDiffSchema(), file, nil))

	var created string
	for _, s := range stmts {
		if s.Kind == "rebuild_table" && strings.HasPrefix(s.SQL, "CREATE TABLE") {
			created = s.Description
		}
	}
	if created == "" {
		t.Fatalf("no rebuild was generated:\n%s", sqlOf(stmts))
	}
	for _, want := range []string{"hp", "stops accepting NULL"} {
		if !strings.Contains(created, want) {
			t.Errorf("the rebuild is described as %q, which does not say %q", created, want)
		}
	}
}

// The other direction, which a database built before Story 9 emits for real: a
// reference property that refuses the NULL a row without an owner needs. Its own
// sentence, because it is about what the table will accept from now on rather
// than about rows that already exist.
func TestGenerate_ARebuildSaysWhenAColumnHasToStartAcceptingNull(t *testing.T) {
	file := &schema.DatabaseSchema{
		SchemaVersion: 1,
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
			{Name: "owner", SQLType: "INTEGER", References: schema.EntityReference, Nullable: false},
		}}},
		EntityTypeNames: map[string]bool{},
	}

	stmts := NewGenerator(file, domain, Config{StrictDrop: true}).
		Generate(schema.Diff(domain.ToDiffSchema(), file, nil))

	var created string
	for _, s := range stmts {
		if s.Kind == "rebuild_table" && strings.HasPrefix(s.SQL, "CREATE TABLE") {
			created = s.Description
		}
	}
	if created == "" {
		t.Fatalf("no rebuild was generated:\n%s", sqlOf(stmts))
	}
	for _, want := range []string{"owner", "has to accept NULL"} {
		if !strings.Contains(created, want) {
			t.Errorf("the rebuild is described as %q, which does not say %q", created, want)
		}
	}
	if strings.Contains(created, "stops accepting NULL") {
		t.Errorf("the rebuild is described as %q — the other direction's sentence", created)
	}
}
