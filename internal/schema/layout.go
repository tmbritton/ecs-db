package schema

// The three shapes a component's table can have. A component's *type* decides
// which, and it is the thing a diff has to compare rather than the type itself:
// "string" and "integer" are different types and the same shape, so one can
// become the other by changing a column. "string" and "entity-ref" are the same
// kind of thing to a reader — neither is an object — and different shapes, so
// one cannot become the other at all.
const (
	// LayoutColumns is one column per declared property, which is what an
	// object component gets.
	LayoutColumns = "columns"
	// LayoutValue is a single column named "value".
	LayoutValue = "value"
	// LayoutTargetEntityID is a single column named "target_entity_id".
	LayoutTargetEntityID = "target_entity_id"
)

// StorageLayout is the shape of the table a component of this type is built
// with.
//
// It exists because the diff has to predict what the generator will build, and
// used to predict it with `type == object`, which put entity-ref in with the
// scalars. A component changing between entity-ref and any other scalar type
// was then reported as a changed *property*, the generator answered that with a
// table rebuild, and the rebuild's copy read a column that did not exist —
// leaving a database that failed to open until somebody edited schema.json back.
//
// An unknown type has no real answer: ValidateSchema refuses one, and the
// generator refuses it again with "unsupported component type". LayoutValue is
// returned because it is the answer that changes nothing — the alternative
// would drop a table to make room for a component that cannot be built, and
// then fail on the building.
func StorageLayout(componentType string) string {
	switch componentType {
	case ComponentTypeObject:
		return LayoutColumns
	case ComponentTypeEntityRef:
		return LayoutTargetEntityID
	default:
		return LayoutValue
	}
}

// LayoutColumnName is the single data column a layout has, and "" for the
// object layout, which has as many as the component declares.
func LayoutColumnName(layout string) string {
	switch layout {
	case LayoutValue:
		return "value"
	case LayoutTargetEntityID:
		return "target_entity_id"
	default:
		return ""
	}
}
