package schema

import "testing"

// The primary key is absent rather than false: SQLite reports every INTEGER
// PRIMARY KEY as nullable, so the flag says nothing there and the column is left
// out of the comparison instead of having the quirk written into an expectation.
func TestColumnNullable_ThePrimaryKeyIsNotCompared(t *testing.T) {
	for _, ct := range ComponentTypes() {
		comp := Component{Type: ct}
		if ct == ComponentTypeArray {
			comp.Items = &Property{Type: PropertyTypeString}
		}
		if _, present := ColumnNullable(comp)["entity_id"]; present {
			t.Errorf("%s component: entity_id is in the nullability map", ct)
		}
	}
}

// An entity-ref property is the only column the generator declares nullable, and
// the reason it is nullable is in storage.columnConstraint.
func TestColumnNullable_OnlyAReferencePropertyMayBeNull(t *testing.T) {
	props := map[string]Property{}
	for _, pt := range PropertyTypes() {
		p := Property{Type: pt}
		if pt == PropertyTypeArray {
			p.Items = &Property{Type: PropertyTypeString}
		}
		props["p_"+pt] = p
	}
	got := ColumnNullable(Component{Type: ComponentTypeObject, Properties: props})

	for _, pt := range PropertyTypes() {
		name := "p_" + pt
		want := pt == PropertyTypeEntityRef
		if got[name] != want {
			t.Errorf("a %s property is nullable=%v, want %v", pt, got[name], want)
		}
	}
}

// A component that *is* a reference is not the same as a property that is one:
// the reference is the whole row, so it may not be missing.
func TestColumnNullable_TheDataColumnOfEachShape(t *testing.T) {
	cases := []struct {
		comp   Component
		column string
	}{
		{Component{Type: ComponentTypeEntityRef}, "target_entity_id"},
		{Component{Type: ComponentTypeString}, "value"},
		{Component{Type: ComponentTypeInteger}, "value"},
		{Component{Type: ComponentTypeBoolean}, "value"},
		{Component{Type: ComponentTypeNumber}, "value"},
		{Component{Type: ComponentTypeArray, Items: &Property{Type: PropertyTypeString}}, "value"},
	}
	for _, tc := range cases {
		got := ColumnNullable(tc.comp)
		if _, present := got[tc.column]; !present {
			t.Errorf("%s component: %s is not in the map at all: %v", tc.comp.Type, tc.column, got)
			continue
		}
		if got[tc.column] {
			t.Errorf("%s component: %s is nullable, want NOT NULL", tc.comp.Type, tc.column)
		}
	}
}
