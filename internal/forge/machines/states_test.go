package machines_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
)

// nestedMachine is what the canvas edits: a compound state with children, a
// state carrying someone else's meta, and two transitions on one event.
const nestedMachine = `{
  "id": "nested",
  "initial": "idle",
  "states": {
    "idle": {
      "meta": { "notes": "written by hand", "forge": { "x": 40, "y": 40 } },
      "on": {
        "GO": [{ "target": "combat", "cond": "inRange" }, { "target": "resting" }],
        "POKE": [{ "target": "combat.attacking" }]
      }
    },
    "combat": {
      "initial": "attacking",
      "states": { "attacking": {}, "fleeing": {} }
    },
    "resting": {}
  }
}
`

// onDisk is the file's bytes, for the tests that check the canvas never writes
// one.
func onDisk(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func machinePath(t *testing.T, dir string) string {
	t.Helper()
	return filepath.Join(dir, "nested.json")
}

// working reads back the session's working value, which is what every one of
// these has to change — the file on disk is Save's business.
func working(t *testing.T, s interface {
	Working(string) (*agent.MachineDefinition, error)
}, path string,
) *agent.MachineDefinition {
	t.Helper()
	def, err := s.Working(path)
	if err != nil {
		t.Fatalf("Working: %v", err)
	}
	return def
}

func metaOf(t *testing.T, def *agent.MachineDefinition, name string) map[string]json.RawMessage {
	t.Helper()
	node, ok := def.States[name]
	if !ok {
		t.Fatalf("no state %q", name)
	}
	raw, ok := node.Extra["meta"]
	if !ok {
		return nil
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("meta on %q is not an object: %s", name, raw)
	}
	return out
}

func TestMoveState_WritesThePositionIntoMeta(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.MoveState(path, "idle", 120, 260); err != nil {
		t.Fatalf("MoveState: %v", err)
	}

	meta := metaOf(t, working(t, s, path), "idle")
	if got := string(meta["forge"]); got != `{"x":120,"y":260}` {
		t.Errorf("forge = %s, want {\"x\":120,\"y\":260}", got)
	}
	// Merged, not replaced: a user's own keys under meta survive, which is the
	// cost Story 1 accepted when it chose meta over a sidecar.
	if got := string(meta["notes"]); got != `"written by hand"` {
		t.Errorf("someone else's meta was destroyed: notes = %s", got)
	}
}

func TestMoveState_GivesAnUnpositionedStateItsFirstPosition(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.MoveState(path, "resting", 10, 20); err != nil {
		t.Fatalf("MoveState: %v", err)
	}
	if got := string(metaOf(t, working(t, s, path), "resting")["forge"]); got != `{"x":10,"y":20}` {
		t.Errorf("forge = %s", got)
	}
}

func TestMoveState_ReachesANestedState(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.MoveState(path, "combat.attacking", 5, 6); err != nil {
		t.Fatalf("MoveState: %v", err)
	}
	def := working(t, s, path)
	raw := def.States["combat"].Children["attacking"].Extra["meta"]
	if !strings.Contains(string(raw), `"x":5`) {
		t.Errorf("the nested state was not moved: %s", raw)
	}
}

// meta that is not an object cannot be merged into. Overwriting it would
// destroy whatever someone put there, and a canvas that eats your notes because
// you nudged a box is worse than one that says it cannot.
func TestMoveState_RefusesToOverwriteAMetaItCannotMergeInto(t *testing.T) {
	const odd = `{"id":"odd","initial":"a","states":{"a":{"meta":"a note"}}}`
	s, dir := open(t, map[string]string{"odd.json": odd})
	path := filepath.Join(dir, "odd.json")

	err := s.MoveState(path, "a", 1, 2)
	if err == nil {
		t.Fatal("moving a state whose meta is not an object silently replaced it")
	}
	// The message, not merely that it failed: reading the order of a non-object
	// fails too, so "it errored" is satisfied by a version that has no idea
	// why. This is the sentence that tells someone what to do about it.
	if !strings.Contains(err.Error(), "without destroying what is there") {
		t.Errorf("the refusal does not explain what is in the way: %v", err)
	}
	if got := string(working(t, s, path).States["a"].Extra["meta"]); got != `"a note"` {
		t.Errorf("meta was changed anyway: %s", got)
	}
}

func TestAddState_NamesSomethingNothingHasTaken(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	first, err := s.AddState(path, 10, 10)
	if err != nil {
		t.Fatalf("AddState: %v", err)
	}
	second, err := s.AddState(path, 20, 20)
	if err != nil {
		t.Fatalf("AddState: %v", err)
	}
	if first == second {
		t.Fatalf("both new states are called %q", first)
	}
	def := working(t, s, path)
	for _, name := range []string{first, second} {
		if _, ok := def.States[name]; !ok {
			t.Errorf("no state %q", name)
		}
	}
	// Positioned where it was dropped, or a double-click puts every new state
	// in the same place.
	if got := string(metaOf(t, def, first)["forge"]); got != `{"x":10,"y":10}` {
		t.Errorf("the new state is not where it was added: %s", got)
	}
	// And in authored order, at the end, so the file reads as it was built.
	if def.StateOrder[len(def.StateOrder)-1] != second {
		t.Errorf("state order is %v, want %q last", def.StateOrder, second)
	}
}

func TestRenameState_RenamesTheKeyAndTheTransitionsThatNameIt(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.RenameState(path, "resting", "napping"); err != nil {
		t.Fatalf("RenameState: %v", err)
	}
	def := working(t, s, path)
	if _, ok := def.States["napping"]; !ok {
		t.Fatal("the state was not renamed")
	}
	if _, ok := def.States["resting"]; ok {
		t.Error("the old name is still there")
	}
	// The transition that targeted it follows, or renaming a state silently
	// dangles every edge into it.
	for _, tr := range def.States["idle"].On["GO"] {
		if tr.Target == "resting" {
			t.Error("a transition still targets the old name")
		}
	}
	found := false
	for _, tr := range def.States["idle"].On["GO"] {
		if tr.Target == "napping" {
			found = true
		}
	}
	if !found {
		t.Error("no transition targets the new name")
	}
}

// Authored order is kept in place. Renaming the state that is already last
// cannot tell that apart from moving it to the end, which is what the first
// version of this checked.
// Authored order is kept in place. Renaming the state that is already first —
// or last, which the version before that did — cannot tell "kept in place" from
// "moved to the front", so this renames the middle one and asserts the whole
// slice.
func TestRenameState_KeepsTheStateWhereItWasInTheAuthoredOrder(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	// idle, combat, resting.
	if err := s.RenameState(path, "combat", "battle"); err != nil {
		t.Fatalf("RenameState: %v", err)
	}
	got := working(t, s, path).StateOrder
	want := []string{"idle", "battle", "resting"}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// A nested state renamed: a transition that named it by its bare name has to
// follow as well as one that named it by its path. For a top-level state the
// two spellings are the same string, so only this can tell them apart.
func TestRenameState_FollowsBothSpellingsOfANestedTarget(t *testing.T) {
	const twoWays = `{
	  "id": "ways", "initial": "idle",
	  "states": {
	    "idle": { "on": {
	      "BARE": [{ "target": "attacking" }],
	      "DOTTED": [{ "target": "combat.attacking" }]
	    } },
	    "combat": { "initial": "attacking", "states": { "attacking": {} } }
	  }
	}`
	s, dir := open(t, map[string]string{"ways.json": twoWays})
	path := filepath.Join(dir, "ways.json")

	if err := s.RenameState(path, "combat.attacking", "striking"); err != nil {
		t.Fatalf("RenameState: %v", err)
	}
	def := working(t, s, path)
	if got := def.States["idle"].On["BARE"][0].Target; got != "striking" {
		t.Errorf("the bare target is %q, want striking", got)
	}
	if got := def.States["idle"].On["DOTTED"][0].Target; got != "combat.striking" {
		t.Errorf("the dotted target is %q, want combat.striking", got)
	}
}

func TestRenameState_MovesTheInitialWithIt(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.RenameState(path, "idle", "waiting"); err != nil {
		t.Fatalf("RenameState: %v", err)
	}
	if got := working(t, s, path).Initial; got != "waiting" {
		t.Errorf("initial = %q, want waiting — renaming the initial state broke the machine", got)
	}
}

// A dot is XState's own path separator and the canvas keys nodes on a dotted
// path, so a state named "a.b" is ambiguous to the engine's resolver as well.
// Story 4 pinned what happens; this is the story that stops Forge authoring it.
func TestRenameState_RefusesANameWithADotInIt(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.RenameState(path, "resting", "deeply.nested"); err == nil {
		t.Fatal("a dot in a state name is ambiguous to the engine's own resolver")
	}
	for _, bad := range []string{"", "  "} {
		if err := s.RenameState(path, "resting", bad); err == nil {
			t.Errorf("accepted %q as a state name", bad)
		}
	}
	if err := s.RenameState(path, "resting", "combat"); err == nil {
		t.Error("accepted a name another state at the same level already has")
	}
}

func TestSetInitial_MovesTheMachinesInitial(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.SetInitial(path, "resting"); err != nil {
		t.Fatalf("SetInitial: %v", err)
	}
	if got := working(t, s, path).Initial; got != "resting" {
		t.Errorf("initial = %q, want resting", got)
	}
}

// A nested state's initial belongs to the compound state that holds it, not to
// the machine. Setting the machine's would name a state that is not one of its
// children, which is a validation error and a different machine.
func TestSetInitial_OnANestedStateMovesItsParents(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.SetInitial(path, "combat.fleeing"); err != nil {
		t.Fatalf("SetInitial: %v", err)
	}
	def := working(t, s, path)
	if got := def.States["combat"].Initial; got != "fleeing" {
		t.Errorf("combat.initial = %q, want fleeing", got)
	}
	if got := def.Initial; got != "idle" {
		t.Errorf("the machine's initial moved to %q; it should not have", got)
	}
}

func TestAddTransition_NamesAnEventTheMachineDoesNotAlreadyUse(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	event, err := s.AddTransition(path, "resting", "idle")
	if err != nil {
		t.Fatalf("AddTransition: %v", err)
	}
	if event == "GO" || event == "POKE" {
		t.Errorf("the new transition reuses %q, which already means something else", event)
	}
	def := working(t, s, path)
	got := def.States["resting"].On[event]
	if len(got) != 1 || got[0].Target != "idle" {
		t.Fatalf("resting.on[%s] = %+v", event, got)
	}
	// A second one between the same two states does not collide.
	second, err := s.AddTransition(path, "resting", "idle")
	if err != nil {
		t.Fatalf("second AddTransition: %v", err)
	}
	if second == event {
		t.Errorf("both transitions are called %q", event)
	}
}

func TestAddTransition_ReachesNestedStatesAtBothEnds(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	event, err := s.AddTransition(path, "combat.attacking", "combat.fleeing")
	if err != nil {
		t.Fatalf("AddTransition: %v", err)
	}
	def := working(t, s, path)
	got := def.States["combat"].Children["attacking"].On[event]
	if len(got) != 1 {
		t.Fatalf("no transition on the nested source: %+v", def.States["combat"].Children["attacking"].On)
	}
	// The target is written as a path the engine resolves, not as a bare name
	// that might mean a different state.
	if _, resolved := agent.FindState(def, got[0].Target); resolved != "combat.fleeing" {
		t.Errorf("target %q resolves to %q, want combat.fleeing", got[0].Target, resolved)
	}
}

func TestAddTransition_RefusesAnEndThatIsNotThere(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if _, err := s.AddTransition(path, "nowhere", "idle"); err == nil {
		t.Error("accepted a source that does not exist")
	}
	if _, err := s.AddTransition(path, "idle", "nowhere"); err == nil {
		t.Error("accepted a target that does not exist")
	}
}

func TestDeleteTransition_RemovesTheOneNamedAndNotItsSibling(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	// idle has two transitions on GO. Delete the second.
	if err := s.DeleteTransition(path, ref("idle", "on", "GO", 1)); err != nil {
		t.Fatalf("DeleteTransition: %v", err)
	}
	got := working(t, s, path).States["idle"].On["GO"]
	if len(got) != 1 {
		t.Fatalf("GO has %d transitions, want 1", len(got))
	}
	if got[0].Target != "combat" {
		t.Errorf("the wrong one was deleted: %q is left", got[0].Target)
	}
}

// An event key with an empty list is a transition that fires on nothing. Left
// behind, it draws no edge and reads in the file as something someone meant.
func TestDeleteTransition_RemovesTheEventWhenItsLastOneGoes(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.DeleteTransition(path, ref("idle", "on", "POKE", 0)); err != nil {
		t.Fatalf("DeleteTransition: %v", err)
	}
	def := working(t, s, path)
	if _, ok := def.States["idle"].On["POKE"]; ok {
		t.Error("the event key is still there with nothing on it")
	}
	for _, key := range def.States["idle"].OnOrder {
		if key == "POKE" {
			t.Error("the event is still in the authored order")
		}
	}
}

func TestDeleteTransition_RefusesAnIndexThatIsNotThere(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	for _, tc := range []struct {
		from, kind, event string
		i                 int
	}{
		{"idle", "on", "GO", 2},
		{"idle", "on", "NOPE", 0},
		{"nowhere", "on", "GO", 0},
		{"idle", "after", "GO", 0},
	} {
		if err := s.DeleteTransition(path, ref(tc.from, tc.kind, tc.event, tc.i)); err == nil {
			t.Errorf("accepted %+v", tc)
		}
	}
}

func TestDeleteState_RemovesItAndLeavesTheRest(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.DeleteState(path, "resting"); err != nil {
		t.Fatalf("DeleteState: %v", err)
	}
	def := working(t, s, path)
	if _, ok := def.States["resting"]; ok {
		t.Error("the state is still there")
	}
	for _, name := range def.StateOrder {
		if name == "resting" {
			t.Error("the state is still in the authored order")
		}
	}
	// The transition that pointed at it is left dangling rather than quietly
	// removed: Story 4 draws a dangling edge, and losing the transition would
	// lose the actions on it too.
	found := false
	for _, tr := range def.States["idle"].On["GO"] {
		if tr.Target == "resting" {
			found = true
		}
	}
	if !found {
		t.Error("the transition into the deleted state was removed as well")
	}
}

func TestDeleteState_ReachesANestedStateAndRefusesTheLastOne(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.DeleteState(path, "combat.fleeing"); err != nil {
		t.Fatalf("DeleteState: %v", err)
	}
	if _, ok := working(t, s, path).States["combat"].Children["fleeing"]; ok {
		t.Error("the nested state is still there")
	}

	// A machine with no states does not validate, so deleting the last one
	// produces something that could never be saved.
	single := `{"id":"single","initial":"only","states":{"only":{}}}`
	s2, dir2 := open(t, map[string]string{"single.json": single})
	if err := s2.DeleteState(filepath.Join(dir2, "single.json"), "only"); err == nil {
		t.Error("deleting the last state left a machine that cannot be saved")
	}
}

// The warning the confirmation shows. A count would not tell you whether the
// thing about to break matters.
func TestTransitionsTargeting_NamesThemRatherThanCountingThem(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	got := s.TransitionsTargeting(path, "combat.attacking")
	if len(got) != 1 || !strings.Contains(got[0], "POKE") || !strings.Contains(got[0], "idle") {
		t.Errorf("got %v, want one naming POKE and idle", got)
	}
	// Both ways of naming the same state are found, or the warning is honest
	// only for whichever spelling the author happened to use.
	if got := s.TransitionsTargeting(path, "combat"); len(got) != 1 {
		t.Errorf("transitions into combat: %v", got)
	}
	if got := s.TransitionsTargeting(path, "combat.fleeing"); len(got) != 0 {
		t.Errorf("nothing targets combat.fleeing, got %v", got)
	}
}

// None of these may touch the disk: saving is the footer's job, and a canvas
// that wrote on every drag would make Discard a lie.
func TestCanvasMutations_ChangeTheSessionAndNotTheFile(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	before := onDisk(t, path)
	if err := s.MoveState(path, "idle", 1, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddState(path, 3, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTransition(path, "resting", "idle"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetInitial(path, "resting"); err != nil {
		t.Fatal(err)
	}
	if onDisk(t, path) != before {
		t.Error("a canvas edit wrote to the file")
	}
	dirty, err := s.Dirty()
	if err != nil || len(dirty) != 1 {
		t.Errorf("the session does not report the machine as unsaved: %v %v", dirty, err)
	}
}

// A machine with states and no initial does not validate, so the first state
// added to one becomes it — otherwise adding a state to a half-built machine
// leaves it just as unloadable as it was.
func TestAddState_BecomesTheInitialWhenThereIsNone(t *testing.T) {
	const headless = `{"id":"headless","states":{}}`
	s, dir := open(t, map[string]string{"headless.json": headless})
	path := filepath.Join(dir, "headless.json")

	name, err := s.AddState(path, 0, 0)
	if err != nil {
		t.Fatalf("AddState: %v", err)
	}
	if got := working(t, s, path).Initial; got != name {
		t.Errorf("initial = %q, want the state just added, %q", got, name)
	}

	// And a machine that already has one keeps it.
	s2, dir2 := open(t, map[string]string{"nested.json": nestedMachine})
	p2 := machinePath(t, dir2)
	if _, err := s2.AddState(p2, 0, 0); err != nil {
		t.Fatal(err)
	}
	if got := working(t, s2, p2).Initial; got != "idle" {
		t.Errorf("initial moved to %q", got)
	}
}

// An initial naming a state that is gone is a validation error the author did
// not make — the machine stops loading because of a delete they did mean.
func TestDeleteState_MovesTheInitialOffTheStateItRemoves(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.DeleteState(path, "idle"); err != nil {
		t.Fatalf("DeleteState: %v", err)
	}
	def := working(t, s, path)
	if def.Initial == "idle" {
		t.Fatal("the machine still enters a state that is not there")
	}
	if _, ok := def.States[def.Initial]; !ok {
		t.Errorf("initial = %q, which is not a state", def.Initial)
	}

	// The same inside a compound state, where the initial belongs to the
	// parent rather than to the machine.
	if err := s.DeleteState(path, "combat.attacking"); err != nil {
		t.Fatalf("DeleteState: %v", err)
	}
	combat := working(t, s, path).States["combat"]
	if combat.Initial == "attacking" {
		t.Error("the compound state still enters a child that is not there")
	}
	if _, ok := combat.Children[combat.Initial]; !ok {
		t.Errorf("combat.initial = %q, which is not one of its children", combat.Initial)
	}
}

// 6. A path names one node. The engine's own resolver searches by bare name
// across the whole tree, because that is what an author may write in a
// transition target — but accepting a fuzzy match for a path handed back by the
// canvas would let an edit land on a state nobody pointed at.
func TestStateEdits_ResolveAPathExactlyRatherThanSearchingForIt(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	// "attacking" exists, but only as a child of combat. The engine would find
	// it; an edit addressed to it must not.
	if _, found := agent.FindState(mustRead(t, s, path), "attacking"); found == "" {
		t.Fatal("the premise is wrong: the engine cannot find this either")
	}
	for _, op := range []struct {
		name string
		run  func() error
	}{
		{"move", func() error { return s.MoveState(path, "attacking", 1, 1) }},
		{"rename", func() error { return s.RenameState(path, "attacking", "x") }},
		{"delete", func() error { return s.DeleteState(path, "attacking") }},
		{"initial", func() error { return s.SetInitial(path, "attacking") }},
		{"connect", func() error { _, err := s.AddTransition(path, "attacking", "idle"); return err }},
	} {
		if err := op.run(); err == nil {
			t.Errorf("%s landed on a state its path does not name", op.name)
		}
	}
}

func mustRead(t *testing.T, s interface {
	Working(string) (*agent.MachineDefinition, error)
}, path string,
) *agent.MachineDefinition {
	t.Helper()
	return working(t, s, path)
}

// Renaming a compound state moves every path that ran *through* it. A rewrite
// that matched target strings against the old name left these dangling: the
// target is neither the old name nor the old path, it merely starts with it.
func TestRenameState_FollowsPathsThroughACompoundState(t *testing.T) {
	const through = `{
	  "id": "thr", "initial": "idle",
	  "states": {
	    "idle": { "on": {
	      "DOTTED": [{ "target": "combat.attacking" }],
	      "BARE": [{ "target": "fleeing" }],
	      "PARENT": [{ "target": "combat" }]
	    } },
	    "combat": { "initial": "attacking", "states": { "attacking": {}, "fleeing": {} } }
	  }
	}`
	s, dir := open(t, map[string]string{"thr.json": through})
	path := filepath.Join(dir, "thr.json")

	if err := s.RenameState(path, "combat", "battle"); err != nil {
		t.Fatalf("RenameState: %v", err)
	}
	def := working(t, s, path)
	on := def.States["idle"].On

	// A path through the renamed state follows it.
	if got := on["DOTTED"][0].Target; got != "battle.attacking" {
		t.Errorf("the dotted target is %q, want battle.attacking", got)
	}
	// The state itself follows, keeping the spelling it was written in.
	if got := on["PARENT"][0].Target; got != "battle" {
		t.Errorf("the target of the renamed state is %q, want battle", got)
	}
	// A bare name into a child still resolves by descent, so it is left alone
	// rather than churned into a path nobody wrote.
	if got := on["BARE"][0].Target; got != "fleeing" {
		t.Errorf("the bare target was rewritten to %q for no reason", got)
	}
	// And all three still resolve, which is the property under all of it.
	for event, want := range map[string]string{
		"DOTTED": "battle.attacking", "BARE": "battle.fleeing", "PARENT": "battle",
	} {
		if _, got := agent.FindState(def, on[event][0].Target); got != want {
			t.Errorf("%s resolves to %q, want %q", event, got, want)
		}
	}
}

// The property under every rename: each transition still resolves to the state
// it resolved to before. Asserted that way rather than on target strings,
// because a string is only how a target is spelled and resolution is what it
// means.
//
// This machine is the awkward case. Two states in different branches share a
// name, and *this engine resolves a bare target from the machine root* rather
// than relative to the source — so both "attacking" targets already mean
// combat.attacking, before anything is renamed. Real XState scopes to the
// parent; this one does not, which is a divergence worth knowing about and not
// this story's to change. What a rename must do is keep the machine running the
// way it ran, whichever rule that is.
func TestRenameState_KeepsEveryTransitionPointingWhereItPointed(t *testing.T) {
	const twins = `{
	  "id": "twins", "initial": "combat",
	  "states": {
	    "combat": { "initial": "attacking", "states": {
	      "attacking": {},
	      "hiding": { "on": { "X": [{ "target": "attacking" }] } }
	    } },
	    "duel": { "initial": "attacking", "states": {
	      "attacking": {},
	      "waiting": { "on": { "Y": [{ "target": "duel.attacking" }] } }
	    } }
	  }
	}`
	s, dir := open(t, map[string]string{"twins.json": twins})
	path := filepath.Join(dir, "twins.json")

	before := resolutions(t, working(t, s, path))
	if before["combat.hiding|X"] != "combat.attacking" || before["duel.waiting|Y"] != "duel.attacking" {
		t.Fatalf("the fixture does not exercise what it claims: %v", before)
	}

	if err := s.RenameState(path, "combat.attacking", "striking"); err != nil {
		t.Fatalf("RenameState: %v", err)
	}
	after := resolutions(t, working(t, s, path))

	// combat's own transition follows the state it named.
	if after["combat.hiding|X"] != "combat.striking" {
		t.Errorf("combat's transition now resolves to %q", after["combat.hiding|X"])
	}
	// The other branch, which named its own state by path, is untouched.
	if after["duel.waiting|Y"] != "duel.attacking" {
		t.Errorf("duel's transition was dragged along to %q", after["duel.waiting|Y"])
	}
}

// resolutions is where every transition in a machine points, keyed by the state
// and event that carries it.
func resolutions(t *testing.T, def *agent.MachineDefinition) map[string]string {
	t.Helper()
	out := map[string]string{}
	var walk func(map[string]*agent.StateNode, string)
	walk = func(states map[string]*agent.StateNode, prefix string) {
		for name, node := range states {
			for event, list := range node.On {
				for _, tr := range list {
					_, path := agent.FindState(def, tr.Target)
					out[prefix+name+"|"+event] = path
				}
			}
			walk(node.Children, prefix+name+".")
		}
	}
	walk(def.States, "")
	return out
}

// An id the author wrote is not the one parsing derived, and other transitions
// may target it. Rewriting it to fix a *name* nobody referred to would break
// every one of them.
func TestRenameState_LeavesAnAuthoredIdAlone(t *testing.T) {
	const authored = `{
	  "id": "auth", "initial": "a",
	  "states": {
	    "a": { "id": "the-start", "on": { "GO": [{ "target": "the-start" }] } },
	    "b": {}
	  }
	}`
	s, dir := open(t, map[string]string{"auth.json": authored})
	path := filepath.Join(dir, "auth.json")

	if err := s.RenameState(path, "a", "beginning"); err != nil {
		t.Fatalf("RenameState: %v", err)
	}
	def := working(t, s, path)
	if got := def.States["beginning"].ID; got != "the-start" {
		t.Errorf("the authored id became %q", got)
	}
	// And a derived one still follows, or a rename leaves stale ids behind.
	if got := def.States["b"].ID; got != "auth.b" {
		t.Errorf("b's derived id is %q", got)
	}
}

// The AC that had no test: nothing is lost when two edits arrive close
// together. The first version of the move read the recorded position through
// one lock acquisition and wrote position+delta through another, so eight
// concurrent drags left five of them lost.
//
// Asserted on the total, which is the only thing that can catch it: every
// individual result looks plausible.
func TestMoveBy_LosesNothingWhenDragsArriveTogether(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	const n = 24
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.MoveBy(path, "idle", 10, 5); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("MoveBy: %v", err)
	}

	// idle starts at 40,40 in the fixture.
	var got struct{ X, Y float64 }
	if err := json.Unmarshal(metaOf(t, working(t, s, path), "idle")["forge"], &got); err != nil {
		t.Fatal(err)
	}
	if got.X != 40+10*n || got.Y != 40+5*n {
		t.Errorf("after %d drags of (10,5) the state is at (%v,%v), want (%v,%v) — %v were lost",
			n, got.X, got.Y, float64(40+10*n), float64(40+5*n), (40+10*n-got.X)/10)
	}
}

// The same shape, for the other mutation that reads before it writes.
func TestAddState_LosesNothingWhenAddsArriveTogether(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	const n = 16
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.AddState(path, 10, 10); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	def := working(t, s, path)
	// Three to start with. Every add has to have produced its own state, or two
	// that arrived together chose the same free name and one overwrote the
	// other.
	if len(def.States) != 3+n {
		t.Errorf("%d states after %d adds, want %d", len(def.States), n, 3+n)
	}
	if len(def.StateOrder) != len(def.States) {
		t.Errorf("the authored order has %d entries for %d states", len(def.StateOrder), len(def.States))
	}
}

// The one documented reason setLayout is hand-rolled instead of marshalling a
// map: a map alphabetises, and the file is read in diffs. Asserted on the bytes,
// because every other test here goes through a map and throws the order away.
func TestMoveState_KeepsTheAuthorsKeyOrderInMeta(t *testing.T) {
	const ordered = `{
	  "id": "ord", "initial": "a",
	  "states": {
	    "a": { "meta": { "zeta": 1, "alpha": 2, "a&b": "amp" } }
	  }
	}`
	s, dir := open(t, map[string]string{"ord.json": ordered})
	path := filepath.Join(dir, "ord.json")

	if err := s.MoveState(path, "a", 7, 8); err != nil {
		t.Fatalf("MoveState: %v", err)
	}
	got := string(working(t, s, path).States["a"].Extra["meta"])

	if !strings.HasPrefix(got, `{"zeta":1,"alpha":2,`) {
		t.Errorf("the author's key order was not kept: %s", got)
	}
	// Appended, not inserted: a new key belongs at the end.
	if !strings.HasSuffix(got, `"forge":{"x":7,"y":8}}`) {
		t.Errorf("forge is not at the end: %s", got)
	}
	// And an ampersand stays an ampersand. The emitter turns HTML escaping off
	// everywhere else it writes a string; a nudge of a box should not be the one
	// thing that mangles someone's key.
	if !strings.Contains(got, `"a&b"`) {
		t.Errorf("a key was HTML-escaped: %s", got)
	}
}

// meta.forge is Forge's own object, and later stories will put things in it. A
// move replaces only the position, or the first drag destroys whatever they
// wrote.
func TestMoveState_KeepsWhatElseIsUnderForge(t *testing.T) {
	const withExtra = `{
	  "id": "ext", "initial": "a",
	  "states": { "a": { "meta": { "forge": { "colour": "amber", "x": 1, "y": 2 } } } }
	}`
	s, dir := open(t, map[string]string{"ext.json": withExtra})
	path := filepath.Join(dir, "ext.json")

	if err := s.MoveState(path, "a", 30, 40); err != nil {
		t.Fatalf("MoveState: %v", err)
	}
	got := string(metaOf(t, working(t, s, path), "a")["forge"])
	if !strings.Contains(got, `"colour":"amber"`) {
		t.Errorf("a key under forge was destroyed by a move: %s", got)
	}
	if !strings.Contains(got, `"x":30`) || !strings.Contains(got, `"y":40`) {
		t.Errorf("the position was not written: %s", got)
	}
}

// One state's path may be a suffix of another's. Rewriting targets by matching
// the end of a resolved path would then drag the wrong transition along.
func TestRenameState_DoesNotFollowAPathThatMerelyEndsTheSameWay(t *testing.T) {
	const suffix = `{
	  "id": "sfx", "initial": "attacking",
	  "states": {
	    "attacking": {},
	    "combat": { "initial": "attacking", "states": { "attacking": {} },
	      "on": { "OUT": [{ "target": "combat.attacking" }] } },
	    "idle": { "on": { "TOP": [{ "target": "attacking" }] } }
	  }
	}`
	s, dir := open(t, map[string]string{"sfx.json": suffix})
	path := filepath.Join(dir, "sfx.json")

	// Rename the top-level one. "combat.attacking" ends with "attacking".
	if err := s.RenameState(path, "attacking", "striking"); err != nil {
		t.Fatalf("RenameState: %v", err)
	}
	def := working(t, s, path)

	if got := def.States["idle"].On["TOP"][0].Target; got != "striking" {
		t.Errorf("the transition into the renamed state points at %q", got)
	}
	// The nested one is a different state and keeps its own name.
	if got := def.States["combat"].On["OUT"][0].Target; got != "combat.attacking" {
		t.Errorf("a transition into a different state was rewritten to %q", got)
	}
	if _, ok := def.States["combat"].Children["attacking"]; !ok {
		t.Error("the nested state was renamed too")
	}
}

// An authored id that happens to end with the state's name is still authored.
// Rewriting it by suffix would break every "#custom.a" reference to fix a name
// nobody referred to.
func TestRenameState_LeavesAnAuthoredIdThatLooksDerivedAlone(t *testing.T) {
	const lookalike = `{
	  "id": "auth", "initial": "a",
	  "states": { "a": { "id": "custom.a" }, "b": {} }
	}`
	s, dir := open(t, map[string]string{"look.json": lookalike})
	path := filepath.Join(dir, "look.json")

	if err := s.RenameState(path, "a", "beginning"); err != nil {
		t.Fatalf("RenameState: %v", err)
	}
	if got := working(t, s, path).States["beginning"].ID; got != "custom.a" {
		t.Errorf("the authored id became %q; only the derived form follows a rename", got)
	}
}

// ── entry and exit actions ───────────────────────────────────────────────────

func actionsOf(t *testing.T, def *agent.MachineDefinition, state, kind string) []agent.ActionSpec {
	t.Helper()
	node, ok := def.States[state]
	if !ok {
		t.Fatalf("no state %q", state)
	}
	if kind == "exit" {
		return node.Exit
	}
	return node.Entry
}

func TestAddAction_AppendsInAuthoredOrderAndReportsWhatItAdded(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	for _, name := range []string{"log", "setAnimation"} {
		if err := s.AddAction(path, "idle", "entry", name); err != nil {
			t.Fatalf("AddAction %s: %v", name, err)
		}
	}
	got := actionsOf(t, working(t, s, path), "idle", "entry")
	if len(got) != 2 || got[0].Type != "log" || got[1].Type != "setAnimation" {
		t.Fatalf("entry = %+v, want log then setAnimation", got)
	}
	// Added bare, so the file says only what has been chosen. An action written
	// out with an empty params object claims a decision nobody made.
	if got[0].Params != nil {
		t.Errorf("the new action carries params nobody set: %+v", got[0].Params)
	}
}

func TestAddAction_ReachesExitAndNestedStates(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.AddAction(path, "combat.attacking", "exit", "log"); err != nil {
		t.Fatalf("AddAction: %v", err)
	}
	node := working(t, s, path).States["combat"].Children["attacking"]
	if len(node.Exit) != 1 || node.Exit[0].Type != "log" {
		t.Errorf("exit = %+v", node.Exit)
	}
	if len(node.Entry) != 0 {
		t.Errorf("it went on entry instead: %+v", node.Entry)
	}
}

// The registry is the list of what the engine will accept. A name that is not
// on it makes a machine the engine refuses to load, found at startup rather
// than at the moment of the mistake.
func TestAddAction_RefusesANameTheRegistryDoesNotHave(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	if err := s.AddAction(path, "idle", "entry", "teleport"); err == nil {
		t.Error("accepted an action the engine does not register")
	}
	// And the map-gated ones, which this project does not have: the engine
	// would not register computePath either, so offering it would be Forge and
	// the game disagreeing about the vocabulary.
	if err := s.AddAction(path, "idle", "entry", "computePath"); err == nil {
		t.Error("accepted an action that needs a map this project has not got")
	}
	if err := s.AddAction(path, "idle", "nowhere", "log"); err == nil {
		t.Error("accepted a kind of action list that does not exist")
	}
}

func TestRemoveAction_TakesTheOneNamedAndNotItsNeighbour(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)
	for _, name := range []string{"log", "setAnimation", "setPursueTarget"} {
		if err := s.AddAction(path, "idle", "entry", name); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.RemoveAction(path, "idle", "entry", 1); err != nil {
		t.Fatalf("RemoveAction: %v", err)
	}
	got := actionsOf(t, working(t, s, path), "idle", "entry")
	if len(got) != 2 || got[0].Type != "log" || got[1].Type != "setPursueTarget" {
		t.Errorf("entry = %+v, want log then setPursueTarget", got)
	}
}

func TestRemoveAction_RefusesAnIndexThatIsNotThere(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)
	if err := s.AddAction(path, "idle", "entry", "log"); err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{-1, 1, 99} {
		if err := s.RemoveAction(path, "idle", "entry", i); err == nil {
			t.Errorf("accepted index %d", i)
		}
	}
}

func TestSetActionParam_ConvertsByTheTypeTheRegistryDeclares(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)
	if err := s.AddAction(path, "idle", "entry", "dealDamage"); err != nil {
		t.Fatal(err)
	}

	if err := s.SetActionParam(path, "idle", "entry", 0, "amount", "5"); err != nil {
		t.Fatalf("SetActionParam: %v", err)
	}
	if err := s.SetActionParam(path, "idle", "entry", 0, "target", "$player"); err != nil {
		t.Fatalf("SetActionParam: %v", err)
	}
	got := actionsOf(t, working(t, s, path), "idle", "entry")[0]
	// A number, not the string "5" — the engine reads these as their declared
	// type and a quoted number is a different value.
	if amount, ok := got.Params["amount"].(float64); !ok || amount != 5 {
		t.Errorf("amount = %#v, want the number 5", got.Params["amount"])
	}
	if target, ok := got.Params["target"].(string); !ok || target != "$player" {
		t.Errorf("target = %#v", got.Params["target"])
	}
}

func TestSetActionParam_RefusesAValueTheTypeCannotHold(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)
	if err := s.AddAction(path, "idle", "entry", "dealDamage"); err != nil {
		t.Fatal(err)
	}

	err := s.SetActionParam(path, "idle", "entry", 0, "amount", "quite a lot")
	if err == nil {
		t.Fatal("accepted a word where the registry declares a number")
	}
	// Named, so the message says which field to go and fix.
	if !strings.Contains(err.Error(), "amount") {
		t.Errorf("the refusal does not name the parameter: %v", err)
	}
	if err := s.SetActionParam(path, "idle", "entry", 0, "nonesuch", "1"); err == nil {
		t.Error("accepted a parameter the action does not take")
	}
}

// An absent optional parameter is what the engine expects. An empty string is a
// value — and for dealDamage.target it is a different one from the default.
func TestSetActionParam_ClearingRemovesTheParameterRatherThanEmptyingIt(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)
	if err := s.AddAction(path, "idle", "entry", "dealDamage"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActionParam(path, "idle", "entry", 0, "target", "12"); err != nil {
		t.Fatal(err)
	}

	if err := s.SetActionParam(path, "idle", "entry", 0, "target", ""); err != nil {
		t.Fatalf("SetActionParam: %v", err)
	}
	got := actionsOf(t, working(t, s, path), "idle", "entry")[0]
	if _, present := got.Params["target"]; present {
		t.Errorf("target is still there as %#v", got.Params["target"])
	}
	// And the last one going leaves no empty params object behind.
	if err := s.SetActionParam(path, "idle", "entry", 0, "amount", ""); err != nil {
		t.Fatal(err)
	}
	if got := actionsOf(t, working(t, s, path), "idle", "entry")[0]; got.Params != nil {
		t.Errorf("an empty params object was left behind: %#v", got.Params)
	}
}

func TestSetActionParam_HandlesEveryTypeTheRegistryDeclares(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)
	if err := s.AddAction(path, "idle", "entry", "attachComponent"); err != nil {
		t.Fatal(err)
	}

	// object: a JSON document, which is what the engine takes there.
	if err := s.SetActionParam(path, "idle", "entry", 0, "data", `{"hp": 3}`); err != nil {
		t.Fatalf("SetActionParam: %v", err)
	}
	got := actionsOf(t, working(t, s, path), "idle", "entry")[0]
	obj, ok := got.Params["data"].(map[string]any)
	if !ok || obj["hp"] != float64(3) {
		t.Errorf("data = %#v", got.Params["data"])
	}
	if err := s.SetActionParam(path, "idle", "entry", 0, "data", "{not json"); err == nil {
		t.Error("accepted something that is not JSON where an object is declared")
	}
}

// The catalogue is the engine's own vocabulary for this project, not a list.
func TestActionCatalogue_IsTheRegistrysAndFollowsWhetherTheresAMap(t *testing.T) {
	s, _ := open(t, map[string]string{"nested.json": nestedMachine})

	names := map[string]bool{}
	for _, meta := range s.ActionCatalogue() {
		names[meta.Name] = true
		if meta.Description == "" {
			t.Errorf("%s has no description, so the dropdown can only show its name", meta.Name)
		}
	}
	if !names["dealDamage"] {
		t.Error("the catalogue is missing a built-in the engine registers")
	}
	// This project has no map, so the engine would not register these — and a
	// machine using one would not load.
	if names["computePath"] {
		t.Error("the catalogue offers an action that needs a map this project has not got")
	}
	// Sorted, so two renders of the same project agree.
	got := s.ActionCatalogue()
	for i := 1; i < len(got); i++ {
		if got[i-1].Name > got[i].Name {
			t.Fatalf("the catalogue is not in a settled order: %s before %s", got[i-1].Name, got[i].Name)
		}
	}
}

// An action edit addresses the node the inspector named, so its path resolves
// exactly. The engine's own resolver searches by bare name across the tree,
// because that is what an author may write in a transition target — accepting a
// fuzzy match here would let an edit land on a state nobody pointed at.
func TestActionEdits_ResolveAPathExactly(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)

	// "attacking" exists, but only under combat. The engine would find it.
	if _, found := agent.FindState(working(t, s, path), "attacking"); found == "" {
		t.Fatal("the premise is wrong: the engine cannot find this either")
	}
	if err := s.AddAction(path, "attacking", "entry", "log"); err == nil {
		t.Error("an action landed on a state its path does not name")
	}
	if err := s.AddAction(path, "combat.attacking", "entry", "log"); err != nil {
		t.Fatalf("the real path was refused: %v", err)
	}
	for _, op := range []struct {
		name string
		run  func() error
	}{
		{"remove", func() error { return s.RemoveAction(path, "attacking", "entry", 0) }},
		{"param", func() error { return s.SetActionParam(path, "attacking", "entry", 0, "message", "x") }},
	} {
		if err := op.run(); err == nil {
			t.Errorf("%s landed on a state its path does not name", op.name)
		}
	}
}

// ParseFloat accepts three things a JSON file cannot hold, and storing one is
// not recoverable without loss: every later edit clones through the emitter and
// fails, so the machine cannot be edited, rendered or saved, and only Discard
// gets out — throwing away everything else unsaved with it.
func TestSetActionParam_RefusesANumberTheFileCannotHold(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)
	if err := s.AddAction(path, "idle", "entry", "dealDamage"); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"NaN", "Inf", "+Inf", "-Inf", "infinity", "1_000", "0x1p4"} {
		if err := s.SetActionParam(path, "idle", "entry", 0, "amount", bad); err == nil {
			t.Errorf("%q was accepted as a number", bad)
		}
		// And the session is still usable, which is the part that matters.
		if _, err := s.Working(path); err != nil {
			t.Fatalf("the machine is wedged after %q: %v", bad, err)
		}
	}
	// The ordinary spellings still work, including the ones JSON allows.
	for _, good := range []string{"5", "-2", "0", "1.5", "1e3", "-0.25"} {
		if err := s.SetActionParam(path, "idle", "entry", 0, "amount", good); err != nil {
			t.Errorf("%q was refused: %v", good, err)
		}
	}
}

// null unmarshals into a nil map without error, so it is the one non-object
// that slips past a map-typed decode.
func TestSetActionParam_RefusesEveryJSONValueThatIsNotAnObject(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)
	if err := s.AddAction(path, "idle", "entry", "attachComponent"); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"null", " null ", "[1,2]", `"a string"`, "3", "true"} {
		if err := s.SetActionParam(path, "idle", "entry", 0, "data", bad); err == nil {
			t.Errorf("%q was accepted where an object is declared", bad)
		}
	}
}

func TestAddAction_ObsoleteTileBooleanIsNotAuthorable(t *testing.T) {
	s, dir := openWithMap(t, map[string]string{"nested.json": nestedMachine})
	path := machinePath(t, dir)
	if err := s.AddAction(path, "idle", "entry", "setTilePassable"); err == nil {
		t.Fatal("Forge offered a builtin that writes a Tile field the engine no longer reads")
	}
}

// The catalogue follows the project both ways. Nothing tested the direction
// that *narrows* it, which silently makes a legitimate action unreachable.
func TestActionCatalogue_OffersTheMapActionsWhenThereIsAMap(t *testing.T) {
	without, _ := open(t, map[string]string{"nested.json": nestedMachine})
	with, _ := openWithMap(t, map[string]string{"nested.json": nestedMachine})

	has := func(s interface{ ActionCatalogue() []agent.ActionMeta }, name string) bool {
		for _, meta := range s.ActionCatalogue() {
			if meta.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"computePath", "stepAlongPath"} {
		if has(without, name) {
			t.Errorf("%s is offered to a project with no map", name)
		}
		if !has(with, name) {
			t.Errorf("%s is not offered to a project that has one", name)
		}
	}
	if has(with, "setTilePassable") {
		t.Error("obsolete tile Boolean offered even when a map is present")
	}
	// And the ungated ones are there either way.
	if !has(without, "dealDamage") || !has(with, "dealDamage") {
		t.Error("an ungated builtin went missing")
	}
}

// The emitter writes the bare string form only when Bare is set and there are
// no params, so an action authored as "dealDamage" has to come back as one
// after a parameter is set and cleared again. Clearing the flag on set made
// that one-way, leaving a spurious {"type": "dealDamage"} in the file.
func TestSetActionParam_LeavesABareActionBareAfterAParameterComesAndGoes(t *testing.T) {
	const bare = `{
	  "id": "bare", "initial": "a",
	  "states": { "a": { "entry": ["dealDamage"] } }
	}`
	s, dir := open(t, map[string]string{"bare.json": bare})
	path := filepath.Join(dir, "bare.json")

	before := emitted(t, s, path)
	if err := s.SetActionParam(path, "a", "entry", 0, "amount", "5"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActionParam(path, "a", "entry", 0, "amount", ""); err != nil {
		t.Fatal(err)
	}
	if got := emitted(t, s, path); got != before {
		t.Errorf("setting a parameter and clearing it rewrote the file:\n%s\nwant:\n%s", got, before)
	}
	// Compared on what would be written rather than on Dirty, which also
	// reports the whitespace difference between this fixture and the emitter's
	// own layout — a difference that was there before the edit and is Story 2's
	// "would be reformatted", not this one's.
}

// emitted is what the session would write, which is where a spurious diff shows.
func emitted(t *testing.T, s interface {
	Working(string) (*agent.MachineDefinition, error)
}, path string,
) string {
	t.Helper()
	raw, err := agent.EmitMachine(working(t, s, path))
	if err != nil {
		t.Fatalf("EmitMachine: %v", err)
	}
	return string(raw)
}
