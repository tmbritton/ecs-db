package agent

import "testing"

// Compile-time interface satisfaction checks. These lines fail to compile if
// any method signature is wrong — catching drift before runtime.
var (
	_ ActionHandler = (*testActionHandler)(nil)
	_ GuardHandler  = (*testGuardHandler)(nil)
	_ WorldWriter   = (*testWorldWriter)(nil)
	_ WorldReader   = (*testWorldReader)(nil)
)

// testActionHandler is a test double for ActionHandler.
type testActionHandler struct{ runErr error }

func (h *testActionHandler) Run(ActionContext) error { return h.runErr }

// testGuardHandler is a test double for GuardHandler.
type testGuardHandler struct{ result bool }

func (h *testGuardHandler) Evaluate(GuardContext) bool { return h.result }

// testWorldWriter is a test double for WorldWriter.
type testWorldWriter struct{}

func (w *testWorldWriter) SpawnEntity(entityType string) (int64, error) { return 1, nil }
func (w *testWorldWriter) AttachComponent(entityID int64, compName string, values map[string]any) error {
	return nil
}
func (w *testWorldWriter) DetachComponent(entityID int64, compName string) error { return nil }
func (w *testWorldWriter) SetComponentValue(entityID int64, compName, field string, value any) error {
	return nil
}

// testWorldReader is a test double for WorldReader.
type testWorldReader struct{}

func (r *testWorldReader) GetComponentValue(entityID int64, compName, field string) (any, error) {
	return nil, nil
}

func (r *testWorldReader) HasComponent(entityID int64, compName string) (bool, error) {
	return false, nil
}

func TestContextTypes_Compile(t *testing.T) {
	ac := ActionContext{
		EntityID: 1,
		Tick:     10,
		World:    &testWorldWriter{},
		Params:   map[string]any{"k": "v"},
		Event:    Event{Type: "TEST", Payload: map[string]any{"x": 1}},
	}
	gc := GuardContext{
		EntityID: 1,
		Tick:     10,
		World:    &testWorldReader{},
		Params:   map[string]any{},
		Event:    Event{Type: "TEST"},
	}
	_ = ac
	_ = gc
}
