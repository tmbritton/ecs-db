package agent

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// initialSchema is deliberately minimal: these cases are about state structure,
// not context, so nothing here should trip the context-key checks.
func initialSchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {Type: "object", Properties: map[string]schema.Property{"x": {Type: "number"}}},
		},
		EntityTypes: map[string]schema.EntityType{
			"Thing": {RequiredComponents: []string{"Position"}, ValidationLevel: schema.ValidationStrict},
		},
	}
}

// A machine whose `initial` names a state that does not exist parses cleanly,
// passes every other check, and then fails at runtime. That is the worst shape
// a validation gap can take, and it is reachable through ordinary editing:
// deleting a state on Epic 13's canvas is one click.
func TestValidateMachine_Initial(t *testing.T) {
	tests := []struct {
		name    string
		machine string
		wantErr string // substring; "" means the machine must validate
	}{
		{
			name:    "machine initial names an existing top-level state",
			machine: `{"id":"m","initial":"idle","states":{"idle":{},"busy":{}}}`,
		},
		{
			name:    "machine initial names a state that does not exist",
			machine: `{"id":"m","initial":"nowhere","states":{"idle":{}}}`,
			wantErr: `initial state "nowhere"`,
		},
		{
			// Scoping matters: `initial` selects a child, not any state in the
			// tree. Validating against the machine-wide set of known states
			// would wave this through.
			name: "machine initial names a nested state rather than a top-level one",
			machine: `{"id":"m","initial":"inner","states":{
				"outer":{"initial":"inner","states":{"inner":{}}}
			}}`,
			wantErr: `initial state "inner"`,
		},
		{
			name: "compound state initial names one of its children",
			machine: `{"id":"m","initial":"outer","states":{
				"outer":{"initial":"inner","states":{"inner":{},"other":{}}}
			}}`,
		},
		{
			name: "compound state initial names a child that does not exist",
			machine: `{"id":"m","initial":"outer","states":{
				"outer":{"initial":"nowhere","states":{"inner":{}}}
			}}`,
			wantErr: `initial state "nowhere"`,
		},
		{
			// Same scoping point, one level down.
			name: "compound state initial names a state from a different branch",
			machine: `{"id":"m","initial":"a","states":{
				"a":{"initial":"a1","states":{"a1":{}}},
				"b":{"initial":"a1","states":{"b1":{}}}
			}}`,
			wantErr: `initial state "a1"`,
		},
		{
			name:    "an atomic state needs no initial",
			machine: `{"id":"m","initial":"idle","states":{"idle":{}}}`,
		},
		{
			// A compound state with no initial has no defined entry point.
			// XState requires one; the engine would pick nothing.
			name: "compound state with children but no initial",
			machine: `{"id":"m","initial":"outer","states":{
				"outer":{"states":{"inner":{}}}
			}}`,
			wantErr: "has child states but no initial",
		},
		{
			// Parallel regions are all entered at once, so an initial would be
			// meaningless rather than missing.
			name: "parallel state needs no initial",
			machine: `{"id":"m","initial":"par","states":{
				"par":{"type":"parallel","states":{
					"a":{"initial":"a1","states":{"a1":{}}},
					"b":{"initial":"b1","states":{"b1":{}}}
				}}
			}}`,
		},
		{
			// The codebase treats bare keys and dotted ids interchangeably for
			// transition targets, so accept both here rather than making
			// `initial` the one place a dotted id is rejected.
			name:    "initial given as a fully qualified state id",
			machine: `{"id":"m","initial":"m.idle","states":{"idle":{}}}`,
		},
		{
			name: "final state needs no initial",
			machine: `{"id":"m","initial":"idle","states":{
				"idle":{"on":{"GO":"done"}},
				"done":{"type":"final"}
			}}`,
		},
	}

	registry := NewRegistry()
	s := initialSchema()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def, err := ParseMachine([]byte(tt.machine))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			errs := ValidateMachine(def, registry, s)

			if tt.wantErr == "" {
				if len(errs) != 0 {
					t.Fatalf("want no errors, got %v", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatalf("want an error containing %q, got none", tt.wantErr)
			}
			var found bool
			for _, e := range errs {
				if strings.Contains(e.Error(), tt.wantErr) {
					found = true
				}
			}
			if !found {
				t.Errorf("want an error containing %q, got %v", tt.wantErr, errs)
			}
		})
	}
}

// Every error is reported, not just the first — someone fixing a machine should
// see all of what is wrong with it in one pass.
func TestValidateMachine_ReportsEveryBadInitial(t *testing.T) {
	def, err := ParseMachine([]byte(`{"id":"m","initial":"nowhere","states":{
		"a":{"initial":"missingA","states":{"a1":{}}},
		"b":{"initial":"missingB","states":{"b1":{}}}
	}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	errs := ValidateMachine(def, NewRegistry(), initialSchema())

	for _, want := range []string{"nowhere", "missingA", "missingB"} {
		var found bool
		for _, e := range errs {
			if strings.Contains(e.Error(), want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no error mentions %q; got %v", want, errs)
		}
	}
}

// The real machines in this repo must keep validating. A rule that rejects
// working files is worse than the gap it closes.
func TestValidateMachine_RealMachinesStillValidate(t *testing.T) {
	for _, path := range []string{
		"../../behaviors/goblin.json",
		"../../e2e/fixtures/project/behaviors/e2e-wander.json",
	} {
		t.Run(path, func(t *testing.T) {
			raw, err := readFileForTest(path)
			if err != nil {
				t.Skipf("not present: %v", err)
			}
			def, err := ParseMachine(raw)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			// The real registry, not the empty one — these use real actions.
			for _, e := range ValidateMachine(def, realRegistryForTest(), realSchemaForTest(t)) {
				if strings.Contains(e.Error(), "initial") {
					t.Errorf("%s no longer validates: %v", path, e)
				}
			}
		})
	}
}

// An editor needs every failure at once, and a validation hook takes one error.
// errors.Join carries the list across that boundary.
func TestValidateMachineError_JoinsEveryFailure(t *testing.T) {
	def, err := ParseMachine([]byte(`{"id":"m","initial":"nowhere","states":{
		"a":{"entry":"noSuchAction","on":{"GO":{"target":"a","cond":"noSuchGuard"}}}
	}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	joined := ValidateMachineError(def, NewRegistry(), initialSchema())
	if joined == nil {
		t.Fatal("want an error")
	}

	unwrapped, ok := joined.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("error does not unwrap to a list; a caller cannot show them separately: %v", joined)
	}
	parts := unwrapped.Unwrap()
	if len(parts) < 3 {
		t.Errorf("got %d errors, want every failure: %v", len(parts), parts)
	}

	for _, want := range []string{"noSuchAction", "noSuchGuard", "nowhere"} {
		var found bool
		for _, p := range parts {
			if strings.Contains(p.Error(), want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no error mentions %q; got %v", want, parts)
		}
	}
}

func TestValidateMachineError_NilWhenValid(t *testing.T) {
	def, err := ParseMachine([]byte(`{"id":"m","initial":"idle","states":{"idle":{}}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := ValidateMachineError(def, NewRegistry(), initialSchema()); got != nil {
		t.Errorf("ValidateMachineError = %v, want nil for a valid machine", got)
	}
}
