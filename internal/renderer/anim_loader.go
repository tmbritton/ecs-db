package renderer

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/fsnotify/fsnotify"
)

// AnimDef describes a named animation: which sprite sheet, which column frames, at what playback rate.
type AnimDef struct {
	Name   string  `toml:"name"`
	Sheet  string  `toml:"sheet"`
	Frames []int   `toml:"frames"`
	FPS    float64 `toml:"fps"`
	Loop   bool    `toml:"loop"`
}

type animFile struct {
	Animation []AnimDef `toml:"animation"`
}

// AnimLoader loads and hot-reloads animation definitions from a TOML file.
type AnimLoader struct {
	mu   sync.RWMutex
	defs map[string]*AnimDef
}

func NewAnimLoader() *AnimLoader {
	return &AnimLoader{defs: make(map[string]*AnimDef)}
}

// Load parses the TOML at path and atomically replaces the in-memory map.
// On parse failure it returns the error and retains the previous definitions.
func (al *AnimLoader) Load(path string) error {
	var f animFile
	if _, err := toml.DecodeFile(path, &f); err != nil {
		return fmt.Errorf("anim: parse %q: %w", path, err)
	}
	m := make(map[string]*AnimDef, len(f.Animation))
	for i := range f.Animation {
		d := f.Animation[i]
		m[d.Name] = &d
	}
	al.mu.Lock()
	al.defs = m
	al.mu.Unlock()
	return nil
}

// Get returns the AnimDef for name, or false if not found.
func (al *AnimLoader) Get(name string) (*AnimDef, bool) {
	al.mu.RLock()
	d, ok := al.defs[name]
	al.mu.RUnlock()
	return d, ok
}

// Watch blocks until ctx is cancelled, reloading path on any write event (debounced at 50 ms).
func (al *AnimLoader) Watch(ctx context.Context, path string) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("anim watcher: %w", err)
	}
	defer fw.Close()
	if err := fw.Add(path); err != nil {
		return fmt.Errorf("anim watcher: %q: %w", path, err)
	}

	const debounce = 50 * time.Millisecond
	var (
		timerMu sync.Mutex
		timer   *time.Timer
	)
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-fw.Events:
			if !ok {
				return nil
			}
			if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) || ev.Has(fsnotify.Rename) {
				timerMu.Lock()
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(debounce, func() {
					if err := al.Load(path); err != nil {
						log.Printf("anim: reload %q: %v", path, err)
					} else {
						log.Printf("anim: reloaded %q", path)
					}
				})
				timerMu.Unlock()
			}
		case err, ok := <-fw.Errors:
			if !ok {
				return nil
			}
			log.Printf("anim watcher: %v", err)
		}
	}
}
