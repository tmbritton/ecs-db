package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
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
	return &cfg, nil
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
		Map: MapConfig{Path: "mods/map/level1.toml"},
	}
}
