package storage

import (
	"database/sql"
	"fmt"
	"sort"
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
	// References is the column's foreign key, as PRAGMA foreign_key_list
	// reports it and as schema.EntityReference is written — or
	// schema.NoReference when there is none. See schema.DomainColumn for why
	// the two are different answers.
	References string
	// Nullable is the negation of PRAGMA table_info's notnull, and is only
	// meaningful for data columns. See schema.ColumnNullable.
	Nullable bool
}

func (c DomainColumn) DefaultVal() string {
	return c.Default
}

// ListComponentTables returns the names of all component tables (comp_*)
// in the database, sorted alphabetically.
func ListComponentTables(db *sql.DB) ([]string, error) {
	// The underscore is escaped. In SQL LIKE, "_" matches any single character,
	// so 'comp_%' also matches "company", "compass" and "compact_things" — any
	// table whose name begins with "comp". Such a table was introspected as a
	// component, TrimPrefix left its name untouched because it does not start
	// with "comp_", and the diff asked for "comp_compact_things" to be dropped:
	// a table that does not exist, so the DROP succeeded, changed nothing, and
	// was asked for again next time.
	//
	// That used to be bounded — it fired only on a version bump, and the bump
	// itself made it stop. Since the migration no longer waits for a version to
	// move, it would repeat on every open of the database, backup and all,
	// forever.
	rows, err := db.Query(
		`SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'comp\_%' ESCAPE '\' ORDER BY name`,
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

	refs, err := introspectReferences(db, quotedName, tableName)
	if err != nil {
		return nil, err
	}

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
			Name:       name,
			SQLType:    strings.ToUpper(colType),
			Default:    dfltValue.String,
			IsPK:       pk == 1,
			References: refs[name],
			Nullable:   notNull == 0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating PRAGMA table_info: %w", err)
	}
	return columns, nil
}

// introspectReferences reads a table's foreign keys, keyed by the column each
// one is declared on.
//
// PRAGMA table_info does not report constraints, so this is a second query and
// the reason a column's foreign key was invisible to the diff until Story 11.
// Two details of what SQLite returns are load-bearing. The rows come back in
// reverse declaration order rather than column order, so they are keyed by the
// "from" column instead of zipped against table_info. And "to" is NULL for a
// reference written without naming a column — "REFERENCES entities" — which no
// generated table has and a hand-edited one might; it is reported as the
// table alone, so it cannot be mistaken for the canonical form.
//
// A foreign key spanning several columns arrives as one row per column sharing
// an id. The engine never emits one; if a database has one, every column in it
// is reported with the whole key, which no single-column expectation matches —
// so it is repaired into the canonical shape rather than silently accepted.
func introspectReferences(db *sql.DB, quotedName, tableName string) (map[string]string, error) {
	rows, err := db.Query(fmt.Sprintf(
		`SELECT id, seq, "table", "from", "to", on_delete FROM pragma_foreign_key_list(%s)`,
		quotedName))
	if err != nil {
		return nil, fmt.Errorf("PRAGMA foreign_key_list(%s): %w", tableName, err)
	}
	defer func() { _ = rows.Close() }()

	type part struct {
		seq      int
		from     string
		to       sql.NullString
		table    string
		onDelete string
	}
	byID := map[int][]part{}
	order := make([]int, 0)
	for rows.Next() {
		var p part
		var id int
		if err := rows.Scan(&id, &p.seq, &p.table, &p.from, &p.to, &p.onDelete); err != nil {
			return nil, fmt.Errorf("scanning foreign key of %s: %w", tableName, err)
		}
		if _, seen := byID[id]; !seen {
			order = append(order, id)
		}
		byID[id] = append(byID[id], p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating foreign keys of %s: %w", tableName, err)
	}

	perColumn := map[string][]string{}
	for _, id := range order {
		parts := byID[id]
		sort.Slice(parts, func(i, j int) bool { return parts[i].seq < parts[j].seq })

		targets := make([]string, 0, len(parts))
		for _, p := range parts {
			if p.to.Valid {
				targets = append(targets, p.to.String)
			}
		}
		ref := parts[0].table
		if len(targets) > 0 {
			ref += "(" + strings.Join(targets, ", ") + ")"
		}
		ref += " ON DELETE " + parts[0].onDelete

		for _, p := range parts {
			perColumn[p.from] = append(perColumn[p.from], ref)
		}
	}

	// Every key on a column, joined, rather than whichever one was declared
	// last. SQLite allows a column to carry more than one — "x INTEGER
	// REFERENCES entities(id) ON DELETE CASCADE REFERENCES entities(id) ON
	// DELETE SET NULL" is a legal column — and keeping only one of them made a
	// column carrying the canonical key *plus* a stray one read as correct and
	// never get repaired. Two keys is not the shape the generator emits, so the
	// joined text matches no expectation and the table is rebuilt into the one
	// key it should have. Sorted, so the answer does not depend on declaration
	// order.
	refs := make(map[string]string, len(perColumn))
	for col, keys := range perColumn {
		if len(keys) > 1 {
			sort.Strings(keys)
		}
		refs[col] = strings.Join(keys, " + ")
	}
	return refs, nil
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

// withRenames returns the schema as it will be once the rename statements in
// changes have run.
//
// schema.Diff decides what the renames are — this only replays them onto the
// storage-side representation, which the generator reads. Doing the deciding
// twice, on two types, is how the two halves came to disagree in the first
// place.
func (ds *DomainSchema) withRenames(changes []schema.Change) *DomainSchema {
	if ds == nil {
		return nil
	}
	out := &DomainSchema{
		SchemaVersion:   ds.SchemaVersion,
		Components:      make(map[string]DomainComponent, len(ds.Components)),
		EntityTypeNames: make(map[string]bool, len(ds.EntityTypeNames)),
	}
	for k, v := range ds.Components {
		cols := make([]DomainColumn, len(v.Columns))
		copy(cols, v.Columns)
		v.Columns = cols
		out.Components[k] = v
	}
	for k, v := range ds.EntityTypeNames {
		out.EntityTypeNames[k] = v
	}

	// Components first: a property rename names its component by the new name.
	for _, c := range changes {
		if c.Kind != schema.ChangeRenamedComponent {
			continue
		}
		if comp, ok := out.Components[c.OldName]; ok {
			out.Components[c.Component] = comp
			delete(out.Components, c.OldName)
		}
	}
	for _, c := range changes {
		if c.Kind != schema.ChangeRenamedProperty {
			continue
		}
		comp, ok := out.Components[c.Component]
		if !ok {
			continue
		}
		for i := range comp.Columns {
			if strings.EqualFold(comp.Columns[i].Name, c.OldName) {
				comp.Columns[i].Name = c.Property
			}
		}
		out.Components[c.Component] = comp
	}
	for _, c := range changes {
		if c.Kind != schema.ChangeRenamedEntityType {
			continue
		}
		if out.EntityTypeNames[c.OldName] {
			delete(out.EntityTypeNames, c.OldName)
			out.EntityTypeNames[c.ETName] = true
		}
	}
	return out
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
				Name:       c.Name,
				SQLType:    c.SQLType,
				IsPK:       c.IsPK,
				References: c.References,
				Nullable:   c.Nullable,
			}
		}
		result.Components[k] = schema.DomainComponent{
			Type:    v.Type,
			Columns: domCols,
		}
	}
	return result
}
