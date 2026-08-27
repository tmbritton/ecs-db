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
	"github.com/tmbritton/ecs-db/internal/forge/session"
	"github.com/tmbritton/ecs-db/internal/forge/status"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

const mapTSX = `<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" name="fixture" tilewidth="16" tileheight="16" tilecount="2" columns="2">
 <image source="fixture.png" width="32" height="16"/>
 <tile id="0" type="floor"><properties><property name="passable" type="bool" value="true"/></properties></tile>
 <tile id="1" type="wall"><properties><property name="passable" type="bool" value="false"/></properties></tile>
</tileset>
`

func mapBody(id, data string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<map version="1.10" orientation="orthogonal" renderorder="right-down" width="2" height="2" tilewidth="16" tileheight="16" infinite="0" nextlayerid="3" nextobjectid="1">
 <properties>
  <property name="mapId" value="` + id + `"/>
 </properties>
 <tileset firstgid="1" source="fixture.tsx"/>
 <layer id="1" name="ground" width="2" height="2">
  <data encoding="csv">
` + data + `
</data>
 </layer>
 <objectgroup id="2" name="spawns"/>
</map>
`
}

// mapServer builds a server with a map session over a temp project holding two
// maps, the first of which is the configured one.
func mapServer(t *testing.T) (*httptest.Server, *Server, *maps.Session, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"fixture.tsx": mapTSX,
		"level1.tmx":  mapBody("level1", "2,2,\n2,1"),
		"arena.tmx":   mapBody("arena", "1,1,\n1,1"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configured := filepath.Join(dir, "level1.tmx")
	mapSess := maps.Open(maps.Config{MapPath: configured})

	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(sessionSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	sess, err := session.Open(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{Addr: "127.0.0.1:0", Session: sess, MapSession: mapSess, Engine: status.Config{}}, testFS())
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	return srv, s, mapSess, configured
}

func TestMapMode_ListsTheProjectsMapsAndMarksTheOneTheEngineLoads(t *testing.T) {
	srv, _, _, _ := mapServer(t)
	body := mapPage(t, srv, "/forge/map")

	for _, want := range []string{
		`data-testid="map-mode"`,
		`data-testid="map-level1.tmx"`,
		`data-testid="map-arena.tmx"`,
		`data-testid="map-configured-level1.tmx"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("MAP mode does not render %s", want)
		}
	}
	if strings.Contains(body, `data-testid="map-configured-arena.tmx"`) {
		t.Error("a map the config does not name is marked as loaded")
	}
	// The configured map is the default selection: it is the one the engine
	// reads, so it is the one to open on.
	if !strings.Contains(body, `data-testid="map-title">level1.tmx`) {
		t.Errorf("MAP did not open the configured map:\n%s", body)
	}
}

func TestMapMode_SelectsTheMapTheURLNames(t *testing.T) {
	srv, _, _, configured := mapServer(t)
	arena := filepath.Join(filepath.Dir(configured), "arena.tmx")
	body := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(arena))
	if !strings.Contains(body, `data-testid="map-title">arena.tmx`) {
		t.Errorf("the URL's map was not opened:\n%s", body)
	}
}

// A stale link or a hand-typed path selects something real rather than leaving
// the mode showing a map that is not there.
func TestMapMode_APathTheProjectDoesNotHaveFallsBackToTheConfiguredMap(t *testing.T) {
	srv, _, _, _ := mapServer(t)
	body := mapPage(t, srv, "/forge/map?map=%2Fetc%2Fpasswd")
	if !strings.Contains(body, `data-testid="map-title">level1.tmx`) {
		t.Errorf("an unknown map did not fall back:\n%s", body)
	}
}

func TestMapMode_AProjectWithNoMapSaysSo(t *testing.T) {
	mapSess := maps.Open(maps.Config{})
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(sessionSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	sess, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{Addr: "127.0.0.1:0", Session: sess, MapSession: mapSess}, testFS())
	srv := httptest.NewServer(s.routes())
	defer srv.Close()

	body := mapPage(t, srv, "/forge/map")
	if !strings.Contains(body, `data-testid="no-maps"`) {
		t.Errorf("a project with no map did not say so:\n%s", body)
	}
	// And no save footer: there is nothing to save, and a footer whose buttons
	// named no map would fall back to whichever one the server picked — which
	// is how MAP's Save came to write a file the user was not looking at.
	if strings.Contains(body, `data-testid="save-footer"`) {
		t.Errorf("a project with no map renders a save footer:\n%s", body)
	}
}

func TestMapSave_WritesTheSelectedMapAndReportsIt(t *testing.T) {
	srv, s, sess, configured := mapServer(t)
	paintFirstCell(t, sess, configured)

	if got := post(t, srv, "/forge/map/save?map="+url.QueryEscape(configured)); got != http.StatusNoContent {
		t.Fatalf("POST = %d, want %d", got, http.StatusNoContent)
	}

	raw, err := os.ReadFile(configured)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "1,2,\n2,1") {
		t.Errorf("the edit was not written:\n%s", raw)
	}
	reports := s.saves.All()
	if len(reports) != 1 || reports[0].Path != configured {
		t.Fatalf("save reports: %+v", reports)
	}
	if !reports[0].Rendered() {
		t.Error("the save was not reported")
	}
}

func TestMapSave_ReportsAConflictRatherThanOverwriting(t *testing.T) {
	srv, s, _, configured := mapServer(t)
	if err := os.WriteFile(configured, []byte(mapBody("level1", "1,1,\n1,1")), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := post(t, srv, "/forge/map/save?map="+url.QueryEscape(configured)); got != http.StatusNoContent {
		t.Fatalf("POST = %d, want %d", got, http.StatusNoContent)
	}

	reports := s.saves.All()
	if len(reports) != 1 {
		t.Fatalf("save reports: %+v", reports)
	}
	if !strings.Contains(reports[0].Summary(), "changed on disk") {
		t.Errorf("summary is %q, want the conflict", reports[0].Summary())
	}

	// Reload is the way out, and it clears the stale report — a message about a
	// state that no longer exists is worse than none.
	if got := post(t, srv, "/forge/map/reload?map="+url.QueryEscape(configured)); got != http.StatusNoContent {
		t.Fatalf("POST = %d, want %d", got, http.StatusNoContent)
	}
	if got := s.saves.All(); len(got) != 0 {
		t.Errorf("reload left a stale report: %+v", got)
	}
}

func TestMapSaveOverwriting_KeepsMine(t *testing.T) {
	srv, _, sess, configured := mapServer(t)
	paintFirstCell(t, sess, configured)
	if err := os.WriteFile(configured, []byte(mapBody("level1", "2,2,\n2,2")), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := post(t, srv, "/forge/map/save/overwrite?map="+url.QueryEscape(configured)); got != http.StatusNoContent {
		t.Fatalf("POST = %d, want %d", got, http.StatusNoContent)
	}

	raw, _ := os.ReadFile(configured)
	if !strings.Contains(string(raw), "1,2,\n2,1") {
		t.Errorf("keeping mine did not write mine:\n%s", raw)
	}
}

func TestMapDiscard_RestoresWithoutWriting(t *testing.T) {
	srv, _, sess, configured := mapServer(t)
	before, err := os.Stat(configured)
	if err != nil {
		t.Fatal(err)
	}
	paintFirstCell(t, sess, configured)
	if got := post(t, srv, "/forge/map/discard?map="+url.QueryEscape(configured)); got != http.StatusNoContent {
		t.Fatalf("POST = %d, want %d", got, http.StatusNoContent)
	}

	if dirty, _ := sess.Dirty(); len(dirty) != 0 {
		t.Errorf("discard left the map unsaved: %v", dirty)
	}
	after, err := os.Stat(configured)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("discard wrote to disk")
	}
}

// The path arrives from the page. A handler that passed it through would let a
// crafted request write to any file on disk.
func TestMapRoutes_RefuseAPathTheProjectDoesNotHave(t *testing.T) {
	srv, s, _, _ := mapServer(t)
	for _, route := range []string{"save", "save/overwrite", "discard", "reload"} {
		t.Run(route, func(t *testing.T) {
			s.setEditProblem("")
			if got := post(t, srv, "/forge/map/"+route+"?map=%2Fetc%2Fpasswd"); got != http.StatusNoContent {
				t.Fatalf("POST = %d, want %d", got, http.StatusNoContent)
			}
			if got := lastProblem(s); !strings.Contains(got, "not a map this project has open") {
				t.Errorf("refusal was %q", got)
			}
		})
	}
}

func TestMapRoutes_RefuseWhenNoProjectIsOpen(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())
	srv := httptest.NewServer(s.routes())
	defer srv.Close()
	if got := post(t, srv, "/forge/map/save"); got != http.StatusConflict {
		t.Fatalf("POST = %d, want %d", got, http.StatusConflict)
	}
}

// The save footer is in the shell and on screen everywhere, so unsaved map work
// has to be visible from the modes that cannot save it.
func TestFooter_SaysWhenAnotherModeHoldsAnUnsavedMap(t *testing.T) {
	srv, _, sess, configured := mapServer(t)
	paintFirstCell(t, sess, configured)

	body := mapPage(t, srv, "/forge/schema")
	if !strings.Contains(body, "1 unsaved map in MAP") {
		t.Errorf("SCHEMA's footer hides unsaved map work:\n%s", body)
	}
}

func mapPage(t *testing.T, srv *httptest.Server, path string) string {
	t.Helper()
	res, body := get(t, srv, path)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", path, res.StatusCode)
	}
	return body
}

func paintFirstCell(t *testing.T, sess *maps.Session, path string) {
	t.Helper()
	if err := sess.Edit(path, func(d *tiled.Document) error {
		return d.SetLayerData(0, []uint32{1, 2, 2, 1})
	}); err != nil {
		t.Fatal(err)
	}
}

func lastProblem(s *Server) string {
	msg, _ := s.lastEditProblem()
	return msg
}

// A map can appear beside the others without Forge doing anything — Tiled
// saving a new level into the same directory. The page has to notice.
func TestMapMode_PicksUpAMapAddedWhileForgeIsRunning(t *testing.T) {
	srv, _, _, configured := mapServer(t)
	added := filepath.Join(filepath.Dir(configured), "e2e-late.tmx")
	if err := os.WriteFile(added, []byte(mapBody("late", "1,1,\n1,1")), 0o600); err != nil {
		t.Fatal(err)
	}
	body := mapPage(t, srv, "/forge/map")
	if !strings.Contains(body, `data-testid="map-e2e-late.tmx"`) {
		t.Errorf("a map added beside the others is not listed:\n%s", body)
	}
}

func TestMapMode_MarksAnUnsavedMapAndPostsToItsOwnSaveRoute(t *testing.T) {
	srv, _, sess, configured := mapServer(t)
	paintFirstCell(t, sess, configured)

	body := mapPage(t, srv, "/forge/map")
	if !strings.Contains(body, `data-testid="map-dirty-level1.tmx"`) {
		t.Errorf("an unsaved map is not marked in the list:\n%s", body)
	}
	if !strings.Contains(body, "save-footer--dirty") {
		t.Error("MAP's footer does not report the unsaved map")
	}
	// MAP's Save saves the map on screen, not schema.json and not some other
	// map. One button does one thing, to one named file.
	if !strings.Contains(body, "/forge/map/save?map="+url.QueryEscape(configured)) {
		t.Errorf("MAP's footer does not post to the map save route:\n%s", body)
	}
	if strings.Contains(body, "/forge/schema/save") {
		t.Error("MAP's footer posts to the schema save route")
	}
}

// The footer names unsaved work it *cannot* save. The map on screen is the one
// it can, so counting it would tell the user to go and save what they are
// already looking at.
func TestMapFooter_DoesNotReportTheMapItIsAboutToSaveAsElsewhere(t *testing.T) {
	srv, _, sess, configured := mapServer(t)
	paintFirstCell(t, sess, configured)

	body := mapPage(t, srv, "/forge/map")
	if strings.Contains(body, "unsaved map in MAP") {
		t.Errorf("MAP's footer reports its own map as work it cannot save:\n%s", body)
	}

	// A second, unselected map that is dirty *is* reported, because this
	// footer's Save will not write it.
	arena := filepath.Join(filepath.Dir(configured), "arena.tmx")
	if err := sess.Edit(arena, func(d *tiled.Document) error { return d.SetLayerData(0, []uint32{2, 2, 2, 2}) }); err != nil {
		t.Fatal(err)
	}
	body = mapPage(t, srv, "/forge/map")
	if !strings.Contains(body, "1 unsaved map in MAP") {
		t.Errorf("MAP's footer hides an unsaved map in another tab:\n%s", body)
	}
}

// A conflict's two buttons have to operate on the file the conflict is about.
// They posted to /forge/schema/ whatever the file was, so resolving a map
// conflict with "Use theirs" reloaded schema.json — discarding unsaved schema
// work to fix a problem somewhere else entirely.
func TestSaveReports_ResolveTheFileTheConflictIsAbout(t *testing.T) {
	srv, s, sess, configured := mapServer(t)
	paintFirstCell(t, sess, configured)
	if err := os.WriteFile(configured, []byte(mapBody("level1", "1,1,\n1,1")), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := post(t, srv, "/forge/map/save?map="+url.QueryEscape(configured)); got != http.StatusNoContent {
		t.Fatalf("POST = %d", got)
	}

	body := mapPage(t, srv, "/forge/map")
	if !strings.Contains(body, `data-testid="conflict-reload"`) {
		t.Fatalf("the conflict offers no way out:\n%s", body)
	}
	if !strings.Contains(body, "/forge/map/reload") || !strings.Contains(body, "/forge/map/save/overwrite") {
		t.Errorf("the resolutions do not name the map routes:\n%s", body)
	}
	if strings.Contains(body, "/forge/schema/reload") {
		t.Error("a map conflict offers to reload schema.json")
	}
	// And the buttons name the map, so they cannot fall back to another one.
	if !strings.Contains(body, url.QueryEscape(configured)) {
		t.Error("the resolutions do not name which map they are about")
	}
	_ = s
}

// The footer follows the selection. Its Save and Discard must name the map on
// screen: without that they fell back to the configured map, so the confirm
// dialog named one file while the request destroyed another's work.
func TestMapFooter_ActsOnTheMapOnScreen(t *testing.T) {
	srv, _, sess, configured := mapServer(t)
	arena := filepath.Join(filepath.Dir(configured), "arena.tmx")
	paintFirstCell(t, sess, configured)
	if err := sess.Edit(arena, func(d *tiled.Document) error { return d.SetLayerData(0, []uint32{2, 2, 2, 2}) }); err != nil {
		t.Fatal(err)
	}

	body := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(arena))
	if !strings.Contains(body, "/forge/map/save?map="+url.QueryEscape(arena)) {
		t.Errorf("Save does not name the map on screen:\n%s", body)
	}
	if !strings.Contains(body, "/forge/map/discard?map="+url.QueryEscape(arena)) {
		t.Errorf("Discard does not name the map on screen:\n%s", body)
	}

	// And pressing it discards that map and leaves the other's work alone.
	if got := post(t, srv, "/forge/map/discard?map="+url.QueryEscape(arena)); got != http.StatusNoContent {
		t.Fatalf("POST = %d", got)
	}
	dirty, err := sess.Dirty()
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 1 || dirty[0] != configured {
		t.Errorf("discard hit the wrong map: dirty is %v, want just %s", dirty, configured)
	}
}

// A request that names no map is refused rather than defaulted — defaulting is
// what made the footer act on a file the user was not looking at.
func TestMapRoutes_RefuseARequestThatNamesNoMap(t *testing.T) {
	srv, s, _, _ := mapServer(t)
	for _, route := range []string{"save", "save/overwrite", "discard", "reload"} {
		t.Run(route, func(t *testing.T) {
			s.setEditProblem("")
			if got := post(t, srv, "/forge/map/"+route); got != http.StatusNoContent {
				t.Fatalf("POST = %d", got)
			}
			if got := lastProblem(s); !strings.Contains(got, "no map named") {
				t.Errorf("refusal was %q", got)
			}
		})
	}
}

// game.toml names a map that is not there. That is a broken project with a
// reason to show, not a project without a level — and the advice for the second
// ("add a [map] section") is useless to the first, who has one.
func TestMapMode_AConfiguredMapThatIsMissingIsNotTheSameAsNoMap(t *testing.T) {
	dir := t.TempDir()
	mapSess := maps.Open(maps.Config{MapPath: filepath.Join(dir, "gone.tmx")})
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(sessionSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	sess, err := session.Open(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{Addr: "127.0.0.1:0", Session: sess, MapSession: mapSess}, testFS())
	srv := httptest.NewServer(s.routes())
	defer srv.Close()

	body := mapPage(t, srv, "/forge/map")
	if strings.Contains(body, `data-testid="no-maps"`) {
		t.Errorf("a project that declares a map was told to declare one:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="map-unavailable"`) {
		t.Errorf("the mode does not say the configured map could not be opened:\n%s", body)
	}
	if !strings.Contains(body, "gone.tmx") {
		t.Error("the reason does not name the file")
	}
}

// A map deleted from the project while Forge held unsaved edits is in no tab
// and reachable from nothing. Saving it writes the file back, which is usually
// what somebody wants — and the problem panel says so, so the route has to
// accept it. Checking against the tab strip alone made that message a lie.
func TestMapSave_WritesBackAMapThatVanishedFromTheProject(t *testing.T) {
	srv, _, sess, configured := mapServer(t)
	arena := filepath.Join(filepath.Dir(configured), "arena.tmx")
	if err := sess.Edit(arena, func(d *tiled.Document) error { return d.SetLayerData(0, []uint32{2, 2, 2, 2}) }); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(arena); err != nil {
		t.Fatal(err)
	}
	// A render is what notices; the strip drops it and the problem appears.
	body := mapPage(t, srv, "/forge/map")
	if !strings.Contains(body, "no longer in the project") {
		t.Fatalf("the stranded map was not reported:\n%s", body)
	}

	if got := post(t, srv, "/forge/map/save/overwrite?map="+url.QueryEscape(arena)); got != http.StatusNoContent {
		t.Fatalf("POST = %d", got)
	}
	raw, err := os.ReadFile(arena)
	if err != nil {
		t.Fatalf("the stranded map was not written back: %v", err)
	}
	if !strings.Contains(string(raw), "2,2,\n2,2") {
		t.Errorf("what was written is not the work that was held:\n%s", raw)
	}
}

// Which session holds the path is what decides how a conflict is resolved. A
// schema conflict has to offer the schema routes even when a map session is
// present, or the buttons fix the wrong file.
func TestSaveReports_ASchemaConflictOffersTheSchemaRoutes(t *testing.T) {
	srv, s, _, _ := mapServer(t)
	schemaPath := s.cfg.Session.Path()
	if err := s.cfg.Session.Edit(func(d *schema.DatabaseSchema) error { d.SchemaVersion = 9; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(schemaPath, []byte(strings.Replace(sessionSchema, `"schemaVersion": 3`, `"schemaVersion": 4`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := post(t, srv, "/forge/schema/save"); got != http.StatusNoContent {
		t.Fatalf("POST = %d", got)
	}

	body := mapPage(t, srv, "/forge/schema")
	if !strings.Contains(body, `data-testid="conflict-reload"`) {
		t.Fatalf("the schema conflict offers no way out:\n%s", body)
	}
	if !strings.Contains(body, "/forge/schema/reload") {
		t.Errorf("a schema conflict does not offer the schema routes:\n%s", body)
	}
	if strings.Contains(body, "/forge/map/reload") || strings.Contains(body, "/forge/agents/reload") {
		t.Errorf("a schema conflict offers another session's routes:\n%s", body)
	}
}

func TestMapCanvas_DrawsACellPerNonEmptyTile(t *testing.T) {
	srv, _, _, _ := mapServer(t)
	body := mapPage(t, srv, "/forge/map")

	if !strings.Contains(body, `data-testid="map-canvas"`) {
		t.Fatalf("the canvas did not render:\n%s", body)
	}
	// The fixture is 2x2 with every cell filled.
	if got := strings.Count(body, `data-testid="map-cell"`); got != 4 {
		t.Errorf("got %d cells, want 4", got)
	}
	// Each one draws from the tileset image, through the asset route.
	if !strings.Contains(body, "/forge/asset?path=") {
		t.Errorf("no cell points at an image:\n%s", body)
	}
	// And a wall is a different slice of the sheet from a floor, which is what
	// makes them look different.
	if !strings.Contains(body, "background-position:0px 0px") ||
		!strings.Contains(body, "background-position:-16px 0px") {
		t.Errorf("every cell draws the same slice of the sheet:\n%s", body)
	}
}

func TestMapCanvas_ListsTheLayersAndTheTilesetPalette(t *testing.T) {
	srv, _, _, _ := mapServer(t)
	body := mapPage(t, srv, "/forge/map")

	if !strings.Contains(body, `data-testid="layer-ground"`) {
		t.Errorf("the layer panel does not list the map's layer:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="palette-fixture"`) {
		t.Errorf("the palette does not list the map's tileset:\n%s", body)
	}
	// Two tiles in the fixture tileset, both offered.
	for _, gid := range []string{"1", "2"} {
		if !strings.Contains(body, `data-testid="palette-tile-`+gid+`"`) {
			t.Errorf("the palette does not offer gid %s", gid)
		}
	}
}

func TestMapCanvas_HidingALayerIsAViewAndNotAnEdit(t *testing.T) {
	srv, _, sess, configured := mapServer(t)
	before, err := os.ReadFile(configured)
	if err != nil {
		t.Fatal(err)
	}

	body := mapPage(t, srv, "/forge/map?hide=0&map="+url.QueryEscape(configured))
	if strings.Contains(body, `data-testid="map-cell"`) {
		t.Errorf("the hidden layer was still drawn:\n%s", body)
	}
	if !strings.Contains(body, `data-hidden="true"`) {
		t.Error("the layer row does not show as hidden")
	}
	// The file is untouched and the session is clean: an eye is not an edit.
	after, err := os.ReadFile(configured)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("hiding a layer wrote to the file")
	}
	if dirty, _ := sess.Dirty(); len(dirty) != 0 {
		t.Errorf("hiding a layer marked the map unsaved: %v", dirty)
	}
}

func TestMapCanvas_SelectingATileIsInTheURLAndSurvivesAReload(t *testing.T) {
	srv, _, _, configured := mapServer(t)
	body := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(configured)+"&tile=2")

	if !strings.Contains(body, `data-testid="palette-tile-2"`) {
		t.Fatalf("the palette is missing:\n%s", body)
	}
	if !strings.Contains(body, `data-selected="true"`) {
		t.Errorf("the selected tile is not marked:\n%s", body)
	}
	if !strings.Contains(body, "tile 2") {
		t.Errorf("the status line does not name the selected tile:\n%s", body)
	}
}

// Every link in the mode carries the whole view, or looking at a different
// layer would deselect your brush.
func TestMapCanvas_LinksKeepTheRestOfTheView(t *testing.T) {
	srv, _, _, configured := mapServer(t)
	body := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(configured)+"&tile=2&hide=1")

	// The layer toggle keeps the tile and the map.
	if !strings.Contains(body, "tile=2") {
		t.Errorf("a link dropped the tile selection:\n%s", body)
	}
	if !strings.Contains(body, "hide=1") {
		t.Errorf("a link dropped the hidden layers:\n%s", body)
	}
}

// The canvas is on a two-second stream whose identical-patch suppression
// depends on two renders of an unchanged map agreeing exactly.
func TestMapCanvas_TwoRendersOfAnUnchangedMapAreIdentical(t *testing.T) {
	srv, _, _, configured := mapServer(t)
	at := "/forge/map?map=" + url.QueryEscape(configured) + "&tile=1"
	first := mapPage(t, srv, at)
	for i := 0; i < 4; i++ {
		if got := mapPage(t, srv, at); got != first {
			t.Fatalf("render %d differs from the first", i+2)
		}
	}
}

func TestMapCanvas_AnUnresolvedGIDIsDrawnRatherThanSkipped(t *testing.T) {
	srv, _, sess, configured := mapServer(t)
	if err := sess.Edit(configured, func(d *tiled.Document) error {
		return d.SetLayerData(0, []uint32{1, 9999, 2, 1})
	}); err != nil {
		t.Fatal(err)
	}
	body := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(configured))

	if !strings.Contains(body, `data-testid="map-cell-unresolved"`) {
		t.Errorf("the unresolved cell was skipped rather than shown:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="map-canvas-problem"`) {
		t.Error("the status line does not say what is wrong")
	}
	// The three good cells still draw.
	if got := strings.Count(body, `data-testid="map-cell"`); got != 3 {
		t.Errorf("got %d drawn cells, want 3", got)
	}
}

func TestMapCanvas_TheLensOffersOnlyAuthored(t *testing.T) {
	srv, _, _, _ := mapServer(t)
	body := mapPage(t, srv, "/forge/map")

	for _, want := range []string{
		`data-testid="lens-authored"`,
		`data-testid="lens-live"`,
		`data-testid="lens-replay"`,
		"Epic 18",
		"Epic 19",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the source lens is missing %s:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `<button type="button" class="segmented__off" disabled`) {
		t.Error("LIVE and REPLAY are not disabled")
	}
}

func TestMapCanvas_TheStatusLineSaysHowBigTheMapIs(t *testing.T) {
	srv, _, _, _ := mapServer(t)
	body := mapPage(t, srv, "/forge/map")

	if !strings.Contains(body, "2×2 cells") {
		t.Errorf("the status line does not give the map's size:\n%s", body)
	}
	if !strings.Contains(body, "16×16 px") {
		t.Errorf("the status line does not give the tile size:\n%s", body)
	}
	if !strings.Contains(body, "1 layer") {
		t.Errorf("the status line does not give the layer count:\n%s", body)
	}
}

// The layer a stroke lands on. Story 4 paints into it, and its own notes say
// the panel showing which one is active is not decoration.
func TestMapCanvas_TheActiveLayerIsMarkedAndSelectable(t *testing.T) {
	srv, _, _, configured := mapServer(t)
	body := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(configured))

	// The first layer is active by default: a map always has somewhere to paint.
	if !strings.Contains(body, `data-active="true"`) {
		t.Errorf("no layer is marked active:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="layer-select-ground"`) {
		t.Errorf("a layer cannot be made active:\n%s", body)
	}
	if !strings.Contains(body, "painting ground") {
		t.Errorf("the status line does not say where a stroke would land:\n%s", body)
	}
}

// Two controls on one row, because conflating them means you cannot look at a
// layer without also painting into it.
func TestMapCanvas_TheEyeAndTheNameAreDifferentControls(t *testing.T) {
	srv, _, _, configured := mapServer(t)
	body := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(configured))

	if !strings.Contains(body, `data-testid="layer-eye-ground"`) {
		t.Errorf("the row has no visibility toggle:\n%s", body)
	}
	// The eye's link changes hide and not layer; the name's does the reverse.
	if !strings.Contains(body, "hide=0") {
		t.Errorf("the eye does not toggle visibility:\n%s", body)
	}
}

// A layer the file hides can be looked at. It could not be while Forge's view
// state was OR'd with the file: the eye changed the URL, flipped nothing, and
// never changed its own state.
func TestMapCanvas_ALayerTheFileHidesStartsHiddenAndCanBeShown(t *testing.T) {
	srv, _, sess, configured := mapServer(t)
	raw, err := os.ReadFile(configured)
	if err != nil {
		t.Fatal(err)
	}
	hidden := strings.Replace(string(raw), `<layer id="1" name="ground"`,
		`<layer id="1" name="ground" visible="0"`, 1)
	if hidden == string(raw) {
		t.Fatal("the fixture's layer could not be hidden")
	}
	if err := os.WriteFile(configured, []byte(hidden), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sess.Reload(configured); err != nil {
		t.Fatal(err)
	}

	at := "/forge/map?map=" + url.QueryEscape(configured)
	body := mapPage(t, srv, at)
	if strings.Contains(body, `data-testid="map-cell"`) {
		t.Errorf("a layer the file hides was drawn on first load:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="layer-file-hidden-ground"`) {
		t.Errorf("the row does not say the file hides it:\n%s", body)
	}

	// Turning it on: the URL says which layers are hidden, and this one is not.
	body = mapPage(t, srv, at+"&hide=")
	if !strings.Contains(body, `data-testid="map-cell"`) {
		t.Errorf("a layer the file hides could not be shown:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="layer-file-hidden-ground"`) {
		t.Error("showing it claimed the file no longer hides it")
	}
}

// Tiled's other way of making a layer invisible. An open eye over a layer that
// draws nothing, with no explanation, is worse than either state alone.
func TestMapCanvas_ALayerAtZeroOpacitySaysSo(t *testing.T) {
	srv, _, sess, configured := mapServer(t)
	raw, err := os.ReadFile(configured)
	if err != nil {
		t.Fatal(err)
	}
	ghost := strings.Replace(string(raw), `<layer id="1" name="ground"`,
		`<layer id="1" name="ground" opacity="0"`, 1)
	if err := os.WriteFile(configured, []byte(ghost), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sess.Reload(configured); err != nil {
		t.Fatal(err)
	}

	body := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(configured))
	if !strings.Contains(body, `data-testid="layer-transparent-ground"`) {
		t.Errorf("a fully transparent layer is not marked:\n%s", body)
	}
	if strings.Contains(body, `data-testid="map-cell"`) {
		t.Error("a fully transparent layer was drawn")
	}
}

// Zoom is in the URL like every other part of the view, and the control marks
// the scale actually in force — which for a view that has asked for nothing is
// the fitted one, not the parameter.
func TestMapCanvas_ZoomIsAControlAndIsInTheURL(t *testing.T) {
	srv, _, _, configured := mapServer(t)
	at := "/forge/map?map=" + url.QueryEscape(configured)

	body := mapPage(t, srv, at)
	if !strings.Contains(body, `data-testid="zoom-control"`) {
		t.Fatalf("there is no zoom control:\n%s", body)
	}
	// The fixture is 2x2 at 16px, so it fits at the largest step FitScale
	// offers and the control says so without anyone asking.
	if !strings.Contains(body, `data-testid="zoom-4" data-current="true"`) {
		t.Errorf("the control does not mark the fitted scale:\n%s", body)
	}

	body = mapPage(t, srv, at+"&zoom=1")
	if !strings.Contains(body, `data-testid="zoom-1" data-current="true"`) {
		t.Errorf("asking for 1x did not select it:\n%s", body)
	}
	if !strings.Contains(body, "1× · 16px cells") {
		t.Errorf("the status line does not say what a cell comes to:\n%s", body)
	}

	// And at the fitted default the cell is four times its size in the file,
	// which is the half of the readout that 1x cannot show.
	body = mapPage(t, srv, at)
	if !strings.Contains(body, "4× · 64px cells") {
		t.Errorf("the status line does not scale the cell size:\n%s", body)
	}
	if !strings.Contains(body, `data-cell-w="64"`) {
		t.Errorf("the canvas does not carry its cell size for Story 5:\n%s", body)
	}
}

// A zoom the control cannot produce is ignored rather than honoured: a
// hand-typed 137 would draw one tile the size of the panel.
func TestMapCanvas_AZoomOutsideTheStepsIsIgnored(t *testing.T) {
	srv, _, _, configured := mapServer(t)
	at := "/forge/map?map=" + url.QueryEscape(configured)
	for _, bad := range []string{"137", "0", "-2", "nonsense", "2.5"} {
		body := mapPage(t, srv, at+"&zoom="+url.QueryEscape(bad))
		if !strings.Contains(body, `data-testid="zoom-4" data-current="true"`) {
			t.Errorf("zoom=%q was honoured:\n%s", bad, body)
		}
	}
}

// A map whose tilesets do not resolve still renders — with the reason on the
// panel above — and there is nothing to zoom. A control marking none of its six
// steps is worse than no control.
func TestMapCanvas_AMapThatWillNotResolveHasNoZoomControl(t *testing.T) {
	srv, _, sess, configured := mapServer(t)
	if err := os.Remove(filepath.Join(filepath.Dir(configured), "fixture.tsx")); err != nil {
		t.Fatal(err)
	}
	if err := sess.Reload(configured); err != nil {
		t.Fatal(err)
	}

	body := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(configured))
	if !strings.Contains(body, `data-testid="map-editor"`) {
		t.Fatalf("the mode stopped rendering:\n%s", body)
	}
	if strings.Contains(body, `data-testid="zoom-control"`) {
		t.Errorf("a map with nothing drawn offers a zoom control:\n%s", body)
	}
	// Not a row of zeros: "0×0 cells" reads as a map that is 0x0, which is a
	// different and untrue thing from a map that could not be drawn.
	if strings.Contains(body, "0×0 cells") || strings.Contains(body, "0× · 0px") {
		t.Errorf("the status line reports a map of no size:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="map-not-drawn"`) {
		t.Errorf("the status line does not say the map is not drawn:\n%s", body)
	}
	// And the reason is on the panel, which is what makes this a broken map
	// rather than an empty one.
	if !strings.Contains(body, "fixture.tsx") {
		t.Errorf("the reason is not shown:\n%s", body)
	}
}
