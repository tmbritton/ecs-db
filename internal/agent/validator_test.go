package agent

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// testSchema returns a DatabaseSchema with three object components, no field
// name collisions across components:
//
//	Position: {x, y}
//	Health:   {hp, maxHp}
//	Velocity: {speed}
func testSchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {
				Type: "object",
				Properties: map[string]schema.Property{
					"x": {Type: "number"},
					"y": {Type: "number"},
				},
			},
			"Health": {
				Type: "object",
				Properties: map[string]schema.Property{
					"hp":    {Type: "integer"},
					"maxHp": {Type: "integer"},
				},
			},
			"Velocity": {
				Type: "object",
				Properties: map[string]schema.Property{
					"speed": {Type: "number"},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
}

// ambiguousSchema returns a schema where "speed" appears in two components.
func ambiguousSchema() schema.DatabaseSchema {
	s := testSchema()
	s.Components["Movement"] = schema.Component{
		Type: "object",
		Properties: map[string]schema.Property{
			"speed": {Type: "number"},
		},
	}
	return s
}

func testRegistry() *Registry {
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "setTimer"}, &testActionHandler{})
	r.RegisterAction(ActionMeta{Name: "moveTowardTarget"}, &testActionHandler{})
	r.RegisterGuard(GuardMeta{Name: "timerExpired"}, &testGuardHandler{})
	r.RegisterGuard(GuardMeta{Name: "atTarget"}, &testGuardHandler{})
	return r
}

// -- ValidationError --

func TestValidationError_WithState(t *testing.T) {
	e := ValidationError{MachineID: "m", StateID: "m.s", Field: "f", Message: "bad"}
	got := e.Error()
	if !strings.Contains(got, "m") || !strings.Contains(got, "m.s") || !strings.Contains(got, "bad") {
		t.Errorf("Error() = %q, want machine ID, state ID, and message", got)
	}
}

func TestValidationError_MachineLevel(t *testing.T) {
	e := ValidationError{MachineID: "m", Field: "f", Message: "bad"}
	got := e.Error()
	if !strings.Contains(got, "m") || !strings.Contains(got, "bad") {
		t.Errorf("Error() = %q, want machine ID and message", got)
	}
	if strings.Contains(got, "state") {
		t.Errorf("Error() = %q, should not mention state for machine-level error", got)
	}
}

// -- ValidateMachine: clean path --

func TestValidateMachine_Clean(t *testing.T) {
	def := mustParse(t, `{
		"id":"m","initial":"a",
		"context":{"x":0,"speed":1},
		"states":{
			"a":{
				"entry":["setTimer"],
				"on":{"TICK":[{"target":"b","cond":"atTarget","actions":["moveTowardTarget"]}]}
			},
			"b":{"entry":["moveTowardTarget"]}
		}
	}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

// -- Unknown action --

func TestValidateMachine_UnknownEntryAction(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"a","states":{"a":{"entry":["unknown"]}}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "unknown" {
		t.Errorf("Field = %q, want %q", errs[0].Field, "unknown")
	}
}

func TestValidateMachine_UnknownExitAction(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"a","states":{"a":{"exit":["ghost"]}}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "ghost" {
		t.Errorf("Field = %q, want %q", errs[0].Field, "ghost")
	}
}

func TestValidateMachine_UnknownTransitionAction(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"a","states":{
		"a":{"on":{"E":[{"actions":["ghost"]}]}}
	}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "ghost" {
		t.Errorf("Field = %q, want %q", errs[0].Field, "ghost")
	}
}

// -- Unknown guard --

func TestValidateMachine_UnknownGuard(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"a","states":{
		"a":{"on":{"E":[{"target":"a","cond":"unknownGuard"}]}}
	}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "unknownGuard" {
		t.Errorf("Field = %q, want %q", errs[0].Field, "unknownGuard")
	}
}

// -- Transition target resolution --

func TestValidateMachine_UnknownOnTarget(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"a","states":{
		"a":{"on":{"E":"nowhere"}}
	}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "nowhere" {
		t.Errorf("Field = %q, want %q", errs[0].Field, "nowhere")
	}
}

func TestValidateMachine_UnknownAfterTarget(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"a","states":{
		"a":{"after":{"500":"nowhere"}}
	}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "nowhere" {
		t.Errorf("Field = %q, want %q", errs[0].Field, "nowhere")
	}
}

func TestValidateMachine_EmptyTarget_NoError(t *testing.T) {
	// Internal transition: no target, just an action — valid
	def := mustParse(t, `{"id":"m","initial":"a","states":{
		"a":{"on":{"E":[{"actions":["setTimer"]}]}}
	}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateMachine_KnownTargetByID_NoError(t *testing.T) {
	// Target may be the full ID (machineID.stateName) as well as the bare name
	def := mustParse(t, `{"id":"m","initial":"a","states":{
		"a":{"on":{"E":"m.b"}},
		"b":{}
	}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateMachine_TargetNestedChildByBareName_NoError(t *testing.T) {
	// Verifies collectStateIDs recurses into children so a nested state's bare
	// name is a valid transition target from outside the compound state.
	def := mustParse(t, `{"id":"m","initial":"outer","states":{
		"outer":{
			"type":"compound","initial":"inner",
			"states":{"inner":{}}
		},
		"other":{"on":{"E":"inner"}}
	}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

// -- History node target --

func TestValidateMachine_HistoryDefaultTarget_Unknown(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"a","states":{
		"a":{
			"type":"compound","initial":"h",
			"states":{
				"h":{"type":"history","target":"nowhere"},
				"s":{}
			}
		}
	}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "nowhere" {
		t.Errorf("Field = %q, want %q", errs[0].Field, "nowhere")
	}
}

func TestValidateMachine_HistoryDefaultTarget_Valid(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"a","states":{
		"a":{
			"type":"compound","initial":"h",
			"states":{
				"h":{"type":"history","target":"s"},
				"s":{}
			}
		}
	}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

// -- Context key validation --

func TestValidateMachine_ContextKeyNoMatch(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"a","context":{"ghost":0},"states":{"a":{}}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "ghost" {
		t.Errorf("Field = %q, want %q", errs[0].Field, "ghost")
	}
	if !strings.Contains(errs[0].Message, "does not match") {
		t.Errorf("Message = %q, want it to contain %q", errs[0].Message, "does not match")
	}
}

func TestValidateMachine_ContextKeyAmbiguous(t *testing.T) {
	// "speed" appears in both Velocity and Movement in ambiguousSchema()
	def := mustParse(t, `{"id":"m","initial":"a","context":{"speed":1},"states":{"a":{}}}`)
	errs := ValidateMachine(def, testRegistry(), ambiguousSchema())
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "speed" {
		t.Errorf("Field = %q, want %q", errs[0].Field, "speed")
	}
	if !strings.Contains(errs[0].Message, "ambiguous") {
		t.Errorf("Message = %q, want it to contain %q", errs[0].Message, "ambiguous")
	}
}

func TestValidateMachine_ContextKeyValid(t *testing.T) {
	// x → Position, hp → Health — both resolve uniquely
	def := mustParse(t, `{"id":"m","initial":"a","context":{"x":0,"hp":100},"states":{"a":{}}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

// -- Multiple errors collected --

func TestValidateMachine_MultipleErrors(t *testing.T) {
	// Combines: bad context key, bad entry action, bad transition target
	def := mustParse(t, `{"id":"m","initial":"a","context":{"ghost":0},"states":{
		"a":{
			"entry":["badAction"],
			"on":{"E":"nowhere"}
		}
	}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 3 {
		t.Fatalf("expected exactly 3 errors, got %d: %v", len(errs), errs)
	}
	fields := map[string]bool{}
	for _, e := range errs {
		fields[e.Field] = true
	}
	for _, want := range []string{"ghost", "badAction", "nowhere"} {
		if !fields[want] {
			t.Errorf("expected error for field %q, not found in %v", want, errs)
		}
	}
}

// -- Nested state validation --

func TestValidateMachine_NestedStateErrors(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"outer","states":{
		"outer":{
			"type":"compound","initial":"inner",
			"states":{
				"inner":{"entry":["unknownAction"]}
			}
		}
	}}`)
	errs := ValidateMachine(def, testRegistry(), testSchema())
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "unknownAction" {
		t.Errorf("Field = %q, want %q", errs[0].Field, "unknownAction")
	}
}

func TestValidateMachine_AfterDuration_Valid(t *testing.T) {
	s := testSchema()
	r := NewRegistry()
	for _, dur := range []string{"500", "500ms", "1s", "1.5s", "2m"} {
		raw := `{"id":"m","initial":"idle","states":{"idle":{"after":{"` + dur + `":[{"target":"idle"}]}}}}`
		def := mustParse(t, raw)
		errs := ValidateMachine(def, r, s)
		for _, e := range errs {
			if e.Field == dur {
				t.Errorf("duration %q rejected unexpectedly: %s", dur, e.Message)
			}
		}
	}
}

func TestValidateMachine_AfterDuration_Invalid(t *testing.T) {
	s := testSchema()
	r := NewRegistry()
	raw := `{"id":"m","initial":"idle","states":{"idle":{"after":{"bad":[{"target":"idle"}]}}}}`
	def := mustParse(t, raw)
	errs := ValidateMachine(def, r, s)
	found := false
	for _, e := range errs {
		if e.Field == "bad" {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for duration 'bad', got none")
	}
}
