package agent

import (
	"testing"
)

// callCountingAction counts invocations.
type callCountingAction struct{ count int }

func (a *callCountingAction) Run(ActionContext) error { a.count++; return nil }

// recordingGuard returns a fixed result and records calls.
type recordingGuard struct {
	result bool
	calls  int
}

func (g *recordingGuard) Evaluate(GuardContext) bool { g.calls++; return g.result }

// interpreterRegistry has a standard set of test handlers.
func interpreterRegistry() *Registry {
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "onEnter"}, &callCountingAction{})
	r.RegisterAction(ActionMeta{Name: "onExit"}, &callCountingAction{})
	r.RegisterAction(ActionMeta{Name: "doWork"}, &callCountingAction{})
	r.RegisterGuard(GuardMeta{Name: "alwaysTrue"}, &recordingGuard{result: true})
	r.RegisterGuard(GuardMeta{Name: "alwaysFalse"}, &recordingGuard{result: false})
	return r
}

// startedAgent parses a machine JSON, calls StartAgent, and returns all handles.
func startedAgent(t *testing.T, json string, entityID int64) (*Agent, *Registry, *captureWorldWriter, *testMachineWriter) {
	t.Helper()
	def := mustParse(t, json)
	def.ContextManifest = map[string]string{}
	r := interpreterRegistry()
	world := &captureWorldWriter{}
	mw := &testMachineWriter{}
	a := NewAgent(def, entityID, "")
	if err := StartAgent(a, r, 0, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	return a, r, world, mw
}

// send is a helper that calls SendEvent and fails the test on error.
func send(t *testing.T, a *Agent, ev string, r *Registry, world WorldWriter, mw *testMachineWriter) {
	t.Helper()
	if err := SendEvent(a, Event{Type: ev}, 1, r, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent(%q): %v", ev, err)
	}
}

// ── Flat machine basics ───────────────────────────────────────────────────────

func TestSendEvent_UnconditionalTransition(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"GO":"b"}},"b":{}}
	}`, 1)
	mw.savedStates = nil
	send(t, a, "GO", r, world, mw)

	if len(a.Configuration) != 1 || a.Configuration[0].ID != "m.b" {
		t.Errorf("config = %v, want [m.b]", nodeIDs(a.Configuration))
	}
	if len(mw.savedStates) == 0 || mw.savedStates[0] != "m.b" {
		t.Errorf("savedStates = %v, want [m.b]", mw.savedStates)
	}
}

func TestSendEvent_UnknownEvent_NoTransition(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"GO":"b"}},"b":{}}
	}`, 1)
	mw.savedStates = nil
	send(t, a, "NOPE", r, world, mw)

	if a.Configuration[0].ID != "m.a" {
		t.Errorf("config = %v, want [m.a]", nodeIDs(a.Configuration))
	}
	if mw.savedStates != nil {
		t.Error("SetMachineState should not be called when no transition fires")
	}
}

func TestSendEvent_GuardTrue_Transitions(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"E":[{"target":"b","cond":"alwaysTrue"}]}},"b":{}}
	}`, 1)
	send(t, a, "E", r, world, mw)
	if a.Configuration[0].ID != "m.b" {
		t.Errorf("config = %v, want [m.b]", nodeIDs(a.Configuration))
	}
}

func TestSendEvent_GuardFalse_NoTransition(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"E":[{"target":"b","cond":"alwaysFalse"}]}},"b":{}}
	}`, 1)
	send(t, a, "E", r, world, mw)
	if a.Configuration[0].ID != "m.a" {
		t.Errorf("config = %v, want [m.a]", nodeIDs(a.Configuration))
	}
}

func TestSendEvent_EntryExitActionsOrder(t *testing.T) {
	order := []string{}
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "exitA"}, actionFunc(func(ActionContext) error {
		order = append(order, "exitA")
		return nil
	}))
	r.RegisterAction(ActionMeta{Name: "enterB"}, actionFunc(func(ActionContext) error {
		order = append(order, "enterB")
		return nil
	}))

	def := mustParse(t, `{
		"id":"m","initial":"a",
		"states":{
			"a":{"exit":["exitA"],"on":{"GO":"b"}},
			"b":{"entry":["enterB"]}
		}
	}`)
	def.ContextManifest = map[string]string{}
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)

	if err := SendEvent(a, Event{Type: "GO"}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if len(order) != 2 || order[0] != "exitA" || order[1] != "enterB" {
		t.Errorf("order = %v, want [exitA, enterB]", order)
	}
}

func TestSendEvent_TransitionActions(t *testing.T) {
	ran := 0
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "doWork"}, actionFunc(func(ActionContext) error { ran++; return nil }))

	def := mustParse(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"E":[{"actions":["doWork"]}]}}}
	}`)
	def.ContextManifest = map[string]string{}
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)

	if err := SendEvent(a, Event{Type: "E"}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if ran != 1 {
		t.Errorf("doWork ran %d times, want 1", ran)
	}
}

func TestSendEvent_SelfTransition(t *testing.T) {
	exitCount, enterCount := 0, 0
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "onExit"}, actionFunc(func(ActionContext) error { exitCount++; return nil }))
	r.RegisterAction(ActionMeta{Name: "onEnter"}, actionFunc(func(ActionContext) error { enterCount++; return nil }))

	def := mustParse(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"entry":["onEnter"],"exit":["onExit"],"on":{"LOOP":"a"}}}
	}`)
	def.ContextManifest = map[string]string{}
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)
	enterCount = 0 // reset; StartAgent fires entry once

	if err := SendEvent(a, Event{Type: "LOOP"}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if exitCount != 1 || enterCount != 1 {
		t.Errorf("self-transition: exit=%d enter=%d, want 1 each", exitCount, enterCount)
	}
}

// ── Persistence ───────────────────────────────────────────────────────────────

func TestSendEvent_AppendTransition_Recorded(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{"id":"m","initial":"a","states":{"a":{"on":{"GO":"b"}},"b":{}}}`, 1)
	mw.savedTransition = nil
	if err := SendEvent(a, Event{Type: "GO"}, 5, r, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	rec := mw.savedTransition
	if rec == nil {
		t.Fatal("AppendTransition not called")
	}
	if rec.Tick != 5 {
		t.Errorf("Tick = %d, want 5", rec.Tick)
	}
	if rec.Event != "GO" {
		t.Errorf("Event = %q, want GO", rec.Event)
	}
	if len(rec.FromStates) == 0 || rec.FromStates[0] != "m.a" {
		t.Errorf("FromStates = %v, want [m.a]", rec.FromStates)
	}
	if len(rec.ToStates) == 0 || rec.ToStates[0] != "m.b" {
		t.Errorf("ToStates = %v, want [m.b]", rec.ToStates)
	}
	if rec.CondResult != nil {
		t.Error("unconditional transition: CondResult should be nil")
	}
}

func TestSendEvent_CondResult_GuardTrue(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"E":[{"target":"b","cond":"alwaysTrue"}]}},"b":{}}
	}`, 1)
	mw.savedTransition = nil
	_ = SendEvent(a, Event{Type: "E"}, 1, r, world, &testWorldReader{}, mw)
	if mw.savedTransition == nil || mw.savedTransition.CondResult == nil || !*mw.savedTransition.CondResult {
		t.Errorf("CondResult should be &true")
	}
}

// ── Compound state ────────────────────────────────────────────────────────────

func TestSendEvent_CompoundInitialResolution(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"outer",
		"states":{
			"outer":{"type":"compound","initial":"inner","states":{"inner":{"on":{"GO":"done"}}}},
			"done":{}
		}
	}`, 1)

	if len(a.Configuration) != 1 || a.Configuration[0].ID != "m.inner" {
		t.Fatalf("initial config = %v, want [m.inner]", nodeIDs(a.Configuration))
	}
	send(t, a, "GO", r, world, mw)
	if a.Configuration[0].ID != "m.done" {
		t.Errorf("after GO: config = %v, want [m.done]", nodeIDs(a.Configuration))
	}
}

// ── Depth preemption ──────────────────────────────────────────────────────────

func TestSendEvent_DepthPreemption(t *testing.T) {
	outerRan, innerRan := false, false
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "outerAction"}, actionFunc(func(ActionContext) error { outerRan = true; return nil }))
	r.RegisterAction(ActionMeta{Name: "innerAction"}, actionFunc(func(ActionContext) error { innerRan = true; return nil }))

	def := mustParse(t, `{
		"id":"m","initial":"outer",
		"states":{
			"outer":{
				"type":"compound","initial":"inner",
				"on":{"E":[{"actions":["outerAction"]}]},
				"states":{"inner":{"on":{"E":[{"actions":["innerAction"]}]}}}
			}
		}
	}`)
	def.ContextManifest = map[string]string{}
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)

	if err := SendEvent(a, Event{Type: "E"}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if !innerRan {
		t.Error("inner action should have run")
	}
	if outerRan {
		t.Error("outer action should be preempted by inner")
	}
}

// ── Parallel regions ──────────────────────────────────────────────────────────

func TestSendEvent_ParallelRegions_BothTransition(t *testing.T) {
	leftRan, rightRan := 0, 0
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "leftAction"}, actionFunc(func(ActionContext) error { leftRan++; return nil }))
	r.RegisterAction(ActionMeta{Name: "rightAction"}, actionFunc(func(ActionContext) error { rightRan++; return nil }))

	def := mustParse(t, `{
		"id":"m","initial":"p",
		"states":{
			"p":{
				"type":"parallel",
				"states":{
					"left":{"on":{"E":[{"actions":["leftAction"]}]}},
					"right":{"on":{"E":[{"actions":["rightAction"]}]}}
				}
			}
		}
	}`)
	def.ContextManifest = map[string]string{}
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)

	if len(a.Configuration) != 2 {
		t.Fatalf("initial config len = %d, want 2", len(a.Configuration))
	}
	if err := SendEvent(a, Event{Type: "E"}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if leftRan != 1 {
		t.Errorf("leftAction ran %d times, want 1", leftRan)
	}
	if rightRan != 1 {
		t.Errorf("rightAction ran %d times, want 1", rightRan)
	}
}

// ── History ───────────────────────────────────────────────────────────────────

func TestSendEvent_HistoryShallow_RestoresRecordedState(t *testing.T) {
	def := mustParse(t, `{
		"id":"m","initial":"c",
		"states":{
			"c":{
				"type":"compound","initial":"s1",
				"on":{"BACK":"c.h"},
				"states":{
					"h":{"type":"history"},
					"s1":{"on":{"NEXT":"s2"}},
					"s2":{"on":{"OUT":"done"}}
				}
			},
			"done":{"on":{"BACK":"c.h"}}
		}
	}`)
	def.ContextManifest = map[string]string{}
	r := interpreterRegistry()
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)

	doSend := func(ev string) {
		t.Helper()
		if err := SendEvent(a, Event{Type: ev}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
			t.Fatalf("SendEvent(%q): %v", ev, err)
		}
	}

	doSend("NEXT") // c.s1 → c.s2
	doSend("OUT")  // c.s2 → done; records s2 in history
	doSend("BACK") // done → c.h → restores c.s2

	if len(a.Configuration) == 0 || a.Configuration[0].ID != "m.s2" {
		t.Errorf("history restore: config = %v, want [m.s2]", nodeIDs(a.Configuration))
	}
}

func TestSendEvent_HistoryShallow_DefaultTargetWhenNoHistory(t *testing.T) {
	def := mustParse(t, `{
		"id":"m","initial":"done",
		"states":{
			"c":{
				"type":"compound","initial":"s1",
				"states":{
					"h":{"type":"history","target":"s1"},
					"s1":{}
				}
			},
			"done":{"on":{"BACK":"c.h"}}
		}
	}`)
	def.ContextManifest = map[string]string{}
	r := interpreterRegistry()
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)

	if err := SendEvent(a, Event{Type: "BACK"}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if len(a.Configuration) == 0 || a.Configuration[0].ID != "m.s1" {
		t.Errorf("default history: config = %v, want [m.s1]", nodeIDs(a.Configuration))
	}
}

// ── Final state lifecycle ─────────────────────────────────────────────────────

func TestSendEvent_FinalState_DetachesActivatingComponent(t *testing.T) {
	def := mustParse(t, `{
		"id":"m","initial":"active",
		"states":{"active":{"on":{"DONE":"finished"}},"finished":{"type":"final"}}
	}`)
	def.ContextManifest = map[string]string{}
	r := interpreterRegistry()
	world := &captureWorldWriter{}
	a := NewAgent(def, 1, "StatusBuff")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, world, &testWorldReader{}, mw)

	if err := SendEvent(a, Event{Type: "DONE"}, 1, r, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if len(world.detached) == 0 || world.detached[0] != "StatusBuff" {
		t.Errorf("detached = %v, want [StatusBuff]", world.detached)
	}
}

func TestSendEvent_FinalState_NoPrimaryDetach(t *testing.T) {
	def := mustParse(t, `{
		"id":"m","initial":"active",
		"states":{"active":{"on":{"DONE":"finished"}},"finished":{"type":"final"}}
	}`)
	def.ContextManifest = map[string]string{}
	r := interpreterRegistry()
	world := &captureWorldWriter{}
	a := NewAgent(def, 1, "") // primary machine — no component to detach
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, world, &testWorldReader{}, mw)

	if err := SendEvent(a, Event{Type: "DONE"}, 1, r, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if len(world.detached) != 0 {
		t.Errorf("primary machine should not detach, got %v", world.detached)
	}
}

// ── After scheduling / cancellation ──────────────────────────────────────────

func TestSendEvent_AfterEntry_Scheduled(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"GO":"b"}},"b":{"after":{"500":"a"}}}
	}`, 1)
	mw.scheduled = nil
	if err := SendEvent(a, Event{Type: "GO"}, 10, r, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if len(mw.scheduled) == 0 {
		t.Error("ScheduleAfterEvent not called on entry to b")
	}
	// "500" means 500 ms; at 50 ms/tick that is 10 ticks. tick=10 → targetTick=20.
	if mw.scheduled[0].targetTick != 20 {
		t.Errorf("targetTick = %d, want 20 (10+10)", mw.scheduled[0].targetTick)
	}
}

func TestSendEvent_AfterExit_Cancelled(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"after":{"200":"b"},"on":{"GO":"b"}},"b":{}}
	}`, 1)
	mw.cancelled = nil
	if err := SendEvent(a, Event{Type: "GO"}, 1, r, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if len(mw.cancelled) == 0 {
		t.Error("CancelAfterEvents not called on exit from a")
	}
}
