package schema

import "strings"

// EntityReference is the foreign key every reference to an entity carries.
//
// Written in the form PRAGMA foreign_key_list reports it — "<table>(<column>)
// ON DELETE <action>" — because comparing what a database has against what the
// generator would emit is the only thing this constant is for, and the
// comparison happens in that form. storage builds its DDL fragment from it.
//
// See storage.entityRefReference for what ON DELETE CASCADE means here and why
// it is not a refusal.
const EntityReference = "entities(id) ON DELETE CASCADE"

// NoReference is what a column that references nothing has.
//
// Named rather than written as "" at each site, because "no foreign key" is a
// real answer that has to be distinguished from a foreign key with no ON DELETE
// clause — which reads as "entities(id) ON DELETE NO ACTION", not as nothing.
const NoReference = ""

// ColumnReference is the foreign key the generator puts on a column holding a
// value of this property type.
func ColumnReference(propertyType string) string {
	if propertyType == PropertyTypeEntityRef {
		return EntityReference
	}
	return NoReference
}

// ColumnReferences is the foreign key on every column of this component's
// table, keyed by lowercase column name.
//
// The diff's side of the agreement with the generator: it says what a table
// built from this component would carry, so a table that carries something else
// can be recognised as needing rebuilding. Both sides are exercised against a
// real database in storage's TestReferences_TheDiffExpectsWhatTheGeneratorEmits.
func ColumnReferences(comp Component) map[string]string {
	// entity_id first, and on every table whatever its shape — the reference
	// this engine has always had, and the one the property diff skips because
	// it is the primary key.
	refs := map[string]string{"entity_id": EntityReference}

	layout := StorageLayout(comp.Type)
	if layout == LayoutColumns {
		for name, prop := range comp.Properties {
			refs[strings.ToLower(name)] = ColumnReference(prop.Type)
		}
		return refs
	}
	// One data column, holding a value of the component's own type — component
	// and property types are one vocabulary, so ColumnReference answers for
	// both. LayoutColumnName gives the column its name.
	refs[LayoutColumnName(layout)] = ColumnReference(comp.Type)
	return refs
}
