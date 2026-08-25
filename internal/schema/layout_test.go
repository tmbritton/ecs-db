package schema

import "testing"

// The three shapes, stated. A component's type decides what columns its table
// has, and only three answers exist — which is one more than the diff used to
// distinguish.
func TestStorageLayout(t *testing.T) {
	for compType, want := range map[string]string{
		ComponentTypeObject:    LayoutColumns,
		ComponentTypeEntityRef: LayoutTargetEntityID,
		ComponentTypeString:    LayoutValue,
		ComponentTypeInteger:   LayoutValue,
		ComponentTypeNumber:    LayoutValue,
		ComponentTypeBoolean:   LayoutValue,
		ComponentTypeArray:     LayoutValue,
	} {
		if got := StorageLayout(compType); got != want {
			t.Errorf("StorageLayout(%q) = %q, want %q", compType, got, want)
		}
	}
}

// An unknown type has no real answer — ValidateSchema refuses one and the
// generator refuses it again — so the layout is the one that changes nothing
// rather than the one that drops a table to build something unbuildable.
func TestStorageLayout_AnUnknownTypeChangesNothing(t *testing.T) {
	if got := StorageLayout("something-else"); got != LayoutValue {
		t.Errorf("StorageLayout of an unknown type = %q, want %q", got, LayoutValue)
	}
}

// entity-ref's SQL type, which no longer has a row in the scalar table because
// it is a different layout — and which the generator still needs to agree
// about, since an entity-ref column is INTEGER and holds an entity id.
func TestPropertySQLTypeForComponent_CoversEveryType(t *testing.T) {
	for compType, want := range map[string]string{
		ComponentTypeString:    "TEXT",
		ComponentTypeInteger:   "INTEGER",
		ComponentTypeNumber:    "REAL",
		ComponentTypeBoolean:   "INTEGER",
		ComponentTypeEntityRef: "INTEGER",
		ComponentTypeArray:     "TEXT",
	} {
		if got := propertySQLTypeForComponent(compType); got != want {
			t.Errorf("propertySQLTypeForComponent(%q) = %q, want %q", compType, got, want)
		}
	}
}

// ComponentTypes is what storage's layout-agreement test enumerates, so an
// empty or short list would make that test pass by having nothing to check.
func TestComponentTypes_IsEveryTypeASchemaMayDeclare(t *testing.T) {
	got := ComponentTypes()
	want := []string{
		ComponentTypeArray, ComponentTypeBoolean, ComponentTypeEntityRef,
		ComponentTypeInteger, ComponentTypeNumber, ComponentTypeObject,
		ComponentTypeString,
	}
	if len(got) != len(want) {
		t.Fatalf("ComponentTypes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ComponentTypes() = %v, want %v (sorted)", got, want)
		}
	}
	// And each one really is accepted, so the list cannot drift from the set
	// that decides it.
	for _, ct := range got {
		if !supportedComponentTypes[ct] {
			t.Errorf("ComponentTypes() lists %q, which is not a supported type", ct)
		}
	}
}
