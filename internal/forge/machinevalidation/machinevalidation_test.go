package machinevalidation_test

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
	mv "github.com/tmbritton/ecs-db/internal/forge/machinevalidation"
	"github.com/tmbritton/ecs-db/internal/forge/validation"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// broken carries one of most kinds of error the engine reports, including two
// states with the same leaf name so that attribution by StateID alone would put
// a message on the wrong node.
const broken = `{
  "id": "m",
  "initial": "idle",
  "states": {
    "idle": {
      "entry": ["noSuchAction"],
      "on": {
        "GO": [{ "target": "nowhere" }],
        "POKE": [{ "target": "resting", "cond": "noSuchGuard" }],
        "HIT": [{ "target": "resting", "actions": ["alsoMissing"] }]
      },
      "after": { "soon": [{ "target": "resting" }] }
    },
    "combat": {
      "initial": "attacking",
      "states": {
        "attacking": {},
        "idle": { "entry": ["onlyHere"] }
      }
    },
    "resting": {}
  }
}`

func check(t *testing.T, src string) (mv.Report, chart.Chart) {
	t.Helper()
	def, err := agent.ParseMachine([]byte(src))
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	errs := agent.ValidateMachine(def, agent.NewRegistry(), schema.DatabaseSchema{})
	c := chart.Build(def, "")
	return mv.Check(def, errs, c), c
}

func messages(problems []validation.Problem) string {
	var out []string
	for _, p := range problems {
		out = append(out, p.Message)
	}
	return strings.Join(out, " | ")
}

// ── attribution ───────────────────────────────────────────────────────────────

// The two "idle" states share one StateID and are different nodes. Attribution
// by id would put both messages on both, or on whichever was found first.
func TestCheck_TellsTwoStatesOfTheSameNameApart(t *testing.T) {
	report, _ := check(t, broken)

	top := messages(report.ForState("idle"))
	nested := messages(report.ForState("combat.idle"))
	if !strings.Contains(top, "noSuchAction") || strings.Contains(top, "onlyHere") {
		t.Errorf("idle carries %q", top)
	}
	if !strings.Contains(nested, "onlyHere") || strings.Contains(nested, "noSuchAction") {
		t.Errorf("combat.idle carries %q", nested)
	}
}

// A transition's error belongs to the edge that runs it, not to the state it
// leaves — which is where it would land if the edge were not looked for.
func TestCheck_AttachesATransitionsErrorToItsEdge(t *testing.T) {
	report, _ := check(t, broken)

	for _, tc := range []struct{ id, want string }{
		{"idle|on|GO|0", "nowhere"},
		{"idle|on|POKE|0", "noSuchGuard"},
		{"idle|on|HIT|0", "alsoMissing"},
		{"idle|after|soon|0", "soon"},
	} {
		if got := messages(report.ForEdge(tc.id)); !strings.Contains(got, tc.want) {
			t.Errorf("edge %s carries %q, want it to mention %q", tc.id, got, tc.want)
		}
		if got := messages(report.ForState("idle")); strings.Contains(got, tc.want) {
			t.Errorf("the state also carries the edge's message %q: %s", tc.want, got)
		}
	}
}

// A machine with children and no initial is nobody's node.
func TestCheck_KeepsAMachineLevelErrorOnTheMachine(t *testing.T) {
	report, _ := check(t, `{"id": "m", "states": {"a": {}}}`)

	if !strings.Contains(messages(report.Machine), "no initial state") {
		t.Errorf("machine carries %q", messages(report.Machine))
	}
	if len(report.ForState("a")) != 0 {
		t.Errorf("the error was hung on an arbitrary node: %q", messages(report.ForState("a"))) //nolint:govet
	}
}

// Nothing is invisible: an error naming a state the chart never drew still goes
// somewhere rather than being dropped.
func TestCheck_KeepsAnErrorWithNowhereToGo(t *testing.T) {
	def, err := agent.ParseMachine([]byte(`{"id": "m", "initial": "a", "states": {"a": {}}}`))
	if err != nil {
		t.Fatal(err)
	}
	stray := agent.ValidationError{
		MachineID: "m", StatePath: "ghost", Field: "x", Message: "something about a state that is not drawn",
	}
	report := mv.Check(def, []agent.ValidationError{stray}, chart.Build(def, ""))

	if !strings.Contains(messages(report.Machine), "not drawn") {
		t.Errorf("an error naming no node was dropped; machine carries %q", messages(report.Machine))
	}
	if report.Count() != 1 {
		t.Errorf("Count = %d, want 1", report.Count())
	}
}

// Two states can leave transitions that fail the same way. An error belongs to
// the edge out of *its own* state, not to the first edge anywhere that happens
// to name the same thing.
func TestCheck_DoesNotAttachAnErrorToAnotherStatesEdge(t *testing.T) {
	report, _ := check(t, `{
	  "id": "m",
	  "initial": "first",
	  "states": {
	    "first":  { "on": { "GO": [{ "target": "nowhere" }] } },
	    "second": { "on": { "GO": [{ "target": "nowhere" }] } }
	  }
	}`)

	for _, id := range []string{"first|on|GO|0", "second|on|GO|0"} {
		if got := report.ForEdge(id); len(got) != 1 {
			t.Errorf("edge %s carries %d problems, want 1: %q", id, len(got), messages(got))
		}
	}
}

// An error with no Field is about the state, and an internal transition's
// target is the empty string — so matching on an empty Field would put every
// stateless error on the first internal transition out of that state.
func TestCheck_DoesNotAttachAFieldlessErrorToAnInternalTransition(t *testing.T) {
	report, _ := check(t, `{
	  "id": "m",
	  "initial": "outer",
	  "states": {
	    "outer": {
	      "states": { "inner": {} },
	      "on": { "POKE": [{}] }
	    }
	  }
	}`)

	// "state has child states but no initial state" carries no Field.
	if got := messages(report.ForState("outer")); !strings.Contains(got, "no initial state") {
		t.Errorf("outer carries %q", got)
	}
	if got := report.ForEdge("outer|on|POKE|0"); len(got) != 0 {
		t.Errorf("a stateless error was hung on an internal transition: %q", messages(got))
	}
}

// ── what the canvas and the footer ask ────────────────────────────────────────

func TestReport_SaysWhichNodesAndEdgesAreMarked(t *testing.T) {
	report, _ := check(t, broken)

	if !report.HasState("idle") || !report.HasState("combat.idle") {
		t.Error("a state carrying an error is not marked")
	}
	if report.HasState("resting") {
		t.Error("a state carrying nothing is marked")
	}
	if !report.HasEdge("idle|on|GO|0") {
		t.Error("an edge carrying an error is not marked")
	}
	if report.HasEdge("idle|on|POKE|0") == report.HasEdge("resting|on|NOPE|0") {
		t.Error("marking does not distinguish an edge that exists from one that does not")
	}
}

// Every one of these is a reason ValidateMachineError fails, which is the only
// thing that takes the Save button away.
func TestCheck_EveryProblemBlocksTheSave(t *testing.T) {
	report, _ := check(t, broken)

	if report.Count() == 0 {
		t.Fatal("the fixture produces no problems")
	}
	var seen int
	for _, group := range groups(report) {
		for _, p := range group {
			seen++
			if !p.Blocking {
				t.Errorf("a validation error is not blocking: %q", p.Message)
			}
			if p.Owner.Kind != validation.OwnerMachine || p.Owner.Name != "m" {
				t.Errorf("a problem is not owned by the machine: %+v", p.Owner)
			}
		}
	}
	if seen != report.Count() {
		t.Errorf("walked %d problems, Count says %d", seen, report.Count())
	}
}

// groups is every list of problems the report holds, which is what the three
// templates render one at a time.
func groups(r mv.Report) [][]validation.Problem {
	out := [][]validation.Problem{r.Machine}
	for _, ps := range r.States {
		out = append(out, ps)
	}
	for _, ps := range r.Edges {
		out = append(out, ps)
	}
	return out
}

func TestCheck_CountsEveryProblemOnce(t *testing.T) {
	report, _ := check(t, broken)

	var total int
	total += len(report.Machine)
	for _, ps := range report.States {
		total += len(ps)
	}
	for _, ps := range report.Edges {
		total += len(ps)
	}
	if total != report.Count() {
		t.Errorf("Count = %d, but the groups hold %d", report.Count(), total)
	}
}

// The lists are rendered on a two-second stream that suppresses a patch when
// the markup is unchanged, so each group has to hold the same problems in the
// same order every time — which it does by holding them in the order the engine
// reported them, and the engine reports them in the file's order.
func TestCheck_KeepsEachGroupInTheEnginesOrder(t *testing.T) {
	// idle declares GO, POKE, HIT and then after-soon, in that order.
	want := []string{"nowhere", "noSuchGuard", "alsoMissing", "soon"}

	for run := 0; run < 6; run++ {
		report, _ := check(t, broken)
		var got []string
		for _, id := range []string{"idle|on|GO|0", "idle|on|POKE|0", "idle|on|HIT|0", "idle|after|soon|0"} {
			for _, p := range report.ForEdge(id) {
				got = append(got, p.Field)
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("run %d: %v, want %v", run, got, want)
		}
	}
}

// Two problems on one state come out in the order the engine found them, which
// is the order the file declares the actions in.
func TestCheck_KeepsTwoProblemsOnOneStateInOrder(t *testing.T) {
	report, _ := check(t, `{
	  "id": "m",
	  "initial": "idle",
	  "states": { "idle": { "entry": ["zzz", "aaa"] } }
	}`)

	var got []string
	for _, p := range report.ForState("idle") {
		got = append(got, p.Field)
	}
	if strings.Join(got, ",") != "zzz,aaa" {
		t.Errorf("problems = %v, want the file's order zzz,aaa", got)
	}
}

// An empty machine has nothing to say, and says it without a nil map.
func TestCheck_SaysNothingAboutAValidMachine(t *testing.T) {
	def, err := agent.ParseMachine([]byte(`{"id": "m", "initial": "a", "states": {"a": {}}}`))
	if err != nil {
		t.Fatal(err)
	}
	report := mv.Check(def, nil, chart.Build(def, ""))

	if report.Count() != 0 || report.HasState("a") {
		t.Errorf("a valid machine reports %+v", report)
	}
}

// A state's own error must not be filed under one of its transitions, even when
// the two name the same thing — an entry action and a transition action are the
// same action type on the same state, and Field alone cannot tell them apart.
func TestCheck_KeepsAStatesOwnErrorOnTheState(t *testing.T) {
	report, _ := check(t, `{
	  "id": "m",
	  "initial": "a",
	  "states": {
	    "a": { "entry": ["boom"], "on": { "GO": [{ "target": "b", "actions": ["boom"] }] } },
	    "b": {}
	  }
	}`)

	if !report.HasState("a") {
		t.Error("the state carrying the bad entry action is not marked")
	}
	if got := messages(report.ForState("a")); !strings.Contains(got, "entry action") {
		t.Errorf("state a carries %q", got)
	}
	// And the transition's own copy is still on the edge.
	if got := messages(report.ForEdge("a|on|GO|0")); !strings.Contains(got, "transition action") {
		t.Errorf("edge carries %q", got)
	}
}

// The same, for a compound state whose initial names something a sibling
// transition happens to target.
func TestCheck_KeepsAStatesInitialErrorOnTheState(t *testing.T) {
	report, _ := check(t, `{
	  "id": "m",
	  "initial": "outer",
	  "states": {
	    "outer": {
	      "initial": "gone",
	      "states": { "inner": {} },
	      "on": { "GO": [{ "target": "gone" }] }
	    }
	  }
	}`)

	if !report.HasState("outer") {
		t.Error("the compound state with no valid initial is not marked")
	}
	if got := messages(report.ForState("outer")); !strings.Contains(got, "is not a child state") {
		t.Errorf("outer carries %q", got)
	}
}
