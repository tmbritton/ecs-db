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
			sqlType := propertySQLType(prop)
			col := fmt.Sprintf("\t%s %s NOT NULL", strings.ToLower(propName), sqlType)
			cols = append(cols, col)
		}

	case schema.ComponentTypeEntityRef:
		cols = append(cols, "\ttarget_entity_id INTEGER NOT NULL REFERENCES entities(id)")

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
