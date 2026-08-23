package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/session"

	"github.com/tmbritton/ecs-db/internal/jsonorder"
	"github.com/tmbritton/ecs-db/internal/schema"
)

func editFixture() *schema.DatabaseSchema {
	return &schema.DatabaseSchema{
		SchemaVersion: 3,
		Components: map[string]schema.Component{
			"Position": {
				Type:          schema.ComponentTypeObject,
				Properties:    map[string]schema.Property{"x": {Type: "number"}, "y": {Type: "number"}},
				PropertyOrder: []string{"x", "y"},
			},
			"Health": {
				Type:          schema.ComponentTypeObject,
				Properties:    map[string]schema.Property{"hp": {Type: "integer"}},
				PropertyOrder: []string{"hp"},
			},
		},
		ComponentOrder: []string{"Position", "Health"},
		EntityTypes: map[string]schema.EntityType{
			// Both components appear in both lists across the two types, so a
			// test that only handled one of required/optional shows up.
			"Player": {
				RequiredComponents: []string{"Position"},
				OptionalComponents: []string{"Health"},
				ValidationLevel:    schema.ValidationStrict,
			},
			"Goblin": {
				RequiredComponents: []string{"Health"},
				OptionalComponents: []string{"Position"},
				ValidationLevel:    schema.ValidationStrict,
			},
		},
		EntityTypeOrder: []string{"Player", "Goblin"},
	}
}

// mustRoundTrip is the assertion that actually proves an edit is saveable.
//
// ValidateSchema is not enough: component structure is checked in
// Component.UnmarshalJSON, not in ValidateSchema, so a structurally broken
// component passes validation and then fails on load. Session.Edit does not
// validate at all and Save only runs ValidateSchema, so such an edit would be
// written to the user's file and refused the next time anything read it.
func mustRoundTrip(t *testing.T, d *schema.DatabaseSchema) {
	t.Helper()
	out, err := schema.Marshal(*d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if _, err := schema.LoadSchema(out); err != nil {
		t.Fatalf("the edited schema cannot be loaded back: %v\n%s", err, out)
	}
	if err := schema.ValidateSchema(*d); err != nil {
		t.Fatalf("the edited schema does not validate: %v", err)
	}
}

func order(d *schema.DatabaseSchema) []string {
	return jsonorder.Apply(d.ComponentOrder, d.Components)
}

// "Behavior" is reserved, and the editor must say so at the point of typing
// rather than letting it through to be refused on save.
func TestValidComponentName_RejectsTheReservedName(t *testing.T) {
	d := editFixture()
	for _, name := range []string{"Behavior", "behavior", "BEHAVIOR", "BeHaViOr"} {
		t.Run(name, func(t *testing.T) {
			if err := addComponent(d, name); err == nil {
				t.Errorf("addComponent(%q) was accepted; the engine will refuse it", name)
			}
			if err := renameComponent(d, "Health", name); err == nil {
				t.Errorf("renameComponent to %q was accepted", name)
			}
		})
	}
}

// A new component lands after the authored ones, where jsonorder.Apply says it
// will — not sorted in among someone's arrangement.
func TestAddComponent(t *testing.T) {
	d := editFixture()
	// "Alpha", not "Sprite": a name that sorts *first* proves the order slice
	// is being appended to. A name that sorts last comes out in the right place
	// either way, because jsonorder.Apply appends unrecorded keys sorted.
	if err := addComponent(d, "Alpha"); err != nil {
		t.Fatalf("addComponent: %v", err)
	}
	if got := order(d); strings.Join(got, ",") != "Position,Health,Alpha" {
		t.Errorf("order = %v, want the new one appended, not sorted in", got)
	}
	// It must be loadable, not merely valid: component structure is checked in
	// Component.UnmarshalJSON, which ValidateSchema never runs.
	mustRoundTrip(t, d)

	// Adding twice gives two components rather than an error.
	if err := addComponent(d, "Alpha"); err != nil {
		t.Fatalf("second add: %v", err)
	}
	if len(d.Components) != 4 {
		t.Errorf("components = %d, want 4", len(d.Components))
	}
}

// Renaming must keep the component's place and follow every reference, or the
// schema stops validating the moment it is renamed.
func TestRenameComponent(t *testing.T) {
	d := editFixture()
	if err := renameComponent(d, "Position", "Transform"); err != nil {
		t.Fatalf("renameComponent: %v", err)
	}

	if got := order(d); strings.Join(got, ",") != "Transform,Health" {
		t.Errorf("order = %v, want the rename in place, not moved to the end", got)
	}
	if _, gone := d.Components["Position"]; gone {
		t.Error("the old name survived")
	}
	// Both lists, on both types — a rename that handled only one of them left
	// the other dangling.
	if got := d.EntityTypes["Player"].RequiredComponents; strings.Join(got, ",") != "Transform" {
		t.Errorf("Player.requiredComponents = %v, want the reference followed", got)
	}
	if got := d.EntityTypes["Goblin"].OptionalComponents; strings.Join(got, ",") != "Transform" {
		t.Errorf("Goblin.optionalComponents = %v, want the reference followed", got)
	}
	mustRoundTrip(t, d)
}

func TestRenameComponent_RejectsACollision(t *testing.T) {
	d := editFixture()
	if err := renameComponent(d, "Position", "Health"); err == nil {
		t.Error("renaming onto an existing name was accepted")
	}
	if _, ok := d.Components["Position"]; !ok {
		t.Error("the rejected rename still removed the original")
	}
}

// Deleting must not leave an entity type referencing something that is gone.
func TestDeleteComponent(t *testing.T) {
	d := editFixture()
	if err := deleteComponent(d, "Health"); err != nil {
		t.Fatalf("deleteComponent: %v", err)
	}
	if got := d.EntityTypes["Player"].OptionalComponents; len(got) != 0 {
		t.Errorf("Player.optionalComponents = %v, want the reference removed", got)
	}
	if got := d.EntityTypes["Goblin"].RequiredComponents; len(got) != 0 {
		t.Errorf("Goblin.requiredComponents = %v, want the reference removed", got)
	}
	mustRoundTrip(t, d)
}

// Field names become lowercased SQL columns, so maxHp and maxhp are distinct
// JSON keys and the same column. Caught here rather than as a confusing
// duplicate-column error out of SQLite.
func TestAddField_RejectsALowercaseCollision(t *testing.T) {
	d := editFixture()
	if err := addField(d, "Health", "HP"); err == nil {
		t.Error("adding HP beside hp was accepted; they are the same SQL column")
	} else if !strings.Contains(err.Error(), "lowercased") {
		t.Errorf("error = %v, does not explain why", err)
	}
}

func TestFieldEdits(t *testing.T) {
	t.Run("add appends in order", func(t *testing.T) {
		d := editFixture()
		// "a", not "z", for the same reason as Alpha above.
		if err := addField(d, "Position", "a"); err != nil {
			t.Fatalf("addField: %v", err)
		}
		got := jsonorder.Apply(d.Components["Position"].PropertyOrder, d.Components["Position"].Properties)
		if strings.Join(got, ",") != "x,y,a" {
			t.Errorf("fields = %v, want the new one appended, not sorted in", got)
		}
		mustRoundTrip(t, d)
	})

	t.Run("rename keeps its place", func(t *testing.T) {
		d := editFixture()
		if err := renameField(d, "Position", "x", "col"); err != nil {
			t.Fatalf("renameField: %v", err)
		}
		got := jsonorder.Apply(d.Components["Position"].PropertyOrder, d.Components["Position"].Properties)
		if strings.Join(got, ",") != "col,y" {
			t.Errorf("fields = %v, want the rename in place", got)
		}
	})

	t.Run("retype", func(t *testing.T) {
		d := editFixture()
		if err := retypeField(d, "Position", "x", schema.PropertyTypeString); err != nil {
			t.Fatalf("retypeField: %v", err)
		}
		if got := d.Components["Position"].Properties["x"].Type; got != "string" {
			t.Errorf("type = %q, want string", got)
		}
	})

	t.Run("retype to object fills in a shape that validates", func(t *testing.T) {
		d := editFixture()
		if err := retypeField(d, "Position", "x", schema.PropertyTypeObject); err != nil {
			t.Fatalf("retypeField: %v", err)
		}
		if err := schema.ValidateSchema(*d); err != nil {
			t.Errorf("an object field with no properties cannot be saved: %v", err)
		}
	})

	t.Run("retype to array fills in items", func(t *testing.T) {
		d := editFixture()
		if err := retypeField(d, "Position", "x", schema.PropertyTypeArray); err != nil {
			t.Fatalf("retypeField: %v", err)
		}
		if d.Components["Position"].Properties["x"].Items == nil {
			t.Error("an array field with no items cannot be saved")
		}
	})

	t.Run("delete", func(t *testing.T) {
		d := editFixture()
		if err := deleteField(d, "Position", "y"); err != nil {
			t.Fatalf("deleteField: %v", err)
		}
		if _, ok := d.Components["Position"].Properties["y"]; ok {
			t.Error("the field survived")
		}
	})

	// An object component with no properties fails validation, so deleting the
	// last one would produce a schema that can never be saved — better to
	// refuse than to leave someone stuck.
	t.Run("the last field cannot be deleted", func(t *testing.T) {
		d := editFixture()
		if err := deleteField(d, "Health", "hp"); err == nil {
			t.Error("deleting the last field was accepted; the schema could never be saved")
		}
	})
}

// Switching shape must always leave something the engine can load.
func TestSetShape(t *testing.T) {
	for _, shape := range []string{
		schema.ComponentTypeObject, schema.ComponentTypeEntityRef, schema.ComponentTypeArray,
		schema.ComponentTypeString, schema.ComponentTypeInteger,
		schema.ComponentTypeNumber, schema.ComponentTypeBoolean,
	} {
		t.Run(shape, func(t *testing.T) {
			d := editFixture()
			if err := setShape(d, "Position", shape); err != nil {
				t.Fatalf("setShape: %v", err)
			}
			if err := schema.ValidateSchema(*d); err != nil {
				t.Errorf("shape %q left the schema invalid: %v", shape, err)
			}
		})
	}

	t.Run("an unknown shape is refused", func(t *testing.T) {
		d := editFixture()
		if err := setShape(d, "Position", "nonsense"); err == nil {
			t.Error("an unsupported shape was accepted")
		}
	})

	// Leaving object clears the properties, and this is the test that says so
	// rather than an earlier version's claim that they were kept harmlessly.
	//
	// Marshal writes properties and items independently — deliberately, so a
	// component carrying both round-trips — so keeping them wrote dead
	// properties into the user's file beside the items, permanently. Losing
	// the fields on a shape change is the lesser harm and is at least visible.
	t.Run("leaving object clears the fields rather than leaving them in the file", func(t *testing.T) {
		for _, shape := range []string{
			schema.ComponentTypeString, schema.ComponentTypeArray, schema.ComponentTypeEntityRef,
		} {
			t.Run(shape, func(t *testing.T) {
				d := editFixture()
				if err := setShape(d, "Position", shape); err != nil {
					t.Fatalf("setShape: %v", err)
				}
				if len(d.Components["Position"].Properties) != 0 {
					t.Error("properties survived a shape change and will be written to the file")
				}

				out, err := schema.Marshal(*d)
				if err != nil {
					t.Fatalf("Marshal: %v", err)
				}
				if strings.Contains(string(out), `"properties"`) &&
					strings.Contains(string(out), `"type": "`+shape+`"`) {
					// Only Health should still have properties.
					if strings.Count(string(out), `"properties"`) > 1 {
						t.Errorf("dead properties were written for a %s component:\n%s", shape, out)
					}
				}
			})
		}
	})

	// The shape actually changes — an implementation that validated but never
	// assigned would pass a test that only checks ValidateSchema.
	t.Run("the shape is actually applied", func(t *testing.T) {
		d := editFixture()
		for _, shape := range []string{
			schema.ComponentTypeString, schema.ComponentTypeArray, schema.ComponentTypeObject,
		} {
			if err := setShape(d, "Position", shape); err != nil {
				t.Fatalf("setShape(%s): %v", shape, err)
			}
			if got := d.Components["Position"].Type; got != shape {
				t.Errorf("Type = %q after setting %q", got, shape)
			}
		}
	})

	// An array needs items and an object needs properties, or the component
	// cannot be loaded back — and Session.Edit does not validate, so an edit
	// that produced one would be written and then refused on load.
	t.Run("every shape round-trips through the real serialiser", func(t *testing.T) {
		for _, shape := range []string{
			schema.ComponentTypeObject, schema.ComponentTypeEntityRef, schema.ComponentTypeArray,
			schema.ComponentTypeString, schema.ComponentTypeInteger,
			schema.ComponentTypeNumber, schema.ComponentTypeBoolean,
		} {
			t.Run(shape, func(t *testing.T) {
				d := editFixture()
				if err := setShape(d, "Position", shape); err != nil {
					t.Fatalf("setShape: %v", err)
				}
				mustRoundTrip(t, d)
			})
		}
	})
}

// Every edit must name what it could not find, rather than silently doing
// nothing.
func TestEdits_RejectAnUnknownTarget(t *testing.T) {
	d := editFixture()
	cases := map[string]error{
		"rename component": renameComponent(d, "Nope", "X"),
		"delete component": deleteComponent(d, "Nope"),
		"set shape":        setShape(d, "Nope", schema.ComponentTypeObject),
		"add field":        addField(d, "Nope", "f"),
		"rename field":     renameField(d, "Position", "nope", "f"),
		"retype field":     retypeField(d, "Position", "nope", "string"),
		"delete field":     deleteField(d, "Position", "nope"),
	}
	for name, err := range cases {
		if err == nil {
			t.Errorf("%s: no error for a target that does not exist", name)
		}
	}
}

// Names become SQL identifiers, concatenated into DDL with no quoting. This is
// the first UI that invites typing one.
func TestNames_MustBeSQLIdentifiers(t *testing.T) {
	bad := []string{
		"has space", "has-dash", "has.dot", "1leading", "quote'", `double"`,
		"back\\slash", "amp&", "hash#", "percent%", "semi;colon", "paren(",
		"unicodeé", "new\nline", "",
	}
	for _, name := range bad {
		t.Run("component "+name, func(t *testing.T) {
			d := editFixture()
			if err := addComponent(d, name); err == nil {
				t.Errorf("addComponent(%q) was accepted; it becomes a raw SQL table name", name)
			}
			if err := renameComponent(d, "Health", name); err == nil {
				t.Errorf("renameComponent to %q was accepted", name)
			}
		})
		t.Run("field "+name, func(t *testing.T) {
			d := editFixture()
			if err := addField(d, "Position", name); err == nil {
				t.Errorf("addField(%q) was accepted; it becomes a raw SQL column name", name)
			}
			if err := renameField(d, "Position", "x", name); err == nil {
				t.Errorf("renameField to %q was accepted", name)
			}
		})
	}

	for _, name := range []string{"Good", "_leading", "with_underscore", "trailing9"} {
		t.Run("accepted "+name, func(t *testing.T) {
			d := editFixture()
			if err := addComponent(d, name); err != nil {
				t.Errorf("addComponent(%q) was refused: %v", name, err)
			}
		})
	}
}

// Table names are comp_ + strings.ToLower(name), so Health and health are
// distinct JSON keys and the same table — the second CREATE TABLE IF NOT EXISTS
// silently does nothing and the two components share one.
func TestComponentNames_RejectACaseCollision(t *testing.T) {
	d := editFixture()
	for _, name := range []string{"health", "HEALTH", "HeAlTh"} {
		t.Run(name, func(t *testing.T) {
			if err := addComponent(d, name); err == nil {
				t.Errorf("addComponent(%q) beside Health was accepted; same SQL table", name)
			} else if !strings.Contains(err.Error(), "collides") {
				t.Errorf("error = %v, does not explain why", err)
			}
			if err := renameComponent(d, "Position", name); err == nil {
				t.Errorf("renameComponent to %q was accepted", name)
			}
		})
	}
}

// A schema with no components cannot be saved, so deleting the last one would
// leave someone stuck — the same reasoning deleteField already applies.
func TestDeleteComponent_RefusesTheLastOne(t *testing.T) {
	d := editFixture()
	if err := deleteComponent(d, "Health"); err != nil {
		t.Fatalf("deleteComponent: %v", err)
	}
	if err := deleteComponent(d, "Position"); err == nil {
		t.Error("deleting the last component was accepted; the schema could never be saved")
	}
}

// Renaming to the same name is a no-op, not an error — a change event fires
// whenever the input loses focus, whether or not the text changed.
func TestRenames_ToTheSameNameAreNoOps(t *testing.T) {
	d := editFixture()
	if err := renameComponent(d, "Health", "Health"); err != nil {
		t.Errorf("renaming a component to its own name errored: %v", err)
	}
	if _, ok := d.Components["Health"]; !ok {
		t.Error("a no-op rename removed the component")
	}
	if err := renameField(d, "Position", "x", "x"); err != nil {
		t.Errorf("renaming a field to its own name errored: %v", err)
	}
	if _, ok := d.Components["Position"].Properties["x"]; !ok {
		t.Error("a no-op rename removed the field")
	}
}

// Renaming a field to a name that differs only in case is the same collision
// as adding one, and the field being renamed must not block itself.
func TestRenameField_CollisionRules(t *testing.T) {
	d := editFixture()
	if err := renameField(d, "Position", "x", "Y"); err == nil {
		t.Error("renaming x to Y beside y was accepted; same SQL column")
	}
	// Changing the case of the field's own name is allowed: it is the same
	// column either way, and refusing would make the field unrenameable.
	if err := renameField(d, "Position", "x", "X"); err != nil {
		t.Errorf("renaming a field to its own name in different case was refused: %v", err)
	}
}

// retypeField must refuse a type the engine has no notion of, rather than
// writing it and failing on load.
func TestRetypeField_RefusesAnUnknownType(t *testing.T) {
	d := editFixture()
	if err := retypeField(d, "Position", "x", "nonsense"); err == nil {
		t.Error("an unsupported property type was accepted")
	}
	mustRoundTrip(t, d)
}

// uniqueName is what makes clicking "add" twice produce two things.
func TestUniqueName(t *testing.T) {
	taken := map[string]bool{"a": true, "a2": true}
	if got := uniqueName("a", func(n string) bool { return taken[n] }); got != "a3" {
		t.Errorf("uniqueName = %q, want a3", got)
	}
	if got := uniqueName("free", func(n string) bool { return taken[n] }); got != "free" {
		t.Errorf("uniqueName = %q, want the name unchanged when it is free", got)
	}
}

// The last field of a non-object component is not special: it has no fields at
// all, so the guard must not fire on a shape that never had any.
func TestDeleteField_LastFieldGuardIsForObjectsOnly(t *testing.T) {
	d := editFixture()
	if err := setShape(d, "Health", schema.ComponentTypeString); err != nil {
		t.Fatalf("setShape: %v", err)
	}
	// Health now has no properties, so there is nothing to delete and the
	// error should say that rather than the last-field message.
	err := deleteField(d, "Health", "hp")
	if err == nil {
		t.Fatal("deleting a field from a shapeless component was accepted")
	}
	if strings.Contains(err.Error(), "last field") {
		t.Errorf("error = %v, want it to say the field does not exist", err)
	}
}

// ── the handlers, which had almost no coverage of their own ──────────────────

func editServer(t *testing.T) (*httptest.Server, *Server, *session.Session) {
	t.Helper()
	srv, s, sess, _ := sessionServer(t)
	return srv, s, sess
}

func postTo(t *testing.T, srv *httptest.Server, path string) int {
	t.Helper()
	resp, err := srv.Client().Post(srv.URL+path, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// The arguments have to reach the edit. A handler that ignored them would
// silently do the wrong thing, or nothing.
func TestEditHandlers_UseTheirArguments(t *testing.T) {
	t.Run("shape", func(t *testing.T) {
		srv, _, sess := editServer(t)
		if code := postTo(t, srv, "/forge/schema/shape?component=Position&shape=string"); code != 204 {
			t.Fatalf("status = %d", code)
		}
		var got string
		sess.Read(func(d schema.DatabaseSchema) { got = d.Components["Position"].Type })
		if got != "string" {
			t.Errorf("Type = %q, want string — the shape argument was ignored", got)
		}
	})

	t.Run("rename", func(t *testing.T) {
		srv, _, sess := editServer(t)
		if code := postTo(t, srv, "/forge/schema/component?rename=Position&to=Placement"); code != 204 {
			t.Fatalf("status = %d", code)
		}
		var ok bool
		sess.Read(func(d schema.DatabaseSchema) { _, ok = d.Components["Placement"] })
		if !ok {
			t.Error("the rename target was ignored")
		}
	})

	t.Run("field retype", func(t *testing.T) {
		srv, _, sess := editServer(t)
		if code := postTo(t, srv, "/forge/schema/field?component=Position&retype=x&type=boolean"); code != 204 {
			t.Fatalf("status = %d", code)
		}
		var got string
		sess.Read(func(d schema.DatabaseSchema) { got = d.Components["Position"].Properties["x"].Type })
		if got != "boolean" {
			t.Errorf("field type = %q, want boolean — the type argument was ignored", got)
		}
	})

	t.Run("behavior", func(t *testing.T) {
		srv, _, sess := editServer(t)
		if code := postTo(t, srv, "/forge/schema/behavior?component=Position&behavior=wander"); code != 204 {
			t.Fatalf("status = %d", code)
		}
		var got string
		sess.Read(func(d schema.DatabaseSchema) { got = d.Components["Position"].Behavior })
		if got != "wander" {
			t.Errorf("Behavior = %q, want wander", got)
		}
	})
}

// A request naming no action is a bug in the caller, not a silent no-op.
func TestEditHandlers_RejectAMalformedRequest(t *testing.T) {
	srv, _, _ := editServer(t)
	for _, path := range []string{"/forge/schema/component", "/forge/schema/field?component=Position"} {
		if code := postTo(t, srv, path); code != http.StatusBadRequest {
			t.Errorf("POST %s = %d, want 400", path, code)
		}
	}
}

// With no project open the edits must answer rather than panic.
func TestEditHandlers_WithNoSession(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)

	for _, path := range []string{
		"/forge/schema/version",
		"/forge/schema/component?add=X",
		"/forge/schema/shape?component=X&shape=string",
		"/forge/schema/behavior?component=X&behavior=y",
		"/forge/schema/field?component=X&add=f",
	} {
		if code := postTo(t, srv, path); code != http.StatusConflict {
			t.Errorf("POST %s = %d, want 409", path, code)
		}
	}
}

// A refused edit is recorded so the mode can show why, and cleared by the next
// one that works — a stale explanation must not outlive the state it described.
func TestEditHandlers_RecordAndClearTheProblem(t *testing.T) {
	srv, s, _ := editServer(t)

	if code := postTo(t, srv, "/forge/schema/component?add=Behavior"); code != 204 {
		t.Fatalf("status = %d, want 204 — the outcome travels on the stream", code)
	}
	if got := problemOf(s); !strings.Contains(got, "reserved") {
		t.Errorf("problem = %q, want the reason recorded", got)
	}

	if code := postTo(t, srv, "/forge/schema/version"); code != 204 {
		t.Fatalf("status = %d", code)
	}
	if got := problemOf(s); got != "" {
		t.Errorf("problem = %q, want it cleared by a successful edit", got)
	}
}

// A full page load starts clean: an edit refused in another tab is not this
// page's problem to report.
func TestModePage_ClearsAStaleEditProblem(t *testing.T) {
	srv, s, _ := editServer(t)
	if code := postTo(t, srv, "/forge/schema/component?add=Behavior"); code != 204 {
		t.Fatalf("status = %d", code)
	}
	if problemOf(s) == "" {
		t.Fatal("the fixture did not record a problem")
	}
	get(t, srv, "/forge/schema")
	if got := problemOf(s); got != "" {
		t.Errorf("problem = %q, want a page load to start clean", got)
	}
}

// A page subscribes to its stream naming the component it shows, and that URL
// cannot change afterwards. Renaming left the stream asking for a name that no
// longer existed, the editor silently swapped to whichever component sorted
// first, and the next edit hit *that* one.
func TestRename_KeepsThePageOnTheRenamedComponent(t *testing.T) {
	srv, s, _ := editServer(t)

	// A second component, so the one being renamed is deliberately *not* the
	// first — the fallback lands on the first, so renaming that one would hide
	// the bug entirely, which is exactly why the original test missed it.
	if code := postTo(t, srv, "/forge/schema/component?add=Extra"); code != 204 {
		t.Fatalf("add: %d", code)
	}
	if code := postTo(t, srv, "/forge/schema/component?rename=Extra&to=Vitality"); code != 204 {
		t.Fatalf("status = %d", code)
	}

	req := httptest.NewRequest(http.MethodGet, "/forge/schema?component=Extra", nil)
	data := s.modeData(req)
	if data.Selected != "Vitality" {
		t.Errorf("a page showing Health now selects %q — an edit from it would hit the wrong component",
			data.Selected)
	}

	// And a chain of renames follows all the way.
	if code := postTo(t, srv, "/forge/schema/component?rename=Vitality&to=Hitpoints"); code != 204 {
		t.Fatalf("status = %d", code)
	}
	if got := s.modeData(req).Selected; got != "Hitpoints" {
		t.Errorf("after a second rename the page selects %q, want Hitpoints", got)
	}
}
