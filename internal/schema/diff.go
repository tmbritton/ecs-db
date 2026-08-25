package schema

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// DomainSchema is the "as-built" database schema projected into the domain
// for diff comparison. Storage converts its internal representation to this.
type DomainSchema struct {
	SchemaVersion   int
	Components      map[string]DomainComponent // key = lowercase name
	EntityTypeNames map[string]bool
}

// DomainComponent represents a component table's structure as found in the DB.
type DomainComponent struct {
	Type    string         // "object", "string", "integer", etc.
	Columns []DomainColumn // ordered by PRAGMA cid
}

// DomainColumn represents a single column in a component table.
type DomainColumn struct {
	Name    string
	SQLType string
	IsPK    bool
	// References is the foreign key on this column as PRAGMA
	// foreign_key_list reports it — "entities(id) ON DELETE CASCADE" — and
	// NoReference when the column has none. A foreign key with no ON DELETE
	// clause reads as "... ON DELETE NO ACTION", which is a different answer
	// from having no foreign key: the first refuses the delete, the second
	// leaves a dangling reference.
	References string
	// Nullable is the negation of PRAGMA table_info's notnull. It is only
	// meaningful for data columns: SQLite reports every INTEGER PRIMARY KEY as
	// nullable, so it says nothing about entity_id. See ColumnNullable.
	Nullable bool
}

// ChangeKind identifies the category of a schema change.
type ChangeKind string

const (
	ChangeAddedComponent     ChangeKind = "added_component"
	ChangeRemovedComponent   ChangeKind = "removed_component"
	ChangeAddedProperty      ChangeKind = "added_property"
	ChangeRemovedProperty    ChangeKind = "removed_property"
	ChangedPropertyType      ChangeKind = "changed_property_type"
	ChangeChangedConstraint  ChangeKind = "changed_constraint"
	ChangeChangedNullability ChangeKind = "changed_nullability"
	ChangeRenamedComponent   ChangeKind = "renamed_component"
	ChangeRenamedProperty    ChangeKind = "renamed_property"
	ChangeRenamedEntityType  ChangeKind = "renamed_entity_type"
	ChangeAddedEntityType    ChangeKind = "added_entity_type"
	ChangeRemovedEntityType  ChangeKind = "removed_entity_type"
	ChangeChangedEntityType  ChangeKind = "changed_entity_type"
)

// Change represents a single structural difference between the database
// schema and the file schema.
type Change struct {
	Kind      ChangeKind
	Component string // lowercase component name
	Property  string // lowercase property name (for property-level changes)
	OldType   string // old SQL type (for type changes)
	NewType   string // new SQL type (for type changes)
	ETName    string // entity type name (for entity-type changes)
	// Reason says why a change is what it is, for the cases where the kind
	// alone is misleading. A component whose shape changed comes out as
	// remove+add, which is indistinguishable from a component somebody
	// deleted — and the person being asked to confirm the drop is entitled to
	// know that their data cannot come across rather than that they appear to
	// have deleted something they did not.
	Reason string
	// OldRef and NewRef carry the foreign key for ChangeChangedConstraint, in
	// the form DomainColumn.References uses. Not OldType/NewType: a constraint
	// change leaves the SQL type alone, so those would both say INTEGER and
	// describe the change as nothing.
	OldRef string
	NewRef string
	// OldNullable and NewNullable carry whether the column accepts NULL, for
	// ChangeChangedNullability. Both are booleans whose false is meaningful, so
	// unlike the type fields they are always compared.
	OldNullable bool
	NewNullable bool
	// OldName is what a renamed thing was called in the database. The new name
	// is in Component, Property or ETName, because that is what every other
	// change on it uses.
	OldName string
	OldET   *EntityType // previous entity type spec (for changed_entity_type)
	NewET   *EntityType // new entity type spec (for changed_entity_type)
}

// phase returns a numeric priority used for deterministic ordering.
// Additions come first (1), modifications second (2), removals last (3).
func (c Change) phase() int {
	switch c.Kind {
	case ChangeRenamedComponent, ChangeRenamedProperty, ChangeRenamedEntityType:
		// Ahead of everything. A component renamed and given a property has to
		// be renamed before the ALTER TABLE that adds the property names it.
		return 0
	case ChangeAddedComponent, ChangeAddedProperty, ChangeAddedEntityType:
		return 1
	case ChangedPropertyType, ChangeChangedConstraint, ChangeChangedNullability, ChangeChangedEntityType:
		return 2
	case ChangeRemovedComponent, ChangeRemovedProperty, ChangeRemovedEntityType:
		return 3
	}
	return 99 // safety net
}

// sortKey returns a comparable tuple for ordering changes within a phase.
func (c Change) sortKey() string {
	primary := c.Component
	if primary == "" && c.ETName != "" {
		primary = c.ETName
	}
	return primary + "\x00" + c.Property + "\x00" + string(c.Kind)
}

// Diff computes the structural differences between the as-built database
// schema and the current file schema. Returns an empty (non-nil) slice for
// identical schemas. Changes are ordered: additions → modifications →
// removals, with alphabetical sorting within each phase.
//
// Entity type changes require both the current file schema and the previous
// file schema, since entity type spec details are not stored in the database.
// Pass nil for oldFile to skip ChangedEntityType detection (only Adds/Removes
// for entity types will be emitted, which is correct on initial bootstrap).
func Diff(domain *DomainSchema, file, oldFile *DatabaseSchema) []Change {
	if domain == nil {
		domain = &DomainSchema{
			Components:      make(map[string]DomainComponent),
			EntityTypeNames: make(map[string]bool),
		}
	}
	if file == nil {
		file = &DatabaseSchema{
			Components:  make(map[string]Component),
			EntityTypes: make(map[string]EntityType),
		}
	}

	// Renames first, and applied rather than merely recorded: everything below
	// then compares the database as it will be once the renames have run, and
	// needs to know nothing about them. Suppressing the add/remove pairs
	// afterwards would instead leave every other comparison — types, foreign
	// keys, nullability — looking at a column it believed had been dropped.
	domain, changes, skippedRenames := applyRenames(domain, file)

	// ── Component diff ───────────────────────────────────────────────
	// Build lowercase key sets.
	dbCompNames := make([]string, 0, len(domain.Components))
	for k := range domain.Components {
		dbCompNames = append(dbCompNames, strings.ToLower(k))
	}
	fileCompNames := make([]string, 0, len(file.Components))
	for k := range file.Components {
		fileCompNames = append(fileCompNames, strings.ToLower(k))
	}

	dbCompSet := make(map[string]bool, len(dbCompNames))
	for _, n := range dbCompNames {
		dbCompSet[n] = true
	}
	fileCompSet := make(map[string]bool, len(fileCompNames))
	for _, n := range fileCompNames {
		fileCompSet[n] = true
	}

	// Components in file but not in DB → added.
	for _, name := range fileCompNames {
		if !dbCompSet[name] {
			changes = append(changes, Change{
				Kind:      ChangeAddedComponent,
				Component: name,
			})
		}
	}

	// Components in DB but not in file → removed.
	for _, name := range dbCompNames {
		if !fileCompSet[name] {
			changes = append(changes, Change{
				Kind:      ChangeRemovedComponent,
				Component: name,
				Reason:    droppedTableReason(name, dbCompSet, fileCompNames, skippedRenames),
			})
		}
	}

	// Components present in both → structural comparison.
	for _, name := range dbCompNames {
		if !fileCompSet[name] {
			continue
		}
		dbComp := domain.Components[name]

		// Find the corresponding file component by lowercase name.
		var fileComp Component
		for fk, fc := range file.Components {
			if strings.ToLower(fk) == name {
				fileComp = fc
				break
			}
		}

		// Can the table that is there become the table the file wants?
		//
		// Not "is the db the same type as the file". The db has no record of a
		// component's declared type — storage.InferComponentType guesses it
		// from the columns, and an object component whose one property happens
		// to be called "value" builds a table indistinguishable from a string
		// component's. Comparing the guess against the file dropped that
		// table, with its data, on any version bump. It is the shape Forge
		// gives every new object component.
		//
		// So the question is about columns, which the db does record.
		fileLayout := StorageLayout(fileComp.Type)

		if !canBecome(dbComp.Columns, fileLayout) {
			// No column can be altered into the other shape's column, so the
			// table goes and a new one is built. Remove is destructive, which
			// is what lets MigrationConfirm refuse it: the data cannot come
			// across, and that is a thing to be told rather than to discover.
			reason := fmt.Sprintf("its shape changed to %q, and no column can be altered into that", fileLayout)
			changes = append(changes, Change{
				Kind:      ChangeRemovedComponent,
				Component: name,
				Reason:    reason,
			})
			changes = append(changes, Change{
				Kind:      ChangeAddedComponent,
				Component: name,
				Reason:    reason,
			})
			continue
		}

		if fileLayout == LayoutColumns {
			diffObjectProperties(name, dbComp.Columns, fileComp.Properties, &changes)
		} else {
			diffScalarComponent(name, dbComp.Columns, fileComp, &changes)
		}
		diffReferences(name, dbComp.Columns, fileComp, &changes)
		diffNullability(name, dbComp.Columns, fileComp, &changes)
	}

	// ── Entity type diff (names against DB) ──────────────────────────
	fileETNames := make([]string, 0, len(file.EntityTypes))
	for k := range file.EntityTypes {
		fileETNames = append(fileETNames, k)
	}

	for _, fk := range fileETNames {
		if !domain.EntityTypeNames[fk] {
			et := file.EntityTypes[fk]
			changes = append(changes, Change{
				Kind:   ChangeAddedEntityType,
				ETName: fk,
				NewET:  &et,
			})
		}
	}

	for dk := range domain.EntityTypeNames {
		if _, ok := file.EntityTypes[dk]; !ok {
			changes = append(changes, Change{
				Kind:   ChangeRemovedEntityType,
				ETName: dk,
			})
		}
	}

	// ── Entity type diff (specs, requires oldFile) ───────────────────
	if oldFile != nil {
		for name, oldET := range oldFile.EntityTypes {
			newET, ok := file.EntityTypes[name]
			if !ok {
				continue // already handled as removed above
			}
			if !entityTypeDeepEqual(oldET, newET) {
				oldCopy := oldET
				newCopy := newET
				changes = append(changes, Change{
					Kind:   ChangeChangedEntityType,
					ETName: name,
					OldET:  &oldCopy,
					NewET:  &newCopy,
				})
			}
		}
	}

	// ── Sort for determinism ─────────────────────────────────────────
	sort.Slice(changes, func(i, j int) bool {
		pi := changes[i].phase()
		pj := changes[j].phase()
		if pi != pj {
			return pi < pj
		}
		return changes[i].sortKey() < changes[j].sortKey()
	})

	return changes
}

// droppedTableReason is droppedColumnReason for a whole component: the table
// goes, and it might have been a rename.
func droppedTableReason(name string, dbSet map[string]bool, fileNames []string, skipped map[string]string) string {
	base := fmt.Sprintf("the table comp_%s is dropped, and every row in it goes with it", name)

	// The author declared a rename and it could not be carried out. Saying
	// nothing here would drop the table they asked to keep, silently.
	if to, was := skipped[name]; was {
		return base + fmt.Sprintf(
			"; it is declared as renamed to %q, which was not applied because comp_%s is already in the database",
			to, to)
	}

	var newNames []string
	for _, f := range fileNames {
		if !dbSet[f] {
			newNames = append(newNames, f)
		}
	}
	// Both sides counted, not just the added one. With two tables dropped and
	// one added, every drop would be told it might be the rename — two
	// contradictory suggestions, each stated as if it were the answer, and
	// acting on the wrong one moves the wrong rows.
	dropped := 0
	for d := range dbSet {
		if !slices.Contains(fileNames, d) {
			dropped++
		}
	}
	if len(newNames) != 1 || dropped != 1 {
		return base
	}
	return base + fmt.Sprintf(
		"; if this is a rename to %q, say so with \"renamedFrom\": %q and the table is moved instead",
		newNames[0], name)
}

// applyRenames rewrites the introspected schema into the shape the file's
// renamedFrom declarations describe, and returns the renames it applied.
//
// A rename cannot be inferred: "x became col_x" and "x was deleted and col_x
// added" are the same diff, and guessing wrong copies data into a column the
// author did not mean. So the author says it, and this is where what they said
// is taken at face value.
//
// A rename is only applied when the database has the old name and does not have
// the new one. Nothing happens when the migration has already run, so
// renamedFrom may stay in the file for good; and nothing happens when the
// database never had the old name, which comes out as the plain addition it is.
func applyRenames(domain *DomainSchema, file *DatabaseSchema) (*DomainSchema, []Change, map[string]string) {
	changes := make([]Change, 0)
	// Renames the file declares that could not be carried out, old name → new.
	// The author wrote them down, so the table going instead is a thing to say
	// rather than to do quietly.
	skipped := map[string]string{}
	out := &DomainSchema{
		SchemaVersion:   domain.SchemaVersion,
		Components:      make(map[string]DomainComponent, len(domain.Components)),
		EntityTypeNames: make(map[string]bool, len(domain.EntityTypeNames)),
	}
	for k, v := range domain.Components {
		out.Components[strings.ToLower(k)] = v
	}
	for k, v := range domain.EntityTypeNames {
		out.EntityTypeNames[k] = v
	}

	// Components, in a fixed order so two renames never depend on map order.
	for _, name := range sortedKeys(file.Components) {
		comp := file.Components[name]
		newName := strings.ToLower(name)
		oldName := strings.ToLower(comp.RenamedFrom)
		if oldName == "" || oldName == newName {
			continue
		}
		if _, hasOld := out.Components[oldName]; !hasOld {
			continue
		}
		if _, hasNew := out.Components[newName]; hasNew {
			// Both names are in the database. Renaming would overwrite a table
			// that is already there and delete the one being renamed, so it is
			// refused — and recorded, because the old table is about to be
			// dropped and the author asked for the opposite.
			skipped[oldName] = newName
			continue
		}
		out.Components[newName] = out.Components[oldName]
		delete(out.Components, oldName)
		changes = append(changes, Change{
			Kind:      ChangeRenamedComponent,
			Component: newName,
			OldName:   oldName,
			Reason:    fmt.Sprintf("it was called %q", oldName),
		})
	}

	// Properties, against whatever the component is called by now.
	for _, name := range sortedKeys(file.Components) {
		comp := file.Components[name]
		dbComp, ok := out.Components[strings.ToLower(name)]
		if !ok || StorageLayout(comp.Type) != LayoutColumns {
			continue
		}
		cols := make([]DomainColumn, len(dbComp.Columns))
		copy(cols, dbComp.Columns)
		var renamed bool
		for _, propName := range sortedKeys(comp.Properties) {
			prop := comp.Properties[propName]
			newCol := strings.ToLower(propName)
			oldCol := strings.ToLower(prop.RenamedFrom)
			if oldCol == "" || oldCol == newCol {
				continue
			}
			oldAt, newAt := -1, -1
			for i, c := range cols {
				switch strings.ToLower(c.Name) {
				case oldCol:
					oldAt = i
				case newCol:
					newAt = i
				}
			}
			if oldAt < 0 || newAt >= 0 {
				continue
			}
			cols[oldAt].Name = newCol
			renamed = true
			changes = append(changes, Change{
				Kind:      ChangeRenamedProperty,
				Component: strings.ToLower(name),
				Property:  newCol,
				OldName:   oldCol,
				Reason:    fmt.Sprintf("it was called %q", oldCol),
			})
		}
		if renamed {
			dbComp.Columns = cols
			out.Components[strings.ToLower(name)] = dbComp
		}
	}

	// Entity types, which are rows rather than columns.
	for _, name := range sortedKeys(file.EntityTypes) {
		et := file.EntityTypes[name]
		if et.RenamedFrom == "" || et.RenamedFrom == name {
			continue
		}
		// No "the new name already exists" guard, unlike the two above. A table
		// cannot be renamed onto another table, but this rename is an UPDATE
		// moving rows from one type string to another, which is safe whether or
		// not the destination already has some — and skipping it left entities
		// filed under a name the schema no longer declares, which is the whole
		// defect.
		if !out.EntityTypeNames[et.RenamedFrom] {
			continue
		}
		delete(out.EntityTypeNames, et.RenamedFrom)
		out.EntityTypeNames[name] = true
		changes = append(changes, Change{
			Kind:    ChangeRenamedEntityType,
			ETName:  name,
			OldName: et.RenamedFrom,
			Reason:  fmt.Sprintf("it was called %q", et.RenamedFrom),
		})
	}

	return out, changes, skipped
}

// sortedKeys is map iteration made deterministic.
//
// It matters because Diff does not get to assume the schema validated. A chain
// — B renamed from A, C renamed from B — is refused where the schema is loaded,
// but Forge previews a schema in the middle of being edited, and two renames
// applied in different orders reach different states. Sorted keys make the
// answer to an impossible schema at least the same answer every time.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// diffObjectProperties compares columns of an object component table against
// the file's property declarations.
func diffObjectProperties(compName string, dbCols []DomainColumn, fileProps map[string]Property, changes *[]Change) {
	// Build sets of column names (lowercase, excluding entity_id).
	dbColNames := make(map[string]string) // name → SQLType
	for _, c := range dbCols {
		if c.IsPK {
			continue // skip entity_id
		}
		dbColNames[strings.ToLower(c.Name)] = strings.ToUpper(c.SQLType)
	}

	filePropNames := make(map[string]string) // name → SQLType
	for name, prop := range fileProps {
		filePropNames[strings.ToLower(name)] = PropertySQLType(prop)
	}

	// Properties in file but not in DB → added.
	for fp := range filePropNames {
		if _, ok := dbColNames[fp]; !ok {
			*changes = append(*changes, Change{
				Kind:      ChangeAddedProperty,
				Component: compName,
				Property:  fp,
				NewType:   filePropNames[fp],
			})
		}
	}

	// Properties in DB but not in file → removed.
	var dropped, added []string
	for dp := range dbColNames {
		if _, ok := filePropNames[dp]; !ok {
			dropped = append(dropped, dp)
		}
	}
	for fp := range filePropNames {
		if _, ok := dbColNames[fp]; !ok {
			added = append(added, fp)
		}
	}
	// Deliberately unsorted. These two used to be, for determinism, and neither
	// sort could affect anything: added is only read when it holds exactly one
	// name, and Diff re-sorts every change by phase and sortKey before
	// returning, so the order they are emitted in is not the order anybody sees.
	for _, dp := range dropped {
		*changes = append(*changes, Change{
			Kind:      ChangeRemovedProperty,
			Component: compName,
			Property:  dp,
			OldType:   dbColNames[dp],
			Reason:    droppedColumnReason(dp, dbColNames[dp], dropped, added, filePropNames),
		})
	}

	// Properties in both: compare SQL types.
	for dp, dbType := range dbColNames {
		if ft, ok := filePropNames[dp]; ok && ft != dbType {
			*changes = append(*changes, Change{
				Kind:      ChangedPropertyType,
				Component: compName,
				Property:  dp,
				OldType:   dbType,
				NewType:   ft,
			})
		}
	}
}

// droppedColumnReason says what a dropped column takes with it, and offers the
// rename it might have been.
//
// A rename cannot be inferred — "x became col_x" and "x was deleted and col_x
// added" are the same diff — so this does not act on the guess, it says it. Only
// when the guess is unambiguous: exactly one column dropped and one added, of
// the same SQL type. Any more than that and there is no way to say which went
// with which, and a wrong suggestion is worse than none.
//
// This is the only notice anybody gets. The migration runs under MigrationAuto
// by default, which drops the column and reports success.
func droppedColumnReason(column, sqlType string, dropped, added []string, fileTypes map[string]string) string {
	base := fmt.Sprintf("the column %q is dropped, and the data in it goes with it", column)
	// Both sides counted. The comment above said "exactly one column dropped
	// and one added" and the code only checked the added one, so dropping two
	// columns and adding one told each of them it might be the rename.
	if len(added) != 1 || len(dropped) != 1 {
		return base
	}
	newName := added[0]
	if fileTypes[newName] != sqlType {
		return base
	}
	return base + fmt.Sprintf(
		"; if this is a rename to %q, say so with \"renamedFrom\": %q and the column is moved instead",
		newName, column)
}

// diffScalarComponent compares the SQL type of a scalar component's value column.
func diffScalarComponent(compName string, dbCols []DomainColumn, fileComp Component, changes *[]Change) {
	fileSQLType := propertySQLTypeForComponent(fileComp.Type)

	for _, c := range dbCols {
		if c.IsPK {
			continue
		}
		// One data column, whose name is the layout's — "value" for the scalar
		// types and "target_entity_id" for an entity-ref. Which one it is does
		// not matter here, because the caller only reaches this when both sides
		// have the *same* layout, so the column is the same column and only its
		// type can have changed.
		if strings.ToUpper(c.SQLType) != fileSQLType {
			*changes = append(*changes, Change{
				Kind:      ChangedPropertyType,
				Component: compName,
				Property:  "value",
				OldType:   strings.ToUpper(c.SQLType),
				NewType:   fileSQLType,
			})
		}
		return
	}
}

// diffReferences compares the foreign key on each column against the one the
// generator would emit for it.
//
// This is the half of a table's shape introspection used not to read at all.
// Story 9 changed what the generator emits without changing any schema file, so
// every database built before it kept a reference that refuses where the file
// now says cascade, and — for a reference declared as an object property — no
// foreign key whatsoever. Neither is expressible as a property type change, and
// a diff that cannot state a difference cannot ask for it to be repaired.
//
// Only columns the file declares are compared. One it does not is a removed
// property, which diffObjectProperties has already said, and saying it twice
// would mean two rebuilds of one table.
func diffReferences(compName string, dbCols []DomainColumn, fileComp Component, changes *[]Change) {
	want := ColumnReferences(fileComp)
	for _, c := range dbCols {
		name := strings.ToLower(c.Name)
		wantRef, declared := want[name]
		if !declared || c.References == wantRef {
			continue
		}
		*changes = append(*changes, Change{
			Kind:      ChangeChangedConstraint,
			Component: compName,
			Property:  name,
			OldRef:    c.References,
			NewRef:    wantRef,
			Reason:    referenceReason(name, c.References, wantRef),
		})
	}
}

// diffNullability compares whether each column accepts NULL against whether the
// generator would let it.
//
// The other half of the shape introspection used not to read. It matters in both
// directions and for different reasons: a column that should refuse NULLs and
// does not is a table the file does not describe, and a column that should
// accept them and does not is the wedge Story 9 found — a rebuild of it fails on
// the rows an ALTER left empty, and the store returns that failure from every
// subsequent open.
//
// The primary key is skipped. SQLite reports every INTEGER PRIMARY KEY as
// nullable, so comparing it would report every table in every database.
func diffNullability(compName string, dbCols []DomainColumn, fileComp Component, changes *[]Change) {
	want := ColumnNullable(fileComp)
	for _, c := range dbCols {
		if c.IsPK {
			continue
		}
		name := strings.ToLower(c.Name)
		wantNullable, declared := want[name]
		if !declared || c.Nullable == wantNullable {
			continue
		}
		*changes = append(*changes, Change{
			Kind:        ChangeChangedNullability,
			Component:   compName,
			Property:    name,
			OldNullable: c.Nullable,
			NewNullable: wantNullable,
			Reason:      nullabilityReason(name, wantNullable),
		})
	}
}

// nullabilityReason says what the change is for. The two directions are not
// variations on one sentence: one is about what the table will refuse from now
// on, the other is about rows that already exist.
func nullabilityReason(column string, wantNullable bool) string {
	if wantNullable {
		return fmt.Sprintf("%s has to accept NULL, because it is a reference a row may not have yet", column)
	}
	return fmt.Sprintf("%s stops accepting NULL, and rows that have none take the value adding the column would have given them", column)
}

// referenceReason says what the change means, because the constraint text on
// its own does not. Somebody reading a confirmation dialog is deciding whether
// to let a table be rebuilt, and "owner gains a foreign key it never had" is
// the sentence that answers it.
//
// The column is named because a rebuild carries the reasons of every change
// that wanted it, deduplicated — so a sentence with no column in it would
// collapse three columns gaining a foreign key into one line saying nothing
// about which, or how many.
func referenceReason(column, old, want string) string {
	switch {
	case old == NoReference:
		return fmt.Sprintf("%s gains a foreign key it never had, so a reference to a deleted entity cannot be left behind", column)
	case want == NoReference:
		// Deliberately not "it is no longer a reference to an entity". The old
		// key can point anywhere — a hand-edited table can reference any table
		// — and describing a foreign key to something else as an entity
		// reference that ended is a sentence about a thing that never was.
		return fmt.Sprintf("%s loses its foreign key to %s", column, old)
	default:
		return fmt.Sprintf("%s changes its foreign key from %q to %q", column, old, want)
	}
}

// propertySQLTypeForComponent returns the SQL type for a scalar component type.
// Only valid for non-object component types.
func propertySQLTypeForComponent(compType string) string {
	switch compType {
	case ComponentTypeString:
		return "TEXT"
	case ComponentTypeInteger:
		return "INTEGER"
	case ComponentTypeNumber:
		return "REAL"
	case ComponentTypeBoolean:
		return "INTEGER"
	case ComponentTypeEntityRef:
		return "INTEGER"
	case ComponentTypeArray:
		return "TEXT"
	default:
		return "TEXT"
	}
}

// entityTypeDeepEqual compares two entity types for equality.
// RequiredComponents and OptionalComponents are compared as sets (order-insensitive).
func entityTypeDeepEqual(a, b EntityType) bool {
	return a.AllowExtraComponents == b.AllowExtraComponents &&
		a.ValidationLevel == b.ValidationLevel &&
		equalStringSliceSets(a.RequiredComponents, b.RequiredComponents) &&
		equalStringSliceSets(a.OptionalComponents, b.OptionalComponents)
}

// equalStringSliceSets compares two string slices as unordered sets.
func equalStringSliceSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aSorted := make([]string, len(a))
	bSorted := make([]string, len(b))
	copy(aSorted, a)
	copy(bSorted, b)
	sort.Strings(aSorted)
	sort.Strings(bSorted)
	for i := range aSorted {
		if aSorted[i] != bSorted[i] {
			return false
		}
	}
	return true
}

// ComponentByName looks up a Component from a DatabaseSchema by its
// lowercase name. Returns the component, the canonical (original-case)
// name, and whether it was found.
func ComponentByName(db *DatabaseSchema, name string) (Component, string) {
	lower := strings.ToLower(name)
	for k, c := range db.Components {
		if strings.ToLower(k) == lower {
			return c, k
		}
	}
	return Component{}, ""
}

// PropertyByName looks up a Property by lowercase name from a map.
func PropertyByName(props map[string]Property, name string) (Property, bool) {
	lower := strings.ToLower(name)
	for k, p := range props {
		if strings.ToLower(k) == lower {
			return p, true
		}
	}
	return Property{}, false
}

// canBecome reports whether the columns a table already has can be altered into
// the ones a component of this layout needs.
//
// An object's columns can: properties are added, dropped and retyped one at a
// time, and the generator orders additions before rebuilds, so by the time a
// rebuild copies the surviving columns they all exist in the old table. Whatever
// is there now, an object can be reached from it.
//
// The other two layouts have one fixed column each, and no ALTER renames a
// column into it. A table holding "value" cannot become one holding
// "target_entity_id": the data would have to be an entity id and it is a string.
// That is a table to drop and rebuild, and the drop is destructive so the
// confirm policy can refuse it.
func canBecome(dbCols []DomainColumn, fileLayout string) bool {
	if fileLayout == LayoutColumns {
		return true
	}
	want := LayoutColumnName(fileLayout)
	data := make([]DomainColumn, 0, len(dbCols))
	for _, c := range dbCols {
		if c.IsPK && c.Name == "entity_id" {
			continue
		}
		data = append(data, c)
	}
	return len(data) == 1 && data[0].Name == want
}
