// Package validation says what is wrong with the schema being edited, while it
// is being edited.
//
// It decides nothing. Every rule here is the engine's, asked through the
// engine's own functions — schema.ValidateSchema for structure and
// cross-references, schema.ValidateBehaviorRefs for behaviour bindings, and
// agent.FieldIndex for the one thing the engine only discovers later. If Forge
// and the engine ever disagree about whether a schema is valid, the engine is
// right and Forge was wrong to say otherwise, so there is no second copy of any
// rule in this file to drift from the first.
//
// What this package does add is *where* and *how many*. ValidateSchema returns
// one error for a whole file, so on its own it can neither say which control to
// hang the message on nor show a second problem before the first is fixed. Both
// are recovered by asking the same function about smaller pieces of the same
// schema — never by re-deciding what those pieces mean.
package validation

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/jsonorder"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// OwnerKind names what a problem is about.
type OwnerKind int

const (
	// OwnerSchema is schema.json as a whole: nothing smaller could be blamed.
	OwnerSchema OwnerKind = iota
	OwnerComponent
	OwnerEntityType
	// OwnerMachine is one behaviour machine, named by its id.
	//
	// Check never produces one: the machine checks are agent.ValidateMachine's,
	// run per machine by the editing session against the value being edited,
	// and this package looks at schema.json. AGENTS sets it on the problems it
	// renders through the shared list.
	//
	// Nothing reads it yet — the list renders severity from Blocking alone, so
	// removing it would change no output. It is set because a problem is about
	// something, and the zero value would record a machine the engine refuses
	// as a fault in schema.json. Story 8 attributes each problem to the node
	// that caused it, and that is the reader.
	OwnerMachine
)

// Owner is the thing a problem is about.
type Owner struct {
	Kind OwnerKind
	Name string // empty for OwnerSchema
}

// Control keys. A problem carrying one renders beside that control and is
// associated with it; a problem with no control renders at the top of its
// owner's editor.
const (
	// FieldBehavior is the behaviour-binding dropdown, on both a component and
	// an entity type.
	FieldBehavior = "behavior"
)

// PropertyField is the control key for one row of a component's fields table.
// A function rather than a format string spelled out at both ends: the
// validation that produces the key and the template that looks it up have to
// agree, and two literals eventually will not.
func PropertyField(name string) string { return "property:" + name }

// Problem is one thing wrong, and where to say it.
type Problem struct {
	Owner   Owner
	Field   string // a control key, or empty for the owner as a whole
	Message string
	// Blocking is true when the save itself would be refused — which today means
	// exactly schema.ValidateSchema, the check editable.File.Save runs before
	// writing. Nothing else sets it.
	//
	// That is a deliberately narrow definition. "This is wrong" and "this cannot
	// be written" are different claims, and only the second justifies taking the
	// Save button away: disabling it for anything the engine would accept leaves
	// someone unable to save a change they did make because of one they did not.
	Blocking bool
}

// Report is everything Forge can say about the schema right now.
type Report struct {
	Problems []Problem
	// Partial is true when a blocking problem came from schema.ValidateSchema,
	// which stops at the first failure it finds. The problems listed are real;
	// there may be more behind them. The UI has to say so rather than imply a
	// completeness it does not have.
	Partial bool
}

// Blocked reports whether a save would be refused.
func (r Report) Blocked() bool {
	for _, p := range r.Problems {
		if p.Blocking {
			return true
		}
	}
	return false
}

// General is the problems that belong to no component or entity type.
func (r Report) General() []Problem { return r.For(OwnerSchema, "") }

// For is every problem about one owner, whichever control it hangs on.
func (r Report) For(kind OwnerKind, name string) []Problem {
	var out []Problem
	for _, p := range r.Problems {
		if p.Owner.Kind == kind && p.Owner.Name == name {
			out = append(out, p)
		}
	}
	return out
}

// At is one owner's problems for one control. Pass an empty field for the ones
// that belong to the owner as a whole rather than to any single control.
func (r Report) At(kind OwnerKind, name, field string) []Problem {
	var out []Problem
	for _, p := range r.For(kind, name) {
		if p.Field == field {
			out = append(out, p)
		}
	}
	return out
}

// Counts is how many of each, for a one-line summary.
func (r Report) Counts() (errors, warnings int) {
	for _, p := range r.Problems {
		if p.Blocking {
			errors++
			continue
		}
		warnings++
	}
	return errors, warnings
}

// Input is what a check needs to know beyond the schema itself.
type Input struct {
	Schema schema.DatabaseSchema
	// BehaviorDirs are the mods' behaviours directories, in load order. A
	// binding resolves if any of them holds the file, which is how the loader
	// itself resolves one — a later mod may supply a machine an earlier one
	// does not.
	BehaviorDirs []string
	// Machines are the machines the project actually resolved. A file that
	// exists but did not load is a different problem from one that is not
	// there, and only this can tell them apart.
	Machines []project.Machine
	// Problems are the project's own loading failures, which is what turns
	// "that machine did not resolve" into a sentence naming the file and the
	// reason.
	Problems []project.Problem
}

// Check collects everything wrong with the schema being edited.
func Check(in Input) Report {
	structural, partial := structuralProblems(in.Schema)
	report := Report{Partial: partial}
	report.Problems = append(report.Problems, structural...)
	report.Problems = append(report.Problems, bindingProblems(in)...)
	report.Problems = append(report.Problems, ambiguityWarnings(in)...)
	return report
}

// ── structure, cross-reference and SQL compatibility ────────────────────────

// probeComponent and probeEntityType are the names the carrier schemas below
// use. They are placeholders in a schema that is never saved, shown or diffed —
// only handed to schema.ValidateSchema and thrown away — and they are spelled
// so that a real schema colliding with one would be an act of deliberate
// sabotage.
const (
	probeComponent  = "__forgeProbeComponent__"
	probeEntityType = "__forgeProbeEntityType__"
)

// structuralProblems asks schema.ValidateSchema what is wrong, and then asks it
// again about smaller pieces to find out where.
//
// The narrowing is sound because of how the engine's three phases are scoped.
// Structure looks at the version, at whether the two maps are non-empty, and at
// component names; cross-reference looks at one entity type at a time; SQL
// compatibility looks at one component at a time. So a carrier holding a single
// component answers exactly that component's naming and SQL questions, and one
// holding every component and a single entity type answers exactly that entity
// type's cross-reference questions. Nothing is re-decided here: the verdict on
// every carrier is the engine's.
//
// Components are asked first and alone. That is the engine's own order, and for
// its reason: what an entity type refers to means nothing while the components
// it refers to are themselves malformed, and a component that fails on its own
// terms fails inside every entity type's carrier too — which is how one bad
// component name came out as a complaint against every entity type in the file.
//
// A failure that no carrier reproduces is still reported, unattached, rather
// than dropped. That covers the rules about the file as a whole, and it is the
// safety net for any rule the engine grows later that this narrowing does not
// anticipate.
func structuralProblems(s schema.DatabaseSchema) ([]Problem, bool) {
	whole := schema.ValidateSchema(s)
	if whole == nil {
		return nil, false
	}

	// The engine's own words, blamed on nothing in particular.
	unattached := []Problem{{Message: whole.Error(), Blocking: true}}

	// With one half of the file empty there is nothing to narrow to. Not a
	// rule — the engine still decides whether this is wrong and still supplies
	// the sentence — but a schema with no components makes every reference in
	// every entity type dangle, and reporting those would blame each entity
	// type in turn for the one thing that is actually missing.
	if len(s.Components) == 0 || len(s.EntityTypes) == 0 {
		return unattached, true
	}
	// A carrier with nothing real in it but the version. It fails only on the
	// rules that judge the file as a whole, and when it does, narrowing further
	// would blame every component and every entity type for one problem that is
	// none of theirs.
	if schema.ValidateSchema(emptyCarrier(s.SchemaVersion)) != nil {
		return unattached, true
	}

	var out []Problem
	for _, name := range jsonorder.Apply(s.ComponentOrder, s.Components) {
		if err := schema.ValidateSchema(componentCarrier(s, name)); err != nil {
			out = append(out, Problem{
				Owner:    Owner{Kind: OwnerComponent, Name: name},
				Message:  err.Error(),
				Blocking: true,
			})
		}
	}
	if len(out) > 0 {
		return out, true
	}

	for _, name := range jsonorder.Apply(s.EntityTypeOrder, s.EntityTypes) {
		if err := schema.ValidateSchema(entityTypeCarrier(s, name)); err != nil {
			out = append(out, Problem{
				Owner:    Owner{Kind: OwnerEntityType, Name: name},
				Message:  err.Error(),
				Blocking: true,
			})
		}
	}
	if len(out) == 0 {
		return unattached, true
	}
	return out, true
}

// emptyCarrier is a valid schema at the version under test, and nothing else.
func emptyCarrier(version int) schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: version,
		Components:    map[string]schema.Component{probeComponent: filler()},
		EntityTypes:   map[string]schema.EntityType{probeEntityType: fillerType()},
	}
}

// componentCarrier is one real component, carried by a filler entity type that
// asks nothing of it.
func componentCarrier(s schema.DatabaseSchema, name string) schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: s.SchemaVersion,
		Components:    map[string]schema.Component{name: s.Components[name]},
		EntityTypes:   map[string]schema.EntityType{probeEntityType: fillerType()},
	}
}

// entityTypeCarrier is one real entity type against every real component.
//
// The components are the real ones and not fillers: what is being asked is
// whether this type's references resolve, and substituting them would make
// every reference resolve. It is only reached once every component has passed
// on its own, so none of them can fail here for reasons of its own.
func entityTypeCarrier(s schema.DatabaseSchema, name string) schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: s.SchemaVersion,
		Components:    s.Components,
		EntityTypes:   map[string]schema.EntityType{name: s.EntityTypes[name]},
	}
}

func filler() schema.Component {
	return schema.Component{
		Type:       schema.ComponentTypeObject,
		Properties: map[string]schema.Property{"value": {Type: schema.PropertyTypeNumber}},
	}
}

func fillerType() schema.EntityType {
	return schema.EntityType{ValidationLevel: schema.ValidationStrict}
}

// ── behaviour bindings ──────────────────────────────────────────────────────

// bindingProblems reports behaviour fields that name a machine the engine will
// not find, and machines it will find but not be able to load.
//
// Both are warnings, and that is a correction to what this story originally
// built. The reasoning for making a missing file blocking was that it fails
// schema.ValidateBehaviorRefs, "which is startup validation". It is not, yet:
// that function has no caller in the engine — this package is its first anywhere
// — and nothing outside internal/schema and internal/forge reads the Behavior
// field at all. So today a binding naming a missing file and one naming a
// rejected file are equally inert: the entity spawns and does nothing either
// way, and the game starts fine.
//
// Blocking the save for it would therefore have been exactly what this package
// refuses to do three functions further down for the ambiguity warning — invent
// a rule the engine does not have — with a worse consequence, because it is
// reached by doing nothing at all. Open a project whose schema.json already
// binds a machine someone deleted, and Save is dead for every other change in
// the file until you clear a binding you did not want to clear.
//
// When ValidateBehaviorRefs gains its engine-side caller, as its own doc comment
// says it is meant to, the first tier becomes blocking and this comment is the
// place to say so.
func bindingProblems(in Input) []Problem {
	var out []Problem
	add := func(owner Owner, behavior string, carrier func() schema.DatabaseSchema) {
		if behavior == "" {
			return
		}
		warn := func(message string) {
			out = append(out, Problem{Owner: owner, Field: FieldBehavior, Message: message})
		}
		// Asked before the engine's check, which answers this case by naming
		// behaviorsDir — a parameter of a Go function, not anything in the
		// user's game.toml.
		if len(in.BehaviorDirs) == 0 {
			warn("nothing declares where behaviour machines live, so " + quote(behavior) +
				" cannot resolve. Give a [[mods]] section in game.toml a behaviors " +
				"directory, or clear this binding.")
			return
		}
		if err := resolveRef(carrier(), in.BehaviorDirs); err != nil {
			warn(err.Error())
			return
		}
		if hasMachine(in.Machines, behavior) {
			return
		}
		warn(unresolvedMessage(behavior, in.Problems))
	}

	s := in.Schema
	for _, name := range jsonorder.Apply(s.ComponentOrder, s.Components) {
		comp := s.Components[name]
		add(Owner{Kind: OwnerComponent, Name: name}, comp.Behavior, func() schema.DatabaseSchema {
			return schema.DatabaseSchema{Components: map[string]schema.Component{name: comp}}
		})
	}
	for _, name := range jsonorder.Apply(s.EntityTypeOrder, s.EntityTypes) {
		et := s.EntityTypes[name]
		add(Owner{Kind: OwnerEntityType, Name: name}, et.Behavior, func() schema.DatabaseSchema {
			return schema.DatabaseSchema{EntityTypes: map[string]schema.EntityType{name: et}}
		})
	}
	return out
}

// resolveRef asks the engine's own check, once per mod behaviours directory.
//
// The engine's function takes a single directory because the interpreter is
// meant to call it with one; a project has as many as it has mods, and the
// loader accepts a machine contributed by any of them. So the binding resolves
// if any directory satisfies the check.
//
// When none does, the message is widened to name every directory searched —
// the single-directory message would otherwise point at whichever mod happened
// to be first and read as though that were the only place the file could have
// lived. But only when the complaint is actually about a directory: a behaviour
// name containing a path separator is rejected before the filesystem is touched,
// and attaching "searched /a, /b" to that describes a search that never
// happened. Which case it is falls out of the answers rather than being decided
// here — a directory-independent complaint is the same sentence from every
// directory.
func resolveRef(one schema.DatabaseSchema, dirs []string) error {
	errs := make([]error, 0, len(dirs))
	for _, dir := range dirs {
		err := schema.ValidateBehaviorRefs(one, dir)
		if err == nil {
			return nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		// No directories to ask. The caller handles this case before calling,
		// so reaching it would mean nothing was checked — which must not read
		// as "checked and fine".
		return fmt.Errorf("no behaviours directory was searched")
	}
	for _, err := range errs[1:] {
		if err.Error() != errs[0].Error() {
			return fmt.Errorf("%w (searched %s)", errs[0], strings.Join(dirs, ", "))
		}
	}
	return errs[0]
}

func hasMachine(machines []project.Machine, id string) bool {
	for _, m := range machines {
		if m.ID == id {
			return true
		}
	}
	return false
}

// unresolvedMessage says why a machine whose file exists is not among the ones
// the project resolved.
//
// The project records why it dropped each file it could not use. Without that,
// the only honest thing to say was "either no file declares that id, or the file
// that does was rejected — the log says which", which is a sentence that sends
// someone to a terminal to find out what the tool already knows.
//
// Matched on the filename rather than a substring of the path. Problem.Path is
// sometimes a file and sometimes a whole behaviours directory — a directory that
// could not be read, or one declaring an id twice — and a substring match
// reported a *different* machine's failure as this one's: a mod at
// mods/wander/behaviors whose chase.json is duplicated told the reader that
// machine "wander" failed to load because "chase" is declared twice.
func unresolvedMessage(behavior string, problems []project.Problem) string {
	const lead = "the file is there, but no machine with this id is loaded"
	want := behavior + ".json"
	var reasons []string
	for _, p := range problems {
		if filepath.Base(p.Path) != want {
			continue
		}
		reasons = append(reasons, p.String())
	}
	if len(reasons) == 0 {
		// Still two causes, but a narrower second one. Epic 13 Story 2 made
		// every create, rename and delete re-resolve, so Forge's own changes
		// are never stale — but nothing watches the behaviours directory, and
		// editing a machine file in a text editor is a first-class workflow
		// here. Claiming the id must be wrong would send those users to check
		// something already correct, which is what the previous wording was
		// written to avoid.
		return lead + ". Either the file declares a different " + quote("id") +
			" — a binding matches the id inside the file, not the filename — or it was " +
			"changed outside Forge, which does not watch the behaviours directory."
	}
	sort.Strings(reasons)
	return lead + ": " + strings.Join(reasons, "; ")
}

func quote(s string) string { return "\"" + s + "\"" }

// ── the ambiguity warning ───────────────────────────────────────────────────

// ambiguityWarnings predicts a machine-load failure from the schema edit that
// causes it.
//
// agent.ValidateMachine rejects a machine whose context key matches more than
// one component's field, because it cannot tell which one to seed from. That
// error arrives when the machine loads, which is a long way from adding an "hp"
// field to a second component — and by then the schema looks fine and the
// machine looks broken.
//
// A warning, not an error. The schema is legal; it is the machine that will
// refuse. Blocking the save would be Forge inventing a rule the engine does not
// have, and the fix may well belong in the machine rather than the schema.
//
// The index comes from agent.FieldIndex, which is the same one ValidateMachine
// builds — a second implementation of "which components declare this field" is
// how the prediction and the failure it predicts come to disagree.
func ambiguityWarnings(in Input) []Problem {
	index := agent.FieldIndex(in.Schema)

	var out []Problem
	for _, m := range in.Machines {
		if m.Definition == nil {
			continue
		}
		keys := make([]string, 0, len(m.Definition.Context))
		for key := range m.Definition.Context {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			owners := index[key]
			if len(owners) < 2 {
				continue
			}
			owners = append([]string(nil), owners...)
			sort.Strings(owners)
			message := fmt.Sprintf(
				"machine %q seeds context key %q, which %s both declare. "+
					"The engine cannot tell which to seed from and will refuse to load the machine.",
				m.ID, key, strings.Join(owners, " and "))
			// On every component that declares it: whichever one is open is
			// the one the message has to reach, and the fix is to rename the
			// field on one of them.
			for _, owner := range owners {
				out = append(out, Problem{
					Owner:   Owner{Kind: OwnerComponent, Name: owner},
					Field:   PropertyField(key),
					Message: message,
				})
			}
		}
	}
	return out
}
