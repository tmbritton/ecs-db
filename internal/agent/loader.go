package agent

import (
	"fmt"
	"os"
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
}

func NewLoader(registry *Registry, schema schema.DatabaseSchema) *Loader {
	return &Loader{
		registry: registry,
		schema:   schema,
		machines: make(map[string]*MachineDefinition),
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

// Get returns the currently active definition for machineID, or (nil, false)
// if the machine has never been successfully loaded.
func (l *Loader) Get(machineID string) (*MachineDefinition, bool) {
	def, ok := l.machines[machineID]
	return def, ok
}
