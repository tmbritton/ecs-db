package schema

import "testing"

// col is a column as introspection reports it. Nullability is derived from the
// reference rather than passed: an entity-ref property is the one column the
// generator declares nullable, so a fixture that said otherwise would describe a
// table the generator could not have built — and would now be reported as a
// nullability change on top of whatever the case was about.
func col(name, sqlType, ref string, pk bool) DomainColumn {
	return DomainColumn{
		Name: name, SQLType: sqlType, References: ref, IsPK: pk,
		Nullable: ref == EntityReference && name != "entity_id" && name != "target_entity_id",
	}
}

func entityIDCol() DomainColumn { return col("entity_id", "INTEGER", EntityReference, true) }

// The two forms Story 9 changed, both invisible to the diff until now.
func TestDiff_AForeignKeyThatChangedIsAChange(t *testing.T) {
	cases := []struct {
		name   string
		dbCols []DomainColumn
		comp   Component
		want   []Change
	}{
		{
			name: "a component that is a reference, built before the cascade",
			dbCols: []DomainColumn{
				entityIDCol(),
				col("target_entity_id", "INTEGER", "entities(id) ON DELETE NO ACTION", false),
			},
			comp: Component{Type: ComponentTypeEntityRef},
			want: []Change{{
				Kind: ChangeChangedConstraint, Component: "carrier", Property: "target_entity_id",
				OldRef: "entities(id) ON DELETE NO ACTION", NewRef: EntityReference,
			}},
		},
		{
			name: "a reference property, built with no foreign key at all",
			dbCols: []DomainColumn{
				entityIDCol(),
				col("owner", "INTEGER", NoReference, false),
				col("hp", "INTEGER", NoReference, false),
			},
			comp: Component{Type: ComponentTypeObject, Properties: map[string]Property{
				"owner": {Type: PropertyTypeEntityRef},
				"hp":    {Type: PropertyTypeInteger},
			}},
			// Two changes, because a reference property built before Story 9
			// differs in two ways: it has no foreign key, and it refuses the
			// NULL that a row without an owner needs. Reporting only the first
			// would rebuild the table into one that still wedges.
			want: []Change{
				{
					Kind: ChangeChangedConstraint, Component: "carrier", Property: "owner",
					OldRef: NoReference, NewRef: EntityReference,
				},
				{
					Kind: ChangeChangedNullability, Component: "carrier", Property: "owner",
					OldNullable: false, NewNullable: true,
				},
			},
		},
		{
			name: "a reference that stopped being one",
			dbCols: []DomainColumn{
				entityIDCol(),
				col("owner", "INTEGER", EntityReference, false),
			},
			comp: Component{Type: ComponentTypeObject, Properties: map[string]Property{
				"owner": {Type: PropertyTypeInteger},
			}},
			// Also two: it loses the foreign key, and it stops accepting the
			// NULLs it was allowed to hold while it was a reference. The second
			// is what used to make this edit unopenable.
			want: []Change{
				{
					Kind: ChangeChangedConstraint, Component: "carrier", Property: "owner",
					OldRef: EntityReference, NewRef: NoReference,
				},
				{
					Kind: ChangeChangedNullability, Component: "carrier", Property: "owner",
					OldNullable: true, NewNullable: false,
				},
			},
		},
		{
			// entity_id is a reference like any other, and the property diff
			// skips primary-key columns everywhere else.
			name: "entity_id itself",
			dbCols: []DomainColumn{
				col("entity_id", "INTEGER", "entities(id) ON DELETE NO ACTION", true),
				col("value", "TEXT", NoReference, false),
			},
			comp: Component{Type: ComponentTypeString},
			want: []Change{{
				Kind: ChangeChangedConstraint, Component: "carrier", Property: "entity_id",
				OldRef: "entities(id) ON DELETE NO ACTION", NewRef: EntityReference,
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			domain := &DomainSchema{
				Components:      map[string]DomainComponent{"carrier": {Columns: tc.dbCols}},
				EntityTypeNames: map[string]bool{},
			}
			file := &DatabaseSchema{
				Components:  map[string]Component{"Carrier": tc.comp},
				EntityTypes: map[string]EntityType{},
			}
			assertChanges(t, Diff(domain, file, nil), tc.want)
		})
	}
}

// The everyday case: a database that already matches must produce nothing, or
// every open would rebuild every table.
func TestDiff_AMatchingForeignKeyIsNotAChange(t *testing.T) {
	for _, ct := range ComponentTypes() {
		comp := Component{Type: ct}
		switch ct {
		case ComponentTypeArray:
			comp.Items = &Property{Type: PropertyTypeString}
		case ComponentTypeObject:
			comp.Properties = map[string]Property{
				"owner": {Type: PropertyTypeEntityRef},
				"hp":    {Type: PropertyTypeInteger},
			}
		}

		nullable := ColumnNullable(comp)
		var cols []DomainColumn
		for name, ref := range ColumnReferences(comp) {
			sqlType := "INTEGER"
			if name != "entity_id" && name != "owner" && name != "target_entity_id" {
				sqlType = sqlTypeOfDataColumn(comp, name)
			}
			cols = append(cols, DomainColumn{
				Name: name, SQLType: sqlType, References: ref,
				IsPK: name == "entity_id", Nullable: nullable[name],
			})
		}

		domain := &DomainSchema{
			Components:      map[string]DomainComponent{"c": {Columns: cols}},
			EntityTypeNames: map[string]bool{},
		}
		file := &DatabaseSchema{
			Components:  map[string]Component{"C": comp},
			EntityTypes: map[string]EntityType{},
		}
		if got := Diff(domain, file, nil); len(got) != 0 {
			t.Errorf("%s component matching its own table produced %+v", ct, got)
		}
	}
}

// sqlTypeOfDataColumn is what the generator would give a data column, so the
// table this test builds is one the generator would have built.
func sqlTypeOfDataColumn(comp Component, name string) string {
	if StorageLayout(comp.Type) == LayoutColumns {
		return PropertySQLType(comp.Properties[name])
	}
	return propertySQLTypeForComponent(comp.Type)
}

// A column the file no longer declares is a removed property, not a constraint
// change — and must not be reported as both.
func TestDiff_AColumnTheFileDoesNotDeclareIsOnlyRemoved(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{"carrier": {Columns: []DomainColumn{
			entityIDCol(),
			col("hp", "INTEGER", NoReference, false),
			col("owner", "INTEGER", EntityReference, false),
		}}},
		EntityTypeNames: map[string]bool{},
	}
	file := &DatabaseSchema{
		Components: map[string]Component{"Carrier": {
			Type:       ComponentTypeObject,
			Properties: map[string]Property{"hp": {Type: PropertyTypeInteger}},
		}},
		EntityTypes: map[string]EntityType{},
	}
	assertChanges(t, Diff(domain, file, nil), []Change{
		{Kind: ChangeRemovedProperty, Component: "carrier", Property: "owner", OldType: "INTEGER"},
	})
}

// A shape change is already remove-and-add; the new table is built from the
// file, so there is nothing for a constraint change to say about it.
func TestDiff_AShapeChangeDoesNotAlsoReportItsConstraints(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{"carrier": {Columns: []DomainColumn{
			entityIDCol(),
			col("value", "TEXT", NoReference, false),
		}}},
		EntityTypeNames: map[string]bool{},
	}
	file := &DatabaseSchema{
		Components:  map[string]Component{"Carrier": {Type: ComponentTypeEntityRef}},
		EntityTypes: map[string]EntityType{},
	}
	got := Diff(domain, file, nil)
	for _, c := range got {
		if c.Kind == ChangeChangedConstraint {
			t.Errorf("a shape change also reported %+v", c)
		}
	}
}
