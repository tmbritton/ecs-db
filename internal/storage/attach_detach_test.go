package storage

import (
	"context"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/world"
)

// adSchema returns a schema with Goblin entity type including optional Velocity component.
func adSchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x": {Type: schema.PropertyTypeNumber},
					"y": {Type: schema.PropertyTypeNumber},
				},
			},
			"Health": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"hp": {Type: schema.PropertyTypeInteger},
				},
			},
			"Velocity": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"dx": {Type: schema.PropertyTypeNumber},
					"dy": {Type: schema.PropertyTypeNumber},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{
			"Goblin": {
				RequiredComponents:   []string{"Position", "Health"},
				OptionalComponents:   []string{"Velocity"},
				AllowExtraComponents: false,
				ValidationLevel:      schema.ValidationStrict,
			},
		},
	}
}

func TestSQLiteStore_GetEntityType_Found(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()

	e, err := createGoblin(ctx, store)
	if err != nil {
		t.Fatalf("createGoblin: %v", err)
	}

	entityType, err := store.GetEntityType(ctx, e.ID)
	if err != nil {
		t.Fatalf("GetEntityType: %v", err)
	}
	if entityType != "Goblin" {
		t.Errorf("entityType = %q, want %q", entityType, "Goblin")
	}
}

func TestSQLiteStore_GetEntityType_NotFound(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()

	_, err := store.GetEntityType(ctx, 99999)
	if err == nil {
		t.Fatal("expected error for non-existent entity")
	}
	if err != nil {
		found := false
		current := err
		for current != nil {
			if _, ok := current.(*world.EntityNotFoundError); ok {
				found = true
				break
			}
			type unwrapper interface{ Unwrap() error }
			if u, ok := current.(unwrapper); ok {
				current = u.Unwrap()
			} else {
				break
			}
		}
		if !found {
			t.Errorf("expected EntityNotFoundError in chain, got: %T: %v", err, err)
		}
	}
}

func TestSQLiteStore_HasComponent_True(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()

	e, err := createGoblin(ctx, store)
	if err != nil {
		t.Fatalf("createGoblin: %v", err)
	}

	has, err := store.HasComponent(ctx, e.ID, "Health")
	if err != nil {
		t.Fatalf("HasComponent: %v", err)
	}
	if !has {
		t.Error("expected HasComponent(true) for Health")
	}
}

func TestSQLiteStore_HasComponent_False(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()

	e, err := createGoblin(ctx, store)
	if err != nil {
		t.Fatalf("createGoblin: %v", err)
	}

	has, err := store.HasComponent(ctx, e.ID, "Velocity")
	if err != nil {
		t.Fatalf("HasComponent: %v", err)
	}
	if has {
		t.Error("expected HasComponent(false) for Velocity (not attached)")
	}
}

func TestIntegration_AttachOptionalComponent(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()
	svc := world.NewEntityService(store)
	ds := adSchema()
	svc.SetSchema(ds)

	e, err := createGoblin(ctx, store)
	if err != nil {
		t.Fatalf("createGoblin: %v", err)
	}

	err = svc.AttachComponent(ctx, e.ID, "Velocity", map[string]interface{}{"dx": 1.0, "dy": 2.0})
	if err != nil {
		t.Fatalf("AttachComponent: %v", err)
	}

	has, err := store.HasComponent(ctx, e.ID, "Velocity")
	if err != nil {
		t.Fatalf("HasComponent after attach: %v", err)
	}
	if !has {
		t.Error("expected Velocity component after attach")
	}
}

func TestIntegration_AttachUndeclaredComponent(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()
	svc := world.NewEntityService(store)
	svc.SetSchema(adSchema())

	e, err := createGoblin(ctx, store)
	if err != nil {
		t.Fatalf("createGoblin: %v", err)
	}

	// "MagicShield" is not declared in schema.
	err = svc.AttachComponent(ctx, e.ID, "MagicShield", nil)
	if err == nil {
		t.Fatal("expected error attaching undeclared component")
	}
}

func TestIntegration_AttachDuplicate(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()
	svc := world.NewEntityService(store)
	svc.SetSchema(adSchema())

	e, err := createGoblin(ctx, store)
	if err != nil {
		t.Fatalf("createGoblin: %v", err)
	}

	// First attach.
	if err := svc.AttachComponent(ctx, e.ID, "Velocity", map[string]interface{}{"dx": 1.0, "dy": 0.0}); err != nil {
		t.Fatalf("first AttachComponent: %v", err)
	}

	// Second attach — should fail because Velocity is already attached.
	err = svc.AttachComponent(ctx, e.ID, "Velocity", map[string]interface{}{"dx": 5.0, "dy": 0.0})
	if err == nil {
		t.Fatal("expected error on duplicate attach")
	}
	// Domain-level HasComponent catches duplicates before hitting the adapter.
	// The adapter's ErrAlreadyAttached path is only reached on a race condition.
	if !strings.Contains(err.Error(), "already attached") {
		t.Errorf("expected 'already attached' in error, got: %v", err)
	}
}

func TestIntegration_AttachToNonExistentEntity(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()
	svc := world.NewEntityService(store)
	svc.SetSchema(adSchema())

	err := svc.AttachComponent(ctx, 99999, "Position", nil)
	if err == nil {
		t.Fatal("expected error attaching to non-existent entity")
	}
}

func TestIntegration_DetachOptionalComponent(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()
	svc := world.NewEntityService(store)
	svc.SetSchema(adSchema())

	e, err := createGoblinWithVelocity(ctx, store)
	if err != nil {
		t.Fatalf("createGoblinWithVelocity: %v", err)
	}

	err = svc.DetachComponent(ctx, e.ID, "Velocity")
	if err != nil {
		t.Fatalf("DetachComponent: %v", err)
	}

	has, err := store.HasComponent(ctx, e.ID, "Velocity")
	if err != nil {
		t.Fatalf("HasComponent after detach: %v", err)
	}
	if has {
		t.Error("expected Velocity component to be gone after detach")
	}
}

func TestIntegration_DetachRequiredComponent(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()
	svc := world.NewEntityService(store)
	svc.SetSchema(adSchema())

	e, err := createGoblin(ctx, store)
	if err != nil {
		t.Fatalf("createGoblin: %v", err)
	}

	err = svc.DetachComponent(ctx, e.ID, "Health")
	if err == nil {
		t.Fatal("expected error detaching required Health component")
	}

	has, err := store.HasComponent(ctx, e.ID, "Health")
	if err != nil {
		t.Fatalf("HasComponent after failed detach: %v", err)
	}
	if !has {
		t.Error("Health should still be attached after failed detach")
	}
}

func TestIntegration_DetachFromNonExistentEntity(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()
	svc := world.NewEntityService(store)
	svc.SetSchema(adSchema())

	err := svc.DetachComponent(ctx, 99999, "Health")
	if err == nil {
		t.Fatal("expected error detaching from non-existent entity")
	}
}

func TestIntegration_FullAttachDetachLifecycle(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()
	svc := world.NewEntityService(store)
	svc.SetSchema(adSchema())

	e, err := createGoblin(ctx, store)
	if err != nil {
		t.Fatalf("createGoblin: %v", err)
	}

	// Attach Velocity.
	if err := svc.AttachComponent(ctx, e.ID, "Velocity", map[string]interface{}{"dx": 1.0, "dy": 0.0}); err != nil {
		t.Fatalf("AttachComponent: %v", err)
	}
	has, _ := store.HasComponent(ctx, e.ID, "Velocity")
	if !has {
		t.Error("expected Velocity row after attach")
	}

	// Detach Velocity.
	if err := svc.DetachComponent(ctx, e.ID, "Velocity"); err != nil {
		t.Fatalf("DetachComponent: %v", err)
	}
	has, _ = store.HasComponent(ctx, e.ID, "Velocity")
	if has {
		t.Error("expected Velocity row to be gone after detach")
	}
}

func TestIntegration_GetEntityType_ReturnsCorrectType(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()

	e, err := createGoblin(ctx, store)
	if err != nil {
		t.Fatalf("createGoblin: %v", err)
	}

	et, err := store.GetEntityType(ctx, e.ID)
	if err != nil {
		t.Fatalf("GetEntityType: %v", err)
	}
	if et != "Goblin" {
		t.Errorf("GetEntityType = %q, want Goblin", et)
	}
}

func TestIntegration_DetachNonAttachedComponent(t *testing.T) {
	store := makeStore(t, adSchema())
	ctx := context.Background()
	svc := world.NewEntityService(store)
	svc.SetSchema(adSchema())

	e, err := createGoblin(ctx, store)
	if err != nil {
		t.Fatalf("createGoblin: %v", err)
	}

	// Goblin exists but has no Velocity attached — detach should fail.
	err = svc.DetachComponent(ctx, e.ID, "Velocity")
	if err == nil {
		t.Fatal("expected error detaching a component that was never attached")
	}
}

// createGoblin creates a Goblin with required components only.
func createGoblin(ctx context.Context, store *SQLiteStore) (*world.Entity, error) {
	svc := world.NewEntityService(store)
	svc.SetSchema(adSchema())
	return svc.CreateEntity(ctx, "Goblin", []world.EntityComponent{
		{Name: "Position", Values: map[string]interface{}{"x": 0.0, "y": 0.0}},
		{Name: "Health", Values: map[string]interface{}{"hp": 100}},
	})
}

// createGoblinWithVelocity creates a Goblin with Position, Health, and Velocity.
func createGoblinWithVelocity(ctx context.Context, store *SQLiteStore) (*world.Entity, error) {
	svc := world.NewEntityService(store)
	svc.SetSchema(adSchema())
	return svc.CreateEntity(ctx, "Goblin", []world.EntityComponent{
		{Name: "Position", Values: map[string]interface{}{"x": 0.0, "y": 0.0}},
		{Name: "Health", Values: map[string]interface{}{"hp": 100}},
		{Name: "Velocity", Values: map[string]interface{}{"dx": 1.0, "dy": 2.0}},
	})
}
