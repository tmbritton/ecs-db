package server

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
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
	s := New(Config{Addr: "127.0.0.1:0", Session: schemaSession, AnimationSession: anim, TileSize: 16}, testFS())
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

func TestSpritesMode_AnimationAndBindingEditsRemainDraftsUntilSave(t *testing.T) {
	srv, s, path := animationServer(t)
	for _, tt := range []struct {
		op, name, value, want string
	}{
		{"fps", "idle", "12.5", "fps = 12.5"},
		{"frames", "idle", "2,0,2", "frames = [2, 0, 2]"},
		{"loop", "idle", "false", "loop = false"},
		{"sheet", "idle", "sprites/alternate.png", `sheet = "sprites/alternate.png"`},
		{"binding-sheet", "Player", "sprites/other.png", `sheet = "sprites/other.png"`},
		{"binding-name", "Player", "Hero", `entity_type = "Hero"`},
	} {
		t.Run(tt.op, func(t *testing.T) {
			q := url.Values{"file": {path}, "name": {tt.name}, "value": {tt.value}}
			response, err := http.Post(srv.URL+"/forge/sprites/edit/"+tt.op+"?"+q.Encode(), "application/x-www-form-urlencoded", nil)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				t.Errorf("edit status = %s", response.Status)
			}
			if err := s.cfg.AnimationSession.Read(func(doc *animations.Document) error {
				if !strings.Contains(string(doc.Bytes()), tt.want) {
					t.Errorf("edit did not reach working TOML: %s", doc.Bytes())
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if disk, err := os.ReadFile(path); err != nil || string(disk) != serverAnimations {
				t.Errorf("edit wrote disk before Save: %s, %v", disk, err)
			}
		})
	}
}

func TestSpritesMode_InvalidEditAndGuessedFileDoNotChangeWorkingTOML(t *testing.T) {
	srv, s, path := animationServer(t)
	for _, tt := range []struct{ name, file, op, value string }{
		{"bad fps", path, "fps", "-1"},
		{"bad frames", path, "frames", "0,nope"},
		{"bad loop", path, "loop", "maybe"},
		{"later file", filepath.Join(filepath.Dir(filepath.Dir(path)), "later", "animations.toml"), "fps", "5"},
		{"later mod sheet", path, "sheet", filepath.Join(filepath.Dir(filepath.Dir(path)), "later", "sprites", "later.png")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			q := url.Values{"file": {tt.file}, "name": {"idle"}, "value": {tt.value}}
			response, err := http.Post(srv.URL+"/forge/sprites/edit/"+tt.op+"?"+q.Encode(), "application/x-www-form-urlencoded", nil)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if problem, _ := s.lastEditProblem(); problem == "" {
				t.Error("invalid edit was not reported")
			}
			if err := s.cfg.AnimationSession.Read(func(doc *animations.Document) error {
				if string(doc.Bytes()) != serverAnimations {
					t.Errorf("invalid edit changed working file: %s", doc.Bytes())
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSpritesMode_CreatesAnimationAndBindingAsIndependentDrafts(t *testing.T) {
	srv, s, path := animationServer(t)
	for _, tt := range []struct {
		op, name, sheet, want string
	}{
		{"animation", "walk", "sprites/player.png", `name = "walk"`},
		{"binding", "Goblin", "sprites/goblin.png", `entity_type = "Goblin"`},
	} {
		t.Run(tt.op, func(t *testing.T) {
			q := url.Values{"file": {path}, "name": {tt.name}, "sheet": {tt.sheet}, "frames": {"0,1"}, "fps": {"8"}, "loop": {"true"}}
			response, err := http.Post(srv.URL+"/forge/sprites/create/"+tt.op+"?"+q.Encode(), "application/x-www-form-urlencoded", nil)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if err := s.cfg.AnimationSession.Read(func(doc *animations.Document) error {
				if !strings.Contains(string(doc.Bytes()), tt.want) {
					t.Errorf("new %s missing from draft: %s", tt.op, doc.Bytes())
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	if disk, err := os.ReadFile(path); err != nil || string(disk) != serverAnimations {
		t.Errorf("creation wrote disk before Save: %s, %v", disk, err)
	}
}

func TestSpritesImage_OnlyServesNamedArtInTheFirstAssetsMod(t *testing.T) {
	srv, _, path := animationServer(t)
	for _, folder := range []string{filepath.Dir(path), filepath.Join(filepath.Dir(filepath.Dir(path)), "later")} {
		if err := os.Mkdir(filepath.Join(folder, "sprites"), 0o700); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(folder, "sprites", "player.png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 32, 16))); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		name, sheet string
		want        int
	}{
		{"named", "sprites/player.png", http.StatusOK},
		{"guessed", "sprites/guess.png", http.StatusNotFound},
		{"later mod", filepath.Join(filepath.Dir(filepath.Dir(path)), "later", "sprites", "player.png"), http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + "/forge/sprites/image?sheet=" + url.QueryEscape(tt.sheet))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Errorf("image status = %s; want %d", resp.Status, tt.want)
			}
			if tt.want == http.StatusOK && (resp.Header.Get("Content-Type") != "image/png" || resp.Header.Get("X-Content-Type-Options") != "nosniff") {
				t.Errorf("image response headers: %v", resp.Header)
			}
		})
	}
	page := mapPage(t, srv, "/forge/sprites?file="+url.QueryEscape(path))
	if !strings.Contains(page, `data-testid="sprite-strip"`) || strings.Contains(page, `data-testid="sprite-preview-problem"`) {
		t.Errorf("active sheet not previewed despite image route working: %s", page)
	}
}

func TestSpritesMode_SelectedAnimationSurvivesItsStreamQuery(t *testing.T) {
	srv, s, path := animationServer(t)
	if err := s.cfg.AnimationSession.Edit(func(d *animations.Document) error {
		return d.AddAnimation("walk", "sprites/player.png", []int{1, 0}, 4, true)
	}); err != nil {
		t.Fatal(err)
	}
	page := mapPage(t, srv, "/forge/sprites?file="+url.QueryEscape(path)+"&animation=walk")
	if !strings.Contains(page, `data-testid="sprites-selected-animation"`) || !strings.Contains(page, `data-testid="sprite-frames"`) || !strings.Contains(page, "animation=walk") {
		t.Fatalf("selected sprite and its stream query were lost: %s", page)
	}
}

func TestSpritesMode_RenamedAnimationKeepsItsSelectionUntilReload(t *testing.T) {
	srv, _, path := animationServer(t)
	q := url.Values{"file": {path}, "name": {"idle"}, "value": {"stand"}}
	resp, err := http.Post(srv.URL+"/forge/sprites/edit/name?"+q.Encode(), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	page := mapPage(t, srv, "/forge/sprites?animation=idle")
	if !strings.Contains(page, `data-testid="animation-stand" aria-current="true"`) || !strings.Contains(page, `data-testid="sprite-name" value="stand"`) {
		t.Errorf("rename stranded selection on the old name: %s", page)
	}
	resp, err = http.Post(srv.URL+"/forge/sprites/discard?file="+url.QueryEscape(path), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	page = mapPage(t, srv, "/forge/sprites?animation=idle")
	if !strings.Contains(page, `data-testid="animation-idle" aria-current="true"`) {
		t.Error("discard left the selection following an abandoned rename")
	}
}

func TestSpritesMode_AReusedOldNameCanBeSelected(t *testing.T) {
	srv, s, path := animationServer(t)
	if err := s.cfg.AnimationSession.Edit(func(d *animations.Document) error {
		if err := d.RenameAnimation("idle", "stand"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.recordRename(renameAnimationKind, "idle", "stand")
	q := url.Values{"file": {path}, "name": {"idle"}, "sheet": {"sprites/player.png"}, "frames": {"0"}, "fps": {"4"}, "loop": {"true"}}
	resp, err := http.Post(srv.URL+"/forge/sprites/create/animation?"+q.Encode(), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	page := mapPage(t, srv, "/forge/sprites?animation=idle")
	if !strings.Contains(page, `data-testid="animation-idle" aria-current="true"`) || !strings.Contains(page, `data-testid="sprite-fps" value="4"`) {
		t.Errorf("a new animation with the old name was redirected to stand: %s", page)
	}
}

func TestSpritesMode_RenamingBackDoesNotLoopTheSelection(t *testing.T) {
	srv, _, path := animationServer(t)
	for _, tt := range []struct{ from, to string }{{"idle", "stand"}, {"stand", "idle"}} {
		q := url.Values{"file": {path}, "name": {tt.from}, "value": {tt.to}}
		resp, err := http.Post(srv.URL+"/forge/sprites/edit/name?"+q.Encode(), "application/x-www-form-urlencoded", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	page := mapPage(t, srv, "/forge/sprites?animation=idle")
	if !strings.Contains(page, `data-testid="animation-idle" aria-current="true"`) || !strings.Contains(page, `data-testid="sprite-name" value="idle"`) {
		t.Errorf("renaming back to idle stranded the selected animation: %s", page)
	}
}

func TestSpritesMode_ReusedIntermediateNameDoesNotRetargetTheOriginalEditor(t *testing.T) {
	srv, _, path := animationServer(t)
	for _, tt := range []struct{ from, to string }{{"idle", "stand"}, {"stand", "walk"}} {
		q := url.Values{"file": {path}, "name": {tt.from}, "value": {tt.to}}
		resp, err := http.Post(srv.URL+"/forge/sprites/edit/name?"+q.Encode(), "application/x-www-form-urlencoded", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	q := url.Values{"file": {path}, "name": {"stand"}, "sheet": {"sprites/player.png"}, "frames": {"0"}, "fps": {"4"}, "loop": {"true"}}
	resp, err := http.Post(srv.URL+"/forge/sprites/create/animation?"+q.Encode(), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	page := mapPage(t, srv, "/forge/sprites?animation=idle")
	if !strings.Contains(page, `data-testid="animation-walk" aria-current="true"`) || !strings.Contains(page, `data-testid="sprite-name" value="walk"`) {
		t.Errorf("original editor switched to newly created stand: %s", page)
	}
	page = mapPage(t, srv, "/forge/sprites?animation=stand")
	if !strings.Contains(page, `data-testid="animation-stand" aria-current="true"`) {
		t.Error("new stand is not selectable")
	}
}

func TestSpritesMode_RefusesOutOfRangeFramesWhenSheetIsAvailable(t *testing.T) {
	srv, s, path := animationServer(t)
	folder := filepath.Join(filepath.Dir(path), "sprites")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(folder, "player.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 32, 16))); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, endpoint, value string
	}{
		{"existing frames", "/forge/sprites/edit/frames", "99"},
		{"new animation", "/forge/sprites/create/animation", "99"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			name := "idle"
			if tt.endpoint == "/forge/sprites/create/animation" {
				name = "new"
			}
			q := url.Values{"file": {path}, "name": {name}, "value": {tt.value}, "sheet": {"sprites/player.png"}, "frames": {tt.value}, "fps": {"8"}, "loop": {"true"}}
			resp, err := http.Post(srv.URL+tt.endpoint+"?"+q.Encode(), "application/x-www-form-urlencoded", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if problem, _ := s.lastEditProblem(); !strings.Contains(problem, "column 99") {
				t.Errorf("out-of-bounds frame was not refused: %s", problem)
			}
			if err := s.cfg.AnimationSession.Read(func(d *animations.Document) error {
				if string(d.Bytes()) != serverAnimations {
					t.Errorf("out-of-bounds edit reached the draft: %s", d.Bytes())
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSpritesMode_RejectsNonPNGEvenInsideSelectedSprites(t *testing.T) {
	srv, s, path := animationServer(t)
	q := url.Values{"file": {path}, "name": {"idle"}, "value": {"sprites/player.gif"}}
	resp, err := http.Post(srv.URL+"/forge/sprites/edit/sheet?"+q.Encode(), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if problem, _ := s.lastEditProblem(); !strings.Contains(problem, "PNG") {
		t.Errorf("game-incompatible format was not refused: %s", problem)
	}
}

func TestSpritesMode_WarnsWhenAnEntityBindingCannotPlayTheSelectedColumns(t *testing.T) {
	srv, s, path := animationServer(t)
	folder := filepath.Join(filepath.Dir(path), "sprites")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		width int
	}{{"player.png", 32}, {"short.png", 16}} {
		file, err := os.Create(filepath.Join(folder, tt.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, tt.width, 16))); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.cfg.AnimationSession.Edit(func(d *animations.Document) error {
		if err := d.SetFrames("idle", []int{1}); err != nil {
			return err
		}
		return d.SetBindingSheet("Player", "sprites/short.png")
	}); err != nil {
		t.Fatal(err)
	}
	page := mapPage(t, srv, "/forge/sprites?animation=idle")
	for _, want := range []string{`data-testid="sprite-binding-warning-Player"`, "If Player uses this animation", "column 1"} {
		if !strings.Contains(page, want) {
			t.Errorf("bound sheet mismatch was not explained (missing %s)", want)
		}
	}
	// The renderer crops row zero of a taller sheet. Such a binding is not an
	// editor-authored one-row strip, but it can play the selected columns.
	f, err := os.Create(filepath.Join(folder, "short.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 32, 32))); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	page = mapPage(t, srv, "/forge/sprites?animation=idle")
	if strings.Contains(page, `data-testid="sprite-binding-warning-Player"`) {
		t.Error("a playable first row in a taller bound sheet was wrongly rejected")
	}
}

func TestSpritesImportDialog_ListsProjectLocalImages(t *testing.T) {
	srv, _, path := animationServer(t)
	projectRoot := filepath.Dir(filepath.Dir(path))
	source := filepath.Join(projectRoot, "source.png")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 32, 16))); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	page := mapPage(t, srv, "/forge/sprites?import=1")
	for _, want := range []string{`data-testid="sprite-import-dialog"`, `data-testid="sprite-import-source"`, source, `data-testid="sprite-import-submit"`} {
		if !strings.Contains(page, want) {
			t.Errorf("import dialog omitted %q", want)
		}
	}
}

func TestSpritesImport_CopiesArtAndCreatesDraftButRequiresSaveForTOML(t *testing.T) {
	srv, s, path := animationServer(t)
	projectRoot := filepath.Dir(filepath.Dir(path))
	source := filepath.Join(projectRoot, "source.png")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 32, 16))); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"source": {source}, "animation": {"run"}}
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.PostForm(srv.URL+"/forge/sprites/import?file="+url.QueryEscape(path), form)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "animation=run") {
		t.Fatalf("successful import did not select new animation: %s, %s", resp.Status, resp.Header.Get("Location"))
	}
	copyPath := filepath.Join(projectRoot, "main", "sprites", "source.png")
	if copied, err := os.ReadFile(copyPath); err != nil || !bytes.Equal(copied, mustReadServer(t, source)) {
		t.Errorf("imported art was not copied: %v", err)
	}
	if err := s.cfg.AnimationSession.Read(func(d *animations.Document) error {
		if !strings.Contains(string(d.Bytes()), "frames = [0, 1]") {
			t.Errorf("import did not create two frames in draft: %s", d.Bytes())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if disk := mustReadServer(t, path); string(disk) != serverAnimations {
		t.Errorf("import wrote animation TOML without Save: %s", disk)
	}
}

func TestSpritesImport_UnknownEntityTypeRefusesBeforeCopy(t *testing.T) {
	srv, s, path := animationServer(t)
	projectRoot := filepath.Dir(filepath.Dir(path))
	source := filepath.Join(projectRoot, "source.png")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.PostForm(srv.URL+"/forge/sprites/import?file="+url.QueryEscape(path), url.Values{
		"source": {source}, "animation": {"run"}, "entity_type": {"Unknown"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "import=1") {
		t.Fatalf("refusal did not return to the dialog: %s, %s", resp.Status, resp.Header.Get("Location"))
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "main", "sprites", "source.png")); !os.IsNotExist(err) {
		t.Errorf("invalid entity binding copied a sprite: %v", err)
	}
	if err := s.cfg.AnimationSession.Read(func(d *animations.Document) error {
		if string(d.Bytes()) != serverAnimations {
			t.Errorf("unknown entity type changed the draft: %s", d.Bytes())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func mustReadServer(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
