package storage

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tmbritton/ecs-db/internal/schema"
	_ "modernc.org/sqlite"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// openMigrationTestDB opens an in-memory SQLite connection with required pragmas.
func openMigrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(p); err != nil {
			t.Fatalf("pragma: %v", err)
		}
	}
	return db
}

// bootstrapMigrationDB initialises a fresh DB via bootstrapDatabase.
func bootstrapMigrationDB(t *testing.T, db *sql.DB, s schema.DatabaseSchema) {
	t.Helper()
	if err := bootstrapDatabase(db, s, ""); err != nil {
		t.Fatalf("bootstrapDatabase: %v", err)
	}
}

// readMetaValue reads a single value from the meta table.
func readMetaValue(t *testing.T, db *sql.DB, key string) string {
	t.Helper()
	var v string
	if err := db.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&v); err != nil {
		t.Fatalf("reading meta %q: %v", key, err)
	}
	return v
}

// tableExists returns true if a table with the given name is in sqlite_master.
func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	_ = db.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", name,
	).Scan(&n)
	return n > 0
}

// columnExists returns true if `table` has a column named `col`.
func columnExists(t *testing.T, db *sql.DB, table, col string) bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk interface{}
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			continue
		}
		if strings.EqualFold(name, col) {
			return true
		}
	}
	return false
}

// ── SchemaMigrationError ──────────────────────────────────────────────────────

func TestSchemaMigrationError_ErrorFormat(t *testing.T) {
	inner := errors.New("table already exists")
	e := &SchemaMigrationError{
		Change:       "position",
		ChangeKind:   "create_table",
		SQL:          "CREATE TABLE comp_position (...)",
		Underlying:   inner,
		StatementIdx: 0,
		TotalStmts:   6,
	}
	msg := e.Error()
	for _, want := range []string{
		"create_table",
		"position",
		"1/6",
		"CREATE TABLE comp_position",
		"table already exists",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q:\n%s", want, msg)
		}
	}
}

func TestSchemaMigrationError_Unwrap(t *testing.T) {
	inner := errors.New("driver error")
	e := &SchemaMigrationError{Underlying: inner}
	if !errors.Is(e, inner) {
		t.Error("errors.Is should reach underlying via Unwrap")
	}
}

// ── MigrationRequiresConfirmation ─────────────────────────────────────────────

func TestMigrationRequiresConfirmation_ErrorListsDestructive(t *testing.T) {
	e := &MigrationRequiresConfirmation{
		DestructiveStatements: []Statement{
			{Kind: "drop_table", Description: "Drop component table comp_old"},
			{Kind: "rebuild_table", Description: "Rebuild comp_position"},
		},
	}
	msg := e.Error()
	if !strings.Contains(msg, "2 destructive") {
		t.Errorf("message missing count: %s", msg)
	}
	if !strings.Contains(msg, "drop_table") {
		t.Errorf("message missing drop_table: %s", msg)
	}
	if !strings.Contains(msg, "rebuild_table") {
		t.Errorf("message missing rebuild_table: %s", msg)
	}
}

func TestMigrationRequiresConfirmation_IsError(t *testing.T) {
	var e error = &MigrationRequiresConfirmation{}
	if e.Error() == "" {
		t.Error("empty error message")
	}
}

// ── MigrationLogger / nopLogger ──────────────────────────────────────────────

func TestNopLogger_DoesNotPanic(t *testing.T) {
	l := NopLogger()
	l.Infof("test %d", 1)
	l.Warnf("warn %s", "ok")
}

func TestNewMigrationRunner_NilLoggerDefaultsToNop(t *testing.T) {
	db := openMigrationTestDB(t)
	r := NewMigrationRunner(db, schema.DatabaseSchema{}, MigrationAuto, nil)
	if r.logger == nil {
		t.Error("logger should not be nil")
	}
	// Should not panic
	r.logger.Infof("hello")
}

// ── Run: introspection failure ────────────────────────────────────────────────

func TestMigrate_IntrospectFails_ReturnsError(t *testing.T) {
	// An empty DB (no meta table) causes IntrospectAll → ReadSchemaVersion to fail.
	db := openMigrationTestDB(t)

	runner := NewMigrationRunner(db, schema.DatabaseSchema{SchemaVersion: 1}, MigrationAuto, NopLogger())
	err := runner.Run()

	if err == nil {
		t.Fatal("expected error when meta table is absent, got nil")
	}
	if !strings.Contains(err.Error(), "introspecting db") {
		t.Errorf("expected error to mention introspection, got: %v", err)
	}
}

// ── Run: no-op cases ──────────────────────────────────────────────────────────

func TestMigrate_NoChanges_ReturnsNil(t *testing.T) {
	db := openMigrationTestDB(t)
	s := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s)

	runner := NewMigrationRunner(db, s, MigrationAuto, NopLogger())
	if err := runner.Run(); err != nil {
		t.Errorf("Run() = %v, want nil (no structural changes, same version)", err)
	}
}

func TestMigrate_EntityTypeOnlyChanges_NoDDLButNoError(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeNumber},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	// v2: adds an entity type (no component DDL)
	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components:    s1.Components,
		EntityTypes: map[string]schema.EntityType{
			"Player": {RequiredComponents: []string{"position"}},
		},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	if err := runner.Run(); err != nil {
		t.Errorf("Run() = %v, want nil (entity-type-only changes)", err)
	}

	if got := readMetaValue(t, db, "schema_version"); got != "2" {
		t.Errorf("schema_version = %q, want 2 after entity-type-only migration", got)
	}
}

// ── Run: happy path ───────────────────────────────────────────────────────────

func TestMigrate_AddComponent_Success(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"velocity": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"vx": {Type: schema.PropertyTypeNumber},
				"vy": {Type: schema.PropertyTypeNumber},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	if err := runner.Run(); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	if !tableExists(t, db, "comp_velocity") {
		t.Error("comp_velocity table not created after migration")
	}
}

func TestMigrate_AddProperty_Success(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeNumber},
				"y": {Type: schema.PropertyTypeNumber},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeNumber},
				"y": {Type: schema.PropertyTypeNumber},
				"z": {Type: schema.PropertyTypeNumber},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	if err := runner.Run(); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	if !columnExists(t, db, "comp_position", "z") {
		t.Error("column z not added to comp_position")
	}
}

func TestMigrate_MixedChanges_Success(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"health": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"hp": {Type: schema.PropertyTypeInteger},
			}},
			"old": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	// v2: adds a component, adds a property, removes a component
	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"health": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"hp":    {Type: schema.PropertyTypeInteger},
				"maxhp": {Type: schema.PropertyTypeInteger},
			}},
			"name": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	if err := runner.Run(); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	if !tableExists(t, db, "comp_name") {
		t.Error("comp_name not created")
	}
	if tableExists(t, db, "comp_old") {
		t.Error("comp_old not dropped")
	}
	if !columnExists(t, db, "comp_health", "maxhp") {
		t.Error("maxhp column not added to comp_health")
	}
}

// ── Run: meta updates ─────────────────────────────────────────────────────────

func TestMigrate_UpdatesMetaVersion(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"tag": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	if err := runner.Run(); err != nil {
		t.Fatalf("Run() = %v", err)
	}

	if got := readMetaValue(t, db, "schema_version"); got != "2" {
		t.Errorf("schema_version = %q, want 2", got)
	}
}

func TestMigrate_UpdatesMetaBuildTime(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	before := time.Now().UTC().Truncate(time.Second)

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"tag": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	if err := runner.Run(); err != nil {
		t.Fatalf("Run() = %v", err)
	}

	got := readMetaValue(t, db, "build_time")
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("build_time %q is not RFC3339: %v", got, err)
	}
	if parsed.Before(before) {
		t.Errorf("build_time %v is before test start %v", parsed, before)
	}
}

func TestMigrate_PureVersionBump_UpdatesMetaVersion(t *testing.T) {
	// Version bump with no structural changes should still update meta.
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeNumber},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	// v2 has the exact same structure, just a higher version number
	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components:    s1.Components,
		EntityTypes:   map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	if err := runner.Run(); err != nil {
		t.Fatalf("Run() = %v", err)
	}

	if got := readMetaValue(t, db, "schema_version"); got != "2" {
		t.Errorf("schema_version = %q, want 2 (pure version bump should still update meta)", got)
	}
}

// ── Run: meta update failure ──────────────────────────────────────────────────

func TestMigrate_MetaSchemaVersionUpdateFails_ReturnsError(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	// Install a trigger that raises an error on any UPDATE to meta.
	if _, err := db.Exec(`
		CREATE TRIGGER fail_meta_update
		BEFORE UPDATE ON meta
		BEGIN
			SELECT RAISE(FAIL, 'intentional test failure on meta update');
		END`); err != nil {
		t.Fatalf("creating trigger: %v", err)
	}

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"tag": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	err := runner.Run()

	if err == nil {
		t.Fatal("expected error from meta update failure, got nil")
	}
	if !strings.Contains(err.Error(), "updating meta") {
		t.Errorf("expected error to mention meta update, got: %v", err)
	}
}

func TestMigrate_MetaBuildTimeUpdateFails_ReturnsError(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	// Trigger that only blocks the build_time update, letting schema_version through.
	if _, err := db.Exec(`
		CREATE TRIGGER fail_build_time_update
		BEFORE UPDATE ON meta
		WHEN NEW.key = 'build_time'
		BEGIN
			SELECT RAISE(FAIL, 'intentional build_time failure');
		END`); err != nil {
		t.Fatalf("creating trigger: %v", err)
	}

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"tag": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	err := runner.Run()

	if err == nil {
		t.Fatal("expected error from build_time update failure, got nil")
	}
	if !strings.Contains(err.Error(), "updating meta") {
		t.Errorf("expected error to mention meta update, got: %v", err)
	}
}

// ── Run: rollback on failure ──────────────────────────────────────────────────

func TestMigrate_FailedStatement_Rollback(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeNumber},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	// Pre-create the temp table that the rebuild sequence would create, so the
	// second rebuild statement (CREATE TABLE comp_position_new) fails.
	if _, err := db.Exec("CREATE TABLE comp_position_new (entity_id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("poisoning db: %v", err)
	}

	// v2 changes x from REAL to INTEGER, which triggers a table rebuild.
	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	err := runner.Run()

	if err == nil {
		t.Fatal("Run() = nil, want error")
	}
	var migErr *SchemaMigrationError
	if !errors.As(err, &migErr) {
		t.Fatalf("expected *SchemaMigrationError, got %T: %v", err, err)
	}
}

func TestMigrate_FailedStatement_ErrorIncludesDetails(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeNumber},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)
	if _, err := db.Exec("CREATE TABLE comp_position_new (entity_id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("poisoning db: %v", err)
	}

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	err := runner.Run()

	var migErr *SchemaMigrationError
	if !errors.As(err, &migErr) {
		t.Fatalf("expected *SchemaMigrationError, got %T", err)
	}
	if migErr.Change == "" {
		t.Error("Change should not be empty")
	}
	if migErr.ChangeKind == "" {
		t.Error("ChangeKind should not be empty")
	}
	if migErr.SQL == "" {
		t.Error("SQL should not be empty")
	}
	if migErr.Underlying == nil {
		t.Error("Underlying should not be nil")
	}
	if migErr.TotalStmts <= 0 {
		t.Error("TotalStmts should be positive")
	}
}

func TestMigrate_MetaNotUpdated_OnFailure(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeNumber},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)
	if _, err := db.Exec("CREATE TABLE comp_position_new (entity_id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("poisoning db: %v", err)
	}

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	_ = runner.Run()

	// schema_version must remain at 1 (rolled back)
	if got := readMetaValue(t, db, "schema_version"); got != "1" {
		t.Errorf("schema_version = %q, want 1 (should have rolled back)", got)
	}
}

// ── Run: MigrationPolicy = confirm ───────────────────────────────────────────

func TestMigrate_ConfirmPolicy_DestructiveBlocks(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"old": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	// v2 removes the component → DROP TABLE (destructive)
	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationConfirm, NopLogger())
	err := runner.Run()

	if err == nil {
		t.Fatal("Run() = nil, want *MigrationRequiresConfirmation")
	}
	var conf *MigrationRequiresConfirmation
	if !errors.As(err, &conf) {
		t.Fatalf("expected *MigrationRequiresConfirmation, got %T: %v", err, err)
	}
	if len(conf.DestructiveStatements) == 0 {
		t.Error("DestructiveStatements should not be empty")
	}
}

func TestMigrate_ConfirmPolicy_NonDestructiveProceeds(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	// v2 only adds a new component — no destructive statements
	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"tag": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationConfirm, NopLogger())
	if err := runner.Run(); err != nil {
		t.Errorf("Run() = %v, want nil (no destructive changes)", err)
	}
}

func TestMigrate_ConfirmPolicy_EmptyDestructiveList_ReturnsNil(t *testing.T) {
	db := openMigrationTestDB(t)
	s := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s)

	// Same schema — no changes at all
	runner := NewMigrationRunner(db, s, MigrationConfirm, NopLogger())
	if err := runner.Run(); err != nil {
		t.Errorf("Run() = %v, want nil (no changes)", err)
	}
}

func TestMigrationRequiresConfirmation_ContainsDropStatement(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"droppable": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationConfirm, NopLogger())
	err := runner.Run()

	var conf *MigrationRequiresConfirmation
	if !errors.As(err, &conf) {
		t.Fatalf("expected *MigrationRequiresConfirmation, got %T", err)
	}
	found := false
	for _, s := range conf.DestructiveStatements {
		if s.Kind == "drop_table" {
			found = true
		}
	}
	if !found {
		t.Error("DestructiveStatements should include a drop_table statement")
	}
}

// ── Run: MigrationPolicy = auto ───────────────────────────────────────────────

func TestMigrate_AutoPolicy_DestructiveProceeds(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"old": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	// v2 removes the component — DROP TABLE is destructive but auto proceeds
	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	if err := runner.Run(); err != nil {
		t.Fatalf("Run() = %v, want nil (auto policy should proceed)", err)
	}

	if tableExists(t, db, "comp_old") {
		t.Error("comp_old should have been dropped")
	}
}

func TestMigrate_DefaultPolicyIsAuto(t *testing.T) {
	// Zero-value MigrationPolicy should run destructive changes (behave as auto).
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"old": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}

	var policy MigrationPolicy // zero value
	runner := NewMigrationRunner(db, s2, policy, NopLogger())
	if err := runner.Run(); err != nil {
		t.Fatalf("Run() = %v, want nil (zero-value policy should proceed with destructive changes)", err)
	}
	if tableExists(t, db, "comp_old") {
		t.Error("comp_old should have been dropped by zero-value policy (auto behavior)")
	}
}

func TestMigrate_EntityPreservedAfterRebuild(t *testing.T) {
	db := openMigrationTestDB(t)
	s1 := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeNumber},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
	bootstrapMigrationDB(t, db, s1)

	res, err := db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Player', 0)")
	if err != nil {
		t.Fatalf("inserting entity: %v", err)
	}
	entityID, _ := res.LastInsertId()
	if _, err := db.Exec("INSERT INTO comp_position (entity_id, x) VALUES (?, ?)", entityID, 1.5); err != nil {
		t.Fatalf("inserting comp_position: %v", err)
	}

	// v2 changes x from REAL to INTEGER — triggers a table rebuild.
	s2 := schema.DatabaseSchema{
		SchemaVersion: 2,
		Components: map[string]schema.Component{
			"position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{},
	}

	runner := NewMigrationRunner(db, s2, MigrationAuto, NopLogger())
	if err := runner.Run(); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	var gotID int64
	if err := db.QueryRow("SELECT entity_id FROM comp_position WHERE entity_id = ?", entityID).Scan(&gotID); err != nil {
		t.Fatalf("entity not found in rebuilt comp_position: %v", err)
	}
	if gotID != entityID {
		t.Errorf("entity_id = %d, want %d after rebuild", gotID, entityID)
	}
}
