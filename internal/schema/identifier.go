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
// **entity_id** is not a keyword at all. It is the primary key the generator
// emits for every component table, so a property of that name is a duplicate
// column — loud, but at bootstrap, and the message names the component rather
// than the property. The other fixed columns the generator emits, value and
// target_entity_id, belong to component kinds that have no properties, so they
// cannot collide and are not reserved.
var unusableAsColumn = func() map[string]string {
	m := map[string]string{"entity_id": "the column every component table already has"}
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
	return nil
}

// identifierRule is the same sentence everywhere, because someone reading it is
// about to go and rename something.
const identifierRule = "a letter or underscore followed by letters, digits or underscores"
