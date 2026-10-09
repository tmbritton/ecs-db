package renderer

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"maps"
	"path/filepath"
	"sort"
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

type entityAsset struct {
	EntityType string `toml:"entity_type"`
	Sheet      string `toml:"sheet"`
}

type animFile struct {
	Animation   []AnimDef     `toml:"animation"`
	EntityAsset []entityAsset `toml:"entity_asset"`
}

// AnimLoader loads and hot-reloads animation definitions from a TOML file.
type AnimLoader struct {
	mu           sync.RWMutex
	defs         map[string]*AnimDef
	entitySheets map[string]string // entity type → sheet path
	// The last successfully applied binding set, not the latest parsed file.
	// A failed sync must retain removed types for retry on the next save.
	syncedSheets map[string]string
}

func NewAnimLoader() *AnimLoader {
	return &AnimLoader{
		defs:         make(map[string]*AnimDef),
		entitySheets: make(map[string]string),
		syncedSheets: make(map[string]string),
	}
}

// Load parses the TOML at path and atomically replaces the in-memory maps.
// On parse failure it returns the error and retains the previous definitions.
func (al *AnimLoader) Load(path string) error {
	var f animFile
	if _, err := toml.DecodeFile(path, &f); err != nil {
		return fmt.Errorf("anim: parse %q: %w", path, err)
	}
	defs := make(map[string]*AnimDef, len(f.Animation))
	for i := range f.Animation {
		d := f.Animation[i]
		defs[d.Name] = &d
	}
	sheets := make(map[string]string, len(f.EntityAsset))
	for _, ea := range f.EntityAsset {
		sheets[ea.EntityType] = ea.Sheet
	}
	al.mu.Lock()
	al.defs = defs
	al.entitySheets = sheets
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

// SheetForEntityType returns the canonical sheet path for an entity type.
func (al *AnimLoader) SheetForEntityType(entityType string) (string, bool) {
	al.mu.RLock()
	s, ok := al.entitySheets[entityType]
	al.mu.RUnlock()
	return s, ok
}

// AllSheets returns a deduplicated sorted list of all sheet paths from [[entity_asset]] entries.
func (al *AnimLoader) AllSheets() []string {
	al.mu.RLock()
	seen := make(map[string]bool, len(al.entitySheets))
	for _, s := range al.entitySheets {
		seen[s] = true
	}
	al.mu.RUnlock()
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// SyncToDatabase stamps comp_sprite.sheet for every entity based on its entity type.
// Runs unconditionally — safe to call on every startup.
func (al *AnimLoader) SyncToDatabase(ctx context.Context, db *sql.DB) error {
	al.mu.RLock()
	sheets := make(map[string]string, len(al.entitySheets))
	maps.Copy(sheets, al.entitySheets)
	previous := make(map[string]string, len(al.syncedSheets))
	maps.Copy(previous, al.syncedSheets)
	al.mu.RUnlock()

	// One save is one binding set. If a later update fails, an earlier one
	// cannot be left applied but unrecorded in syncedSheets: a subsequent
	// removal would never know to clear it.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("anim: begin sheet sync: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	entityTypes := make([]string, 0, len(sheets))
	for entityType := range sheets {
		entityTypes = append(entityTypes, entityType)
	}
	sort.Strings(entityTypes)
	for _, entityType := range entityTypes {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE comp_sprite SET sheet = ?
			 WHERE entity_id IN (SELECT id FROM entities WHERE entity_type = ?)`,
			sheets[entityType], entityType,
		); err != nil {
			return fmt.Errorf("anim: sync sheet for %q: %w", entityType, err)
		}
	}
	removed := make([]string, 0, len(previous))
	for entityType := range previous {
		if _, stillBound := sheets[entityType]; stillBound {
			continue
		}
		removed = append(removed, entityType)
	}
	sort.Strings(removed)
	for _, entityType := range removed {
		if _, err := tx.ExecContext(ctx,
			`UPDATE comp_sprite SET sheet = '' WHERE sheet = ? AND entity_id IN (SELECT id FROM entities WHERE entity_type = ?)`,
			previous[entityType], entityType,
		); err != nil {
			return fmt.Errorf("anim: clear removed binding for %q: %w", entityType, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("anim: commit sheet sync: %w", err)
	}
	al.mu.Lock()
	al.syncedSheets = sheets
	al.mu.Unlock()
	return nil
}

// Watch blocks until ctx is cancelled, reloading path on any write event
// (debounced at 50 ms). Call WatchWithDatabase when entity-sheet bindings must
// update existing sprites in the running game's comp_sprite table as well.
func (al *AnimLoader) Watch(ctx context.Context, path string) error {
	return al.watch(ctx, path, nil)
}

// WatchWithDatabase reloads animation definitions and applies new entity_asset
// bindings to live sprite components after every successful watched save.
func (al *AnimLoader) WatchWithDatabase(ctx context.Context, path string, db *sql.DB) error {
	return al.watch(ctx, path, db)
}

func (al *AnimLoader) watch(ctx context.Context, path string, db *sql.DB) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("anim watcher: %w", err)
	}
	defer fw.Close()
	// Forge saves by atomically renaming a temporary file into place. Watching
	// the file inode stops receiving events after the first replacement; the
	// directory survives every replacement and is filtered to this filename.
	if err := fw.Add(filepath.Dir(path)); err != nil {
		return fmt.Errorf("anim watcher: %q: %w", filepath.Dir(path), err)
	}
	watched, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	const debounce = 50 * time.Millisecond
	var (
		timerMu  sync.Mutex
		reloadMu sync.Mutex // atomic Load + DB sync, even when timers overlap
		timer    *time.Timer
	)
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-fw.Events:
			if !ok {
				return nil
			}
			eventPath, err := filepath.Abs(ev.Name)
			if err != nil || eventPath != watched {
				continue
			}
			if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) || ev.Has(fsnotify.Rename) {
				timerMu.Lock()
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(debounce, func() {
					reloadMu.Lock()
					defer reloadMu.Unlock()
					if ctx.Err() != nil {
						return
					}
					if err := al.Load(path); err != nil {
						log.Printf("anim: reload %q: %v", path, err)
					} else if db != nil {
						if err := al.SyncToDatabase(ctx, db); err != nil {
							log.Printf("anim: reloaded %q, but sheet sync failed: %v", path, err)
						} else {
							log.Printf("anim: reloaded and synced sheets from %q", path)
						}
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
