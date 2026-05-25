package schema

// PropertySQLType maps a Property's semantic type to its SQLite column type.
// This is the canonical mapping used by both the storage layer (DDL generation)
// and the diff layer (SQL-to-SQL comparison).
func PropertySQLType(p Property) string {
	switch p.Type {
	case PropertyTypeString:
		return "TEXT"
	case PropertyTypeInteger:
		return "INTEGER"
	case PropertyTypeNumber:
		return "REAL"
	case PropertyTypeBoolean:
		return "INTEGER"
	case PropertyTypeEntityRef:
		return "INTEGER"
	case PropertyTypeObject, PropertyTypeArray:
		// Nested structures are stored as JSON.
		return "TEXT"
	default:
		return "TEXT"
	}
}
