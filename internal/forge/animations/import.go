package animations

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxImportColumns = 256

// ProjectImages lists regular PNGs under the project, excluding the selected
// sprite destination. WalkDir does not traverse symlinked directories, and a
// symlinked PNG is not an acceptable source.
func (s *Session) ProjectImages() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projectImages()
}

func (s *Session) projectImages() ([]string, error) {
	if err := s.safe(); err != nil {
		return nil, err
	}
	var images []string
	realRoot, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return nil, err
	}
	assetsRoot, err := filepath.EvalSymlinks(s.active.Assets)
	if err != nil {
		return nil, err
	}
	destinationRoot := filepath.Join(assetsRoot, "sprites")
	err = filepath.WalkDir(realRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != realRoot && (within(destinationRoot, path) || entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "bin" || entry.Name() == "test-results") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".png") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			images = append(images, path)
		}
		return nil
	})
	return images, err
}

// readProjectPNG binds the listed source to the file descriptor used for the
// copy. A project file replaced by a symlink between enumeration and opening
// must not turn a local import into a read from outside the project.
func readProjectPNG(projectRoot, path string) ([]byte, error) {
	if !within(projectRoot, path) {
		return nil, fmt.Errorf("sprite source %q is outside the project", path)
	}
	root, err := os.OpenRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	rel, err := filepath.Rel(projectRoot, path)
	if err != nil {
		return nil, err
	}
	before, err := root.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("sprite source %q is not a regular project PNG", path)
	}
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	after, err := root.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, opened) || !os.SameFile(after, opened) {
		return nil, fmt.Errorf("sprite source %q changed since it was listed", path)
	}
	return io.ReadAll(f)
}

// openSpriteDestination pins the assets directory beneath an opened project
// root, then pins its sprites child. A symlink substitution in either pathname
// cannot redirect publication outside the selected project.
func openSpriteDestination(projectRoot, assets string) (*os.Root, error) {
	realProject, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		return nil, err
	}
	realAssets, err := filepath.EvalSymlinks(assets)
	if err != nil {
		return nil, err
	}
	if !within(realProject, realAssets) {
		return nil, fmt.Errorf("selected assets directory %q is outside the project", assets)
	}
	relAssets, err := filepath.Rel(realProject, realAssets)
	if err != nil {
		return nil, err
	}
	before, err := os.Lstat(realAssets)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() {
		return nil, fmt.Errorf("selected assets directory %q is not a directory", assets)
	}
	project, err := os.OpenRoot(realProject)
	if err != nil {
		return nil, err
	}
	defer project.Close()
	root, err := project.OpenRoot(relAssets)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("selected assets directory %q changed while opening: %v", assets, err)
	}
	if err := root.MkdirAll("sprites", 0o700); err != nil {
		return nil, err
	}
	return root.OpenRoot("sprites")
}

// publishSprite writes a temporary .tmp file in the pinned directory, then
// hard-links it at the final .png name. Link is atomic and never replaces an
// existing destination; the PNG watcher never sees a partly written image.
func publishSprite(dir *os.Root, name string, contents []byte) error {
	if name != filepath.Base(name) || filepath.Ext(name) != ".png" {
		return fmt.Errorf("%q is not a sprite filename", name)
	}
	var tmpName string
	var tmp *os.File
	for range 10 {
		var nonce [8]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		tmpName = fmt.Sprintf(".sprite-import-%x.tmp", nonce)
		var err error
		tmp, err = dir.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	if tmp == nil {
		return fmt.Errorf("could not allocate a temporary sprite file")
	}
	defer func() { _ = dir.Remove(tmpName) }()
	if _, err := tmp.Write(contents); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := dir.Link(tmpName, name); err != nil {
		return fmt.Errorf("sprite destination %q already exists or cannot be created: %w", name, err)
	}
	return nil
}

// Import copies a fully validated PNG without replacing a destination, then
// publishes its animation and optional binding in the held TOML draft. The
// image copy is immediate; Save is still needed for animations.toml.
func (s *Session) Import(source, name, entityType string, tileSize int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.current()
	if err != nil {
		return "", err
	}
	images, err := s.projectImages()
	if err != nil {
		return "", err
	}
	listed := false
	for _, path := range images {
		if path == source {
			listed = true
			break
		}
	}
	if !listed {
		return "", fmt.Errorf("%q is not a project-local PNG offered for import", source)
	}
	if tileSize <= 0 {
		return "", fmt.Errorf("window.tileSize must be positive to import a sprite sheet")
	}
	realRoot, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return "", err
	}
	contents, err := readProjectPNG(realRoot, source)
	if err != nil {
		return "", err
	}
	img, err := png.Decode(bytes.NewReader(contents))
	if err != nil {
		return "", fmt.Errorf("decoding sprite PNG %q: %w", source, err)
	}
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	if height != tileSize || width == 0 || width%tileSize != 0 {
		return "", fmt.Errorf("sprite sheet must be one row of %d-pixel square frames; image is %d×%d", tileSize, width, height)
	}
	columns := width / tileSize
	if columns > maxImportColumns {
		return "", fmt.Errorf("sprite strip has %d columns; at most %d can be imported", columns, maxImportColumns)
	}
	frames := make([]int, columns)
	for i := range frames {
		frames[i] = i
	}
	// The image cache watches .png suffixes. A project source may spell its
	// extension .PNG; the selected mod copy must use the watched spelling.
	base := filepath.Base(source)
	destinationName := strings.TrimSuffix(base, filepath.Ext(base)) + ".png"
	sheet := filepath.ToSlash(filepath.Join("sprites", destinationName))
	// Preflight both declarations before any I/O. They are independent in TOML,
	// and a duplicate binding must not strand a newly copied PNG.
	draft, err := Parse(f.Current.Bytes())
	if err != nil {
		return "", err
	}
	if err := draft.AddAnimation(name, sheet, frames, 8, true); err != nil {
		return "", err
	}
	if entityType != "" {
		if err := draft.AddBinding(entityType, sheet); err != nil {
			return "", err
		}
	}
	dir, err := openSpriteDestination(s.root, s.active.Assets)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	if err := s.ValidateSheet(sheet); err != nil {
		return "", err
	}
	if err := publishSprite(dir, destinationName, contents); err != nil {
		return "", err
	}
	f.Current = draft
	return sheet, nil
}
