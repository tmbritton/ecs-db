package animations

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/editable"
)

func animationProject(t *testing.T) (Config, string) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "a", "animations.toml")
	if err := os.WriteFile(path, []byte(sampleTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	return Config{Root: root, Mods: []AssetMod{
		{Name: "no-assets"},
		{Name: "active", Assets: filepath.Join(root, "a")},
		{Name: "later", Assets: filepath.Join(root, "b")},
	}}, path
}

func TestOpen_UsesTheFirstAssetsModEvenWhenOthersDeclareFiles(t *testing.T) {
	cfg, path := animationProject(t)
	if err := os.WriteFile(filepath.Join(cfg.Root, "b", "animations.toml"), []byte("[[animation]]\nname = 'later'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Open(cfg)
	if got := s.Path(); got != path || s.Active().Name != "active" || len(s.Later()) != 1 || s.Later()[0].Name != "later" {
		t.Fatalf("wrong assets mod selected: path %s active %+v later %+v", s.Path(), s.Active(), s.Later())
	}
	if err := s.Read(func(doc *Document) error {
		if doc.Animations()[0].Name != "idle" {
			t.Error("later mod's TOML shadowed the loaded file")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSession_UnchangedSaveDoesNotRewriteTheWatchedTOML(t *testing.T) {
	cfg, path := animationProject(t)
	s := Open(cfg)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("no-op Save replaced the watched TOML: %v", err)
	}
	if bytes, err := os.ReadFile(path); err != nil || string(bytes) != sampleTOML {
		t.Fatalf("no-op Save changed authored bytes: %s, %v", bytes, err)
	}
}

func TestSession_TracksDirtyConflictReloadDiscardAndOverwrite(t *testing.T) {
	cfg, path := animationProject(t)
	s := Open(cfg)
	if err := s.Edit(func(d *Document) error { return d.SetFPS("idle", 24) }); err != nil {
		t.Fatal(err)
	}
	if dirty, err := s.Dirty(); err != nil || !dirty {
		t.Fatalf("edit is not dirty: %v, %v", dirty, err)
	}
	if err := s.Discard(); err != nil {
		t.Fatal(err)
	}
	if dirty, err := s.Dirty(); err != nil || dirty {
		t.Fatalf("discard is not clean: %v, %v", dirty, err)
	}
	if err := s.Edit(func(d *Document) error { return d.SetFPS("idle", 24) }); err != nil {
		t.Fatal(err)
	}
	onDisk := strings.Replace(sampleTOML, "fps = 8 # playback note", "fps = 18 # playback note", 1)
	if err := os.WriteFile(path, []byte(onDisk), 0o600); err != nil {
		t.Fatal(err)
	}
	var conflict *editable.ConflictError
	if err := s.Save(); !errors.As(err, &conflict) {
		t.Fatalf("external change gave %v, want conflict", err)
	}
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := s.Read(func(d *Document) error {
		if d.Animations()[0].FPS != 18 {
			t.Error("Reload did not take the disk's animation")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Edit(func(d *Document) error { return d.SetFPS("idle", 32) }); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOverwriting(); err != nil {
		t.Fatal(err)
	}
	if bytes, err := os.ReadFile(path); err != nil || !strings.Contains(string(bytes), "fps = 32 # playback note") || !strings.Contains(string(bytes), "artist_hint") {
		t.Fatalf("overwrite lost authored TOML: %s, %v", bytes, err)
	}
}

func TestSession_MissingAndMalformedFileAreReportedWithoutLosingTheProject(t *testing.T) {
	cfg, path := animationProject(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s := Open(cfg)
	if s.Problem() == "" || s.Path() != path {
		t.Fatalf("missing active file was hidden: %s, %q", s.Path(), s.Problem())
	}
	if !s.Missing() {
		t.Error("a missing file offered no create action")
	}
	if err := s.Create(); err != nil {
		t.Fatal(err)
	}
	if err := s.Read(func(*Document) error { return nil }); err != nil {
		t.Fatalf("created animation file is not open: %v", err)
	}
	if s.Missing() {
		t.Error("a created file is still marked missing")
	}
	if err := os.WriteFile(path, []byte("[[animation]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err == nil {
		t.Error("malformed disk TOML silently replaced working value")
	}
	if err := s.Create(); err == nil {
		t.Error("Create overwrote a file that appeared on disk")
	}
}

func TestOpen_NoAssetsModOrOutOfProjectAssetsIsAProblem(t *testing.T) {
	for _, tt := range []struct {
		name string
		cfg  func(string) Config
	}{
		{"no assets", func(root string) Config { return Config{Root: root, Mods: []AssetMod{{Name: "logic"}}} }},
		{"external assets", func(root string) Config {
			return Config{Root: root, Mods: []AssetMod{{Name: "external", Assets: t.TempDir()}}}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := Open(tt.cfg(t.TempDir()))
			if s.Problem() == "" || s.Missing() {
				t.Fatalf("unsafe/missing assets mod was shown as a creatable file: %q", s.Problem())
			}
		})
	}
}

func TestSession_RejectsSwappedSymlinkOutsideProject(t *testing.T) {
	cfg, path := animationProject(t)
	s := Open(cfg)
	outside := filepath.Join(t.TempDir(), "outside.toml")
	if err := os.WriteFile(outside, []byte(sampleTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	for _, action := range []func() error{s.Save, s.SaveOverwriting, s.Reload, s.Discard} {
		if err := action(); err == nil {
			t.Error("editing session accepted a swapped external symlink")
		}
	}
}

func TestSession_DeletedHeldFileCanBeRecreatedFromUnsavedWork(t *testing.T) {
	cfg, path := animationProject(t)
	s := Open(cfg)
	if err := s.Edit(func(d *Document) error { return d.SetFPS("idle", 24) }); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !s.Disappeared() {
		t.Error("lost working file is not reachable as a disappeared file")
	}
	if err := s.Reload(); err == nil {
		t.Error("Reload pretended the deleted file still existed")
	}
	if err := s.SaveOverwriting(); err != nil {
		t.Fatalf("held unsaved work could not recreate the deleted file: %v", err)
	}
	if s.Problem() != "" {
		t.Errorf("recreated file still reports a deleted-file problem: %q", s.Problem())
	}
	if raw, err := os.ReadFile(path); err != nil || !strings.Contains(string(raw), "fps = 24 # playback note") {
		t.Errorf("recreated file lost the working edit: %s, %v", raw, err)
	}
}

func TestSession_DeletedCleanFileCanBeRecreatedWithoutInventingDirtyWork(t *testing.T) {
	cfg, path := animationProject(t)
	s := Open(cfg)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOverwriting(); err != nil {
		t.Fatal(err)
	}
	if bytes, err := os.ReadFile(path); err != nil || string(bytes) != sampleTOML {
		t.Fatalf("clean deleted file was not restored exactly: %s, %v", bytes, err)
	}
	if dirty, err := s.Dirty(); err != nil || dirty {
		t.Errorf("recreated unchanged file is dirty: %v, %v", dirty, err)
	}
}

func TestSession_SaveOverwritingRestoresCleanWorkAfterAnotherProcessRecreatesFile(t *testing.T) {
	cfg, path := animationProject(t)
	s := Open(cfg)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !s.Disappeared() {
		t.Fatal("missing file did not offer a restore action")
	}
	if err := os.WriteFile(path, []byte("# another process recreated this file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOverwriting(); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != sampleTOML {
		t.Fatalf("overwrite reported success without restoring held bytes: %s, %v", raw, err)
	}
}

func TestSession_OnlyServesImagesInsideTheSelectedSpriteRoot(t *testing.T) {
	cfg, _ := animationProject(t)
	sprites := filepath.Join(cfg.Root, "a", "sprites")
	if err := os.Mkdir(sprites, 0o700); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(sprites, "player.png")
	if err := os.WriteFile(image, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(sprites, "escape.png")); err != nil {
		t.Fatal(err)
	}
	s := Open(cfg)
	for _, tt := range []struct {
		name, source string
		ok           bool
	}{
		{"relative to assets", "sprites/player.png", true},
		{"relative to project", "a/sprites/player.png", true},
		{"absolute inside", image, true},
		{"symlink outside", "sprites/escape.png", false},
		{"absolute outside", outside, false},
		{"not an image", "animations.toml", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := s.AssetImage(tt.source)
			if tt.ok && (err != nil || resolved != image) || !tt.ok && err == nil {
				t.Errorf("AssetImage(%q) = %q, %v", tt.source, resolved, err)
			}
		})
	}
}

func TestSession_RejectsSpriteRootSymlinkIntoAnotherMod(t *testing.T) {
	cfg, _ := animationProject(t)
	laterSprites := filepath.Join(cfg.Root, "b", "sprites")
	if err := os.Mkdir(laterSprites, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(laterSprites, "later.png"), []byte("later"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(laterSprites, filepath.Join(cfg.Root, "a", "sprites")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(cfg).AssetImage("sprites/later.png"); err == nil {
		t.Fatal("later mod's artwork was served through the selected mod's symlink")
	}
}

func TestSession_SymlinkedProjectRootStillServesItsOwnSprites(t *testing.T) {
	cfg, _ := animationProject(t)
	sprites := filepath.Join(cfg.Root, "a", "sprites")
	if err := os.Mkdir(sprites, 0o700); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(sprites, "player.png")
	if err := os.WriteFile(image, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "project-link")
	if err := os.Symlink(cfg.Root, alias); err != nil {
		t.Fatal(err)
	}
	cfg.Root = alias
	cfg.Mods[1].Assets = filepath.Join(alias, "a")
	s := Open(cfg)
	if s.Problem() != "" {
		t.Fatal(s.Problem())
	}
	if got, err := s.AssetImage("sprites/player.png"); err != nil || got != image {
		t.Fatalf("symlinked project could not serve its own sheet: %q, %v", got, err)
	}
}
