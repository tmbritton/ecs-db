# Epic 4 Story 2: Filesystem Watcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a filesystem watcher that monitors configured behavior directories and hot-reloads machine definitions into the `Loader` without restarting the game.

**Architecture:** A new `Watcher` type (in `internal/agent/watcher.go`) wraps `fsnotify` and calls `Loader.ReloadFile` after a per-file debounce window. `Loader` gains a `sync.RWMutex` (needed because `Loader.Get` and `Loader.ReloadFile` will be called from different goroutines) and a new `ReloadFile(path, modName string) error` method that atomically swaps the in-memory definition on success and retains the old one on failure. `Watcher` is exposed as a standalone type; wiring it into the interpreter startup is deferred to Epic 5.

**Tech Stack:** `github.com/fsnotify/fsnotify` v1.7, Go standard library (`sync`, `context`, `time`)

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/agent/loader.go` | Modify | Add `sync.RWMutex`, protect `machines`/`sources` map access, add `ReloadFile` |
| `internal/agent/loader_test.go` | Modify | Add `ReloadFile` tests (package `agent`, reuses existing helpers) |
| `internal/agent/watcher.go` | Create | `WatchedDir`, `Watcher`, `NewWatcher`, `Start` |
| `internal/agent/watcher_test.go` | Create | Integration tests with real temp dirs (package `agent_test`) |
| `go.mod` / `go.sum` | Modify | Add `github.com/fsnotify/fsnotify` dependency |

---

## Task 1: Add fsnotify Dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the dependency**

```bash
cd /path/to/ecs-db
go get github.com/fsnotify/fsnotify@v1.7.0
```

Expected output: a line like `go: added github.com/fsnotify/fsnotify v1.7.0`

- [ ] **Step 2: Verify it was added to go.mod**

```bash
grep fsnotify go.mod
```

Expected: `github.com/fsnotify/fsnotify v1.7.0`

- [ ] **Step 3: Verify tests still pass**

```bash
go test ./...
```

Expected: all existing tests pass, no compilation errors.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add fsnotify dependency for filesystem watcher"
```

---

## Task 2: Mutex-Protect Loader and Add ReloadFile

The watcher will call `Loader.ReloadFile` from a background goroutine while other code calls `Loader.Get` from the main goroutine. Without protection, concurrent map access is a data race. This task adds a `sync.RWMutex` and a new `ReloadFile` method.

**Files:**
- Modify: `internal/agent/loader.go`
- Modify: `internal/agent/loader_test.go`

- [ ] **Step 1: Write the failing tests**

Add to the end of `internal/agent/loader_test.go`:

```go
func TestLoader_ReloadFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "m.json", validMachineJSON) // initial: "a"

	l := NewLoader(testRegistry(), testSchema())
	if _, err := l.LoadMachine(path); err != nil {
		t.Fatalf("initial LoadMachine: %v", err)
	}

	if err := os.WriteFile(path, []byte(validMachineV2JSON), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := l.ReloadFile(path, "core"); err != nil {
		t.Fatalf("ReloadFile: %v", err)
	}
	def, ok := l.Get("test_machine")
	if !ok {
		t.Fatal("Get returned false after ReloadFile")
	}
	if def.Initial != "b" {
		t.Errorf("Initial = %q, want b", def.Initial)
	}
}

func TestLoader_ReloadFile_FailureRetainsPrevious(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "m.json", validMachineJSON) // initial: "a"

	l := NewLoader(testRegistry(), testSchema())
	if _, err := l.LoadMachine(path); err != nil {
		t.Fatalf("initial LoadMachine: %v", err)
	}

	if err := os.WriteFile(path, []byte(invalidMachineJSON), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := l.ReloadFile(path, "core"); err == nil {
		t.Fatal("expected error from ReloadFile on invalid machine, got nil")
	}

	def, ok := l.Get("test_machine")
	if !ok {
		t.Fatal("Get returned false — previous definition not retained")
	}
	if def.Initial != "a" {
		t.Errorf("Initial = %q, want a (original definition must be retained)", def.Initial)
	}
}

func TestLoader_ReloadFile_MissingFile(t *testing.T) {
	l := NewLoader(testRegistry(), testSchema())
	err := l.ReloadFile("/nonexistent/path/m.json", "core")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
```

- [ ] **Step 2: Run tests — expect compilation failure**

```bash
go test ./internal/agent/ -run TestLoader_ReloadFile -v
```

Expected: compilation error — `l.ReloadFile undefined`

- [ ] **Step 3: Add mutex and ReloadFile to loader.go**

Replace the entire `internal/agent/loader.go` with:

```go
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// Loader reads, parses, and validates machine definition files. It retains
// the last successfully validated definition for each machine ID so that a
// failed hot-reload leaves the previous version in service.
type Loader struct {
	registry *Registry
	schema   schema.DatabaseSchema
	mu       sync.RWMutex
	machines map[string]*MachineDefinition
	sources  map[string]string // machineID → "modName:filepath"
}

func NewLoader(registry *Registry, schema schema.DatabaseSchema) *Loader {
	return &Loader{
		registry: registry,
		schema:   schema,
		machines: make(map[string]*MachineDefinition),
		sources:  make(map[string]string),
	}
}

// LoadMachine reads the file at path, parses and validates it. On success the
// new definition replaces any previously loaded definition with the same ID
// and is returned. On any error the previous definition (if any) is retained
// and the error is returned.
func (l *Loader) LoadMachine(path string) (*MachineDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading machine file %q: %w", path, err)
	}

	def, err := ParseMachine(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %q: %w", path, err)
	}

	errs := ValidateMachine(def, l.registry, l.schema)
	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return nil, fmt.Errorf("validation failed for %q: %s", def.ID, strings.Join(msgs, "; "))
	}

	l.mu.Lock()
	l.machines[def.ID] = def
	l.mu.Unlock()
	return def, nil
}

// ScanDir loads all *.json files from dir under the given modName. Files that
// fail to parse or validate are collected as errors and returned after all
// files are attempted. If the same machine ID was already loaded (from a prior
// ScanDir call), the new definition wins and a warning is printed.
func (l *Loader) ScanDir(dir, modName string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("ScanDir %q: %w", dir, err)
	}

	var errs []string
	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		def, err := l.LoadMachine(path)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		src := modName + ":" + path
		l.mu.Lock()
		if prev, ok := l.sources[def.ID]; ok {
			fmt.Printf("[config] machine %q overridden: was %s, now %s\n", def.ID, prev, src)
		}
		l.sources[def.ID] = src
		l.mu.Unlock()
		count++
	}

	if len(errs) > 0 {
		return count, fmt.Errorf("ScanDir %q: %s", dir, strings.Join(errs, "; "))
	}
	return count, nil
}

// ReloadFile re-parses and re-validates the machine at path under modName.
// On success the in-memory definition is atomically replaced and a log line is printed.
// On failure the previous definition is retained and the error is returned.
func (l *Loader) ReloadFile(path, modName string) error {
	def, err := l.LoadMachine(path)
	if err != nil {
		fmt.Printf("[hot-reload] machine reload failed (%s): %v\n", path, err)
		return err
	}
	l.mu.Lock()
	l.sources[def.ID] = modName + ":" + path
	l.mu.Unlock()
	fmt.Printf("[hot-reload] machine %q reloaded from %s\n", def.ID, path)
	return nil
}

// Get returns the currently active definition for machineID, or (nil, false)
// if the machine has never been successfully loaded.
func (l *Loader) Get(machineID string) (*MachineDefinition, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	def, ok := l.machines[machineID]
	return def, ok
}
```

- [ ] **Step 4: Run the new tests**

```bash
go test ./internal/agent/ -run TestLoader_ReloadFile -v
```

Expected: all three `TestLoader_ReloadFile_*` tests pass.

- [ ] **Step 5: Run the full agent test suite**

```bash
go test ./internal/agent/...
```

Expected: all existing tests still pass (mutex changes are transparent to callers).

- [ ] **Step 6: Commit**

```bash
git add internal/agent/loader.go internal/agent/loader_test.go
git commit -m "feat(epic-4/story-2): add Loader.ReloadFile and concurrent-safe map access"
```

---

## Task 3: Implement Watcher

**Files:**
- Create: `internal/agent/watcher.go`
- Create: `internal/agent/watcher_test.go`

`watcher_test.go` is in `package agent_test` — the same package as `scandir_test.go`. This means it can reuse the `writeFile`, `emptySchema`, `trafficLightJSON`, and `altTrafficLightJSON` identifiers already declared in `scandir_test.go` without redeclaring them.

- [ ] **Step 1: Write the failing tests**

Create `internal/agent/watcher_test.go`:

```go
package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
)

// writeFile, emptySchema, trafficLightJSON, altTrafficLightJSON are declared
// in scandir_test.go (same package agent_test).

func TestWatcher_ReloadsOnFileChange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "traffic_light.json", trafficLightJSON) // initial: "green"

	loader := agent.NewLoader(builtins.NewRegistry(), emptySchema())
	if _, err := loader.ScanDir(dir, "core"); err != nil {
		t.Fatalf("ScanDir: %v", err)
	}

	dirs := []agent.WatchedDir{{Path: dir, ModName: "core"}}
	w := agent.NewWatcher(loader, dirs, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	time.Sleep(50 * time.Millisecond) // let fsnotify register the watch
	writeFile(t, dir, "traffic_light.json", altTrafficLightJSON) // initial: "red"
	time.Sleep(200 * time.Millisecond)

	def, ok := loader.Get("traffic_light")
	if !ok {
		t.Fatal("Get returned false after hot reload")
	}
	if def.Initial != "red" {
		t.Errorf("Initial = %q, want red (hot-reloaded definition)", def.Initial)
	}
}

func TestWatcher_RetainsPreviousOnInvalidFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "traffic_light.json", trafficLightJSON) // initial: "green"

	loader := agent.NewLoader(builtins.NewRegistry(), emptySchema())
	if _, err := loader.ScanDir(dir, "core"); err != nil {
		t.Fatalf("ScanDir: %v", err)
	}

	dirs := []agent.WatchedDir{{Path: dir, ModName: "core"}}
	w := agent.NewWatcher(loader, dirs, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	time.Sleep(50 * time.Millisecond)
	writeFile(t, dir, "traffic_light.json", `{not valid json`) // invalid
	time.Sleep(200 * time.Millisecond)

	def, ok := loader.Get("traffic_light")
	if !ok {
		t.Fatal("Get returned false — previous definition was not retained")
	}
	if def.Initial != "green" {
		t.Errorf("Initial = %q, want green (previous definition must be retained on error)", def.Initial)
	}
}

func TestWatcher_SkipsMissingDirectory(t *testing.T) {
	loader := agent.NewLoader(builtins.NewRegistry(), emptySchema())
	dirs := []agent.WatchedDir{{Path: "/nonexistent/path/behaviors", ModName: "core"}}
	w := agent.NewWatcher(loader, dirs, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Missing dir must not cause Start to return an error — it logs a warning and skips.
	if err := w.Start(ctx); err != nil {
		t.Errorf("Start returned error for missing directory: %v", err)
	}
}
```

- [ ] **Step 2: Run tests — expect compilation failure**

```bash
go test ./internal/agent/ -run TestWatcher -v
```

Expected: compilation error — `agent.WatchedDir undefined`, `agent.NewWatcher undefined`

- [ ] **Step 3: Implement Watcher**

Create `internal/agent/watcher.go`:

```go
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
		t.Stop() // cancel pending reload; we'll reschedule below
	}
	w.timers[filePath] = time.AfterFunc(w.debounce, func() {
		w.mu.Lock()
		delete(w.timers, filePath)
		w.mu.Unlock()
		_ = w.loader.ReloadFile(filePath, modName) // ReloadFile logs success/failure
	})
}

// modNameForPath returns the ModName for the directory that contains filePath,
// or "unknown" if no configured directory is a prefix of filePath.
func (w *Watcher) modNameForPath(filePath string) string {
	for _, d := range w.dirs {
		if strings.HasPrefix(filePath, d.Path) {
			return d.ModName
		}
	}
	return "unknown"
}
```

- [ ] **Step 4: Run the new tests**

```bash
go test ./internal/agent/ -run TestWatcher -v
```

Expected: all three `TestWatcher_*` tests pass. The timing-sensitive tests (`ReloadsOnFileChange`, `RetainsPreviousOnInvalidFile`) rely on a 200ms sleep — if the test environment is extremely slow, increase the sleep. `SkipsMissingDirectory` must pass cleanly with no error return.

- [ ] **Step 5: Run the full test suite**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/watcher.go internal/agent/watcher_test.go
git commit -m "feat(epic-4/story-2): add filesystem watcher with per-file debounce"
```

---

## Self-Review

**Spec coverage check:**

| Requirement | Covered by |
|-------------|------------|
| `internal/agent/watcher.go` with `Watcher` type | Task 3 |
| Uses `github.com/fsnotify/fsnotify` | Task 1 + Task 3 |
| Debounces rapid writes (per-file, 50ms default) | Task 3 `scheduleReload` |
| On change: re-parses and re-validates | Task 2 `ReloadFile` calls `LoadMachine` which calls `ParseMachine` + `ValidateMachine` |
| On success: atomically replaces definition, logs `[hot-reload]` | Task 2 `ReloadFile` |
| On failure: retains previous definition, logs failure | Task 2 `ReloadFile` — `LoadMachine` only writes on success |
| `Watcher.Start(ctx)` blocks until ctx cancelled | Task 3 |
| Missing dirs: skip with logged warning | Task 3 `fw.Add` error path |
| `watcher_test.go` with real temp dirs, no mocks | Task 3 |
| All new code tested, `go test ./...` passes | Tasks 2 and 3 |

**No placeholders found.**

**Type consistency:** `WatchedDir` defined in Task 3 `watcher.go`, used in Task 3 test. `Loader.ReloadFile` defined in Task 2, called in Task 3 `scheduleReload`. All consistent.
