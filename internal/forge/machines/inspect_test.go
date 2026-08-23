package machines_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/machines"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// A machine whose context key matches no component field. The engine refuses
// it, which is what makes its manifest uncomputable.
const strayContext = `{
  "id": "stray",
  "initial": "idle",
  "context": {
    "nosuchfield": 1
  },
  "states": {
    "idle": {}
  }
}
`

func inspect(t *testing.T, s *machines.Session, path string) machines.Inspection {
	t.Helper()
	got, err := s.Inspect(path)
	if err != nil {
		t.Fatalf("Inspect(%s): %v", path, err)
	}
	return got
}

func TestInspect_MapsEveryContextKeyToItsComponent(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	got := inspect(t, s, pathOf(t, s, "wander"))

	if !got.Computed {
		t.Fatalf("a machine the engine accepts has no manifest: %v", got.Errors)
	}
	if got.Manifest["hp"] != "Health" {
		t.Errorf("hp maps to %q, want Health (manifest %v)", got.Manifest["hp"], got.Manifest)
	}
}

// The distinction the whole story exists for: a machine that seeds nothing has
// a manifest that is empty, and a machine that does not validate has no
// manifest at all. Collapsing the two makes the panel lie about the second.
func TestInspect_SeedingNothingIsNotTheSameAsNotComputed(t *testing.T) {
	s, core := open(t, map[string]string{"chase.json": chase, "stray.json": strayContext})

	seedsNothing := inspect(t, s, pathOf(t, s, "chase"))
	if !seedsNothing.Computed {
		t.Fatalf("a valid machine with no context should still be computed: %v", seedsNothing.Errors)
	}
	if len(seedsNothing.Manifest) != 0 {
		t.Errorf("a machine with no context seeds something: %v", seedsNothing.Manifest)
	}

	// The stray never resolves — its context key matches nothing — so it is
	// reached by path rather than through the resolved set.
	broken, err := s.Inspect(filepath.Join(core, "stray.json"))
	if err == nil {
		t.Fatalf("a machine that does not load is not open, so Inspect should refuse: %+v", broken)
	}
}

// The same distinction, for a machine that *is* open — one broken by editing
// rather than one that never loaded.
func TestInspect_AnEditedIntoInvalidMachineHasNoManifest(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")

	if err := s.Edit(path, func(d *agent.MachineDefinition) error {
		// A target no state answers to.
		d.States["idle"].On["GO"][0].Target = "nowhere"
		return nil
	}); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	got := inspect(t, s, path)
	if got.Computed {
		t.Fatalf("a machine with an undefined target validates: %v", got.Manifest)
	}
	if len(got.Errors) == 0 {
		t.Fatal("not computed, and no reason given")
	}
	if got.Manifest != nil {
		t.Errorf("a manifest was reported for a machine that does not validate: %v", got.Manifest)
	}
	var named bool
	for _, e := range got.Errors {
		if strings.Contains(e.Message, "nowhere") {
			named = true
		}
	}
	if !named {
		t.Errorf("no error names the broken target: %v", got.Errors)
	}
}

// Every error, not the first: the panel lists them, and a first-failure report
// sends someone round the edit loop once per mistake.
func TestInspect_ReportsEveryProblem(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")

	if err := s.Edit(path, func(d *agent.MachineDefinition) error {
		d.States["idle"].On["GO"][0].Target = "nowhere"
		d.Context["alsomissing"] = 1
		return nil
	}); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	got := inspect(t, s, path)
	if len(got.Errors) < 2 {
		t.Fatalf("want both problems, got %v", got.Errors)
	}
}

// The invariant that makes the "stale component" state unreachable in AGENTS,
// which is why the panel has no marker for it. ENTS needs one because its
// manifest was computed against the schema as it stood at startup; this one is
// computed against the schema being edited, every render.
func TestInspect_AComputedManifestNamesOnlyLiveComponents(t *testing.T) {
	s, current := openWithSchema(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")

	// The invariant is about a manifest that exists, so it is checked while one
	// does. An earlier version of this test only broke the schema first, which
	// left the manifest nil and the loop below running zero times — it asserted
	// its own precondition and nothing else.
	got := inspect(t, s, path)
	if !got.Computed || len(got.Manifest) == 0 {
		t.Fatalf("the fixture does not produce a manifest to check: %+v", got)
	}
	checked := 0
	for key, comp := range got.Manifest {
		if _, ok := current.Components[comp]; !ok {
			t.Errorf("manifest maps %q to %q, which the schema does not declare", key, comp)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("nothing was checked")
	}

	// And when Health goes away underneath the session — exactly what renaming
	// it in SCHEMA mode does — the answer is "not computed" rather than a
	// manifest still naming it.
	delete(current.Components, "Health")
	current.ComponentOrder = nil

	after := inspect(t, s, path)
	if after.Computed {
		t.Fatalf("hp maps to a component that no longer exists, and the machine still validates: %v", after.Manifest)
	}
	if after.Manifest != nil {
		t.Errorf("a manifest survived the component it names: %v", after.Manifest)
	}
}

// What a caller outside the package can see: inspecting changes nothing about
// the machine and does not make the session dirty.
//
// It deliberately does *not* claim to pin the clone inside Inspect. Every route
// out of this package goes through EmitMachine, which does not serialise
// ContextManifest, so validating the session's own value in place would pass
// every assertion here — which is what an earlier version of this test did.
// inspect_internal_test.go reaches the one place the difference exists.
func TestInspect_ChangesNothingAndDirtiesNothing(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")

	before, err := s.Working(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Inspect(path); err != nil {
		t.Fatal(err)
	}
	after, err := s.Working(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeRaw, err := agent.EmitMachine(before)
	if err != nil {
		t.Fatal(err)
	}
	afterRaw, err := agent.EmitMachine(after)
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeRaw) != string(afterRaw) {
		t.Errorf("inspecting changed the machine:\n%s\nvs\n%s", beforeRaw, afterRaw)
	}
	// And it stays clean: a panel that renders cannot make the footer say there
	// is work to save.
	if dirty, err := s.Dirty(); err != nil || len(dirty) != 0 {
		t.Errorf("inspecting made the session dirty: %v %v", dirty, err)
	}
}

func TestInspect_RefusesAMachineItDoesNotHold(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	if _, err := s.Inspect("/nowhere/wander.json"); err == nil {
		t.Fatal("inspecting a machine the session does not hold was allowed")
	}
}

func TestHeld_ReachesAMachineStrandedByAReResolve(t *testing.T) {
	s, core := open(t, map[string]string{"wander.json": wander}, "extra")
	path := filepath.Join(core, "wander.json")
	if err := s.Edit(path, func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
		t.Fatal(err)
	}
	shadow(t, core, "extra", "wander.json", wander)
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}

	if contains(s.Paths(), path) {
		t.Fatalf("the stranded file is still in the resolved set: %v", s.Paths())
	}
	if !contains(s.Held(), path) {
		t.Fatalf("the stranded file is not reachable at all: held=%v", s.Held())
	}
	if !contains(s.Stranded(), path) {
		t.Errorf("the stranded file is not reported as stranded: %v", s.Stranded())
	}
}

func TestDiscard_LetsAStrandedMachineGo(t *testing.T) {
	s, core := open(t, map[string]string{"wander.json": wander}, "extra")
	path := filepath.Join(core, "wander.json")
	if err := s.Edit(path, func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
		t.Fatal(err)
	}
	shadow(t, core, "extra", "wander.json", wander)
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(path); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	if contains(s.Held(), path) {
		t.Errorf("a discarded stray is still held: %v", s.Held())
	}
	if contains(s.Stranded(), path) {
		t.Errorf("a discarded stray is still reported as stranded: %v", s.Stranded())
	}
	for _, p := range s.Problems() {
		if p.Path == path && strings.Contains(p.Err.Error(), "unsaved changes") {
			t.Errorf("the stranded problem outlived the work it was about: %v", p)
		}
	}
}

// DiscardAll is the footer's control, and the footer counts the resolved set.
// Reaching past it into a file that is not on screen is how a bulk control
// destroys something nobody was looking at.
func TestDiscardAll_LeavesAStrandedMachineAlone(t *testing.T) {
	s, core := open(t, map[string]string{"wander.json": wander}, "extra")
	path := filepath.Join(core, "wander.json")
	if err := s.Edit(path, func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
		t.Fatal(err)
	}
	shadow(t, core, "extra", "wander.json", wander)
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardAll(); err != nil {
		t.Fatalf("DiscardAll: %v", err)
	}

	var initial string
	if err := s.Read(path, func(d *agent.MachineDefinition) { initial = d.Initial }); err != nil {
		t.Fatalf("the stranded work was dropped: %v", err)
	}
	if initial != "moving" {
		t.Errorf("the stranded edit was discarded by a control that does not list it: initial=%q", initial)
	}
}

func TestFreeID_ProposesSomethingNothingHasClaimed(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})

	if got := s.FreeID("NewMachine"); got != "NewMachine" {
		t.Errorf("nothing claims NewMachine, want it back unchanged, got %q", got)
	}
	if got := s.FreeID("wander"); got == "wander" {
		t.Error("proposed an id an existing machine already holds")
	}
}

// The check Create makes reads every file, loaded or not. A proposal that only
// consulted the resolved set would offer a name Create then refuses — which is
// the create button that does nothing, twice in a row.
func TestFreeID_SeesAFileThatDoesNotLoad(t *testing.T) {
	s, core := open(t, map[string]string{"wander.json": wander})
	broken := `{"id": "NewMachine", "states": {"idle": {"invoke": {}}}}`
	if err := os.WriteFile(filepath.Join(core, "NewMachine.json"), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}

	if got := s.FreeID("NewMachine"); got == "NewMachine" {
		t.Error("proposed an id a file on disk already declares, which Create refuses")
	}
}

// An unsaved rename has claimed its new name as surely as a file has: saving it
// later would collide with anything created under it in the meantime.
func TestFreeID_SeesAnUnsavedRename(t *testing.T) {
	s, _ := open(t, map[string]string{"wander.json": wander})
	path := pathOf(t, s, "wander")
	if err := s.RenameID(path, "NewMachine"); err != nil {
		t.Fatal(err)
	}
	if got := s.FreeID("NewMachine"); got == "NewMachine" {
		t.Error("proposed an id an unsaved rename has already taken")
	}
}

// shadow writes a machine into a later mod's directory, so the earlier mod's
// file of the same id stops being the one that resolves.
func shadow(t *testing.T, core, mod, name, body string) {
	t.Helper()
	dir := filepath.Join(filepath.Dir(filepath.Dir(core)), mod, "behaviors")
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A machine can become invalid without being edited: schema.json is what its
// context keys are checked against, and SCHEMA can take the field away while
// AGENTS is open. That machine is still not something Save writes, so it must
// not take the button away — the footer's job is to predict the save it offers,
// not to grade every file in the project.
//
// This is also the only shape that can tell "the dirty set" from "everything
// held" apart: the session refuses to open a machine that is invalid on disk,
// so a held machine is valid until either it or the schema changes.
func TestInvalid_IgnoresAMachineOnlyTheSchemaBroke(t *testing.T) {
	s, current := openWithSchema(t, map[string]string{"wander.json": wander})
	path := machinePathNamed(t, s, "wander.json")
	// Saved once so the file is byte-for-byte what the emitter writes: the
	// machine is then neither dirty nor reformat-only, which is the only state
	// in which "the dirty set" and "everything held" give different answers.
	if result := s.SaveOne(path); result.Err != nil {
		t.Fatalf("SaveOne: %v", result.Err)
	}
	if got := s.Invalid(); len(got) != 0 {
		t.Fatalf("a valid project reports %v", got)
	}
	// Take away the field the machine's context key matched.
	*current = schema.DatabaseSchema{SchemaVersion: current.SchemaVersion}

	if got := s.Invalid(); len(got) != 0 {
		t.Errorf("a machine nobody edited blocks a save that would not write it: %v", got)
	}
	// And it is genuinely invalid now, so the fixture is testing what it says.
	inspection, err := s.Inspect(path)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(inspection.Errors) == 0 {
		t.Fatal("the schema change did not break the machine")
	}
}

// A reformat-only machine is dirty and is not written, so it cannot make the
// save fail either — the same rule Save itself applies.
func TestInvalid_IgnoresAReformatOnlyMachine(t *testing.T) {
	// Authored on one line: its content matches the file and its layout does not.
	compact := `{"id": "wander", "initial": "idle", "context": {"hp": 0}, "states": {"idle": {}}}`
	s, current := openWithSchema(t, map[string]string{"wander.json": compact})
	path := machinePathNamed(t, s, "wander.json")

	dirty, err := s.Dirty()
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 1 || dirty[0] != path {
		t.Fatalf("the fixture is not reformat-dirty: %v", dirty)
	}
	*current = schema.DatabaseSchema{SchemaVersion: current.SchemaVersion}

	if got := s.Invalid(); len(got) != 0 {
		t.Errorf("a machine Save skips blocks the save: %v", got)
	}
}

// machinePathNamed is the held path ending in one filename.
func machinePathNamed(t *testing.T, s *machines.Session, name string) string {
	t.Helper()
	for _, p := range s.Held() {
		if filepath.Base(p) == name {
			return p
		}
	}
	t.Fatalf("no held machine called %s in %v", name, s.Held())
	return ""
}

// Invalid is what the AGENTS footer blocks on, and it is the set a Save would
// actually attempt: dirty machines, minus the ones whose only difference from
// their file is layout. A machine nobody edited is not written, so it cannot
// make the save fail — and validating every held machine on every render of a
// two-second stream would be a walk of every state tree in the project.
func TestInvalid_CountsOnlyWhatASaveWouldWrite(t *testing.T) {
	s, dir := open(t, map[string]string{
		"nested.json": nestedMachine,
		// Written the way Forge writes it, so it is not "reformat only" — which
		// Invalid skips, and which would make this fixture agree with a version
		// that skipped nothing.
		"other.json": `{
  "id": "other",
  "initial": "a",
  "states": {
    "a": {
      "entry": [
        "noSuchAction"
      ]
    }
  }
}
`,
	})
	path := machinePath(t, dir)

	// other.json is invalid on disk and nobody has touched it: not a reason the
	// save the footer offers would be refused.
	if got := s.Invalid(); len(got) != 0 {
		t.Errorf("a machine nobody edited blocks the save: %v", got)
	}

	// Break the one being edited.
	if err := s.AddAction(path, "idle", "entry", "dealDamage"); err != nil {
		t.Fatalf("AddAction: %v", err)
	}
	if err := s.SetTransitionTarget(path, ref("idle", "on", "POKE", 0), "combat.fleeing"); err != nil {
		t.Fatalf("SetTransitionTarget: %v", err)
	}
	if got := s.Invalid(); len(got) != 0 {
		t.Errorf("a valid edit blocks the save: %v", got)
	}

	if err := s.RenameState(path, "combat.fleeing", "gone"); err != nil {
		t.Fatalf("RenameState: %v", err)
	}
	// Renaming carries its targets, so that alone is still valid. Point one at
	// nothing by hand.
	if err := s.Edit(path, func(def *agent.MachineDefinition) error {
		def.States["idle"].On["POKE"][0].Target = "nowhere"
		return nil
	}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	got := s.Invalid()
	if got[path] == 0 {
		t.Fatalf("a machine the engine would refuse does not block the save: %v", got)
	}
	if len(got) != 1 {
		t.Errorf("Invalid names %d machines, want only the edited one: %v", len(got), got)
	}
}
