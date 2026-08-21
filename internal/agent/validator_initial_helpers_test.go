package agent

import (
	"os"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

func readFileForTest(path string) ([]byte, error) { return os.ReadFile(path) }

// realRegistryForTest mirrors what the game registers, minus the two closures
// that need a tile grid — those are in a package that imports this one, so they
// cannot be built here. Any machine relying on them will report an unregistered
// action, which the caller filters out; only "initial" errors are asserted on.
func realRegistryForTest() *Registry { return NewRegistry() }

func realSchemaForTest(t *testing.T) schema.DatabaseSchema {
	t.Helper()
	raw, err := os.ReadFile("../../schema.json")
	if err != nil {
		t.Skipf("schema.json not readable: %v", err)
	}
	s, err := schema.LoadSchema(raw)
	if err != nil {
		t.Fatalf("loading schema.json: %v", err)
	}
	return s
}
