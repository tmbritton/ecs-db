package schema

import (
	"testing"
)

// ── Identical schemas ───────────────────────────────────────────────

func TestDiff_IdenticalSchemas_Empty(t *testing.T) {
	domain := &DomainSchema{
		SchemaVersion:   1,
		Components:      make(map[string]DomainComponent),
		EntityTypeNames: make(map[string]bool),
	}
	file := &DatabaseSchema{
		SchemaVersion: 1,
		Components:    make(map[string]Component),
		EntityTypes:   make(map[string]EntityType),
	}

	changes := Diff(domain, file, nil)

	if changes == nil {
		t.Fatal("Diff returned nil, want empty slice")
	}
	if len(changes) != 0 {
		t.Fatalf("len(changes) = %d, want 0", len(changes))
	}
}

func TestDiff_IdenticalSchemas_NonEmpty(t *testing.T) {
	domain := &DomainSchema{
		SchemaVersion: 3,
		Components: map[string]DomainComponent{
			"position": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "x", SQLType: "REAL"},
					{Name: "y", SQLType: "REAL"},
				},
			},
			"health": {
				Type:    "integer",
				Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "INTEGER"}},
			},
		},
		EntityTypeNames: map[string]bool{"Player": true, "Enemy": true},
	}
	file := &DatabaseSchema{
		SchemaVersion: 3,
		Components: map[string]Component{
			"Position": {
				Type: ComponentTypeObject,
				Properties: map[string]Property{
					"x": {Type: PropertyTypeNumber},
					"y": {Type: PropertyTypeNumber},
				},
			},
			"Health": {Type: ComponentTypeInteger},
		},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}},
			"Enemy":  {RequiredComponents: []string{"Position", "Health"}},
		},
	}

	changes := Diff(domain, file, &DatabaseSchema{
		Components: map[string]Component{
			"Position": {Type: ComponentTypeObject, Properties: map[string]Property{"x": {Type: PropertyTypeNumber}, "y": {Type: PropertyTypeNumber}}},
			"Health":   {Type: ComponentTypeInteger},
		},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}},
			"Enemy":  {RequiredComponents: []string{"Position", "Health"}},
		},
	})

	if len(changes) != 0 {
		t.Fatalf("expected empty diff for identical schemas, got %d changes", len(changes))
	}
}

// ── Component additions ─────────────────────────────────────────────

func TestDiff_AddedComponent_One(t *testing.T) {
	domain := &DomainSchema{Components: make(map[string]DomainComponent), EntityTypeNames: make(map[string]bool)}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Position": {Type: ComponentTypeObject, Properties: map[string]Property{"x": {Type: PropertyTypeNumber}}},
		},
		EntityTypes: map[string]EntityType{},
	}

	changes := Diff(domain, file, nil)
	assertChanges(t, changes, []Change{
		{Kind: ChangeAddedComponent, Component: "position"},
	})
}

func TestDiff_AddedComponent_Multiple(t *testing.T) {
	domain := &DomainSchema{Components: make(map[string]DomainComponent), EntityTypeNames: make(map[string]bool)}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Sprite":   {Type: ComponentTypeString},
			"Health":   {Type: ComponentTypeInteger},
			"Position": {Type: ComponentTypeObject, Properties: map[string]Property{"x": {Type: PropertyTypeNumber}}},
		},
		EntityTypes: map[string]EntityType{},
	}

	changes := Diff(domain, file, nil)

	if len(changes) != 3 {
		t.Fatalf("len(changes) = %d, want 3", len(changes))
	}
	// Should be sorted alphabetically.
	assertChanges(t, changes, []Change{
		{Kind: ChangeAddedComponent, Component: "health"},
		{Kind: ChangeAddedComponent, Component: "position"},
		{Kind: ChangeAddedComponent, Component: "sprite"},
	})
}

// ── Component removals ──────────────────────────────────────────────

func TestDiff_RemovedComponent_One(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"position": {Type: "object", Columns: []DomainColumn{
				{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
				{Name: "x", SQLType: "REAL"},
			}},
		},
		EntityTypeNames: make(map[string]bool),
	}
	file := &DatabaseSchema{Components: make(map[string]Component), EntityTypes: map[string]EntityType{}}

	changes := Diff(domain, file, nil)
	assertChanges(t, changes, []Change{
		{Kind: ChangeRemovedComponent, Component: "position"},
	})
}

func TestDiff_RemovedComponent_Multiple(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"sprite":   {Type: "string", Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "TEXT"}}},
			"health":   {Type: "integer", Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "INTEGER"}}},
			"position": {Type: "object", Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}}},
		},
		EntityTypeNames: make(map[string]bool),
	}
	file := &DatabaseSchema{Components: make(map[string]Component), EntityTypes: map[string]EntityType{}}

	changes := Diff(domain, file, nil)
	assertChanges(t, changes, []Change{
		{Kind: ChangeRemovedComponent, Component: "health"},
		{Kind: ChangeRemovedComponent, Component: "position"},
		{Kind: ChangeRemovedComponent, Component: "sprite"},
	})
}

// ── Added properties ────────────────────────────────────────────────

func TestDiff_AddedProperty_Object(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"position": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "x", SQLType: "REAL"},
				},
			},
		},
		EntityTypeNames: make(map[string]bool),
	}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Position": {
				Type: ComponentTypeObject,
				Properties: map[string]Property{
					"x": {Type: PropertyTypeNumber},
					"y": {Type: PropertyTypeNumber},
				},
			},
		},
		EntityTypes: map[string]EntityType{},
	}

	changes := Diff(domain, file, nil)
	assertChanges(t, changes, []Change{
		{Kind: ChangeAddedProperty, Component: "position", Property: "y", NewType: "REAL"},
	})
}

func TestDiff_AddedProperty_Multiple(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"stats": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "hp", SQLType: "INTEGER"},
				},
			},
		},
		EntityTypeNames: make(map[string]bool),
	}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Stats": {
				Type: ComponentTypeObject,
				Properties: map[string]Property{
					"hp":     {Type: PropertyTypeInteger},
					"name":   {Type: PropertyTypeString},
					"active": {Type: PropertyTypeBoolean},
				},
			},
		},
		EntityTypes: map[string]EntityType{},
	}

	changes := Diff(domain, file, nil)
	assertChanges(t, changes, []Change{
		{Kind: ChangeAddedProperty, Component: "stats", Property: "active", NewType: "INTEGER"},
		{Kind: ChangeAddedProperty, Component: "stats", Property: "name", NewType: "TEXT"},
	})
}

// ── Removed properties ──────────────────────────────────────────────

func TestDiff_RemovedProperty_Object(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"position": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "x", SQLType: "REAL"},
					{Name: "y", SQLType: "REAL"},
				},
			},
		},
		EntityTypeNames: make(map[string]bool),
	}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Position": {
				Type:       ComponentTypeObject,
				Properties: map[string]Property{"x": {Type: PropertyTypeNumber}},
			},
		},
		EntityTypes: map[string]EntityType{},
	}

	changes := Diff(domain, file, nil)
	assertChanges(t, changes, []Change{
		{Kind: ChangeRemovedProperty, Component: "position", Property: "y", OldType: "REAL"},
	})
}

func TestDiff_RemovedProperty_Multiple(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"stats": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "hp", SQLType: "INTEGER"},
					{Name: "name", SQLType: "TEXT"},
					{Name: "active", SQLType: "BOOLEAN"},
				},
			},
		},
		EntityTypeNames: make(map[string]bool),
	}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Stats": {Type: ComponentTypeObject, Properties: map[string]Property{"hp": {Type: PropertyTypeInteger}}},
		},
		EntityTypes: map[string]EntityType{},
	}

	changes := Diff(domain, file, nil)
	assertChanges(t, changes, []Change{
		{Kind: ChangeRemovedProperty, Component: "stats", Property: "active", OldType: "BOOLEAN"},
		{Kind: ChangeRemovedProperty, Component: "stats", Property: "name", OldType: "TEXT"},
	})
}

// ── SQL type changes ────────────────────────────────────────────────

func TestDiff_ChangedPropertyType_Object(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"position": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "x", SQLType: "TEXT"},
				},
			},
		},
		EntityTypeNames: make(map[string]bool),
	}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Position": {Type: ComponentTypeObject, Properties: map[string]Property{"x": {Type: PropertyTypeNumber}}},
		},
		EntityTypes: map[string]EntityType{},
	}

	changes := Diff(domain, file, nil)
	assertChanges(t, changes, []Change{
		{Kind: ChangedPropertyType, Component: "position", Property: "x", OldType: "TEXT", NewType: "REAL"},
	})
}

func TestDiff_ChangedPropertyType_Scalar(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"health": {
				Type:    "string",
				Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "TEXT"}},
			},
		},
		EntityTypeNames: make(map[string]bool),
	}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Health": {Type: ComponentTypeInteger},
		},
		EntityTypes: map[string]EntityType{},
	}

	changes := Diff(domain, file, nil)
	assertChanges(t, changes, []Change{
		{Kind: ChangedPropertyType, Component: "health", Property: "value", OldType: "TEXT", NewType: "INTEGER"},
	})
}

func TestDiff_SameSQLType_NoChange(t *testing.T) {
	// integer and boolean both map to INTEGER — no DDL change needed.
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"health": {
				Type:    "integer",
				Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "INTEGER"}},
			},
		},
		EntityTypeNames: make(map[string]bool),
	}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Health": {Type: ComponentTypeBoolean},
		},
		EntityTypes: map[string]EntityType{},
	}

	changes := Diff(domain, file, nil)
	if len(changes) != 0 {
		t.Fatalf("expected no changes (same SQL type INTEGER), got %d", len(changes))
	}
}

// ── Structural incompatibility ──────────────────────────────────────

func TestDiff_ObjectToScalar_RemoveAdd(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"position": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "x", SQLType: "REAL"},
					{Name: "y", SQLType: "REAL"},
				},
			},
		},
		EntityTypeNames: make(map[string]bool),
	}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Position": {Type: ComponentTypeString},
		},
		EntityTypes: map[string]EntityType{},
	}

	changes := Diff(domain, file, nil)
	// Should be a removed + added pair, NOT property-level diffs.
	assertChanges(t, changes, []Change{
		{Kind: ChangeAddedComponent, Component: "position"},
		{Kind: ChangeRemovedComponent, Component: "position"},
	})
}

func TestDiff_ScalarToObject_RemoveAdd(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"marker": {
				Type:    "string",
				Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "TEXT"}},
			},
		},
		EntityTypeNames: make(map[string]bool),
	}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Marker": {Type: ComponentTypeObject, Properties: map[string]Property{"x": {Type: PropertyTypeNumber}}},
		},
		EntityTypes: map[string]EntityType{},
	}

	changes := Diff(domain, file, nil)
	assertChanges(t, changes, []Change{
		{Kind: ChangeAddedComponent, Component: "marker"},
		{Kind: ChangeRemovedComponent, Component: "marker"},
	})
}

// ── Ordering ────────────────────────────────────────────────────────

func TestDiff_Ordering_MixedChanges(t *testing.T) {
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"health": {
				Type:    "integer",
				Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "INTEGER"}},
			},
			"position": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "x", SQLType: "REAL"},
				},
			},
			"sprite": {
				Type:    "string",
				Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "TEXT"}},
			},
		},
		EntityTypeNames: map[string]bool{"Player": true},
	}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Position": {
				Type: ComponentTypeObject,
				Properties: map[string]Property{
					"x": {Type: PropertyTypeNumber},
					"z": {Type: PropertyTypeNumber}, // new property
				},
			},
			"Shield":   {Type: ComponentTypeObject, Properties: map[string]Property{"strength": {Type: PropertyTypeInteger}}}, // new
			"Velocity": {Type: ComponentTypeObject, Properties: map[string]Property{"vx": {Type: PropertyTypeNumber}}},        // new
		},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}},
			"Enemy":  {RequiredComponents: []string{"Position", "Health"}}, // new ET
		},
	}

	changes := Diff(domain, file, nil)

	// Phase 1: additions (sorted alphabetically by component/ET name)
	// Phase 2: modifications (none in this scenario)
	// Phase 3: removals (sorted alphabetically by component)
	// Note: within one phase, sortKey uses Component/ETName + Property + Kind.
	// "enemy" < "position" < "shield" < "velocity" alphabetically.
	wantOrder := []Change{
		// Phase 1: Additions
		{Kind: ChangeAddedEntityType, ETName: "Enemy"},
		{Kind: ChangeAddedProperty, Component: "position", Property: "z", NewType: "REAL"},
		{Kind: ChangeAddedComponent, Component: "shield"},
		{Kind: ChangeAddedComponent, Component: "velocity"},
		// Phase 3: Removals
		{Kind: ChangeRemovedComponent, Component: "health"},
		{Kind: ChangeRemovedComponent, Component: "sprite"},
	}

	if len(changes) != len(wantOrder) {
		t.Fatalf("len(changes) = %d, want %d", len(changes), len(wantOrder))
	}
	for i, w := range wantOrder {
		c := changes[i]
		if c.Kind != w.Kind {
			t.Errorf("changes[%d].Kind = %q, want %q", i, c.Kind, w.Kind)
		}
		if c.Component != w.Component {
			t.Errorf("changes[%d].Component = %q, want %q", i, c.Component, w.Component)
		}
		if c.Property != w.Property {
			t.Errorf("changes[%d].Property = %q, want %q", i, c.Property, w.Property)
		}
		if c.ETName != w.ETName {
			t.Errorf("changes[%d].ETName = %q, want %q", i, c.ETName, w.ETName)
		}
	}

	// Verify phases: all additions should come before removals.
	lastAddPhase := -1
	firstRemovalPhase := 100
	for i, c := range changes {
		p := c.phase()
		if p == 1 && i > lastAddPhase {
			lastAddPhase = i
		}
		if p == 3 && i < firstRemovalPhase {
			firstRemovalPhase = i
		}
	}
	if lastAddPhase >= firstRemovalPhase {
		t.Errorf("additions should come before removals: last addition at index %d, first removal at %d", lastAddPhase, firstRemovalPhase)
	}
}

// ── Entity type additions/removals ──────────────────────────────────

func TestDiff_AddedEntityType(t *testing.T) {
	domain := &DomainSchema{Components: make(map[string]DomainComponent), EntityTypeNames: map[string]bool{}}
	file := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}},
		},
	}

	changes := Diff(domain, file, nil)
	assertChanges(t, changes, []Change{
		{Kind: ChangeAddedEntityType, ETName: "Player", NewET: &EntityType{RequiredComponents: []string{"Position"}}},
	})
}

func TestDiff_RemovedEntityType(t *testing.T) {
	domain := &DomainSchema{
		Components:      make(map[string]DomainComponent),
		EntityTypeNames: map[string]bool{"Player": true},
	}
	file := &DatabaseSchema{
		Components:  map[string]Component{},
		EntityTypes: map[string]EntityType{},
	}

	changes := Diff(domain, file, nil)
	assertChanges(t, changes, []Change{
		{Kind: ChangeRemovedEntityType, ETName: "Player"},
	})
}

// ── Entity type changes (requires oldFile) ──────────────────────────

func TestDiff_ChangedEntityType_RequiredComponents(t *testing.T) {
	domain := &DomainSchema{
		Components:      make(map[string]DomainComponent),
		EntityTypeNames: map[string]bool{"Player": true},
	}
	file := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position", "Health"}},
		},
	}
	oldFile := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}},
		},
	}

	changes := Diff(domain, file, oldFile)

	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	c := changes[0]
	if c.Kind != ChangeChangedEntityType {
		t.Fatalf("Kind = %q, want changed_entity_type", c.Kind)
	}
	if c.ETName != "Player" {
		t.Errorf("ETName = %q, want Player", c.ETName)
	}
	if len(c.OldET.RequiredComponents) != 1 || c.OldET.RequiredComponents[0] != "Position" {
		t.Errorf("OldET.RequiredComponents = %v, want [Position]", c.OldET.RequiredComponents)
	}
	if len(c.NewET.RequiredComponents) != 2 {
		t.Errorf("NewET.RequireRequiredComponents len = %d, want 2", len(c.NewET.RequiredComponents))
	}
}

func TestDiff_ChangedEntityType_ValidationLevel(t *testing.T) {
	domain := &DomainSchema{
		Components:      make(map[string]DomainComponent),
		EntityTypeNames: map[string]bool{"Player": true},
	}
	file := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}, ValidationLevel: ValidationWarning},
		},
	}
	oldFile := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}, ValidationLevel: ValidationStrict},
		},
	}

	changes := Diff(domain, file, oldFile)
	if len(changes) != 1 {
		t.Fatalf("len = %d, want 1", len(changes))
	}
	if changes[0].Kind != ChangeChangedEntityType {
		t.Errorf("Kind = %q, want changed_entity_type", changes[0].Kind)
	}
}

func TestDiff_ChangedEntityType_AllFields(t *testing.T) {
	domain := &DomainSchema{
		Components:      make(map[string]DomainComponent),
		EntityTypeNames: map[string]bool{"Player": true},
	}
	file := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {
				RequiredComponents:   []string{"Position", "Health"},
				OptionalComponents:   []string{"Velocity", "Sprite"},
				AllowExtraComponents: true,
				ValidationLevel:      ValidationWarning,
			},
		},
	}
	oldFile := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {
				RequiredComponents:   []string{"Position"},
				OptionalComponents:   []string{"Velocity"},
				AllowExtraComponents: false,
				ValidationLevel:      ValidationStrict,
			},
		},
	}

	changes := Diff(domain, file, oldFile)
	if len(changes) != 1 {
		t.Fatalf("len = %d, want 1", len(changes))
	}
	c := changes[0]
	if c.Kind != ChangeChangedEntityType {
		t.Fatalf("Kind = %q, want changed_entity_type", c.Kind)
	}
	if !entityTypeDeepEqual(*c.OldET, oldFile.EntityTypes["Player"]) {
		t.Errorf("OldET doesn't match old file spec")
	}
	if !entityTypeDeepEqual(*c.NewET, file.EntityTypes["Player"]) {
		t.Errorf("NewET doesn't match new file spec")
	}
}

func TestDiff_ChangedEntityType_NoChange(t *testing.T) {
	domain := &DomainSchema{
		Components:      make(map[string]DomainComponent),
		EntityTypeNames: map[string]bool{"Player": true},
	}
	file := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}, ValidationLevel: ValidationStrict, AllowExtraComponents: false},
		},
	}
	oldFile := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}, ValidationLevel: ValidationStrict, AllowExtraComponents: false},
		},
	}

	changes := Diff(domain, file, oldFile)
	// The entity type exists in both DB and file, but hasn't changed from oldFile.
	// No AddedEntityType (exists in both), no RemovedEntityType (exists in both), no ChangedEntityType (identical).
	for _, c := range changes {
		if c.ETName == "Player" {
			t.Errorf("unexpected change for unchanged entity type Player: %+v", c)
		}
	}
}

func TestDiff_ChangedEntityType_OrderInsensitive(t *testing.T) {
	// RequiredComponents in different order should NOT trigger a change.
	domain := &DomainSchema{
		Components:      make(map[string]DomainComponent),
		EntityTypeNames: map[string]bool{"Player": true},
	}
	file := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Health", "Position"}},
		},
	}
	oldFile := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position", "Health"}},
		},
	}

	changes := Diff(domain, file, oldFile)
	for _, c := range changes {
		if c.ETName == "Player" && c.Kind == ChangeChangedEntityType {
			t.Error("should not detect a change when only order differs")
		}
	}
}

func TestDiff_EntityType_AddedAndChangedInOneDiff(t *testing.T) {
	domain := &DomainSchema{
		Components:      make(map[string]DomainComponent),
		EntityTypeNames: map[string]bool{"Player": true},
		// Enemy is absent — will be "AddedEntityType"
	}
	file := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}},           // same
			"Goblin": {RequiredComponents: []string{"Position", "Health"}}, // new
		},
	}
	oldFile := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}},
		},
	}

	changes := Diff(domain, file, oldFile)

	// Expect: AddedEntityType Goblin, no Player change.
	addedCount := 0
	changedCount := 0
	for _, c := range changes {
		switch c.ETName {
		case "Goblin":
			if c.Kind == ChangeAddedEntityType {
				addedCount++
			}
		case "Player":
			if c.Kind == ChangeChangedEntityType {
				changedCount++
			}
		}
	}
	if addedCount != 1 {
		t.Errorf("expected 1 AddedEntityType for Goblin, got %d", addedCount)
	}
	if changedCount != 0 {
		t.Errorf("Player should not appear as ChangedEntityType, got %d", changedCount)
	}
}

// ── Nil safety ──────────────────────────────────────────────────────

func TestDiff_NilInputs(t *testing.T) {
	// Both nil — should not panic, return empty slice.
	changes := Diff(nil, nil, nil)
	if changes == nil {
		t.Fatal("Diff(nil, nil, nil) returned nil")
	}
	if len(changes) != 0 {
		t.Fatalf("len = %d, want 0", len(changes))
	}

	// Only domain nil.
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Position": {Type: ComponentTypeObject, Properties: map[string]Property{"x": {Type: PropertyTypeNumber}}},
		},
		EntityTypes: map[string]EntityType{},
	}
	changes = Diff(nil, file, nil)
	if len(changes) != 1 || changes[0].Kind != ChangeAddedComponent {
		t.Errorf("Diff(nil, file, nil) expected 1 AddedComponent, got %d", len(changes))
	}

	// Only file nil.
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"position": {Type: "object", Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}}},
		},
		EntityTypeNames: map[string]bool{},
	}
	changes = Diff(domain, nil, nil)
	if len(changes) != 1 || changes[0].Kind != ChangeRemovedComponent {
		t.Errorf("Diff(domain, nil, nil) expected 1 RemovedComponent, got %d", len(changes))
	}
}

// ── Helper ──────────────────────────────────────────────────────────

func assertChanges(t *testing.T, got, want []Change) {
	if len(got) != len(want) {
		t.Fatalf("len(changes) = %d, want %d\nGot: %+v\nWant: %+v", len(got), len(want), got, want)
	}
	for i, w := range want {
		g := got[i]
		if g.Kind != w.Kind {
			t.Errorf("changes[%d].Kind = %q, want %q", i, g.Kind, w.Kind)
		}
		if g.Component != w.Component {
			t.Errorf("changes[%d].Component = %q, want %q", i, g.Component, w.Component)
		}
		if g.Property != w.Property {
			t.Errorf("changes[%d].Property = %q, want %q", i, g.Property, w.Property)
		}
		if g.ETName != w.ETName {
			t.Errorf("changes[%d].ETName = %q, want %q", i, g.ETName, w.ETName)
		}
	}
}

// ── Coverage helpers (unreachable ChangeKind) ───────────────────────

func TestChange_Phase_UnknownKind(t *testing.T) {
	// Exercise the safety net default branch.
	c := Change{Kind: ChangeKind("bogus")}
	if p := c.phase(); p != 99 {
		t.Errorf("phase() = %d, want 99", p)
	}
}

func TestChange_SortKey_UnknownKind(t *testing.T) {
	c := Change{Kind: ChangeKind("bogus"), Component: "alpha"}
	key := c.sortKey()
	if key == "" {
		t.Error("sortKey() returned empty string")
	}
}

// ── propertySQLTypeForComponent coverage ────────────────────────────

func TestDiff_ScalarTypes_AllCovered(t *testing.T) {
	// Tests that every scalar type maps to the expected SQL type via the
	// diff's internal propertySQLTypeForComponent. Each subtest passes a
	// domain component with a different type and verifies the generated
	// change uses the correct SQL type.
	tests := []struct {
		name     string
		dbType   string
		dbSQL    string
		fileType string
		wantNew  string
		wantOld  string
	}{
		{"integer→string", "integer", "INTEGER", ComponentTypeString, "TEXT", "INTEGER"},
		{"number→integer", "number", "REAL", ComponentTypeInteger, "INTEGER", "REAL"},
		{"boolean→number", "boolean", "INTEGER", ComponentTypeNumber, "REAL", "INTEGER"},
		{"entity-ref→array", "entity-ref", "INTEGER", ComponentTypeArray, "TEXT", "INTEGER"},
		{"string→entity-ref", "string", "TEXT", ComponentTypeEntityRef, "INTEGER", "TEXT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domain := &DomainSchema{
				Components: map[string]DomainComponent{
					"val": {
						Type:    tt.dbType,
						Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: tt.dbSQL}},
					},
				},
				EntityTypeNames: make(map[string]bool),
			}
			file := &DatabaseSchema{
				Components: map[string]Component{
					"Val": {Type: tt.fileType},
				},
				EntityTypes: map[string]EntityType{},
			}

			changes := Diff(domain, file, nil)
			assertChanges(t, changes, []Change{
				{Kind: ChangedPropertyType, Component: "val", Property: "value", OldType: tt.wantOld, NewType: tt.wantNew},
			})
		})
	}
}

// ── equalStringSliceSets coverage ──────────────────────────────────

func TestDiff_ChangedEntityType_DifferentLengths(t *testing.T) {
	// Tests that entity type comparison handles different-size slices
	// correctly (the len(a) != len(b) early return in equalStringSliceSets).
	domain := &DomainSchema{
		Components:      make(map[string]DomainComponent),
		EntityTypeNames: map[string]bool{"Player": true},
	}
	file := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position", "Health"}},
		},
	}
	oldFile := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}},
		},
	}

	changes := Diff(domain, file, oldFile)
	// Should detect the change (extra "Health" in required).
	found := false
	for _, c := range changes {
		if c.Kind == ChangeChangedEntityType && c.ETName == "Player" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected ChangedEntityType for Player, got: %+v", changes)
	}
}

// ── Empty entity type names ─────────────────────────────────────────

func TestDiff_EntityTypeNamesNotSet(t *testing.T) {
	// Domain with nil EntityTypeNames should not panic.
	domain := &DomainSchema{
		Components:      make(map[string]DomainComponent),
		EntityTypeNames: nil,
	}
	file := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}},
		},
	}

	changes := Diff(domain, file, nil)
	// "Player" is in file but nil map → should be treated as absent → AddedEntityType
	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	if changes[0].Kind != ChangeAddedEntityType {
		t.Errorf("Kind = %q, want added_entity_type", changes[0].Kind)
	}
}

// ── Branch coverage for remaining uncovered lines ──────────────────

func TestDiff_ModificationPhase_Sorting(t *testing.T) {
	// Covers the phase() case for ChangedPropertyType / ChangeChangedEntityType.
	// Need a scenario with modifications mixed alongside additions/removals.
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"position": {
				Type: "object",
				Columns: []DomainColumn{
					{Name: "entity_id", SQLType: "INTEGER", IsPK: true},
					{Name: "x", SQLType: "TEXT"}, // TEXT in DB, will be changed to REAL
				},
			},
		},
		EntityTypeNames: map[string]bool{"Player": true},
	}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Position": {
				Type:       ComponentTypeObject,
				Properties: map[string]Property{"x": {Type: PropertyTypeNumber}}, // TEXT → REAL change
			},
		},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}},
		},
	}
	oldFile := &DatabaseSchema{
		Components: map[string]Component{
			"Position": {
				Type:       ComponentTypeObject,
				Properties: map[string]Property{"x": {Type: PropertyTypeString}}, // was string
			},
		},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position", "Other"}, AllowExtraComponents: true},
		},
	}

	changes := Diff(domain, file, oldFile)
	// Expect one ChangedPropertyType for position.x (TEXT→REAL)
	// and one ChangedEntityType for Player.
	modCount := 0
	for _, c := range changes {
		if c.Kind == ChangedPropertyType || c.Kind == ChangeChangedEntityType {
			modCount++
		}
	}
	if modCount == 0 {
		t.Fatal("expected at least one modification change")
	}
	// Verify ordering: modifications (phase 2) come before nothing else here.
	// The sort comparison that compares phase(i) vs phase(j) is now covered.
}

func TestDiff_OldFileContainsRemovedEntityType(t *testing.T) {
	// Covers the continue branch when oldFile has an entity type that
	// doesn't exist in the current file (line 213).
	domain := &DomainSchema{
		Components:      make(map[string]DomainComponent),
		EntityTypeNames: map[string]bool{},
	}
	file := &DatabaseSchema{
		Components:  map[string]Component{},
		EntityTypes: map[string]EntityType{},
	}
	oldFile := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Zombie": {RequiredComponents: []string{"Position"}},
		},
	}

	changes := Diff(domain, file, oldFile)
	// Should not panic or crash. Zombie is in oldFile but not in file,
	// so the continue branch is taken.
	if changes == nil {
		t.Fatal("changes is nil")
	}
	if len(changes) != 0 {
		t.Fatalf("expected empty diff (no file DB entity types), got %d changes", len(changes))
	}
}

func TestDiff_ScalarComponent_UnknownType(t *testing.T) {
	// Covers the default branch in propertySQLTypeForComponent via
	// diffScalarComponent when the file declares an unknown scalar type.
	domain := &DomainSchema{
		Components: map[string]DomainComponent{
			"mystery": {
				Type:    "string",
				Columns: []DomainColumn{{Name: "entity_id", SQLType: "INTEGER", IsPK: true}, {Name: "value", SQLType: "TEXT"}},
			},
		},
		EntityTypeNames: make(map[string]bool),
	}
	file := &DatabaseSchema{
		Components: map[string]Component{
			"Mystery": {Type: "bogus-type"},
		},
		EntityTypes: map[string]EntityType{},
	}

	changes := Diff(domain, file, nil)
	// "bogus-type" maps to TEXT via default branch, same as "string" → TEXT.
	// No change expected.
	if len(changes) != 0 {
		t.Fatalf("expected no changes (same SQL type TEXT), got %d", len(changes))
	}
}

func TestDiff_ChangedEntityType_SameLength_DifferentValues(t *testing.T) {
	// Covers the element-mismatch branch in equalStringSliceSets (line 359):
	// same length but different content.
	domain := &DomainSchema{
		Components:      make(map[string]DomainComponent),
		EntityTypeNames: map[string]bool{"Player": true},
	}
	file := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Position"}},
		},
	}
	oldFile := &DatabaseSchema{
		Components: map[string]Component{},
		EntityTypes: map[string]EntityType{
			"Player": {RequiredComponents: []string{"Health"}}, // same length, different value
		},
	}

	changes := Diff(domain, file, oldFile)
	found := false
	for _, c := range changes {
		if c.Kind == ChangeChangedEntityType && c.ETName == "Player" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected ChangedEntityType for Player, got: %+v", changes)
	}
}
