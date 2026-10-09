package animations

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func projectPNG(t *testing.T, root, name string, width, height int) string {
	t.Helper()
	path := filepath.Join(root, name)
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSession_ImportCopiesProjectPNGAndAuthorsDraftWithoutTouchingTOML(t *testing.T) {
	cfg, animationPath := animationProject(t)
	source := projectPNG(t, cfg.Root, "walking.png", 48, 16)
	s := Open(cfg)
	images, err := s.ProjectImages()
	if err != nil || len(images) != 1 || images[0] != source {
		t.Fatalf("project image picker = %v, %v", images, err)
	}
	sheet, err := s.Import(source, "run", "Goblin", 16)
	if err != nil || sheet != "sprites/walking.png" {
		t.Fatalf("imported sheet = %q, %v", sheet, err)
	}
	copyPath := filepath.Join(cfg.Root, "a", "sprites", "walking.png")
	if copied, err := os.ReadFile(copyPath); err != nil || !bytes.Equal(copied, mustRead(t, source)) {
		t.Errorf("imported PNG was not copied exactly: %v", err)
	}
	if raw := mustRead(t, animationPath); string(raw) != sampleTOML {
		t.Errorf("import wrote TOML before Save: %s", raw)
	}
	if err := s.Read(func(d *Document) error {
		if got := string(d.Bytes()); !strings.HasPrefix(got, sampleTOML) || !strings.Contains(got, "frames = [0, 1, 2]") || !strings.Contains(got, `entity_type = "Goblin"`) {
			t.Errorf("import lost unknown TOML or did not create playable draft: %s", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if view := s.Preview("run", 16); view.Problem != "" || view.Columns != 3 {
		t.Errorf("imported animation cannot be previewed: %+v", view)
	}
}

func TestSession_ImportRefusesInvalidSourcesBeforeCopyOrEdit(t *testing.T) {
	for _, tt := range []struct {
		name, source  string
		width, height int
	}{
		{"second row", "second.png", 16, 32},
		{"incomplete column", "incomplete.png", 17, 16},
		{"source outside project", "outside", 16, 16},
		{"source symlink outside project", "escape", 16, 16},
		{"truncated PNG", "truncated.png", 32, 16},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, animationPath := animationProject(t)
			outside := projectPNG(t, t.TempDir(), "outside.png", tt.width, tt.height)
			source := filepath.Join(cfg.Root, tt.source)
			switch tt.source {
			case "outside":
				source = outside
			case "escape":
				source += ".png"
				if err := os.Symlink(outside, source); err != nil {
					t.Fatal(err)
				}
			default:
				source = projectPNG(t, cfg.Root, tt.source, tt.width, tt.height)
				if tt.source == "truncated.png" {
					if err := os.WriteFile(source, mustRead(t, source)[:33], 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			s := Open(cfg)
			if _, err := s.Import(source, "new", "Goblin", 16); err == nil {
				t.Fatal("unsafe/invalid import succeeded")
			}
			if _, err := os.Stat(filepath.Join(cfg.Root, "a", "sprites", filepath.Base(source))); !os.IsNotExist(err) {
				t.Fatalf("rejected import copied an image anyway: %v", err)
			}
			if raw := mustRead(t, animationPath); string(raw) != sampleTOML {
				t.Errorf("rejected import changed TOML: %s", raw)
			}
		})
	}
}

func TestSession_ImportBoundsTheNumberOfFrames(t *testing.T) {
	cfg, path := animationProject(t)
	source := projectPNG(t, cfg.Root, "huge.png", (maxImportColumns+1)*16, 16)
	s := Open(cfg)
	if _, err := s.Import(source, "huge", "", 16); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("overlong strip was accepted: %v", err)
	}
	if raw := mustRead(t, path); string(raw) != sampleTOML {
		t.Errorf("failed import modified the document: %s", raw)
	}
}

func TestSession_ImportNormalizesUppercasePNGExtensionForTheWatcher(t *testing.T) {
	cfg, _ := animationProject(t)
	source := projectPNG(t, cfg.Root, "upper.PNG", 16, 16)
	sheet, err := Open(cfg).Import(source, "new", "", 16)
	if err != nil || sheet != "sprites/upper.png" {
		t.Fatalf("image cache only watches .png files, imported %q: %v", sheet, err)
	}
	if copied := mustRead(t, filepath.Join(cfg.Root, "a", "sprites", "upper.png")); !bytes.Equal(copied, mustRead(t, source)) {
		t.Error("normalized extension lost PNG bytes")
	}
}

func TestSession_ProjectImagesWorkThroughASymlinkedProjectRoot(t *testing.T) {
	cfg, _ := animationProject(t)
	source := projectPNG(t, cfg.Root, "walk.png", 16, 16)
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(cfg.Root, alias); err != nil {
		t.Fatal(err)
	}
	cfg.Root = alias
	cfg.Mods[1].Assets = filepath.Join(alias, "a")
	s := Open(cfg)
	images, err := s.ProjectImages()
	if err != nil || len(images) != 1 || filepath.Base(images[0]) != filepath.Base(source) {
		t.Fatalf("symlinked project lost its PNG candidate: %v, %v", images, err)
	}
}

func TestReadProjectPNG_RejectsSymlinkAndReadsARegularSource(t *testing.T) {
	root := t.TempDir()
	regular := projectPNG(t, root, "regular.png", 16, 16)
	link := filepath.Join(root, "link.png")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readProjectPNG(root, link); err == nil {
		t.Fatal("source reader followed a symlink")
	}
	if got, err := readProjectPNG(root, regular); err != nil || !bytes.Equal(got, mustRead(t, regular)) {
		t.Errorf("source reader refused a regular PNG: %v", err)
	}
}

func TestReadProjectPNG_RefusesAParentSymlinkOutsideTheProject(t *testing.T) {
	project := t.TempDir()
	parent := filepath.Join(project, "art")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = projectPNG(t, parent, "sprite.png", 16, 16)
	outside := t.TempDir()
	_ = projectPNG(t, outside, "sprite.png", 16, 16)
	if err := os.Rename(parent, filepath.Join(project, "moved-art")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := readProjectPNG(project, filepath.Join(parent, "sprite.png")); err == nil {
		t.Fatal("source reader followed a parent symlink outside the project")
	}
}

func TestPublishSprite_UsesPinnedDestinationAfterSpritesPathIsSwapped(t *testing.T) {
	assets := t.TempDir()
	dir := filepath.Join(assets, "sprites")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pinned, err := openSpriteDestination(assets, assets)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	moved := filepath.Join(assets, "original-sprites")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	if err := publishSprite(pinned, "import.png", []byte("complete PNG bytes")); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(moved, "import.png")); string(got) != "complete PNG bytes" {
		t.Errorf("original destination received %q", got)
	}
	if _, err := os.Stat(filepath.Join(outside, "import.png")); !os.IsNotExist(err) {
		t.Fatalf("import escaped through the substituted sprites symlink: %v", err)
	}
}

func TestOpenSpriteDestination_RefusesAssetsSymlinkOutsideProject(t *testing.T) {
	project := t.TempDir()
	assets := filepath.Join(project, "assets")
	if err := os.Symlink(t.TempDir(), assets); err != nil {
		t.Fatal(err)
	}
	if pinned, err := openSpriteDestination(project, assets); err == nil {
		pinned.Close()
		t.Fatal("selected assets root escaped the project")
	}
}

func TestPublishSprite_AssetsDirectorySwapCannotRedirectPinnedCopy(t *testing.T) {
	project := t.TempDir()
	assets := filepath.Join(project, "assets")
	if err := os.Mkdir(assets, 0o700); err != nil {
		t.Fatal(err)
	}
	pinned, err := openSpriteDestination(project, assets)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	moved := filepath.Join(project, "moved-assets")
	if err := os.Rename(assets, moved); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, assets); err != nil {
		t.Fatal(err)
	}
	if err := publishSprite(pinned, "sprite.png", []byte("complete bytes")); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(moved, "sprites", "sprite.png")); string(got) != "complete bytes" {
		t.Errorf("selected pinned assets got %q", got)
	}
	if _, err := os.Stat(filepath.Join(outside, "sprites", "sprite.png")); !os.IsNotExist(err) {
		t.Fatalf("sprite escaped into substituted assets directory: %v", err)
	}
}

func TestSession_ImportRefusesExistingNameOrDestinationWithoutOverwriting(t *testing.T) {
	cfg, animationPath := animationProject(t)
	source := projectPNG(t, cfg.Root, "fresh.png", 16, 16)
	s := Open(cfg)
	if _, err := s.Import(source, "idle", "", 16); err == nil {
		t.Error("duplicate animation name was allowed")
	}
	if _, err := s.Import(source, "fresh", "Player", 16); err == nil {
		t.Error("duplicate binding was allowed")
	}
	folder := filepath.Join(cfg.Root, "a", "sprites")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(folder, "fresh.png")
	if err := os.WriteFile(destination, []byte("existing art"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Import(source, "fresh", "", 16); err == nil {
		t.Error("import overwrote an existing destination")
	}
	if got := mustRead(t, destination); string(got) != "existing art" {
		t.Errorf("existing sprite changed: %s", got)
	}
	if raw := mustRead(t, animationPath); string(raw) != sampleTOML {
		t.Errorf("refused import wrote TOML: %s", raw)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
