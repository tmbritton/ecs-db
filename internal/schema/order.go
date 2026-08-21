package schema

import "github.com/tmbritton/ecs-db/internal/jsonorder"

// The key-order machinery lives in internal/jsonorder because the XState
// emitter needs exactly the same thing, and two copies of "what order were
// these keys in" would drift.
var keyOrder = jsonorder.Keys

func orderedKeys[T any](recorded []string, present map[string]T) []string {
	return jsonorder.Apply(recorded, present)
}
