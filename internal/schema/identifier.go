package schema

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// identifierPattern is what a component or property name has to look like to
// survive the trip into SQL.
//
// A component becomes the table `comp_` + lowercase(name) and a property becomes
// a column named lowercase(name), both interpolated into DDL and into every
// INSERT. Anything that is not an identifier does not reliably fail: a property
// called "two words" builds a column called "two" whose declared type is
// "words INTEGER", and the property the schema asked for is simply absent.
//
// Case is allowed here and folded away downstream, because the names this
// engine already uses are mixed-case — Position, TileType — and lowercasing is
// what the generator does rather than something to refuse. What that folding
// costs is the collision check below.
//
// ASCII only, which is stricter than SQL: SQLite would accept Größe or 位置 as
// a bare column name. Deliberate, and worth knowing it is a choice rather than
// a limit — Unicode case folding is not the ASCII lowercasing the generator
// does, so the collision check below would be answering a different question
// from the one the DDL asks. The Kelvin sign K lowercases to an ASCII k, which
// is exactly the kind of near-collision that would slip through.
//
// Stated here, in the package that owns the schema, so the generator and the
// insert path can both rely on it having happened rather than each keeping a
// regex that agrees with the other until one is edited.
var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidIdentifier reports whether a name can be used as a SQL identifier by the
// generator. Exported because Forge validates a name as it is typed, and a
// second opinion about what is legal is a way for the two to disagree.
func ValidIdentifier(name string) bool { return identifierPattern.MatchString(name) }

// ValidColumnName reports whether a property of this name can be a column: an
// identifier, and not one of the names SQLite will read as something else.
// Component names do not go through this — a table is "comp_" + the name, so
// comp_select is ordinary.
func ValidColumnName(name string) bool {
	if !ValidIdentifier(name) {
		return false
	}
	_, unusable := unusableAsColumn[strings.ToLower(name)]
	return !unusable
}

// unusableAsColumn is every name that is a perfectly good identifier and still
// cannot be a column in this schema.
//
// Two families, and they fail differently.
//
// **Reserved words.** SQLite has 147 keywords and most of them are fine as a
// bare column name — action, key, first, last, row, match, range and 78 others
// all round-trip, and refusing them would be refusing names a game schema
// actually wants. The 60 below are the ones that do not: they end a CREATE
// TABLE with "near \"INTEGER\": syntax error", which is a message with no path
// back to the property somebody typed. The list is derived from SQLite rather
// than guessed at, and TestReservedColumnNames_MatchesWhatSQLiteActuallyRefuses
// re-derives it, so a SQLite upgrade that changes the answer fails a test
// instead of a user's map.
//
// **The three CURRENT_ keywords.** These are worse than a syntax error and are
// why this list exists at all: SQLite resolves CURRENT_TIME as a keyword
// expression before it resolves a column of that name, so the table builds, the
// INSERT succeeds, and every read comes back as the clock. A property called
// current_time stores 42 and reads "23:54:37". That is the same silent class as
// "two words" building a column called "two", surviving one layer further in.
//
// **entity_id and target_entity_id** are not keywords at all. They are columns
// the generator emits for itself, and a property of the same name collides with
// one.
//
// entity_id is the primary key of every component table, so a property called
// that is a duplicate column — loud, but at bootstrap, and the message names the
// component rather than the property.
//
// target_entity_id is the data column of a component whose *type* is a
// reference, and this comment used to say it could not collide because such
// components have no properties. That is true of any one component and not true
// over time: a component's type can change. An object with a single entity-ref
// property called target_entity_id builds a table that a component of type
// entity-ref would also build, so Diff lets the type change without dropping the
// table — and the two columns disagree about NULL, which the property form
// allows and the reference form does not. The engine writes such a NULL through
// CreateEntity with no value for the property, and the rebuild then has nothing
// to put in its place:
//
//	INSERT INTO comp_link_new (entity_id, target_entity_id)
//	  SELECT entity_id, target_entity_id FROM comp_link
//	NOT NULL constraint failed: comp_link_new.target_entity_id
//
// which the store returns from every subsequent open. Reserving the name closes
// that with a message naming the property, at the point the schema is loaded.
//
// **value** is the third fixed column and is deliberately *not* reserved. An
// object whose one property is called value is the shape Forge gives every new
// component, and it retypes to and from a scalar without trouble: both forms
// refuse NULL, so the rows copy across. Reserving it would refuse the everyday
// schema to close a hole that is not there.
var unusableAsColumn = func() map[string]string {
	m := map[string]string{
		"entity_id":        "the column every component table already has",
		"target_entity_id": "the column a component of type entity-ref stores its reference in",
	}
	for _, w := range strings.Fields(`
		add all alter and as autoincrement between case cast check collate commit
		constraint create default deferrable delete distinct drop else escape
		except exists foreign from group having in index insert intersect into is
		isnull join limit not nothing notnull null on or order primary raise
		references returning select set table then to transaction union unique
		update using values when where`) {
		m[w] = "a SQL keyword SQLite will not read as a column name"
	}
	for _, w := range []string{"current_date", "current_time", "current_timestamp"} {
		m[w] = "a SQL keyword that reads back as the clock rather than what was stored"
	}
	return m
}()

// validateIdentifiers refuses any component or property name that cannot become
// a table or column, and any two that would become the same one.
func validateIdentifiers(s DatabaseSchema) error {
	// Sorted, so a schema with two problems reports the same one every run.
	// A map's iteration order would make the message depend on the weather.
	compNames := make([]string, 0, len(s.Components))
	for name := range s.Components {
		compNames = append(compNames, name)
	}
	sort.Strings(compNames)

	tables := make(map[string]string, len(compNames))
	for _, name := range compNames {
		if !ValidIdentifier(name) {
			return fmt.Errorf("component %q cannot be a table name: %s", name, identifierRule)
		}
		// comp_ + lowercase is what the generator builds, so two names that
		// differ only in case are one table. The second one silently gets no
		// table of its own, and every write to it fails much later with "no
		// such column".
		table := "comp_" + strings.ToLower(name)
		if first, taken := tables[table]; taken {
			return fmt.Errorf("components %q and %q would both be table %q; names that differ only in case are one name here",
				first, name, table)
		}
		tables[table] = name

		propNames := make([]string, 0, len(s.Components[name].Properties))
		for prop := range s.Components[name].Properties {
			propNames = append(propNames, prop)
		}
		sort.Strings(propNames)

		columns := make(map[string]string, len(propNames))
		for _, prop := range propNames {
			if !ValidIdentifier(prop) {
				return fmt.Errorf("component %q: property %q cannot be a column name: %s",
					name, prop, identifierRule)
			}
			col := strings.ToLower(prop)
			// Reserved words are a property problem and not a component one:
			// the table is "comp_" + the name, so comp_select is ordinary.
			if why, unusable := unusableAsColumn[col]; unusable {
				return fmt.Errorf("component %q: property %q cannot be a column name: %q is %s",
					name, prop, col, why)
			}
			if first, taken := columns[col]; taken {
				return fmt.Errorf("component %q: properties %q and %q would both be column %q",
					name, first, prop, col)
			}
			columns[col] = prop
		}
	}
	if err := validateRenames(s, compNames); err != nil {
		return err
	}
	return nil
}

// validateRenames checks that every renamedFrom names something that could have
// been a table, a column or a type, and that it is not still in use.
//
// The name has to be a usable identifier for the same reason the new one does:
// it is interpolated into an ALTER TABLE. And a renamedFrom that names something
// the file *still declares* is the case worth refusing loudly — "rename x to
// col_x" while x is also declared is either a mistake or a swap, and the
// migration would have to both rename a column and keep it.
func validateRenames(s DatabaseSchema, compNames []string) error {
	for _, name := range compNames {
		comp := s.Components[name]
		if from := comp.RenamedFrom; from != "" {
			if !ValidIdentifier(from) {
				return fmt.Errorf("component %q: renamedFrom %q cannot be a table name: %s",
					name, from, identifierRule)
			}
			if strings.EqualFold(from, name) {
				return fmt.Errorf("component %q is renamed from itself; drop the renamedFrom", name)
			}
			if _, still := lookupFold(s.Components, from); still {
				return fmt.Errorf("component %q is renamed from %q, which the schema still declares",
					name, from)
			}
		}

		props := make([]string, 0, len(comp.Properties))
		for prop := range comp.Properties {
			props = append(props, prop)
		}
		sort.Strings(props)
		for _, prop := range props {
			from := comp.Properties[prop].RenamedFrom
			if from == "" {
				continue
			}
			if !ValidIdentifier(from) {
				return fmt.Errorf("component %q: property %q: renamedFrom %q cannot be a column name: %s",
					name, prop, from, identifierRule)
			}
			if why, unusable := unusableAsColumn[strings.ToLower(from)]; unusable {
				return fmt.Errorf("component %q: property %q: renamedFrom %q cannot be a column name: %q is %s",
					name, prop, from, strings.ToLower(from), why)
			}
			if strings.EqualFold(from, prop) {
				return fmt.Errorf("component %q: property %q is renamed from itself; drop the renamedFrom",
					name, prop)
			}
			if _, still := lookupFold(comp.Properties, from); still {
				return fmt.Errorf("component %q: property %q is renamed from %q, which the component still declares",
					name, prop, from)
			}
		}

		// Below the top level there are no columns to rename: a nested object
		// and an array's items live inside one JSON column, so a renamedFrom
		// there does nothing at all. Refused rather than ignored, because a
		// data-preservation field that silently does nothing is the exact
		// failure this whole story is about.
		for _, prop := range props {
			if err := noNestedRename(name, prop, comp.Properties[prop]); err != nil {
				return err
			}
		}
		if comp.Items != nil {
			// The items themselves as well as anything under them: an array
			// component's items are the one nested position reachable without
			// going through a property, so the recursion below never sees it.
			if comp.Items.RenamedFrom != "" {
				return fmt.Errorf("component %q: items has a renamedFrom, which does nothing: "+
					"an array's items are stored inside one JSON column and have no column to rename", name)
			}
			if err := noNestedRename(name, "items", *comp.Items); err != nil {
				return err
			}
		}
	}

	// Two things renamed from one name. Whichever were applied first would take
	// the table and the other would silently become a plain addition — an empty
	// table where the author expected their data. Chains and cycles are already
	// refused by the "still declares" checks above: renaming A→B and B→C means
	// the file declares B, which B→C is renamed from.
	if err := noSharedRenameSource(s, compNames); err != nil {
		return err
	}

	etNames := make([]string, 0, len(s.EntityTypes))
	for name := range s.EntityTypes {
		etNames = append(etNames, name)
	}
	sort.Strings(etNames)
	for _, name := range etNames {
		from := s.EntityTypes[name].RenamedFrom
		if from == "" {
			continue
		}
		if from == name {
			return fmt.Errorf("entity type %q is renamed from itself; drop the renamedFrom", name)
		}
		if _, still := s.EntityTypes[from]; still {
			return fmt.Errorf("entity type %q is renamed from %q, which the schema still declares", name, from)
		}
	}
	return nil
}

// noNestedRename refuses a renamedFrom anywhere below a component's top-level
// properties, where it could not be acted on.
func noNestedRename(comp, path string, p Property) error {
	names := make([]string, 0, len(p.Properties))
	for n := range p.Properties {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		child := p.Properties[n]
		if child.RenamedFrom != "" {
			return fmt.Errorf("component %q: %s.%s has a renamedFrom, which does nothing: "+
				"a nested property is stored inside its parent's JSON column and has no column to rename",
				comp, path, n)
		}
		if err := noNestedRename(comp, path+"."+n, child); err != nil {
			return err
		}
	}
	if p.Items != nil {
		if p.Items.RenamedFrom != "" {
			return fmt.Errorf("component %q: %s.items has a renamedFrom, which does nothing: "+
				"an array's items are stored inside one JSON column and have no column to rename",
				comp, path)
		}
		return noNestedRename(comp, path+".items", *p.Items)
	}
	return nil
}

// noSharedRenameSource refuses two things claiming to have been the same thing.
func noSharedRenameSource(s DatabaseSchema, compNames []string) error {
	claimed := map[string]string{}
	for _, name := range compNames {
		// The component's own renamedFrom, when it has one. Not a `continue`:
		// the properties below have to be checked whether or not the component
		// they are in was renamed, and skipping them was this function's first
		// bug.
		if from := strings.ToLower(s.Components[name].RenamedFrom); from != "" {
			if first, taken := claimed[from]; taken {
				return fmt.Errorf("components %q and %q are both renamed from %q", first, name, from)
			}
			claimed[from] = name
		}

		props := make([]string, 0, len(s.Components[name].Properties))
		for prop := range s.Components[name].Properties {
			props = append(props, prop)
		}
		sort.Strings(props)
		inComp := map[string]string{}
		for _, prop := range props {
			pf := strings.ToLower(s.Components[name].Properties[prop].RenamedFrom)
			if pf == "" {
				continue
			}
			if first, taken := inComp[pf]; taken {
				return fmt.Errorf("component %q: properties %q and %q are both renamed from %q",
					name, first, prop, pf)
			}
			inComp[pf] = prop
		}
	}

	etClaimed := map[string]string{}
	etNames := make([]string, 0, len(s.EntityTypes))
	for name := range s.EntityTypes {
		etNames = append(etNames, name)
	}
	sort.Strings(etNames)
	for _, name := range etNames {
		from := s.EntityTypes[name].RenamedFrom
		if from == "" {
			continue
		}
		if first, taken := etClaimed[from]; taken {
			return fmt.Errorf("entity types %q and %q are both renamed from %q", first, name, from)
		}
		etClaimed[from] = name
	}
	return nil
}

// lookupFold finds a key ignoring case, which is how the generator treats
// component and column names.
func lookupFold[V any](m map[string]V, key string) (V, bool) {
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	var zero V
	return zero, false
}

// identifierRule is the same sentence everywhere, because someone reading it is
// about to go and rename something.
const identifierRule = "a letter or underscore followed by letters, digits or underscores"
