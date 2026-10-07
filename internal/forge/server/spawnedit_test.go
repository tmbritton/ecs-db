package server

import (
	"net/url"
	"strings"
	"testing"

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
