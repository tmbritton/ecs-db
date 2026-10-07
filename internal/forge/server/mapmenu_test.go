package server

import (
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func layeredMapServer(t *testing.T) (*httptest.Server, *Server, string) {
	t.Helper()
	srv, s, sess, path := mapServer(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	withProps := strings.Replace(string(raw), `<objectgroup id="2" name="spawns"/>`,
		`<layer id="3" name="props" width="2" height="2"><data encoding="csv">0,1,0,0</data></layer>
 <objectgroup id="2" name="spawns"/>`, 1)
	if withProps == string(raw) {
		t.Fatal("map fixture changed")
	}
	if err := os.WriteFile(path, []byte(withProps), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sess.Reload(path); err != nil {
		t.Fatal(err)
	}
	return srv, s, path
}

func mapLayers(t *testing.T, s *Server, path string) []tiled.Layer {
	t.Helper()
	var out []tiled.Layer
	if err := s.cfg.MapSession.Read(path, func(d *tiled.Document) error {
		m, err := d.Map()
		if err == nil {
			out = append(out, m.Layers...)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func openLayerMenu(t *testing.T, srv *httptest.Server, s *Server, path string, index int) string {
	t.Helper()
	open := "/forge/map/menu?map=" + url.QueryEscape(path) + "&target=layer&layer=" + strconv.Itoa(index) + "&x=10&y=20"
	if code := postSignals(t, srv, open, `{}`); code != 204 {
		t.Fatalf("open layer %d menu: %d: %s", index, code, problemOf(s))
	}
	return s.openMapMenu().Token
}

func TestMapLayerMenu_RenameMoveDeleteThroughSession(t *testing.T) {
	srv, s, path := layeredMapServer(t)
	base := "/forge/map/layer?map=" + url.QueryEscape(path) + "&id=3"
	if code := postSignals(t, srv, base+"&layer=1&token="+openLayerMenu(t, srv, s, path, 1)+"&op=rename&name=items", `{}`); code != 204 {
		t.Fatalf("rename %d: %s", code, problemOf(s))
	}
	if got := mapLayers(t, s, path); len(got) != 2 || got[1].Name != "items" {
		t.Errorf("renamed layers: %+v", got)
	}
	if code := postSignals(t, srv, base+"&layer=1&token="+openLayerMenu(t, srv, s, path, 1)+"&op=up", `{}`); code != 204 {
		t.Fatalf("move %d: %s", code, problemOf(s))
	}
	if got := mapLayers(t, s, path); got[0].Name != "items" || got[1].Name != "ground" {
		t.Errorf("new draw order: %+v", got)
	}
	if code := postSignals(t, srv, base+"&layer=0&token="+openLayerMenu(t, srv, s, path, 0)+"&op=delete", `{}`); code != 204 {
		t.Fatalf("delete %d: %s", code, problemOf(s))
	}
	if got := mapLayers(t, s, path); len(got) != 1 || got[0].Name != "ground" {
		t.Errorf("deleted wrong layer: %+v", got)
	}
}

func TestMapMenu_OpensOneTargetAndClosesOnRequest(t *testing.T) {
	srv, s, _, path := mapServer(t)
	base := "/forge/map/menu?map=" + url.QueryEscape(path)
	if code := postSignals(t, srv, base+"&target=layer&layer=0&x=100&y=200", `{}`); code != 204 {
		t.Fatalf("open = %d: %s", code, problemOf(s))
	}
	menu := s.openMapMenu()
	if !menu.Open || menu.Kind != "layer" || menu.Layer != 0 || menu.X != 100 || menu.Y != 200 {
		t.Errorf("open menu = %+v", menu)
	}
	if code := postSignals(t, srv, base+"&target=spawn&id=999&x=130&y=170", `{}`); code != 204 {
		t.Fatalf("bad target = %d", code)
	}
	if got := s.openMapMenu(); got.Open {
		t.Errorf("a bad second target left the first menu open: %+v", got)
	}
	_ = postSignals(t, srv, base+"&target=layer&layer=0&x=10&y=20", `{}`)
	_ = postSignals(t, srv, "/forge/map/menu?close=1", `{}`)
	if s.openMapMenu().Open {
		t.Error("close did not close")
	}
}

func TestMapMenu_PageLoadClosesAnotherTabsMenu(t *testing.T) {
	srv, s, _, path := mapServer(t)
	base := "/forge/map/menu?map=" + url.QueryEscape(path)
	if code := postSignals(t, srv, base+"&target=layer&layer=0&x=10&y=20", `{}`); code != 204 {
		t.Fatal(code)
	}
	if !s.openMapMenu().Open {
		t.Fatal("menu did not open")
	}
	_ = mapPage(t, srv, "/forge/map?map="+url.QueryEscape(path))
	if s.openMapMenu().Open {
		t.Error("a full page load inherited an old tab's menu")
	}
}

func TestMapMenu_LateCloseCannotCloseANewerMenu(t *testing.T) {
	srv, s, _, path := mapServer(t)
	base := "/forge/map/menu?map=" + url.QueryEscape(path)
	_ = postSignals(t, srv, base+"&target=layer&layer=0&x=10&y=20", `{}`)
	old := s.openMapMenu().Token
	_ = postSignals(t, srv, base+"&target=canvas&cellx=0&celly=0&x=30&y=40", `{}`)
	newer := s.openMapMenu().Token
	if old == newer || newer == "" {
		t.Fatalf("menu openings share an identity: %q, %q", old, newer)
	}
	_ = postSignals(t, srv, "/forge/map/menu?close=1&token="+old, `{}`)
	if got := s.openMapMenu(); !got.Open || got.Token != newer {
		t.Errorf("late close dismissed another menu: %+v", got)
	}
	_ = postSignals(t, srv, "/forge/map/menu?close=1&token="+newer, `{}`)
	if s.openMapMenu().Open {
		t.Error("current menu did not close")
	}
}

func TestMapMenu_SlowerOpenCannotDismissANewerMenu(t *testing.T) {
	for _, oldResult := range []struct {
		name string
		menu modes.MapMenu
	}{
		{"empty cell", modes.MapMenu{}},
		{"valid older layer", modes.MapMenu{Open: true, Kind: "layer", Layer: 0}},
	} {
		t.Run(oldResult.name, func(t *testing.T) {
			_, s, _, _ := mapServer(t)
			older := s.beginMapMenuOpen()
			newer := s.beginMapMenuOpen()
			if !s.finishMapMenuOpen(newer, modes.MapMenu{Open: true, Kind: "spawn", ObjectID: 7}) {
				t.Fatal("newest menu was not applied")
			}
			if s.finishMapMenuOpen(older, oldResult.menu) {
				t.Fatal("an older menu response was applied")
			}
			if got := s.openMapMenu(); !got.Open || got.Kind != "spawn" || got.ObjectID != 7 {
				t.Errorf("older response replaced the new menu: %+v", got)
			}
		})
	}
}

func TestMapMenu_QueuedSpawnActionCannotEditOrCloseANewerMenu(t *testing.T) {
	srv, s, _, path := mapServer(t)
	base := "?map=" + url.QueryEscape(path)
	if code := postSignals(t, srv, "/forge/map/spawn/place"+base+"&group=0&type=Player&x=0&y=0", `{}`); code != 204 {
		t.Fatal(code)
	}
	_ = postSignals(t, srv, "/forge/map/menu"+base+"&target=spawn&id=1&x=10&y=20", `{}`)
	old := s.openMapMenu().Token
	_ = postSignals(t, srv, "/forge/map/menu?close=1&token="+old, `{}`)
	_ = postSignals(t, srv, "/forge/map/menu"+base+"&target=spawn&id=1&x=15&y=25", `{}`)
	newer := s.openMapMenu().Token
	if code := postSignals(t, srv, "/forge/map/spawn/duplicate"+base+"&id=1&token="+old, `{}`); code != 204 {
		t.Fatal(code)
	}
	if got := spawnObjects(t, s, path); len(got) != 1 {
		t.Errorf("old duplicate changed the map: %+v", got)
	}
	if got := s.openMapMenu(); !got.Open || got.Token != newer {
		t.Errorf("old duplicate closed newer menu: %+v", got)
	}
	if code := postSignals(t, srv, "/forge/map/spawn/delete"+base+"&id=1&token="+old, `{}`); code != 204 {
		t.Fatal(code)
	}
	if got := spawnObjects(t, s, path); len(got) != 1 {
		t.Errorf("old delete changed the map: %+v", got)
	}
	if got := s.openMapMenu(); !got.Open || got.Token != newer {
		t.Errorf("old delete closed newer menu: %+v", got)
	}
}

func TestMapMenu_SpawnDuplicateAllocatesAnIDAndDeleteCloses(t *testing.T) {
	srv, s, _, path := mapServer(t)
	base := "?map=" + url.QueryEscape(path)
	if code := postSignals(t, srv, "/forge/map/spawn/place"+base+"&group=0&type=Player&x=0&y=0", `{}`); code != 204 {
		t.Fatal(code)
	}
	_ = postSignals(t, srv, "/forge/map/menu"+base+"&target=spawn&id=1&x=10&y=20", `{}`)
	if code := postSignals(t, srv, "/forge/map/spawn/duplicate"+base+"&id=1&token="+s.openMapMenu().Token, `{}`); code != 204 {
		t.Fatalf("duplicate %d: %s", code, problemOf(s))
	}
	if got := spawnObjects(t, s, path); len(got) != 2 || got[1].ID != 2 || got[1].Type != "Player" {
		t.Errorf("duplicate: %+v", got)
	}
	_ = postSignals(t, srv, "/forge/map/menu"+base+"&target=spawn&id=2&x=10&y=20", `{}`)
	if code := postSignals(t, srv, "/forge/map/spawn/delete"+base+"&id=2&token="+s.openMapMenu().Token, `{}`); code != 204 {
		t.Fatal(code)
	}
	if got := spawnObjects(t, s, path); len(got) != 1 || got[0].ID != 1 {
		t.Errorf("deleted wrong object: %+v", got)
	}
	var counter int
	_ = s.cfg.MapSession.Read(path, func(d *tiled.Document) error { counter = d.NextObjectID(); return nil })
	if counter != 3 {
		t.Errorf("duplicate id was recycled after delete: %d", counter)
	}
}

func TestMapLayerMenu_StaleIndexCannotEditAnotherLayer(t *testing.T) {
	srv, s, path := layeredMapServer(t)
	base := "/forge/map/layer?map=" + url.QueryEscape(path)
	token := openLayerMenu(t, srv, s, path, 1)
	if code := postSignals(t, srv, base+"&layer=1&id=3&token="+token+"&op=up", `{}`); code != 204 {
		t.Fatal(code)
	}
	// Even if another menu is open, it must not authorize the old action.
	_ = openLayerMenu(t, srv, s, path, 0)
	if code := postSignals(t, srv, base+"&layer=1&id=3&token="+token+"&op=delete", `{}`); code != 204 {
		t.Fatal(code)
	}
	if got := mapLayers(t, s, path); len(got) != 2 || got[0].Name != "props" || got[1].Name != "ground" {
		t.Errorf("stale index deleted or changed a layer: %+v", got)
	}
	if got := problemOf(s); !strings.Contains(got, "changed") {
		t.Errorf("no visible stale-target refusal: %q", got)
	}
	if got := s.openMapMenu(); !got.Open || got.Layer != 0 {
		t.Errorf("old action closed the newer menu: %+v", got)
	}
}

func TestMapLayerMenu_ChangedNameCannotActOnStaleMenu(t *testing.T) {
	srv, s, path := layeredMapServer(t)
	base := "?map=" + url.QueryEscape(path)
	if code := postSignals(t, srv, "/forge/map/menu"+base+"&target=layer&layer=1&x=10&y=20", `{}`); code != 204 {
		t.Fatal(code)
	}
	token := s.openMapMenu().Token
	if err := s.cfg.MapSession.Edit(path, func(d *tiled.Document) error { return d.RenameLayer(1, "elsewhere") }); err != nil {
		t.Fatal(err)
	}
	if code := postSignals(t, srv, "/forge/map/layer"+base+"&id=3&layer=1&token="+token+"&op=delete", `{}`); code != 204 {
		t.Fatal(code)
	}
	if got := mapLayers(t, s, path); len(got) != 2 || got[1].Name != "elsewhere" {
		t.Errorf("stale menu deleted the layer renamed in another tab: %+v", got)
	}
	if got := problemOf(s); !strings.Contains(got, "changed") {
		t.Errorf("no visible refusal: %q", got)
	}
}

func TestMapLayerMenu_ActionQueuedAfterClosingIsRefused(t *testing.T) {
	srv, s, path := layeredMapServer(t)
	base := "?map=" + url.QueryEscape(path)
	if code := postSignals(t, srv, "/forge/map/menu"+base+"&target=layer&layer=1&x=10&y=20", `{}`); code != 204 {
		t.Fatal(code)
	}
	token := s.openMapMenu().Token
	_ = postSignals(t, srv, "/forge/map/menu?close=1", `{}`)
	if code := postSignals(t, srv, "/forge/map/layer"+base+"&id=3&layer=1&token="+token+"&op=delete", `{}`); code != 204 {
		t.Fatal(code)
	}
	if got := mapLayers(t, s, path); len(got) != 2 {
		t.Errorf("closed menu changed the map: %+v", got)
	}
	if got := problemOf(s); !strings.Contains(got, "menu") {
		t.Errorf("no visible refusal for a closed menu: %q", got)
	}
}

func TestMapLayerMenu_ReorderingKeepsTheStrokesLayerIdentity(t *testing.T) {
	srv, s, path := layeredMapServer(t)
	base := "?map=" + url.QueryEscape(path)
	token := openLayerMenu(t, srv, s, path, 1)
	if code := postSignals(t, srv, "/forge/map/layer"+base+"&layer=1&id=3&token="+token+"&op=up", `{}`); code != 204 {
		t.Fatal(code)
	}
	// The tab's $layer index has not changed, but layerID still names props.
	if code := postSignals(t, srv, "/forge/map/paint"+base+"&x=0&y=0",
		`{"tool":"stamp","tile":1,"layer":1,"layerID":3,"hideID3":false}`); code != 204 {
		t.Fatal(code)
	}
	got := mapLayers(t, s, path)
	if got[0].Name != "props" || got[0].TileAt(0, 0).GID != 1 ||
		got[1].Name != "ground" || got[1].TileAt(0, 0).GID != 2 {
		t.Errorf("stroke after move hit another layer: %+v", got)
	}
}

func TestMapMenu_CanvasPicksTopmostVisibleTile(t *testing.T) {
	srv, s, path := layeredMapServer(t)
	base := "/forge/map/menu?map=" + url.QueryEscape(path) + "&target=canvas&cellx=1&celly=0&x=40&y=60"
	for _, tc := range []struct {
		name    string
		signals string
		want    uint32
	}{
		{"top layer", `{}`, 1},
		{"top hidden", `{"hideID3":true}`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := postSignals(t, srv, base, tc.signals); code != 204 {
				t.Fatal(code)
			}
			if got := s.openMapMenu(); !got.Open || got.Tile.GID != tc.want {
				t.Errorf("picker: %+v, want tile %d", got, tc.want)
			}
		})
	}
	if code := postSignals(t, srv, base, `{"hideID1":true,"hideID3":true}`); code != 204 {
		t.Fatal(code)
	}
	if s.openMapMenu().Open {
		t.Error("empty cell should not offer a picker")
	}
	if got := problemOf(s); got != "" {
		t.Errorf("a right-click on an empty cell is ordinary, not an edit error: %q", got)
	}
}

func TestMapMenu_CanvasSkipsAZeroOpacityLayer(t *testing.T) {
	srv, s, path := layeredMapServer(t)
	// Opacity is a Tiled attribute Forge does not edit. Reload a transparent
	// authored layer so the canvas and picker both read the same file.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `<layer id="3" name="props"`, `<layer id="3" name="props" opacity="0"`, 1))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.MapSession.Reload(path); err != nil {
		t.Fatal(err)
	}
	base := "/forge/map/menu?map=" + url.QueryEscape(path) + "&target=canvas&cellx=1&celly=0&x=40&y=60"
	if code := postSignals(t, srv, base, `{}`); code != 204 {
		t.Fatal(code)
	}
	if got := s.openMapMenu().Tile.GID; got != 2 {
		t.Errorf("picked transparent layer's tile %d instead of visible ground tile 2", got)
	}
}

func TestMapMenu_DuplicateLayerIDCannotRoutePaintToTheFirstMatch(t *testing.T) {
	srv, s, path := layeredMapServer(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `<layer id="3" name="props"`, `<layer id="1" name="props"`, 1))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.MapSession.Reload(path); err != nil {
		t.Fatal(err)
	}
	base := "?map=" + url.QueryEscape(path)
	if code := postSignals(t, srv, "/forge/map/paint"+base+"&x=0&y=0",
		`{"tool":"stamp","tile":1,"layer":1,"layerID":1,"hideID1":false}`); code != 204 {
		t.Fatal(code)
	}
	if got := mapLayers(t, s, path); got[0].TileAt(0, 0).GID != 2 || got[1].TileAt(0, 0).GID != 0 {
		t.Errorf("ambiguous layer ID painted a layer: %+v", got)
	}
	if got := problemOf(s); !strings.Contains(got, "ambiguous") {
		t.Errorf("no visible explanation for duplicate ID: %q", got)
	}
}
