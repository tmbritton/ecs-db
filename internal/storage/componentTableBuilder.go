package storage

import (
	"fmt"
	"strings"

	"github.com/tmbritton/ecs-db/internal/jsonorder"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// componentTableSQL generates the CREATE TABLE statement for a single component.
// Object components produce one typed column per property. Non-object components
// produce a single "value" column with an appropriate SQL type.
func componentTableSQL(name string, comp schema.Component) (string, error) {
	// The last line of defence, not the first. schema.ValidateSchema refuses a
	// name that cannot be an identifier, which is where a caller gets a message
	// naming the file they have to fix. This is here because the generator
	// interpolates these names into DDL, and a generator that trusted its input
	// would build "CREATE TABLE comp_probe (two words INTEGER)" — a column
	// called "two" of type "words INTEGER" — for anyone who called it without
	// validating first.
	if !schema.ValidIdentifier(name) {
		return "", fmt.Errorf("component %q cannot be a table name", name)
	}
	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS comp_%s (\n", strings.ToLower(name))
	cols := []string{
		"\tentity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE",
	}

	switch comp.Type {
	case schema.ComponentTypeObject:
		// Authored order, not map order.
		//
		// Ranging over the map directly gave a different column order on every
		// call. In the engine that was invisible — the DDL runs once at startup
		// and nobody reads it — but Forge shows this text as a live preview,
		// where the columns visibly reshuffle as you type. Since Epic 11 the
		// order the author wrote is recorded, so the DDL can read like the file
		// it came from; a component built in code has none and sorts instead.
		for _, propName := range jsonorder.Apply(comp.PropertyOrder, comp.Properties) {
			if !schema.ValidIdentifier(propName) {
				return "", fmt.Errorf("component %q: property %q cannot be a column name", name, propName)
			}
			prop := comp.Properties[propName]
			col := fmt.Sprintf("\t%s %s", strings.ToLower(propName), columnConstraint(prop))
			cols = append(cols, col)
		}

	case schema.ComponentTypeEntityRef:
		cols = append(cols, "\ttarget_entity_id "+entityRefColumnType)

	case schema.ComponentTypeArray:
		// Arrays are stored as JSON in a single column regardless of item type.
		cols = append(cols, "\tvalue TEXT NOT NULL DEFAULT '[]'")

	case schema.ComponentTypeString:
		cols = append(cols, "\tvalue TEXT NOT NULL DEFAULT ''")

	case schema.ComponentTypeInteger:
		cols = append(cols, "\tvalue INTEGER NOT NULL DEFAULT 0")

	case schema.ComponentTypeNumber:
		cols = append(cols, "\tvalue REAL NOT NULL DEFAULT 0.0")

	case schema.ComponentTypeBoolean:
		cols = append(cols, "\tvalue INTEGER NOT NULL DEFAULT 0")

	default:
		return "", fmt.Errorf("unsupported component type %q", comp.Type)
	}

	sql += strings.Join(cols, ",\n")
	sql += "\n)"
	return sql, nil
}

// propertySQLType maps a Property to its SQLite column type.
func propertySQLType(p schema.Property) string {
	return schema.PropertySQLType(p)
}

// entityRefReference is the foreign key every reference to an entity carries,
// and the only place this engine says what such a reference means.
//
// **ON DELETE CASCADE**: when the target goes, the component that pointed at it
// goes. Not a refusal — which is what no ON DELETE clause meant, and which made
// deleting an entity fail because of some other entity's data — and not SET
// NULL, which would leave a component describing a relationship to something
// that is not there.
//
// The whole row goes, so for an object component that means the whole
// component: a Holder with an owner and an hp loses both. That is the cost of
// one rule rather than two, and it is the same rule entity_id has always had.
//
// ddlgen's ALTER TABLE path composes its own clause from this constant rather
// than using entityRefColumnType, because it needs the nullability split below.
// The text is built from schema.EntityReference rather than written out again,
// so the constraint the generator emits and the one the diff expects to find
// cannot drift apart. schema owns the statement because the diff lives there
// and cannot import storage; the "REFERENCES" keyword is DDL, so it is added
// here. TestReferences_TheDiffExpectsWhatTheGeneratorEmits builds a real table
// for every type and checks the two ends against each other.
const entityRefReference = "REFERENCES " + schema.EntityReference

// entityRefColumnType is the whole column for a component whose *type* is a
// reference. NOT NULL, because the row is nothing else: a Carrier that points
// at nothing is not a Carrier.
const entityRefColumnType = "INTEGER NOT NULL " + entityRefReference

// columnConstraint is the type and constraints for one property's column.
//
// An entity-ref property used to get "INTEGER NOT NULL" here and a foreign key
// only when it arrived through ALTER TABLE ADD COLUMN — so the same schema
// produced a constraint or no constraint depending on whether the component was
// there at the start, and a rebuild silently dropped what an ALTER had added.
func columnConstraint(prop schema.Property) string {
	if schema.PropertyNullable(prop.Type) {
		// Nullable, unlike the component form, and unlike every other
		// property. A reference is the whole of a Carrier and only part of a
		// Holder, so "no owner yet" is a state a Holder can legitimately be in
		// — and it is the state every existing row is in the moment somebody
		// adds the property, because ALTER TABLE ADD COLUMN has no value to
		// backfill with.
		//
		// The three paths have to agree about this or a database wedges: with
		// NOT NULL here and nullable on the ALTER, adding the property to a
		// component that had rows and then changing any *other* property of it
		// made the rebuild's INSERT...SELECT fail on a NOT NULL constraint —
		// and since the store returns that error, every subsequent open failed
		// the same way, with nothing able to repair it. Introspection does not
		// read notnull, so the diff could never see what had happened.
		return "INTEGER " + entityRefReference
	}
	return propertySQLType(prop) + " NOT NULL"
}
