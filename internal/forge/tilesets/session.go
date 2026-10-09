// Package tilesets is one editing session per external project tileset, shared
// across every working TMX that names it. An embedded tileset has no file to
// save and a TSJ has no lossless writer.
package tilesets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/tmbritton/ecs-db/internal/forge/editable"
	"github.com/tmbritton/ecs-db/internal/forge/maps"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

type Entry struct {
	Path     string
	Maps     []string
	Writable bool
	Problem  string
}

type Session struct {
	root    string
	maps    *maps.Session
	mu      sync.Mutex
	files   map[string]*editable.File[*tiled.TilesetDocument]
	entries []Entry
}

func Open(root string, mapSession *maps.Session) *Session {
	s := &Session{root: root, maps: mapSession, files: make(map[string]*editable.File[*tiled.TilesetDocument])}
	s.Refresh()
	return s
}

// Refresh discovers from working TMX values before taking the tileset lock.
// MAP preview holds its map lock when it calls the tileset opener, so reversing
// that lock order here would deadlock a concurrent TSX edit and preview.
func (s *Session) Refresh() {
	if s.maps == nil {
		return
	}
	s.maps.Refresh()
	byPath := make(map[string]*Entry)
	for _, mp := range s.maps.Maps() {
		_ = s.maps.Read(mp.Path, func(doc *tiled.Document) error {
			m, err := doc.Map()
			if err != nil {
				return err
			}
			for _, ref := range m.Tilesets {
				if ref.Source == "" {
					continue // embedded XML belongs to the map, not a TSX file
				}
				source := filepath.FromSlash(ref.Source)
				path := filepath.Clean(filepath.Join(filepath.Dir(mp.Path), source))
				if filepath.IsAbs(source) {
					path = source
				}
				safe, err := s.contained(path)
				if err == nil {
					path = safe
				}
				entry := byPath[path]
				if entry == nil {
					entry = &Entry{Path: path}
					byPath[path] = entry
					if err != nil {
						entry.Problem = err.Error()
					} else if ext := strings.ToLower(filepath.Ext(path)); ext != ".tsx" {
						entry.Problem = fmt.Sprintf("%s is read-only: Forge writes external .tsx tilesets only", ext)
					} else {
						entry.Writable = true
					}
				}
				entry.Maps = append(entry.Maps, mp.Path)
			}
			return nil
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var entries []Entry
	for path, entry := range byPath {
		if s.files[path] != nil && entry.Problem != "" {
			if _, err := s.file(path); err != nil {
				entry.Problem += "; working TSX is retained, but this path is unsafe until repaired"
			} else {
				entry.Writable = true
				entry.Problem += "; working TSX is retained and SaveOverwriting can recreate it"
			}
		}
		if entry.Writable && s.files[path] == nil {
			f, err := editable.Open(path, Codec(path))
			if err != nil {
				entry.Writable = false
				entry.Problem = err.Error()
			} else {
				s.files[path] = f
			}
		}
		sort.Strings(entry.Maps)
		entries = append(entries, *entry)
	}
	for path, f := range s.files {
		if _, found := byPath[path]; !found {
			if dirty, err := f.Dirty(); err == nil && !dirty {
				delete(s.files, path)
				continue
			}
			// A removed source cannot make unsaved work unreachable. If its
			// path has become unsafe, keep the work visible but offer no writes.
			entry := Entry{Path: path, Problem: "no open map currently references this tileset; unsaved work is retained"}
			if _, err := s.file(path); err != nil {
				entry.Problem += "; path must be repaired before saving: " + err.Error()
			} else {
				entry.Writable = true
			}
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	s.entries = entries
}

func (s *Session) contained(path string) (string, error) {
	target, err := s.inside(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("tileset %s is not a regular file", path)
	}
	return target, nil
}

// inside resolves symlinks and checks the project boundary for a file or its
// existing parent. Unlike contained, the parent need not be a regular file.
func (s *Session) inside(path string) (string, error) {
	root, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return "", fmt.Errorf("project root %s is unavailable: %w", s.root, err)
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("tileset %s cannot be opened: %w", path, err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("tileset %s is outside the project root", path)
	}
	return target, nil
}

func (s *Session) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, len(s.entries))
	for i, entry := range s.entries {
		out[i] = entry
		out[i].Maps = append([]string(nil), entry.Maps...)
	}
	return out
}

func (s *Session) file(path string) (*editable.File[*tiled.TilesetDocument], error) {
	if f := s.files[filepath.Clean(path)]; f != nil {
		if _, err := s.contained(path); err == nil {
			return f, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err // an existing path must still resolve inside the project
		}
		if _, err := os.Lstat(path); err == nil {
			return nil, fmt.Errorf("tileset %q is a broken symlink", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if _, err := s.inside(filepath.Dir(path)); err != nil {
			return nil, err // an absent target under a replaced parent is not safe
		}
		return f, nil // a disappeared file remains reachable through its held path
	}
	safe, err := s.contained(path)
	if err != nil {
		return nil, err
	}
	f := s.files[safe]
	if f == nil {
		return nil, fmt.Errorf("%s is not a writable project tileset", path)
	}
	return f, nil
}

func (s *Session) Read(path string, fn func(*tiled.TilesetDocument) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.file(path)
	if err != nil {
		return err
	}
	return fn(f.Current)
}

// Describe reads the working TSX or a discovered read-only TSJ. Only tilesets
// referenced by a map (or retained with unsaved work) are selectable.
func (s *Session) Describe(path string) (*tiled.Tileset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.entries {
		if entry.Path != path {
			continue
		}
		if f := s.files[path]; f != nil {
			if _, err := s.file(path); err != nil {
				return nil, err
			}
			return f.Current.Tileset()
		}
		if !strings.EqualFold(filepath.Ext(path), ".tsj") {
			return nil, fmt.Errorf("tileset %q is not open", path)
		}
		safe, err := s.contained(path)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(safe)
		if err != nil {
			return nil, err
		}
		return tiled.ParseTileset(data, filepath.Base(path), filepath.Dir(path))
	}
	return nil, fmt.Errorf("tileset %q is not in the project session", path)
}

func (s *Session) Edit(path string, fn func(*tiled.TilesetDocument) error) error {
	return s.Read(path, fn)
}

// WorkingBytes supplies MAP with the one unsaved TSX document shared by all
// referring maps. A path not held here follows MAP's normal disk opener.
func (s *Session) WorkingBytes(path string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.files[filepath.Clean(path)]; f != nil {
		if _, err := s.file(path); err != nil {
			return nil, err
		}
		return append([]byte(nil), f.Current.Bytes()...), nil
	}
	safe, err := s.contained(path)
	if err == nil {
		if f := s.files[safe]; f != nil {
			return append([]byte(nil), f.Current.Bytes()...), nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return os.ReadFile(path)
}

func (s *Session) Dirty() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for path, f := range s.files {
		dirty, err := f.Dirty()
		if err != nil {
			return nil, err
		}
		if dirty {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *Session) act(path string, operation func(*editable.File[*tiled.TilesetDocument]) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.file(path)
	if err != nil {
		return err
	}
	return operation(f)
}

func (s *Session) Save(path string) error {
	return s.act(path, (*editable.File[*tiled.TilesetDocument]).Save)
}

func (s *Session) SaveOverwriting(path string) error {
	return s.act(path, (*editable.File[*tiled.TilesetDocument]).SaveOverwriting)
}

func (s *Session) Discard(path string) error {
	return s.act(path, (*editable.File[*tiled.TilesetDocument]).Discard)
}

func (s *Session) Reload(path string) error {
	return s.act(path, (*editable.File[*tiled.TilesetDocument]).Reload)
}
