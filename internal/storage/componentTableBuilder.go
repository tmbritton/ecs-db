package storage

import (
	"fmt"
	"strings"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// componentTableSQL generates the CREATE TABLE statement for a single component.
// Object components produce one typed column per property. Non-object components
// produce a single "value" column with an appropriate SQL type.
func componentTableSQL(name string, comp schema.Component) (string, error) {
	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS comp_%s (\n", strings.ToLower(name))
	cols := []string{
		"\tentity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE",
	}

	switch comp.Type {
	case schema.ComponentTypeObject:
		for propName, prop := range comp.Properties {
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
