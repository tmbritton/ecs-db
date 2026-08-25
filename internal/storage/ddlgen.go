package storage

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tmbritton/ecs-db/internal/jsonorder"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// Statement represents a single DDL operation or a line within a
// multi-statement operation (e.g. table rebuild).
type Statement struct {
	SQL string // The raw SQL to execute
	// Kind is one of "create_table", "alter_add_column", "rebuild_table",
	// "drop_table", "rename_table", "rename_column", "rename_entity_type", or
	// "error" for a change the generator could not express.
	Kind        string
	Destructive bool   // true for DROP TABLE, column removal, type change
	Component   string // affected component (lowercase)
	Description string // human-readable summary

	// Substitutions are the columns this statement reads through a COALESCE,
	// because they refuse a NULL the source column allows. Carried so the
	// runner can say how many rows it actually invented a value for: the
	// difference between a no-op and eight thousand rows quietly taking a zero
	// is not visible in the SQL.
	Substitutions []Substitution
}

// Substitution is one column whose NULLs a rebuild replaces, and with what.
type Substitution struct {
	Column  string
	Default string
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
//
// The component and property names in a Change are interpolated into ALTER
// TABLE and DROP TABLE below without being checked, unlike componentTableSQL,
// which refuses a name that cannot be an identifier. The names come from a
// schema.Diff of two schemas, and schema.ValidateSchema refuses an unusable
// name before either of them can be loaded — so this is reachable only from a
// caller that skipped validation, which is the same caller componentTableSQL
// guards against. Closing it properly means Generate returning an error, which
// is a signature every caller uses; recorded rather than done here.
func (g *Generator) Generate(changes []schema.Change) []Statement {
	if changes == nil {
		return []Statement{}
	}

	// One rebuild per component, not one per change that wants one.
	//
	// genRebuild ignores the change it is handed: it builds the whole table
	// from the file schema, so two rebuild-causing changes on one component
	// produced two identical four-statement sequences. That happened to work —
	// the second copied from a table already in the new shape — and became
	// common once a constraint could ask for a rebuild too, since a stale
	// database has a constraint change and a property change on the same
	// component.
	// The reasons are collected first so the one rebuild that survives can
	// carry all of them. Dropping the later sequences would otherwise drop
	// what they said, and what a rebuild is for is the whole content of the
	// confirmation dialog it appears in.
	reasons := rebuildReasons(changes)

	stmts := make([]Statement, 0)
	rebuilt := map[string]bool{}
	for _, change := range changes {
		if causesRebuild(change.Kind) {
			if rebuilt[change.Component] {
				continue
			}
			rebuilt[change.Component] = true
			stmts = append(stmts, g.genRebuildFor(change, reasons[change.Component])...)
			continue
		}
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
	case schema.ChangeRenamedComponent:
		return g.genRenameComponent(c)
	case schema.ChangeRenamedProperty:
		return g.genRenameProperty(c)
	case schema.ChangeRenamedEntityType:
		return g.genRenameEntityType(c)
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
		Description: describe("Create component table comp_"+c.Component, c.Reason),
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
	// An entity-ref column is nullable, which is what SQLite requires here —
	// it rejects NOT NULL DEFAULT NULL on ALTER TABLE ADD COLUMN when rows
	// exist — and, since columnConstraint declares the same thing, is what the
	// created and rebuilt forms of the table say too. That agreement is
	// load-bearing: see columnConstraint for the database it used to wedge.
	notNullClause := " NOT NULL"
	extraClause := ""
	if schema.PropertyNullable(prop.Type) {
		notNullClause = ""
		extraClause = " " + entityRefReference
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

// genRenameComponent moves a table rather than dropping it and building an
// empty one.
//
// Not destructive: ALTER TABLE ... RENAME TO keeps every row, every column type
// and every foreign key, including the ones other tables hold on this one. The
// whole point of the change is that nothing is lost, so MigrationConfirm has
// nothing to stop.
func (g *Generator) genRenameComponent(c schema.Change) []Statement {
	return []Statement{{
		SQL:         fmt.Sprintf("ALTER TABLE comp_%s RENAME TO comp_%s", c.OldName, c.Component),
		Kind:        "rename_table",
		Destructive: false,
		Component:   c.Component,
		Description: describe(
			fmt.Sprintf("Rename comp_%s to comp_%s", c.OldName, c.Component), c.Reason),
	}}
}

// genRenameProperty moves a column. RENAME COLUMN keeps the data, the type, the
// NOT NULL and the foreign key — and a foreign key follows the column it is on,
// so a renamed entity-ref property still cascades.
func (g *Generator) genRenameProperty(c schema.Change) []Statement {
	return []Statement{{
		SQL: fmt.Sprintf("ALTER TABLE comp_%s RENAME COLUMN %s TO %s",
			c.Component, c.OldName, c.Property),
		Kind:        "rename_column",
		Destructive: false,
		Component:   c.Component,
		Description: describe(
			fmt.Sprintf("Rename comp_%s.%s to %s", c.Component, c.OldName, c.Property), c.Reason),
	}}
}

// genRenameEntityType is the one rename that is not DDL. An entity's type is a
// string in a column, so the entities filed under the old name have to be moved
// to the new one or they stop matching any type the schema declares.
func (g *Generator) genRenameEntityType(c schema.Change) []Statement {
	return []Statement{{
		SQL: fmt.Sprintf("UPDATE entities SET entity_type = %s WHERE entity_type = %s",
			sqlQuote(c.ETName), sqlQuote(c.OldName)),
		Kind:        "rename_entity_type",
		Destructive: false,
		Description: describe(
			fmt.Sprintf("Refile entities of type %q as %q", c.OldName, c.ETName), c.Reason),
	}}
}

// sqlQuote renders a string literal. Entity type names are not identifiers and
// are not validated as such — they are values in a column, and may contain
// anything a JSON string can.
func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// genRemoveComponent produces a DROP TABLE IF EXISTS statement.
func (g *Generator) genRemoveComponent(c schema.Change) []Statement {
	return []Statement{{
		SQL:         fmt.Sprintf("DROP TABLE IF EXISTS comp_%s", c.Component),
		Kind:        "drop_table",
		Destructive: true,
		Component:   c.Component,
		Description: describe("Drop component table comp_"+c.Component, c.Reason),
	}}
}

// genRebuildFor produces the rebuild sequence answering one change, carrying
// the reasons every change that wanted this table rebuilt gave.
//
// A dropped column, a retyped column and a redeclared foreign key all need the
// same thing, because SQLite has no ALTER for any of them: the table is built
// again from the file schema and the rows copied across.
func (g *Generator) genRebuildFor(c schema.Change, reason string) []Statement {
	if g.domain == nil {
		return []Statement{{
			Kind:        "error",
			Destructive: true,
			Component:   c.Component,
			Description: "ERROR: cannot rebuild comp_" + c.Component + " — no domain schema available",
		}}
	}
	return g.genRebuild(c.Component, reason)
}

// causesRebuild reports whether a change is answered with a table rebuild —
// which is to say, with the same four statements as any other such change on
// the same component.
func causesRebuild(k schema.ChangeKind) bool {
	switch k {
	case schema.ChangeRemovedProperty, schema.ChangedPropertyType,
		schema.ChangeChangedConstraint, schema.ChangeChangedNullability:
		return true
	default:
		return false
	}
}

// rebuildReasons joins, per component, what each change that wants a rebuild
// had to say for itself. Changes with nothing to say contribute nothing, so a
// component whose rebuild needs no explaining keeps the description it had.
func rebuildReasons(changes []schema.Change) map[string]string {
	byComp := map[string][]string{}
	for _, c := range changes {
		if !causesRebuild(c.Kind) || c.Reason == "" {
			continue
		}
		if !slices.Contains(byComp[c.Component], c.Reason) {
			byComp[c.Component] = append(byComp[c.Component], c.Reason)
		}
	}
	out := make(map[string]string, len(byComp))
	for comp, reasons := range byComp {
		out[comp] = strings.Join(reasons, "; ")
	}
	return out
}

// genRebuild generates the table-rebuild SQL sequence:
//
//	PRAGMA foreign_keys = OFF;
//	CREATE TABLE comp_<name>_new (...);
//	INSERT INTO comp_<name>_new SELECT <cols> FROM comp_<name>;
//	DROP TABLE comp_<name>;
//	ALTER TABLE comp_<name>_new RENAME TO comp_<name>;
//	PRAGMA foreign_keys = ON;
func (g *Generator) genRebuild(compName, reason string) []Statement {
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

	// The named column list for INSERT ... SELECT. Named rather than positional
	// so entity_id is preserved and a column-order difference between the old
	// and new tables cannot land data in the wrong column.
	//
	// The two sides differ where a column that used to accept NULL no longer
	// does: the destination is the plain name, the source substitutes the value
	// the column would have been given if it were being added now. See
	// copyExpression.
	dbCols := g.domain.Components[compName].Columns
	names := make([]string, 0, len(newCols))
	reads := make([]string, 0, len(newCols))
	var subs []Substitution
	for _, c := range newCols {
		names = append(names, c.Name)
		read := copyExpression(c, dbCols)
		reads = append(reads, read)
		if read != c.Name {
			subs = append(subs, Substitution{Column: c.Name, Default: c.Default})
		}
	}
	colList := strings.Join(names, ", ")
	readList := strings.Join(reads, ", ")

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
		Description: describe("Create temp table "+tempName, reason),
	})

	// 2. INSERT INTO comp_<name>_new (cols) SELECT cols FROM comp_<name>
	// Named columns preserve entity_id and survive column-order differences.
	selectSQL := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
		tempName, colList, readList, tableName)
	stmts = append(stmts, Statement{
		SQL:           selectSQL,
		Kind:          "rebuild_table",
		Destructive:   true,
		Component:     compName,
		Description:   "Copy data from " + tableName,
		Substitutions: subs,
	})

	// 3. DROP TABLE comp_<name>
	stmts = append(stmts, Statement{
		SQL:         "DROP TABLE " + tableName,
		Kind:        "rebuild_table",
		Destructive: true,
		Component:   compName,
		Description: describe("Drop old table "+tableName, reason),
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

// rebuildColumn is one column of the table a rebuild is about to create.
//
// A struct rather than the DDL string this used to be. The generator has to know
// each column's name to write the INSERT, and whether it refuses NULL and what
// it would default to in order to write the SELECT — and it used to recover the
// name by splitting its own output on whitespace, which is a parser for a format
// it had just finished emitting.
type rebuildColumn struct {
	Name    string
	DDL     string // the whole definition, as it goes into CREATE TABLE
	NotNull bool
	// Default is what ALTER TABLE ADD COLUMN would give existing rows for this
	// property type — not necessarily what appears in DDL, which for an object
	// property is nothing at all. It is here so the copy can put a value where a
	// NULL used to be, from the same source the ALTER path uses.
	Default string
}

// buildNewColumns generates the column definitions for a rebuild table.
func buildNewColumns(comp schema.Component) []rebuildColumn {
	cols := []rebuildColumn{{
		Name: "entity_id",
		DDL:  "entity_id INTEGER PRIMARY KEY " + entityRefReference,
	}}

	switch comp.Type {
	case schema.ComponentTypeObject:
		// The same rule componentTableSQL uses, so a rebuilt table comes out
		// with its columns in the same order as the original. These two halves
		// of the generator disagreed: this one sorted and that one used map
		// order.
		for _, propName := range jsonorder.Apply(comp.PropertyOrder, comp.Properties) {
			prop := comp.Properties[propName]
			name := strings.ToLower(propName)
			// Through the same helper componentTableSQL uses, so a rebuilt
			// table carries the constraints a created one does. It did not: an
			// entity-ref property came out of a rebuild with no foreign key,
			// silently dropping one an ALTER TABLE had added.
			cols = append(cols, rebuildColumn{
				Name:    name,
				DDL:     fmt.Sprintf("%s %s", name, columnConstraint(prop)),
				NotNull: !schema.PropertyNullable(prop.Type),
				Default: defaultValueForProperty(prop),
			})
		}

	case schema.ComponentTypeEntityRef:
		// Reached when an entity-ref component's stored column type is not
		// INTEGER — a hand-edited table, or one built before entity-ref had a
		// settled shape. A change *between* entity-ref and another layout no
		// longer arrives here: schema.Diff makes that remove-and-add, because
		// no ALTER renames value into target_entity_id.
		//
		// No Default: there is no honest value for a reference that is missing,
		// which is the whole reason this column is NOT NULL. A row with a NULL
		// in it cannot be carried across, and nothing here pretends otherwise.
		cols = append(cols, rebuildColumn{
			Name: "target_entity_id", DDL: "target_entity_id " + entityRefColumnType, NotNull: true,
		})
	case schema.ComponentTypeArray:
		cols = append(cols, scalarColumn("TEXT", "'[]'"))
	case schema.ComponentTypeString:
		cols = append(cols, scalarColumn("TEXT", "''"))
	case schema.ComponentTypeInteger, schema.ComponentTypeBoolean:
		cols = append(cols, scalarColumn("INTEGER", "0"))
	case schema.ComponentTypeNumber:
		cols = append(cols, scalarColumn("REAL", "0.0"))
	}

	return cols
}

// scalarColumn is the single data column of a non-object component.
func scalarColumn(sqlType, dflt string) rebuildColumn {
	return rebuildColumn{
		Name:    "value",
		DDL:     fmt.Sprintf("value %s NOT NULL DEFAULT %s", sqlType, dflt),
		NotNull: true,
		Default: dflt,
	}
}

// copyExpression is what the rebuild's SELECT reads for one column.
//
// The plain column name, except where carrying it across would fail: a column
// that refuses NULL, reading from one that allowed it. Then it substitutes the
// column's default — the same value ALTER TABLE ADD COLUMN gives existing rows
// when a NOT NULL property is added, so the two ways of arriving at "this column
// now has to hold a value and these rows do not have one" give the same answer.
// They used to disagree completely: adding invented a value, retyping made the
// database refuse to open ever again.
//
// Narrow on purpose. Without drift there is nothing to coalesce and the SQL is
// what it always was, which matters because Forge shows this text to somebody
// deciding whether to allow the migration.
func copyExpression(c rebuildColumn, dbCols []DomainColumn) string {
	if !c.NotNull || c.Default == "" || c.Default == "NULL" {
		return c.Name
	}
	for _, old := range dbCols {
		if strings.EqualFold(old.Name, c.Name) && old.Nullable && !old.IsPK {
			return fmt.Sprintf("COALESCE(%s, %s)", c.Name, c.Default)
		}
	}
	return c.Name
}

// buildCreateTable generates a CREATE TABLE statement.
func buildCreateTable(tableName string, compName string, cols []rebuildColumn) string {
	sql := fmt.Sprintf("CREATE TABLE %s (\n", tableName)
	colDefs := make([]string, len(cols))
	for i, c := range cols {
		colDefs[i] = "\t" + c.DDL
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
// schema.Diff() emits a shape change as remove+add but phase ordering puts add
// before remove. A shape change is one the columns cannot be altered through:
// object → a single fixed column, or between "value" and "target_entity_id".
// Not the reverse — a scalar becoming an object keeps its table, because any
// columns can be added and dropped into an object's.
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

// describe appends a change's reason to a statement description, when it has
// one. A drop reads the same whether somebody deleted a component or changed
// its shape, and only one of those is a surprise worth explaining.
func describe(what, reason string) string {
	if reason == "" {
		return what
	}
	return what + " — " + reason
}
