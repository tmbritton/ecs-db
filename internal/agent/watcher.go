package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchedDir pairs a directory path with its mod name for source tracking and log messages.
type WatchedDir struct {
	Path    string
	ModName string
}

// Watcher monitors behavior directories for *.json file changes and hot-reloads
// machine definitions into a Loader via ReloadFile.
type Watcher struct {
	loader   *Loader
	dirs     []WatchedDir
	debounce time.Duration
	mu       sync.Mutex
	timers   map[string]*time.Timer // keyed by absolute file path
}

// NewWatcher creates a Watcher. debounce controls how long to wait after the
// last file event before triggering a reload (coalesces rapid editor saves).
// A zero debounce is treated as 50ms.
func NewWatcher(loader *Loader, dirs []WatchedDir, debounce time.Duration) *Watcher {
	if debounce == 0 {
		debounce = 50 * time.Millisecond
	}
	return &Watcher{
		loader:   loader,
		dirs:     dirs,
		debounce: debounce,
		timers:   make(map[string]*time.Timer),
	}
}

// Start watches all configured directories until ctx is cancelled.
// Directories that do not exist are skipped with a logged warning.
// Returns nil when ctx is done; returns an error only if the underlying
// filesystem watcher cannot be created.
func (w *Watcher) Start(ctx context.Context) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watcher: create: %w", err)
	}
	defer fw.Close()

	for _, d := range w.dirs {
		if err := fw.Add(d.Path); err != nil {
			fmt.Printf("[hot-reload] skipping directory %q: %v\n", d.Path, err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-fw.Events:
			if !ok {
				return nil
			}
			if filepath.Ext(event.Name) != ".json" {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			w.scheduleReload(event.Name)
		case err, ok := <-fw.Errors:
			if !ok {
				return nil
			}
			fmt.Printf("[hot-reload] watcher error: %v\n", err)
		}
	}
}

// scheduleReload debounces reload triggers for filePath. Each new event for
// the same file cancels any pending timer and starts a fresh one.
func (w *Watcher) scheduleReload(filePath string) {
	modName := w.modNameForPath(filePath)
	w.mu.Lock()
	defer w.mu.Unlock()
	if t, ok := w.timers[filePath]; ok {
		t.Stop()
	}
	w.timers[filePath] = time.AfterFunc(w.debounce, func() {
		w.mu.Lock()
		delete(w.timers, filePath)
		w.mu.Unlock()
		_ = w.loader.ReloadFile(filePath, modName)
	})
}

// modNameForPath returns the ModName for the directory containing filePath,
// or "unknown" if no configured directory is a prefix of filePath.
func (w *Watcher) modNameForPath(filePath string) string {
	for _, d := range w.dirs {
		prefix := d.Path
		if !strings.HasSuffix(prefix, string(filepath.Separator)) {
			prefix += string(filepath.Separator)
		}
		if strings.HasPrefix(filePath, prefix) {
			return d.ModName
		}
	}
	return "unknown"
}
