package config

import (
	"os"
	"path/filepath"
	"testing"
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
