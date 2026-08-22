// Properties that must hold over every schema, not just the ones a test author
// thought of. The narrowing in structuralProblems asks the engine about smaller
// pieces of a schema, and the risk of that is subtle: a carrier that reports a
// problem the engine would not (a control the user cannot get past for a reason
// that is not real), or one that misses a problem the engine would report (a
// Save button that fails when pressed). Neither is likely to be found by
// enumerating cases by hand.
package validation_test

import (
	"math/rand"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/project"

	"github.com/tmbritton/ecs-db/internal/forge/validation"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// randomSchema builds a schema from the shapes the editor can produce plus the
// ones only a hand-edited file can — an unsupported component type, a validation
// level the engine does not know, a version below one, a component named
// Behavior.
func randomSchema(r *rand.Rand) schema.DatabaseSchema {
	names := []string{"Position", "Health", "Behavior", "Sprite", "Anchor"}
	goodTypes := []string{"object", "array", "entity-ref", "string", "integer", "number", "boolean"}
	badTypes := []string{"quaternion", "", "Object"}
	goodLevels := []schema.ValidationLevel{"strict", "warning"}
	badLevels := []schema.ValidationLevel{"", "whenever", "STRICT"}

	// Half the corpus is drawn to be mostly sound and half is drawn wild. A
	// generator that is wrong nine times out of ten produces almost no valid
	// schemas, and the agreement it then demonstrates is agreement about
	// rejection only — which is the half that was never in doubt.
	tame := r.Intn(2) == 0
	pick := func(good, bad []string) string {
		if tame || r.Intn(4) > 0 {
			return good[r.Intn(len(good))]
		}
		return bad[r.Intn(len(bad))]
	}

	s := schema.DatabaseSchema{
		Components:  map[string]schema.Component{},
		EntityTypes: map[string]schema.EntityType{},
	}
	if tame {
		s.SchemaVersion = 1 + r.Intn(3)
	} else {
		s.SchemaVersion = r.Intn(4) - 1
	}

	count := 1 + r.Intn(3)
	if !tame {
		count = r.Intn(4)
	}
	for i := 0; i < count; i++ {
		n := names[r.Intn(len(names))]
		if tame {
			// "Behavior" is reserved, so a tame schema does not reach for it.
			n = names[r.Intn(len(names)-1)]
			if n == "Behavior" {
				n = "Position"
			}
		}
		s.Components[n] = schema.Component{
			Type:       pick(goodTypes, badTypes),
			Properties: map[string]schema.Property{"f": {Type: "number"}},
		}
		s.ComponentOrder = append(s.ComponentOrder, n)
	}

	declared := append([]string(nil), s.ComponentOrder...)
	refFrom := func() string {
		// A tame schema refers to components it declared; a wild one refers to
		// whatever, which is how a dangling reference gets into the corpus.
		if tame && len(declared) > 0 {
			return declared[r.Intn(len(declared))]
		}
		return names[r.Intn(len(names))]
	}

	types := 1 + r.Intn(2)
	if !tame {
		types = r.Intn(3)
	}
	for i := 0; i < types; i++ {
		n := "T" + string(rune('A'+r.Intn(3)))
		et := schema.EntityType{}
		if tame {
			et.ValidationLevel = goodLevels[r.Intn(len(goodLevels))]
		} else {
			et.ValidationLevel = append(append([]schema.ValidationLevel(nil), goodLevels...), badLevels...)[r.Intn(len(goodLevels)+len(badLevels))]
		}
		for j := 0; j < r.Intn(3); j++ {
			et.RequiredComponents = append(et.RequiredComponents, refFrom())
		}
		for j := 0; j < r.Intn(2); j++ {
			c := refFrom()
			// A tame schema does not put one component in both lists.
			if tame && contains(et.RequiredComponents, c) {
				continue
			}
			et.OptionalComponents = append(et.OptionalComponents, c)
		}
		s.EntityTypes[n] = et
		s.EntityTypeOrder = append(s.EntityTypeOrder, n)
	}
	return s
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// bindingsAndMachines gives a schema behaviour bindings that mostly do not
// resolve, plus a machine seeding a field name that several components may
// declare. Everything it produces is a warning, which is the point: the
// agreement property below has to hold with the non-structural checks firing,
// or "blocking" has quietly come to mean something other than "the save will be
// refused".
func bindingsAndMachines(r *rand.Rand, s *schema.DatabaseSchema) validation.Input {
	ids := []string{"wander", "chase", "sub/wander", ""}
	for _, n := range s.ComponentOrder {
		if r.Intn(3) == 0 {
			c := s.Components[n]
			c.Behavior = ids[r.Intn(len(ids))]
			s.Components[n] = c
		}
	}
	for _, n := range s.EntityTypeOrder {
		if r.Intn(3) == 0 {
			et := s.EntityTypes[n]
			et.Behavior = ids[r.Intn(len(ids))]
			s.EntityTypes[n] = et
		}
	}
	in := validation.Input{Schema: *s}
	if r.Intn(2) == 0 {
		in.BehaviorDirs = []string{"/nonexistent/a", "/nonexistent/b"}
	}
	if r.Intn(2) == 0 {
		in.Machines = []project.Machine{{
			ID:         "wander",
			Definition: &agent.MachineDefinition{ID: "wander", Context: map[string]any{"f": 0}},
		}}
	}
	if r.Intn(2) == 0 {
		in.Problems = []project.Problem{{Path: "/nonexistent/a/wander.json", Err: errString("rejected")}}
	}
	return in
}

// The two properties the narrowing must have, over ten thousand schemas:
//
//  1. Forge never blocks a save the engine would accept — a false positive is a
//     control the user cannot get past for a reason that is not real.
//  2. Forge never accepts a save the engine would refuse — a false negative is a
//     Save button that fails when pressed.
func TestNarrowingAgreesWithTheEngine(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	var blockedCount, cleanCount, warned int
	for i := 0; i < 10000; i++ {
		s := randomSchema(r)
		// With the behaviour and ambiguity checks firing too, not just the
		// structural ones: those two produce warnings, and the whole point of
		// the narrow definition of Blocking is that no number of warnings ever
		// adds up to a disabled Save.
		in := bindingsAndMachines(r, &s)
		engineRefuses := schema.ValidateSchema(s) != nil
		report := validation.Check(in)
		forgeBlocks := report.Blocked()

		if _, warnings := report.Counts(); warnings > 0 {
			warned++
		}
		if forgeBlocks != engineRefuses {
			t.Fatalf("disagreement on schema %+v:\n  engine refuses = %v\n  Forge blocks   = %v\n  engine says: %v",
				s, engineRefuses, forgeBlocks, schema.ValidateSchema(s))
		}
		if forgeBlocks {
			blockedCount++
		} else {
			cleanCount++
		}
	}
	// Both outcomes have to occur, or the agreement is vacuous.
	// Both outcomes have to be well represented, not merely present: a corpus
	// that is 99% invalid demonstrates agreement about rejection only.
	if blockedCount < 1000 || cleanCount < 1000 {
		t.Fatalf("lopsided corpus: %d blocked, %d clean", blockedCount, cleanCount)
	}
	// And the warnings have to actually be firing, or the addition above proves
	// nothing about them.
	if warned < 1000 {
		t.Fatalf("only %d schemas produced a warning; the non-structural checks are\n"+
			"barely exercised, so their not blocking is not evidence", warned)
	}
	t.Logf("%d blocked, %d clean, %d carrying warnings", blockedCount, cleanCount, warned)
}

// Every problem Forge attributes must name a real owner: a message hung on a
// component that does not exist points at a control that is not on the page.
func TestEveryAttributionNamesSomethingThatExists(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for i := 0; i < 10000; i++ {
		s := randomSchema(r)
		for _, p := range validation.Check(validation.Input{Schema: s}).Problems {
			switch p.Owner.Kind {
			case validation.OwnerComponent:
				if _, ok := s.Components[p.Owner.Name]; !ok {
					t.Fatalf("problem blamed on component %q, which is not in the schema: %s",
						p.Owner.Name, p.Message)
				}
			case validation.OwnerEntityType:
				if _, ok := s.EntityTypes[p.Owner.Name]; !ok {
					t.Fatalf("problem blamed on entity type %q, which is not in the schema: %s",
						p.Owner.Name, p.Message)
				}
			case validation.OwnerSchema:
				if p.Owner.Name != "" {
					t.Fatalf("a file-wide problem carries a name: %q", p.Owner.Name)
				}
			}
		}
	}
}

// The report renders on a 2-second stream and identical renders are suppressed,
// so map order leaking into it would both flicker the page and defeat the
// suppression.
func TestCheckIsDeterministic(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for i := 0; i < 2000; i++ {
		s := randomSchema(r)
		first := validation.Check(validation.Input{Schema: s})
		for j := 0; j < 5; j++ {
			again := validation.Check(validation.Input{Schema: s})
			if len(again.Problems) != len(first.Problems) {
				t.Fatalf("problem count varies between runs: %d then %d", len(first.Problems), len(again.Problems))
			}
			for k := range first.Problems {
				if again.Problems[k] != first.Problems[k] {
					t.Fatalf("problem %d differs between runs:\n  %+v\n  %+v", k, first.Problems[k], again.Problems[k])
				}
			}
		}
	}
}

// structuralProblems ends with a fallback: a failure that no carrier reproduces
// is reported unattached rather than dropped. Nothing reaches it today — the
// engine's rules are covered exhaustively by the file-wide guards above it and
// the two carriers below — which means it cannot be mutation-tested, and it
// would be easy to mistake for dead code and delete.
//
// This measures the claim instead. Over the corpus, a schema with both halves
// populated and a sane version always gets its message attached to a control.
// If the engine grows a rule the narrowing does not anticipate, this starts
// failing and the fallback starts mattering — which is the moment someone needs
// to know, and the reason the fallback is there.
func TestEveryBlockedSchemaWithSomethingToBlameNamesIt(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	var checked int
	for i := 0; i < 10000; i++ {
		s := randomSchema(r)
		if s.SchemaVersion < 1 || len(s.Components) == 0 || len(s.EntityTypes) == 0 {
			continue // the file-wide guards own these, and they have their own tests
		}
		report := validation.Check(validation.Input{Schema: s})
		if !report.Blocked() {
			continue
		}
		checked++
		if len(report.General()) > 0 {
			t.Fatalf("a blocked schema with %d components and %d entity types produced an\n"+
				"unattached message, which means the narrowing missed a rule:\n  %s\n  schema: %+v",
				len(s.Components), len(s.EntityTypes), report.General()[0].Message, s)
		}
	}
	if checked < 500 {
		t.Fatalf("only %d schemas reached the assertion; the corpus is not exercising it", checked)
	}
	t.Logf("%d blocked schemas, all attributed", checked)
}
