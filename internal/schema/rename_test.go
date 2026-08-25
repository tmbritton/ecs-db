package schema

import (
	"fmt"
	"strings"
	"testing"
)

func renameFile() *DatabaseSchema {
	return &DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]Component{
			"Placement": {
				RenamedFrom: "Position",
				Type:        ComponentTypeObject,
				Properties: map[string]Property{
					"col_x": {Type: PropertyTypeInteger, RenamedFrom: "x"},
				},
			},
		},
		EntityTypes: map[string]EntityType{"Thing": {ValidationLevel: "strict"}},
	}
}

func renameDomain() *DomainSchema {
	return &DomainSchema{
		Components: map[string]DomainComponent{"position": {Columns: []DomainColumn{
			{Name: "entity_id", SQLType: "INTEGER", IsPK: true, References: EntityReference},
			{Name: "x", SQLType: "INTEGER"},
		}}},
		EntityTypeNames: map[string]bool{},
	}
}

// A declared rename is a rename, not a drop and an add.
func TestDiff_ADeclaredRenameIsARename(t *testing.T) {
	assertChanges(t, Diff(renameDomain(), renameFile(), nil), []Change{
		{Kind: ChangeRenamedComponent, Component: "placement", OldName: "position"},
		{Kind: ChangeRenamedProperty, Component: "placement", Property: "col_x", OldName: "x"},
		{Kind: ChangeAddedEntityType, ETName: "Thing"},
	})
}

// Everything else compares against the renamed shape, rather than against a
// column it believes is gone. The type changed too, and that is a type change on
// the renamed column — not a removal and an addition.
func TestDiff_ARenamedColumnIsStillCompared(t *testing.T) {
	file := renameFile()
	comp := file.Components["Placement"]
	comp.Properties = map[string]Property{"col_x": {Type: PropertyTypeString, RenamedFrom: "x"}}
	file.Components["Placement"] = comp

	assertChanges(t, Diff(renameDomain(), file, nil), []Change{
		{Kind: ChangeRenamedComponent, Component: "placement", OldName: "position"},
		{Kind: ChangeRenamedProperty, Component: "placement", Property: "col_x", OldName: "x"},
		// The entity type is an addition and the retype a modification, so they
		// fall either side of the phase boundary.
		{Kind: ChangeAddedEntityType, ETName: "Thing"},
		{
			Kind: ChangedPropertyType, Component: "placement", Property: "col_x",
			OldType: "INTEGER", NewType: "TEXT",
		},
	})
}

// Running it twice does nothing, so an author may leave renamedFrom in the file.
func TestDiff_ARenameThatAlreadyRanIsNotAChange(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{"placement": {Columns: []DomainColumn{
			{Name: "entity_id", SQLType: "INTEGER", IsPK: true, References: EntityReference},
			{Name: "col_x", SQLType: "INTEGER"},
		}}},
		EntityTypeNames: map[string]bool{"Thing": true},
	}
	if got := Diff(domain, renameFile(), nil); len(got) != 0 {
		t.Errorf("a rename that already ran produced %+v", got)
	}
}

// And a renamedFrom naming something the database never had is just an addition.
func TestDiff_ARenameFromSomethingThatIsNotThereIsAnAddition(t *testing.T) {
	domain := &DomainSchema{
		Components:      map[string]DomainComponent{},
		EntityTypeNames: map[string]bool{"Thing": true},
	}
	assertChanges(t, Diff(domain, renameFile(), nil), []Change{
		{Kind: ChangeAddedComponent, Component: "placement"},
	})
}

// An entity type rename is rows rather than DDL, and produces no change at all
// when no entity of the old type exists.
func TestDiff_AnEntityTypeRename(t *testing.T) {
	file := &DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]Component{},
		EntityTypes:   map[string]EntityType{"Creature": {RenamedFrom: "Monster", ValidationLevel: "strict"}},
	}

	withRows := &DomainSchema{
		Components:      map[string]DomainComponent{},
		EntityTypeNames: map[string]bool{"Monster": true},
	}
	assertChanges(t, Diff(withRows, file, nil), []Change{
		{Kind: ChangeRenamedEntityType, ETName: "Creature", OldName: "Monster"},
	})

	none := &DomainSchema{
		Components:      map[string]DomainComponent{},
		EntityTypeNames: map[string]bool{},
	}
	assertChanges(t, Diff(none, file, nil), []Change{
		{Kind: ChangeAddedEntityType, ETName: "Creature"},
	})
}

// Renames run before anything else: a component renamed and given a property has
// to be renamed before the ALTER TABLE names it.
func TestDiff_RenamesComeFirst(t *testing.T) {
	file := renameFile()
	comp := file.Components["Placement"]
	comp.Properties = map[string]Property{
		"col_x": {Type: PropertyTypeInteger, RenamedFrom: "x"},
		"y":     {Type: PropertyTypeInteger},
	}
	file.Components["Placement"] = comp

	got := Diff(renameDomain(), file, nil)
	var renames, adds int
	for _, c := range got {
		switch c.Kind {
		case ChangeRenamedComponent, ChangeRenamedProperty:
			renames++
			if adds > 0 {
				t.Errorf("a rename came after an addition: %+v", got)
			}
		case ChangeAddedProperty, ChangeAddedComponent, ChangeAddedEntityType:
			adds++
		}
	}
	if renames != 2 || adds == 0 {
		t.Fatalf("expected two renames and an addition, got %+v", got)
	}
}

// ── Validation ───────────────────────────────────────────────────────

func TestValidateSchema_RefusesAnUnusableRenamedFrom(t *testing.T) {
	cases := map[string]struct {
		mutate func(*DatabaseSchema)
		want   string
	}{
		"a component renamed from an unusable name": {
			mutate: func(s *DatabaseSchema) {
				c := s.Components["Placement"]
				c.RenamedFrom = "two words"
				s.Components["Placement"] = c
			},
			want: "two words",
		},
		"a property renamed from an unusable name": {
			mutate: func(s *DatabaseSchema) {
				c := s.Components["Placement"]
				c.Properties = map[string]Property{"col_x": {Type: PropertyTypeInteger, RenamedFrom: "current_time"}}
				s.Components["Placement"] = c
			},
			want: "current_time",
		},
		"a component renamed from itself": {
			mutate: func(s *DatabaseSchema) {
				c := s.Components["Placement"]
				c.RenamedFrom = "Placement"
				s.Components["Placement"] = c
			},
			want: "Placement",
		},
		"a component renamed from one the file still declares": {
			mutate: func(s *DatabaseSchema) {
				s.Components["Position"] = Component{
					Type:       ComponentTypeObject,
					Properties: map[string]Property{"x": {Type: PropertyTypeInteger}},
				}
			},
			want: "Position",
		},
		"a property renamed from one the same component still declares": {
			mutate: func(s *DatabaseSchema) {
				c := s.Components["Placement"]
				c.Properties = map[string]Property{
					"col_x": {Type: PropertyTypeInteger, RenamedFrom: "x"},
					"x":     {Type: PropertyTypeInteger},
				}
				s.Components["Placement"] = c
			},
			want: "x",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := renameFile()
			s.EntityTypes = map[string]EntityType{
				"Thing": {RequiredComponents: []string{"Placement"}, ValidationLevel: "strict"},
			}
			tc.mutate(s)
			err := ValidateSchema(*s)
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the error does not name %q:\n%v", tc.want, err)
			}
		})
	}
}

func TestValidateSchema_AcceptsAWellFormedRename(t *testing.T) {
	s := renameFile()
	s.EntityTypes = map[string]EntityType{
		"Thing": {RequiredComponents: []string{"Placement"}, ValidationLevel: "strict"},
	}
	if err := ValidateSchema(*s); err != nil {
		t.Errorf("a well-formed rename was refused: %v", err)
	}
}

// ── The writer ───────────────────────────────────────────────────────

// Marshal is hand-written, so a field it does not know about is dropped on the
// first Forge save — which for renamedFrom would mean the migration runs once,
// the field vanishes, and nothing records what the column used to be called.
//
// The schema carries every field rather than the ones today's schema.json
// happens to use: a round trip of the checked-in file cannot notice a field the
// file does not contain, which is exactly the case a new field is in.
func TestMarshal_KeepsEveryField(t *testing.T) {
	s := DatabaseSchema{
		SchemaVersion:  4,
		ComponentOrder: []string{"Placement", "Tags", "Carrier"},
		Components: map[string]Component{
			"Placement": {
				RenamedFrom:   "Position",
				Type:          ComponentTypeObject,
				Behavior:      "placement",
				PropertyOrder: []string{"col_x", "nested"},
				Properties: map[string]Property{
					"col_x": {Type: PropertyTypeInteger, RenamedFrom: "x"},
					"nested": {
						Type:          PropertyTypeObject,
						RenamedFrom:   "old_nested",
						PropertyOrder: []string{"deep"},
						Properties:    map[string]Property{"deep": {Type: PropertyTypeString}},
					},
				},
			},
			"Tags":    {Type: ComponentTypeArray, Items: &Property{Type: PropertyTypeString}},
			"Carrier": {Type: ComponentTypeEntityRef},
		},
		EntityTypeOrder: []string{"Creature"},
		EntityTypes: map[string]EntityType{
			"Creature": {
				RenamedFrom:          "Monster",
				Behavior:             "goblin",
				RequiredComponents:   []string{"Placement"},
				OptionalComponents:   []string{"Tags", "Carrier"},
				AllowExtraComponents: true,
				ValidationLevel:      ValidationWarning,
			},
		},
	}

	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	back, err := LoadSchema(out)
	if err != nil {
		t.Fatalf("the file Marshal wrote does not parse: %v\n%s", err, out)
	}

	if got := back.Components["Placement"].RenamedFrom; got != "Position" {
		t.Errorf("a component's renamedFrom came back %q, want %q\n%s", got, "Position", out)
	}
	if got := back.Components["Placement"].Properties["col_x"].RenamedFrom; got != "x" {
		t.Errorf("a leaf property's renamedFrom came back %q, want %q\n%s", got, "x", out)
	}
	if got := back.Components["Placement"].Properties["nested"].RenamedFrom; got != "old_nested" {
		t.Errorf("an expanded property's renamedFrom came back %q, want %q\n%s", got, "old_nested", out)
	}
	if got := back.EntityTypes["Creature"].RenamedFrom; got != "Monster" {
		t.Errorf("an entity type's renamedFrom came back %q, want %q\n%s", got, "Monster", out)
	}

	// And nothing else was lost on the way through.
	if got := back.Components["Placement"].Behavior; got != "placement" {
		t.Errorf("behavior came back %q", got)
	}
	if got := back.EntityTypes["Creature"].Behavior; got != "goblin" {
		t.Errorf("entity type behavior came back %q", got)
	}
	if !back.EntityTypes["Creature"].AllowExtraComponents {
		t.Error("allowExtraComponents came back false")
	}
	if got := back.Components["Tags"].Items; got == nil || got.Type != PropertyTypeString {
		t.Errorf("array items came back %+v", got)
	}
}

// ── The survivors of the mutation battery ────────────────────────────

// A database that has both names. Applying the rename would overwrite the
// component that is already there with the one being renamed, and delete the
// original — so the rename is not applied, and the old component is reported as
// the removal it is.
func TestDiff_ARenameIsNotAppliedWhenBothNamesExist(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"position": {Columns: []DomainColumn{
				{Name: "entity_id", SQLType: "INTEGER", IsPK: true, References: EntityReference},
				{Name: "x", SQLType: "INTEGER"},
			}},
			"placement": {Columns: []DomainColumn{
				{Name: "entity_id", SQLType: "INTEGER", IsPK: true, References: EntityReference},
				{Name: "col_x", SQLType: "INTEGER"},
			}},
		},
		EntityTypeNames: map[string]bool{"Thing": true},
	}
	got := Diff(domain, renameFile(), nil)
	for _, c := range got {
		if c.Kind == ChangeRenamedComponent {
			t.Errorf("a rename was applied over a table that was already there: %+v", got)
		}
	}
	assertChanges(t, got, []Change{
		{Kind: ChangeRemovedComponent, Component: "position"},
	})
}

// Renames have to be independent of each other, because nothing sequences them.
// A chain is what would make the order matter, and it is refused where the
// schema is loaded rather than resolved here.
func TestValidateSchema_RefusesARenameChain(t *testing.T) {
	s := DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]Component{
			"B": {
				RenamedFrom: "A", Type: ComponentTypeObject,
				Properties: map[string]Property{"x": {Type: PropertyTypeInteger}},
			},
			"C": {
				RenamedFrom: "B", Type: ComponentTypeObject,
				Properties: map[string]Property{"x": {Type: PropertyTypeInteger}},
			},
		},
		EntityTypes: map[string]EntityType{
			"Thing": {RequiredComponents: []string{"B", "C"}, ValidationLevel: "strict"},
		},
	}
	err := ValidateSchema(s)
	if err == nil {
		t.Fatal("a rename chain was accepted; whichever ran first would decide the outcome")
	}
	if !strings.Contains(err.Error(), "B") {
		t.Errorf("the error does not name the link in the chain:\n%v", err)
	}
}

// And two things claiming to be the same thing: whichever ran first would take
// the table, and the other would quietly become an empty one.
func TestValidateSchema_RefusesTwoRenamesFromOneName(t *testing.T) {
	cases := map[string]DatabaseSchema{
		"two components": {
			SchemaVersion: 1,
			Components: map[string]Component{
				"B": {
					RenamedFrom: "A", Type: ComponentTypeObject,
					Properties: map[string]Property{"x": {Type: PropertyTypeInteger}},
				},
				"C": {
					RenamedFrom: "A", Type: ComponentTypeObject,
					Properties: map[string]Property{"x": {Type: PropertyTypeInteger}},
				},
			},
			EntityTypes: map[string]EntityType{
				"Thing": {RequiredComponents: []string{"B", "C"}, ValidationLevel: "strict"},
			},
		},
		"two properties": {
			SchemaVersion: 1,
			Components: map[string]Component{
				"Position": {Type: ComponentTypeObject, Properties: map[string]Property{
					"a": {Type: PropertyTypeInteger, RenamedFrom: "x"},
					"b": {Type: PropertyTypeInteger, RenamedFrom: "x"},
				}},
			},
			EntityTypes: map[string]EntityType{
				"Thing": {RequiredComponents: []string{"Position"}, ValidationLevel: "strict"},
			},
		},
		"two entity types": {
			SchemaVersion: 1,
			Components: map[string]Component{
				"Position": {
					Type:       ComponentTypeObject,
					Properties: map[string]Property{"x": {Type: PropertyTypeInteger}},
				},
			},
			EntityTypes: map[string]EntityType{
				"B":     {RenamedFrom: "A", ValidationLevel: "strict"},
				"C":     {RenamedFrom: "A", ValidationLevel: "strict"},
				"Thing": {ValidationLevel: "strict"},
			},
		},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidateSchema(s)
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), "both renamed from") {
				t.Errorf("the error does not say what is wrong:\n%v", err)
			}
		})
	}
}

// A dropped column that could not be the added one — the types differ — is not
// offered as a rename. A wrong suggestion is worse than none: acting on it would
// put a string in an integer column.
func TestDiff_ARenameIsNotSuggestedAcrossATypeChange(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{"position": {Columns: []DomainColumn{
			{Name: "entity_id", SQLType: "INTEGER", IsPK: true, References: EntityReference},
			{Name: "x", SQLType: "INTEGER"},
		}}},
		EntityTypeNames: map[string]bool{"Thing": true},
	}
	file := &DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]Component{
			"Position": {Type: ComponentTypeObject, Properties: map[string]Property{
				"label": {Type: PropertyTypeString},
			}},
		},
		EntityTypes: map[string]EntityType{"Thing": {ValidationLevel: "strict"}},
	}
	for _, c := range Diff(domain, file, nil) {
		if c.Kind == ChangeRemovedProperty {
			if strings.Contains(c.Reason, "renamedFrom") {
				t.Errorf("a rename was suggested between an INTEGER and a TEXT column: %q", c.Reason)
			}
			if !strings.Contains(c.Reason, "goes with it") {
				t.Errorf("the drop does not say what it takes: %q", c.Reason)
			}
		}
	}
}

// Diff does not get to assume the schema validated: Forge previews one in the
// middle of being edited, and a rename chain is refusable only where the file is
// loaded. Two renames whose order matters must at least produce the same answer
// every time, rather than one that depends on map iteration.
func TestDiff_AnUnvalidatedRenameChainIsStillDeterministic(t *testing.T) {
	chain := func() *DatabaseSchema {
		return &DatabaseSchema{
			SchemaVersion: 1,
			Components: map[string]Component{
				"B": {
					RenamedFrom: "A", Type: ComponentTypeObject,
					Properties: map[string]Property{"x": {Type: PropertyTypeInteger}},
				},
				"C": {
					RenamedFrom: "B", Type: ComponentTypeObject,
					Properties: map[string]Property{"x": {Type: PropertyTypeInteger}},
				},
				"D": {
					RenamedFrom: "C", Type: ComponentTypeObject,
					Properties: map[string]Property{"x": {Type: PropertyTypeInteger}},
				},
			},
			EntityTypes: map[string]EntityType{},
		}
	}
	domain := func() *DomainSchema {
		cols := []DomainColumn{
			{Name: "entity_id", SQLType: "INTEGER", IsPK: true, References: EntityReference},
			{Name: "x", SQLType: "INTEGER"},
		}
		return &DomainSchema{
			Components: map[string]DomainComponent{
				"a": {Columns: cols}, "b": {Columns: cols}, "c": {Columns: cols},
			},
			EntityTypeNames: map[string]bool{},
		}
	}

	// This schema would be refused by ValidateSchema — see
	// TestValidateSchema_RefusesARenameChain. What is asserted here is only that
	// Diff answers it the same way twice, not that the answer is meaningful.
	first := fmt.Sprintf("%+v", Diff(domain(), chain(), nil))
	for range 40 {
		if got := fmt.Sprintf("%+v", Diff(domain(), chain(), nil)); got != first {
			t.Fatalf("two runs of the same diff disagree:\n %s\n %s", first, got)
		}
	}
}

// A rename the author declared and the database cannot accept: both names are
// already tables. The rename is refused — it would overwrite one table with
// another — and the drop that happens instead says the declaration was ignored,
// because otherwise the author asked to keep the table and it went anyway.
func TestDiff_ASkippedRenameSaysItWasSkipped(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"position": {Columns: []DomainColumn{
				{Name: "entity_id", SQLType: "INTEGER", IsPK: true, References: EntityReference},
				{Name: "x", SQLType: "INTEGER"},
			}},
			"placement": {Columns: []DomainColumn{
				{Name: "entity_id", SQLType: "INTEGER", IsPK: true, References: EntityReference},
				{Name: "col_x", SQLType: "INTEGER"},
			}},
		},
		EntityTypeNames: map[string]bool{"Thing": true},
	}
	var removal *Change
	for _, c := range Diff(domain, renameFile(), nil) {
		if c.Kind == ChangeRemovedComponent {
			got := c
			removal = &got
		}
	}
	if removal == nil {
		t.Fatal("nothing was removed, so this proves nothing")
	}
	for _, want := range []string{"declared as renamed", "placement", "already in the database"} {
		if !strings.Contains(removal.Reason, want) {
			t.Errorf("the drop does not say %q:\n%s", want, removal.Reason)
		}
	}
}

// Two dropped and one added is not an unambiguous rename, and offering it to
// both would be two contradictory suggestions. Acting on the wrong one moves the
// wrong data, which is what the guess exists to avoid.
func TestDiff_ARenameIsNotSuggestedWhenTwoThingsWereDropped(t *testing.T) {
	t.Run("columns", func(t *testing.T) {
		domain := &DomainSchema{
			Components: map[string]DomainComponent{"position": {Columns: []DomainColumn{
				{Name: "entity_id", SQLType: "INTEGER", IsPK: true, References: EntityReference},
				{Name: "x", SQLType: "INTEGER"},
				{Name: "y", SQLType: "INTEGER"},
			}}},
			EntityTypeNames: map[string]bool{"Thing": true},
		}
		file := &DatabaseSchema{
			SchemaVersion: 1,
			Components: map[string]Component{
				"Position": {Type: ComponentTypeObject, Properties: map[string]Property{
					"z": {Type: PropertyTypeInteger},
				}},
			},
			EntityTypes: map[string]EntityType{"Thing": {ValidationLevel: "strict"}},
		}
		var suggested int
		for _, c := range Diff(domain, file, nil) {
			if c.Kind == ChangeRemovedProperty && strings.Contains(c.Reason, "renamedFrom") {
				suggested++
			}
		}
		if suggested > 0 {
			t.Errorf("%d of two dropped columns were each offered as the rename", suggested)
		}
	})

	t.Run("tables", func(t *testing.T) {
		cols := []DomainColumn{
			{Name: "entity_id", SQLType: "INTEGER", IsPK: true, References: EntityReference},
			{Name: "x", SQLType: "INTEGER"},
		}
		domain := &DomainSchema{
			Components: map[string]DomainComponent{
				"position": {Columns: cols},
				"velocity": {Columns: cols},
			},
			EntityTypeNames: map[string]bool{"Thing": true},
		}
		file := &DatabaseSchema{
			SchemaVersion: 1,
			Components: map[string]Component{
				"Placement": {Type: ComponentTypeObject, Properties: map[string]Property{
					"x": {Type: PropertyTypeInteger},
				}},
			},
			EntityTypes: map[string]EntityType{"Thing": {ValidationLevel: "strict"}},
		}
		var suggested int
		for _, c := range Diff(domain, file, nil) {
			if c.Kind == ChangeRemovedComponent && strings.Contains(c.Reason, "renamedFrom") {
				suggested++
			}
		}
		if suggested > 0 {
			t.Errorf("%d of two dropped tables were each offered as the rename", suggested)
		}
	})
}

// A renamedFrom below the top level has no column to act on, so it is refused
// rather than ignored — a data-preservation field that silently does nothing is
// what this story exists to remove.
func TestValidateSchema_RefusesANestedRenamedFrom(t *testing.T) {
	cases := map[string]Component{
		"a nested object's property": {
			Type: ComponentTypeObject,
			Properties: map[string]Property{
				"outer": {Type: PropertyTypeObject, Properties: map[string]Property{
					"inner": {Type: PropertyTypeInteger, RenamedFrom: "was"},
				}},
			},
		},
		"an array's items": {
			Type: ComponentTypeObject,
			Properties: map[string]Property{
				"tags": {Type: PropertyTypeArray, Items: &Property{Type: PropertyTypeString, RenamedFrom: "was"}},
			},
		},
		"an array component's items": {
			Type:  ComponentTypeArray,
			Items: &Property{Type: PropertyTypeString, RenamedFrom: "was"},
		},
	}
	for name, comp := range cases {
		t.Run(name, func(t *testing.T) {
			s := DatabaseSchema{
				SchemaVersion: 1,
				Components:    map[string]Component{"Thing": comp},
				EntityTypes: map[string]EntityType{
					"Entity": {RequiredComponents: []string{"Thing"}, ValidationLevel: "strict"},
				},
			}
			err := ValidateSchema(s)
			if err == nil {
				t.Fatal("a renamedFrom that cannot be acted on was accepted")
			}
			if !strings.Contains(err.Error(), "does nothing") {
				t.Errorf("the error does not say it does nothing:\n%v", err)
			}
		})
	}
}
