package modes

import (
	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
	"github.com/tmbritton/ecs-db/internal/forge/validation"
)

// Owner kinds, re-exported so the .templ files in this package can name them
// without importing validation — the same reason Component and EntityType are
// aliased in schema.go.
const (
	ownerComponent  = validation.OwnerComponent
	ownerEntityType = validation.OwnerEntityType
)

// componentProblems is one component's problems for one control. Pass an empty
// field for the ones that belong to the component as a whole.
func componentProblems(data Data, name, field string) []components.Problem {
	return data.Validation.At(ownerComponent, name, field)
}

// typeProblems is componentProblems for the other half of the file.
func typeProblems(data Data, name, field string) []components.Problem {
	return data.Validation.At(ownerEntityType, name, field)
}

// fileProblems is what is wrong with schema.json as a whole — a version below
// one, or a half of the file that is empty. Rendered in both modes because it
// is a fact about the file, and which tab happens to be open does not change it.
func fileProblems(data Data) []components.Problem { return data.Validation.General() }

// behaviorField and propertyField name the controls a problem can hang on.
// Thin wrappers so the .templ files spell the key once, in one place, and the
// key the validator writes and the key the template reads cannot drift.
func behaviorField() string { return validation.FieldBehavior }

func propertyField(name string) string { return validation.PropertyField(name) }

// problemsID is components.ProblemsID under a name the .templ files can use.
func problemsID(parts ...string) string { return components.ProblemsID(parts...) }

// anyBlocking reports whether a save would be refused, for the controls that
// need to say so.
func anyBlocking(problems []components.Problem) bool { return components.Blocking(problems) }

// describedBy is components.DescribedBy, re-exported for the same reason.
func describedBy(id string, problems []components.Problem) string {
	return components.DescribedBy(id, problems)
}

// hasProblems reports whether an owner has anything wrong with it, for the
// marker on its list row.
//
// A problem renders inside its owner's editor, and only one owner's editor is
// on screen — so without a marker on the list, a schema with two broken entity
// types shows a disabled Save button, a count of two, and nothing else at all
// until the user clicks through every row looking for them.
func hasProblems(data Data, kind validation.OwnerKind, name string) bool {
	return len(data.Validation.For(kind, name)) > 0
}

// hasBlockingProblems distinguishes the marker's severity, so a row carrying
// the reason the save is refused is not shown the same as one carrying advice.
func hasBlockingProblems(data Data, kind validation.OwnerKind, name string) bool {
	return components.Blocking(data.Validation.For(kind, name))
}

// unplacedComponentProblems is every problem for a component that no control in
// its editor will show.
//
// The editor places a problem on the behaviour dropdown and on a fields-table
// row; anything else — an owner-level problem, or a field problem for a
// property that has no row — has to render somewhere or it is lost. A
// non-object component keeps whatever `properties` a hand-edited file gave it,
// and `agent.FieldIndex` reads them, so an ambiguity warning could attach to a
// row that is never drawn.
//
// Written as "everything the controls did not take" rather than as a list of
// the cases that fall through, so a control added later cannot silently start
// swallowing a message.
func unplacedComponentProblems(data Data, name string) []components.Problem {
	placed := map[string]bool{behaviorField(): true}
	comp := data.Schema.Components[name]
	if isObject(comp) {
		for _, field := range propertyNames(comp) {
			placed[propertyField(field)] = true
		}
	}
	return unplaced(data.Validation.For(ownerComponent, name), placed)
}

// unplacedTypeProblems is the same for an entity type, whose editor places
// problems on one control.
func unplacedTypeProblems(data Data, name string) []components.Problem {
	return unplaced(data.Validation.For(ownerEntityType, name), map[string]bool{behaviorField(): true})
}

func unplaced(problems []components.Problem, placed map[string]bool) []components.Problem {
	var out []components.Problem
	for _, p := range problems {
		if placed[p.Field] {
			continue
		}
		out = append(out, p)
	}
	return out
}

// OwnerKind is aliased so the .templ files in this package can take one as a
// parameter, on the same terms as Component and EntityType in schema.go.
type OwnerKind = validation.OwnerKind

// problemSummary is the list-row marker's title and its screen-reader text. A
// count rather than the first message: the messages are in the editor, and one
// of three quoted on a badge reads as the only one.
func problemSummary(data Data, kind validation.OwnerKind, name string) string {
	problems := data.Validation.For(kind, name)
	errors := 0
	for _, p := range problems {
		if p.Blocking {
			errors++
		}
	}
	switch {
	case errors == len(problems) && errors == 1:
		return "1 problem, which stops the save"
	case errors == len(problems):
		return itoa(errors) + " problems, which stop the save"
	case errors == 0 && len(problems) == 1:
		return "1 warning"
	case errors == 0:
		return itoa(len(problems)) + " warnings"
	default:
		return itoa(errors) + " of " + itoa(len(problems)) + " problems stop the save"
	}
}
