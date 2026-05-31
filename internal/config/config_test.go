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
