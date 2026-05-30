package storage_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/storage"
	_ "modernc.org/sqlite"
)

func setupMachineWriterDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	stmts := []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY AUTOINCREMENT, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL DEFAULT 0)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	if err := storage.EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	return db
}

func insertTestEntity(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res, err := db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('test', 0)")
	if err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func beginWriterTx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	t.Cleanup(func() { tx.Rollback() })
	return tx
}

func TestSetMachineState_InsertsRow(t *testing.T) {
	db := setupMachineWriterDB(t)
	entityID := insertTestEntity(t, db)
	tx := beginWriterTx(t, db)
	mw := storage.NewMachineWriter(tx)

	if err := mw.SetMachineState(entityID, "m", []string{"m.idle"}, 5); err != nil {
		t.Fatalf("SetMachineState: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var states string
	var updatedAt int64
	err := db.QueryRow("SELECT current_states, updated_at FROM behavior_components WHERE entity_id=? AND machine_id=?", entityID, "m").Scan(&states, &updatedAt)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !strings.Contains(states, "m.idle") {
		t.Errorf("current_states = %q, want to contain m.idle", states)
	}
	if updatedAt != 5 {
		t.Errorf("updated_at = %d, want 5", updatedAt)
	}
}

func TestSetMachineState_UpdatesExistingRow(t *testing.T) {
	db := setupMachineWriterDB(t)
	entityID := insertTestEntity(t, db)

	tx1, _ := db.BeginTx(context.Background(), nil)
	_ = storage.NewMachineWriter(tx1).SetMachineState(entityID, "m", []string{"m.idle"}, 1)
	_ = tx1.Commit()

	tx2, _ := db.BeginTx(context.Background(), nil)
	_ = storage.NewMachineWriter(tx2).SetMachineState(entityID, "m", []string{"m.active"}, 2)
	_ = tx2.Commit()

	var states string
	_ = db.QueryRow("SELECT current_states FROM behavior_components WHERE entity_id=?", entityID).Scan(&states)
	if !strings.Contains(states, "m.active") {
		t.Errorf("current_states = %q, expected m.active after update", states)
	}
}

func TestAppendTransition_InsertsRow(t *testing.T) {
	db := setupMachineWriterDB(t)
	tx := beginWriterTx(t, db)
	mw := storage.NewMachineWriter(tx)

	tr := true
	rec := agent.TransitionRecord{
		Tick: 10, WallMs: 999, EntityID: 1, MachineID: "m",
		FromStates: []string{"m.a"}, ToStates: []string{"m.b"},
		Event: "GO", CondResult: &tr, ActionsRun: []string{"doWork"},
	}
	if err := mw.AppendTransition(rec); err != nil {
		t.Fatalf("AppendTransition: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var tick int64
	var event string
	var condResult sql.NullInt64
	err := db.QueryRow("SELECT tick, event, cond_result FROM transitions ORDER BY id DESC LIMIT 1").Scan(&tick, &event, &condResult)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if tick != 10 {
		t.Errorf("tick = %d, want 10", tick)
	}
	if event != "GO" {
		t.Errorf("event = %q, want GO", event)
	}
	if !condResult.Valid || condResult.Int64 != 1 {
		t.Errorf("cond_result = %v, want 1", condResult)
	}
}

func TestAppendTransition_NilCondResult(t *testing.T) {
	db := setupMachineWriterDB(t)
	tx := beginWriterTx(t, db)
	mw := storage.NewMachineWriter(tx)

	rec := agent.TransitionRecord{
		Tick: 1, WallMs: 0, EntityID: 1, MachineID: "m",
		FromStates: []string{"m.a"}, ToStates: []string{"m.b"},
		Event: "E", CondResult: nil, ActionsRun: nil,
	}
	_ = mw.AppendTransition(rec)
	_ = tx.Commit()

	var condResult sql.NullInt64
	_ = db.QueryRow("SELECT cond_result FROM transitions ORDER BY id DESC LIMIT 1").Scan(&condResult)
	if condResult.Valid {
		t.Errorf("cond_result = %v, want NULL for unconditional transition", condResult)
	}
}

func TestScheduleAfterEvent_InsertsRow(t *testing.T) {
	db := setupMachineWriterDB(t)
	tx := beginWriterTx(t, db)
	mw := storage.NewMachineWriter(tx)

	if err := mw.ScheduleAfterEvent(1, "m", "xstate.after(500).m.idle", 15); err != nil {
		t.Fatalf("ScheduleAfterEvent: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var evType string
	var targetTick int64
	err := db.QueryRow("SELECT event_type, target_tick FROM event_queue WHERE entity_id=1 AND machine_id='m'").Scan(&evType, &targetTick)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if evType != "xstate.after(500).m.idle" {
		t.Errorf("event_type = %q, want xstate.after(500).m.idle", evType)
	}
	if targetTick != 15 {
		t.Errorf("target_tick = %d, want 15", targetTick)
	}
}

func TestScheduleAfterEvent_MultipleEntries(t *testing.T) {
	db := setupMachineWriterDB(t)
	tx := beginWriterTx(t, db)
	mw := storage.NewMachineWriter(tx)

	_ = mw.ScheduleAfterEvent(1, "m", "xstate.after(500).m.idle", 10)
	_ = mw.ScheduleAfterEvent(1, "m", "xstate.after(1000).m.idle", 20)
	_ = tx.Commit()

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM event_queue WHERE entity_id=1 AND machine_id='m'").Scan(&count)
	if count != 2 {
		t.Errorf("event_queue rows = %d, want 2", count)
	}
}

func TestCancelAfterEvents_DeletesMatchingRows(t *testing.T) {
	db := setupMachineWriterDB(t)

	tx1, _ := db.BeginTx(context.Background(), nil)
	mw1 := storage.NewMachineWriter(tx1)
	_ = mw1.ScheduleAfterEvent(1, "m", "xstate.after(500).m.idle", 10)
	_ = mw1.ScheduleAfterEvent(1, "m", "xstate.after(200).m.active", 5)
	_ = tx1.Commit()

	tx2, _ := db.BeginTx(context.Background(), nil)
	mw2 := storage.NewMachineWriter(tx2)
	if err := mw2.CancelAfterEvents(1, "m", []string{"m.idle"}); err != nil {
		t.Fatalf("CancelAfterEvents: %v", err)
	}
	_ = tx2.Commit()

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM event_queue WHERE entity_id=1 AND machine_id='m'").Scan(&count)
	if count != 1 {
		t.Errorf("event_queue rows = %d, want 1 (m.active survives)", count)
	}
}

func TestCancelAfterEvents_NoopWhenEmpty(t *testing.T) {
	db := setupMachineWriterDB(t)
	tx := beginWriterTx(t, db)
	mw := storage.NewMachineWriter(tx)

	if err := mw.CancelAfterEvents(1, "m", []string{"m.idle"}); err != nil {
		t.Fatalf("CancelAfterEvents on empty queue: %v", err)
	}
}

func TestCancelAfterEvents_DoesNotMatchSiblingState(t *testing.T) {
	db := setupMachineWriterDB(t)

	tx1, _ := db.BeginTx(context.Background(), nil)
	mw1 := storage.NewMachineWriter(tx1)
	_ = mw1.ScheduleAfterEvent(1, "m", "xstate.after(500).m.idle", 10)
	_ = mw1.ScheduleAfterEvent(1, "m", "xstate.after(500).m.outer.idle", 10)
	_ = tx1.Commit()

	tx2, _ := db.BeginTx(context.Background(), nil)
	mw2 := storage.NewMachineWriter(tx2)
	_ = mw2.CancelAfterEvents(1, "m", []string{"m.idle"})
	_ = tx2.Commit()

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM event_queue WHERE entity_id=1").Scan(&count)
	if count != 1 {
		t.Errorf("event_queue rows = %d, want 1 (m.outer.idle survives)", count)
	}
}

func TestAfterEventFlow_EnterExitReenter(t *testing.T) {
	db := setupMachineWriterDB(t)
	entityID := insertTestEntity(t, db)

	tx1, _ := db.BeginTx(context.Background(), nil)
	mw := storage.NewMachineWriter(tx1)
	_ = mw.SetMachineState(entityID, "m", []string{"m.idle"}, 0)
	_ = mw.ScheduleAfterEvent(entityID, "m", "xstate.after(500).m.idle", 10)
	_ = tx1.Commit()

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM event_queue WHERE entity_id=?", entityID).Scan(&count)
	if count != 1 {
		t.Fatalf("after entry: event_queue rows = %d, want 1", count)
	}

	tx2, _ := db.BeginTx(context.Background(), nil)
	mw2 := storage.NewMachineWriter(tx2)
	_ = mw2.CancelAfterEvents(entityID, "m", []string{"m.idle"})
	_ = mw2.SetMachineState(entityID, "m", []string{"m.active"}, 1)
	_ = tx2.Commit()

	_ = db.QueryRow("SELECT COUNT(*) FROM event_queue WHERE entity_id=?", entityID).Scan(&count)
	if count != 0 {
		t.Fatalf("after exit: event_queue rows = %d, want 0", count)
	}

	tx3, _ := db.BeginTx(context.Background(), nil)
	mw3 := storage.NewMachineWriter(tx3)
	_ = mw3.ScheduleAfterEvent(entityID, "m", "xstate.after(500).m.idle", 25)
	_ = tx3.Commit()

	var targetTick int64
	_ = db.QueryRow("SELECT target_tick FROM event_queue WHERE entity_id=?", entityID).Scan(&targetTick)
	if targetTick != 25 {
		t.Errorf("re-entry target_tick = %d, want 25", targetTick)
	}
}
