package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"

	_ "modernc.org/sqlite"
)

// built is the schema the database was created with; edited is what the editor
// currently holds. The preview is the difference between them.
func built() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 3,
		Components: map[string]schema.Component{
			"Position": {
				Type:          schema.ComponentTypeObject,
				Properties:    map[string]schema.Property{"x": {Type: "number"}, "y": {Type: "number"}},
				PropertyOrder: []string{"x", "y"},
			},
			"Health": {
				Type:          schema.ComponentTypeObject,
				Properties:    map[string]schema.Property{"hp": {Type: "integer"}},
				PropertyOrder: []string{"hp"},
			},
		},
		ComponentOrder: []string{"Position", "Health"},
		EntityTypes: map[string]schema.EntityType{
			"Player": {RequiredComponents: []string{"Position"}, ValidationLevel: schema.ValidationStrict},
		},
		EntityTypeOrder: []string{"Player"},
	}
}

func property(t string) schema.Property { return schema.Property{Type: t} }

func bootstrap(t *testing.T, s schema.DatabaseSchema) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ecs.db")
	store, err := storage.NewSQLiteStore(path, s, "")
	if err != nil {
		t.Fatalf("bootstrapping: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
	return path
}

func TestCheck_AdditiveChange(t *testing.T) {
	db := bootstrap(t, built())

	edited := built()
	comp := edited.Components["Position"]
	comp.Properties["z"] = schema.Property{Type: "number"}
	comp.PropertyOrder = append(comp.PropertyOrder, "z")
	edited.Components["Position"] = comp

	got := Check(db, edited, built())

	if !got.Available {
		t.Fatalf("preview unavailable: %s", got.Reason)
	}
	// The statement itself, not merely that there was one: a generator fed
	// the wrong schema still returns something.
	if len(got.Statements) != 1 {
		t.Fatalf("adding one column produced %d statements: %+v", len(got.Statements), got.Statements)
	}
	sql := got.Statements[0].SQL
	if !strings.Contains(sql, "comp_position") || !strings.Contains(sql, "z") {
		t.Errorf("the statement does not add z to comp_position: %s", sql)
	}
	if got.Statements[0].Kind != "alter_add_column" {
		t.Errorf("adding a column was generated as %q: %s", got.Statements[0].Kind, sql)
	}
	if len(got.Problems) != 0 {
		t.Errorf("a valid addition reported problems: %v", got.Problems)
	}
	if got.Destructive() {
		t.Errorf("adding a column was reported as destructive: %+v", got.Statements)
	}
}

// Dropping a column takes everything in it. This is the case the whole story
// exists for.
func TestCheck_DestructiveChange(t *testing.T) {
	db := bootstrap(t, built())

	edited := built()
	comp := edited.Components["Position"]
	delete(comp.Properties, "y")
	comp.PropertyOrder = []string{"x"}
	edited.Components["Position"] = comp

	got := Check(db, edited, built())

	if !got.Available {
		t.Fatalf("preview unavailable: %s", got.Reason)
	}
	if !got.Destructive() {
		t.Fatalf("dropping a column was not reported as destructive: %+v", got.Statements)
	}
	// The statements themselves, not a count: "3 destructive changes" tells
	// someone nothing about whether to proceed.
	var named bool
	for _, st := range got.DestructiveStatements() {
		if strings.Contains(strings.ToLower(st.SQL+st.Description), "position") {
			named = true
		}
	}
	if !named {
		t.Errorf("nothing names what will be lost: %+v", got.DestructiveStatements())
	}
}

func TestCheck_DroppingAComponentIsDestructive(t *testing.T) {
	db := bootstrap(t, built())

	edited := built()
	delete(edited.Components, "Health")
	delete(edited.EntityTypes, "Player")

	got := Check(db, edited, built())
	if !got.Destructive() {
		t.Errorf("dropping a table was not reported as destructive: %+v", got.Statements)
	}
}

// Nothing changed means nothing to warn about — and that is a different fact
// from "we could not check", which is why Available exists.
func TestCheck_NoChanges(t *testing.T) {
	db := bootstrap(t, built())

	got := Check(db, built(), built())
	if !got.Available {
		t.Fatalf("preview unavailable: %s", got.Reason)
	}
	if len(got.Statements) != 0 {
		t.Errorf("an unchanged schema produced statements: %+v", got.Statements)
	}
	if got.Destructive() {
		t.Error("an unchanged schema was reported as destructive")
	}
}

// An empty change list and "we could not look" are different facts, and the
// second one must never render as the first.
func TestCheck_WithNoDatabase(t *testing.T) {
	got := Check(filepath.Join(t.TempDir(), "absent.db"), built(), built())

	if got.Available {
		t.Error("a preview was reported as available with no database")
	}
	if got.Reason == "" {
		t.Error("nothing explains why the change could not be checked")
	}
	if len(got.Statements) != 0 {
		t.Errorf("statements were produced with no database: %+v", got.Statements)
	}
	if got.Destructive() {
		t.Error("an unavailable preview claimed a destructive change")
	}
}

func TestCheck_WithAnUnreadableDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junk.db")
	if err := os.WriteFile(path, []byte("not a database"), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	got := Check(path, built(), built())
	if got.Available {
		t.Error("an unreadable file was reported as a usable database")
	}
}

// Forge never writes to the game's database. The migration is the engine's to
// run, at its own startup; Forge only says what will happen.
func TestCheck_OpensTheDatabaseReadOnly(t *testing.T) {
	path := bootstrap(t, built())

	db, err := open(path)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec("CREATE TABLE forge_should_not_do_this (id INTEGER)"); err == nil {
		t.Fatal("a write through the preview's connection succeeded")
	}
	if _, err := db.Exec("DROP TABLE comp_position"); err == nil {
		t.Fatal("a drop through the preview's connection succeeded")
	}
}

// The preview must not modify the database it is inspecting.
func TestCheck_DoesNotTouchTheDatabase(t *testing.T) {
	path := bootstrap(t, built())
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	edited := built()
	delete(edited.Components, "Health")
	for range 5 {
		Check(path, edited, built())
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if string(before) != string(after) {
		t.Error("the preview modified the database it was inspecting")
	}
}

// A real edit is usually a mix, and the confirmation dialog must show only the
// half that loses data — a dialog that lists the safe changes too teaches the
// author to click through it.
func TestCheck_SeparatesDestructiveFromSafe(t *testing.T) {
	db := bootstrap(t, built())

	edited := built()
	comp := edited.Components["Position"]
	comp.Properties["z"] = schema.Property{Type: "number"}
	comp.PropertyOrder = append(comp.PropertyOrder, "z")
	edited.Components["Position"] = comp
	delete(edited.Components, "Health")
	delete(edited.EntityTypes, "Player")

	got := Check(db, edited, built())

	destructive := got.DestructiveStatements()
	if len(destructive) == 0 {
		t.Fatal("the dropped table was not reported as destructive")
	}
	if len(destructive) >= len(got.Statements) {
		t.Fatalf("every statement was reported as destructive; the added column is not:\n%+v", got.Statements)
	}
	for _, st := range destructive {
		if !st.Destructive {
			t.Errorf("a safe statement was listed as destructive: %+v", st)
		}
		if strings.Contains(st.SQL, "ADD COLUMN") {
			t.Errorf("adding a column was listed as destructive: %s", st.SQL)
		}
	}
}

// Cycling a component's shape — one click in the schema editor — drops the
// table and builds a new one. Nothing in the UI says so; this is what says so.
func TestCheck_ChangingAComponentShapeIsDestructive(t *testing.T) {
	db := bootstrap(t, built())

	edited := built()
	comp := edited.Components["Position"]
	comp.Type = schema.ComponentTypeEntityRef
	comp.Properties = nil
	comp.PropertyOrder = nil
	edited.Components["Position"] = comp

	got := Check(db, edited, built())

	if !got.Destructive() {
		t.Fatalf("a shape change was not reported as destructive: %+v", got.Statements)
	}
	var dropped bool
	for _, st := range got.DestructiveStatements() {
		if strings.Contains(st.SQL, "DROP TABLE") && strings.Contains(st.SQL, "comp_position") {
			dropped = true
		}
	}
	if !dropped {
		t.Errorf("nothing says the table is dropped: %+v", got.DestructiveStatements())
	}
}

// A statement with a description and no SQL cannot render as a change — it
// would be a blank row claiming to be safe. These are the migrations that
// would fail at the engine's next start, so they are reported separately.
func TestPartition_SeparatesUnrenderableStatements(t *testing.T) {
	got := partition([]storage.Statement{
		{Kind: "create_table", SQL: "CREATE TABLE comp_a (entity_id INTEGER)", Component: "a"},
		{Kind: "error", Description: "ERROR: unknown component b", Component: "b"},
		{Kind: "rebuild_table", Description: "orphaned description", Component: "c"},
	})

	for _, st := range got.Statements {
		if st.SQL == "" {
			t.Errorf("a statement with no SQL was listed as a change: %+v", st)
		}
	}
	if len(got.Statements) != 1 {
		t.Errorf("expected the one renderable statement, got %+v", got.Statements)
	}
	if len(got.Problems) != 2 {
		t.Errorf("expected both unrenderable statements as problems, got %v", got.Problems)
	}
}

// Statements arrive in the order the generator produced them. That order is
// the migration plan — drops before rebuilds before creates — not a display
// preference.
func TestCheck_PreservesStatementOrder(t *testing.T) {
	db := bootstrap(t, built())
	edited := built()
	comp := edited.Components["Position"]
	delete(comp.Properties, "y")
	comp.PropertyOrder = []string{"x"}
	edited.Components["Position"] = comp
	delete(edited.Components, "Health")
	delete(edited.EntityTypes, "Player")

	first := Check(db, edited, built())
	for range 10 {
		again := Check(db, edited, built())
		if len(again.Statements) != len(first.Statements) {
			t.Fatalf("statement count varies between calls: %d then %d",
				len(first.Statements), len(again.Statements))
		}
		for i := range first.Statements {
			if again.Statements[i].SQL != first.Statements[i].SQL {
				t.Fatalf("statement order varies between calls at %d:\n %s\n %s",
					i, first.Statements[i].SQL, again.Statements[i].SQL)
			}
		}
	}
}

// What the engine will act on, from the preview's side. TestEngine_* above
// proves the engine behaves this way; these pin that the preview reports it.
//
// There was a Preview.WillMigrate here, which existed only because the engine
// used to need a version bump. Once it stopped needing one, the method said
// "there is something to run", which is what an empty statement list already
// says — so it went, along with the two templates that branched on it.
func TestCheck_ReportsWhatTheEngineWillRun(t *testing.T) {
	db := bootstrap(t, built())

	withColumn := func(v int) schema.DatabaseSchema {
		s := built()
		s.SchemaVersion = v
		comp := s.Components["Position"]
		comp.Properties["z"] = property("number")
		comp.PropertyOrder = append(comp.PropertyOrder, "z")
		s.Components["Position"] = comp
		return s
	}

	// Statements without a version bump are listed, and the engine runs them
	// now, which it did not before Story 11 opened its version gate.
	unbumped := Check(db, withColumn(built().SchemaVersion), built())
	if len(unbumped.Statements) == 0 {
		t.Fatal("no statements to reason about")
	}

	// A database that matches has nothing pending, whatever the version says.
	if matching := Check(db, built(), built()); len(matching.Statements) != 0 {
		t.Errorf("a matching database has %d pending statement(s): %+v",
			len(matching.Statements), matching.Statements)
	}

	bumped := Check(db, withColumn(built().SchemaVersion+1), built())
	if len(bumped.Statements) != len(unbumped.Statements) {
		t.Errorf("the same edit lists %d statements bumped and %d unbumped",
			len(bumped.Statements), len(unbumped.Statements))
	}
	if bumped.DBVersion != built().SchemaVersion {
		t.Errorf("DBVersion = %d, want %d", bumped.DBVersion, built().SchemaVersion)
	}
	if bumped.FileVersion != built().SchemaVersion+1 {
		t.Errorf("FileVersion = %d, want %d", bumped.FileVersion, built().SchemaVersion+1)
	}
}

// A database built at a version the saved file no longer matches was already
// out of step before this edit — reported distinctly, as engine-status does.
func TestCheck_ReportsAPreExistingVersionDisagreement(t *testing.T) {
	db := bootstrap(t, built())

	snapshot := built()
	snapshot.SchemaVersion = built().SchemaVersion + 2 // saved file, already ahead

	if got := Check(db, snapshot, snapshot); !got.Stale() {
		t.Errorf("a database behind the saved file was not reported as stale: db=%d file=%d",
			got.DBVersion, got.SnapshotVersion)
	}
	// An unavailable preview knows nothing, and must not claim staleness.
	if got := Check(filepath.Join(t.TempDir(), "absent.db"), snapshot, snapshot); got.Stale() {
		t.Error("an unavailable preview claimed the database was stale")
	}
	// An ordinary edit against a matching database is not stale.
	if got := Check(db, built(), built()); got.Stale() {
		t.Errorf("a matching database was reported as stale: db=%d file=%d",
			got.DBVersion, got.SnapshotVersion)
	}
}

// A check that could not run and a database that is not there are different
// answers. Only one of them means a save is safe.
func TestPreview_FailedIsNotTheSameAsAbsent(t *testing.T) {
	absent := Check(filepath.Join(t.TempDir(), "absent.db"), built(), built())
	if absent.Failed {
		t.Error("an absent database was reported as a failed check")
	}
	if absent.Holds() {
		t.Error("a save was held for a database that does not exist")
	}

	junk := filepath.Join(t.TempDir(), "junk.db")
	if err := os.WriteFile(junk, []byte("not a database"), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	unreadable := Check(junk, built(), built())
	if !unreadable.Failed {
		t.Error("an unreadable database was not reported as a failed check")
	}
	if !unreadable.Holds() {
		t.Error("a save was allowed through a check that could not run")
	}
	if unreadable.Reason == "" {
		t.Error("nothing explains what went wrong")
	}
}

// Holds is the gate every save route passes through, so both halves of it have
// to be live.
func TestPreview_HoldsOnDestructiveChanges(t *testing.T) {
	db := bootstrap(t, built())

	edited := built()
	delete(edited.Components, "Health")
	delete(edited.EntityTypes, "Player")

	if got := Check(db, edited, built()); !got.Holds() {
		t.Error("a destructive change did not hold the save")
	}
	if got := Check(db, built(), built()); got.Holds() {
		t.Error("an unchanged schema held the save")
	}
}

// A snapshot that will not parse tells us nothing about staleness, and must
// not be read as a version of zero — which Stale() would take for "not stale"
// and say nothing about.
func TestPreview_UnknownSnapshotIsNotReadAsVersionZero(t *testing.T) {
	db := bootstrap(t, built())

	p := Check(db, built(), schema.DatabaseSchema{}) // snapshot failed to parse
	p.SnapshotUnknown = true
	if p.Stale() {
		t.Error("an unparseable snapshot was compared against the database anyway")
	}

	// The same zero version, this time genuinely read: still not a comparison
	// worth making, and for the same reason it must not claim staleness.
	q := Check(db, built(), schema.DatabaseSchema{})
	if q.Stale() {
		t.Error("a snapshot with no version was treated as a real disagreement")
	}
}
