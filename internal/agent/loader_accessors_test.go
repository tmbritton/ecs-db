package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

func accessorLoader(t *testing.T) (*Loader, string) {
	t.Helper()
	dir := t.TempDir()
	s := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {Type: "object", Properties: map[string]schema.Property{"x": {Type: "number"}}},
		},
		EntityTypes: map[string]schema.EntityType{
			"Thing": {RequiredComponents: []string{"Position"}, ValidationLevel: schema.ValidationStrict},
		},
	}
	return NewLoader(NewRegistry(), s), dir
}

func writeMachine(t *testing.T, dir, id string) string {
	t.Helper()
	p := filepath.Join(dir, id+".json")
	body := `{"id":"` + id + `","initial":"idle","states":{"idle":{}}}`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("writing machine: %v", err)
	}
	return p
}

// The machine-list panel in AGENTS mode has nothing to call today: machines and
// sources are private. These are the accessors it needs.
func TestLoader_ListAndSources(t *testing.T) {
	l, dir := accessorLoader(t)
	writeMachine(t, dir, "alpha")
	writeMachine(t, dir, "beta")

	if _, err := l.ScanDir(dir, "core"); err != nil {
		t.Fatalf("ScanDir: %v", err)
	}

	list := l.List()
	if len(list) != 2 {
		t.Fatalf("List() has %d machines, want 2: %v", len(list), list)
	}
	for _, id := range []string{"alpha", "beta"} {
		def, ok := list[id]
		if !ok {
			t.Errorf("List() is missing %q", id)
			continue
		}
		if def.ID != id {
			t.Errorf("List()[%q].ID = %q", id, def.ID)
		}
	}

	sources := l.Sources()
	if len(sources) != 2 {
		t.Fatalf("Sources() has %d entries, want 2: %v", len(sources), sources)
	}
	want := "core:" + filepath.Join(dir, "alpha.json")
	if sources["alpha"] != want {
		t.Errorf("Sources()[\"alpha\"] = %q, want %q", sources["alpha"], want)
	}
}

// The loader's maps are mutated by ReloadFile under lock, driven by the watcher
// goroutine. Handing the real map out would race with it — and a caller that
// deleted a key would silently unload a machine from the running engine.
func TestLoader_AccessorsReturnCopies(t *testing.T) {
	l, dir := accessorLoader(t)
	writeMachine(t, dir, "alpha")
	if _, err := l.ScanDir(dir, "core"); err != nil {
		t.Fatalf("ScanDir: %v", err)
	}

	list := l.List()
	delete(list, "alpha")
	list["injected"] = &MachineDefinition{ID: "injected"}

	sources := l.Sources()
	delete(sources, "alpha")
	sources["injected"] = "nowhere"

	if _, ok := l.Get("alpha"); !ok {
		t.Error("deleting from the List() copy unloaded the machine from the loader")
	}
	if got := l.List(); len(got) != 1 || got["alpha"] == nil {
		t.Errorf("the loader's machines were mutated through List(): %v", got)
	}
	if got := l.Sources(); len(got) != 1 || got["injected"] != "" {
		t.Errorf("the loader's sources were mutated through Sources(): %v", got)
	}
}

// Empty rather than nil: a caller ranging over the result should not have to
// special-case a project with no machines yet.
func TestLoader_AccessorsOnAnEmptyLoader(t *testing.T) {
	l, _ := accessorLoader(t)
	if got := l.List(); got == nil || len(got) != 0 {
		t.Errorf("List() on an empty loader = %v, want an empty map", got)
	}
	if got := l.Sources(); got == nil || len(got) != 0 {
		t.Errorf("Sources() on an empty loader = %v, want an empty map", got)
	}
}

// The accessors read maps the watcher goroutine writes through ReloadFile. The
// copy tests above pass with the locks removed — only the race detector, with
// the two running concurrently, shows that the locking is load-bearing.
func TestLoader_AccessorsAreSafeAlongsideReload(t *testing.T) {
	l, dir := accessorLoader(t)
	path := writeMachine(t, dir, "alpha")
	if _, err := l.ScanDir(dir, "core"); err != nil {
		t.Fatalf("ScanDir: %v", err)
	}

	const iterations = 200
	done := make(chan struct{}, 2)

	go func() {
		defer func() { done <- struct{}{} }()
		for range iterations {
			if err := l.ReloadFile(path, "core", nil); err != nil {
				t.Errorf("ReloadFile: %v", err)
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for range iterations {
			for id := range l.List() {
				_ = id
			}
			for id := range l.Sources() {
				_ = id
			}
		}
	}()

	<-done
	<-done
}
