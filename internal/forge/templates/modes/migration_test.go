package modes

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/migration"
	"github.com/tmbritton/ecs-db/internal/storage"
)

// A plan in the order the generator produced it: drops before rebuilds before
// creates. Sorting it for display would be reordering the migration.
func previewFixture() migration.Preview {
	return migration.Preview{
		Available:   true,
		DBVersion:   3,
		FileVersion: 4,
		Statements: []storage.Statement{
			{
				Kind: "drop_table", SQL: "DROP TABLE IF EXISTS comp_health", Destructive: true,
				Component: "health", Description: "Drop component table comp_health",
			},
			{
				Kind: "rebuild_table", SQL: "DROP TABLE comp_position", Destructive: true,
				Component: "position", Description: "Drop old table comp_position",
			},
			{
				Kind: "alter_add_column", SQL: "ALTER TABLE comp_sprite ADD COLUMN frame INTEGER NOT NULL DEFAULT 0",
				Component: "sprite", Description: `Add column "frame" to comp_sprite`,
			},
		},
	}
}

func withPreview(p migration.Preview) Data {
	data := modeFixture()
	data.Migration = p
	return data
}

func TestMigrationPanel_RendersStatementsInGeneratorOrder(t *testing.T) {
	got := renderMode(t, withPreview(previewFixture()))

	health := strings.Index(got, "DROP TABLE IF EXISTS comp_health")
	position := strings.Index(got, "DROP TABLE comp_position")
	sprite := strings.Index(got, "ALTER TABLE comp_sprite")
	for name, at := range map[string]int{"comp_health": health, "comp_position": position, "comp_sprite": sprite} {
		if at < 0 {
			t.Fatalf("the panel does not show the statement for %s", name)
		}
	}
	if health >= position || position >= sprite {
		t.Errorf("statements are not in the order the generator produced them: %d/%d/%d",
			health, position, sprite)
	}
}

// Destructive statements are visually distinct, and the distinction is in the
// markup rather than only in a colour — a red border is not readable by a test
// and not readable by everyone.
func TestMigrationPanel_MarksDestructiveStatements(t *testing.T) {
	got := renderMode(t, withPreview(previewFixture()))

	if n := strings.Count(got, `data-destructive="true"`); n != 2 {
		t.Errorf("marked %d statements destructive, want 2", n)
	}
	if n := strings.Count(got, `data-destructive="false"`); n != 1 {
		t.Errorf("marked %d statements safe, want 1", n)
	}
	if !strings.Contains(got, `data-testid="destructive-badge"`) {
		t.Error("nothing labels a destructive statement in words")
	}
	// Counted, not searched for as a literal: the first attempt at this
	// embedded a newline and the template's own indentation, so the needle
	// could never match and the branch was dead. A badge on every statement
	// passed the whole suite.
	if n := strings.Count(got, `data-testid="destructive-badge"`); n != 2 {
		t.Errorf("rendered %d destructive badges for 2 destructive statements", n)
	}
}

// "Cannot be checked" and "nothing will happen" are different facts, and the
// second one would be a lie.
func TestMigrationPanel_UnavailableSaysWhy(t *testing.T) {
	got := renderMode(t, withPreview(migration.Preview{
		Reason: "no database yet — the engine will create one on first run",
	}))

	if !strings.Contains(got, "no database yet") {
		t.Error("the panel does not say why the change could not be checked")
	}
	if strings.Contains(got, `data-testid="migration-none"`) {
		t.Error("an unavailable preview rendered as nothing pending")
	}
	if strings.Contains(got, `data-testid="migration-statement"`) {
		t.Error("an unavailable preview rendered statements")
	}
}

func TestMigrationPanel_NothingPendingIsItsOwnState(t *testing.T) {
	got := renderMode(t, withPreview(migration.Preview{Available: true, DBVersion: 3, FileVersion: 3}))

	if !strings.Contains(got, `data-testid="migration-none"`) {
		t.Error("a database already in shape does not say so")
	}
	if strings.Contains(got, `data-testid="migration-unavailable"`) {
		t.Error("a checked preview rendered as unavailable")
	}
	if strings.Contains(got, `data-testid="migration-inert"`) {
		t.Error("the version warning showed with nothing pending to warn about")
	}
}

// The engine migrates only when schemaVersion changes. Statements it will
// never run are a correct list and a misleading answer.
func TestMigrationPanel_WarnsWhenTheEngineWillNotAct(t *testing.T) {
	p := previewFixture()
	p.FileVersion = p.DBVersion
	got := renderMode(t, withPreview(p))

	if !strings.Contains(got, `data-testid="migration-inert"`) {
		t.Fatal("no warning that the engine will skip these changes")
	}
	if !strings.Contains(got, "schemaVersion") {
		t.Error("the warning does not name what has to change")
	}

	bumped := renderMode(t, withPreview(previewFixture()))
	if strings.Contains(bumped, `data-testid="migration-inert"`) {
		t.Error("the warning showed for a change the engine will run")
	}
}

func TestMigrationPanel_ReportsAPreExistingDisagreement(t *testing.T) {
	p := previewFixture()
	p.SnapshotVersion = 5 // the saved file was already ahead of the database
	got := renderMode(t, withPreview(p))

	if !strings.Contains(got, `data-testid="migration-stale"`) {
		t.Fatal("a database behind the saved file is not reported")
	}
	if strings.Contains(renderMode(t, withPreview(previewFixture())), `data-testid="migration-stale"`) {
		t.Error("a database in step with the saved file was reported as stale")
	}
}

func TestMigrationPanel_ShowsWhatCannotBeGenerated(t *testing.T) {
	p := previewFixture()
	p.Problems = []string{"ERROR: unknown property z on comp_position"}
	got := renderMode(t, withPreview(p))

	if !strings.Contains(got, `data-testid="migration-problems"`) {
		t.Fatal("a change the generator could not render is not shown")
	}
	if !strings.Contains(got, "unknown property z") {
		t.Error("the problem does not say what it was")
	}
}
