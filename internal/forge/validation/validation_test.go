package validation_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/forge/validation"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// sound is a schema every test starts from: two components, two entity types,
// nothing wrong with it.
func sound() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 3,
		Components: map[string]schema.Component{
			"Position": {
				Type:          schema.ComponentTypeObject,
				Properties:    map[string]schema.Property{"x": {Type: schema.PropertyTypeNumber}},
				PropertyOrder: []string{"x"},
			},
			"Health": {
				Type:          schema.ComponentTypeObject,
				Properties:    map[string]schema.Property{"hp": {Type: schema.PropertyTypeInteger}},
				PropertyOrder: []string{"hp"},
			},
		},
		ComponentOrder: []string{"Position", "Health"},
		EntityTypes: map[string]schema.EntityType{
			"Goblin": {RequiredComponents: []string{"Position"}, ValidationLevel: schema.ValidationStrict},
			"Rock":   {RequiredComponents: []string{"Position"}, ValidationLevel: schema.ValidationStrict},
		},
		// Authored order, deliberately not alphabetical: a report that sorted
		// its problems instead of following the file would pass unnoticed
		// against a fixture whose two orders agree.
		EntityTypeOrder: []string{"Rock", "Goblin"},
	}
}

func machine(id string, context map[string]any) project.Machine {
	return project.Machine{ID: id, Definition: &agent.MachineDefinition{ID: id, Context: context}}
}

func messages(problems []validation.Problem) string {
	var b strings.Builder
	for _, p := range problems {
		b.WriteString(p.Message)
		b.WriteString("\n")
	}
	return b.String()
}

func TestCheck_SaysNothingAboutASoundSchema(t *testing.T) {
	got := validation.Check(validation.Input{Schema: sound()})
	if len(got.Problems) != 0 {
		t.Fatalf("expected no problems, got:\n%s", messages(got.Problems))
	}
	if got.Blocked() {
		t.Error("a sound schema must not block the save")
	}
	if got.Partial {
		t.Error("nothing was cut short, so the report is not partial")
	}
}

func TestCheck_AttributesProblemsToTheirOwner(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*schema.DatabaseSchema)
		wantKind validation.OwnerKind
		wantName string
		wantText string
	}{
		{
			name: "an entity type requiring a component that does not exist",
			mutate: func(s *schema.DatabaseSchema) {
				et := s.EntityTypes["Goblin"]
				et.RequiredComponents = []string{"Position", "Nope"}
				s.EntityTypes["Goblin"] = et
			},
			wantKind: validation.OwnerEntityType, wantName: "Goblin",
			wantText: "undeclared component",
		},
		{
			name: "a component that is both required and optional",
			mutate: func(s *schema.DatabaseSchema) {
				et := s.EntityTypes["Rock"]
				et.OptionalComponents = []string{"Position"}
				s.EntityTypes["Rock"] = et
			},
			wantKind: validation.OwnerEntityType, wantName: "Rock",
			wantText: "both requiredComponents and optionalComponents",
		},
		{
			name: "a validation level the engine does not know",
			mutate: func(s *schema.DatabaseSchema) {
				et := s.EntityTypes["Rock"]
				et.ValidationLevel = "whenever"
				s.EntityTypes["Rock"] = et
			},
			wantKind: validation.OwnerEntityType, wantName: "Rock",
			wantText: "invalid validationLevel",
		},
		{
			name: "a component named Behavior",
			mutate: func(s *schema.DatabaseSchema) {
				s.Components["Behavior"] = s.Components["Health"]
				s.ComponentOrder = append(s.ComponentOrder, "Behavior")
			},
			wantKind: validation.OwnerComponent, wantName: "Behavior",
			wantText: "reserved component name",
		},
		{
			name: "a component shape with no SQL mapping",
			mutate: func(s *schema.DatabaseSchema) {
				c := s.Components["Health"]
				c.Type = "quaternion"
				s.Components["Health"] = c
			},
			wantKind: validation.OwnerComponent, wantName: "Health",
			wantText: "no SQL mapping",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := sound()
			tc.mutate(&s)
			got := validation.Check(validation.Input{Schema: s})

			if !got.Blocked() {
				t.Fatalf("expected the save to be blocked, got:\n%s", messages(got.Problems))
			}
			if !got.Partial {
				t.Error("ValidateSchema stops at the first failure, so the report is partial")
			}
			owned := got.For(tc.wantKind, tc.wantName)
			if len(owned) != 1 {
				t.Fatalf("want 1 problem on %q, got %d; whole report:\n%s",
					tc.wantName, len(owned), messages(got.Problems))
			}
			if !strings.Contains(owned[0].Message, tc.wantText) {
				t.Errorf("message %q does not mention %q", owned[0].Message, tc.wantText)
			}
			if !owned[0].Blocking {
				t.Error("the engine would refuse this schema, so the problem blocks")
			}
			if n := len(got.Problems); n != 1 {
				t.Errorf("one defect should produce one problem, got %d:\n%s", n, messages(got.Problems))
			}
		})
	}
}

// The point of narrowing: ValidateSchema returns one error for a whole file, so
// without it a second broken entity type is invisible until the first is fixed.
func TestCheck_ReportsEveryBrokenEntityTypeAtOnce(t *testing.T) {
	s := sound()
	for _, name := range []string{"Goblin", "Rock"} {
		et := s.EntityTypes[name]
		et.RequiredComponents = []string{"Missing" + name}
		s.EntityTypes[name] = et
	}

	got := validation.Check(validation.Input{Schema: s})
	if len(got.Problems) != 2 {
		t.Fatalf("want both entity types reported, got %d:\n%s", len(got.Problems), messages(got.Problems))
	}
	// Authored order, not map order and not sorted: two runs must produce the
	// same page, and it must be the page the file is arranged as.
	if got.Problems[0].Owner.Name != "Rock" || got.Problems[1].Owner.Name != "Goblin" {
		t.Errorf("want authored order Rock then Goblin, got %q then %q",
			got.Problems[0].Owner.Name, got.Problems[1].Owner.Name)
	}
	if !strings.Contains(got.Problems[0].Message, "MissingRock") {
		t.Errorf("the first problem names the wrong component: %q", got.Problems[0].Message)
	}
	if !strings.Contains(got.Problems[1].Message, "MissingGoblin") {
		t.Errorf("the second problem names the wrong component: %q", got.Problems[1].Message)
	}
}

// The same, for the other half of the file. Components do not short-circuit
// against each other — only against the entity types that refer to them.
func TestCheck_ReportsEveryBrokenComponentAtOnce(t *testing.T) {
	s := sound()
	for _, name := range []string{"Position", "Health"} {
		c := s.Components[name]
		c.Type = "quaternion"
		s.Components[name] = c
	}

	got := validation.Check(validation.Input{Schema: s})
	if len(got.Problems) != 2 {
		t.Fatalf("want both components reported, got %d:\n%s", len(got.Problems), messages(got.Problems))
	}
	if got.Problems[0].Owner.Name != "Position" || got.Problems[1].Owner.Name != "Health" {
		t.Errorf("want authored order Position then Health, got %q then %q",
			got.Problems[0].Owner.Name, got.Problems[1].Owner.Name)
	}
}

// A rule about the file as a whole has no owner to blame, and blaming every
// component for it would be worse than saying it once.
func TestCheck_LeavesFileWideProblemsUnattached(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*schema.DatabaseSchema)
		wantText string
	}{
		{
			name:     "a schemaVersion below one",
			mutate:   func(s *schema.DatabaseSchema) { s.SchemaVersion = 0 },
			wantText: "schemaVersion",
		},
		{
			name: "no components at all",
			mutate: func(s *schema.DatabaseSchema) {
				s.Components = map[string]schema.Component{}
				s.ComponentOrder = nil
			},
			wantText: "at least one component",
		},
		{
			name: "no entity types at all",
			mutate: func(s *schema.DatabaseSchema) {
				s.EntityTypes = map[string]schema.EntityType{}
				s.EntityTypeOrder = nil
			},
			wantText: "at least one entity type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := sound()
			tc.mutate(&s)
			got := validation.Check(validation.Input{Schema: s})

			if len(got.Problems) != 1 {
				t.Fatalf("want exactly one problem, got %d:\n%s", len(got.Problems), messages(got.Problems))
			}
			if len(got.General()) != 1 {
				t.Fatalf("want the problem unattached, got owner %+v", got.Problems[0].Owner)
			}
			if !strings.Contains(got.Problems[0].Message, tc.wantText) {
				t.Errorf("message %q does not mention %q", got.Problems[0].Message, tc.wantText)
			}
			if !got.Blocked() {
				t.Error("the engine would refuse this schema")
			}
		})
	}
}

// ── behaviour bindings ──────────────────────────────────────────────────────

func behaviorsDir(t *testing.T, ids ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, id := range ids {
		path := filepath.Join(dir, id+".json")
		if err := os.WriteFile(path, []byte(`{"id":"`+id+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCheck_ReportsABindingWithNoMachineFile(t *testing.T) {
	s := sound()
	et := s.EntityTypes["Goblin"]
	et.Behavior = "wander"
	s.EntityTypes["Goblin"] = et
	comp := s.Components["Health"]
	comp.Behavior = "bleed"
	s.Components["Health"] = comp

	got := validation.Check(validation.Input{
		Schema:       s,
		BehaviorDirs: []string{behaviorsDir(t)},
	})

	// A warning, not an error. schema.ValidateBehaviorRefs has no caller in the
	// engine and nothing outside internal/schema and internal/forge reads the
	// Behavior field, so the game starts and the entity simply does nothing.
	// Taking Save away for it would refuse a file the engine accepts — and
	// would do so on merely opening a project someone else broke.
	if got.Blocked() {
		t.Fatalf("a missing behaviour file blocked the save; the engine does not check\n"+
			"this at all today. Problems:\n%s", messages(got.Problems))
	}
	for _, tc := range []struct {
		kind validation.OwnerKind
		name string
		want string
	}{
		{validation.OwnerEntityType, "Goblin", "wander"},
		{validation.OwnerComponent, "Health", "bleed"},
	} {
		on := got.At(tc.kind, tc.name, validation.FieldBehavior)
		if len(on) != 1 {
			t.Fatalf("want 1 problem on %q's behavior field, got %d:\n%s",
				tc.name, len(on), messages(got.Problems))
		}
		if !strings.Contains(on[0].Message, tc.want) {
			t.Errorf("message %q does not name the machine %q", on[0].Message, tc.want)
		}
	}
}

// A binding with nowhere at all to resolve. The engine's own answer names
// "behaviorsDir", which is a parameter of a Go function and not anything the
// user can edit.
func TestCheck_PointsAtGameTomlWhenNoModDeclaresABehavioursDirectory(t *testing.T) {
	s := sound()
	et := s.EntityTypes["Goblin"]
	et.Behavior = "wander"
	s.EntityTypes["Goblin"] = et

	got := validation.Check(validation.Input{Schema: s})
	on := got.At(validation.OwnerEntityType, "Goblin", validation.FieldBehavior)
	if len(on) != 1 {
		t.Fatalf("want 1 problem, got %d:\n%s", len(on), messages(got.Problems))
	}
	if strings.Contains(on[0].Message, "behaviorsDir") {
		t.Errorf("the message names an internal parameter rather than the file the\n"+
			"user would edit: %q", on[0].Message)
	}
	for _, want := range []string{"game.toml", "behaviors", "wander"} {
		if !strings.Contains(on[0].Message, want) {
			t.Errorf("message %q does not mention %q", on[0].Message, want)
		}
	}
}

// A name with a path separator is rejected before the filesystem is touched, so
// the complaint is not about any directory and must not claim a search.
func TestCheck_DoesNotClaimASearchThatNeverHappened(t *testing.T) {
	s := sound()
	et := s.EntityTypes["Goblin"]
	et.Behavior = "sub/wander"
	s.EntityTypes["Goblin"] = et

	core, extra := behaviorsDir(t), behaviorsDir(t)
	got := validation.Check(validation.Input{Schema: s, BehaviorDirs: []string{core, extra}})
	on := got.At(validation.OwnerEntityType, "Goblin", validation.FieldBehavior)
	if len(on) != 1 {
		t.Fatalf("want 1 problem, got %d:\n%s", len(on), messages(got.Problems))
	}
	if !strings.Contains(on[0].Message, "path separators") {
		t.Fatalf("want the engine's own complaint, got %q", on[0].Message)
	}
	if strings.Contains(on[0].Message, "searched") {
		t.Errorf("a name rejected before any directory was read still claims a search:\n%s",
			on[0].Message)
	}
}

func TestCheck_AcceptsABindingSuppliedByAnyMod(t *testing.T) {
	s := sound()
	et := s.EntityTypes["Goblin"]
	et.Behavior = "wander"
	s.EntityTypes["Goblin"] = et

	// core has no such machine; the later mod supplies it. The loader accepts a
	// machine contributed by any mod, so this binding resolves.
	got := validation.Check(validation.Input{
		Schema:       s,
		BehaviorDirs: []string{behaviorsDir(t), behaviorsDir(t, "wander")},
		Machines:     []project.Machine{machine("wander", nil)},
	})
	if len(got.Problems) != 0 {
		t.Fatalf("expected no problems, got:\n%s", messages(got.Problems))
	}
}

func TestCheck_NamesEveryDirectorySearched(t *testing.T) {
	s := sound()
	et := s.EntityTypes["Goblin"]
	et.Behavior = "wander"
	s.EntityTypes["Goblin"] = et

	// Three, so "all of them" and "all but the first" are different answers.
	dirs := []string{behaviorsDir(t), behaviorsDir(t), behaviorsDir(t)}
	got := validation.Check(validation.Input{Schema: s, BehaviorDirs: dirs})

	on := got.At(validation.OwnerEntityType, "Goblin", validation.FieldBehavior)
	if len(on) != 1 {
		t.Fatalf("want 1 problem, got %d:\n%s", len(on), messages(got.Problems))
	}

	// The searched list itself, not the whole message. The engine's own
	// complaint quotes one of these paths inside it, so asserting on the message
	// is satisfied by that directory whether or not it was listed — which is how
	// a list that dropped its first entry passed this test.
	_, list, found := strings.Cut(on[0].Message, "(searched ")
	if !found {
		t.Fatalf("no searched list in %q", on[0].Message)
	}
	list = strings.TrimSuffix(list, ")")
	for i, dir := range dirs {
		if !strings.Contains(list, dir) {
			t.Errorf("directory %d of %d is missing from the searched list %q — the message\n"+
				"then reads as though the others were the only places the file could\n"+
				"have lived", i, len(dirs), list)
		}
	}
}

// A file that is present but did not load is a different failure from one that
// is not there: the engine starts, and the entity simply does nothing.
func TestCheck_WarnsWhenTheFileIsThereButNoMachineLoaded(t *testing.T) {
	s := sound()
	et := s.EntityTypes["Goblin"]
	et.Behavior = "wander"
	s.EntityTypes["Goblin"] = et

	dir := behaviorsDir(t, "wander")
	got := validation.Check(validation.Input{
		Schema:       s,
		BehaviorDirs: []string{dir},
		Machines:     nil, // nothing resolved
		Problems: []project.Problem{
			{Path: filepath.Join(dir, "wander.json"), Err: errString("unknown guard \"canSee\"")},
		},
	})

	on := got.At(validation.OwnerEntityType, "Goblin", validation.FieldBehavior)
	if len(on) != 1 {
		t.Fatalf("want 1 problem, got %d:\n%s", len(on), messages(got.Problems))
	}
	if on[0].Blocking {
		t.Error("nothing in the engine refuses this, so Forge must not refuse the save")
	}
	if got.Blocked() {
		t.Error("a warning alone must not block the save")
	}
	if !strings.Contains(on[0].Message, "canSee") {
		t.Errorf("message %q does not say why the machine was rejected —\n"+
			"which is the whole reason the project's problem list is threaded in here", on[0].Message)
	}
}

// Problem.Path is sometimes a file and sometimes a whole behaviours directory,
// and a substring match reported a different machine's failure as this one's.
func TestCheck_DoesNotBlameOneMachineForAnothersFailure(t *testing.T) {
	s := sound()
	et := s.EntityTypes["Goblin"]
	et.Behavior = "wander"
	s.EntityTypes["Goblin"] = et
	dir := behaviorsDir(t, "wander")

	tests := []struct {
		name    string
		problem project.Problem
	}{
		{
			// wanderer.json contains "wander" as a substring and is a different file.
			name:    "a different file whose name contains this id",
			problem: project.Problem{Path: filepath.Join(dir, "wanderer.json"), Err: errString(`guard "nope" is not registered`)},
		},
		{
			// A mod directory named after its headline machine is not exotic.
			name:    "a problem about the directory rather than a file",
			problem: project.Problem{Path: dir, Err: errString(`machine "chase" is declared by 2 files`)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validation.Check(validation.Input{
				Schema:       s,
				BehaviorDirs: []string{dir},
				Problems:     []project.Problem{tc.problem},
			})
			on := got.At(validation.OwnerEntityType, "Goblin", validation.FieldBehavior)
			if len(on) != 1 {
				t.Fatalf("want 1 problem, got %d:\n%s", len(on), messages(got.Problems))
			}
			if strings.Contains(on[0].Message, tc.problem.Err.Error()) {
				t.Errorf("wander's binding is explained by something that is not about\n"+
					"wander: %s", on[0].Message)
			}
		})
	}
}

func TestCheck_SaysWhereToLookWhenTheProjectRecordedNoReason(t *testing.T) {
	s := sound()
	et := s.EntityTypes["Goblin"]
	et.Behavior = "wander"
	s.EntityTypes["Goblin"] = et

	got := validation.Check(validation.Input{
		Schema:       s,
		BehaviorDirs: []string{behaviorsDir(t, "wander")},
	})
	on := got.At(validation.OwnerEntityType, "Goblin", validation.FieldBehavior)
	if len(on) != 1 {
		t.Fatalf("want 1 problem, got %d:\n%s", len(on), messages(got.Problems))
	}
	if !strings.Contains(on[0].Message, `"id"`) {
		t.Errorf("message %q does not point at the id inside the file, which is what\n"+
			"the binding actually matches", on[0].Message)
	}
	// It no longer blames Forge's *startup* — the list re-resolves on every
	// mutation now — but an edit made outside Forge is still a real cause,
	// because nothing watches the behaviours directory.
	if strings.Contains(on[0].Message, "started") {
		t.Errorf("message %q still blames a list read once at startup, which stopped\n"+
			"being true when the list started refreshing", on[0].Message)
	}
	if !strings.Contains(on[0].Message, "outside Forge") {
		t.Errorf("message %q states one cause as though it were the only one: a file\n"+
			"edited in a text editor reaches this too", on[0].Message)
	}
}

func TestCheck_IgnoresAnEmptyBinding(t *testing.T) {
	got := validation.Check(validation.Input{Schema: sound(), BehaviorDirs: []string{behaviorsDir(t)}})
	if len(got.Problems) != 0 {
		t.Fatalf("nothing is bound, so there is nothing to resolve; got:\n%s", messages(got.Problems))
	}
}

// ── the ambiguity warning ───────────────────────────────────────────────────

// ambiguous is sound() with "hp" added to a second component, which is the edit
// the warning exists to catch.
func ambiguous() schema.DatabaseSchema {
	s := sound()
	c := s.Components["Position"]
	c.Properties["hp"] = schema.Property{Type: schema.PropertyTypeInteger}
	c.PropertyOrder = append(c.PropertyOrder, "hp")
	s.Components["Position"] = c
	return s
}

func TestCheck_WarnsWhenAContextKeyBecomesAmbiguous(t *testing.T) {
	got := validation.Check(validation.Input{
		Schema:   ambiguous(),
		Machines: []project.Machine{machine("wander", map[string]any{"hp": 0})},
	})

	if got.Blocked() {
		t.Fatal("the schema is legal — refusing the save would be a rule the engine does not have")
	}
	if len(got.Problems) != 2 {
		t.Fatalf("want the warning on both components, got %d:\n%s", len(got.Problems), messages(got.Problems))
	}
	for _, name := range []string{"Health", "Position"} {
		on := got.At(validation.OwnerComponent, name, validation.PropertyField("hp"))
		if len(on) != 1 {
			t.Fatalf("want the warning on %q's hp field, got %d", name, len(on))
		}
		for _, want := range []string{"Health", "Position", "hp", "wander"} {
			if !strings.Contains(on[0].Message, want) {
				t.Errorf("warning on %q does not name %q: %s", name, want, on[0].Message)
			}
		}
	}
}

func TestCheck_DoesNotWarnAboutFieldsNoMachineSeeds(t *testing.T) {
	// "hp" is declared twice, and nothing reads it as a context key.
	got := validation.Check(validation.Input{
		Schema:   ambiguous(),
		Machines: []project.Machine{machine("wander", map[string]any{"x": 0})},
	})
	if len(got.Problems) != 0 {
		t.Fatalf("two components may share a field name; only a machine seeding it makes\n"+
			"that a problem. Got:\n%s", messages(got.Problems))
	}
}

func TestCheck_DoesNotWarnWhenOneComponentDeclaresTheKey(t *testing.T) {
	got := validation.Check(validation.Input{
		Schema:   sound(),
		Machines: []project.Machine{machine("wander", map[string]any{"hp": 0})},
	})
	if len(got.Problems) != 0 {
		t.Fatalf("exactly one match is what the engine asks for; got:\n%s", messages(got.Problems))
	}
}

func TestCheck_SurvivesAMachineThatFailedToParse(t *testing.T) {
	got := validation.Check(validation.Input{
		Schema:   ambiguous(),
		Machines: []project.Machine{{ID: "wander"}}, // no Definition
	})
	if len(got.Problems) != 0 {
		t.Fatalf("a machine with no definition seeds no context; got:\n%s", messages(got.Problems))
	}
}

// The warning is a prediction, and this is the thing it predicts. If the engine
// ever stops rejecting an ambiguous key — or starts rejecting something else —
// this fails and the warning gets revisited, rather than quietly describing a
// failure that no longer happens.
func TestCheck_PredictsTheErrorTheEngineWouldRaise(t *testing.T) {
	s := ambiguous()
	def := &agent.MachineDefinition{
		ID:      "wander",
		Initial: "idle",
		Context: map[string]any{"hp": 0},
		States:  map[string]*agent.StateNode{"idle": {ID: "idle"}},
	}

	warned := validation.Check(validation.Input{
		Schema:   s,
		Machines: []project.Machine{{ID: def.ID, Definition: def}},
	})
	if len(warned.Problems) == 0 {
		t.Fatal("Forge said nothing about a schema the engine will not load a machine against")
	}

	errs := agent.ValidateMachine(def, agent.NewRegistry(), s)
	var found bool
	for _, e := range errs {
		if e.Field == "hp" && strings.Contains(e.Message, "ambiguous") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the engine no longer rejects this, so the warning is describing a\n"+
			"failure that does not happen. Engine said: %v", errs)
	}

	// And the other way: with the second declaration removed, neither complains.
	clean := sound()
	if got := validation.Check(validation.Input{
		Schema:   clean,
		Machines: []project.Machine{{ID: def.ID, Definition: def}},
	}); len(got.Problems) != 0 {
		t.Errorf("expected no problems, got:\n%s", messages(got.Problems))
	}
	for _, e := range agent.ValidateMachine(def, agent.NewRegistry(), clean) {
		if strings.Contains(e.Message, "ambiguous") {
			t.Errorf("engine still calls it ambiguous: %v", e)
		}
	}
}

// ── the report itself ───────────────────────────────────────────────────────

func TestReport_CountsAndBlocking(t *testing.T) {
	r := validation.Report{Problems: []validation.Problem{
		{Message: "a", Blocking: true},
		{Message: "b"},
		{Message: "c"},
	}}
	errs, warns := r.Counts()
	if errs != 1 || warns != 2 {
		t.Errorf("want 1 error and 2 warnings, got %d and %d", errs, warns)
	}
	if !r.Blocked() {
		t.Error("one blocking problem blocks")
	}
	if (validation.Report{Problems: []validation.Problem{{Message: "b"}}}).Blocked() {
		t.Error("warnings do not block")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
