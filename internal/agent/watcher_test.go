package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
)

// writeFile, emptySchema, trafficLightJSON, altTrafficLightJSON are declared
// in scandir_test.go (same package agent_test — no redeclaration needed).

func TestWatcher_ReloadsOnFileChange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "traffic_light.json", trafficLightJSON) // initial: "green"

	loader := agent.NewLoader(builtins.NewRegistry(), emptySchema())
	if _, err := loader.ScanDir(dir, "core"); err != nil {
		t.Fatalf("ScanDir: %v", err)
	}

	dirs := []agent.WatchedDir{{Path: dir, ModName: "core"}}
	w := agent.NewWatcher(loader, dirs, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	time.Sleep(50 * time.Millisecond)                            // let fsnotify register the watch
	writeFile(t, dir, "traffic_light.json", altTrafficLightJSON) // initial: "red"
	time.Sleep(200 * time.Millisecond)

	def, ok := loader.Get("traffic_light")
	if !ok {
		t.Fatal("Get returned false after hot reload")
	}
	if def.Initial != "red" {
		t.Errorf("Initial = %q, want red (hot-reloaded definition)", def.Initial)
	}
}

func TestWatcher_RetainsPreviousOnInvalidFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "traffic_light.json", trafficLightJSON) // initial: "green"

	loader := agent.NewLoader(builtins.NewRegistry(), emptySchema())
	if _, err := loader.ScanDir(dir, "core"); err != nil {
		t.Fatalf("ScanDir: %v", err)
	}

	dirs := []agent.WatchedDir{{Path: dir, ModName: "core"}}
	w := agent.NewWatcher(loader, dirs, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	time.Sleep(50 * time.Millisecond)
	writeFile(t, dir, "traffic_light.json", `{not valid json`) // invalid
	time.Sleep(200 * time.Millisecond)

	def, ok := loader.Get("traffic_light")
	if !ok {
		t.Fatal("Get returned false — previous definition was not retained")
	}
	if def.Initial != "green" {
		t.Errorf("Initial = %q, want green (previous definition must be retained on error)", def.Initial)
	}
}

func TestWatcher_SkipsMissingDirectory(t *testing.T) {
	loader := agent.NewLoader(builtins.NewRegistry(), emptySchema())
	dirs := []agent.WatchedDir{{Path: "/nonexistent/path/behaviors", ModName: "core"}}
	w := agent.NewWatcher(loader, dirs, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Missing dir must not cause Start to return an error — it logs a warning and skips.
	if err := w.Start(ctx); err != nil {
		t.Errorf("Start returned error for missing directory: %v", err)
	}
}
