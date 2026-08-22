package machines_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
)

// The story's criterion, and it is worded to rule out the easy version:
//
//	"A save is observed by the running engine's watcher and hot-swaps, with no
//	restart — driven end to end, not asserted from the fact that a file changed."
//
// So this runs the engine's own agent.Watcher over the same directory the
// session writes to, and asks the engine's own loader what it thinks the
// machine is afterwards. Asserting on the file would prove the save worked and
// nothing about whether a running game would ever see it.
//
// It is also the only test that exercises the reason internal/forge/atomicfile
// exists: a plain truncate-then-write leaves a window where the watcher reads
// an empty or half-written file, fails to parse it, and keeps the stale
// definition — which from here would look exactly like a save that never
// happened.
func TestSave_IsPickedUpByTheEnginesWatcher(t *testing.T) {
	s, dir := open(t, map[string]string{"wander.json": wander})
	path := filepath.Join(dir, "wander.json")

	// The engine side: its loader, its watcher, over the real directory.
	loader := agent.NewLoader(builtins.NewRegistry(), testSchema())
	if _, err := loader.ScanDir(dir, "core"); err != nil {
		t.Fatalf("ScanDir: %v", err)
	}
	if def, ok := loader.Get("wander"); !ok || def.Initial != "idle" {
		t.Fatalf("the engine did not load the machine to begin with: %v", ok)
	}

	watcher := agent.NewWatcher(loader, []agent.WatchedDir{{Path: dir, ModName: "core"}}, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = watcher.Start(ctx) }()
	time.Sleep(50 * time.Millisecond) // let fsnotify register the watch

	// The Forge side: an ordinary edit and an ordinary save.
	if err := s.Edit(path, func(d *agent.MachineDefinition) error {
		d.Initial = "moving"
		return nil
	}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	results, err := s.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(results) != 1 || !results[0].Saved {
		t.Fatalf("the save did not happen: %+v", results)
	}

	// Polled rather than slept-then-asserted: a fixed sleep either flakes or is
	// slower than it needs to be, and the failure message should say what the
	// engine ended up thinking rather than "not equal".
	deadline := time.Now().Add(3 * time.Second)
	for {
		def, ok := loader.Get("wander")
		if ok && def.Initial == "moving" {
			return
		}
		if time.Now().After(deadline) {
			got := "<not loaded>"
			if ok {
				got = def.Initial
			}
			body, _ := os.ReadFile(path)
			t.Fatalf("the running engine never saw the save: initial=%s\nfile on disk:\n%s", got, body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
