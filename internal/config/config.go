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
