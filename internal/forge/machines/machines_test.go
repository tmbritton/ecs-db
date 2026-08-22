package machines_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/machines"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/schema"
)

const wander = `{
  "id": "wander",
  "initial": "idle",
  "context": {
    "hp": 0
  },
  "states": {
    "idle": {
      "on": {
        "GO": [{ "target": "moving" }]
      }
    },
    "moving": {}
  }
}
`

const chase = `{
  "id": "chase",
  "initial": "seeking",
  "states": {
    "seeking": {}
  }
}
`

func testSchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Health": {
				Type:          schema.ComponentTypeObject,
				Properties:    map[string]schema.Property{"hp": {Type: schema.PropertyTypeInteger}},
				PropertyOrder: []string{"hp"},
			},
		},
		ComponentOrder: []string{"Health"},
		EntityTypes: map[string]schema.EntityType{
			"Goblin": {Behavior: "wander", ValidationLevel: schema.ValidationStrict},
			"Rock":   {ValidationLevel: schema.ValidationStrict},
		},
		EntityTypeOrder: []string{"Goblin", "Rock"},
	}
}

// open builds a project with one mod holding the two machines above.
func open(t *testing.T, files map[string]string, extraMods ...string) (*machines.Session, string) {
	t.Helper()
	root := t.TempDir()
	core := filepath.Join(root, "core", "behaviors")
	if err := os.MkdirAll(core, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(core, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mods := []project.Mod{{Name: "core", Behaviors: core}}
	for _, name := range extraMods {
		dir := filepath.Join(root, name, "behaviors")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, project.Mod{Name: name, Behaviors: dir})
	}

	current := testSchema()
	s, err := machines.Open(machines.Config{
		Mods:   mods,
		Schema: func() schema.DatabaseSchema { return current },
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, core
}

func pathOf(t *testing.T, s *machines.Session, id string) string {
	t.Helper()
	for _, m := range s.Machines() {
		if m.ID == id {
			return m.Path
		}
	}
	t.Fatalf("no machine %q among %v", id, ids(s))
	return ""
}

func ids(s *machines.Session) []string {
	var out []string
	for _, m := range s.Machines() {
		out = append(out, m.ID)
	}
	return out
}

func TestOpen_ResolvesEveryMachine(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander, "chase.json": chase})
	got := ids(s)
	if len(got) != 2 {
		t.Fatalf("want 2 machines, got %v (problems: %v)", got, s.Problems())
	}
	if dirty, err := s.Dirty(); err != nil || len(dirty) != 0 {
		t.Errorf("a freshly opened session is not clean: %v %v", dirty, err)
	}
}

func TestEdit_MarksOnlyThatMachineDirty(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander, "chase.json": chase})
	path := pathOf(t, s, "wander")

	if err := s.Edit(path, func(d *agent.MachineDefinition) error {
		d.Initial = "moving"
		return nil
	}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	dirty, err := s.Dirty()
	if err != nil {
		t.Fatalf("Dirty: %v", err)
	}
	if len(dirty) != 1 || dirty[0] != path {
		t.Errorf("want only %s dirty, got %v", path, dirty)
	}
}

// Read must not hand out a pointer into the session.
func TestRead_CannotReachTheSession(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")

	if err := s.Read(path, func(d *agent.MachineDefinition) {
		d.Initial = "tampered"
		d.States["idle"].Entry = []agent.ActionSpec{{Type: "log"}}
	}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if dirty, _ := s.Dirty(); len(dirty) != 0 {
		t.Fatalf("writing through a Read changed the session: %v", dirty)
	}
	var initial string
	_ = s.Read(path, func(d *agent.MachineDefinition) { initial = d.Initial })
	if initial != "idle" {
		t.Errorf("the session kept the tampered value: %q", initial)
	}
}

// The story's own criterion, and the reason it names mtime as well as bytes:
// Story 1 made the emitter byte-stable, so a save that rewrote every file would
// produce identical bytes and a bytes-only assertion would pass.
func TestSave_WritesOnlyTheDirtyFile(t *testing.T) {
	s, dir := open(t, map[string]string{"wander.json": wander, "chase.json": chase})
	other := filepath.Join(dir, "chase.json")

	before, err := os.Stat(other)
	if err != nil {
		t.Fatal(err)
	}
	// Coarse filesystem timestamps would make an immediate rewrite look
	// untouched, so put a gap either side of the save.
	time.Sleep(20 * time.Millisecond)

	path := pathOf(t, s, "wander")
	if err := s.Edit(path, func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
		t.Fatal(err)
	}
	results, err := s.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(results) != 1 || !results[0].Saved {
		t.Fatalf("want one saved machine, got %+v", results)
	}

	after, err := os.Stat(other)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("an untouched machine was rewritten: %v → %v", before.ModTime(), after.ModTime())
	}
	if body, _ := os.ReadFile(path); !strings.Contains(string(body), `"initial": "moving"`) {
		t.Errorf("the edit did not reach the file:\n%s", body)
	}
}

// The decision this story had to make: one half-built machine must not hold
// finished work hostage.
func TestSave_RefusesOneMachineWithoutBlockingTheOthers(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander, "chase.json": chase})
	good, bad := pathOf(t, s, "wander"), pathOf(t, s, "chase")

	if err := s.Edit(good, func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
		t.Fatal(err)
	}
	// A transition to a state that does not exist: exactly the half-built state
	// a canvas is in between picking a source and picking a target.
	if err := s.Edit(bad, func(d *agent.MachineDefinition) error {
		d.States["seeking"].On = map[string][]agent.Transition{"GO": {{Target: "nowhere"}}}
		d.States["seeking"].OnOrder = []string{"GO"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	results, err := s.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	byPath := map[string]machines.SaveResult{}
	for _, r := range results {
		byPath[r.Path] = r
	}
	if !byPath[good].Saved {
		t.Errorf("a valid machine was not saved: %v", byPath[good].Err)
	}
	if byPath[bad].Saved {
		t.Error("a machine with a transition to a state that does not exist was written")
	}
	if byPath[bad].Err == nil || !strings.Contains(byPath[bad].Err.Error(), "nowhere") {
		t.Errorf("the refusal does not say what is wrong: %v", byPath[bad].Err)
	}
	// And the refusal did not touch the file.
	if body, _ := os.ReadFile(bad); strings.Contains(string(body), "nowhere") {
		t.Errorf("the refused machine was written anyway:\n%s", body)
	}
}

func TestDiscard_RestoresWithoutTouchingDisk(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")
	before, _ := os.ReadFile(path)

	if err := s.Edit(path, func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(path); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if dirty, _ := s.Dirty(); len(dirty) != 0 {
		t.Errorf("still dirty after discarding: %v", dirty)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("Discard wrote to disk")
	}
}

// Two tabs, one file. Epic 11 built the conflict path for schema.json; it has
// to still work when the file is a machine, because the way out of a conflict
// is the only thing standing between the user and losing their work.
func TestSave_RefusesAFileThatChangedUnderneath(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")

	if err := s.Edit(path, func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
		t.Fatal(err)
	}
	// Somebody else writes the file — another tab, or an editor.
	theirs := strings.Replace(wander, `"initial": "idle"`, `"initial": "moving"`, 1)
	theirs = strings.Replace(theirs, `"moving": {}`, `"moving": {}, "resting": {}`, 1)
	if err := os.WriteFile(path, []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}

	results, err := s.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(results) != 1 || results[0].Saved {
		t.Fatalf("the save went through over someone else's write: %+v", results)
	}
	if !strings.Contains(results[0].Err.Error(), "changed on disk") {
		t.Errorf("the refusal does not say why: %v", results[0].Err)
	}
	// And it did not write: the whole point of refusing.
	if body, _ := os.ReadFile(path); !strings.Contains(string(body), "resting") {
		t.Errorf("their version was overwritten anyway:\n%s", body)
	}
}

func TestConflict_KeepMineAndTakeTheirs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		resolve func(*testing.T, *machines.Session, string)
		want    string
		unwant  string
	}{
		{
			name: "keep mine",
			resolve: func(t *testing.T, s *machines.Session, path string) {
				if err := s.SaveOverwriting(path); err != nil {
					t.Fatalf("SaveOverwriting: %v", err)
				}
			},
			want: `"initial": "moving"`, unwant: "resting",
		},
		{
			name: "take theirs",
			resolve: func(t *testing.T, s *machines.Session, path string) {
				if err := s.ReloadOne(path); err != nil {
					t.Fatalf("ReloadOne: %v", err)
				}
			},
			want: "resting", unwant: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := open(t, map[string]string{"wander.json": wander})
			path := pathOf(t, s, "wander")
			if err := s.Edit(path, func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
				t.Fatal(err)
			}
			theirs := strings.Replace(wander, `"moving": {}`, `"moving": {},
    "resting": {}`, 1)
			if err := os.WriteFile(path, []byte(theirs), 0o600); err != nil {
				t.Fatal(err)
			}

			tc.resolve(t, s, path)

			body, _ := os.ReadFile(path)
			if !strings.Contains(string(body), tc.want) {
				t.Errorf("want %q in the file:\n%s", tc.want, body)
			}
			if tc.unwant != "" && strings.Contains(string(body), tc.unwant) {
				t.Errorf("did not want %q in the file:\n%s", tc.unwant, body)
			}
			// Either way the conflict is over: nothing is left unsaved.
			if dirty, _ := s.Dirty(); len(dirty) != 0 {
				t.Errorf("still in conflict: %v", dirty)
			}
		})
	}
}

// The directory changed underneath Forge — a machine added, removed or renamed
// by something that is not this editor. Discard cannot recover from that: it
// restores each file's own snapshot, which is what Forge last read.
func TestReloadAll_TakesWhatIsOnDiskIncludingWhatIsGone(t *testing.T) {
	s, dir := open(t, map[string]string{"wander.json": wander, "chase.json": chase})
	if err := os.Remove(filepath.Join(dir, "chase.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "patrol.json"), []byte(
		`{"id":"patrol","initial":"a","states":{"a":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := s.ReloadAll(); err != nil {
		t.Fatalf("ReloadAll: %v", err)
	}
	got := ids(s)
	if len(got) != 2 || !contains(got, "wander") || !contains(got, "patrol") {
		t.Errorf("want wander and patrol, got %v (problems: %v)", got, s.Problems())
	}
	// patrol.json was written compactly, and the emitter has one canonical
	// style — so it differs from its own file the instant Forge opens it, with
	// nobody having edited anything. It counts as dirty, and it has to say why.
	changes, err := s.Changes()
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	for _, c := range changes {
		if filepath.Base(c.Path) == "patrol.json" && !c.Reformatting {
			t.Errorf("a file nobody edited is reported as an edit: %+v", c)
		}
		if filepath.Base(c.Path) == "wander.json" {
			t.Errorf("an untouched canonical file is dirty at all: %+v", c)
		}
	}
}

// The distinction has to survive an actual edit: a machine that was going to be
// reformatted *and* has been changed is changed, not reformatted.
func TestChanges_TellReformattingApartFromEditing(t *testing.T) {
	s, dir := open(t, map[string]string{"wander.json": wander})
	compact := filepath.Join(dir, "patrol.json")
	if err := os.WriteFile(compact, []byte(
		`{"id":"patrol","initial":"a","states":{"a":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}

	byPath := func() map[string]bool {
		t.Helper()
		changes, err := s.Changes()
		if err != nil {
			t.Fatalf("Changes: %v", err)
		}
		out := map[string]bool{}
		for _, c := range changes {
			out[c.Path] = c.Reformatting
		}
		return out
	}
	if got := byPath(); len(got) != 1 || !got[compact] {
		t.Fatalf("want patrol reported as reformatting only, got %v", got)
	}

	if err := s.Edit(compact, func(d *agent.MachineDefinition) error {
		d.States["b"] = &agent.StateNode{ID: "patrol.b"}
		d.StateOrder = append(d.StateOrder, "b")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := byPath(); got[compact] {
		t.Errorf("an edited machine is still reported as merely reformatting: %v", got)
	}
}

// The race the review found, kept as a test.
//
// agent.ValidateMachine writes ContextManifest onto whatever it validates, and
// Save validates the session's live working value. Any caller holding a pointer
// into the session is therefore reading a struct that a save is writing — which
// is exactly what Machines() used to hand out, and what the SSE loop
// dereferences on every tick of every open tab. One tab is enough; no
// adversarial timing is needed.
func TestMachines_IsSafeToReadWhileSaving(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander, "chase.json": chase})
	path := pathOf(t, s, "wander")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			for _, m := range s.Machines() {
				if m.Definition != nil {
					_ = len(m.Definition.ContextManifest)
					_ = m.Definition.Initial
				}
			}
		}
	}()
	for range 50 {
		_ = s.Edit(path, func(d *agent.MachineDefinition) error {
			d.Initial = "moving"
			return nil
		})
		_, _ = s.Save()
		_ = s.Edit(path, func(d *agent.MachineDefinition) error {
			d.Initial = "idle"
			return nil
		})
		_, _ = s.Save()
	}
	<-done
}

// The manifest is what ENTS renders context seeds from, and only
// ValidateMachine populates it — a parse-derived definition has none. Handing
// one out made every seed on every entity type read "no longer in the schema".
func TestMachines_KeepTheValidatedContextManifest(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	found := false
	for _, m := range s.Machines() {
		if m.ID != "wander" {
			continue
		}
		found = true
		if got := m.Definition.ContextManifest["hp"]; got != "Health" {
			t.Errorf("want hp seeded from Health, got %q — the panel that reads this\n"+
				"reports an empty manifest as a component that no longer exists", got)
		}
	}
	if !found {
		t.Fatal("wander did not resolve")
	}
}

// AC 5, against the input that actually exercises it: a machine authored in
// another whitespace style is dirty on open, and saving something else must not
// rewrite it.
func TestSave_LeavesAFileNobodyEditedAlone(t *testing.T) {
	s, dir := open(t, map[string]string{"wander.json": wander})
	compact := filepath.Join(dir, "patrol.json")
	if err := os.WriteFile(compact, []byte(
		`{"id":"patrol","initial":"a","states":{"a":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(compact)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)

	if err := s.Edit(pathOf(t, s, "wander"), func(d *agent.MachineDefinition) error {
		d.Initial = "moving"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	results, err := s.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	for _, r := range results {
		if r.Path == compact {
			t.Errorf("a file nobody edited was rewritten by a save of something else: %+v", r)
		}
	}
	after, _ := os.Stat(compact)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("its mtime moved: %v → %v", before.ModTime(), after.ModTime())
	}
	// SaveOne still reformats it, for a caller that asks deliberately.
	if r := s.SaveOne(compact); !r.Saved {
		t.Errorf("an explicit save of it was refused: %v", r.Err)
	}
}

// A machine can leave the resolved set while Forge holds unsaved work on it —
// shadowed by a later mod, or its file broken outside the editor. Dropping the
// open file would discard that work with no message at all.
func TestReload_KeepsUnsavedWorkOnAMachineThatLeftTheSet(t *testing.T) {
	s, core := open(t, map[string]string{"wander.json": wander}, "extra")
	path := filepath.Join(core, "wander.json")
	if err := s.Edit(path, func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
		t.Fatal(err)
	}

	// A later mod supplies its own wander, which wins — so the core file is no
	// longer the machine, and its path leaves the resolved set.
	extra := filepath.Join(filepath.Dir(filepath.Dir(core)), "extra", "behaviors")
	if err := os.WriteFile(filepath.Join(extra, "wander.json"), []byte(wander), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	dirty, err := s.Dirty()
	if err != nil {
		t.Fatalf("Dirty: %v", err)
	}
	if len(dirty) != 0 {
		t.Errorf("the shadowed file is still listed as ordinary unsaved work: %v", dirty)
	}
	// It is not in the list, and it is not lost either: the edit is still there
	// and the reason it vanished is reported.
	var reported bool
	for _, p := range s.Problems() {
		if p.Path == path && strings.Contains(p.Err.Error(), "unsaved changes") {
			reported = true
		}
	}
	if !reported {
		t.Errorf("nothing says the edit is stranded: %v", s.Problems())
	}
	var initial string
	if err := s.Read(path, func(d *agent.MachineDefinition) { initial = d.Initial }); err != nil {
		t.Fatalf("the working value was discarded: %v", err)
	}
	if initial != "moving" {
		t.Errorf("the edit was lost: initial=%q", initial)
	}
}
