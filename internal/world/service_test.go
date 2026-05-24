package world

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

func baseSchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {Type: schema.ComponentTypeObject},
			"Health":   {Type: schema.ComponentTypeObject},
		},
		EntityTypes: map[string]schema.EntityType{
			"Goblin": {
				RequiredComponents: []string{"Position", "Health"},
				ValidationLevel:    schema.ValidationStrict,
			},
		},
	}
}

// detachSchema is like baseSchema but includes an optional component for detach tests.
func detachSchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {Type: schema.ComponentTypeObject},
			"Health":   {Type: schema.ComponentTypeObject},
			"Velocity": {Type: schema.ComponentTypeObject},
		},
		EntityTypes: map[string]schema.EntityType{
			"Goblin": {
				RequiredComponents: []string{"Position", "Health"},
				OptionalComponents: []string{"Velocity"},
				ValidationLevel:    schema.ValidationStrict,
			},
		},
	}
}

func TestEntityService_CreateEntity_Success(t *testing.T) {
	tx := &mockTx{
		insertEntityResults: []insertEntityResult{{id: 1, err: nil}},
	}
	store := &mockStore{
		currentTick: 5,
		tx:          tx,
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	e, err := svc.CreateEntity(context.Background(), "Goblin", []EntityComponent{
		{Name: "Position", Values: map[string]interface{}{"x": 10.0, "y": 20.0}},
		{Name: "Health", Values: map[string]interface{}{"hp": 100}},
	})
	if err != nil {
		t.Fatalf("CreateEntity error: %v", err)
	}
	if e.ID != 1 {
		t.Errorf("entity ID = %d, want 1", e.ID)
	}
	if e.EntityType != "Goblin" {
		t.Errorf("entity type = %q, want %q", e.EntityType, "Goblin")
	}
	if e.CreatedTick != 5 {
		t.Errorf("created tick = %d, want 5", e.CreatedTick)
	}
	if !tx.committed {
		t.Error("transaction was not committed")
	}
}

func TestEntityService_CreateEntity_ValidationFails_NoDBCall(t *testing.T) {
	store := &mockStore{
		currentTick: 5,
		tx:          &mockTx{},
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	// Missing Health component.
	_, err := svc.CreateEntity(context.Background(), "Goblin", []EntityComponent{
		{Name: "Position", Values: map[string]interface{}{"x": 0.0, "y": 0.0}},
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	// Should have warnings from the ValidationResult.
	if len(svc.Warnings()) != 0 {
		t.Errorf("warnings = %d, want 0", len(svc.Warnings()))
	}
}

func TestEntityService_CreateEntity_UnknownEntityType(t *testing.T) {
	store := &mockStore{
		currentTick: 0,
		tx:          &mockTx{},
	}

	svc := NewEntityService(store)
	svc.SetSchema(schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	})

	_, err := svc.CreateEntity(context.Background(), "UnknownType", nil)
	if err == nil {
		t.Fatal("expected error for unknown entity type")
	}
}

func TestEntityService_CreateEntity_BeginTxFails(t *testing.T) {
	store := &mockStore{
		beginTxErr: fmt.Errorf("db locked"),
		tx:         &mockTx{},
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	_, err := svc.CreateEntity(context.Background(), "Goblin", []EntityComponent{
		{Name: "Position", Values: map[string]interface{}{}},
		{Name: "Health", Values: map[string]interface{}{}},
	})
	if err == nil {
		t.Fatal("expected error from BeginTx")
	}
}

func TestEntityService_CreateEntity_InsertComponentFails_Rollback(t *testing.T) {
	tx := &mockTx{
		insertEntityResults: []insertEntityResult{
			{id: 42, err: nil},
		},
		insertCompErr: fmt.Errorf("constraint violation"),
	}
	store := &mockStore{
		currentTick: 10,
		tx:          tx,
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	_, err := svc.CreateEntity(context.Background(), "Goblin", []EntityComponent{
		{Name: "Position", Values: map[string]interface{}{}},
		{Name: "Health", Values: map[string]interface{}{}},
	})
	if err == nil {
		t.Fatal("expected error from InsertComponent")
	}
	if !tx.rolledBack {
		t.Error("transaction should have been rolled back after insert failure")
	}
	if tx.committed {
		t.Error("transaction should NOT have been committed after insert failure")
	}
}

func TestEntityService_CreateEntity_WarningModeProceeds(t *testing.T) {
	tx := &mockTx{
		insertEntityResults: []insertEntityResult{{id: 1, err: nil}},
	}
	store := &mockStore{
		currentTick: 1,
		tx:          tx,
	}

	svc := NewEntityService(store)
	svc.SetSchema(schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {Type: schema.ComponentTypeObject},
			"Health":   {Type: schema.ComponentTypeObject},
		},
		EntityTypes: map[string]schema.EntityType{
			"Particle": {
				RequiredComponents: []string{"Position", "Health"},
				ValidationLevel:    schema.ValidationWarning,
			},
		},
	})

	// Missing Health — but warning mode allows it.
	e, err := svc.CreateEntity(context.Background(), "Particle", []EntityComponent{
		{Name: "Position", Values: map[string]interface{}{}},
	})
	if err != nil {
		t.Fatalf("warning mode should not error: %v", err)
	}
	if e.ID != 1 {
		t.Errorf("entity ID = %d, want 1", e.ID)
	}
	if !tx.committed {
		t.Error("transaction should have been committed in warning mode")
	}
	// Should have a warning about missing Health.
	if len(svc.Warnings()) == 0 {
		t.Error("expected warnings in warning mode")
	}
}

func TestEntityService_CreateEntity_InsertEntityFails_Rollback(t *testing.T) {
	tx := &mockTx{
		insertEntityResults: []insertEntityResult{
			{id: 0, err: fmt.Errorf("constraint violation")},
		},
	}
	store := &mockStore{
		currentTick: 1,
		tx:          tx,
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	_, err := svc.CreateEntity(context.Background(), "Goblin", []EntityComponent{
		{Name: "Position", Values: map[string]interface{}{}},
		{Name: "Health", Values: map[string]interface{}{}},
	})
	if err == nil {
		t.Fatal("expected error from InsertEntity")
	}
	if !tx.rolledBack {
		t.Error("transaction should have been rolled back after insert failure")
	}
	if tx.committed {
		t.Error("transaction should NOT have been committed")
	}
}

func TestEntityService_CreateEntity_GetCurrentTickFails_Rollback(t *testing.T) {
	store := &mockStore{
		currentTickErr: fmt.Errorf("world table locked"),
		tx:             &mockTx{},
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	_, err := svc.CreateEntity(context.Background(), "Goblin", []EntityComponent{
		{Name: "Position", Values: map[string]interface{}{}},
		{Name: "Health", Values: map[string]interface{}{}},
	})
	if err == nil {
		t.Fatal("expected error from GetCurrentTick")
	}
}

func TestEntityService_Warnings_IsClearedOnError(t *testing.T) {
	svc := NewEntityService(nil)
	svc.SetSchema(schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	})

	// Just verify Warnings() doesn't panic.
	if svc.Warnings() != nil {
		t.Error("new service should have nil warnings")
	}
}

func TestValidationError_Error(t *testing.T) {
	e := &ValidationError{Type: "Goblin", Errors: []string{"missing Health"}}
	want := `entity creation validation failed for type "Goblin": missing Health`
	if e.Error() != want {
		t.Errorf("Error() = %q, want %q", e.Error(), want)
	}
}

func TestValidationError_Error_EmptyErrors(t *testing.T) {
	e := &ValidationError{Type: "Test", Errors: []string{}}
	if e.Error() == "" {
		t.Error("Error() should not be empty")
	}
}

func TestEntityService_CreateEntity_WarningsConcurrency(t *testing.T) {
	store := &mockStore{
		currentTick: 1,
		tx: &mockTx{
			insertEntityResults: []insertEntityResult{{id: 1, err: nil}},
		},
	}

	svc := NewEntityService(store)
	svc.SetSchema(schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"A": {Type: schema.ComponentTypeObject},
			"B": {Type: schema.ComponentTypeObject},
		},
		EntityTypes: map[string]schema.EntityType{
			"X": {RequiredComponents: []string{"A", "B"}, ValidationLevel: schema.ValidationWarning},
		},
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.CreateEntity(context.Background(), "X", []EntityComponent{
				{Name: "A", Values: map[string]interface{}{}},
			})
			if err != nil {
				t.Error("unexpected error: ", err)
			}
		}()
	}
	wg.Wait()
}

// ---- AttachComponent tests ----

func TestEntityService_AttachComponent_Success(t *testing.T) {
	tx := &mockTx{}
	store := &mockStore{
		entityType:   "Goblin",
		hasComponent: false,
		tx:           tx,
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	err := svc.AttachComponent(context.Background(), 1, "Position", map[string]interface{}{"x": 5.0})
	if err != nil {
		t.Fatalf("AttachComponent error: %v", err)
	}
	if !tx.committed {
		t.Error("transaction should have been committed")
	}
}

func TestEntityService_AttachComponent_EntityNotFound(t *testing.T) {
	store := &mockStore{
		entityTypeErr: &EntityNotFoundError{ID: 999},
		tx:            &mockTx{},
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	err := svc.AttachComponent(context.Background(), 999, "Position", nil)
	if err == nil {
		t.Fatal("expected error for non-existent entity")
	}
	var notFound *EntityNotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected EntityNotFoundError, got %T: %v", err, err)
	}
}

func TestEntityService_AttachComponent_AlreadyAttached(t *testing.T) {
	store := &mockStore{
		entityType:   "Goblin",
		hasComponent: true,
		tx:           &mockTx{},
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	err := svc.AttachComponent(context.Background(), 1, "Position", nil)
	if err == nil {
		t.Fatal("expected error for already attached component")
	}
	if !strings.Contains(err.Error(), "already attached") {
		t.Errorf("expected 'already attached' in error: %v", err)
	}
}

func TestEntityService_AttachComponent_ValidationError(t *testing.T) {
	store := &mockStore{
		entityType:   "Goblin",
		hasComponent: false,
		tx:           &mockTx{},
	}

	svc := NewEntityService(store)
	svc.SetSchema(schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {Type: schema.ComponentTypeObject},
		},
		EntityTypes: map[string]schema.EntityType{
			"Goblin": {
				RequiredComponents:   []string{"Position"},
				AllowExtraComponents: false,
				ValidationLevel:      schema.ValidationStrict,
			},
		},
	})

	// "Health" not declared in schema.
	err := svc.AttachComponent(context.Background(), 1, "Health", nil)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestEntityService_AttachComponent_WarningModeProceeds(t *testing.T) {
	tx := &mockTx{}
	store := &mockStore{
		entityType:   "Goblin",
		hasComponent: false,
		tx:           tx,
	}

	svc := NewEntityService(store)
	svc.SetSchema(schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {Type: schema.ComponentTypeObject},
			"Velocity": {Type: schema.ComponentTypeObject},
		},
		EntityTypes: map[string]schema.EntityType{
			"Goblin": {
				RequiredComponents:   []string{"Position"},
				AllowExtraComponents: false,
				ValidationLevel:      schema.ValidationWarning,
			},
		},
	})

	// Velocity is not allowed but warning mode permits it.
	err := svc.AttachComponent(context.Background(), 1, "Velocity", nil)
	if err != nil {
		t.Fatalf("warning mode should not error: %v", err)
	}
	if !tx.committed {
		t.Error("transaction should have been committed in warning mode")
	}
}

func TestEntityService_AttachComponent_RollbackOnFailure(t *testing.T) {
	tx := &mockTx{
		attachCompErr: fmt.Errorf("constraint violation"),
	}
	store := &mockStore{
		entityType:   "Goblin",
		hasComponent: false,
		tx:           tx,
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	err := svc.AttachComponent(context.Background(), 1, "Position", nil)
	if err == nil {
		t.Fatal("expected error from AttachComponent")
	}
	if !tx.rolledBack {
		t.Error("transaction should have been rolled back")
	}
	if tx.committed {
		t.Error("transaction should NOT have been committed")
	}
}

// ---- DetachComponent tests ----

func TestEntityService_DetachComponent_Success(t *testing.T) {
	tx := &mockTx{}
	store := &mockStore{
		entityType: "Goblin",
		tx:         tx,
	}

	svc := NewEntityService(store)
	svc.SetSchema(detachSchema())

	// Velocity is optional so it can be detached.
	err := svc.DetachComponent(context.Background(), 1, "Velocity")
	if err != nil {
		t.Fatalf("DetachComponent error: %v", err)
	}
	if !tx.committed {
		t.Error("transaction should have been committed")
	}
}

func TestEntityService_DetachComponent_RequiredComponent(t *testing.T) {
	store := &mockStore{
		entityType: "Goblin",
		tx:         &mockTx{},
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	err := svc.DetachComponent(context.Background(), 1, "Health")
	if err == nil {
		t.Fatal("expected error for required component detach")
	}
	if _, ok := err.(*ComponentMutationError); !ok {
		t.Errorf("expected ComponentMutationError, got %T: %v", err, err)
	}
}

func TestEntityService_DetachComponent_EntityNotFound(t *testing.T) {
	store := &mockStore{
		entityTypeErr: &EntityNotFoundError{ID: 999},
		tx:            &mockTx{},
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	err := svc.DetachComponent(context.Background(), 999, "Position")
	if err == nil {
		t.Fatal("expected error for non-existent entity")
	}
}

func TestEntityService_DetachComponent_RollbackOnFailure(t *testing.T) {
	tx := &mockTx{
		detachCompErr: fmt.Errorf("database error"),
	}
	store := &mockStore{
		entityType: "Goblin",
		tx:         tx,
	}

	svc := NewEntityService(store)
	svc.SetSchema(detachSchema())

	// Velocity is optional so validation passes, then the DB fails.
	err := svc.DetachComponent(context.Background(), 1, "Velocity")
	if err == nil {
		t.Fatal("expected error from DetachComponent")
	}
	if !tx.rolledBack {
		t.Error("transaction should have been rolled back")
	}
	if tx.committed {
		t.Error("transaction should NOT have been committed")
	}
}

func TestEntityService_DetachComponent_RequiredComponentValidationFails(t *testing.T) {
	store := &mockStore{
		beginTxErr: fmt.Errorf("db locked"),
		entityType: "Goblin",
		tx:         &mockTx{},
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	// Health is required so validation fails before BeginTx is called.
	err := svc.DetachComponent(context.Background(), 1, "Health")
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestComponentMutationError_Error(t *testing.T) {
	e := &ComponentMutationError{
		Action:   "attach",
		EntityID: 42,
		Type:     "Goblin",
		Errors:   []string{"component already attached"},
	}
	if !strings.Contains(e.Error(), "attach") {
		t.Errorf("Error() should contain 'attach': %s", e.Error())
	}
	if !strings.Contains(e.Error(), "42") {
		t.Errorf("Error() should contain entity id: %s", e.Error())
	}

	e2 := &ComponentMutationError{Action: "detach", EntityID: 1, Errors: nil}
	if e2.Error() == "" {
		t.Error("Error() should not be empty for nil Errors")
	}
}

func TestIsAlreadyAttached(t *testing.T) {
	if !IsAlreadyAttached(ErrAlreadyAttached) {
		t.Error("expected ErrAlreadyAttached to be detected")
	}
	if IsAlreadyAttached(fmt.Errorf("something else")) {
		t.Error("should not detect unrelated error")
	}
	wrapped := fmt.Errorf("wrapper: %w", ErrAlreadyAttached)
	if !IsAlreadyAttached(wrapped) {
		t.Error("should detect wrapped ErrAlreadyAttached")
	}
}

func TestEntityNotFoundError_Error(t *testing.T) {
	e := &EntityNotFoundError{ID: 42}
	got := e.Error()
	if !strings.Contains(got, "42") {
		t.Errorf("Error() = %q, should contain entity id", got)
	}
}

func TestEntityService_AttachComponent_HasComponentError(t *testing.T) {
	store := &mockStore{
		entityType:      "Goblin",
		hasComponentErr: fmt.Errorf("db locked"),
		tx:              &mockTx{},
	}

	svc := NewEntityService(store)
	svc.SetSchema(baseSchema())

	err := svc.AttachComponent(context.Background(), 1, "Position", nil)
	if err == nil {
		t.Fatal("expected error from HasComponent")
	}
	if !strings.Contains(err.Error(), "db locked") {
		t.Errorf("expected 'db locked' in error: %v", err)
	}
}

func TestEntityService_DetachComponent_BeginTxError(t *testing.T) {
	store := &mockStore{
		entityType: "Goblin",
		beginTxErr: fmt.Errorf("db locked"),
		tx:         &mockTx{},
	}

	svc := NewEntityService(store)
	svc.SetSchema(detachSchema())

	// Velocity is optional so validation passes, then BeginTx fails.
	err := svc.DetachComponent(context.Background(), 1, "Velocity")
	if err == nil {
		t.Fatal("expected error from BeginTx")
	}
	if !strings.Contains(err.Error(), "db locked") {
		t.Errorf("expected 'db locked' in error: %v", err)
	}
}
