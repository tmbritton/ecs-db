package schema

import (
	"sort"
	"testing"
)

// Every property type has to have an answer, and the enumeration is what makes
// that true of a type added later. A hardcoded list would let an eighth type
// pass silently — which is the mistake this test's counterpart in storage was
// written twice to avoid.
func TestColumnReference_EveryPropertyTypeIsAnswered(t *testing.T) {
	for _, pt := range PropertyTypes() {
		got := ColumnReference(pt)
		want := NoReference
		if pt == PropertyTypeEntityRef {
			want = EntityReference
		}
		if got != want {
			t.Errorf("ColumnReference(%q) = %q, want %q", pt, got, want)
		}
	}
}

func TestPropertyTypes_IsEveryDeclaredType(t *testing.T) {
	got := PropertyTypes()
	if len(got) != len(supportedPropertyTypes) {
		t.Fatalf("PropertyTypes() = %v, but %d types are supported", got, len(supportedPropertyTypes))
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("PropertyTypes() = %v, not sorted", got)
	}
	for _, pt := range got {
		if !supportedPropertyTypes[pt] {
			t.Errorf("PropertyTypes() names %q, which is not a supported type", pt)
		}
	}
}

// entity_id is a reference like any other, on every table whatever its shape.
func TestColumnReferences_EntityIDAlwaysCascades(t *testing.T) {
	for _, ct := range ComponentTypes() {
		comp := Component{Type: ct}
		if ct == ComponentTypeArray {
			comp.Items = &Property{Type: PropertyTypeString}
		}
		refs := ColumnReferences(comp)
		if refs["entity_id"] != EntityReference {
			t.Errorf("%s component: entity_id references %q, want %q",
				ct, refs["entity_id"], EntityReference)
		}
	}
}

func TestColumnReferences_TheDataColumnOfEachShape(t *testing.T) {
	cases := []struct {
		name string
		comp Component
		want map[string]string
	}{
		{
			name: "a component that is a reference",
			comp: Component{Type: ComponentTypeEntityRef},
			want: map[string]string{"entity_id": EntityReference, "target_entity_id": EntityReference},
		},
		{
			name: "a scalar references nothing",
			comp: Component{Type: ComponentTypeString},
			want: map[string]string{"entity_id": EntityReference, "value": NoReference},
		},
		{
			name: "an object references through the properties that are references",
			comp: Component{Type: ComponentTypeObject, Properties: map[string]Property{
				"Owner": {Type: PropertyTypeEntityRef},
				"hp":    {Type: PropertyTypeInteger},
			}},
			want: map[string]string{
				"entity_id": EntityReference,
				"owner":     EntityReference,
				"hp":        NoReference,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ColumnReferences(tc.comp)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for col, want := range tc.want {
				if got[col] != want {
					t.Errorf("%s references %q, want %q", col, got[col], want)
				}
			}
		})
	}
}
