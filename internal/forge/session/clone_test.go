package session

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// richSchema exercises every branch of clone. The original fixture was a flat
// object component with alphabetically-ordered primitive properties, no
// behavior, no array and no nesting — so seven of clone's eight branches were
// dead in tests, and deleting any of them passed the whole suite.
//
// Non-alphabetical order throughout, on purpose: a clone that drops the order
// slices looks identical under a sorted fixture.
const richSchema = `{
  "schemaVersion": 3,
  "components": {
    "Sprite": {
      "type": "object",
      "properties": {
        "sheet":     { "type": "string" },
        "animation": { "type": "string" },
        "flip_x":    { "type": "boolean" }
      }
    },
    "Bound": {
      "type": "object",
      "behavior": "wander",
      "properties": {
        "zeta":  { "type": "number" },
        "alpha": { "type": "number" }
      }
    },
    "Waypoints": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "y": { "type": "number" },
          "x": { "type": "number" }
        }
      }
    },
    "Nested": {
      "type": "object",
      "properties": {
        "outer": {
          "type": "object",
          "properties": {
            "inner": { "type": "string" }
          }
        }
      }
    }
  },
  "entityTypes": {
    "Zulu": {
      "requiredComponents": ["Sprite", "Bound"],
      "optionalComponents": ["Waypoints"],
      "allowExtraComponents": true,
      "validationLevel": "warning"
    },
    "Alpha": {
      "requiredComponents": ["Sprite"],
      "optionalComponents": [],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    }
  }
}
`

func richSession(t *testing.T) (*Session, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(richSchema), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, path
}

// Edit adopts the clone, so anything clone drops is erased from the user's
// file. A no-op edit must therefore change nothing at all — not the value, and
// not the bytes it serialises to.
func TestClone_ANoOpEditChangesNothing(t *testing.T) {
	s, path := richSession(t)

	if err := s.Edit(func(*schema.DatabaseSchema) error { return nil }); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	mustDirty(t, s, false, "after a no-op edit")

	// And the file it would write is byte-identical, which is the property that
	// actually protects the user.
	if err := s.SaveOverwriting(); err != nil {
		t.Fatalf("SaveOverwriting: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if string(after) != richSchema {
		t.Errorf("a no-op edit rewrote the file\n--- want ---\n%s\n--- got ---\n%s", richSchema, after)
	}
}

// Every reachable slot, mutated through the value Read hands out. None of it
// may touch the session.
func TestClone_ReadCannotReachTheSession(t *testing.T) {
	s, _ := richSession(t)

	s.Read(func(d schema.DatabaseSchema) {
		d.ComponentOrder[0] = "CLOBBERED"
		d.EntityTypeOrder[0] = "CLOBBERED"

		sprite := d.Components["Sprite"]
		sprite.PropertyOrder[0] = "CLOBBERED"
		sprite.Properties["sheet"] = schema.Property{Type: "boolean"}

		bound := d.Components["Bound"]
		bound.Properties["zeta"] = schema.Property{Type: "string"}

		way := d.Components["Waypoints"]
		way.Items.Type = "CLOBBERED"
		way.Items.Properties["x"] = schema.Property{Type: "string"}

		nested := d.Components["Nested"]
		nested.Properties["outer"].Properties["inner"] = schema.Property{Type: "boolean"}

		zulu := d.EntityTypes["Zulu"]
		zulu.RequiredComponents[0] = "CLOBBERED"
		zulu.OptionalComponents[0] = "CLOBBERED"

		delete(d.Components, "Sprite")
		delete(d.EntityTypes, "Alpha")
	})

	mustDirty(t, s, false, "after mutating everything Read handed out")

	s.Read(func(d schema.DatabaseSchema) {
		if d.ComponentOrder[0] == "CLOBBERED" || d.EntityTypeOrder[0] == "CLOBBERED" {
			t.Error("an order slice was shared with the session")
		}
		if len(d.Components) != 4 || len(d.EntityTypes) != 2 {
			t.Error("a top-level map was shared with the session")
		}
		if d.Components["Sprite"].PropertyOrder[0] == "CLOBBERED" {
			t.Error("PropertyOrder was shared with the session")
		}
		if d.Components["Sprite"].Properties["sheet"].Type != "string" {
			t.Error("a Properties map was shared with the session")
		}
		if d.Components["Waypoints"].Items.Type != "object" {
			t.Error("Component.Items was shared with the session")
		}
		if d.Components["Waypoints"].Items.Properties["x"].Type != "number" {
			t.Error("a nested Items.Properties map was shared with the session")
		}
		if d.Components["Nested"].Properties["outer"].Properties["inner"].Type != "string" {
			t.Error("a nested Property.Properties map was shared with the session")
		}
		if d.EntityTypes["Zulu"].RequiredComponents[0] == "CLOBBERED" {
			t.Error("RequiredComponents was shared with the session")
		}
		if d.EntityTypes["Zulu"].OptionalComponents[0] == "CLOBBERED" {
			t.Error("OptionalComponents was shared with the session")
		}
	})
}

// Every scalar field must survive a round trip through clone. A struct-literal
// copy drops anything it does not name, and Edit writes that loss to disk.
func TestClone_PreservesEveryValue(t *testing.T) {
	s, _ := richSession(t)

	var before, after schema.DatabaseSchema
	s.Read(func(d schema.DatabaseSchema) { before = d })
	if err := s.Edit(func(*schema.DatabaseSchema) error { return nil }); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	s.Read(func(d schema.DatabaseSchema) { after = d })

	if !reflect.DeepEqual(before, after) {
		t.Errorf("a value did not survive clone\n before: %#v\n after:  %#v", before, after)
	}

	// Named explicitly, because DeepEqual failing tells you *that* something
	// went missing, not what.
	if after.Components["Bound"].Behavior != "wander" {
		t.Error("Component.Behavior was dropped")
	}
	if after.Components["Waypoints"].Items == nil {
		t.Error("Component.Items was dropped")
	}
	if len(after.Components["Sprite"].PropertyOrder) != 3 {
		t.Error("Component.PropertyOrder was dropped")
	}
	if !after.EntityTypes["Zulu"].AllowExtraComponents {
		t.Error("EntityType.AllowExtraComponents was dropped")
	}
	if after.EntityTypes["Zulu"].ValidationLevel != schema.ValidationWarning {
		t.Error("EntityType.ValidationLevel was dropped")
	}
}

// The tripwire. clone copies field by field, so a field added to any of these
// types later is silently dropped — and Edit adopts the clone, so the loss is
// written to the user's file. Nothing else in the repo notices.
//
// If this fails, add the field to clone and then to the list here.
func TestClone_CoversEveryField(t *testing.T) {
	known := map[string][]string{
		"DatabaseSchema": {"SchemaVersion", "Components", "EntityTypes", "ComponentOrder", "EntityTypeOrder"},
		"Component":      {"Type", "Behavior", "Properties", "Items", "PropertyOrder"},
		"Property":       {"Type", "Properties", "Items", "PropertyOrder"},
		"EntityType":     {"Behavior", "RequiredComponents", "OptionalComponents", "AllowExtraComponents", "ValidationLevel"},
	}
	types := map[string]reflect.Type{
		"DatabaseSchema": reflect.TypeOf(schema.DatabaseSchema{}),
		"Component":      reflect.TypeOf(schema.Component{}),
		"Property":       reflect.TypeOf(schema.Property{}),
		"EntityType":     reflect.TypeOf(schema.EntityType{}),
	}

	for name, typ := range types {
		var actual []string
		for i := range typ.NumField() {
			actual = append(actual, typ.Field(i).Name)
		}
		want := known[name]
		if !reflect.DeepEqual(actual, want) {
			t.Errorf("schema.%s has changed shape.\n  now:  %v\n  known: %v\n"+
				"Update clone() in session.go to carry the change, then update this list. "+
				"Edit adopts the clone, so a field clone does not copy is erased from the user's file.",
				name, actual, strings.Join(want, " "))
		}
	}
}
