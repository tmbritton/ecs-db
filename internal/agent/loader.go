package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// Loader reads, parses, and validates machine definition files. It retains
// the last successfully validated definition for each machine ID so that a
// failed hot-reload leaves the previous version in service.
type Loader struct {
	registry *Registry
	schema   schema.DatabaseSchema
	machines map[string]*MachineDefinition
	sources  map[string]string // machineID → "modName:filepath"
}

func NewLoader(registry *Registry, schema schema.DatabaseSchema) *Loader {
	return &Loader{
		registry: registry,
		schema:   schema,
		machines: make(map[string]*MachineDefinition),
		sources:  make(map[string]string),
	}
}

// LoadMachine reads the file at path, parses and validates it. On success the
// new definition replaces any previously loaded definition with the same ID
// and is returned. On any error the previous definition (if any) is retained
// and the error is returned.
func (l *Loader) LoadMachine(path string) (*MachineDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading machine file %q: %w", path, err)
	}

	def, err := ParseMachine(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %q: %w", path, err)
	}

	errs := ValidateMachine(def, l.registry, l.schema)
	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return nil, fmt.Errorf("validation failed for %q: %s", def.ID, strings.Join(msgs, "; "))
	}

	l.machines[def.ID] = def
	return def, nil
}

// ScanDir loads all *.json files from dir under the given modName. Files that
// fail to parse or validate are collected as errors and returned after all
// files are attempted. If the same machine ID was already loaded (from a prior
// ScanDir call), the new definition wins and a warning is printed.
func (l *Loader) ScanDir(dir, modName string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("ScanDir %q: %w", dir, err)
	}

	var errs []string
	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		def, err := l.LoadMachine(path)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		src := modName + ":" + path
		if prev, ok := l.sources[def.ID]; ok {
			fmt.Printf("[config] machine %q overridden: was %s, now %s\n", def.ID, prev, src)
		}
		l.sources[def.ID] = src
		count++
	}

	if len(errs) > 0 {
		return count, fmt.Errorf("ScanDir %q: %s", dir, strings.Join(errs, "; "))
	}
	return count, nil
}

// Get returns the currently active definition for machineID, or (nil, false)
// if the machine has never been successfully loaded.
func (l *Loader) Get(machineID string) (*MachineDefinition, bool) {
	def, ok := l.machines[machineID]
	return def, ok
}
