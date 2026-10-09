package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/animations"
	"github.com/tmbritton/ecs-db/internal/forge/session"
)

const serverAnimations = `# author note
[[animation]]
name = "idle"
sheet = "sprites/player.png"
frames = [0]
fps = 8
loop = true

[[entity_asset]]
entity_type = "Player"
sheet = "sprites/player.png"
`

func animationServer(t *testing.T) (*httptest.Server, *Server, string) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"main", "later"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "main", "animations.toml")
	if err := os.WriteFile(path, []byte(serverAnimations), 0o600); err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(root, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(sessionSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	schemaSession, err := session.Open(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	anim := animations.Open(animations.Config{Root: root, Mods: []animations.AssetMod{
		{Name: "main", Assets: filepath.Join(root, "main")},
		{Name: "later", Assets: filepath.Join(root, "later")},
	}})
	s := New(Config{Addr: "127.0.0.1:0", Session: schemaSession, AnimationSession: anim}, testFS())
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	return srv, s, path
}

func TestSpritesMode_OnlyFirstAssetsModIsGameLoaded(t *testing.T) {
	srv, _, path := animationServer(t)
	body := mapPage(t, srv, "/forge/sprites?file="+url.QueryEscape(path))
	for _, want := range []string{
		`data-testid="sprites-mode"`, `data-testid="animation-file"`, `data-testid="sprites-active-mod"`,
		"main", "later", "not loaded", "idle", `data-testid="save-footer"`,
		"hot-reloads after Save",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("SPRT session page omitted %s", want)
		}
	}
	if strings.Contains(body, `data-testid="mode-stub"`) {
		t.Error("SPRT still displays a stub")
	}
}

func TestSpritesMode_ReloadsExternallyChangedActiveFileAndRejectsGuessedSavePath(t *testing.T) {
	srv, s, path := animationServer(t)
	_ = mapPage(t, srv, "/forge/sprites?file="+url.QueryEscape(path))
	modified := strings.Replace(serverAnimations, `name = "idle"`, `name = "rest"`, 1)
	if err := os.WriteFile(path, []byte(modified), 0o600); err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(srv.URL+"/forge/sprites/reload?file="+url.QueryEscape(path), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	body := mapPage(t, srv, "/forge/sprites?file="+url.QueryEscape(path))
	if response.StatusCode != http.StatusNoContent || !strings.Contains(body, "rest") || strings.Contains(body, `save-footer--dirty`) {
		t.Errorf("Reload did not take external file: %s, body %s", response.Status, body)
	}
	guessed := filepath.Join(filepath.Dir(path), "secret.toml")
	response, err = http.Post(srv.URL+"/forge/sprites/save?file="+url.QueryEscape(guessed), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	problem, _ := s.lastEditProblem()
	if problem == "" {
		t.Error("guessed animation path was accepted for Save")
	}
}

func TestSpritesMode_ConflictButtonsResolveTheActiveFileNotSchema(t *testing.T) {
	srv, s, path := animationServer(t)
	if err := s.cfg.AnimationSession.Edit(func(doc *animations.Document) error { return doc.SetFPS("idle", 24) }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(serverAnimations, "fps = 8", "fps = 16", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(srv.URL+"/forge/sprites/save?file="+url.QueryEscape(path), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	body := mapPage(t, srv, "/forge/sprites?file="+url.QueryEscape(path))
	for _, want := range []string{`data-testid="conflict-reload"`, `data-testid="conflict-overwrite"`, "/forge/sprites/reload?file=", "/forge/sprites/save/overwrite?file="} {
		if !strings.Contains(body, want) {
			t.Errorf("animation conflict omitted %q", want)
		}
	}
}

func TestSpritesMode_MissingActiveFileCanBeCreatedInTheSelectedMod(t *testing.T) {
	srv, s, path := animationServer(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(path))
	s.cfg.AnimationSession = animations.Open(animations.Config{Root: root, Mods: []animations.AssetMod{{Name: "main", Assets: filepath.Dir(path)}}})
	body := mapPage(t, srv, "/forge/sprites")
	if !strings.Contains(body, `data-testid="sprites-create-file"`) || !strings.Contains(body, `data-testid="sprites-file-problem"`) {
		t.Error("missing loaded animation file had no create action and explanation")
	}
	response, err := http.Post(srv.URL+"/forge/sprites/create?file="+url.QueryEscape(path), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("create active file: %s", response.Status)
	}
	body = mapPage(t, srv, "/forge/sprites")
	if !strings.Contains(body, `data-testid="sprites-animation-count"`) || strings.Contains(body, `data-testid="sprites-create-file"`) {
		t.Error("newly created animation file did not open as an empty session")
	}
}

func TestSpritesMode_ProvidesReloadAfterInitialOrLaterMalformedTOML(t *testing.T) {
	for _, initiallyBroken := range []bool{true, false} {
		t.Run(fmt.Sprintf("initially broken=%v", initiallyBroken), func(t *testing.T) {
			srv, s, path := animationServer(t)
			if err := os.WriteFile(path, []byte("[[animation]\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if initiallyBroken {
				s.cfg.AnimationSession = animations.Open(animations.Config{
					Root: filepath.Dir(filepath.Dir(path)), Mods: []animations.AssetMod{{Name: "main", Assets: filepath.Dir(path)}},
				})
			} else if err := s.cfg.AnimationSession.Reload(); err == nil {
				t.Error("a malformed external edit was accepted")
			}
			body := mapPage(t, srv, "/forge/sprites")
			if !strings.Contains(body, `data-testid="save-footer-reload"`) || !strings.Contains(body, `data-testid="sprites-file-problem"`) {
				t.Error("a bad active TOML had no way to reload after repair")
			}
			if err := os.WriteFile(path, []byte(serverAnimations), 0o600); err != nil {
				t.Fatal(err)
			}
			response, err := http.Post(srv.URL+"/forge/sprites/reload?file="+url.QueryEscape(path), "application/x-www-form-urlencoded", nil)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			body = mapPage(t, srv, "/forge/sprites")
			if !strings.Contains(body, `data-testid="animation-idle"`) || strings.Contains(body, `data-testid="sprites-file-problem"`) {
				t.Error("repaired animation file did not reopen in SPRT")
			}
		})
	}
}

func TestSpritesMode_DirtyWorkingCopyIsReachableAfterMalformedExternalReload(t *testing.T) {
	srv, s, path := animationServer(t)
	if err := s.cfg.AnimationSession.Edit(func(d *animations.Document) error { return d.SetFPS("idle", 24) }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[[animation]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(srv.URL+"/forge/sprites/reload?file="+url.QueryEscape(path), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	body := mapPage(t, srv, "/forge/sprites")
	for _, want := range []string{`data-testid="save-footer-restore"`, `data-testid="save-footer-reload"`, "unsaved working copy"} {
		if !strings.Contains(body, want) {
			t.Errorf("failed Reload concealed held edits: missing %s", want)
		}
	}
	response, err = http.Post(srv.URL+"/forge/sprites/save/overwrite?file="+url.QueryEscape(path), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	disk, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(disk), "fps = 24") || s.cfg.AnimationSession.Problem() != "" {
		t.Errorf("Keep my copy did not write the held valid TOML: %s, %v", disk, err)
	}
}
