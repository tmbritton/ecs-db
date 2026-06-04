package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"

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
	Actions   string `toml:"actions"`
	Guards    string `toml:"guards"`
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
