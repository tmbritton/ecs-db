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
	if _, err := loader.ScanDir(dir1, "core"); err != nil {
		t.Fatalf("first ScanDir: %v", err)
	}
	if _, err := loader.ScanDir(dir2, "override-mod"); err != nil {
		t.Fatalf("second ScanDir: %v", err)
	}

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
