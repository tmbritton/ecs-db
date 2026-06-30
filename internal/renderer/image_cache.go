//go:build ebitengine

package renderer

import (
	"context"
	"image"
	_ "image/png"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hajimehoshi/ebiten/v2"
)

// ImageCache loads sprite sheet PNGs on first use and caches them for subsequent Draw calls.
// Evict removes an entry so the next Get reloads from disk — used by WatchDir for hot-reload.
type ImageCache struct {
	mu   sync.RWMutex
	imgs map[string]*ebiten.Image
}

func NewImageCache() *ImageCache {
	return &ImageCache{imgs: make(map[string]*ebiten.Image)}
}

// Get returns the cached image for path, loading it from disk on first use.
// Returns nil, false if path is empty or the file cannot be decoded.
func (ic *ImageCache) Get(path string) (*ebiten.Image, bool) {
	if path == "" {
		return nil, false
	}
	ic.mu.RLock()
	img, ok := ic.imgs[path]
	ic.mu.RUnlock()
	if ok {
		return img, true
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, false
	}
	img = ebiten.NewImageFromImage(src)

	ic.mu.Lock()
	ic.imgs[path] = img
	ic.mu.Unlock()
	return img, true
}

// Evict removes path from the cache. The next Get call will reload from disk.
func (ic *ImageCache) Evict(path string) {
	ic.mu.Lock()
	delete(ic.imgs, path)
	ic.mu.Unlock()
}

// evictByBasename removes all cache entries whose path has the same base filename as basename.
// Used to match relative cache keys against absolute fsnotify event paths.
func (ic *ImageCache) evictByBasename(basename string) {
	ic.mu.Lock()
	for k := range ic.imgs {
		if filepath.Base(k) == basename {
			delete(ic.imgs, k)
			log.Printf("image cache: evicted %q (file changed)", k)
		}
	}
	ic.mu.Unlock()
}

// WatchDir blocks until ctx is cancelled, evicting PNG entries from the cache whenever a
// file in dir is written or replaced (debounced at 50 ms). Run in a goroutine.
func (ic *ImageCache) WatchDir(ctx context.Context, dir string) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fw.Close()
	if err := fw.Add(dir); err != nil {
		return err
	}

	const debounce = 50 * time.Millisecond
	var (
		timerMu sync.Mutex
		timers  = make(map[string]*time.Timer)
	)
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-fw.Events:
			if !ok {
				return nil
			}
			if filepath.Ext(ev.Name) != ".png" {
				continue
			}
			if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) || ev.Has(fsnotify.Rename) {
				base := filepath.Base(ev.Name)
				timerMu.Lock()
				if t := timers[base]; t != nil {
					t.Stop()
				}
				timers[base] = time.AfterFunc(debounce, func() {
					ic.evictByBasename(base)
				})
				timerMu.Unlock()
			}
		case err, ok := <-fw.Errors:
			if !ok {
				return nil
			}
			log.Printf("image cache watcher: %v", err)
		}
	}
}
