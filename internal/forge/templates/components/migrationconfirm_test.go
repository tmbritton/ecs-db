package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/migration"
	"github.com/tmbritton/ecs-db/internal/storage"
)

// A plan in the order the generator produced it: drops before rebuilds before
// creates. Sorting it for display would be reordering the migration.
func confirmPreview() migration.Preview {
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

func renderConfirm(t *testing.T, p migration.Preview) string {
	t.Helper()
	var buf bytes.Buffer
	c := MigrationConfirm(MigrationConfirmProps{
		Preview:      p,
		CancelAction: "@post('/forge/schema/save/cancel')",
		SaveAction:   "@post('/forge/schema/save/confirm')",
	})
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// The confirmation lists what will be lost. A count would tell someone nothing
// about whether to proceed.
func TestMigrationConfirm_ListsStatementsNotACount(t *testing.T) {
	// The dialog alone, not the whole page: the panel behind it lists every
	// statement by design, so a page-wide search would be asking a different
	// question than the one this test is named for.
	got := renderConfirm(t, confirmPreview())

	if !strings.Contains(got, `data-testid="migration-confirm"`) {
		t.Fatal("no confirmation rendered")
	}
	if n := strings.Count(got, `data-testid="confirm-statement"`); n != 2 {
		t.Errorf("the confirmation lists %d statements, want the 2 destructive ones", n)
	}
	if strings.Contains(got, "ALTER TABLE comp_sprite") {
		t.Error("the confirmation lists a statement that destroys nothing")
	}
	if !strings.Contains(got, "comp_health") {
		t.Error("the confirmation does not name what will be lost")
	}
	if !strings.Contains(got, `data-testid="confirm-cancel"`) || !strings.Contains(got, `data-testid="confirm-save"`) {
		t.Error("the confirmation is not a choice between two answers")
	}
	// There is no way to stop being asked. Dropping a column is not a routine
	// confirmation to train someone out of reading.
	for _, phrase := range []string{"don't ask", "Don't ask", "do not ask", "always allow", "remember"} {
		if strings.Contains(got, phrase) {
			t.Errorf("the confirmation offers a way to skip itself: %q", phrase)
		}
	}
}

// A confirmation with nothing destructive to confirm is a dialog with no
// question in it.
func TestMigrationConfirm_OnlyWhenSomethingIsLost(t *testing.T) {
	// The server decides whether to render it at all — see
	// TestConfirmation_OnlyWhenAHoldIsInPlace in the server package. What is
	// pinned here is that the dialog reports what it is for.
	safe := migration.Preview{
		Available:   true,
		DBVersion:   3,
		FileVersion: 4,
		Statements: []storage.Statement{
			{Kind: "create_table", SQL: "CREATE TABLE comp_new (entity_id INTEGER)", Description: "Create comp_new"},
		},
	}
	if got := renderConfirm(t, safe); strings.Contains(got, "confirm-statement") {
		t.Error("the dialog listed a statement that destroys nothing")
	}
}

// The dialog announces itself. ModalShell carries this; the panel is where a
// regression would show up.
func TestMigrationConfirm_IsALabelledDialog(t *testing.T) {
	got := renderConfirm(t, confirmPreview())

	if !strings.Contains(got, `aria-modal="true"`) {
		t.Error("the confirmation is not marked as a modal dialog")
	}
	if !strings.Contains(got, `aria-label="This save destroys data"`) {
		t.Error("the confirmation has no accessible name")
	}
}
