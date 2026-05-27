package schema

import (
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
}

// ChangeKind identifies the category of a schema change.
type ChangeKind string

const (
	ChangeAddedComponent    ChangeKind = "added_component"
	ChangeRemovedComponent  ChangeKind = "removed_component"
	ChangeAddedProperty     ChangeKind = "added_property"
	ChangeRemovedProperty   ChangeKind = "removed_property"
	ChangedPropertyType     ChangeKind = "changed_property_type"
	ChangeAddedEntityType   ChangeKind = "added_entity_type"
	ChangeRemovedEntityType ChangeKind = "removed_entity_type"
	ChangeChangedEntityType ChangeKind = "changed_entity_type"
)

// Change represents a single structural difference between the database
// schema and the file schema.
type Change struct {
	Kind      ChangeKind
	Component string      // lowercase component name
	Property  string      // lowercase property name (for property-level changes)
	OldType   string      // old SQL type (for type changes)
	NewType   string      // new SQL type (for type changes)
	ETName    string      // entity type name (for entity-type changes)
	OldET     *EntityType // previous entity type spec (for changed_entity_type)
	NewET     *EntityType // new entity type spec (for changed_entity_type)
}

// phase returns a numeric priority used for deterministic ordering.
// Additions come first (1), modifications second (2), removals last (3).
func (c Change) phase() int {
	switch c.Kind {
	case ChangeAddedComponent, ChangeAddedProperty, ChangeAddedEntityType:
		return 1
	case ChangedPropertyType, ChangeChangedEntityType:
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

	changes := make([]Change, 0)

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

		dbIsObject := dbComp.Type == "object"
		fileIsObject := fileComp.Type == ComponentTypeObject

		if dbIsObject != fileIsObject {
			// Structural incompatibility → treat as remove + add.
			changes = append(changes, Change{
				Kind:      ChangeRemovedComponent,
				Component: name,
			})
			changes = append(changes, Change{
				Kind:      ChangeAddedComponent,
				Component: name,
			})
			continue
		}

		if dbIsObject && fileIsObject {
			diffObjectProperties(name, dbComp.Columns, fileComp.Properties, &changes)
		} else {
			diffScalarComponent(name, dbComp.Columns, fileComp, &changes)
		}
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
	for dp := range dbColNames {
		if _, ok := filePropNames[dp]; !ok {
			*changes = append(*changes, Change{
				Kind:      ChangeRemovedProperty,
				Component: compName,
				Property:  dp,
				OldType:   dbColNames[dp],
			})
		}
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

// diffScalarComponent compares the SQL type of a scalar component's value column.
func diffScalarComponent(compName string, dbCols []DomainColumn, fileComp Component, changes *[]Change) {
	fileSQLType := propertySQLTypeForComponent(fileComp.Type)

	for _, c := range dbCols {
		if c.IsPK {
			continue
		}
		// For scalar components there's exactly one data column (named "value").
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
