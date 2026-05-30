package agent

import (
	"fmt"
	"testing"
)

// Compile-time interface satisfaction checks. These lines fail to compile if
// any method signature is wrong — catching drift before runtime.
var (
	_ ActionHandler = (*testActionHandler)(nil)
	_ GuardHandler  = (*testGuardHandler)(nil)
	_ WorldWriter   = (*testWorldWriter)(nil)
	_ WorldReader   = (*testWorldReader)(nil)
)

type testActionHandler struct{ runErr error }

func (h *testActionHandler) Run(ActionContext) error { return h.runErr }

type testGuardHandler struct{ result bool }

func (h *testGuardHandler) Evaluate(GuardContext) bool { return h.result }

type testWorldWriter struct{}

func (w *testWorldWriter) SpawnEntity(entityType string) (int64, error) { return 1, nil }
func (w *testWorldWriter) AttachComponent(entityID int64, compName string, values map[string]any) error {
	return nil
}
func (w *testWorldWriter) DetachComponent(entityID int64, compName string) error { return nil }
func (w *testWorldWriter) SetComponentValue(entityID int64, compName, field string, value any) error {
	return nil
}

type testWorldReader struct{}

func (r *testWorldReader) GetComponentValue(entityID int64, compName, field string) (any, error) {
	return nil, nil
}

func (r *testWorldReader) HasComponent(entityID int64, compName string) (bool, error) {
	return false, nil
}

func (r *testWorldReader) FindEntityByType(entityType string) (int64, error) {
	return 0, fmt.Errorf("FindEntityByType: not implemented in test stub")
}

func TestContextTypes_Compile(t *testing.T) {
	ac := ActionContext{
		EntityID:        1,
		Tick:            10,
		World:           &testWorldWriter{},
		Reader:          &testWorldReader{},
		Params:          map[string]any{"k": "v"},
		Event:           Event{Type: "TEST", Payload: map[string]any{"x": 1}},
		ContextManifest: map[string]string{},
	}
	gc := GuardContext{
		EntityID:        1,
		Tick:            10,
		World:           &testWorldReader{},
		Params:          map[string]any{},
		Event:           Event{Type: "TEST"},
		ContextManifest: map[string]string{},
	}
	_ = ac
	_ = gc
}

func TestNewRegistry_Empty(t *testing.T) {
	r := NewRegistry()
	if got := r.Actions(); len(got) != 0 {
		t.Errorf("Actions() = %v, want empty slice", got)
	}
	if got := r.Guards(); len(got) != 0 {
		t.Errorf("Guards() = %v, want empty slice", got)
	}
}

func TestRegistry_RegisterAndGetAction(t *testing.T) {
	r := NewRegistry()
	meta := ActionMeta{
		Name:        "dealDamage",
		Description: "Deal damage to a target entity",
		Params: []ParamSchema{
			{Name: "amount", Type: "number", Required: true},
			{Name: "target", Type: "string", Required: false, Default: "$self"},
		},
	}
	handler := &testActionHandler{}
	r.RegisterAction(meta, handler)

	got, ok := r.GetAction("dealDamage")
	if !ok {
		t.Fatal("GetAction: expected ok=true, got false")
	}
	if got != handler {
		t.Errorf("GetAction returned wrong handler")
	}
}

func TestRegistry_GetAction_Miss(t *testing.T) {
	r := NewRegistry()
	_, ok := r.GetAction("notRegistered")
	if ok {
		t.Error("GetAction: expected ok=false for unknown name, got true")
	}
}

func TestRegistry_RegisterAndGetGuard(t *testing.T) {
	r := NewRegistry()
	meta := GuardMeta{
		Name:        "inRange",
		Description: "True when entity is within distance of target",
		Params: []ParamSchema{
			{Name: "distance", Type: "number", Required: true},
		},
	}
	handler := &testGuardHandler{result: true}
	r.RegisterGuard(meta, handler)

	got, ok := r.GetGuard("inRange")
	if !ok {
		t.Fatal("GetGuard: expected ok=true, got false")
	}
	if got != handler {
		t.Errorf("GetGuard returned wrong handler")
	}
}

func TestRegistry_GetGuard_Miss(t *testing.T) {
	r := NewRegistry()
	_, ok := r.GetGuard("notRegistered")
	if ok {
		t.Error("GetGuard: expected ok=false for unknown name, got true")
	}
}

func TestRegistry_Actions_ReturnsSortedMetas(t *testing.T) {
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "zzz"}, &testActionHandler{})
	r.RegisterAction(ActionMeta{Name: "aaa"}, &testActionHandler{})
	r.RegisterAction(ActionMeta{Name: "mmm"}, &testActionHandler{})

	metas := r.Actions()
	if len(metas) != 3 {
		t.Fatalf("Actions() len = %d, want 3", len(metas))
	}
	want := []string{"aaa", "mmm", "zzz"}
	for i, m := range metas {
		if m.Name != want[i] {
			t.Errorf("Actions()[%d].Name = %q, want %q", i, m.Name, want[i])
		}
	}
}

func TestRegistry_Guards_ReturnsSortedMetas(t *testing.T) {
	r := NewRegistry()
	r.RegisterGuard(GuardMeta{Name: "zzz"}, &testGuardHandler{})
	r.RegisterGuard(GuardMeta{Name: "aaa"}, &testGuardHandler{})

	metas := r.Guards()
	if len(metas) != 2 {
		t.Fatalf("Guards() len = %d, want 2", len(metas))
	}
	if metas[0].Name != "aaa" {
		t.Errorf("Guards()[0].Name = %q, want %q", metas[0].Name, "aaa")
	}
	if metas[1].Name != "zzz" {
		t.Errorf("Guards()[1].Name = %q, want %q", metas[1].Name, "zzz")
	}
}

func TestRegistry_DuplicateAction_Panics(t *testing.T) {
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "doThing"}, &testActionHandler{})
	defer func() {
		if rec := recover(); rec == nil {
			t.Error("expected panic on duplicate RegisterAction, got none")
		}
	}()
	r.RegisterAction(ActionMeta{Name: "doThing"}, &testActionHandler{})
}

func TestRegistry_DuplicateGuard_Panics(t *testing.T) {
	r := NewRegistry()
	r.RegisterGuard(GuardMeta{Name: "isReady"}, &testGuardHandler{})
	defer func() {
		if rec := recover(); rec == nil {
			t.Error("expected panic on duplicate RegisterGuard, got none")
		}
	}()
	r.RegisterGuard(GuardMeta{Name: "isReady"}, &testGuardHandler{})
}
