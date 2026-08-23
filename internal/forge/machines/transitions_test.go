package machines_test

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/machines"
)

// transitionMachine is what the transition inspector edits: two transitions on
// one event (the order is the logic), a guard authored as the bare shorthand,
// an internal transition with actions and no target, and an after.
const transitionMachine = `{
  "id": "tr",
  "initial": "idle",
  "states": {
    "idle": {
      "on": {
        "GO": [{ "target": "combat", "cond": "inRange" }, { "target": "resting" }],
        "POKE": [{ "actions": ["log"] }]
      },
      "after": {
        "500": [{ "target": "resting" }]
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

func openTransitions(t *testing.T) (*machines.Session, string) {
	t.Helper()
	s, dir := open(t, map[string]string{"nested.json": transitionMachine})
	return s, machinePath(t, dir)
}

func ref(from, kind, event string, i int) machines.TransitionRef {
	return machines.TransitionRef{From: from, Kind: kind, Event: event, Index: i}
}

func transitionsOn(t *testing.T, s *machines.Session, path, state, event string) []agent.Transition {
	t.Helper()
	node, err := machines.StateAt(working(t, s, path), state)
	if err != nil {
		t.Fatalf("StateAt(%q): %v", state, err)
	}
	return node.On[event]
}

// ── the catalogue ─────────────────────────────────────────────────────────────

func TestGuardCatalogue_IsTheRegistrysAndNotAList(t *testing.T) {
	s, _ := openTransitions(t)

	var names []string
	for _, meta := range s.GuardCatalogue() {
		names = append(names, meta.Name)
	}
	want := []string{"atTarget", "hasComponent", "healthAbove", "inRange", "timerExpired"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("guards = %v, want %v", names, want)
	}
}

// The same gating the action catalogue has, for the same reason: the engine
// registers inLineOfSight only when a map is configured, so offering it to a
// mapless project would report a machine the engine then refuses to load.
func TestGuardCatalogue_IgnoresTheLineOfSightGuardWithoutAMap(t *testing.T) {
	mapless, _ := openTransitions(t)
	withMap, _ := openWithMap(t, map[string]string{"nested.json": transitionMachine})

	if hasGuard(mapless.GuardCatalogue(), "inLineOfSight") {
		t.Error("a mapless project offers inLineOfSight, which its engine does not register")
	}
	if !hasGuard(withMap.GuardCatalogue(), "inLineOfSight") {
		t.Error("a project with a map does not offer inLineOfSight, which its engine registers")
	}
	if !hasGuard(withMap.GuardCatalogue(), "pathComplete") {
		t.Error("a project with a map does not offer pathComplete")
	}
}

func hasGuard(metas []agent.GuardMeta, name string) bool {
	for _, m := range metas {
		if m.Name == name {
			return true
		}
	}
	return false
}

// The guard catalogue carries its descriptions, which is what the dropdown says
// beside each name.
func TestGuardCatalogue_CarriesTheRegisteredDescriptions(t *testing.T) {
	s, _ := openTransitions(t)
	for _, meta := range s.GuardCatalogue() {
		if meta.Description == "" {
			t.Errorf("guard %q has no description", meta.Name)
		}
	}
}

// ── what the dropdowns are made of ────────────────────────────────────────────

func TestStateTargets_IsEveryStateInAuthoredOrder(t *testing.T) {
	s, path := openTransitions(t)

	got := strings.Join(machines.StateTargets(working(t, s, path)), ",")
	want := "idle,combat,combat.attacking,combat.fleeing,resting"
	if got != want {
		t.Errorf("targets = %s, want %s", got, want)
	}
}

// Suggestions, and only the ones this machine already uses: the event name is
// free text and a typo makes a transition that never fires.
func TestEventNames_AreTheOnesThisMachineAlreadyUses(t *testing.T) {
	s, path := openTransitions(t)

	got := strings.Join(machines.EventNames(working(t, s, path)), ",")
	if got != "GO,POKE" {
		t.Errorf("events = %s, want GO,POKE", got)
	}
}

// ── the event ─────────────────────────────────────────────────────────────────

func TestSetTransitionEvent_RenamesTheKeyInPlaceWhenItHoldsOne(t *testing.T) {
	s, path := openTransitions(t)

	got, err := s.SetTransitionEvent(path, ref("idle", "on", "POKE", 0), "PROD")
	if err != nil {
		t.Fatalf("SetTransitionEvent: %v", err)
	}
	if got != ref("idle", "on", "PROD", 0) {
		t.Errorf("new ref = %+v", got)
	}
	node, _ := machines.StateAt(working(t, s, path), "idle")
	if _, still := node.On["POKE"]; still {
		t.Error("the old event key is still there")
	}
	if len(node.On["PROD"]) != 1 {
		t.Fatalf("PROD holds %d transitions", len(node.On["PROD"]))
	}
	// In place, so the file's key order is untouched — a rename that moved the
	// event to the end would be a diff nobody asked for.
	if strings.Join(node.OnOrder, ",") != "GO,PROD" {
		t.Errorf("OnOrder = %v, want GO,PROD", node.OnOrder)
	}
}

// In place means *in place*: with two events on either side of it, the renamed
// one stays between them. A remove-and-append would leave the machine behaving
// identically and the file reordered, which is a diff nobody asked for — and
// which the two-event fixture above cannot tell apart, because a key appended to
// a one-key order lands where it already was.
func TestSetTransitionEvent_KeepsTheRenamedEventBetweenItsNeighbours(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": `{
	  "id": "tr",
	  "initial": "idle",
	  "states": {
	    "idle": {
	      "on": {
	        "AAA": [{ "target": "idle" }],
	        "MMM": [{ "target": "idle" }],
	        "ZZZ": [{ "target": "idle" }]
	      }
	    }
	  }
	}`})
	path := machinePath(t, dir)

	if _, err := s.SetTransitionEvent(path, ref("idle", "on", "MMM", 0), "BBB"); err != nil {
		t.Fatalf("SetTransitionEvent: %v", err)
	}
	node, _ := machines.StateAt(working(t, s, path), "idle")
	if got := strings.Join(node.OnOrder, ","); got != "AAA,BBB,ZZZ" {
		t.Errorf("OnOrder = %s, want AAA,BBB,ZZZ", got)
	}
}

// Moving one of several onto an event that already exists appends it, which is
// the only sane default: appended means lowest priority, and priority is the
// order.
func TestSetTransitionEvent_MovesOneOfSeveralOntoAnExistingEvent(t *testing.T) {
	s, path := openTransitions(t)

	got, err := s.SetTransitionEvent(path, ref("idle", "on", "GO", 0), "POKE")
	if err != nil {
		t.Fatalf("SetTransitionEvent: %v", err)
	}
	if got != ref("idle", "on", "POKE", 1) {
		t.Errorf("new ref = %+v, want POKE index 1", got)
	}
	node, _ := machines.StateAt(working(t, s, path), "idle")
	if len(node.On["GO"]) != 1 || node.On["GO"][0].Target != "resting" {
		t.Errorf("GO = %+v, want only the unguarded one", node.On["GO"])
	}
	if len(node.On["POKE"]) != 2 || node.On["POKE"][1].Target != "combat" {
		t.Errorf("POKE = %+v, want the moved one appended", node.On["POKE"])
	}
}

// Two new events keep the order they were made in, rather than being
// alphabetised. The order is recorded as each one arrives; without that the
// emitter's fallback sorts whatever it was never told about, so making ZZZ and
// then AAA would write AAA first — a file that reads in an order nobody chose.
func TestSetTransitionEvent_RecordsANewEventWhereItWasMade(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": `{
	  "id": "tr",
	  "initial": "idle",
	  "states": {
	    "idle": {
	      "on": {
	        "GO": [{ "target": "idle" }, { "target": "idle" }, { "target": "idle" }]
	      }
	    }
	  }
	}`})
	path := machinePath(t, dir)

	for _, to := range []string{"ZZZ", "AAA"} {
		if _, err := s.SetTransitionEvent(path, ref("idle", "on", "GO", 0), to); err != nil {
			t.Fatalf("SetTransitionEvent(%s): %v", to, err)
		}
	}
	node, _ := machines.StateAt(working(t, s, path), "idle")
	if got := strings.Join(node.OnOrder, ","); got != "GO,ZZZ,AAA" {
		t.Errorf("OnOrder = %s, want GO,ZZZ,AAA", got)
	}
}

func TestSetTransitionEvent_LeavesEverythingAloneWhenTheNameIsUnchanged(t *testing.T) {
	s, path := openTransitions(t)

	got, err := s.SetTransitionEvent(path, ref("idle", "on", "GO", 1), "GO")
	if err != nil {
		t.Fatalf("SetTransitionEvent: %v", err)
	}
	if got != ref("idle", "on", "GO", 1) {
		t.Errorf("new ref = %+v", got)
	}
	if len(transitionsOn(t, s, path, "idle", "GO")) != 2 {
		t.Error("renaming an event to itself changed the list")
	}
}

func TestSetTransitionEvent_RefusesANameWithNothingInIt(t *testing.T) {
	s, path := openTransitions(t)

	if _, err := s.SetTransitionEvent(path, ref("idle", "on", "GO", 0), "  "); err == nil {
		t.Error("an event with no name was accepted")
	}
}

// The engine's parser and not a regex written here.
func TestSetTransitionEvent_ValidatesAnAfterDurationWithTheEnginesParser(t *testing.T) {
	for _, tc := range []struct {
		in string
		ok bool
	}{
		{"500", true},
		{"1s", true},
		{"1.5s", true},
		{"2m", true},
		{"1 second", false},
		{"soon", false},
		{"", false},
	} {
		s, path := openTransitions(t)
		_, err := s.SetTransitionEvent(path, ref("idle", "after", "500", 0), tc.in)
		if tc.ok && err != nil {
			t.Errorf("%q: %v", tc.in, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%q was accepted as a duration", tc.in)
		}
	}
}

// The refusal has to carry a spelling that works, because the field gives no
// other clue what one looks like.
func TestSetTransitionEvent_SaysWhatADurationLooksLike(t *testing.T) {
	s, path := openTransitions(t)

	_, err := s.SetTransitionEvent(path, ref("idle", "after", "500", 0), "1 second")
	if err == nil {
		t.Fatal("accepted")
	}
	for _, want := range []string{"1 second", "500", "1s"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not mention %q", err, want)
		}
	}
}

// An ordinary event is not a duration and must not be measured as one.
func TestSetTransitionEvent_DoesNotValidateAnOrdinaryEventAsADuration(t *testing.T) {
	s, path := openTransitions(t)

	if _, err := s.SetTransitionEvent(path, ref("idle", "on", "GO", 0), "1 second"); err != nil {
		t.Errorf("an event name was measured as a duration: %v", err)
	}
}

// ── the target ────────────────────────────────────────────────────────────────

func TestSetTransitionTarget_WritesTheDottedPath(t *testing.T) {
	s, path := openTransitions(t)

	if err := s.SetTransitionTarget(path, ref("idle", "on", "GO", 1), "combat.fleeing"); err != nil {
		t.Fatalf("SetTransitionTarget: %v", err)
	}
	if got := transitionsOn(t, s, path, "idle", "GO")[1].Target; got != "combat.fleeing" {
		t.Errorf("target = %q", got)
	}
}

// An empty target is a transition that runs its actions and changes no state.
// It is a legitimate thing to write, and the dropdown has to be able to say it
// or it would silently retarget POKE the first time anyone touched the panel.
func TestSetTransitionTarget_ClearingItMakesTheTransitionInternal(t *testing.T) {
	s, path := openTransitions(t)

	if err := s.SetTransitionTarget(path, ref("idle", "on", "GO", 1), ""); err != nil {
		t.Fatalf("SetTransitionTarget: %v", err)
	}
	if got := transitionsOn(t, s, path, "idle", "GO")[1].Target; got != "" {
		t.Errorf("target = %q, want empty", got)
	}
}

// Clearing the target of a transition authored as a plain target string has to
// clear the flag that says so, or the emitter writes "GO": "" — a transition
// aimed at the state called empty-string. It parses back as internal and breaks
// nothing, which is exactly why it would have gone unnoticed.
func TestSetTransitionTarget_ClearingABareTransitionDoesNotEmitAnEmptyTarget(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": `{
	  "id": "tr",
	  "initial": "idle",
	  "states": { "idle": { "on": { "GO": "resting" } }, "resting": {} }
	}`})
	path := machinePath(t, dir)

	if err := s.SetTransitionTarget(path, ref("idle", "on", "GO", 0), ""); err != nil {
		t.Fatalf("SetTransitionTarget: %v", err)
	}
	raw, err := agent.EmitMachine(working(t, s, path))
	if err != nil {
		t.Fatalf("EmitMachine: %v", err)
	}
	if strings.Contains(string(raw), `"GO": ""`) || strings.Contains(string(raw), `"GO":""`) {
		t.Errorf("the file says the transition targets the state called empty-string:\n%s", raw)
	}
}

func TestSetTransitionTarget_RefusesSomethingThatIsNotAState(t *testing.T) {
	s, path := openTransitions(t)

	if err := s.SetTransitionTarget(path, ref("idle", "on", "GO", 0), "combat.nowhere"); err == nil {
		t.Error("a target that is not a state was accepted")
	}
	if got := transitionsOn(t, s, path, "idle", "GO")[0].Target; got != "combat" {
		t.Errorf("the refused edit landed anyway: %q", got)
	}
}

// ── the guard ─────────────────────────────────────────────────────────────────

func TestSetTransitionGuard_WritesTheShorthandForAGuardWithNoParams(t *testing.T) {
	s, path := openTransitions(t)

	if err := s.SetTransitionGuard(path, ref("idle", "on", "GO", 1), "atTarget"); err != nil {
		t.Fatalf("SetTransitionGuard: %v", err)
	}
	cond := transitionsOn(t, s, path, "idle", "GO")[1].Cond
	if cond == nil || cond.Type != "atTarget" {
		t.Fatalf("cond = %+v", cond)
	}
	// The shorthand, so a guard with nothing on it emits as the string it would
	// have been authored as rather than being expanded into an object.
	if !cond.Bare || cond.Params != nil {
		t.Errorf("the guard was written as an object: bare=%v params=%v", cond.Bare, cond.Params)
	}
}

func TestSetTransitionGuard_ClearingItRemovesTheCondEntirely(t *testing.T) {
	s, path := openTransitions(t)

	if err := s.SetTransitionGuard(path, ref("idle", "on", "GO", 0), ""); err != nil {
		t.Fatalf("SetTransitionGuard: %v", err)
	}
	if cond := transitionsOn(t, s, path, "idle", "GO")[0].Cond; cond != nil {
		t.Errorf("cond = %+v, want nil", cond)
	}
}

func TestSetTransitionGuard_RefusesOneTheEngineDoesNotRegister(t *testing.T) {
	s, path := openTransitions(t)

	err := s.SetTransitionGuard(path, ref("idle", "on", "GO", 1), "looksRight")
	if err == nil {
		t.Fatal("an unregistered guard was accepted")
	}
	if !strings.Contains(err.Error(), "looksRight") {
		t.Errorf("refusal does not name the guard: %v", err)
	}
}

// Changing which guard it is drops the parameters, because they belonged to a
// different schema — inRange's distance means nothing to healthAbove, and
// leaving it there writes a parameter that guard does not take.
func TestSetTransitionGuard_DropsTheParametersWhenTheGuardChanges(t *testing.T) {
	s, path := openTransitions(t)
	r := ref("idle", "on", "GO", 0)

	if err := s.SetGuardParam(path, r, "distance", "3"); err != nil {
		t.Fatalf("SetGuardParam: %v", err)
	}
	if err := s.SetTransitionGuard(path, r, "healthAbove"); err != nil {
		t.Fatalf("SetTransitionGuard: %v", err)
	}
	cond := transitionsOn(t, s, path, "idle", "GO")[0].Cond
	if cond.Params != nil {
		t.Errorf("params survived a change of guard: %v", cond.Params)
	}
}

func TestSetTransitionGuard_KeepsTheParametersWhenTheGuardIsUnchanged(t *testing.T) {
	s, path := openTransitions(t)
	r := ref("idle", "on", "GO", 0)

	if err := s.SetGuardParam(path, r, "distance", "3"); err != nil {
		t.Fatalf("SetGuardParam: %v", err)
	}
	if err := s.SetTransitionGuard(path, r, "inRange"); err != nil {
		t.Fatalf("SetTransitionGuard: %v", err)
	}
	if got := transitionsOn(t, s, path, "idle", "GO")[0].Cond.Params["distance"]; got != float64(3) {
		t.Errorf("distance = %v, want 3", got)
	}
}

// ── the guard's parameters ────────────────────────────────────────────────────

func TestSetGuardParam_ConvertsByTheRegistrysDeclaredType(t *testing.T) {
	s, path := openTransitions(t)
	r := ref("idle", "on", "GO", 0)

	if err := s.SetGuardParam(path, r, "distance", "4"); err != nil {
		t.Fatalf("SetGuardParam: %v", err)
	}
	if err := s.SetGuardParam(path, r, "target", "$player"); err != nil {
		t.Fatalf("SetGuardParam: %v", err)
	}
	params := transitionsOn(t, s, path, "idle", "GO")[0].Cond.Params
	if params["distance"] != float64(4) {
		t.Errorf("distance = %#v, want the number 4", params["distance"])
	}
	if params["target"] != "$player" {
		t.Errorf("target = %#v", params["target"])
	}
}

func TestSetGuardParam_RefusesANumberThatIsNotOne(t *testing.T) {
	s, path := openTransitions(t)

	if err := s.SetGuardParam(path, ref("idle", "on", "GO", 0), "distance", "quite"); err == nil {
		t.Error("a distance of \"quite\" was accepted")
	}
}

func TestSetGuardParam_RefusesAParameterTheGuardDoesNotTake(t *testing.T) {
	s, path := openTransitions(t)

	if err := s.SetGuardParam(path, ref("idle", "on", "GO", 0), "threshold", "3"); err == nil {
		t.Error("inRange accepted healthAbove's parameter")
	}
}

func TestSetGuardParam_RefusesAParameterOnATransitionWithNoGuard(t *testing.T) {
	s, path := openTransitions(t)

	err := s.SetGuardParam(path, ref("idle", "on", "GO", 1), "distance", "3")
	if err == nil {
		t.Fatal("a parameter was set on a transition that has no guard")
	}
	// In those words. Letting it fall through to the schema lookup refuses it
	// too, with `"" does not take a parameter called "distance"` — which names
	// the empty guard it invented rather than the one that is missing.
	if !strings.Contains(err.Error(), "no guard") {
		t.Errorf("the refusal does not say the guard is missing: %v", err)
	}
}

// Clearing the last parameter puts the guard back to the shorthand, exactly as
// clearing an action's last parameter does — the same code, so it cannot drift.
func TestSetGuardParam_ClearingTheLastOneRestoresTheShorthand(t *testing.T) {
	s, path := openTransitions(t)
	r := ref("idle", "on", "GO", 0)

	if err := s.SetGuardParam(path, r, "distance", "4"); err != nil {
		t.Fatalf("SetGuardParam: %v", err)
	}
	if err := s.SetGuardParam(path, r, "distance", ""); err != nil {
		t.Fatalf("SetGuardParam: %v", err)
	}
	cond := transitionsOn(t, s, path, "idle", "GO")[0].Cond
	if cond.Params != nil {
		t.Errorf("params = %v, want none", cond.Params)
	}
	if !cond.Bare {
		t.Error("the guard stayed in object form after its last parameter went")
	}
}

// ── the transition's actions ──────────────────────────────────────────────────

func TestAddTransitionAction_AppendsARegisteredActionBare(t *testing.T) {
	s, path := openTransitions(t)
	r := ref("idle", "on", "POKE", 0)

	if err := s.AddTransitionAction(path, r, "dealDamage"); err != nil {
		t.Fatalf("AddTransitionAction: %v", err)
	}
	got := transitionsOn(t, s, path, "idle", "POKE")[0].Actions
	if len(got) != 2 || got[0].Type != "log" || got[1].Type != "dealDamage" {
		t.Fatalf("actions = %+v", got)
	}
	if !got[1].Bare || got[1].Params != nil {
		t.Error("a new action arrived carrying parameters nobody set")
	}
}

func TestAddTransitionAction_RefusesOneTheEngineDoesNotRegister(t *testing.T) {
	s, path := openTransitions(t)

	if err := s.AddTransitionAction(path, ref("idle", "on", "POKE", 0), "explode"); err == nil {
		t.Error("an unregistered action was accepted")
	}
}

func TestRemoveTransitionAction_TakesTheOneNamed(t *testing.T) {
	s, path := openTransitions(t)
	r := ref("idle", "on", "POKE", 0)

	if err := s.AddTransitionAction(path, r, "dealDamage"); err != nil {
		t.Fatalf("AddTransitionAction: %v", err)
	}
	if err := s.RemoveTransitionAction(path, r, 0); err != nil {
		t.Fatalf("RemoveTransitionAction: %v", err)
	}
	got := transitionsOn(t, s, path, "idle", "POKE")[0].Actions
	if len(got) != 1 || got[0].Type != "dealDamage" {
		t.Errorf("actions = %+v, want only dealDamage", got)
	}
}

func TestRemoveTransitionAction_RefusesAnIndexThatIsNotThere(t *testing.T) {
	s, path := openTransitions(t)

	if err := s.RemoveTransitionAction(path, ref("idle", "on", "POKE", 0), 4); err == nil {
		t.Error("an index past the end was accepted")
	}
}

func TestSetTransitionActionParam_WritesByTheRegistrysType(t *testing.T) {
	s, path := openTransitions(t)
	r := ref("idle", "on", "POKE", 0)

	if err := s.AddTransitionAction(path, r, "dealDamage"); err != nil {
		t.Fatalf("AddTransitionAction: %v", err)
	}
	if err := s.SetTransitionActionParam(path, r, 1, "amount", "7"); err != nil {
		t.Fatalf("SetTransitionActionParam: %v", err)
	}
	got := transitionsOn(t, s, path, "idle", "POKE")[0].Actions[1].Params["amount"]
	if got != float64(7) {
		t.Errorf("amount = %#v, want the number 7", got)
	}
}

// ── the order, which is the logic ─────────────────────────────────────────────

func TestMoveTransition_SwapsWithItsNeighbourAndSaysWhereItWent(t *testing.T) {
	s, path := openTransitions(t)

	got, err := s.MoveTransition(path, ref("idle", "on", "GO", 0), 1)
	if err != nil {
		t.Fatalf("MoveTransition: %v", err)
	}
	if got != ref("idle", "on", "GO", 1) {
		t.Errorf("new ref = %+v, want index 1", got)
	}
	list := transitionsOn(t, s, path, "idle", "GO")
	if list[0].Target != "resting" || list[1].Target != "combat" {
		t.Errorf("order = %q,%q", list[0].Target, list[1].Target)
	}
}

func TestMoveTransition_RefusesToMoveOffEitherEnd(t *testing.T) {
	s, path := openTransitions(t)

	if _, err := s.MoveTransition(path, ref("idle", "on", "GO", 0), -1); err == nil {
		t.Error("the first transition was moved up")
	}
	if _, err := s.MoveTransition(path, ref("idle", "on", "GO", 1), 1); err == nil {
		t.Error("the last transition was moved down")
	}
	list := transitionsOn(t, s, path, "idle", "GO")
	if list[0].Target != "combat" {
		t.Errorf("a refused move landed anyway: %q is first", list[0].Target)
	}
}

// ── addressing ────────────────────────────────────────────────────────────────

func TestTransitionEdits_RefuseAReferenceThatNamesNothing(t *testing.T) {
	s, path := openTransitions(t)

	for name, r := range map[string]machines.TransitionRef{
		"no such state": ref("nowhere", "on", "GO", 0),
		"no such event": ref("idle", "on", "NOPE", 0),
		"no such index": ref("idle", "on", "GO", 9),
		"no such kind":  ref("idle", "sideways", "GO", 0),
	} {
		if err := s.SetTransitionTarget(path, r, "resting"); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// A state path is resolved exactly, never searched for: the panel is addressing
// the transition the canvas named.
func TestTransitionEdits_ResolveAStatePathExactly(t *testing.T) {
	s, path := openTransitions(t)

	// "attacking" is a real state, but only as combat.attacking.
	if err := s.SetTransitionTarget(path, ref("attacking", "on", "GO", 0), "resting"); err == nil {
		t.Error("a bare name resolved to a nested state")
	}
}

// Every one of these changes the session and none of them touches the file:
// saving is the footer's job, and an edit that wrote on every keystroke would
// make Discard a lie.
func TestTransitionEdits_ChangeTheSessionAndNotTheFile(t *testing.T) {
	s, dir := open(t, map[string]string{"nested.json": transitionMachine})
	path := machinePath(t, dir)
	before := onDisk(t, path)
	r := ref("idle", "on", "GO", 0)

	if err := s.SetTransitionTarget(path, r, "resting"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTransitionGuard(path, r, "atTarget"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveTransition(path, r, 1); err != nil {
		t.Fatal(err)
	}
	if onDisk(t, path) != before {
		t.Error("a transition edit wrote the file")
	}
}
