package storage

import (
	"context"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/world"
)

// everyKindSchema declares one component of each kind the insert path supports,
// so the update path can be held to the same coverage.
func everyKindSchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {
				Type: schema.ComponentTypeObject,
				Properties: map[string]schema.Property{
					"x":     {Type: schema.PropertyTypeNumber},
					"y":     {Type: schema.PropertyTypeNumber},
					"label": {Type: schema.PropertyTypeString},
				},
			},
			"Hp":      {Type: schema.ComponentTypeInteger},
			"Tags":    {Type: schema.ComponentTypeArray, Items: &schema.Property{Type: schema.PropertyTypeString}},
			"Carrier": {Type: schema.ComponentTypeEntityRef},
		},
		EntityTypes: map[string]schema.EntityType{
			"Thing": {
				RequiredComponents:   []string{"Position"},
				OptionalComponents:   []string{"Hp", "Tags", "Carrier"},
				AllowExtraComponents: false,
			},
		},
	}
}

func mustInsert(t *testing.T, ctx context.Context, tx world.Tx, id int64, comp string, vals world.ComponentValues) {
	t.Helper()
	if err := tx.InsertComponent(ctx, id, comp, vals); err != nil {
		t.Fatalf("InsertComponent %s: %v", comp, err)
	}
}

func withTx(t *testing.T, store *SQLiteStore, fn func(ctx context.Context, tx world.Tx)) {
	t.Helper()
	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	fn(ctx, tx)
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func TestSetComponentValues_UpdatesOnlyTheNamedFields(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	var id int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		var err error
		id, err = tx.InsertEntity(ctx, "Thing", 0)
		if err != nil {
			t.Fatalf("InsertEntity: %v", err)
		}
		if err := tx.InsertComponent(ctx, id, "Position", world.ComponentValues{
			"x": 1.0, "y": 2.0, "label": "start",
		}); err != nil {
			t.Fatalf("InsertComponent: %v", err)
		}
	})

	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		if err := tx.SetComponentValues(ctx, id, "Position", world.ComponentValues{"y": 9.0}); err != nil {
			t.Fatalf("SetComponentValues: %v", err)
		}
	})

	var x, y float64
	var label string
	if err := store.DB().QueryRow(
		`SELECT x, y, label FROM comp_position WHERE entity_id = ?`, id,
	).Scan(&x, &y, &label); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if y != 9 {
		t.Errorf("y = %v, want 9", y)
	}
	if x != 1 || label != "start" {
		t.Errorf("x = %v, label = %q — a field nobody named was rewritten", x, label)
	}
}

func TestSetComponentValues_TouchesOnlyTheEntityItNames(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	var a, b int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		a, _ = tx.InsertEntity(ctx, "Thing", 0)
		mustInsert(t, ctx, tx, a, "Position", world.ComponentValues{"x": 1.0, "y": 1.0, "label": "a"})
		b, _ = tx.InsertEntity(ctx, "Thing", 0)
		mustInsert(t, ctx, tx, b, "Position", world.ComponentValues{"x": 2.0, "y": 2.0, "label": "b"})
	})

	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		if err := tx.SetComponentValues(ctx, a, "Position", world.ComponentValues{"label": "moved"}); err != nil {
			t.Fatalf("SetComponentValues: %v", err)
		}
	})

	var label string
	if err := store.DB().QueryRow(`SELECT label FROM comp_position WHERE entity_id = ?`, b).Scan(&label); err != nil {
		t.Fatalf("reading the other entity: %v", err)
	}
	if label != "b" {
		t.Errorf("the other entity's label = %q, want it untouched", label)
	}
}

func TestSetComponentValues_RefusesAFieldTheComponentDoesNotHave(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	var id int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		id, _ = tx.InsertEntity(ctx, "Thing", 0)
		mustInsert(t, ctx, tx, id, "Position", world.ComponentValues{"x": 1.0, "y": 2.0, "label": "l"})
	})

	ctx := context.Background()
	tx, _ := store.BeginTx(ctx)
	defer func() { _ = tx.Rollback() }()
	err := tx.SetComponentValues(ctx, id, "Position", world.ComponentValues{"z": 1.0})
	if err == nil {
		t.Fatal("a property the component does not declare was accepted")
	}
	if !strings.Contains(err.Error(), `"z"`) {
		t.Errorf("error = %q, want it to name the property", err)
	}
}

func TestSetComponentValues_RefusesAComponentTheSchemaDoesNotDeclare(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	ctx := context.Background()
	tx, _ := store.BeginTx(ctx)
	defer func() { _ = tx.Rollback() }()

	err := tx.SetComponentValues(ctx, 1, "Nonsense", world.ComponentValues{"x": 1})
	if err == nil {
		t.Fatal("an undeclared component was accepted")
	}
	if !strings.Contains(err.Error(), "Nonsense") {
		t.Errorf("error = %q, want it to name the component", err)
	}
}

func TestSetComponentValues_NoFieldsIsNoStatement(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	var id int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		id, _ = tx.InsertEntity(ctx, "Thing", 0)
		mustInsert(t, ctx, tx, id, "Position", world.ComponentValues{"x": 1.0, "y": 2.0, "label": "l"})
	})

	// A trigger, so "no statement" is a claim about what the database was told
	// rather than about what the caller believes it asked for.
	for _, stmt := range []string{
		`CREATE TABLE writes (entity_id INTEGER NOT NULL)`,
		`CREATE TRIGGER w AFTER UPDATE ON comp_position
		 BEGIN INSERT INTO writes VALUES (NEW.entity_id); END`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatalf("watching comp_position: %v", err)
		}
	}

	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		if err := tx.SetComponentValues(ctx, id, "Position", world.ComponentValues{}); err != nil {
			t.Fatalf("SetComponentValues with nothing to set: %v", err)
		}
	})

	var n int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM writes`).Scan(&n); err != nil {
		t.Fatalf("counting writes: %v", err)
	}
	if n != 0 {
		t.Errorf("an update with nothing to set still wrote %d times", n)
	}
}

func TestSetComponentValues_RefusesAComponentTheEntityDoesNotHave(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	var id int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		id, _ = tx.InsertEntity(ctx, "Thing", 0)
		mustInsert(t, ctx, tx, id, "Position", world.ComponentValues{"x": 1.0, "y": 2.0, "label": "l"})
	})

	ctx := context.Background()
	tx, _ := store.BeginTx(ctx)
	defer func() { _ = tx.Rollback() }()

	// Hp is declared and the entity may carry it; this one does not.
	err := tx.SetComponentValues(ctx, id, "Hp", world.ComponentValues{"value": 3})
	if err == nil {
		t.Fatal("an update that matched no row was reported as a write")
	}
	if !strings.Contains(err.Error(), "comp_hp") {
		t.Errorf("error = %q, want it to name the component", err)
	}

	// And the same for an entity that is not there at all.
	if err := tx.SetComponentValues(ctx, 9999, "Position", world.ComponentValues{"x": 1.0}); err == nil {
		t.Error("an update to an entity that does not exist was reported as a write")
	}
}

func TestSetComponentValues_RefusesAnEntityRefTargetThatIsNil(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	var id int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		id, _ = tx.InsertEntity(ctx, "Thing", 0)
		mustInsert(t, ctx, tx, id, "Carrier", world.ComponentValues{"target_entity_id": id})
	})

	ctx := context.Background()
	tx, _ := store.BeginTx(ctx)
	defer func() { _ = tx.Rollback() }()

	err := tx.SetComponentValues(ctx, id, "Carrier", world.ComponentValues{"target_entity_id": nil})
	if err == nil {
		t.Fatal("a nil target was accepted, to be refused later by a NOT NULL column")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("error = %q, want it to say the target is nil", err)
	}
}

func TestSetComponentValues_NamesTheSameBadFieldEveryTime(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	ctx := context.Background()

	// Two fields the component does not declare: the refusal has to name the
	// first in sorted order, not the first the map happened to yield.
	for range 20 {
		tx, _ := store.BeginTx(ctx)
		err := tx.SetComponentValues(ctx, 1, "Position", world.ComponentValues{"z": 1.0, "w": 2.0})
		_ = tx.Rollback()
		if err == nil {
			t.Fatal("two undeclared fields were accepted")
		}
		if !strings.Contains(err.Error(), `"w"`) {
			t.Fatalf("error = %q, want it to name %q every time", err, "w")
		}
	}
}

func TestSetComponentValues_ScalarArrayAndEntityRef(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	var id, other int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		id, _ = tx.InsertEntity(ctx, "Thing", 0)
		other, _ = tx.InsertEntity(ctx, "Thing", 0)
		for _, c := range []struct {
			name string
			vals world.ComponentValues
		}{
			{"Hp", world.ComponentValues{"value": 10}},
			{"Tags", world.ComponentValues{"value": []any{"a"}}},
			{"Carrier", world.ComponentValues{"target_entity_id": other}},
		} {
			if err := tx.InsertComponent(ctx, id, c.name, c.vals); err != nil {
				t.Fatalf("InsertComponent %s: %v", c.name, err)
			}
		}
	})

	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		if err := tx.SetComponentValues(ctx, id, "Hp", world.ComponentValues{"value": 3}); err != nil {
			t.Fatalf("Hp: %v", err)
		}
		if err := tx.SetComponentValues(ctx, id, "Tags", world.ComponentValues{"value": []any{"b", "c"}}); err != nil {
			t.Fatalf("Tags: %v", err)
		}
		if err := tx.SetComponentValues(ctx, id, "Carrier", world.ComponentValues{"target_entity_id": id}); err != nil {
			t.Fatalf("Carrier: %v", err)
		}
	})

	var hp int
	if err := store.DB().QueryRow(`SELECT value FROM comp_hp WHERE entity_id = ?`, id).Scan(&hp); err != nil {
		t.Fatalf("reading hp: %v", err)
	}
	if hp != 3 {
		t.Errorf("hp = %d, want 3", hp)
	}
	var tags string
	if err := store.DB().QueryRow(`SELECT value FROM comp_tags WHERE entity_id = ?`, id).Scan(&tags); err != nil {
		t.Fatalf("reading tags: %v", err)
	}
	if tags != `["b","c"]` {
		t.Errorf("tags = %s, want the new array as JSON", tags)
	}
	var target int64
	if err := store.DB().QueryRow(`SELECT target_entity_id FROM comp_carrier WHERE entity_id = ?`, id).Scan(&target); err != nil {
		t.Fatalf("reading carrier: %v", err)
	}
	if target != id {
		t.Errorf("target = %d, want %d", target, id)
	}
}

func TestSetComponentValues_RefusesAValueUnderTheWrongKey(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	ctx := context.Background()
	tx, _ := store.BeginTx(ctx)
	defer func() { _ = tx.Rollback() }()

	for _, c := range []struct{ comp, key string }{
		{"Hp", "hp"},
		{"Tags", "items"},
		{"Carrier", "entity"},
	} {
		err := tx.SetComponentValues(ctx, 1, c.comp, world.ComponentValues{c.key: 1})
		if err == nil {
			t.Errorf("%s: a value under %q was accepted", c.comp, c.key)
		}
	}
}

func TestSetComponentValues_EntityRefTakesTheShorthandKeyToo(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	var id, other int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		id, _ = tx.InsertEntity(ctx, "Thing", 0)
		other, _ = tx.InsertEntity(ctx, "Thing", 0)
		mustInsert(t, ctx, tx, id, "Carrier", world.ComponentValues{"target_entity_id": id})
	})

	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		if err := tx.SetComponentValues(ctx, id, "Carrier", world.ComponentValues{"target": other}); err != nil {
			t.Fatalf("SetComponentValues: %v", err)
		}
	})

	var target int64
	if err := store.DB().QueryRow(`SELECT target_entity_id FROM comp_carrier WHERE entity_id = ?`, id).Scan(&target); err != nil {
		t.Fatalf("reading carrier: %v", err)
	}
	if target != other {
		t.Errorf("target = %d, want %d — InsertComponent takes \"target\" and this has to as well", target, other)
	}
}

func TestDeleteEntity_TakesEveryComponentRowWithIt(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	var id int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		id, _ = tx.InsertEntity(ctx, "Thing", 0)
		mustInsert(t, ctx, tx, id, "Position", world.ComponentValues{"x": 1.0, "y": 2.0, "label": "l"})
		mustInsert(t, ctx, tx, id, "Hp", world.ComponentValues{"value": 10})
	})

	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		if err := tx.DeleteEntity(ctx, id); err != nil {
			t.Fatalf("DeleteEntity: %v", err)
		}
	})

	for _, table := range []string{"entities", "comp_position", "comp_hp"} {
		col := "entity_id"
		if table == "entities" {
			col = "id"
		}
		var n int
		if err := store.DB().QueryRow(
			"SELECT COUNT(*) FROM "+table+" WHERE "+col+" = ?", id,
		).Scan(&n); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s still holds %d rows for the deleted entity", table, n)
		}
	}
}

// The store now puts foreign_keys on every pooled connection, so the cascade is
// real. This test turns it off anyway, because the explicit deletes have to be
// what does the work: a database opened by another tool, or by a driver whose
// DSN parameters are spelled differently, may not have it on.
func TestDeleteEntity_DoesNotNeedForeignKeysEnforced(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	var id int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		id, _ = tx.InsertEntity(ctx, "Thing", 0)
		mustInsert(t, ctx, tx, id, "Position", world.ComponentValues{"x": 1.0, "y": 2.0, "label": "l"})
	})

	ctx := context.Background()
	conn, err := store.DB().Conn(ctx)
	if err != nil {
		t.Fatalf("Conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	// Put back before the connection returns to the pool, the way migration.go
	// does. It is a shared connection, and one left with enforcement off is one
	// that quietly agrees with whatever the next caller writes.
	defer func() {
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
			t.Errorf("re-enabling foreign keys: %v", err)
		}
	}()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("disabling foreign keys: %v", err)
	}
	sqlTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	tx := &sqliteTx{tx: sqlTx, schema: everyKindSchema()}
	if err := tx.DeleteEntity(ctx, id); err != nil {
		t.Fatalf("DeleteEntity: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var n int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM comp_position WHERE entity_id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("counting comp_position: %v", err)
	}
	if n != 0 {
		t.Errorf("comp_position kept %d orphan rows — the delete leaned on a cascade that was not enforced", n)
	}
}

func TestDeleteEntity_LeavesOtherEntitiesAlone(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	var keep, drop int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		keep, _ = tx.InsertEntity(ctx, "Thing", 0)
		mustInsert(t, ctx, tx, keep, "Position", world.ComponentValues{"x": 1.0, "y": 2.0, "label": "keep"})
		drop, _ = tx.InsertEntity(ctx, "Thing", 0)
		mustInsert(t, ctx, tx, drop, "Position", world.ComponentValues{"x": 3.0, "y": 4.0, "label": "drop"})
	})

	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		if err := tx.DeleteEntity(ctx, drop); err != nil {
			t.Fatalf("DeleteEntity: %v", err)
		}
	})

	var x float64
	if err := store.DB().QueryRow(`SELECT x FROM comp_position WHERE entity_id = ?`, keep).Scan(&x); err != nil {
		t.Fatalf("the surviving entity lost its component: %v", err)
	}
	if x != 1 {
		t.Errorf("x = %v, want 1", x)
	}
}

// A component row the database refuses to delete has to be reported, not
// stepped over. The check runs on a pinned connection with foreign keys off,
// because with the cascade enforced the entities delete would hit the same
// refusal and mask a DeleteEntity that ignored its own errors.
func TestDeleteEntity_ReportsAComponentRowItCouldNotRemove(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	var id int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		id, _ = tx.InsertEntity(ctx, "Thing", 0)
		mustInsert(t, ctx, tx, id, "Position", world.ComponentValues{"x": 1.0, "y": 2.0, "label": "l"})
	})
	if _, err := store.DB().Exec(`CREATE TRIGGER no_delete BEFORE DELETE ON comp_position
		BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("installing refusal: %v", err)
	}

	ctx := context.Background()
	conn, err := store.DB().Conn(ctx)
	if err != nil {
		t.Fatalf("Conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	// Put back before the connection returns to the pool, the way migration.go
	// does. It is a shared connection, and one left with enforcement off is one
	// that quietly agrees with whatever the next caller writes.
	defer func() {
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
			t.Errorf("re-enabling foreign keys: %v", err)
		}
	}()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("disabling foreign keys: %v", err)
	}
	sqlTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = sqlTx.Rollback() }()

	tx := &sqliteTx{tx: sqlTx, schema: everyKindSchema()}
	if err := tx.DeleteEntity(ctx, id); err == nil {
		t.Fatal("DeleteEntity reported success over a component row it did not remove")
	}
}

func TestDeleteEntity_TakesTheInterpreterStateWithIt(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	if err := EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	var id, other int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		id, _ = tx.InsertEntity(ctx, "Thing", 0)
		other, _ = tx.InsertEntity(ctx, "Thing", 0)
	})
	for _, e := range []int64{id, other} {
		if _, err := store.DB().Exec(
			`INSERT INTO behavior_components (entity_id, machine_id, current_states, updated_at)
			 VALUES (?, 'goblin', '["idle"]', 0)`, e); err != nil {
			t.Fatalf("starting a machine: %v", err)
		}
		if _, err := store.DB().Exec(
			`INSERT INTO event_queue (entity_id, machine_id, event_type, target_tick)
			 VALUES (?, 'goblin', 'WAKE', 10)`, e); err != nil {
			t.Fatalf("queueing an event: %v", err)
		}
	}
	if _, err := store.DB().Exec(
		`INSERT INTO transitions (tick, wall_ms, entity_id, machine_id, from_states, to_states, event, actions_run)
		 VALUES (1, 0, ?, 'goblin', '["idle"]', '["walk"]', 'TICK', '[]')`, id); err != nil {
		t.Fatalf("recording a transition: %v", err)
	}

	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		if err := tx.DeleteEntity(ctx, id); err != nil {
			t.Fatalf("DeleteEntity: %v", err)
		}
	})

	// The tick reads behavior_components and event_queue with no join to
	// entities, so a row left behind runs a machine for an entity that is gone.
	for _, table := range []string{"behavior_components", "event_queue"} {
		var n int
		if err := store.DB().QueryRow(
			"SELECT COUNT(*) FROM "+table+" WHERE entity_id = ?", id).Scan(&n); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s kept %d rows for a deleted entity — the tick would keep running it", table, n)
		}
		if err := store.DB().QueryRow(
			"SELECT COUNT(*) FROM "+table+" WHERE entity_id = ?", other).Scan(&n); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if n != 1 {
			t.Errorf("%s lost the row belonging to another entity", table)
		}
	}

	// transitions is the audit log and outlives what it describes.
	var kept int
	if err := store.DB().QueryRow(
		`SELECT COUNT(*) FROM transitions WHERE entity_id = ?`, id).Scan(&kept); err != nil {
		t.Fatalf("counting transitions: %v", err)
	}
	if kept != 1 {
		t.Errorf("the audit log lost %d rows about a deleted entity", 1-kept)
	}
}

func TestDeleteEntity_WorksWithoutTheInterpreterTables(t *testing.T) {
	// EnsureInterpreterTables is a separate call; a store used only for entities
	// never makes them, and a delete must not need them to exist.
	store := makeStore(t, everyKindSchema())
	var id int64
	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		id, _ = tx.InsertEntity(ctx, "Thing", 0)
		mustInsert(t, ctx, tx, id, "Position", world.ComponentValues{"x": 1.0, "y": 2.0, "label": "l"})
	})

	withTx(t, store, func(ctx context.Context, tx world.Tx) {
		if err := tx.DeleteEntity(ctx, id); err != nil {
			t.Fatalf("DeleteEntity: %v", err)
		}
	})
}

// And the entity-ref itself is enforced on the way in, which is the other half
// of what turning foreign keys on bought: a component cannot point at an entity
// that was never there.
func TestInsertComponent_RefusesAnEntityRefToNothing(t *testing.T) {
	store := makeStore(t, everyKindSchema())
	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	id, err := tx.InsertEntity(ctx, "Thing", 0)
	if err != nil {
		t.Fatalf("InsertEntity: %v", err)
	}
	mustInsert(t, ctx, tx, id, "Position", world.ComponentValues{"x": 0.0, "y": 0.0, "label": "l"})

	if err := tx.InsertComponent(ctx, id, "Carrier", world.ComponentValues{"target_entity_id": int64(9999)}); err == nil {
		t.Fatal("a component pointed at an entity that does not exist and was accepted")
	}
}
