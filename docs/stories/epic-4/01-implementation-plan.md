# Config File Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace all hardcoded paths in `cmd/cli/main.go` with a TOML config file, and support multiple mod directories for behaviors and assets with duplicate-ID detection.

**Architecture:** New `internal/config` package holds the `Config` struct and TOML loader. The existing `agent.Loader` gains a `ScanDir` method and a `sources` map for duplicate tracking. The CLI gains a `-config` flag (default `./game.toml`) that falls back to built-in defaults when the file is absent, so the binary still works out-of-the-box.

**Tech Stack:** Go stdlib + `github.com/BurntSushi/toml` (de facto standard Go TOML library, pure Go, no CGo).

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/config/config.go` | Create | `Config` struct, `Load()`, `Defaults()` |
| `internal/config/config_test.go` | Create | Parse, defaults, missing-file, path-resolution tests |
| `internal/agent/loader.go` | Modify | Add `sources` map + `ScanDir(dir, modName string)` method |
| `internal/agent/loader_test.go` | Create | ScanDir happy path, duplicate detection |
| `cmd/cli/main.go` | Modify | `-config` flag, load config, use config paths |
| `game.toml` (project root) | Create | Default config file checked into the repo |

---

## Context

`cmd/cli/main.go` currently hardcodes `./schema.json` and `./ecs.db`. There is no flag parsing and no support for loading machine files at all. The project is being designed for moddability: a core game directory plus optional mod directories that each contribute behavior machines and assets. Load order is explicit (array in config); later mods override earlier ones on duplicate machine IDs, with a logged warning.

---

## Task 1: Add TOML dependency

**Files:**
- Modify: `go.mod` / `go.sum` (via `go get`)

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/BurntSushi/toml@latest
```

- [ ] **Step 2: Verify it resolves**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add BurntSushi/toml dependency"
```

---

## Task 2: Create `internal/config` package

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

### Config struct design

```toml
# game.toml — example

[database]
path = "./ecs.db"

[schema]
path = "./schema.json"

# Mods are loaded in array order; later entries override earlier ones on
# duplicate machine IDs. The first entry is conventionally the core game.
[[mods]]
name      = "core"
behaviors = "./behaviors/"
actions   = "./actions/"
guards    = "./guards/"
assets    = "./assets/"

# [[mods]]
# name      = "my-mod"
# behaviors = "./mods/my-mod/behaviors/"
# actions   = "./mods/my-mod/actions/"
# guards    = "./mods/my-mod/guards/"
# assets    = "./mods/my-mod/assets/"
```

- [ ] **Step 1: Write the failing tests in `internal/config/config_test.go`**

```go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tmbritton/ecs-db/internal/config"
)

func TestLoad_ParsesValidTOML(t *testing.T) {
	raw := `
[database]
path = "./myworld.db"

[schema]
path = "./myschema.json"

[[mods]]
name      = "core"
behaviors = "./behaviors/"
actions   = "./actions/"
guards    = "./guards/"
assets    = "./assets/"

[[mods]]
name      = "expansion"
behaviors = "./mods/expansion/behaviors/"
actions   = "./mods/expansion/actions/"
guards    = "./mods/expansion/guards/"
assets    = "./mods/expansion/assets/"
`
	f := filepath.Join(t.TempDir(), "game.toml")
	if err := os.WriteFile(f, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Database.Path != "./myworld.db" {
		t.Errorf("Database.Path = %q, want ./myworld.db", cfg.Database.Path)
	}
	if cfg.Schema.Path != "./myschema.json" {
		t.Errorf("Schema.Path = %q, want ./myschema.json", cfg.Schema.Path)
	}
	if len(cfg.Mods) != 2 {
		t.Fatalf("len(Mods) = %d, want 2", len(cfg.Mods))
	}
	if cfg.Mods[0].Name != "core" {
		t.Errorf("Mods[0].Name = %q, want core", cfg.Mods[0].Name)
	}
	if cfg.Mods[1].Name != "expansion" {
		t.Errorf("Mods[1].Name = %q, want expansion", cfg.Mods[1].Name)
	}
}

func TestLoad_MissingFileReturnsError(t *testing.T) {
	_, err := config.Load("/nonexistent/path/game.toml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_InvalidTOMLReturnsError(t *testing.T) {
	f := filepath.Join(t.TempDir(), "bad.toml")
	os.WriteFile(f, []byte("[[[[invalid"), 0o644)
	_, err := config.Load(f)
	if err == nil {
		t.Fatal("expected error for invalid TOML, got nil")
	}
}

func TestDefaults(t *testing.T) {
	cfg := config.Defaults()
	if cfg.Database.Path == "" {
		t.Error("Defaults().Database.Path is empty")
	}
	if cfg.Schema.Path == "" {
		t.Error("Defaults().Schema.Path is empty")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail (package doesn't exist yet)**

```bash
go test ./internal/config/... 2>&1 | head -5
```

Expected: compile error — package not found.

- [ ] **Step 3: Write `internal/config/config.go`**

```go
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Database DatabaseConfig `toml:"database"`
	Schema   SchemaConfig   `toml:"schema"`
	Mods     []ModConfig    `toml:"mods"`
}

type DatabaseConfig struct {
	Path string `toml:"path"`
}

type SchemaConfig struct {
	Path string `toml:"path"`
}

type ModConfig struct {
	Name      string `toml:"name"`
	Behaviors string `toml:"behaviors"`
	Actions   string `toml:"actions"` // Lua custom action files (consumed in a future epic)
	Guards    string `toml:"guards"`   // Lua custom guard files (consumed in a future epic)
	Assets    string `toml:"assets"`
}

// Load reads and parses a TOML config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", path, err)
	}
	return &cfg, nil
}

// Defaults returns a Config with built-in defaults, used when no config file
// is present.
func Defaults() *Config {
	return &Config{
		Database: DatabaseConfig{Path: "./ecs.db"},
		Schema:   SchemaConfig{Path: "./schema.json"},
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/config/... -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: add internal/config package with TOML loader"
```

---

## Task 3: Extend `agent.Loader` with `ScanDir` and duplicate tracking

**Files:**
- Modify: `internal/agent/loader.go`
- Create: `internal/agent/loader_test.go`

The Loader currently has `machines map[string]*MachineDefinition`. Add a `sources map[string]string` that tracks `machineID → "modName:filepath"`. When `ScanDir` loads a machine whose ID is already in `machines`, it logs a warning (last mod wins — this is intentional for overrides).

- [ ] **Step 1: Write the failing tests in `internal/agent/loader_test.go`**

```go
package agent_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/schema"
)

func emptySchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}
}

const trafficLightJSON = `{
  "id": "traffic_light",
  "initial": "green",
  "states": {
    "green":  { "on": { "NEXT": "yellow" } },
    "yellow": { "on": { "NEXT": "red"    } },
    "red":    { "on": { "NEXT": "green"  } }
  }
}`

const altTrafficLightJSON = `{
  "id": "traffic_light",
  "initial": "red",
  "states": {
    "green":  { "on": { "NEXT": "yellow" } },
    "yellow": { "on": { "NEXT": "red"    } },
    "red":    { "on": { "NEXT": "green"  } }
  }
}`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", name, err)
	}
	return path
}

func TestScanDir_LoadsAllJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "traffic_light.json", trafficLightJSON)

	loader := agent.NewLoader(builtins.NewRegistry(), emptySchema())
	n, err := loader.ScanDir(dir, "core")
	if err != nil {
		t.Fatalf("ScanDir: %v", err)
	}
	if n != 1 {
		t.Errorf("ScanDir returned %d, want 1", n)
	}

	def, ok := loader.Get("traffic_light")
	if !ok {
		t.Fatal("Get(traffic_light) = false, want true")
	}
	if def.Initial != "green" {
		t.Errorf("Initial = %q, want green", def.Initial)
	}
}

func TestScanDir_EmptyDirReturnsZero(t *testing.T) {
	dir := t.TempDir()
	loader := agent.NewLoader(builtins.NewRegistry(), emptySchema())
	n, err := loader.ScanDir(dir, "core")
	if err != nil {
		t.Fatalf("ScanDir on empty dir: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}

func TestScanDir_DuplicateMachineIDLastModWins(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, dir1, "traffic_light.json", trafficLightJSON)    // initial = "green"
	writeFile(t, dir2, "traffic_light.json", altTrafficLightJSON) // initial = "red"

	loader := agent.NewLoader(builtins.NewRegistry(), emptySchema())
	loader.ScanDir(dir1, "core")
	loader.ScanDir(dir2, "override-mod")

	def, _ := loader.Get("traffic_light")
	if def.Initial != "red" {
		t.Errorf("Initial = %q, want red (override-mod should win)", def.Initial)
	}
}

func TestScanDir_MissingDirReturnsError(t *testing.T) {
	loader := agent.NewLoader(builtins.NewRegistry(), emptySchema())
	_, err := loader.ScanDir("/nonexistent/path", "core")
	if err == nil {
		t.Fatal("expected error for missing directory, got nil")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/agent/... -run "TestScanDir" -v 2>&1 | head -15
```

Expected: compile error — `ScanDir` not defined.

- [ ] **Step 3: Modify `internal/agent/loader.go`**

Replace the entire file:

```go
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tmbritton/ecs-db/internal/schema"
)

type Loader struct {
	registry *Registry
	schema   schema.DatabaseSchema
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

	l.machines[def.ID] = def
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
		if prev, ok := l.sources[def.ID]; ok {
			fmt.Printf("[config] machine %q overridden: was %s, now %s\n", def.ID, prev, src)
		}
		l.sources[def.ID] = src
		count++
	}

	if len(errs) > 0 {
		return count, fmt.Errorf("ScanDir %q: %s", dir, strings.Join(errs, "; "))
	}
	return count, nil
}

// Get returns the currently active definition for machineID, or (nil, false)
// if the machine has never been successfully loaded.
func (l *Loader) Get(machineID string) (*MachineDefinition, bool) {
	def, ok := l.machines[machineID]
	return def, ok
}
```

- [ ] **Step 4: Run ScanDir tests**

```bash
go test ./internal/agent/... -run "TestScanDir" -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Run full suite**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/loader.go internal/agent/loader_test.go
git commit -m "feat: add ScanDir with duplicate-ID detection to agent.Loader"
```

---

## Task 4: Wire config into CLI and create default `game.toml`

**Files:**
- Modify: `cmd/cli/main.go`
- Create: `game.toml` (project root)

- [ ] **Step 1: Create `game.toml` at the project root**

```toml
# ECS Database — game configuration
# See docs/ for the full mod loading guide.

[database]
path = "./ecs.db"

[schema]
path = "./schema.json"

# Mods are loaded in order. Later entries override earlier ones when two mods
# define a machine with the same ID (a warning is printed). The first entry is
# conventionally the core game.
[[mods]]
name      = "core"
behaviors = "./behaviors/"
actions   = "./actions/"
guards    = "./guards/"
assets    = "./assets/"

# To add a mod, uncomment and fill in:
# [[mods]]
# name      = "my-mod"
# behaviors = "./mods/my-mod/behaviors/"
# actions   = "./mods/my-mod/actions/"
# guards    = "./mods/my-mod/guards/"
# assets    = "./mods/my-mod/assets/"
```

- [ ] **Step 2: Rewrite `cmd/cli/main.go`**

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
)

func main() {
	configPath := flag.String("config", "./game.toml", "path to TOML config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "Config file %q not found, using defaults\n", *configPath)
			cfg = config.Defaults()
		} else {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
	}

	// Load schema
	schemaBytes, err := os.ReadFile(cfg.Schema.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading schema from %q: %v\n", cfg.Schema.Path, err)
		os.Exit(1)
	}
	dbSchema, err := schema.LoadSchema(schemaBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing schema from %q: %v\n", cfg.Schema.Path, err)
		os.Exit(1)
	}
	if err := schema.ValidateSchema(dbSchema); err != nil {
		fmt.Fprintf(os.Stderr, "Error validating schema from %q: %v\n", cfg.Schema.Path, err)
		os.Exit(1)
	}

	hash := sha256.Sum256(schemaBytes)
	schemaHash := hex.EncodeToString(hash[:])

	// Initialize database
	db, err := storage.NewSQLiteStore(cfg.Database.Path, dbSchema, schemaHash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing database: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()
	fmt.Printf("Database initialized (schema version %d)\n", dbSchema.SchemaVersion)

	// Load behavior machines from each mod's behaviors directory
	loader := agent.NewLoader(builtins.NewRegistry(), dbSchema)
	for _, mod := range cfg.Mods {
		if mod.Behaviors == "" {
			continue
		}
		n, err := loader.ScanDir(mod.Behaviors, mod.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: loading behaviors for mod %q: %v\n", mod.Name, err)
		}
		if n > 0 {
			fmt.Printf("Loaded %d behavior(s) from mod %qu\n", n, mod.Name)
		}
	}

	fmt.Println("Ready for commands (not implemented yet)")
}
```

- [ ] **Step 3: Build to confirm it compiles**

```bash
go build ./cmd/cli/...
```

Expected: no errors.

- [ ] **Step 4: Run full test suite**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/cli/main.go game.toml
git commit -m "feat: wire TOML config into CLI; add default game.toml"
```

---

## Verification

```bash
# Full test suite
go test ./...

# Config package in isolation
go test ./internal/config/... -v

# ScanDir tests
go test ./internal/agent/... -run "TestScanDir" -v

# Build
go build ./cmd/cli/...
```

---

## Design decisions

**TOML over JSON:** Comments are essential in a user-edited config file. JSON doesn't support them; TOML does. TOML's `[[mods]]` array-of-tables syntax maps cleanly to the ordered mod list without workarounds.

**Mod load order:** Array order is explicit and intentional. Later mods override earlier ones on duplicate machine IDs (with a logged warning). This matches the mental model of "core game first, then mods on top."

**`behaviors` directory per mod, not a flat file list:** Mods typically ship a directory of machines. Scanning a directory is simpler than maintaining an explicit per-machine file list.

**`actions`, `guards`, and `assets` declared but not yet consumed:** All three fields exist in the struct and `game.toml` so mods can declare them now. `actions` and `guards` will point to directories of Lua scripts for custom behavior (consumed in the Lua integration epic). `assets` is wired in when the rendering/audio epic is built.

**`errors.Is(err, fs.ErrNotExist)` for missing config:** The binary must run out-of-the-box without a config file. Unwrapping with `errors.Is` lets us detect "file not found" precisely without string matching on error messages.
