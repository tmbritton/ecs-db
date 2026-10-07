package server

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

func TestMapValidation_MapIDWarningAndWorkingEdit(t *testing.T) {
	srv, s, sess, path := mapServer(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `<property name="mapId" value="level1"/>`, "", 1))
	// The fixture must genuinely lose its ID; a test of a warning that never
	// removed the property would otherwise be a permanently green no-op.
	if strings.Contains(string(raw), `name="mapId"`) {
		t.Fatal("fixture still has its mapId")
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sess.Reload(path); err != nil {
		t.Fatal(err)
	}
	at := "/forge/map?map=" + url.QueryEscape(path)
	if got := mapPage(t, srv, at); !strings.Contains(got, `data-testid="map-id-warning"`) ||
		!strings.Contains(got, "spawns are filed under its path") {
		t.Error("the engine's missing-mapId warning is not shown while editing")
	}
	if code := postSignals(t, srv, "/forge/map/identity?map="+url.QueryEscape(path)+"&id=stable", `{}`); code != 204 {
		t.Fatalf("edit = %d: %s", code, problemOf(s))
	}
	if got := mapPage(t, srv, at); strings.Contains(got, `data-testid="map-id-warning"`) ||
		!strings.Contains(got, `value="stable"`) {
		t.Error("working mapId edit did not clear its warning and fill the field")
	}
	var id string
	if err := sess.Read(path, func(d *tiled.Document) error {
		m, err := d.Map()
		if err == nil {
			id = m.Properties.Get(tiled.PropMapID)
		}
		return err
	}); err != nil || id != "stable" {
		t.Fatalf("mapId was not edited in the working TMX: %q, %v", id, err)
	}
	if err := sess.Discard(path); err != nil {
		t.Fatal(err)
	}
	if got := mapPage(t, srv, at); !strings.Contains(got, `data-testid="map-id-warning"`) {
		t.Error("discard did not restore the original missing-mapId warning")
	}
}

func TestMapValidation_DuplicateMapIDNamesBothFiles(t *testing.T) {
	srv, _, sess, path := mapServer(t)
	other := strings.Replace(path, "level1.tmx", "arena.tmx", 1)
	if err := sess.Edit(other, func(d *tiled.Document) error { return d.SetMapID("level1") }); err != nil {
		t.Fatal(err)
	}
	body := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(path))
	if !strings.Contains(body, `data-testid="map-identity-conflict"`) ||
		!strings.Contains(body, "level1.tmx") || !strings.Contains(body, "arena.tmx") {
		t.Errorf("working-map ID collision is not named on the page: %s", body)
	}
}

func TestMapValidation_UnresolvedTilesetDoesNotHideCanvasOrSpawns(t *testing.T) {
	srv, _, sess, path := mapServer(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `source="fixture.tsx"`, `source="lost.tsx"`, 1))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sess.Reload(path); err != nil {
		t.Fatal(err)
	}
	body := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(path))
	for _, want := range []string{
		`data-testid="map-canvas"`, `data-testid="map-cell-unresolved"`,
		`data-testid="map-tileset-problem-0"`, "lost.tsx", `data-testid="object-group-spawns"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("partial preview missing %q", want)
		}
	}
}

func TestMapValidation_DuplicateSpawnDeepLinkCannotSelectAnArbitraryClaimant(t *testing.T) {
	srv, _, sess, path := mapServer(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `<objectgroup id="2" name="spawns"/>`,
		`<objectgroup id="2" name="spawns"><object id="1" type="Player" x="0" y="0"/><object id="1" type="Player" x="16" y="0"/></objectgroup>`, 1))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sess.Reload(path); err != nil {
		t.Fatal(err)
	}
	body := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(path)+"&spawn=1")
	if !strings.Contains(body, `data-testid="spawn-ambiguous"`) || strings.Contains(body, `data-testid="spawn-delete"`) {
		t.Error("duplicate spawn id deep link selects an arbitrary object with destructive controls")
	}
}

func TestMapValidation_UntypedDuplicateAlsoBlocksSpawnDeepLink(t *testing.T) {
	srv, _, sess, path := mapServer(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `<objectgroup id="2" name="spawns"/>`,
		`<objectgroup id="2" name="spawns"><object id="1" type="Player" x="0" y="0"/><object id="1" name="annotation" x="16" y="0"/></objectgroup>`, 1))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sess.Reload(path); err != nil {
		t.Fatal(err)
	}
	body := mapPage(t, srv, "/forge/map?map="+url.QueryEscape(path)+"&spawn=1")
	if !strings.Contains(body, `data-testid="spawn-ambiguous"`) || strings.Contains(body, `data-testid="spawn-delete"`) {
		t.Error("untyped claimant left a typed spawn's ID edit controls reachable")
	}
}
