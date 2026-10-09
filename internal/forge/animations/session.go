package animations

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tmbritton/ecs-db/internal/forge/atomicfile"
	"github.com/tmbritton/ecs-db/internal/forge/editable"
)

type AssetMod struct {
	Name   string
	Assets string
}

type Config struct {
	Root string
	Mods []AssetMod // same load order the engine uses
}

type Session struct {
	mu      sync.Mutex
	root    string
	active  AssetMod
	later   []AssetMod
	path    string
	problem string
	file    *editable.File[*Document]
}

func documentCodec() editable.Codec[*Document] {
	return editable.Codec[*Document]{
		Marshal:   func(d *Document) ([]byte, error) { return d.Bytes(), nil },
		Unmarshal: Parse,
	}
}

// Open chooses the same first nonempty assets mod that run.go chooses. A file
// that cannot open is a project problem, not a reason to stop Forge.
func Open(cfg Config) *Session {
	s := &Session{}
	root, err := filepath.Abs(cfg.Root)
	if err != nil || cfg.Root == "" {
		s.problem = "a project root is required for animation assets"
		return s
	}
	s.root = root
	for _, mod := range cfg.Mods {
		if mod.Assets == "" {
			continue
		}
		if s.path == "" {
			s.active = mod
			assets, err := filepath.Abs(mod.Assets)
			if err != nil {
				s.problem = err.Error()
				return s
			}
			s.active.Assets = assets
			s.path = filepath.Join(assets, "animations.toml")
		} else {
			s.later = append(s.later, mod)
		}
	}
	if s.path == "" {
		s.problem = "this project declares no assets mod"
		return s
	}
	if err := s.safe(); err != nil {
		s.problem = err.Error()
		return s
	}
	f, err := editable.Open(s.path, documentCodec())
	if err != nil {
		s.problem = err.Error()
		return s
	}
	s.file = f
	return s
}

func (s *Session) Path() string { return s.path }

func (s *Session) Active() AssetMod { return s.active }

func (s *Session) Later() []AssetMod { return append([]AssetMod(nil), s.later...) }

func (s *Session) Problem() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.problem
}

func (s *Session) Missing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil || s.path == "" || s.safe() != nil {
		return false
	}
	_, err := os.Lstat(s.path)
	return errors.Is(err, os.ErrNotExist)
}

// Disappeared reports a file deleted since opening while Forge still holds
// its working bytes. SaveOverwriting can put it back without losing edits.
func (s *Session) Disappeared() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil || s.safe() != nil {
		return false
	}
	_, err := os.Lstat(s.path)
	return errors.Is(err, os.ErrNotExist)
}

func (s *Session) safe() error {
	if s.path == "" || s.root == "" {
		return fmt.Errorf("there is no active animation file")
	}
	root, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	assets, err := filepath.EvalSymlinks(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	assets, err = filepath.Abs(assets)
	if err != nil {
		return err
	}
	if !within(root, assets) {
		return fmt.Errorf("animation assets %q are outside the project root", assets)
	}
	file, err := filepath.EvalSymlinks(s.path)
	if errors.Is(err, os.ErrNotExist) {
		if _, linkErr := os.Lstat(s.path); linkErr == nil {
			return fmt.Errorf("animation file %q is a broken symlink", s.path)
		}
		return nil // a missing file can be created at this safe path
	}
	if err != nil {
		return err
	}
	file, err = filepath.Abs(file)
	if err != nil {
		return err
	}
	if !within(assets, file) {
		return fmt.Errorf("animation file %q is outside the selected assets directory", s.path)
	}
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("animation file %q is not a regular file", s.path)
	}
	return nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ValidateSheet permits a not-yet-created sprite inside the selected mod, but
// never a later mod, traversal, unsupported image or symlink escape.
func (s *Session) ValidateSheet(source string) error {
	if source == "" {
		return fmt.Errorf("a sprite sheet path is required")
	}
	if !strings.EqualFold(filepath.Ext(source), ".png") {
		return fmt.Errorf("sprite sheets must be PNG files; %q is not playable by the game", source)
	}
	if err := s.safe(); err != nil {
		return err
	}
	assets, err := filepath.EvalSymlinks(s.active.Assets)
	if err != nil {
		return err
	}
	spriteRoot := filepath.Join(s.active.Assets, "sprites")
	resolvedRoot, err := filepath.EvalSymlinks(spriteRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && !within(assets, resolvedRoot) {
		return fmt.Errorf("sprites directory is outside the selected assets mod")
	}
	paths := []string{source}
	if !filepath.IsAbs(source) {
		paths = []string{filepath.Join(s.active.Assets, source), filepath.Join(s.root, source)}
	}
	for _, path := range paths {
		if !within(spriteRoot, path) {
			continue
		}
		parent := path
		for {
			resolved, err := filepath.EvalSymlinks(parent)
			if err == nil {
				if resolvedRoot != "" && parent != s.active.Assets && !within(resolvedRoot, resolved) {
					return fmt.Errorf("sprite image %q escapes the selected sprites directory", source)
				}
				if parent == s.active.Assets && !within(assets, resolved) {
					return fmt.Errorf("sprite image %q escapes the selected assets mod", source)
				}
				return nil
			}
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if parent == s.active.Assets {
				break
			}
			parent = filepath.Dir(parent)
		}
	}
	return fmt.Errorf("sprite sheet %q is outside the selected sprites directory", source)
}

// AssetImage resolves a sprite sheet against the selected mod's sprites tree.
// File names written in animations.toml may be project-relative or assets-
// relative; neither form grants access to a later mod or a symlink outside.
func (s *Session) AssetImage(source string) (string, error) {
	if source == "" || s.active.Assets == "" {
		return "", fmt.Errorf("no sprite image is named")
	}
	allowed := map[string]bool{".png": true, ".gif": true, ".jpg": true, ".jpeg": true, ".webp": true, ".bmp": true}
	if !allowed[strings.ToLower(filepath.Ext(source))] {
		return "", fmt.Errorf("%q is not a supported sprite image", source)
	}
	root, err := filepath.EvalSymlinks(filepath.Join(s.active.Assets, "sprites"))
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	projectRoot, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return "", err
	}
	projectRoot, err = filepath.Abs(projectRoot)
	if err != nil {
		return "", err
	}
	if !within(projectRoot, root) {
		return "", fmt.Errorf("sprites directory is outside the project")
	}
	assetsRoot, err := filepath.EvalSymlinks(s.active.Assets)
	if err != nil {
		return "", err
	}
	if !within(assetsRoot, root) {
		return "", fmt.Errorf("sprites directory is outside the selected assets mod")
	}
	paths := []string{source}
	if !filepath.IsAbs(source) {
		paths = []string{filepath.Join(s.active.Assets, source), filepath.Join(s.root, source)}
	}
	for _, path := range paths {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}
		real, err = filepath.Abs(real)
		if err != nil || !within(root, real) {
			continue
		}
		if info, err := os.Stat(real); err == nil && info.Mode().IsRegular() {
			return real, nil
		}
	}
	return "", fmt.Errorf("sprite image %q is unavailable inside %s", source, root)
}

func (s *Session) current() (*editable.File[*Document], error) {
	if err := s.safe(); err != nil {
		return nil, err
	}
	if s.file == nil {
		return nil, fmt.Errorf("animation file %q is not open: %s", s.path, s.problem)
	}
	return s.file, nil
}

func (s *Session) Read(fn func(*Document) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.current()
	if err != nil {
		return err
	}
	return fn(f.Current)
}

func (s *Session) Edit(fn func(*Document) error) error { return s.Read(fn) }

func (s *Session) Dirty() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return false, nil
	}
	return s.file.Dirty()
}

func (s *Session) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.current()
	if err != nil {
		return err
	}
	if err := f.Save(); err != nil {
		return err
	}
	if err := s.restoreDeletedCleanFile(f); err != nil {
		return err
	}
	s.problem = ""
	return nil
}

func (s *Session) SaveOverwriting() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.current()
	if err != nil {
		return err
	}
	if err := f.SaveOverwriting(); err != nil {
		return err
	}
	// A clean draft makes editable.File skip the write. An explicit
	// SaveOverwriting still means "keep mine" if the file reappeared with
	// different bytes between the missing-file notice and this action.
	onDisk, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !bytes.Equal(onDisk, f.Current.Bytes()) || errors.Is(err, os.ErrNotExist) {
		if err := atomicfile.Write(s.path, f.Current.Bytes()); err != nil {
			return err
		}
	}
	if err := s.safe(); err != nil {
		return err
	}
	s.problem = ""
	return nil
}

// editable.File intentionally skips a byte-identical Save; a file that was
// deleted while open still needs to be put back even if its draft is clean.
func (s *Session) restoreDeletedCleanFile(f *editable.File[*Document]) error {
	if _, err := os.Lstat(s.path); errors.Is(err, os.ErrNotExist) {
		return atomicfile.Write(s.path, f.Current.Bytes())
	} else {
		return err
	}
}

func (s *Session) Discard() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.current()
	if err != nil {
		return err
	}
	return f.Discard()
}

func (s *Session) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.safe(); err != nil {
		return err
	}
	if s.file == nil {
		f, err := editable.Open(s.path, documentCodec())
		if err != nil {
			s.problem = err.Error()
			return err
		}
		s.file = f
		s.problem = ""
		return nil
	}
	if err := s.file.Reload(); err != nil {
		s.problem = err.Error()
		return err
	}
	s.problem = ""
	return nil
}

func (s *Session) Create() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		return fmt.Errorf("animation file is already open")
	}
	if err := s.safe(); err != nil {
		return err
	}
	if _, err := os.Lstat(s.path); err == nil {
		return fmt.Errorf("animation file %q already exists", s.path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := atomicfile.Write(s.path, []byte("# Sprite animations and entity sheets\n")); err != nil {
		return err
	}
	f, err := editable.Open(s.path, documentCodec())
	if err != nil {
		return err
	}
	s.file = f
	s.problem = ""
	return nil
}
