package config

// Internal test file: package config (not config_test) so tests can access
// and reset the private instance variable between cases.

import (
	"os"
	"path/filepath"
	"testing"
)

func resetInstance(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { instance = nil })
	instance = nil
}

func TestInit_LoadsValidFile(t *testing.T) {
	resetInstance(t)

	raw := `
[database]
path = "./myworld.db"

[schema]
path = "./myschema.json"

[[mods]]
name      = "core"
behaviors = "./behaviors/"
`
	f := filepath.Join(t.TempDir(), "game.toml")
	if err := os.WriteFile(f, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Init(f); err != nil {
		t.Fatalf("Init: unexpected error: %v", err)
	}

	cfg := Get()
	if cfg.Database.Path != "./myworld.db" {
		t.Errorf("Database.Path = %q, want ./myworld.db", cfg.Database.Path)
	}
	if cfg.Schema.Path != "./myschema.json" {
		t.Errorf("Schema.Path = %q, want ./myschema.json", cfg.Schema.Path)
	}
	if len(cfg.Mods) != 1 || cfg.Mods[0].Name != "core" {
		t.Errorf("Mods = %v, want [{Name:core}]", cfg.Mods)
	}
}

func TestInit_MissingFile_UsesDefaults(t *testing.T) {
	resetInstance(t)

	err := Init("/nonexistent/path/game.toml")
	if err != nil {
		t.Fatalf("Init: expected no error for missing file, got: %v", err)
	}

	cfg := Get()
	defaults := Defaults()
	if cfg.Database.Path != defaults.Database.Path {
		t.Errorf("Database.Path = %q, want default %q", cfg.Database.Path, defaults.Database.Path)
	}
	if cfg.Schema.Path != defaults.Schema.Path {
		t.Errorf("Schema.Path = %q, want default %q", cfg.Schema.Path, defaults.Schema.Path)
	}
}

func TestInit_InvalidTOML_ReturnsError(t *testing.T) {
	resetInstance(t)

	f := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(f, []byte("[[[[invalid"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Init(f); err == nil {
		t.Fatal("Init: expected error for invalid TOML, got nil")
	}
}

func TestInit_InvalidTOML_LeavesInstanceNil(t *testing.T) {
	resetInstance(t)

	f := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(f, []byte("[[[[invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = Init(f) // error expected, ignore

	if instance != nil {
		t.Error("instance should remain nil after failed Init")
	}
}

func TestGet_BeforeInit_Panics(t *testing.T) {
	resetInstance(t)

	defer func() {
		if r := recover(); r == nil {
			t.Error("Get() should panic before Init, but did not")
		}
	}()
	Get()
}

func TestInit_SecondCallReplacesFirst(t *testing.T) {
	resetInstance(t)

	first := `
[database]
path = "./first.db"
[schema]
path = "./schema.json"
`
	second := `
[database]
path = "./second.db"
[schema]
path = "./schema.json"
`
	dir := t.TempDir()
	f1 := filepath.Join(dir, "first.toml")
	f2 := filepath.Join(dir, "second.toml")
	if err := os.WriteFile(f1, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Init(f1); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if err := Init(f2); err != nil {
		t.Fatalf("second Init: %v", err)
	}

	if Get().Database.Path != "./second.db" {
		t.Errorf("Database.Path = %q, want ./second.db", Get().Database.Path)
	}
}
