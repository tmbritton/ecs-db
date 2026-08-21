package editable_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/editable"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// The test codec in editable_test.go uses encoding/json, which is byte-stable
// by construction. The real ones are stable because stories 2 and 3 made them
// so, and the dirty comparison is only exact if that holds. These exercise the
// actual pairs Forge will use, against the actual files in this repo.

func TestFile_WithSchemaMarshal(t *testing.T) {
	original, err := os.ReadFile("../../../schema.json")
	if err != nil {
		t.Skipf("schema.json not readable: %v", err)
	}
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	f, err := editable.Open(path, editable.Codec[schema.DatabaseSchema]{
		Marshal:   schema.Marshal,
		Unmarshal: schema.LoadSchema,
		Validate:  schema.ValidateSchema,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// The real file, freshly opened, must read as clean. If the serialiser
	// were not byte-stable this would be dirty on arrival and the save footer
	// would light up for a file nobody touched.
	if dirty, err := f.Dirty(); err != nil || dirty {
		t.Fatalf("Dirty = %v (err %v) on open, want false", dirty, err)
	}

	f.Current.SchemaVersion++
	if dirty, _ := f.Dirty(); !dirty {
		t.Error("bumping schemaVersion did not register as dirty")
	}
	f.Current.SchemaVersion--
	if dirty, _ := f.Dirty(); dirty {
		t.Error("reverting the bump did not return to clean")
	}

	// A real save must produce a file the loader accepts.
	f.Current.SchemaVersion = 9
	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	reloaded, err := schema.LoadSchema(saved)
	if err != nil {
		t.Fatalf("the saved schema does not load: %v", err)
	}
	if reloaded.SchemaVersion != 9 {
		t.Errorf("SchemaVersion = %d, want 9", reloaded.SchemaVersion)
	}
}

// Set holds heterogeneous files behind an unexported interface method. That
// seals it against outside implementations, which is intended — but it must
// still be usable from outside, which only an external-package test can show.
func TestSet_IsUsableFromAnotherPackage(t *testing.T) {
	schemaRaw, err := os.ReadFile("../../../schema.json")
	if err != nil {
		t.Skipf("schema.json not readable: %v", err)
	}
	machineRaw, err := os.ReadFile("../../../behaviors/goblin.json")
	if err != nil {
		t.Skipf("goblin.json not readable: %v", err)
	}

	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.json")
	machinePath := filepath.Join(dir, "goblin.json")
	if err := os.WriteFile(schemaPath, schemaRaw, 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if err := os.WriteFile(machinePath, machineRaw, 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	sf, err := editable.Open(schemaPath, editable.Codec[schema.DatabaseSchema]{
		Marshal: schema.Marshal, Unmarshal: schema.LoadSchema, Validate: schema.ValidateSchema,
	})
	if err != nil {
		t.Fatalf("Open schema: %v", err)
	}
	mf, err := editable.Open(machinePath, editable.Codec[*agent.MachineDefinition]{
		Marshal: agent.EmitMachine, Unmarshal: agent.ParseMachine,
	})
	if err != nil {
		t.Fatalf("Open machine: %v", err)
	}

	// Two different value types, one set — which is the whole point.
	set := editable.NewSet()
	set.Add(sf)
	set.Add(mf)

	if any, err := set.Any(); err != nil || any {
		t.Fatalf("Any = %v (err %v) on open, want false", any, err)
	}
	mf.Current.Initial = "wandering"
	dirty, err := set.Dirty()
	if err != nil {
		t.Fatalf("Dirty: %v", err)
	}
	if len(dirty) != 1 || dirty[0] != machinePath {
		t.Errorf("dirty = %v, want just the machine", dirty)
	}
}

func TestFile_WithMachineEmitter(t *testing.T) {
	original, err := os.ReadFile("../../../behaviors/goblin.json")
	if err != nil {
		t.Skipf("goblin.json not readable: %v", err)
	}
	path := filepath.Join(t.TempDir(), "goblin.json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	f, err := editable.Open(path, editable.Codec[*agent.MachineDefinition]{
		Marshal:   agent.EmitMachine,
		Unmarshal: agent.ParseMachine,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if dirty, err := f.Dirty(); err != nil || dirty {
		t.Fatalf("Dirty = %v (err %v) on open, want false", dirty, err)
	}

	was := f.Current.Initial
	f.Current.Initial = "wandering"
	if dirty, _ := f.Dirty(); !dirty {
		t.Error("changing the initial state did not register as dirty")
	}
	f.Current.Initial = was
	if dirty, _ := f.Dirty(); dirty {
		t.Error("reverting did not return to clean")
	}

	f.Current.Initial = "wandering"
	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	def, err := agent.ParseMachine(saved)
	if err != nil {
		t.Fatalf("the saved machine does not parse: %v", err)
	}
	if def.Initial != "wandering" {
		t.Errorf("initial = %q, want wandering", def.Initial)
	}

	// Discard restores the last *saved* state, not the state at open — the
	// snapshot advances on save, which is what makes a saved file clean.
	f.Current.Initial = "nonsense"
	if err := f.Discard(); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if f.Current.Initial != "wandering" {
		t.Errorf("after discard initial = %q, want the last saved value", f.Current.Initial)
	}
	if dirty, _ := f.Dirty(); dirty {
		t.Error("discard left the file dirty")
	}
	_ = was
}
