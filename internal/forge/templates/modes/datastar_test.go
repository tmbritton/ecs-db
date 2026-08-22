package modes

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/migration"
	"github.com/tmbritton/ecs-db/internal/forge/web/dstest"
	"github.com/tmbritton/ecs-db/internal/storage"
)

// Every control in this package is a Datastar attribute, and an attribute
// naming a plugin the bundle does not register is skipped in silence — it
// renders perfectly and does nothing. The whole editor once shipped that way.
//
// The modes package had no such guard until the migration panel added more
// attributes to it; the shell and the primitives each had one.
func TestModes_DatastarAttributesResolve(t *testing.T) {
	plugins := dstest.Plugins(t)

	data := modeFixture()
	data.Selected = "Position"
	// With counts available, so the scan reaches the panels' live branch. Left
	// at the zero value it only ever saw the "no database" path, and any
	// attribute added inside the other one would go unchecked.
	data.Counts = available()
	data.Migration = migration.Preview{
		Available:   true,
		DBVersion:   3,
		FileVersion: 4,
		Statements: []storage.Statement{
			{Kind: "drop_table", SQL: "DROP TABLE comp_health", Destructive: true, Description: "Drop comp_health"},
			{Kind: "alter_add_column", SQL: "ALTER TABLE comp_position ADD COLUMN z REAL", Description: "Add z"},
		},
		Problems: []string{"ERROR: unknown component"},
	}
	data.Confirming = true

	// Both modes. Scanning only SCHEMA left every handler in ENTS unchecked —
	// which is how the exemption for data-type came to be added for an
	// attribute no scan ever saw.
	seen := dstest.AssertAttrs(t, plugins, renderMode(t, data), renderEnts(t, entsWithSelection()))
	// Without this the scan passes on a template that emits no handlers at all.
	dstest.RequireSeen(t, seen, "on:click", "on:change", "testid", "type")
}

// The dash form parses as a plugin name rather than an event key and is
// ignored in silence. Named here because it is the specific mistake this
// project has made twice.
func TestModes_UseTheColonForm(t *testing.T) {
	data := modeFixture()
	data.Confirming = true
	data.Migration = migration.Preview{
		Available:  true,
		Statements: []storage.Statement{{Kind: "drop_table", SQL: "DROP TABLE comp_health", Destructive: true}},
	}
	rendered := renderMode(t, data) + renderEnts(t, entsWithSelection())
	for _, bad := range []string{"data-on-click", "data-on-change", "data-on-load"} {
		if strings.Contains(rendered, bad) {
			t.Errorf("%s names an unregistered plugin and is silently ignored", bad)
		}
	}
}

// entsWithSelection renders the editor rather than the empty state, so the
// scan sees every control the mode has.
func entsWithSelection() Data {
	data := entsFixture()
	data.Selected = "Goblin"
	data.Counts = available()
	return data
}
