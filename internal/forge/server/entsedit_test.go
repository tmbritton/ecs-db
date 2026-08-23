package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/session"
	"github.com/tmbritton/ecs-db/internal/forge/status"
	"github.com/tmbritton/ecs-db/internal/jsonorder"
	"github.com/tmbritton/ecs-db/internal/schema"
)

func entsSchema() *schema.DatabaseSchema {
	return &schema.DatabaseSchema{
		SchemaVersion: 3,
		// Each carries a property: an object component with none does not
		// validate, so a fixture without them cannot be opened as a session.
		Components: map[string]schema.Component{
			"Position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{"x": {Type: "number"}}},
			"Health":   {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{"hp": {Type: "integer"}}},
			"Sprite":   {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{"key": {Type: "string"}}},
		},
		ComponentOrder: []string{"Position", "Health", "Sprite"},
		EntityTypes: map[string]schema.EntityType{
			"Player": {
				RequiredComponents: []string{"Position"},
				OptionalComponents: []string{"Health"},
				ValidationLevel:    schema.ValidationStrict,
			},
		},
		EntityTypeOrder: []string{"Player"},
	}
}

// A component cannot be both required and optional. The engine's validator
// treats that as a contradiction, so moving one list to the other has to take
// it out of the first rather than adding it to both.
func TestAttach_AComponentIsNeverInBothLists(t *testing.T) {
	d := entsSchema()
	et := d.EntityTypes["Player"]

	if err := attachRequired(d, &et, "Health"); err != nil {
		t.Fatalf("requiring an optional component: %v", err)
	}
	if contains(et.OptionalComponents, "Health") {
		t.Error("promoting left the component optional as well as required")
	}
	if !contains(et.RequiredComponents, "Health") {
		t.Error("promoting did not make the component required")
	}

	if err := attachOptional(d, &et, "Health"); err != nil {
		t.Fatalf("making it optional again: %v", err)
	}
	if contains(et.RequiredComponents, "Health") {
		t.Error("demoting left the component required as well as optional")
	}
}

// An entity type may only name components the schema declares — otherwise it
// is a contract referring to something that does not exist.
func TestAttach_RefusesAComponentThatDoesNotExist(t *testing.T) {
	d := entsSchema()
	et := d.EntityTypes["Player"]

	if err := attachRequired(d, &et, "NoSuchComponent"); err == nil {
		t.Error("a type was allowed to require a component the schema does not declare")
	}
	if err := attachOptional(d, &et, "NoSuchComponent"); err == nil {
		t.Error("a type was allowed to optionally name a component that does not exist")
	}
}

// The lock is the contract. Detaching a required component is refused, and the
// refusal says how to do what the user meant.
func TestDetach_RefusesARequiredComponent(t *testing.T) {
	d := entsSchema()
	et := d.EntityTypes["Player"]

	err := detachComponent(d, &et, "Position")
	if err == nil {
		t.Fatal("a required component was detached")
	}
	if !strings.Contains(err.Error(), "optional") {
		t.Errorf("the refusal does not say how to remove it: %v", err)
	}
	if !contains(et.RequiredComponents, "Position") {
		t.Error("the refused detach changed the type anyway")
	}

	if err := detachComponent(d, &et, "Health"); err != nil {
		t.Fatalf("detaching an optional component: %v", err)
	}
	if contains(et.OptionalComponents, "Health") {
		t.Error("detaching an optional component did nothing")
	}
}

// A new type must serialise as the format says. EntityType carries omitempty
// only on Behavior, so nil slices become `null` where the file wants `[]`.
func TestAddEntityType_SerialisesEmptyListsAsArrays(t *testing.T) {
	d := entsSchema()
	if err := addEntityType(d, "Ghost"); err != nil {
		t.Fatalf("adding: %v", err)
	}

	// Through schema.Marshal, which is what writes the file. encoding/json is
	// a different serialiser with different rules, and asserting against it
	// measures nothing the user ever sees.
	raw, err := schema.Marshal(*d)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if strings.Contains(string(raw), "null") {
		t.Errorf("a new type serialises a null list, which the engine's format does not use: %s", raw)
	}
	if !strings.Contains(string(raw), `"requiredComponents": []`) {
		t.Errorf("a new type's empty list is not written as []: %s", raw)
	}
	if d.EntityTypes["Ghost"].ValidationLevel != schema.ValidationStrict {
		t.Error("a new type did not default to strict")
	}
	// Authored order records the new type, or a save would drop it to the end
	// of a sorted list.
	if len(d.EntityTypeOrder) == 0 || d.EntityTypeOrder[len(d.EntityTypeOrder)-1] != "Ghost" {
		t.Errorf("the new type is not in the authored order: %v", d.EntityTypeOrder)
	}
}

// Removing the last entry must leave an empty list, not a nil one, for the
// same reason.
func TestDetach_LeavesAnEmptyListNotNull(t *testing.T) {
	d := entsSchema()
	et := d.EntityTypes["Player"]

	if err := detachComponent(d, &et, "Health"); err != nil {
		t.Fatalf("detaching: %v", err)
	}
	d.EntityTypes["Player"] = et
	raw, err := schema.Marshal(*d)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if strings.Contains(string(raw), "null") {
		t.Errorf("emptying the list produced null rather than []: %s", raw)
	}
}

func TestAddEntityType_DoesNotCollide(t *testing.T) {
	d := entsSchema()
	if err := addEntityType(d, "Player"); err != nil {
		t.Fatalf("adding: %v", err)
	}
	if len(d.EntityTypes) != 2 {
		t.Fatalf("adding a colliding name produced %d types", len(d.EntityTypes))
	}
	if _, ok := d.EntityTypes["Player2"]; !ok {
		t.Errorf("expected Player2 alongside Player, got %v", d.EntityTypeOrder)
	}
}

func TestRenameEntityType(t *testing.T) {
	d := entsSchema()
	if err := renameEntityType(d, "Player", "Hero"); err != nil {
		t.Fatalf("renaming: %v", err)
	}
	if _, ok := d.EntityTypes["Player"]; ok {
		t.Error("the old name survived the rename")
	}
	if d.EntityTypes["Hero"].RequiredComponents == nil {
		t.Error("the rename lost the type's contract")
	}
	if d.EntityTypeOrder[0] != "Hero" {
		t.Errorf("authored order still names the old type: %v", d.EntityTypeOrder)
	}

	if err := renameEntityType(d, "Hero", ""); err == nil {
		t.Error("a type was renamed to nothing")
	}
	d.EntityTypes["Other"] = schema.EntityType{}
	if err := renameEntityType(d, "Hero", "Other"); err == nil {
		t.Error("a type was renamed onto an existing one")
	}
}

func TestSetValidationLevel(t *testing.T) {
	et := schema.EntityType{ValidationLevel: schema.ValidationStrict}

	if err := setValidationLevel(&et, "warning"); err != nil {
		t.Fatalf("setting warning: %v", err)
	}
	if et.ValidationLevel != schema.ValidationWarning {
		t.Error("the level did not change")
	}
	// A level the engine does not know would load as an unenforced contract.
	if err := setValidationLevel(&et, "lenient"); err == nil {
		t.Error("an unknown validation level was accepted")
	}
	if et.ValidationLevel != schema.ValidationWarning {
		t.Error("the refused change was applied anyway")
	}
}

// EntityType is a map value. An edit that forgets to write it back changes
// nothing at all, silently.
func TestUpdateEntityType_WritesTheChangeBack(t *testing.T) {
	d := entsSchema()
	err := updateEntityType(d, "Player", func(et *schema.EntityType) error {
		et.AllowExtraComponents = true
		return nil
	})
	if err != nil {
		t.Fatalf("updating: %v", err)
	}
	if !d.EntityTypes["Player"].AllowExtraComponents {
		t.Error("the change was made to a copy and thrown away")
	}

	// A refused change writes nothing back.
	_ = updateEntityType(d, "Player", func(et *schema.EntityType) error {
		et.AllowExtraComponents = false
		return errSentinel
	})
	if !d.EntityTypes["Player"].AllowExtraComponents {
		t.Error("a refused edit was applied anyway")
	}

	if err := updateEntityType(d, "NoSuchType", func(*schema.EntityType) error { return nil }); err == nil {
		t.Error("editing a type that does not exist was accepted")
	}
}

var errSentinel = &sentinelError{}

type sentinelError struct{}

func (*sentinelError) Error() string { return "refused" }

// removeFrom must not write through the caller's backing array. The previous
// implementation filtered in place, so a caller holding the original saw it
// rewritten underneath.
func TestRemoveFrom_DoesNotDisturbTheCaller(t *testing.T) {
	original := []string{"a", "b", "c"}
	got := removeFrom(original, "b")

	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("removeFrom returned %v", got)
	}
	if len(original) != 3 || original[1] != "b" {
		t.Errorf("removeFrom rewrote the caller's slice: %v", original)
	}
	if removeFrom(nil, "x") == nil {
		t.Error("removeFrom returned nil, which serialises as null rather than []")
	}
}

// The routes reach the session. Every one of these once existed as a handler
// that a template pointed at with the wrong parameter name, which answers 204
// and changes nothing — the failure this project has shipped more than once.
func TestEntsRoutes_ReachTheSession(t *testing.T) {
	tests := []struct {
		name string
		path string
		want func(schema.EntityType) bool
	}{
		{
			name: "bind a behaviour",
			path: "/forge/ents/behavior?type=Player&behavior=wander",
			want: func(et schema.EntityType) bool { return et.Behavior == "wander" },
		},
		{
			name: "set the validation level",
			path: "/forge/ents/validation?type=Player&level=warning",
			want: func(et schema.EntityType) bool { return et.ValidationLevel == schema.ValidationWarning },
		},
		{
			name: "toggle allow-extras",
			path: "/forge/ents/extras?type=Player&toggle=1",
			want: func(et schema.EntityType) bool { return et.AllowExtraComponents },
		},
		{
			name: "require a component",
			path: "/forge/ents/component?type=Player&require=Health",
			want: func(et schema.EntityType) bool { return contains(et.RequiredComponents, "Health") },
		},
		{
			name: "allow a component",
			path: "/forge/ents/component?type=Player&optional=Sprite",
			want: func(et schema.EntityType) bool { return contains(et.OptionalComponents, "Sprite") },
		},
		{
			name: "detach an optional component",
			path: "/forge/ents/component?type=Player&detach=Health",
			want: func(et schema.EntityType) bool { return !contains(et.OptionalComponents, "Health") },
		},
		{
			name: "demote a required component",
			path: "/forge/ents/component?type=Player&demote=Position",
			want: func(et schema.EntityType) bool { return contains(et.OptionalComponents, "Position") },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, sess, _ := entsServer(t)
			if code := post(t, srv, tc.path); code != 204 {
				t.Fatalf("POST %s: %d", tc.path, code)
			}
			var got schema.EntityType
			sess.Read(func(d schema.DatabaseSchema) { got = d.EntityTypes["Player"] })
			if !tc.want(got) {
				t.Errorf("the session did not change: %+v", got)
			}
		})
	}
}

// The add dropdowns start on a placeholder. Choosing it is not a choice, and
// must not put an error on screen for someone who did nothing.
func TestEntsRoutes_ThePlaceholderIsNotAnEdit(t *testing.T) {
	srv, s, sess, _ := entsServer(t)
	var before schema.EntityType
	sess.Read(func(d schema.DatabaseSchema) { before = d.EntityTypes["Player"] })

	if code := post(t, srv, "/forge/ents/component?type=Player&require="); code != 204 {
		t.Fatalf("choosing the placeholder: %d", code)
	}
	if problemOf(s) != "" {
		t.Errorf("the placeholder reported a problem: %q", problemOf(s))
	}
	var after schema.EntityType
	sess.Read(func(d schema.DatabaseSchema) { after = d.EntityTypes["Player"] })
	if len(after.RequiredComponents) != len(before.RequiredComponents) {
		t.Error("the placeholder changed the type")
	}
}

// Detaching a required component is refused, and the reason reaches the page
// rather than the request failing silently.
func TestEntsRoutes_ARefusedDetachExplainsItself(t *testing.T) {
	srv, s, sess, _ := entsServer(t)

	if code := post(t, srv, "/forge/ents/component?type=Player&detach=Position"); code != 204 {
		t.Fatalf("detaching a required component: %d", code)
	}
	if problemOf(s) == "" {
		t.Error("a refused detach left nothing on screen to explain it")
	}
	var got schema.EntityType
	sess.Read(func(d schema.DatabaseSchema) { got = d.EntityTypes["Player"] })
	if !contains(got.RequiredComponents, "Position") {
		t.Error("the refused detach changed the type anyway")
	}
}

func entsServer(t *testing.T) (*httptest.Server, *Server, *session.Session, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.json")
	raw, err := json.MarshalIndent(entsSchema(), "", "  ")
	if err != nil {
		t.Fatalf("building the fixture: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	return sessionServerAt(t, path, status.Config{})
}

// The last entity type cannot be deleted: the engine requires one, so the
// session would enter a state that cannot be saved, and the reason would
// arrive later as a failed save rather than beside the button that caused it.
func TestDeleteEntityType_RefusesTheLastOne(t *testing.T) {
	d := entsSchema()
	if err := deleteEntityType(d, "Player"); err == nil {
		t.Fatal("the last entity type was deleted")
	}
	if _, ok := d.EntityTypes["Player"]; !ok {
		t.Error("the refused delete removed it anyway")
	}

	if err := addEntityType(d, "Ghost"); err != nil {
		t.Fatalf("adding: %v", err)
	}
	if err := deleteEntityType(d, "Player"); err != nil {
		t.Fatalf("deleting one of two: %v", err)
	}
	if _, ok := d.EntityTypes["Player"]; ok {
		t.Error("the delete did nothing")
	}
}

// A type deleted and re-added keeps its place, the same property
// deleteComponent maintains — otherwise a delete-and-undo is a whole-file diff.
func TestDeleteEntityType_KeepsAuthoredOrder(t *testing.T) {
	d := entsSchema()
	if err := addEntityType(d, "Ghost"); err != nil {
		t.Fatalf("adding Ghost: %v", err)
	}
	if err := addEntityType(d, "Wraith"); err != nil {
		t.Fatalf("adding Wraith: %v", err)
	}

	if err := deleteEntityType(d, "Ghost"); err != nil {
		t.Fatalf("deleting: %v", err)
	}
	if err := addEntityType(d, "Ghost"); err != nil {
		t.Fatalf("re-adding: %v", err)
	}

	order := jsonorder.Apply(d.EntityTypeOrder, d.EntityTypes)
	want := []string{"Player", "Ghost", "Wraith"}
	if len(order) != len(want) {
		t.Fatalf("order is %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("a type deleted and re-added moved to the end: %v, want %v", order, want)
		}
	}
}

// The three type-level actions, which had no handler coverage at all.
func TestEntsRoutes_AddRenameAndDelete(t *testing.T) {
	srv, _, sess, _ := entsServer(t)

	if code := post(t, srv, "/forge/ents/type?add=Ghost"); code != 204 {
		t.Fatalf("add: %d", code)
	}
	if !hasType(sess, "Ghost") {
		t.Fatal("the add did not reach the session")
	}

	if code := post(t, srv, "/forge/ents/type?rename=Ghost&to=Wraith"); code != 204 {
		t.Fatalf("rename: %d", code)
	}
	if hasType(sess, "Ghost") || !hasType(sess, "Wraith") {
		t.Fatal("the rename did not reach the session")
	}

	if code := post(t, srv, "/forge/ents/type?delete=Wraith"); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if hasType(sess, "Wraith") {
		t.Fatal("the delete did not reach the session")
	}
}

// A rename follows the page to the new name. Components and entity types are
// separate namespaces that may share a name, so the trail for one must not
// move the other — a tag component "Player" beside an entity type "Player" is
// an ordinary ECS idiom.
func TestRenames_DoNotCrossBetweenComponentsAndTypes(t *testing.T) {
	srv, s, _, _ := entsServer(t)

	// The fixture has a component and an entity type that do not collide, so
	// make them collide first: rename the component Sprite to Player.
	if code := post(t, srv, "/forge/schema/component?rename=Sprite&to=PlayerTag"); code != 204 {
		t.Fatalf("renaming the component: %d", code)
	}

	// A page showing entity type "Sprite" — a name the component rename now
	// has a trail for — must not be dragged to "PlayerTag".
	if got := s.followRenames("Sprite", renameTypeKind); got != "Sprite" {
		t.Errorf("a component rename retargeted the entity-type editor: Sprite became %q", got)
	}
	if got := s.followRenames("Sprite", renameComponentKind); got != "PlayerTag" {
		t.Errorf("the component rename was not followed: Sprite became %q", got)
	}

	// And the reverse.
	if code := post(t, srv, "/forge/ents/type?rename=Player&to=Hero"); code != 204 {
		t.Fatalf("renaming the type: %d", code)
	}
	if got := s.followRenames("Player", renameComponentKind); got != "Player" {
		t.Errorf("an entity-type rename retargeted the component editor: Player became %q", got)
	}
	if got := s.followRenames("Player", renameTypeKind); got != "Hero" {
		t.Errorf("the entity-type rename was not followed: Player became %q", got)
	}
}

// Taking what is on disk undoes the renames, so a trail still pointing at the
// new names sends every page to a name that no longer exists — and from there
// to whatever sorts first.
func TestRenames_AreForgottenWhenTheFileIsReloaded(t *testing.T) {
	srv, s, _, _ := entsServer(t)

	if code := post(t, srv, "/forge/ents/type?rename=Player&to=Hero"); code != 204 {
		t.Fatalf("renaming: %d", code)
	}
	if code := post(t, srv, "/forge/schema/reload"); code != 204 {
		t.Fatalf("reloading: %d", code)
	}

	if got := s.followRenames("Player", renameTypeKind); got != "Player" {
		t.Errorf("the rename trail outlived the rename: Player became %q", got)
	}
}

func hasType(sess *session.Session, name string) bool {
	var found bool
	sess.Read(func(d schema.DatabaseSchema) { _, found = d.EntityTypes[name] })
	return found
}

// A binding that no longer resolves is kept, because dropping it would be an
// edit nobody asked for. A binding shaped like a path is not: the engine turns
// the id into a filename.
func TestSetBehavior(t *testing.T) {
	et := schema.EntityType{}

	if err := setBehavior(&et, "no-such-machine"); err != nil {
		t.Errorf("a binding that does not resolve was refused: %v", err)
	}
	if et.Behavior != "no-such-machine" {
		t.Error("the binding was not kept")
	}
	if err := setBehavior(&et, ""); err != nil {
		t.Errorf("unbinding was refused: %v", err)
	}

	for _, bad := range []string{"../secrets", "a/b", `a\b`} {
		et.Behavior = "wander"
		if err := setBehavior(&et, bad); err == nil {
			t.Errorf("%q was accepted as a machine id", bad)
		}
		if et.Behavior != "wander" {
			t.Errorf("the refused binding was applied anyway: %q", et.Behavior)
		}
	}
}
