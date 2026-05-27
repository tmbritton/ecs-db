package storage

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// Statement represents a single DDL operation or a line within a
// multi-statement operation (e.g. table rebuild).
type Statement struct {
	SQL         string // The raw SQL to execute
	Kind        string // "create_table", "alter_add_column", "rebuild_table", "drop_table"
	Destructive bool   // true for DROP TABLE, column removal, type change
	Component   string // affected component (lowercase)
	Description string // human-readable summary
}

// Config holds generator options.
type Config struct {
	// StrictDrop, when false, filters out all destructive statements
	// (DROP TABLE, column removal, type change). True returns everything.
	StrictDrop bool
}

// Generator translates schema.Change entries into SQL DDL statements.
// It requires the file schema for component definitions and optionally
// the domain (DB) schema for column-level rebuild operations.
type Generator struct {
	file   *schema.DatabaseSchema
	domain *DomainSchema
	config Config
}

// NewGenerator creates a DDL generator.
// Panics if file is nil — changes cannot be resolved without a file schema.
func NewGenerator(file *schema.DatabaseSchema, domain *DomainSchema, config Config) *Generator {
	if file == nil {
		panic("ddlgen: file schema must not be nil")
	}
	return &Generator{
		file:   file,
		domain: domain,
		config: config,
	}
}

// Generate converts a list of changes into DDL statements.
// Empty or nil changes produce an empty (non-nil) statement slice.
// Entity type changes are silently skipped (no DDL).
func (g *Generator) Generate(changes []schema.Change) []Statement {
	if changes == nil {
		return []Statement{}
	}

	stmts := make([]Statement, 0)
	for _, change := range changes {
		stmts = append(stmts, g.genChange(change)...)
	}

	// Structural change reorder: if the same component has both a DROP
	// and a CREATE, ensure DROP comes first so the old table is gone
	// before creating the new one.
	stmts = reorderStructuralChanges(stmts)

	// Filter destructive statements if StrictDrop is false.
	if !g.config.StrictDrop {
		return filterNonDestructive(stmts)
	}

	return stmts
}

// genChange dispatches a single change to the appropriate generator.
func (g *Generator) genChange(c schema.Change) []Statement {
	switch c.Kind {
	case schema.ChangeAddedComponent:
		return g.genAddComponent(c)
	case schema.ChangeAddedProperty:
		return g.genAddProperty(c)
	case schema.ChangeRemovedProperty:
		return g.genRemoveProperty(c)
	case schema.ChangedPropertyType:
		return g.genChangePropertyType(c)
	case schema.ChangeRemovedComponent:
		return g.genRemoveComponent(c)
	// Entity type changes produce no DDL.
	case schema.ChangeAddedEntityType,
		schema.ChangeRemovedEntityType,
		schema.ChangeChangedEntityType:
		return nil
	default:
		return nil
	}
}

// genAddComponent produces a CREATE TABLE via the existing componentTableSQL.
func (g *Generator) genAddComponent(c schema.Change) []Statement {
	comp, canonicalName := schema.ComponentByName(g.file, c.Component)
	if canonicalName == "" {
		return []Statement{{
			Kind:        "error",
			Destructive: false,
			Component:   c.Component,
			Description: "ERROR: unknown component " + c.Component,
		}}
	}
	sql, err := componentTableSQL(canonicalName, comp)
	if err != nil {
		return []Statement{{
			Kind:        "error",
			Destructive: false,
			Component:   c.Component,
			Description: "ERROR: " + err.Error(),
		}}
	}
	return []Statement{{
		SQL:         sql,
		Kind:        "create_table",
		Destructive: false,
		Component:   c.Component,
		Description: "Create component table comp_" + c.Component,
	}}
}

// genAddProperty produces an ALTER TABLE ADD COLUMN statement.
func (g *Generator) genAddProperty(c schema.Change) []Statement {
	comp, canonicalName := schema.ComponentByName(g.file, c.Component)
	if canonicalName == "" || comp.Type != schema.ComponentTypeObject {
		return []Statement{{
			Kind:        "error",
			Destructive: false,
			Component:   c.Component,
			Description: "ERROR: unknown property or non-object component " + c.Component,
		}}
	}

	// Look up the property definition.
	prop, found := schema.PropertyByName(comp.Properties, c.Property)
	if !found {
		return []Statement{{
			Kind:        "error",
			Destructive: false,
			Component:   c.Component,
			Description: "ERROR: unknown property " + c.Property + " on comp_" + c.Component,
		}}
	}

	sqlType := schema.PropertySQLType(prop)
	dflt := defaultValueForProperty(prop)
	// entity-ref columns must be nullable when added to an existing table:
	// SQLite rejects NOT NULL DEFAULT NULL on ALTER TABLE ADD COLUMN when rows exist.
	notNullClause := " NOT NULL"
	if prop.Type == schema.PropertyTypeEntityRef {
		notNullClause = ""
	}
	extraClause := ""
	if prop.Type == schema.PropertyTypeEntityRef {
		extraClause = " REFERENCES entities(id)"
	}
	sql := fmt.Sprintf("ALTER TABLE comp_%s ADD COLUMN %s %s%s DEFAULT %s%s",
		c.Component, c.Property, sqlType, notNullClause, dflt, extraClause)

	return []Statement{{
		SQL:         sql,
		Kind:        "alter_add_column",
		Destructive: false,
		Component:   c.Component,
		Description: fmt.Sprintf("Add column %q to comp_%s", c.Property, c.Component),
	}}
}

// genRemoveComponent produces a DROP TABLE IF EXISTS statement.
func (g *Generator) genRemoveComponent(c schema.Change) []Statement {
	return []Statement{{
		SQL:         fmt.Sprintf("DROP TABLE IF EXISTS comp_%s", c.Component),
		Kind:        "drop_table",
		Destructive: true,
		Component:   c.Component,
		Description: "Drop component table comp_" + c.Component,
	}}
}

// genRemoveProperty produces a table-rebuild sequence to drop a column.
func (g *Generator) genRemoveProperty(c schema.Change) []Statement {
	if g.domain == nil {
		return []Statement{{
			Kind:        "error",
			Destructive: true,
			Component:   c.Component,
			Description: "ERROR: cannot rebuild comp_" + c.Component + " — no domain schema available",
		}}
	}
	return g.genRebuild(c.Component, &c)
}

// genChangePropertyType produces a table-rebuild sequence to change a column's type.
func (g *Generator) genChangePropertyType(c schema.Change) []Statement {
	if g.domain == nil {
		return []Statement{{
			Kind:        "error",
			Destructive: true,
			Component:   c.Component,
			Description: "ERROR: cannot rebuild comp_" + c.Component + " — no domain schema available",
		}}
	}
	return g.genRebuild(c.Component, &c)
}

// genRebuild generates the table-rebuild SQL sequence:
//
//	PRAGMA foreign_keys = OFF;
//	CREATE TABLE comp_<name>_new (...);
//	INSERT INTO comp_<name>_new SELECT <cols> FROM comp_<name>;
//	DROP TABLE comp_<name>;
//	ALTER TABLE comp_<name>_new RENAME TO comp_<name>;
//	PRAGMA foreign_keys = ON;
func (g *Generator) genRebuild(compName string, change *schema.Change) []Statement {
	// Look up file component definition.
	comp, canonicalName := schema.ComponentByName(g.file, compName)
	if canonicalName == "" {
		return []Statement{{
			Kind:        "error",
			Destructive: true,
			Component:   compName,
			Description: "ERROR: unknown component " + compName,
		}}
	}

	// Look up domain (DB) component to verify the table exists before rebuilding.
	if _, ok := g.domain.Components[compName]; !ok {
		return []Statement{{
			Kind:        "error",
			Destructive: true,
			Component:   compName,
			Description: "ERROR: comp_" + compName + " not found in domain schema",
		}}
	}

	// Build the new column list from the file schema.
	newCols := buildNewColumns(comp)

	// Build the named column list for INSERT ... SELECT. We derive column
	// names from the new table layout (entity_id first, then the new schema's
	// properties in sorted order). Using named columns ensures entity_id is
	// preserved and column ordering mismatches between old and new tables
	// cannot cause data to land in the wrong column.
	colNames := make([]string, 0, len(newCols))
	for _, colDef := range newCols {
		colNames = append(colNames, strings.Fields(colDef)[0])
	}
	colList := strings.Join(colNames, ", ")

	tableName := "comp_" + compName
	tempName := tableName + "_new"

	stmts := make([]Statement, 0, 4)

	// 1. CREATE TABLE comp_<name>_new (...)
	// PRAGMA foreign_keys toggle is handled by the caller (MigrationRunner)
	// outside the transaction, since SQLite ignores it inside a transaction.
	createSQL := buildCreateTable(tempName, compName, newCols)
	stmts = append(stmts, Statement{
		SQL:         createSQL,
		Kind:        "rebuild_table",
		Destructive: true,
		Component:   compName,
		Description: "Create temp table " + tempName,
	})

	// 2. INSERT INTO comp_<name>_new (cols) SELECT cols FROM comp_<name>
	// Named columns preserve entity_id and survive column-order differences.
	selectSQL := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
		tempName, colList, colList, tableName)
	stmts = append(stmts, Statement{
		SQL:         selectSQL,
		Kind:        "rebuild_table",
		Destructive: true,
		Component:   compName,
		Description: "Copy data from " + tableName,
	})

	// 3. DROP TABLE comp_<name>
	stmts = append(stmts, Statement{
		SQL:         "DROP TABLE " + tableName,
		Kind:        "rebuild_table",
		Destructive: true,
		Component:   compName,
		Description: "Drop old table " + tableName,
	})

	// 4. ALTER TABLE comp_<name>_new RENAME TO comp_<name>
	stmts = append(stmts, Statement{
		SQL:         "ALTER TABLE " + tempName + " RENAME TO " + tableName,
		Kind:        "rebuild_table",
		Destructive: true,
		Component:   compName,
		Description: "Rename temp table to " + tableName,
	})

	return stmts
}

// buildNewColumns generates the column definitions for a rebuild table.
func buildNewColumns(comp schema.Component) []string {
	cols := []string{"entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE"}

	switch comp.Type {
	case schema.ComponentTypeObject:
		// Collect property names, sort for determinism.
		names := make([]string, 0, len(comp.Properties))
		for name := range comp.Properties {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, propName := range names {
			prop := comp.Properties[propName]
			sqlType := schema.PropertySQLType(prop)
			cols = append(cols, fmt.Sprintf("%s %s NOT NULL",
				strings.ToLower(propName), sqlType))
		}

	case schema.ComponentTypeEntityRef:
		cols = append(cols, "target_entity_id INTEGER NOT NULL REFERENCES entities(id)")
	case schema.ComponentTypeArray:
		cols = append(cols, "value TEXT NOT NULL DEFAULT '[]'")
	case schema.ComponentTypeString:
		cols = append(cols, "value TEXT NOT NULL DEFAULT ''")
	case schema.ComponentTypeInteger, schema.ComponentTypeBoolean:
		cols = append(cols, "value INTEGER NOT NULL DEFAULT 0")
	case schema.ComponentTypeNumber:
		cols = append(cols, "value REAL NOT NULL DEFAULT 0.0")
	}

	return cols
}

// buildCreateTable generates a CREATE TABLE statement.
func buildCreateTable(tableName string, compName string, cols []string) string {
	sql := fmt.Sprintf("CREATE TABLE %s (\n", tableName)
	colDefs := make([]string, len(cols))
	for i, c := range cols {
		colDefs[i] = "\t" + c
	}
	sql += strings.Join(colDefs, ",\n")
	sql += "\n)"
	return sql
}

// defaultValueForProperty returns the SQL DEFAULT expression for a property
// type, used in ALTER TABLE ADD COLUMN statements.
func defaultValueForProperty(p schema.Property) string {
	switch p.Type {
	case schema.PropertyTypeString:
		return "''"
	case schema.PropertyTypeInteger:
		return "0"
	case schema.PropertyTypeNumber:
		return "0.0"
	case schema.PropertyTypeBoolean:
		return "0"
	case schema.PropertyTypeEntityRef:
		return "NULL"
	case schema.PropertyTypeObject:
		return "'{}'"
	case schema.PropertyTypeArray:
		return "'[]'"
	default:
		return "NULL"
	}
}

// reorderStructuralChanges ensures that for the same component, any
// DROP TABLE comes before CREATE TABLE. This handles the case where
// schema.Diff() emits a structural incompatibility (object↔scalar) as
// remove+add but phase ordering puts add before remove.
//
// The algorithm handles multiple simultaneous structural incompatibilities
// correctly by collecting which DROPs need to move, then doing a single
// output pass rather than mutating positions incrementally.
func reorderStructuralChanges(stmts []Statement) []Statement {
	// Map component name → DROP statement index.
	dropIdx := map[string]int{}
	// Map component name → CREATE statement index.
	createIdx := map[string]int{}
	for i, s := range stmts {
		switch s.Kind {
		case "drop_table":
			dropIdx[s.Component] = i
		case "create_table":
			createIdx[s.Component] = i
		}
	}

	// Identify DROPs that must be hoisted before their paired CREATE.
	// These are DROPs whose original position is after the paired CREATE.
	hoistedDrops := map[int]bool{}
	for comp, ci := range createIdx {
		di, hasDrop := dropIdx[comp]
		if hasDrop && di > ci {
			hoistedDrops[di] = true
		}
	}

	// Single output pass: skip hoisted DROPs at their original position;
	// emit each hoisted DROP immediately before its paired CREATE.
	reordered := make([]Statement, 0, len(stmts))
	for i, s := range stmts {
		if hoistedDrops[i] {
			continue // already emitted before its CREATE
		}
		if s.Kind == "create_table" {
			di, hasDrop := dropIdx[s.Component]
			if hasDrop && di > i {
				reordered = append(reordered, stmts[di]) // DROP first
			}
		}
		reordered = append(reordered, s)
	}
	return reordered
}

// filterNonDestructive returns only non-destructive statements.
func filterNonDestructive(stmts []Statement) []Statement {
	result := make([]Statement, 0)
	for _, s := range stmts {
		if !s.Destructive {
			result = append(result, s)
		}
	}
	return result
}
