package storage

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/world"
)

func hostileSchema(prop string) schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Probe": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				prop: {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{
			"Thing": {RequiredComponents: []string{"Probe"}, ValidationLevel: "strict"},
		},
	}
}

// The generator's guard is the one that matters for the *silent* names.
// "a-b" and "x) --" produce a SQLite syntax error whether or not anything
// checks them; "two words" produces a column called "two" of type
// "words INTEGER", which SQLite accepts and nobody notices until a write to the
// property that was declared fails with "no such column".
//
// So this is the case that tells the guard apart from its absence.
func TestComponentTableSQL_RefusesANameThatWouldSilentlyBuildTheWrongTable(t *testing.T) {
	for _, prop := range []string{"two words", `x" , "extra`} {
		t.Run(prop, func(t *testing.T) {
			comp := schema.Component{
				Type:       schema.ComponentTypeObject,
				Properties: map[string]schema.Property{prop: {Type: schema.PropertyTypeInteger}},
			}
			sql, err := componentTableSQL("Probe", comp)
			if err == nil {
				t.Fatalf("emitted DDL for a property that cannot be a column:\n%s", sql)
			}
			// %q is how the message renders it, so a name holding a quote
			// appears escaped; compare against the same rendering.
			if !strings.Contains(err.Error(), fmt.Sprintf("%q", prop)) {
				t.Errorf("error = %q, want it to name the property", err)
			}
		})
	}
}

func TestComponentTableSQL_RefusesAComponentNameThatIsNotAnIdentifier(t *testing.T) {
	comp := schema.Component{
		Type:       schema.ComponentTypeObject,
		Properties: map[string]schema.Property{"x": {Type: schema.PropertyTypeInteger}},
	}
	if sql, err := componentTableSQL("two words", comp); err == nil {
		t.Fatalf("emitted DDL for a component that cannot be a table:\n%s", sql)
	}
}

// The store as a whole, for a caller that never ran ValidateSchema: a property
// name that SQLite would accept as a column-plus-type must not build a table.
func TestNewSQLiteStore_RefusesASchemaThatWouldBuildAWrongColumn(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/w.sqlite", hostileSchema("two words"), "")
	if err == nil {
		_ = store.Close()
		t.Fatal("built a table whose column is named after the first word of a property")
	}
	if !strings.Contains(err.Error(), "two words") {
		t.Errorf("error = %q, want it to name the property", err)
	}
}

// insertObjectComponent interpolates field names into an INSERT. It cannot be
// reached with a bad one through the store any more — bootstrap refuses the
// schema first — so the guard is reached the way a future caller would reach
// it: with a transaction carrying a schema the store never validated.
func TestInsertObjectComponent_RefusesAFieldNameItCannotInterpolate(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	ctx := context.Background()
	sqlTx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = sqlTx.Rollback() }()

	// The schema the transaction believes in, rather than the one the store was
	// built from. This is the shape of the mistake the guard exists for.
	tx := &sqliteTx{tx: sqlTx, schema: hostileSchema("two words")}

	id, err := tx.InsertEntity(ctx, "Thing", 0)
	if err != nil {
		t.Fatalf("InsertEntity: %v", err)
	}
	err = tx.InsertComponent(ctx, id, "Probe", world.ComponentValues{"two words": 1})
	if err == nil {
		t.Fatal("a field name that cannot be interpolated was written into an INSERT")
	}
	if !strings.Contains(err.Error(), "cannot be a column name") {
		t.Errorf("error = %q, want it to say the name cannot be a column", err)
	}
	if !strings.Contains(err.Error(), "two words") {
		t.Errorf("error = %q, want it to name the field", err)
	}
}

// The component name is interpolated into the same INSERT the field names are,
// and it is exactly as caller-supplied. Reached the way the field-name guard is:
// with a transaction carrying a schema the store never validated.
func TestInsertComponent_RefusesAComponentNameItCannotInterpolate(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	ctx := context.Background()
	sqlTx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = sqlTx.Rollback() }()

	hostile := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"two words": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{
			"Thing": {RequiredComponents: []string{"two words"}, ValidationLevel: "strict"},
		},
	}
	tx := &sqliteTx{tx: sqlTx, schema: hostile}

	id, err := tx.InsertEntity(ctx, "Thing", 0)
	if err != nil {
		t.Fatalf("InsertEntity: %v", err)
	}
	err = tx.InsertComponent(ctx, id, "two words", world.ComponentValues{"x": 1})
	if err == nil {
		t.Fatal("a component name that cannot be interpolated was written into an INSERT")
	}
	if !strings.Contains(err.Error(), "unsafe identifier") {
		t.Errorf("error = %q, want it to say the identifier is unsafe", err)
	}
}
