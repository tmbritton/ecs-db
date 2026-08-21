package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The Forge server binds to whatever Addr says. If a config file omits the
// [forge] section entirely, Addr must still come back as loopback — an empty
// string would make net/http listen on every interface, which is the opposite
// of what an authoring tool that reads and writes project files should do.
func TestLoad_ForgeDefaults(t *testing.T) {
	tests := []struct {
		name     string
		toml     string
		wantAddr string
	}{
		{
			name:     "forge section omitted falls back to loopback",
			toml:     "[database]\npath = \"./x.db\"\n",
			wantAddr: DefaultForgeAddr,
		},
		{
			name:     "empty forge section falls back to loopback",
			toml:     "[forge]\n",
			wantAddr: DefaultForgeAddr,
		},
		{
			name:     "explicit addr is honoured",
			toml:     "[forge]\naddr = \"0.0.0.0:9000\"\n",
			wantAddr: "0.0.0.0:9000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempConfig(t, tt.toml)
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Forge.Addr != tt.wantAddr {
				t.Errorf("Forge.Addr = %q, want %q", cfg.Forge.Addr, tt.wantAddr)
			}
		})
	}
}

func TestDefaults_ForgeAddr(t *testing.T) {
	if got := Defaults().Forge.Addr; got != DefaultForgeAddr {
		t.Errorf("Defaults().Forge.Addr = %q, want %q", got, DefaultForgeAddr)
	}
}

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "game.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return p
}

// A zero poll interval panics time.NewTicker, so both the defaulting and the
// accessor's own guard are pinned — the accessor is what the server calls, and
// it must be safe even for a Config that never went through Load.
func TestForge_PollInterval(t *testing.T) {
	tests := []struct {
		name string
		toml string
		want time.Duration
	}{
		{
			name: "forge section omitted",
			want: DefaultForgePollSeconds * time.Second,
		},
		{
			name: "empty forge section",
			toml: "[forge]\n",
			want: DefaultForgePollSeconds * time.Second,
		},
		{
			name: "explicit value is honoured",
			toml: "[forge]\npollSeconds = 5\n",
			want: 5 * time.Second,
		},
		{
			name: "zero falls back rather than panicking the ticker",
			toml: "[forge]\npollSeconds = 0\n",
			want: DefaultForgePollSeconds * time.Second,
		},
		{
			name: "negative falls back",
			toml: "[forge]\npollSeconds = -3\n",
			want: DefaultForgePollSeconds * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "game.toml")
			body := "[database]\npath = \"./x.db\"\n" + tt.toml
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("writing config: %v", err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := cfg.Forge.PollInterval(); got != tt.want {
				t.Errorf("PollInterval() = %v, want %v", got, tt.want)
			}
		})
	}

	// A Config built in code, never defaulted, must still be safe.
	if got := (ForgeConfig{}).PollInterval(); got != DefaultForgePollSeconds*time.Second {
		t.Errorf("zero ForgeConfig.PollInterval() = %v, want the default", got)
	}
}
