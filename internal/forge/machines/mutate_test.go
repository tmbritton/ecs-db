package machines_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
)

func TestCreate_WritesAMachineThatValidates(t *testing.T) {
	s, dir := open(t, map[string]string{"wander.json": wander})

	path, err := s.Create("patrol", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if want := filepath.Join(dir, "patrol.json"); path != want {
		t.Errorf("want %s, got %s", want, path)
	}
	// It resolved, which means it parsed *and* validated — the loader rejects a
	// machine that does not.
	if got := ids(s); len(got) != 2 {
		t.Fatalf("the new machine did not resolve: %v (problems: %v)", got, s.Problems())
	}
	// And it is clean on arrival: a file that is dirty the moment it exists
	// would show as unsaved work nobody did.
	if dirty, err := s.Dirty(); err != nil || len(dirty) != 0 {
		t.Errorf("a new machine is already dirty: %v %v", dirty, err)
	}
}

func TestCreate_RefusesAnIdThatAlreadyExists(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	if _, err := s.Create("wander", ""); err == nil {
		t.Fatal("two files declaring one id is a project problem; Forge must not author one")
	}
}

// The case the resolved-set check could not see: a file that declares an id and
// does not load is absent from the set, so Forge would happily write a second
// file with that id — and the duplicate appeared the moment someone fixed the
// first.
func TestCreate_RefusesAnIdDeclaredByAFileThatDoesNotLoad(t *testing.T) {
	broken := `{"id":"patrol","initial":"nowhere","states":{"a":{}}}`
	s, dir := open(t, map[string]string{"wander.json": wander, "halfbuilt.json": broken})
	if got := ids(s); len(got) != 1 || got[0] != "wander" {
		t.Fatalf("the broken machine was expected not to resolve, got %v", got)
	}

	if _, err := s.Create("patrol", ""); err == nil {
		t.Fatal("Forge authored a second file declaring an id another file already has")
	}
	if _, err := os.Stat(filepath.Join(dir, "patrol.json")); !os.IsNotExist(err) {
		t.Error("the file was written despite the refusal")
	}
}

func TestCreate_RefusesToGuessTheMod(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander}, "extra")

	_, err := s.Create("patrol", "")
	if err == nil {
		t.Fatal("with two mods that could hold it, the choice is the user's")
	}
	if !strings.Contains(err.Error(), "core") || !strings.Contains(err.Error(), "extra") {
		t.Errorf("the refusal does not name the options: %v", err)
	}

	// Named, it lands there.
	path, err := s.Create("patrol", "extra")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.Contains(path, filepath.Join("extra", "behaviors")) {
		t.Errorf("the machine did not land in the named mod: %s", path)
	}
}

func TestCreate_RefusesANameThatIsNotUsableAsAFilename(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	for _, name := range []string{
		"", "../escape", "with/slash", "star*", `back\slash`, "space name",
		// Made entirely of dots: filepath.Base leaves these unchanged, so the
		// check that used to sit here waved them through.
		".", "..", ".hidden",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Create(name, ""); err == nil {
				t.Errorf("%q was accepted as a machine id", name)
			}
		})
	}
}

// The two renames are different operations with different consequences, and
// this is the one that has them.
func TestRenameID_ChangesTheIdInsideTheFileAndLeavesItWhereItIs(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")

	if err := s.RenameID(path, "roam"); err != nil {
		t.Fatalf("RenameID: %v", err)
	}
	if r := s.SaveOne(path); !r.Saved {
		t.Fatalf("saving after the rename: %v", r.Err)
	}

	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), `"id": "roam"`) {
		t.Errorf("the id did not change:\n%s", body)
	}
	// The file did not move: nothing resolves through a filename, and moving it
	// as a side effect would be editing the user's tree over a change they did
	// not ask for.
	if filepath.Base(path) != "wander.json" {
		t.Errorf("the file moved: %s", path)
	}
	if got := ids(s); len(got) != 1 || got[0] != "roam" {
		t.Errorf("the resolved set did not follow the rename: %v", got)
	}
	// A state's derived id follows too, so the rename does not add an explicit
	// "id" to every state in the file.
	if strings.Contains(string(body), `"id": "wander.idle"`) || strings.Contains(string(body), `"id": "roam.idle"`) {
		t.Errorf("the rename wrote a derived state id into the file:\n%s", body)
	}
}

func TestRenameID_RefusesAnIdAnotherMachineHas(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander, "chase.json": chase})
	if err := s.RenameID(pathOf(t, s, "wander"), "chase"); err == nil {
		t.Fatal("renaming onto another machine's id would author a duplicate")
	}
}

func TestRenameFile_MovesTheFileAndNothingElse(t *testing.T) {
	s, dir := open(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")

	moved, err := s.RenameFile(path, "wandering")
	if err != nil {
		t.Fatalf("RenameFile: %v", err)
	}
	if want := filepath.Join(dir, "wandering.json"); moved != want {
		t.Errorf("want %s, got %s", want, moved)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the old file is still there")
	}
	// The id is untouched: a filename resolves nothing, so renaming one must
	// not quietly change the thing that does.
	if got := ids(s); len(got) != 1 || got[0] != "wander" {
		t.Errorf("renaming the file changed the machine id: %v", got)
	}
}

func TestRenameFile_RefusesWhileThereIsUnsavedWork(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")
	if err := s.Edit(path, func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameFile(path, "wandering"); err == nil {
		t.Fatal("renaming would have moved the old bytes and orphaned the edit")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the file moved anyway: %v", err)
	}
}

// A delete that discards unsaved work cannot be undone from the file, so it is
// refused for the same reason a file rename is — and with more at stake.
func TestDelete_RefusesWhileThereIsUnsavedWork(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander, "chase.json": chase})
	path := pathOf(t, s, "chase")
	if err := s.Edit(path, func(d *agent.MachineDefinition) error { d.Initial = "gone"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(path); err == nil {
		t.Fatal("an unsaved edit was deleted along with its file")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the file was removed anyway: %v", err)
	}
	if dirty, _ := s.Dirty(); len(dirty) != 1 {
		t.Errorf("the edit is gone: %v", dirty)
	}
}

func TestDelete_RemovesTheFileAndTheMachine(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander, "chase.json": chase})
	path := pathOf(t, s, "chase")

	if err := s.Delete(path); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the file is still there")
	}
	if got := ids(s); len(got) != 1 || got[0] != "wander" {
		t.Errorf("want only wander left, got %v", got)
	}
}

// The refresh Epic 12 Story 7 left as a follow-up. Everything that changes the
// set has to be visible immediately, or the binding dropdowns and the "no
// machine with this id is loaded" warning go on describing the project as it
// was when Forge started.
func TestResolvedSetFollowsEveryMutation(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})

	if _, err := s.Create("patrol", ""); err != nil {
		t.Fatal(err)
	}
	if got := ids(s); len(got) != 2 {
		t.Fatalf("after create: %v", got)
	}

	path := pathOf(t, s, "patrol")
	if err := s.RenameID(path, "guard"); err != nil {
		t.Fatal(err)
	}
	if r := s.SaveOne(path); !r.Saved {
		t.Fatalf("save after rename: %v", r.Err)
	}
	if got := ids(s); !contains(got, "guard") || contains(got, "patrol") {
		t.Errorf("after renaming the id: %v", got)
	}

	if err := s.Delete(path); err != nil {
		t.Fatal(err)
	}
	if got := ids(s); len(got) != 1 || got[0] != "wander" {
		t.Errorf("after delete: %v", got)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// An edit in progress must survive a re-resolve triggered by something else.
func TestReload_KeepsWorkInProgress(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")
	if err := s.Edit(path, func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Create("patrol", ""); err != nil {
		t.Fatal(err)
	}
	dirty, err := s.Dirty()
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 1 || dirty[0] != path {
		t.Errorf("creating a machine discarded an edit to another: %v", dirty)
	}
}

// The list is an editor view, not a directory listing: an unsaved rename has to
// show, or the list names one machine while the editor beside it names another.
func TestMachines_ShowTheWorkingIdBeforeItIsSaved(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")

	if err := s.RenameID(path, "roam"); err != nil {
		t.Fatalf("RenameID: %v", err)
	}
	if got := ids(s); len(got) != 1 || got[0] != "roam" {
		t.Errorf("the list still shows the id on disk: %v", got)
	}
	// The file has not been written, which is the point.
	if body, _ := os.ReadFile(path); strings.Contains(string(body), "roam") {
		t.Errorf("the rename reached disk without a save:\n%s", body)
	}
	// And the facts about where it came from are unchanged by an unsaved edit.
	if s.Machines()[0].Path != path {
		t.Errorf("the path moved: %s", s.Machines()[0].Path)
	}
}

// The duplicate-id guard, against the case that actually needs it.
//
// A machine's id lives inside its file and need not match the filename, which
// is legal and loads fine. Creating "wander" when wander.json exists is refused
// by the file check alone — so a test that only uses that case passes with the
// id check removed, which is exactly what it did.
func TestCreate_RefusesAnIdHeldByADifferentlyNamedFile(t *testing.T) {
	s, _ := open(t, map[string]string{"alternative.json": wander})
	if got := ids(s); len(got) != 1 || got[0] != "wander" {
		t.Fatalf("the fixture did not load as expected: %v", got)
	}
	if _, err := s.Create("wander", ""); err == nil {
		t.Fatal("two files declaring one id is a project problem; Forge authored one")
	}
	if got := ids(s); len(got) != 1 {
		t.Errorf("a second machine with the same id was created: %v", got)
	}
}

// An edit that fails partway must leave the session exactly as it was, rather
// than half-applied — the property Edit's copy-then-adopt exists for.
func TestEdit_LeavesTheSessionUntouchedWhenItFails(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")

	want := fmt.Errorf("changed my mind")
	err := s.Edit(path, func(d *agent.MachineDefinition) error {
		d.Initial = "moving"
		delete(d.States, "moving")
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("want the callback's error back, got %v", err)
	}
	if dirty, _ := s.Dirty(); len(dirty) != 0 {
		t.Fatalf("a failed edit was applied anyway: %v", dirty)
	}
	var initial string
	var states int
	_ = s.Read(path, func(d *agent.MachineDefinition) { initial = d.Initial; states = len(d.States) })
	if initial != "idle" || states != 2 {
		t.Errorf("the session kept part of the failed edit: initial=%q states=%d", initial, states)
	}
}
