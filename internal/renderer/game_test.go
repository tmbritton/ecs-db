package renderer

import (
	"database/sql"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbSchema := schema.DatabaseSchema{SchemaVersion: 1}
	store, err := storage.NewSQLiteStore(":memory:", dbSchema, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	return store.DB()
}

func TestRunTick_AdvancesCurrentTick(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	ticker := newTicker(db, loader, registry)

	if err := ticker.RunTick(); err != nil {
		t.Fatalf("RunTick: %v", err)
	}

	var tick int64
	if err := db.QueryRow(`SELECT CAST(value AS INTEGER) FROM world WHERE key = 'current_tick'`).Scan(&tick); err != nil {
		t.Fatalf("reading current_tick: %v", err)
	}
	if tick != 1 {
		t.Errorf("current_tick = %d, want 1", tick)
	}
}

func TestRunTick_AdvancesWorldVersion(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	ticker := newTicker(db, loader, registry)

	for i := 0; i < 3; i++ {
		if err := ticker.RunTick(); err != nil {
			t.Fatalf("RunTick %d: %v", i, err)
		}
	}

	var version int64
	if err := db.QueryRow(`SELECT CAST(value AS INTEGER) FROM world WHERE key = 'world_version'`).Scan(&version); err != nil {
		t.Fatalf("reading world_version: %v", err)
	}
	if version != 3 {
		t.Errorf("world_version = %d, want 3", version)
	}
}
