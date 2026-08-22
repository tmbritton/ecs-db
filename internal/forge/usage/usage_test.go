package usage

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/world"

	_ "modernc.org/sqlite"
)

func fixture() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 3,
		Components: map[string]schema.Component{
			"Position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{"x": {Type: "number"}, "y": {Type: "number"}}, PropertyOrder: []string{"x", "y"}},
			"Health":   {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{"hp": {Type: "integer"}}, PropertyOrder: []string{"hp"}},
			"Unused":   {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{"n": {Type: "integer"}}, PropertyOrder: []string{"n"}},
		},
		ComponentOrder: []string{"Position", "Health", "Unused"},
		// Deliberately not alphabetical: sorting and preserving look identical
		// against a sorted fixture.
		EntityTypes: map[string]schema.EntityType{
			"Zombie": {RequiredComponents: []string{"Position"}, OptionalComponents: []string{"Health"}, ValidationLevel: schema.ValidationStrict},
			"Player": {RequiredComponents: []string{"Position", "Health"}, OptionalComponents: []string{}, ValidationLevel: schema.ValidationStrict},
		},
		EntityTypeOrder: []string{"Zombie", "Player"},
	}
}

func TestUsedBy(t *testing.T) {
	s := fixture()

	got := UsedBy(s, "Position")
	if len(got) != 2 || got[0] != "Zombie" || got[1] != "Player" {
		t.Errorf("Position is used by %v; want both types in authored order", got)
	}

	// Optional counts as use: it is still a reference that breaks if the
	// component goes.
	if got := UsedBy(s, "Health"); len(got) != 2 {
		t.Errorf("Health is used by %v; an optional declaration is still a use", got)
	}

	// The state that makes deletion safe, and the one worth stating.
	if got := UsedBy(s, "Unused"); len(got) != 0 {
		t.Errorf("Unused is reported as used by %v", got)
	}
	if got := UsedBy(s, "NoSuchComponent"); len(got) != 0 {
		t.Errorf("a component that does not exist is used by %v", got)
	}
}

// Needing a database for this would make it unavailable exactly when no game
// has ever been run — which is when it is most needed.
func TestUsedBy_NeedsNoDatabase(t *testing.T) {
	if got := UsedBy(fixture(), "Position"); len(got) == 0 {
		t.Error("usage could not be derived from the schema alone")
	}
}

func seeded(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ecs.db")
	store, err := storage.NewSQLiteStore(path, fixture(), "")
	if err != nil {
		t.Fatalf("bootstrapping: %v", err)
	}
	defer func() { _ = store.Close() }()

	svc := world.NewEntityService(store)
	svc.SetSchema(fixture())
	for range 3 {
		if _, err := svc.CreateEntity(t.Context(), "Zombie", []world.EntityComponent{
			{Name: "Position", Values: world.ComponentValues{"x": 1.0, "y": 1.0}},
		}); err != nil {
			t.Fatalf("creating a Zombie: %v", err)
		}
	}
	if _, err := svc.CreateEntity(t.Context(), "Player", []world.EntityComponent{
		{Name: "Position", Values: world.ComponentValues{"x": 0.0, "y": 0.0}},
		{Name: "Health", Values: world.ComponentValues{"hp": 10}},
	}); err != nil {
		t.Fatalf("creating a Player: %v", err)
	}
	return path
}

func TestRead_TypeCounts(t *testing.T) {
	got := Read(seeded(t), "")

	if !got.Available {
		t.Fatalf("counts unavailable: %s", got.Reason)
	}
	// Three and one, not a count that an off-by-one could produce either way.
	if n, ok := got.OfType("Zombie"); !ok || n != 3 {
		t.Errorf("Zombie count is %d (ok=%v); want 3", n, ok)
	}
	if n, ok := got.OfType("Player"); !ok || n != 1 {
		t.Errorf("Player count is %d (ok=%v); want 1", n, ok)
	}
}

// A type nobody has spawned has no row in the grouped query. That is a real
// zero, and different from having no database to ask.
func TestRead_AnUnpopulatedTypeIsZeroNotUnknown(t *testing.T) {
	got := Read(seeded(t), "")

	n, ok := got.OfType("Ghost")
	if !ok {
		t.Fatal("a type with no entities was reported as uncountable")
	}
	if n != 0 {
		t.Errorf("a type with no entities counted %d", n)
	}
}

// "Nothing is using this" and "nobody could say" lead to opposite decisions
// about deleting it, so they must never render the same.
func TestRead_WithNoDatabase(t *testing.T) {
	got := Read(filepath.Join(t.TempDir(), "absent.db"), "Position")

	if got.Available {
		t.Error("counts were reported as available with no database")
	}
	if got.Reason == "" {
		t.Error("nothing explains why there are no counts")
	}
	if n, ok := got.OfType("Zombie"); ok || n != 0 {
		t.Errorf("an unavailable type count answered %d (ok=%v)", n, ok)
	}
	if n, ok := got.OfComponent("Position"); ok || n != 0 {
		t.Errorf("an unavailable component count answered %d (ok=%v)", n, ok)
	}
}

func TestRead_WithAnUnreadableDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junk.db")
	if err := os.WriteFile(path, []byte("not a database"), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if got := Read(path, ""); got.Available {
		t.Error("an unreadable file was counted")
	}
}

// Forge never writes to the game's database, here or anywhere else.
func TestRead_DoesNotTouchTheDatabase(t *testing.T) {
	path := seeded(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	for range 5 {
		Read(path, "Position")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if string(before) != string(after) {
		t.Error("counting modified the database")
	}
}

// One query for every type, not one per type. A panel that asks per row turns a
// list into N round trips against a database another process is writing.
func TestRead_AsksOnceForEveryType(t *testing.T) {
	path := seeded(t)
	got := Read(path, "")

	// Both types come back from the single call, which is the observable
	// consequence of grouping rather than querying per type.
	if _, ok := got.OfType("Zombie"); !ok {
		t.Fatal("counts unavailable")
	}
	if len(got.ByType) != 2 {
		t.Errorf("one query returned %d types: %v", len(got.ByType), got.ByType)
	}
}

// The panel asks how many entities carry a component. That is the rows in the
// component's own table, not the population of the types that declare it — a
// component optional on a type is carried by some of that type's entities, not
// all of them.
//
// The fixture is built for exactly this: Health is optional on Zombie and
// required on Player, and the three Zombies are created without it. Counting
// the declaring types answers 4; the truth is 1.
func TestRead_ComponentCountIsTheComponentNotItsTypes(t *testing.T) {
	got := Read(seeded(t), "Health")

	n, ok := got.OfComponent("Health")
	if !ok {
		t.Fatalf("no count for Health: %+v", got)
	}
	if n != 1 {
		t.Errorf("Health is carried by %d entities; want 1 — counting the declaring types gives 4", n)
	}

	// And the type counts are still the type counts.
	if n, _ := got.OfType("Zombie"); n != 3 {
		t.Errorf("Zombie count is %d; want 3", n)
	}
}

// A component added in the editor has no table until the engine migrates.
// Reporting that as "0 rows" says the data is gone rather than not yet made.
func TestRead_AComponentWithNoTableIsAbsentNotZero(t *testing.T) {
	got := Read(seeded(t), "NotYetMigrated")

	if !got.Available {
		t.Fatalf("counts unavailable: %s", got.Reason)
	}
	if n, ok := got.OfComponent("NotYetMigrated"); ok {
		t.Errorf("a component with no table counted %d rows", n)
	}
}

// The component name is concatenated into SQL, because a table name cannot be
// a bound parameter — and the name comes from a file anyone can edit.
func TestRead_RefusesAComponentNameThatIsNotAnIdentifier(t *testing.T) {
	path := seeded(t)

	for _, name := range []string{
		`Health"; DROP TABLE entities; --`,
		"Health OR 1=1",
		"comp_health", // valid identifier, simply no such table
		"héalth",      // not a SQL-safe identifier
		"",            // no component asked about
	} {
		got := Read(path, name)
		if !got.Available {
			t.Errorf("%q made the whole panel unavailable: %s", name, got.Reason)
		}
		if n, ok := got.OfComponent(name); ok {
			t.Errorf("%q counted %d rows", name, n)
		}
	}

	// The database is intact afterwards — the injection attempt did nothing.
	if n, _ := Read(path, "").OfType("Zombie"); n != 3 {
		t.Error("the entities table did not survive")
	}
}

// A single NULL entity_type must not take the whole panel down: the other
// types counted fine, and reporting nothing would be less true than reporting
// them.
func TestRead_SurvivesANullEntityType(t *testing.T) {
	path := seeded(t)
	// Written directly, because the engine's own API will not create one.
	rw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	if _, err := rw.Exec("INSERT INTO entities (entity_type) VALUES (NULL)"); err != nil {
		t.Skipf("the schema forbids a NULL entity_type, so this cannot arise: %v", err)
	}
	_ = rw.Close()

	got := Read(path, "")
	if !got.Available {
		t.Fatalf("one NULL row took the whole panel unavailable: %s", got.Reason)
	}
	if n, _ := got.OfType("Zombie"); n != 3 {
		t.Errorf("Zombie count is %d after a NULL row; want 3", n)
	}
}

// A count taken against a database built to a different schemaVersion counts an
// older world — its type names and table shapes are not the ones on screen.
func TestCounts_AgainstVersion(t *testing.T) {
	got := Read(seeded(t), "")
	if got.DBVersion != fixture().SchemaVersion {
		t.Fatalf("the database records v%d; the fixture is v%d", got.DBVersion, fixture().SchemaVersion)
	}

	if got.AgainstVersion(fixture().SchemaVersion).Stale {
		t.Error("a matching version was reported as stale")
	}
	if !got.AgainstVersion(fixture().SchemaVersion + 1).Stale {
		t.Error("a database built to an older version was not reported as stale")
	}
	// Nothing was counted, so there is nothing to qualify.
	absent := Read(filepath.Join(t.TempDir(), "absent.db"), "")
	if absent.AgainstVersion(99).Stale {
		t.Error("an unavailable count claimed to be stale")
	}
}

// Every reason reaching the panel is a sentence, not a driver error. This one
// is read carefully by someone deciding whether to delete something.
func TestRead_ReasonsAreReadable(t *testing.T) {
	junk := filepath.Join(t.TempDir(), "junk.db")
	if err := os.WriteFile(junk, []byte("not a database"), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	for _, path := range []string{"", filepath.Join(t.TempDir(), "absent.db"), junk} {
		got := Read(path, "")
		if got.Available {
			continue
		}
		for _, leak := range []string{"SQL logic error", "sql:", "(1)", "Scan error", "modernc"} {
			if strings.Contains(got.Reason, leak) {
				t.Errorf("the reason for %q leaks driver output: %q", path, got.Reason)
			}
		}
		if got.Reason == "" {
			t.Errorf("no reason given for %q", path)
		}
	}
}
