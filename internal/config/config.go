package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

var (
	mu       sync.RWMutex
	instance *Config
)

// Init loads config from path, falling back to Defaults() if the file is
// absent. Returns an error only if the file exists but cannot be parsed.
// Must be called once at application startup before any call to Get.
func Init(path string) error {
	cfg, err := Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "Config file %q not found, using defaults\n", path)
		mu.Lock()
		instance = Defaults()
		mu.Unlock()
		return nil
	}
	if err != nil {
		return err
	}
	mu.Lock()
	instance = cfg
	mu.Unlock()
	return nil
}

// Get returns the loaded configuration. Panics if Init has not been called.
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	if instance == nil {
		panic("config: Get called before Init")
	}
	return instance
}

type MapConfig struct {
	Path string `toml:"path"`
}

// DefaultForgeAddr binds the Forge editor to loopback. Forge reads and writes
// project files; it is a local authoring tool, not a service to expose.
const DefaultForgeAddr = "127.0.0.1:7777"

// DefaultForgePollSeconds is how often Forge re-checks the game database for
// the engine-status readout. Taken from config rather than hard-coded so Epic
// 17's Preferences dialog has something to bind to.
const DefaultForgePollSeconds = 2

// ForgeConfig configures the Forge content editor served by `ecs-db forge`.
type ForgeConfig struct {
	Addr        string `toml:"addr"`
	PollSeconds int    `toml:"pollSeconds"`
}

// PollInterval is the status poll period. A zero or negative configured value
// would panic time.NewTicker, so it falls back rather than trusting the file.
func (f ForgeConfig) PollInterval() time.Duration {
	if f.PollSeconds <= 0 {
		return DefaultForgePollSeconds * time.Second
	}
	return time.Duration(f.PollSeconds) * time.Second
}

type Config struct {
	Database DatabaseConfig `toml:"database"`
	Schema   SchemaConfig   `toml:"schema"`
	Mods     []ModConfig    `toml:"mods"`
	Window   WindowConfig   `toml:"window"`
	Map      MapConfig      `toml:"map"`
	Forge    ForgeConfig    `toml:"forge"`
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
	Actions   string `toml:"actions"`
	Guards    string `toml:"guards"`
	Assets    string `toml:"assets"`
}

type WindowConfig struct {
	Title    string `toml:"title"`
	Width    int    `toml:"width"`
	Height   int    `toml:"height"`
	TileSize int    `toml:"tileSize"`
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
	applyDefaults(&cfg)
	resolvePaths(&cfg, filepath.Dir(path))
	return &cfg, nil
}

// resolvePaths rewrites every relative path in the config to be relative to the
// config file that declared it.
//
// A path in game.toml names a file near game.toml, not near wherever the
// process happened to be started. Without this, `ecs-db run -c ../other/game.toml`
// looks for schema.json in the current directory, and — the way this surfaced —
// the engine and Forge disagreed about which file a config meant. The engine
// read these fields verbatim while internal/forge/project resolved them against
// the config, and the repo's own project hid it completely: game.toml sits at
// the repo root with "./schema.json", where both readings coincide.
//
// Resolving once, here, is what makes every consumer agree by construction
// rather than by each remembering to.
func resolvePaths(cfg *Config, root string) {
	rel := func(p string) string {
		// Empty means "not configured" and must stay that way. Resolving it
		// would yield the project directory, which callers treat as a real
		// location: an assets-only mod would suddenly declare a behaviors
		// directory and every stray .json in the project root would be scanned
		// as a machine.
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Clean(filepath.Join(root, p))
	}

	cfg.Database.Path = rel(cfg.Database.Path)
	cfg.Schema.Path = rel(cfg.Schema.Path)
	cfg.Map.Path = rel(cfg.Map.Path)
	for i := range cfg.Mods {
		cfg.Mods[i].Behaviors = rel(cfg.Mods[i].Behaviors)
		cfg.Mods[i].Actions = rel(cfg.Mods[i].Actions)
		cfg.Mods[i].Guards = rel(cfg.Mods[i].Guards)
		cfg.Mods[i].Assets = rel(cfg.Mods[i].Assets)
	}
}

// applyDefaults fills in values whose zero value would be actively wrong, for
// config files that omit the section entirely.
func applyDefaults(cfg *Config) {
	if cfg.Forge.Addr == "" {
		cfg.Forge.Addr = DefaultForgeAddr
	}
	if cfg.Forge.PollSeconds <= 0 {
		cfg.Forge.PollSeconds = DefaultForgePollSeconds
	}
}

// Defaults returns a Config with built-in defaults, used when no config file
// is present.
func Defaults() *Config {
	cfg := defaults()
	applyDefaults(cfg)
	return cfg
}

func defaults() *Config {
	return &Config{
		Database: DatabaseConfig{Path: "./ecs.db"},
		Schema:   SchemaConfig{Path: "./schema.json"},
		Window: WindowConfig{
			Title:    "ECS Demo",
			Width:    640,
			Height:   480,
			TileSize: 16,
		},
		// No map. It used to be "mods/map/level1.toml", which was this
		// repository's own level file — a guess about somebody else's directory
		// layout that happened to be right in exactly one project, and that
		// named a file which no longer exists once Epic 14 story 7 migrated it.
		//
		// These defaults are what a run with no game.toml at all gets, and a
		// project with no config file has no level either. run handles an empty
		// map path deliberately: no tiles, no tilemap renderer, and no advice
		// about adding a Player object to a file nobody named.
	}
}
