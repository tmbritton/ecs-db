package schema

import "testing"

func nullableCol(name, sqlType string, nullable bool) DomainColumn {
	return DomainColumn{Name: name, SQLType: sqlType, Nullable: nullable}
}

// A column that ought to refuse NULLs and does not.
func TestDiff_NullabilityThatDriftedIsAChange(t *testing.T) {
	cases := []struct {
		name   string
		dbCols []DomainColumn
		comp   Component
		want   []Change
	}{
		{
			name: "a property that should not be nullable",
			dbCols: []DomainColumn{
				entityIDCol(),
				nullableCol("hp", "INTEGER", true),
			},
			comp: Component{Type: ComponentTypeObject, Properties: map[string]Property{
				"hp": {Type: PropertyTypeInteger},
			}},
			want: []Change{{
				Kind: ChangeChangedNullability, Component: "holder", Property: "hp",
				OldNullable: true, NewNullable: false,
			}},
		},
		{
			name: "a reference property that is not nullable, which wedges a later rebuild",
			dbCols: []DomainColumn{
				entityIDCol(),
				{Name: "owner", SQLType: "INTEGER", References: EntityReference},
			},
			comp: Component{Type: ComponentTypeObject, Properties: map[string]Property{
				"owner": {Type: PropertyTypeEntityRef},
			}},
			want: []Change{{
				Kind: ChangeChangedNullability, Component: "holder", Property: "owner",
				OldNullable: false, NewNullable: true,
			}},
		},
		{
			name: "a scalar's value column",
			dbCols: []DomainColumn{
				entityIDCol(),
				nullableCol("value", "TEXT", true),
			},
			comp: Component{Type: ComponentTypeString},
			want: []Change{{
				Kind: ChangeChangedNullability, Component: "holder", Property: "value",
				OldNullable: true, NewNullable: false,
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			domain := &DomainSchema{
				Components:      map[string]DomainComponent{"holder": {Columns: tc.dbCols}},
				EntityTypeNames: map[string]bool{},
			}
			file := &DatabaseSchema{
				Components:  map[string]Component{"Holder": tc.comp},
				EntityTypes: map[string]EntityType{},
			}
			assertChanges(t, Diff(domain, file, nil), tc.want)
		})
	}
}

// The primary key is never reported, whatever SQLite says about it.
func TestDiff_ThePrimaryKeysNullabilityIsNeverReported(t *testing.T) {
	for _, pkNullable := range []bool{true, false} {
		domain := &DomainSchema{
			Components: map[string]DomainComponent{"holder": {Columns: []DomainColumn{
				{
					Name: "entity_id", SQLType: "INTEGER", IsPK: true,
					References: EntityReference, Nullable: pkNullable,
				},
				nullableCol("value", "TEXT", false),
			}}},
			EntityTypeNames: map[string]bool{},
		}
		file := &DatabaseSchema{
			Components:  map[string]Component{"Holder": {Type: ComponentTypeString}},
			EntityTypes: map[string]EntityType{},
		}
		if got := Diff(domain, file, nil); len(got) != 0 {
			t.Errorf("entity_id nullable=%v produced %+v", pkNullable, got)
		}
	}
}
