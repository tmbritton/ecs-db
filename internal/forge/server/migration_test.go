package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/status"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
)

// Two fields, because deleting the last field on a component is refused for a
// different reason and would never reach the migration check.
const migrationSchema = `{
  "schemaVersion": 3,
  "components": {
    "Position": {
      "type": "object",
      "properties": {
        "x": { "type": "number" },
        "y": { "type": "number" }
      }
    }
  },
  "entityTypes": {
    "Player": {
      "requiredComponents": ["Position"],
      "optionalComponents": [],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    }
  }
}
`

// migrationServer is a session over a schema plus a real database built from
// it, so the diff is against something the engine actually made.
func migrationServer(t *testing.T) (*httptest.Server, *Server, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(path, []byte(migrationSchema), 0o600); err != nil {
		t.Fatalf("seeding schema: %v", err)
	}

	loaded, err := schema.LoadSchema([]byte(migrationSchema))
	if err != nil {
		t.Fatalf("loading the fixture schema: %v", err)
	}
	dbPath := filepath.Join(dir, "ecs.db")
	store, err := storage.NewSQLiteStore(dbPath, loaded, "")
	if err != nil {
		t.Fatalf("building the fixture database: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the fixture database: %v", err)
	}

	srv, s, _, _ := sessionServerAt(t, path, status.Config{DBPath: dbPath, SchemaPath: path})
	return srv, s, path, dbPath
}

// A save that would drop a column writes nothing and puts the decision on
// screen. This is the story's whole promise.
func TestSave_DestructiveChangeIsHeldForConfirmation(t *testing.T) {
	srv, s, path, _ := migrationServer(t)
	before := read(t, path)

	if code := post(t, srv, "/forge/schema/field?component=Position&delete=y"); code != 204 {
		t.Fatalf("deleting the field: %d", code)
	}
	if code := post(t, srv, "/forge/schema/save"); code != 204 {
		t.Fatalf("save: %d", code)
	}

	if after := read(t, path); after != before {
		t.Fatalf("the file was written before the confirmation was answered:\n%s", after)
	}

	// Observed through the mode-content render the page's stream pushes, not
	// through a fresh page load — a load starts clean by design, and asserting
	// against one would be asserting against the wrong path.
	body := streamConfirm(t, s, "/forge/schema")
	if !strings.Contains(body, `data-testid="migration-confirm"`) {
		t.Fatal("no confirmation was shown for a destructive save")
	}
	// The statements, not a count: what is being lost has to be legible.
	if !strings.Contains(body, "comp_position") {
		t.Error("the confirmation does not name the table being rebuilt")
	}
	if !strings.Contains(body, `data-testid="confirm-statement"`) {
		t.Error("the confirmation lists no statements")
	}
}

// Cancelling is a real decision: nothing is written, and the working edit
// survives so the user can undo it themselves.
func TestSaveCancel_WritesNothing(t *testing.T) {
	srv, s, path, _ := migrationServer(t)
	before := read(t, path)

	post(t, srv, "/forge/schema/field?component=Position&delete=y")
	post(t, srv, "/forge/schema/save")
	if code := post(t, srv, "/forge/schema/save/cancel"); code != 204 {
		t.Fatalf("cancel: %d", code)
	}

	if after := read(t, path); after != before {
		t.Fatalf("cancelling wrote the file:\n%s", after)
	}
	if s.isConfirming() {
		t.Error("the confirmation is still pending after cancelling")
	}
	if strings.Contains(streamConfirm(t, s, "/forge/schema"), `data-testid="migration-confirm"`) {
		t.Error("the confirmation is still on screen after cancelling")
	}
	body := streamContent(t, s)
	// The edit itself is not undone — cancelling declined the save, not the work.
	if strings.Contains(body, `data-testid="field-y"`) {
		t.Error("cancelling the save also discarded the edit")
	}
}

func TestSaveConfirm_Writes(t *testing.T) {
	srv, s, path, _ := migrationServer(t)
	before := read(t, path)

	post(t, srv, "/forge/schema/field?component=Position&delete=y")
	post(t, srv, "/forge/schema/save")
	if code := post(t, srv, "/forge/schema/save/confirm"); code != 204 {
		t.Fatalf("confirm: %d", code)
	}

	after := read(t, path)
	if after == before {
		t.Fatal("confirming did not write the file")
	}
	if strings.Contains(after, `"y"`) {
		t.Errorf("the dropped field is still in the saved file:\n%s", after)
	}
	if s.isConfirming() {
		t.Error("the confirmation is still pending after confirming")
	}
}

// An additive change is not a decision worth interrupting for. A tool that
// confirms everything teaches you to confirm without reading.
func TestSave_AdditiveChangeIsNotHeld(t *testing.T) {
	srv, s, path, _ := migrationServer(t)
	before := read(t, path)

	post(t, srv, "/forge/schema/field?component=Position&add=z")
	if code := post(t, srv, "/forge/schema/save"); code != 204 {
		t.Fatalf("save: %d", code)
	}

	after := read(t, path)
	if after == before {
		t.Fatal("an additive save was held back")
	}
	if !strings.Contains(after, `"z"`) {
		t.Errorf("the added field was not saved:\n%s", after)
	}
	if strings.Contains(streamConfirm(t, s, "/forge/schema"), `data-testid="migration-confirm"`) {
		t.Error("an additive save raised a confirmation")
	}
}

// A confirmation belongs to the tab that asked for it. A fresh page load did
// not ask, and a modal it never triggered is a modal it cannot explain.
func TestPageLoad_ClearsAPendingConfirmation(t *testing.T) {
	srv, s, _, _ := migrationServer(t)

	post(t, srv, "/forge/schema/field?component=Position&delete=y")
	post(t, srv, "/forge/schema/save")
	if !s.isConfirming() {
		t.Fatal("the save was not held")
	}

	get(t, srv, "/forge/schema")
	if s.isConfirming() {
		t.Error("a page load left the confirmation pending")
	}
}

// With no database there is nothing to compare against, and an empty change
// list would read as "nothing will happen" — a different claim, and a false one.
func TestMigrationPanel_WithNoDatabase_SaysSoRatherThanShowingNothing(t *testing.T) {
	srv, _, _, dbPath := migrationServer(t)
	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("removing the database: %v", err)
	}

	_, body := get(t, srv, "/forge/schema?component=Position")
	if !strings.Contains(body, `data-testid="migration-unavailable"`) {
		t.Fatal("the panel does not say the change could not be checked")
	}
	if strings.Contains(body, `data-testid="migration-none"`) {
		t.Error("the panel claimed nothing is pending with no database to check against")
	}
}

// Without a database, a destructive edit cannot be detected — and must not be
// saved silently as though it were checked and found safe.
func TestSave_WithNoDatabase_IsNotHeld(t *testing.T) {
	srv, _, path, dbPath := migrationServer(t)
	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("removing the database: %v", err)
	}
	before := read(t, path)

	post(t, srv, "/forge/schema/field?component=Position&delete=y")
	post(t, srv, "/forge/schema/save")

	if read(t, path) == before {
		t.Error("a save was held back by a check that could not run")
	}
}

// The panel reports what the engine will actually do. Without a version bump
// the engine skips the migration entirely, and a list of statements it will
// never run is a correct answer to the wrong question.
func TestMigrationPanel_SaysWhenTheEngineWillNotAct(t *testing.T) {
	srv, _, _, _ := migrationServer(t)

	post(t, srv, "/forge/schema/field?component=Position&add=z")

	_, body := get(t, srv, "/forge/schema?component=Position")
	if !strings.Contains(body, `data-testid="migration-statement"`) {
		t.Fatal("the panel shows no pending statement to reason about")
	}
	if !strings.Contains(body, `data-testid="migration-inert"`) {
		t.Fatal("the panel does not say the engine will skip this without a version bump")
	}

	post(t, srv, "/forge/schema/version")

	_, bumped := get(t, srv, "/forge/schema?component=Position")
	if strings.Contains(bumped, `data-testid="migration-inert"`) {
		t.Error("the panel still says the engine will skip the change after a version bump")
	}
}

// streamContent renders the mode-content region exactly as the page's SSE
// stream does, which is where every live change actually arrives.
func streamContent(t *testing.T, s *Server) string {
	t.Helper()
	m, ok := mode.Lookup("schema")
	if !ok {
		t.Fatal("no schema mode")
	}
	out, err := s.renderModeContent(m, schemaRequest())
	if err != nil {
		t.Fatalf("rendering mode content: %v", err)
	}
	return out
}

// streamConfirm renders the confirmation region the same way. The dialog is
// shell-level, not part of any mode: the save footer can be pressed from all
// six, so the answer to it has to render on all six.
func streamConfirm(t *testing.T, s *Server, path string) string {
	t.Helper()
	out, err := s.renderConfirmRegion(httptest.NewRequest("GET", path, nil))
	if err != nil {
		t.Fatalf("rendering the confirmation: %v", err)
	}
	return out
}

func schemaRequest() *http.Request {
	return httptest.NewRequest("GET", "/forge/schema?component=Position", nil)
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

// "Keep mine", the answer to a save conflict, drops exactly as many columns as
// an ordinary save. A destructive check on one of three save routes is not a
// check on the tool.
func TestSaveOverwrite_IsHeldForConfirmationToo(t *testing.T) {
	srv, s, path, _ := migrationServer(t)
	before := read(t, path)

	post(t, srv, "/forge/schema/field?component=Position&delete=y")
	if code := post(t, srv, "/forge/schema/overwrite"); code != 204 {
		t.Fatalf("overwrite: %d", code)
	}

	if after := read(t, path); after != before {
		t.Fatalf("keeping mine wrote a destructive change with no confirmation:\n%s", after)
	}
	if !strings.Contains(streamConfirm(t, s, "/forge/schema"), `data-testid="migration-confirm"`) {
		t.Error("no confirmation was shown for a destructive overwrite")
	}
}

// Confirming performs the save that was actually asked for. Save and
// SaveOverwriting differ in whether they check for a conflicting write, and
// answering "yes" to one must not silently perform the other.
func TestSaveConfirm_PerformsTheSaveThatWasHeld(t *testing.T) {
	srv, _, path, _ := migrationServer(t)

	post(t, srv, "/forge/schema/field?component=Position&delete=y")
	post(t, srv, "/forge/schema/overwrite")

	// Someone else writes the file while the question stands. An ordinary Save
	// refuses that; SaveOverwriting is the answer that was held, and takes it.
	if err := os.WriteFile(path, []byte(strings.Replace(migrationSchema, `"schemaVersion": 3`, `"schemaVersion": 4`, 1)), 0o600); err != nil {
		t.Fatalf("simulating a concurrent write: %v", err)
	}

	post(t, srv, "/forge/schema/save/confirm")

	if strings.Contains(read(t, path), `"y"`) {
		t.Error("confirming an overwrite did not overwrite; the held kind was not honoured")
	}
}

// The answer to a question nobody asked is not consent.
func TestSaveConfirm_WithNothingHeldWritesNothing(t *testing.T) {
	srv, _, path, _ := migrationServer(t)
	before := read(t, path)

	post(t, srv, "/forge/schema/field?component=Position&delete=y")
	if code := post(t, srv, "/forge/schema/save/confirm"); code != 204 {
		t.Fatalf("confirm: %d", code)
	}

	if after := read(t, path); after != before {
		t.Fatalf("a save was confirmed that had never been held:\n%s", after)
	}
}

// The dialog stays on the page until the next poll, so its buttons outlive the
// state they belong to. Cancel then Save-anyway must not save.
func TestSaveConfirm_AfterCancelWritesNothing(t *testing.T) {
	srv, _, path, _ := migrationServer(t)
	before := read(t, path)

	post(t, srv, "/forge/schema/field?component=Position&delete=y")
	post(t, srv, "/forge/schema/save")
	post(t, srv, "/forge/schema/save/cancel")
	post(t, srv, "/forge/schema/save/confirm") // the stale button

	if after := read(t, path); after != before {
		t.Fatalf("a cancelled save was completed by the stale dialog:\n%s", after)
	}
}

// Answering twice saves once. Without an atomic take, two clicks both see the
// hold and both save — and the second runs against a file the first has
// already advanced.
func TestSaveConfirm_TwiceSavesOnce(t *testing.T) {
	srv, s, _, _ := migrationServer(t)

	post(t, srv, "/forge/schema/field?component=Position&delete=y")
	post(t, srv, "/forge/schema/save")
	post(t, srv, "/forge/schema/save/confirm")
	post(t, srv, "/forge/schema/save/confirm")

	if s.isConfirming() {
		t.Error("a hold survived being answered")
	}
}

// A check that could not run is not evidence that a save is safe. An absent
// database is different — there is nothing there to destroy — which is the
// distinction Failed exists to make.
func TestSave_WhenTheCheckCannotRun_IsHeld(t *testing.T) {
	srv, s, path, dbPath := migrationServer(t)
	before := read(t, path)

	if err := os.WriteFile(dbPath, []byte("not a database at all"), 0o600); err != nil {
		t.Fatalf("corrupting the database: %v", err)
	}

	post(t, srv, "/forge/schema/field?component=Position&delete=y")
	post(t, srv, "/forge/schema/save")

	if after := read(t, path); after != before {
		t.Fatalf("a save was written while the check could not run:\n%s", after)
	}
	dialog := streamConfirm(t, s, "/forge/schema")
	if !strings.Contains(dialog, `data-testid="confirm-unchecked"`) {
		t.Errorf("the dialog does not say the change could not be checked:\n%s", dialog)
	}
}

// The save footer is in the shell, so Save can be pressed from any of the six
// modes. A held save that renders nowhere is a button that does nothing, for
// good, with no explanation.
func TestSaveConfirm_RendersOnEveryMode(t *testing.T) {
	srv, s, _, _ := migrationServer(t)

	post(t, srv, "/forge/schema/field?component=Position&delete=y")
	post(t, srv, "/forge/schema/save")

	for _, m := range mode.All {
		got := streamConfirm(t, s, m.Path())
		if !strings.Contains(got, `data-testid="migration-confirm"`) {
			t.Errorf("%s renders no confirmation for a held save", m.Slug)
		}
	}
}

// The modal claims the engine will run these statements. Without a version
// bump it will not — and that is the default path, since the whole reason
// WillMigrate exists is that authors do not think to bump it.
func TestSaveConfirm_DoesNotClaimTheEngineWillActWhenItWillNot(t *testing.T) {
	srv, s, _, _ := migrationServer(t)

	post(t, srv, "/forge/schema/field?component=Position&delete=y")
	post(t, srv, "/forge/schema/save")

	got := streamConfirm(t, s, "/forge/schema")
	if strings.Contains(got, "The next time the engine starts it will run") {
		t.Error("the dialog says the engine will run statements it will skip")
	}
	if !strings.Contains(got, "schemaVersion") {
		t.Error("the dialog does not explain why the engine will skip them")
	}

	post(t, srv, "/forge/schema/version")
	if !strings.Contains(streamConfirm(t, s, "/forge/schema"), "The next time the engine starts it will run") {
		t.Error("the dialog does not say the engine will act once the version is bumped")
	}
}

// A hold outlives the edit that caused it. Undo the deletion while the dialog
// is up and there is nothing left to confirm — an empty dialog asking whether
// to destroy nothing is a question with no answer.
func TestConfirmation_DisappearsWhenTheEditNoLongerDestroysAnything(t *testing.T) {
	srv, s, _, _ := migrationServer(t)

	post(t, srv, "/forge/schema/field?component=Position&delete=y")
	post(t, srv, "/forge/schema/save")
	if !strings.Contains(streamConfirm(t, s, "/forge/schema"), `data-testid="migration-confirm"`) {
		t.Fatal("no confirmation to begin with")
	}

	// Put the field back. The hold is still recorded; the reason for it is not.
	post(t, srv, "/forge/schema/field?component=Position&add=y")

	if got := streamConfirm(t, s, "/forge/schema"); strings.Contains(got, `data-testid="migration-confirm"`) {
		t.Errorf("the confirmation outlived the change it was asking about:\n%s", got)
	}
}
