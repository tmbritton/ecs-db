package storage

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// DomainSchema is the "as-built" database schema reconstructed from introspection.
type DomainSchema struct {
	SchemaVersion   int                        // From meta table, 0 if no metadata
	Components      map[string]DomainComponent // Key = lowercase name ("position")
	EntityTypeNames map[string]bool            // Distinct entity_type values
}

// DomainComponent represents a component table's structure as found in the DB.
type DomainComponent struct {
	Type    string // "object", "string", "integer", etc.
	Columns []DomainColumn
}

// DomainColumn represents a single column in a component table.
type DomainColumn struct {
	Name    string
	SQLType string
	Default string // Default value expression from PRAGMA, empty if none
	IsPK    bool
}

func (c DomainColumn) DefaultVal() string {
	return c.Default
}

// ListComponentTables returns the names of all component tables (comp_*)
// in the database, sorted alphabetically.
func ListComponentTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(
		"SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'comp_%' ORDER BY name",
	)
	if err != nil {
		return nil, fmt.Errorf("querying sqlite_master for component tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning component table name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating component tables: %w", err)
	}
	return names, nil
}

// ReadSchemaVersion reads the stored schema_version from the meta table.
func ReadSchemaVersion(db *sql.DB) (int, error) {
	var stored string
	err := db.QueryRow(
		"SELECT value FROM meta WHERE key = 'schema_version'",
	).Scan(&stored)
	if err != nil {
		return 0, fmt.Errorf("reading stored schema_version: %w", err)
	}
	v, err := strconv.Atoi(stored)
	if err != nil {
		return 0, fmt.Errorf("corrupted schema_version in meta: %q", stored)
	}
	return v, nil
}

// IntrospectComponentTable returns the column definitions for a single
// component table via PRAGMA table_info.
func IntrospectComponentTable(db *sql.DB, tableName string) ([]DomainColumn, error) {
	// Double-quote the identifier to handle names with special characters and
	// prevent SQL injection through attacker-controlled table names.
	quotedName := `"` + strings.ReplaceAll(tableName, `"`, `""`) + `"`
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", quotedName))
	if err != nil {
		return nil, fmt.Errorf("PRAGMA table_info(%s): %w", tableName, err)
	}
	defer func() { _ = rows.Close() }()

	var columns []DomainColumn
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull IntBool
		var dfltValue sql.NullString
		var pk IntBool
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return nil, fmt.Errorf("scanning PRAGMA table_info row: %w", err)
		}
		columns = append(columns, DomainColumn{
			Name:    name,
			SQLType: strings.ToUpper(colType),
			Default: dfltValue.String,
			IsPK:    pk == 1,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating PRAGMA table_info: %w", err)
	}
	return columns, nil
}

// IntBool is a helper for scanning SQLite's 0/1 integers from PRAGMA.
type IntBool int

// Scan implements sql.Scanner for IntBool.
func (ib *IntBool) Scan(value interface{}) error {
	if value == nil {
		*ib = 0
		return nil
	}
	switch v := value.(type) {
	case int64:
		*ib = IntBool(v)
	case float64:
		*ib = IntBool(v)
	case []byte:
		if len(v) > 0 && v[0] != '0' {
			*ib = 1
		}
	case string:
		if v != "0" {
			*ib = 1
		}
	default:
		*ib = 0
	}
	return nil
}

// InferComponentType determines the component type from a list of columns.
// The entity_id column is expected first; it is stripped before inference.
func InferComponentType(cols []DomainColumn) string {
	// Strip entity_id (first PK column).
	dataCols := cols
	if len(cols) > 0 && cols[0].Name == "entity_id" && cols[0].IsPK {
		dataCols = cols[1:]
	}

	switch len(dataCols) {
	case 0:
		return "object" // empty object component
	case 1:
		col := dataCols[0]
		switch {
		case col.Name == "value" && col.SQLType == "TEXT" && strings.HasPrefix(col.DefaultVal(), "'["):
			return "array"
		case col.Name == "value" && col.SQLType == "TEXT":
			return "string"
		case col.Name == "value" && col.SQLType == "INTEGER":
			return "integer"
		case col.Name == "value" && col.SQLType == "REAL":
			return "number"
		case col.Name == "target_entity_id":
			return "entity-ref"
		}
		return "object" // fallback
	default:
		return "object"
	}
}

// IntrospectAll reconstructs the full DomainSchema from a live database.
func IntrospectAll(db *sql.DB) (*DomainSchema, error) {
	result := &DomainSchema{
		Components:      make(map[string]DomainComponent),
		EntityTypeNames: make(map[string]bool),
	}

	// 1. Read schema version.
	version, err := ReadSchemaVersion(db)
	if err != nil {
		return nil, fmt.Errorf("reading schema version: %w", err)
	}
	result.SchemaVersion = version

	// 2. List component tables.
	tableNames, err := ListComponentTables(db)
	if err != nil {
		return nil, fmt.Errorf("listing component tables: %w", err)
	}

	// 3. Introspect each component table.
	for _, tableName := range tableNames {
		columns, err := IntrospectComponentTable(db, tableName)
		if err != nil {
			return nil, fmt.Errorf("introspecting %s: %w", tableName, err)
		}
		compName := strings.TrimPrefix(tableName, "comp_")
		result.Components[compName] = DomainComponent{
			Type:    InferComponentType(columns),
			Columns: columns,
		}
	}

	// 4. Read entity type names.
	rows, err := db.Query("SELECT DISTINCT entity_type FROM entities")
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var et string
			if err := rows.Scan(&et); err != nil {
				return nil, fmt.Errorf("scanning entity_type: %w", err)
			}
			result.EntityTypeNames[et] = true
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterating entity types: %w", err)
		}
		_ = rows.Close()
	}
	// If entities table doesn't exist, we silently skip (empty map).

	return result, nil
}

// ToDiffSchema converts the storage-side DomainSchema to the domain-side
// representation used by schema.Diff(). It strips the Default field (not
// needed for diff) and preserves everything else.
func (ds *DomainSchema) ToDiffSchema() *schema.DomainSchema {
	if ds == nil {
		return nil
	}
	result := &schema.DomainSchema{
		SchemaVersion:   ds.SchemaVersion,
		EntityTypeNames: make(map[string]bool),
		Components:      make(map[string]schema.DomainComponent),
	}
	for k, v := range ds.EntityTypeNames {
		result.EntityTypeNames[k] = v
	}
	for k, v := range ds.Components {
		domCols := make([]schema.DomainColumn, len(v.Columns))
		for i, c := range v.Columns {
			domCols[i] = schema.DomainColumn{
				Name:    c.Name,
				SQLType: c.SQLType,
				IsPK:    c.IsPK,
			}
		}
		result.Components[k] = schema.DomainComponent{
			Type:    v.Type,
			Columns: domCols,
		}
	}
	return result
}
