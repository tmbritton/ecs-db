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

func TestTilesClassEdit_UpdatesTheHeldTSXAndBothMapPreviews(t *testing.T) {
	srv, s, tsx := tilesetServer(t)
	response, err := http.Post(srv.URL+"/forge/tiles/tile/class?file="+url.QueryEscape(tsx)+"&tile=0&value=water", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("class edit HTTP %s", response.Status)
	}
	page := mapPage(t, srv, "/forge/tiles?file="+url.QueryEscape(tsx)+"&tile=0")
	if !strings.Contains(page, `data-testid="tile-class"`) || !strings.Contains(page, "water") || !strings.Contains(page, `save-footer--dirty`) {
		t.Error("class edit did not update inspector and dirty footer")
	}
	if !strings.Contains(page, `data-testid="tile-grid-class-0"`) {
		t.Error("class edit did not update the tile grid")
	}
	for _, name := range []string{"level1.tmx", "arena.tmx"} {
		preview, problems, err := s.cfg.MapSession.Preview(filepath.Join(filepath.Dir(tsx), name))
		if err != nil || len(problems) > 0 || preview.Tilesets[0].Tileset.Tiles[0].Type != "water" {
			t.Fatalf("%s missed the unsaved class: %v, %v", name, problems, err)
		}
	}
	disk, err := os.ReadFile(tsx)
	if err != nil || strings.Contains(string(disk), `type="water"`) {
		t.Error("class edit wrote TSX before Save")
	}
}

func TestTilesPropertyEdit_AddsTypedMetadataToImplicitSheetTileWithoutLosingXML(t *testing.T) {
	srv, s, tsx := tilesetServer(t)
	original := strings.Replace(mapTSX,
		`<tile id="0" type="floor"><properties><property name="passable" type="bool" value="true"/></properties></tile>`,
		`<!-- custom metadata stays -->`, 1)
	if err := os.WriteFile(tsx, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.TilesetSession.Reload(tsx); err != nil {
		t.Fatal(err)
	}
	uri := srv.URL + "/forge/tiles/tile/property?file=" + url.QueryEscape(tsx) + "&tile=0&name=entityType&type=string&value=River"
	resp, err := http.Post(uri, "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("property edit: %s", resp.Status)
	}
	page := mapPage(t, srv, "/forge/tiles?file="+url.QueryEscape(tsx)+"&tile=0")
	if !strings.Contains(page, `data-testid="tile-properties"`) || !strings.Contains(page, "River") {
		t.Error("added property was not reflected by the inspector")
	}
	if err := s.cfg.TilesetSession.Save(tsx); err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(tsx)
	if err != nil || !strings.Contains(string(disk), "custom metadata stays") || !strings.Contains(string(disk), `<tile id="0"`) || !strings.Contains(string(disk), `value="River"`) {
		t.Fatalf("implicit tile edit lost preserved XML: %s, %v", disk, err)
	}
}

func TestTilesPropertyEdit_RefusesInvalidTypeValueOrLegacyPassability(t *testing.T) {
	srv, s, tsx := tilesetServer(t)
	for _, tt := range []struct {
		name, args, reason string
	}{
		{"invalid int", "&tile=1&name=hardness&type=int&value=plenty", "whole number"},
		{"unknown type", "&tile=1&name=flag&type=magic&value=hi", "supported"},
		{"legacy passable", "&tile=1&name=passable&type=bool&value=true", "MAP"},
		{"missing name", "&tile=1&type=string&value=hi", "name"},
		{"unknown ID", "&tile=100&name=kind&type=string&value=hi", "no tile"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Post(srv.URL+"/forge/tiles/tile/property?file="+url.QueryEscape(tsx)+tt.args, "application/x-www-form-urlencoded", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			problem, _ := s.lastEditProblem()
			if resp.StatusCode != http.StatusNoContent || !strings.Contains(problem, tt.reason) {
				t.Errorf("refusal %s = HTTP %s, %q", tt.name, resp.Status, problem)
			}
			if dirty, err := s.cfg.TilesetSession.Dirty(); err != nil || len(dirty) > 0 {
				t.Errorf("refused edit dirtied TSX: %v, %v", dirty, err)
			}
		})
	}
}

func TestValidateTileProperty_OnlySupportedFiniteTypedValues(t *testing.T) {
	for _, tt := range []struct {
		name, property, kind, value string
		wantErr                     bool
	}{
		{"string", "entityType", "string", "River", false},
		{"int", "height", "int", "-3", false},
		{"float", "speed", "float", "1.5", false},
		{"bool", "decorative", "bool", "false", false},
		{"invalid float", "speed", "float", "NaN", true},
		{"infinite float", "speed", "float", "+Inf", true},
		{"invalid bool", "decorative", "bool", "1", true},
		{"unsupported class", "terrain", "class", "Grass", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateTileProperty(tt.property, tt.kind, tt.value); (err != nil) != tt.wantErr {
				t.Errorf("validateTileProperty(%q,%q,%q) = %v", tt.property, tt.kind, tt.value, err)
			}
		})
	}
}

func TestTilesMode_EditingControlsBelongOnlyToWritableTSX(t *testing.T) {
	srv, _, tsx := tilesetServer(t)
	writable := mapPage(t, srv, "/forge/tiles?file="+url.QueryEscape(tsx)+"&tile=0")
	for _, want := range []string{`data-testid="tile-class-input"`, `data-testid="tile-property-name"`, `data-testid="tile-property-type"`, `data-testid="tile-property-value"`, `data-testid="tile-property-submit"`} {
		if !strings.Contains(writable, want) {
			t.Errorf("writable TSX omitted %s", want)
		}
	}
	root := filepath.Dir(tsx)
	tsj := filepath.Join(root, "legacy.tsj")
	if err := os.WriteFile(tsj, []byte(`{"name":"legacy","tilewidth":16,"tileheight":16,"tilecount":1,"columns":1,"image":"fixture.png","imagewidth":16,"imageheight":16}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "legacy.tmx"), []byte(strings.ReplaceAll(mapBody("legacy", "1,1,\n1,1"), "fixture.tsx", "legacy.tsj")), 0o600); err != nil {
		t.Fatal(err)
	}
	readonly := mapPage(t, srv, "/forge/tiles?file="+url.QueryEscape(tsj)+"&tile=0")
	if strings.Contains(readonly, `data-testid="tile-class-input"`) || strings.Contains(readonly, `data-testid="tile-property-submit"`) {
		t.Error("read-only TSJ rendered write controls")
	}
}

func TestTilesClassEdit_NoOpAndUnheldPathCannotWrite(t *testing.T) {
	srv, s, tsx := tilesetServer(t)
	for _, tt := range []struct {
		name, path, value string
		wantProblem       bool
	}{
		{"existing class unchanged", tsx, "floor", false},
		{"not in the session", filepath.Join(filepath.Dir(tsx), "unlisted.tsx"), "water", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantProblem {
				if err := os.WriteFile(tt.path, []byte(mapTSX), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			resp, err := http.Post(srv.URL+"/forge/tiles/tile/class?file="+url.QueryEscape(tt.path)+"&tile=0&value="+url.QueryEscape(tt.value), "application/x-www-form-urlencoded", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			problem, _ := s.lastEditProblem()
			if tt.wantProblem != (problem != "") {
				t.Errorf("edit refusal = %q, want problem %v", problem, tt.wantProblem)
			}
			if dirty, err := s.cfg.TilesetSession.Dirty(); err != nil || len(dirty) > 0 {
				t.Errorf("no-op/refused class edit dirtied TSX: %v, %v", dirty, err)
			}
		})
	}
}

func TestTilesPropertyEdit_DoesNotInventAnAbsentCollectionTile(t *testing.T) {
	srv, s, tsx := tilesetServer(t)
	collection := `<?xml version="1.0"?><tileset name="sparse" tilewidth="16" tileheight="16" tilecount="2" columns="0"><tile id="0"><image source="fixture.png" width="16" height="16"/></tile><tile id="21"><image source="fixture.png" width="16" height="16"/></tile></tileset>`
	if err := os.WriteFile(tsx, []byte(collection), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.TilesetSession.Reload(tsx); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{
		"/forge/tiles/tile/class?file=" + url.QueryEscape(tsx) + "&tile=10&value=unknown",
		"/forge/tiles/tile/property?file=" + url.QueryEscape(tsx) + "&tile=10&name=entityType&type=string&value=Wall",
	} {
		resp, err := http.Post(srv.URL+route, "application/x-www-form-urlencoded", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		problem, _ := s.lastEditProblem()
		if !strings.Contains(problem, "no tile 10") {
			t.Errorf("an absent collection ID got no refusal from %s: %q", route, problem)
		}
	}
	if dirty, err := s.cfg.TilesetSession.Dirty(); err != nil || len(dirty) != 0 {
		t.Fatalf("refused collection edit mutated the session: %v, %v", dirty, err)
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
