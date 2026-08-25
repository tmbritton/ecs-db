package schema

import "strings"

// ColumnNullable is whether each column of this component's table may hold NULL,
// keyed by lowercase column name.
//
// One entry is true: an entity-ref *property*. A reference is the whole of a
// component whose type is entity-ref and only part of an object that has one, so
// "no owner yet" is a state such an object can legitimately be in — and it is
// the state every existing row is in the moment somebody adds the property,
// because ALTER TABLE ADD COLUMN has nothing to backfill with. See
// storage.columnConstraint for the database that declaring it NOT NULL wedged.
//
// The primary key is absent from the map rather than false. SQLite reports every
// INTEGER PRIMARY KEY as nullable — it is a rowid alias and assigns itself — so
// the flag says nothing there, and a comparison including it would have to carry
// that quirk around as an expectation. It cannot hold NULL in any case.
func ColumnNullable(comp Component) map[string]bool {
	nullable := map[string]bool{}

	layout := StorageLayout(comp.Type)
	if layout == LayoutColumns {
		for name, prop := range comp.Properties {
			nullable[strings.ToLower(name)] = PropertyNullable(prop.Type)
		}
		return nullable
	}
	// The single data column of a non-object component is the whole row, so it
	// may not be missing — including, and especially, when it is a reference.
	// PropertyNullable is deliberately not consulted: it would say an entity-ref
	// is nullable, and that is the difference between the two forms.
	nullable[LayoutColumnName(layout)] = false
	return nullable
}

// PropertyNullable is whether a column holding a value of this property type may
// be NULL.
//
// The single statement of the rule. It was written out independently in five
// places — this one, the CREATE TABLE builder, the ALTER TABLE builder, the
// rebuild's column list, and the diff — so the DDL the generator emitted and the
// nullability the diff expected could disagree without anything noticing. They
// all read this now, on the same terms as EntityReference and StorageLayout.
func PropertyNullable(propertyType string) bool {
	return propertyType == PropertyTypeEntityRef
}
