package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/maps"
	"github.com/tmbritton/ecs-db/internal/forge/tilesets"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func tilesetServer(t *testing.T) (*httptest.Server, *Server, string) {
	t.Helper()
	srv, s, maps, mapPath := mapServer(t)
	root := filepath.Dir(mapPath)
	session := tilesets.Open(root, maps)
	maps.SetTilesetOpener(session.WorkingBytes)
	s.cfg.TilesetSession = session
	return srv, s, filepath.Join(root, "fixture.tsx")
}

func TestTilesMode_EmptyProjectStillReportsOtherUnsavedWork(t *testing.T) {
	srv, s, tsx := tilesetServer(t)
	root := filepath.Dir(tsx)
	bare := filepath.Join(root, "empty.tmx")
	if err := os.WriteFile(bare, []byte(strings.Replace(mapBody("empty", "0,0,\n0,0"), `<tileset firstgid="1" source="fixture.tsx"/>`, "", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"arena.tmx", "level1.tmx"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	mapSession := maps.Open(maps.Config{Root: root, MapPath: bare})
	if err := mapSession.Edit(bare, func(d *tiled.Document) error { return d.SetMapID("unsaved") }); err != nil {
		t.Fatal(err)
	}
	s.cfg.MapSession = mapSession
	s.cfg.TilesetSession = tilesets.Open(root, mapSession)
	body := mapPage(t, srv, "/forge/tiles")
	if !strings.Contains(body, `1 unsaved map in MAP`) || !strings.Contains(body, `data-testid="save-footer"`) {
		t.Error("empty TILES mode concealed other unsaved project work")
	}
}

func TestTilesMode_URLSelectsARealTileAndReportsUnknownWithoutDirtyingTSX(t *testing.T) {
	srv, s, tsx := tilesetServer(t)
	for _, tt := range []struct {
		name, tile, want, absent string
	}{
		{"default", "", `data-testid="tile-selected"`, `data-testid="tile-problem"`},
		{"last implicit tile", "1", `data-testid="tile-1"`, `data-testid="tile-problem"`},
		{"unknown tile", "40", `data-testid="tile-problem"`, `data-testid="tile-selected"`},
		{"invalid syntax", "nope", `data-testid="tile-problem"`, `data-testid="tile-selected"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			uri := "/forge/tiles?file=" + url.QueryEscape(tsx)
			if tt.tile != "" {
				uri += "&tile=" + url.QueryEscape(tt.tile)
			}
			body := mapPage(t, srv, uri)
			if !strings.Contains(body, tt.want) || strings.Contains(body, tt.absent) {
				t.Errorf("tile selection %q: missing %s or incorrectly rendered %s", tt.tile, tt.want, tt.absent)
			}
			if dirty, err := s.cfg.TilesetSession.Dirty(); err != nil || len(dirty) != 0 {
				t.Errorf("reading tile selection made TSX dirty: %v, %v", dirty, err)
			}
		})
	}
}

func TestTilesMode_PageIdentityIsNotATileGridPage(t *testing.T) {
	srv, _, tsx := tilesetServer(t)
	body := mapPage(t, srv, "/forge/tiles?file="+url.QueryEscape(tsx)+"&page=forge-browser-tab")
	if !strings.Contains(body, `data-testid="tile-1"`) || strings.Contains(body, `data-testid="tile-page-problem"`) {
		t.Error("Forge page identity was mistaken for a tile-grid page")
	}
}

func TestTilesMode_DirtySharedFileConflictAndReloadFollowSelectedFile(t *testing.T) {
	srv, s, tsx := tilesetServer(t)
	if err := s.cfg.TilesetSession.Edit(tsx, func(d *tiled.TilesetDocument) error { return d.SetTileClass(0, "changed") }); err != nil {
		t.Fatal(err)
	}
	body := mapPage(t, srv, "/forge/tiles?file="+url.QueryEscape(tsx))
	for _, want := range []string{`● unsaved`, `/forge/tiles/save?file=`, `data-dirty="true"`} {
		if !strings.Contains(body, want) {
			t.Errorf("dirty TSX missing %s", want)
		}
	}
	schemaPage := mapPage(t, srv, "/forge/schema")
	if !strings.Contains(schemaPage, `1 unsaved tileset in TILES`) {
		t.Error("dirty TSX became invisible on SCHEMA")
	}
	external := strings.Replace(mapTSX, `type="floor"`, `type="external"`, 1)
	if err := os.WriteFile(tsx, []byte(external), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/forge/tiles/save?file=", "/forge/tiles/reload?file="} {
		resp, err := http.Post(srv.URL+route+url.QueryEscape(tsx), "application/x-www-form-urlencoded", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("POST %s: %s", route, resp.Status)
		}
		if strings.Contains(route, "save?") {
			conflict := mapPage(t, srv, "/forge/tiles?file="+url.QueryEscape(tsx))
			for _, want := range []string{`data-testid="conflict-reload"`, `data-testid="conflict-overwrite"`, `/forge/tiles/reload?file=`} {
				if !strings.Contains(conflict, want) {
					t.Errorf("conflicting TSX omitted %s", want)
				}
			}
		}
	}
	if got := mapPage(t, srv, "/forge/tiles?file="+url.QueryEscape(tsx)); strings.Contains(got, `● unsaved`) || strings.Contains(got, `data-testid="conflict-reload"`) {
		t.Error("reload left stale unsaved/conflict status")
	}
	if err := s.cfg.TilesetSession.Read(tsx, func(d *tiled.TilesetDocument) error {
		resolved, err := d.Tileset()
		if err == nil && resolved.Tiles[0].Type != "external" {
			t.Errorf("reload left working class %q", resolved.Tiles[0].Type)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTilesMode_ListsOneSharedTSXWithReferencingMapsAndFileFooter(t *testing.T) {
	srv, _, tsx := tilesetServer(t)
	body := mapPage(t, srv, "/forge/tiles")
	for _, want := range []string{
		`data-testid="tiles-mode"`, `data-testid="tileset-fixture.tsx"`,
		`data-testid="tileset-title">fixture.tsx`, `level1.tmx`, `arena.tmx`,
		`data-testid="save-footer-file"`, `data-testid="save-footer"`, `data-testid="save-footer-reload"`,
		`the next ecs-db run`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("TILES is missing %s", want)
		}
	}
	if got := strings.Count(body, `data-testid="tileset-fixture.tsx"`); got != 1 {
		t.Errorf("shared TSX appears %d times", got)
	}
	selected := mapPage(t, srv, "/forge/tiles?file="+url.QueryEscape(tsx))
	if !strings.Contains(selected, `data-testid="tileset-title">fixture.tsx`) {
		t.Errorf("explicit file selection did not open TSX")
	}
}

func TestTilesMode_ReadOnlyTSJAndGuessedPathCannotSave(t *testing.T) {
	srv, s, tsx := tilesetServer(t)
	root := filepath.Dir(tsx)
	tsj := filepath.Join(root, "legacy.tsj")
	if err := os.WriteFile(tsj, []byte(`{"name":"legacy","tilewidth":16,"tileheight":16,"tilecount":1,"columns":1,"image":"legacy.png","imagewidth":16,"imageheight":16}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "legacy.tmx"), []byte(strings.ReplaceAll(mapBody("legacy", "1,1,\n1,1"), "fixture.tsx", "legacy.tsj")), 0o600); err != nil {
		t.Fatal(err)
	}
	s.cfg.TilesetSession.Refresh()
	body := mapPage(t, srv, "/forge/tiles?file="+url.QueryEscape(tsj))
	for _, want := range []string{`data-testid="tileset-legacy.tsj"`, `data-testid="tileset-readonly"`, `data-testid="tileset-title">legacy.tsj`} {
		if !strings.Contains(body, want) {
			t.Errorf("read-only TSJ missing %s", want)
		}
	}
	if strings.Contains(body, `/forge/tiles/save?file=`) {
		t.Error("TSJ offered a Save action")
	}
	unknown := filepath.Join(root, "not-referenced.tsx")
	if err := os.WriteFile(unknown, []byte(mapTSX), 0o600); err != nil {
		t.Fatal(err)
	}
	body = mapPage(t, srv, "/forge/tiles?file="+url.QueryEscape(unknown))
	if strings.Contains(body, `data-testid="tileset-title">not-referenced.tsx`) {
		t.Error("guessed file appeared as a selectable tileset")
	}
	resp, err := http.Post(srv.URL+"/forge/tiles/save?file="+url.QueryEscape(unknown), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	problem, _ := s.lastEditProblem()
	if problem == "" {
		t.Error("crafted save request did not report refusal")
	}
}

func TestTilesMode_ReadOnlySelectionStillReportsOtherUnsavedWork(t *testing.T) {
	srv, s, tsx := tilesetServer(t)
	root := filepath.Dir(tsx)
	tsj := filepath.Join(root, "legacy.tsj")
	if err := os.WriteFile(tsj, []byte(`{"name":"legacy","tilewidth":16,"tileheight":16,"tilecount":1,"columns":1,"image":"legacy.png","imagewidth":16,"imageheight":16}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "legacy.tmx"), []byte(strings.ReplaceAll(mapBody("legacy", "1,1,\n1,1"), "fixture.tsx", "legacy.tsj")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.TilesetSession.Edit(tsx, func(d *tiled.TilesetDocument) error { return d.SetTileClass(0, "water") }); err != nil {
		t.Fatal(err)
	}
	body := mapPage(t, srv, "/forge/tiles?file="+url.QueryEscape(tsj))
	for _, want := range []string{`data-testid="save-footer"`, `read-only`, `1 unsaved tileset in TILES`} {
		if !strings.Contains(body, want) {
			t.Errorf("read-only selection concealed unsaved work: missing %s", want)
		}
	}
	if strings.Contains(body, `/forge/tiles/save?file=`) {
		t.Error("read-only TSJ offered a Save action")
	}
}

func TestTilesMode_ExternallyChangedTSJImageCanBeReadWithoutVisitingMAP(t *testing.T) {
	srv, s, tsx := tilesetServer(t)
	root := filepath.Dir(tsx)
	tsj := filepath.Join(root, "legacy.tsj")
	for _, image := range []string{"old.png", "new.png"} {
		if err := os.WriteFile(filepath.Join(root, image), []byte("image"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	content := func(image string) string {
		return `{"name":"legacy","tilewidth":16,"tileheight":16,"tilecount":1,"columns":1,"image":"` + image + `","imagewidth":16,"imageheight":16}`
	}
	if err := os.WriteFile(tsj, []byte(content("old.png")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "legacy.tmx"), []byte(strings.ReplaceAll(mapBody("legacy", "1,1,\n1,1"), "fixture.tsx", "legacy.tsj")), 0o600); err != nil {
		t.Fatal(err)
	}
	s.cfg.TilesetSession.Refresh()
	if err := os.WriteFile(tsj, []byte(content("new.png")), 0o600); err != nil {
		t.Fatal(err)
	}
	body := mapPage(t, srv, "/forge/tiles?file="+url.QueryEscape(tsj))
	if !strings.Contains(body, `data-testid="tile-inspector-art-0"`) || !strings.Contains(body, "new.png") || strings.Contains(body, "art unavailable") {
		t.Error("newly authored TSJ art was not usable on TILES without a MAP preview")
	}
	imagePath := filepath.Join(root, "new.png")
	resp, err := http.Get(srv.URL + "/forge/asset?path=" + url.QueryEscape(imagePath))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("new TSJ image is not allow-listed: %s", resp.Status)
	}
}
