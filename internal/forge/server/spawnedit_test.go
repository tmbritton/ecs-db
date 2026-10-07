package server

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func spawnObjects(t *testing.T, s *Server, path string) []tiled.Object {
	t.Helper()
	var out []tiled.Object
	if err := s.cfg.MapSession.Read(path, func(d *tiled.Document) error {
		m, err := d.Map()
		if err == nil {
			out = append(out, m.ObjectGroups[0].Objects...)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSpawnRoutes_PlaceMoveDeleteAndSave(t *testing.T) {
	srv, s, sess, path := mapServer(t)
	base := "?map=" + url.QueryEscape(path)
	if code := postSignals(t, srv, "/forge/map/spawn/place"+base+"&group=0&type=Player&x=1&y=0", `{}`); code != 204 {
		t.Fatalf("place = %d (%s)", code, problemOf(s))
	}
	first := spawnObjects(t, s, path)
	if len(first) != 1 || first[0].ID != 1 || first[0].Type != "Player" || first[0].X != 16 || first[0].Y != 0 {
		t.Fatalf("placed %+v", first)
	}
	if code := postSignals(t, srv, "/forge/map/spawn/move"+base+"&id=1&x=0&y=1", `{}`); code != 204 {
		t.Fatalf("move = %d (%s)", code, problemOf(s))
	}
	moved := spawnObjects(t, s, path)
	if len(moved) != 1 || moved[0].ID != 1 || moved[0].X != 0 || moved[0].Y != 16 {
		t.Errorf("moved %+v", moved)
	}
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := sess.Reload(path); err != nil {
		t.Fatal(err)
	}
	if got := spawnObjects(t, s, path); len(got) != 1 || got[0].ID != 1 || got[0].Y != 16 {
		t.Errorf("saved spawn did not round-trip: %+v", got)
	}
	if code := postSignals(t, srv, "/forge/map/spawn/delete"+base+"&id=1", `{}`); code != 204 {
		t.Fatalf("delete = %d (%s)", code, problemOf(s))
	}
	if code := postSignals(t, srv, "/forge/map/spawn/place"+base+"&group=0&type=Player&x=0&y=0", `{}`); code != 204 {
		t.Fatalf("second place = %d (%s)", code, problemOf(s))
	}
	if got := spawnObjects(t, s, path); len(got) != 1 || got[0].ID != 2 {
		t.Errorf("deleted id was reused: %+v", got)
	}
}

func TestSpawnRoutes_RefusalsLeaveTheMapUntouched(t *testing.T) {
	for _, tc := range []struct {
		name, route, reason string
	}{
		{"unknown type", "/place?group=0&type=Ghost&x=0&y=0", "entity type"},
		{"Tile", "/place?group=0&type=Tile&x=0&y=0", "entity type"},
		{"wrong group", "/place?group=9&type=Player&x=0&y=0", "group"},
		{"outside map", "/place?group=0&type=Player&x=2&y=0", "outside"},
		{"bad cell", "/place?group=0&type=Player&x=oops&y=0", "x"},
		{"missing id", "/move?id=77&x=0&y=0", "no spawn"},
		{"delete missing", "/delete?id=77", "no spawn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, s, _, path := mapServer(t)
			code := postSignals(t, srv, "/forge/map/spawn"+tc.route+"&map="+url.QueryEscape(path), `{}`)
			if code != 204 {
				t.Fatalf("route = %d", code)
			}
			if reason := problemOf(s); !strings.Contains(reason, tc.reason) {
				t.Errorf("refusal %q should say %q", reason, tc.reason)
			}
			if got := spawnObjects(t, s, path); len(got) != 0 {
				t.Errorf("refused edit changed map: %+v", got)
			}
		})
	}
}

func TestSpawnPage_OffersTypesGroupsAndSelectedObject(t *testing.T) {
	srv, _, _, path := mapServer(t)
	initial := mapPage(t, srv, "/forge/map")
	for _, id := range []string{"spawn-type-Player", "object-group-spawns", "spawn-canvas"} {
		if !strings.Contains(initial, `data-testid="`+id+`"`) {
			t.Errorf("missing %s in MAP page", id)
		}
	}
	if code := postSignals(t, srv, "/forge/map/spawn/place?map="+url.QueryEscape(path)+"&group=0&type=Player&x=1&y=0", `{}`); code != 204 {
		t.Fatalf("place = %d", code)
	}
	page := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(path)+"&spawn=1")
	for _, id := range []string{"spawn-1", "spawn-selected", "spawn-delete"} {
		if !strings.Contains(page, `data-testid="`+id+`"`) {
			t.Errorf("missing %s after selecting spawn", id)
		}
	}
	if !strings.Contains(page, "Player") || !strings.Contains(page, "next ecs-db run") {
		t.Error("selection must name the type and the next-run delete effect")
	}
}

func inspectorServer(t *testing.T) (*httptest.Server, *Server, string) {
	t.Helper()
	srv, s, _, path := mapServer(t)
	if err := s.cfg.Session.Edit(func(d *schema.DatabaseSchema) error {
		d.Components["Health"] = schema.Component{
			Type:       schema.ComponentTypeObject,
			Properties: map[string]schema.Property{"hp": {Type: schema.PropertyTypeInteger}},
		}
		d.Components["Label"] = schema.Component{Type: schema.ComponentTypeString}
		d.Components["Probe"] = schema.Component{Type: schema.ComponentTypeString}
		d.Components["Target"] = schema.Component{Type: schema.ComponentTypeEntityRef}
		et := d.EntityTypes["Player"]
		et.RequiredComponents = []string{"Position", "Health"}
		et.OptionalComponents = []string{"Label", "Target"}
		d.EntityTypes["Player"] = et
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	base := "?map=" + url.QueryEscape(path) + "&id=1"
	if code := postSignals(t, srv, "/forge/map/spawn/place?map="+url.QueryEscape(path)+"&group=0&type=Player&x=1&y=0", `{}`); code != 204 {
		t.Fatalf("place = %d: %s", code, problemOf(s))
	}
	return srv, s, base
}

func TestSpawnInspectorRoutes_EditAttachAndDetach(t *testing.T) {
	srv, s, base := inspectorServer(t)
	if code := postSignals(t, srv, "/forge/map/spawn/property"+base+"&component=Health&field=hp&value=7", `{}`); code != 204 {
		t.Fatalf("edit hp = %d: %s", code, problemOf(s))
	}
	// Read from the working document, not from response status: every refusal
	// is deliberately 204 and only the session tells whether it changed.
	path := s.cfg.MapSession.Configured()
	if got := spawnObjects(t, s, path)[0].Properties.Get("Health.hp"); got != "7" {
		t.Errorf("Health.hp = %q, want 7", got)
	}
	if code := postSignals(t, srv, "/forge/map/spawn/component"+base+"&add=Label", `{}`); code != 204 {
		t.Fatalf("attach = %d: %s", code, problemOf(s))
	}
	if got := spawnObjects(t, s, path)[0].Properties.Get("Label.value"); got != "" {
		t.Errorf("scalar default = %q, want empty", got)
	}
	if code := postSignals(t, srv, "/forge/map/spawn/property"+base+"&component=Label&field=value&value=watcher", `{}`); code != 204 {
		t.Fatalf("set scalar = %d", code)
	}
	if got := spawnObjects(t, s, path)[0].Properties.Get("Label.value"); got != "watcher" {
		t.Errorf("scalar value = %q", got)
	}
	if code := postSignals(t, srv, "/forge/map/spawn/component"+base+"&detach=Label", `{}`); code != 204 {
		t.Fatalf("detach = %d", code)
	}
	p := spawnObjects(t, s, path)[0].Properties
	if p.Has("Label.value") || p.Get("Health.hp") != "7" {
		t.Errorf("detach lost data: %+v", p)
	}
}

func TestSpawnInspectorRoutes_RefuseInvalidEditsWithoutDirtyingMap(t *testing.T) {
	for _, tc := range []struct{ name, route, reason string }{
		{"bad integer", "/property&component=Health&field=hp&value=much", "Health.hp"},
		{"required detach", "/component&detach=Health", "required"},
		{"unpermitted add", "/component&add=Probe", "not allowed"},
		{"position edit", "/property&component=Position&field=x&value=9", "position"},
		{"scalar wrong field", "/property&component=Label&field=text&value=no", "value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, s, base := inspectorServer(t)
			path := s.cfg.MapSession.Configured()
			var before string
			_ = s.cfg.MapSession.Read(path, func(d *tiled.Document) error { before = string(d.Bytes()); return nil })
			action, arg, _ := strings.Cut(tc.route, "&")
			code := postSignals(t, srv, "/forge/map/spawn"+action+base+"&"+arg, `{}`)
			if code != 204 || !strings.Contains(problemOf(s), tc.reason) {
				t.Errorf("invalid edit = %d, reason %q; want %q", code, problemOf(s), tc.reason)
			}
			var after string
			_ = s.cfg.MapSession.Read(path, func(d *tiled.Document) error { after = string(d.Bytes()); return nil })
			if after != before {
				t.Error("refused edit changed the working map")
			}
		})
	}
}

func TestSpawnInspectorRoutes_AttachReferenceWithExplicitTarget(t *testing.T) {
	srv, s, base := inspectorServer(t)
	path := s.cfg.MapSession.Configured()
	if code := postSignals(t, srv, "/forge/map/spawn/component"+base+"&add=Target&target=17", `{}`); code != 204 {
		t.Fatalf("attach = %d: %s", code, problemOf(s))
	}
	got := spawnObjects(t, s, path)[0].Properties["Target.target_entity_id"]
	if got.Type != "int" || got.Value != "17" {
		t.Errorf("attached ref = %+v", got)
	}
}

func TestSpawnInspectorRoutes_RepairsSeveralMissingRequiredComponents(t *testing.T) {
	srv, s, base := inspectorServer(t)
	path := s.cfg.MapSession.Configured()
	if err := s.cfg.Session.Edit(func(d *schema.DatabaseSchema) error {
		et := d.EntityTypes["Player"]
		et.RequiredComponents = []string{"Position", "Health", "Label"}
		et.OptionalComponents = []string{"Target"}
		d.EntityTypes["Player"] = et
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.MapSession.Edit(path, func(d *tiled.Document) error {
		return d.RemoveObjectComponent(1, "Health")
	}); err != nil {
		t.Fatal(err)
	}
	if code := postSignals(t, srv, "/forge/map/spawn/component"+base+"&repair=required", `{}`); code != 204 {
		t.Fatalf("repair = %d: %s", code, problemOf(s))
	}
	p := spawnObjects(t, s, path)[0].Properties
	if p.Get("Health.hp") != "0" || !p.Has("Label.value") {
		t.Errorf("repair left required components absent: %+v; reason %s", p, problemOf(s))
	}
}

func TestSpawnInspectorRoutes_AttachObjectWithTwoReferenceTargets(t *testing.T) {
	srv, s, base := inspectorServer(t)
	if err := s.cfg.Session.Edit(func(d *schema.DatabaseSchema) error {
		d.Components["Owner"] = schema.Component{Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
			"first": {Type: schema.PropertyTypeEntityRef}, "second": {Type: schema.PropertyTypeEntityRef},
		}}
		et := d.EntityTypes["Player"]
		et.OptionalComponents = append(et.OptionalComponents, "Owner")
		d.EntityTypes["Player"] = et
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	query := "/forge/map/spawn/component" + base + "&add=Owner&target.first=1&target.second=2"
	if code := postSignals(t, srv, query, `{}`); code != 204 {
		t.Fatalf("attach = %d: %s", code, problemOf(s))
	}
	p := spawnObjects(t, s, s.cfg.MapSession.Configured())[0].Properties
	if p.Get("Owner.first") != "1" || p.Get("Owner.second") != "2" {
		t.Errorf("object reference fields not written: %+v; reason %s", p, problemOf(s))
	}
}
