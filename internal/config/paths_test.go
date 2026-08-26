package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Paths in game.toml name files near game.toml, not near wherever the process
// happens to have been started. Resolving them at load is what makes every
// consumer agree: the engine reads them, Forge's project model reads them, and
// the engine-status readout reads them, and until this they did not have to
// mean the same thing.
func TestLoad_ResolvesPathsRelativeToTheConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "game.toml")
	body := `
[database]
path = "./world.db"

[schema]
path = "./schema.json"

[[mods]]
name      = "core"
behaviors = "./behaviors/"
actions   = "./actions/"
guards    = "./guards/"
assets    = "./assets/"

[map]
path = "maps/level1.tmx"
`
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := map[string]string{
		"database":      filepath.Join(dir, "world.db"),
		"schema":        filepath.Join(dir, "schema.json"),
		"map":           filepath.Join(dir, "maps", "level1.tmx"),
		"mod.behaviors": filepath.Join(dir, "behaviors"),
		"mod.actions":   filepath.Join(dir, "actions"),
		"mod.guards":    filepath.Join(dir, "guards"),
		"mod.assets":    filepath.Join(dir, "assets"),
	}
	got := map[string]string{
		"database":      cfg.Database.Path,
		"schema":        cfg.Schema.Path,
		"map":           cfg.Map.Path,
		"mod.behaviors": cfg.Mods[0].Behaviors,
		"mod.actions":   cfg.Mods[0].Actions,
		"mod.guards":    cfg.Mods[0].Guards,
		"mod.assets":    cfg.Mods[0].Assets,
	}
	for key, w := range want {
		if got[key] != w {
			t.Errorf("%s = %q, want %q", key, got[key], w)
		}
	}
}

// An absolute path means what it says and must be left alone.
func TestLoad_LeavesAbsolutePathsAlone(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "game.toml")
	abs := filepath.Join(t.TempDir(), "elsewhere", "schema.json")
	body := "[schema]\npath = \"" + abs + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Schema.Path != abs {
		t.Errorf("Schema.Path = %q, want the absolute path unchanged", cfg.Schema.Path)
	}
}

// An empty path means "not configured" and must stay empty. Resolving it would
// produce the project directory, which run.go and project.Open both treat as a
// real location — an assets-only mod would suddenly declare a behaviors
// directory, and every stray .json in the project root would be scanned.
func TestLoad_LeavesEmptyPathsEmpty(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "game.toml")
	body := `
[[mods]]
name   = "artpack"
assets = "./art/"
`
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Mods[0].Behaviors != "" {
		t.Errorf("Behaviors = %q, want empty — the mod declares none", cfg.Mods[0].Behaviors)
	}
	if cfg.Mods[0].Actions != "" || cfg.Mods[0].Guards != "" {
		t.Errorf("an undeclared path was resolved: %+v", cfg.Mods[0])
	}
	if cfg.Mods[0].Assets != filepath.Join(dir, "art") {
		t.Errorf("Assets = %q, want it resolved", cfg.Mods[0].Assets)
	}
}

// A config omitting a section leaves that path empty — Load does not apply the
// file defaults, only the [forge] ones. Resolution must not turn that empty
// string into the project directory.
func TestLoad_OmittedSectionsStayEmpty(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "game.toml")
	if err := os.WriteFile(cfgPath, []byte("[window]\ntitle = \"x\"\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Schema.Path != "" {
		t.Errorf("Schema.Path = %q, want empty for an omitted section", cfg.Schema.Path)
	}
	if cfg.Database.Path != "" {
		t.Errorf("Database.Path = %q, want empty for an omitted section", cfg.Database.Path)
	}
}

// Defaults() has no config file to resolve against, so its paths stay relative.
// That is the "no game.toml anywhere" case, where the working directory is the
// only thing they can mean.
func TestDefaults_PathsStayRelative(t *testing.T) {
	cfg := Defaults()
	if filepath.IsAbs(cfg.Schema.Path) {
		t.Errorf("Defaults().Schema.Path = %q, want it left relative", cfg.Schema.Path)
	}
}
