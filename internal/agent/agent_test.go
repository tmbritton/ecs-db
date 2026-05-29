package agent

import (
	"testing"
)

// Compile-time check.
var _ MachineWriter = (*testMachineWriter)(nil)

type testMachineWriter struct {
	savedStates     []string
	savedTransition *TransitionRecord
	scheduled       []scheduledAfter
	cancelled       []cancelledAfter
}

type scheduledAfter struct {
	entityID   int64
	machineID  string
	eventType  string
	targetTick int64
}

type cancelledAfter struct {
	entityID  int64
	machineID string
	stateIDs  []string
}

func (m *testMachineWriter) SetMachineState(entityID int64, machineID string, states []string, tick int64) error {
	m.savedStates = states
	return nil
}

func (m *testMachineWriter) AppendTransition(rec TransitionRecord) error {
	m.savedTransition = &rec
	return nil
}

func (m *testMachineWriter) ScheduleAfterEvent(entityID int64, machineID, eventType string, targetTick int64) error {
	m.scheduled = append(m.scheduled, scheduledAfter{entityID, machineID, eventType, targetTick})
	return nil
}

func (m *testMachineWriter) CancelAfterEvents(entityID int64, machineID string, stateIDs []string) error {
	m.cancelled = append(m.cancelled, cancelledAfter{entityID, machineID, stateIDs})
	return nil
}

// captureWorldWriter records AttachComponent and DetachComponent calls.
type captureWorldWriter struct {
	detached []string
	attached []attachedComp
}

type attachedComp struct {
	compName string
	values   map[string]any
}

func (w *captureWorldWriter) SpawnEntity(entityType string) (int64, error) { return 1, nil }
func (w *captureWorldWriter) DetachComponent(entityID int64, compName string) error {
	w.detached = append(w.detached, compName)
	return nil
}

func (w *captureWorldWriter) AttachComponent(entityID int64, compName string, values map[string]any) error {
	w.attached = append(w.attached, attachedComp{compName, values})
	return nil
}

func (w *captureWorldWriter) SetComponentValue(entityID int64, compName, field string, value any) error {
	return nil
}

// alwaysHasComponent is a WorldReader where HasComponent always returns true.
type alwaysHasComponent struct{}

func (r *alwaysHasComponent) GetComponentValue(int64, string, string) (any, error) { return nil, nil }

func (r *alwaysHasComponent) HasComponent(int64, string) (bool, error) { return true, nil }

// actionFunc adapts a plain function to ActionHandler.
type actionFunc func(ActionContext) error

func (f actionFunc) Run(ctx ActionContext) error { return f(ctx) }

// ── Agent struct ──────────────────────────────────────────────────────────────

func TestNewAgent_Fields(t *testing.T) {
	def := &MachineDefinition{ID: "m", Initial: "a", States: map[string]*StateNode{}}
	a := NewAgent(def, 42, "StatusEffect")
	if a.Definition != def {
		t.Error("Definition not set")
	}
	if a.EntityID != 42 {
		t.Errorf("EntityID = %d, want 42", a.EntityID)
	}
	if a.ActivatedByComponent != "StatusEffect" {
		t.Errorf("ActivatedByComponent = %q, want StatusEffect", a.ActivatedByComponent)
	}
	if a.Configuration != nil {
		t.Error("Configuration should be nil before StartAgent")
	}
	if a.History == nil {
		t.Error("History map should be initialised")
	}
}

func TestNewAgent_EmptyActivatedBy(t *testing.T) {
	def := &MachineDefinition{ID: "m", Initial: "a", States: map[string]*StateNode{}}
	a := NewAgent(def, 1, "")
	if a.ActivatedByComponent != "" {
		t.Error("ActivatedByComponent should be empty for primary machine")
	}
}

// ── StartAgent ────────────────────────────────────────────────────────────────

func TestStartAgent_SeedsContextComponents(t *testing.T) {
	def := mustParse(t, `{
		"id":"m","initial":"idle",
		"context":{"x":0.0,"hp":100},
		"states":{"idle":{}}
	}`)
	def.ContextManifest = map[string]string{"x": "Position", "hp": "Health"}

	world := &captureWorldWriter{}
	a := NewAgent(def, 1, "")
	r := NewRegistry()
	if err := StartAgent(a, r, 0, world, &testWorldReader{}, &testMachineWriter{}); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}

	attached := make(map[string]bool)
	for _, att := range world.attached {
		attached[att.compName] = true
	}
	if !attached["Position"] {
		t.Error("Position not attached")
	}
	if !attached["Health"] {
		t.Error("Health not attached")
	}
}

func TestStartAgent_SkipsExistingComponents(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"idle","context":{"hp":100},"states":{"idle":{}}}`)
	def.ContextManifest = map[string]string{"hp": "Health"}

	world := &captureWorldWriter{}
	a := NewAgent(def, 1, "")
	r := NewRegistry()
	if err := StartAgent(a, r, 0, world, &alwaysHasComponent{}, &testMachineWriter{}); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}

	for _, att := range world.attached {
		if att.compName == "Health" {
			t.Error("Health should not be attached when entity already has it")
		}
	}
}

func TestStartAgent_SetsInitialConfiguration(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"idle","states":{"idle":{},"active":{}}}`)
	def.ContextManifest = map[string]string{}

	a := NewAgent(def, 1, "")
	r := NewRegistry()
	if err := StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, &testMachineWriter{}); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}

	if len(a.Configuration) != 1 {
		t.Fatalf("Configuration len = %d, want 1", len(a.Configuration))
	}
	if a.Configuration[0].ID != "m.idle" {
		t.Errorf("Configuration[0].ID = %q, want m.idle", a.Configuration[0].ID)
	}
}

func TestStartAgent_PersistsMachineState(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"idle","states":{"idle":{}}}`)
	def.ContextManifest = map[string]string{}

	mw := &testMachineWriter{}
	a := NewAgent(def, 7, "")
	r := NewRegistry()
	if err := StartAgent(a, r, 5, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if len(mw.savedStates) == 0 {
		t.Error("SetMachineState not called")
	}
}

func TestStartAgent_RunsEntryActions(t *testing.T) {
	ran := false
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "onEnter"}, actionFunc(func(ActionContext) error {
		ran = true
		return nil
	}))

	def := mustParse(t, `{"id":"m","initial":"idle","states":{"idle":{"entry":["onEnter"]}}}`)
	def.ContextManifest = map[string]string{}

	a := NewAgent(def, 1, "")
	if err := StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, &testMachineWriter{}); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if !ran {
		t.Error("entry action not called by StartAgent")
	}
}
