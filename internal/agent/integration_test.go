package agent_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	_ "modernc.org/sqlite"
)

// ── DB setup ──────────────────────────────────────────────────────────────────

func setupIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY AUTOINCREMENT, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_position    (entity_id INTEGER PRIMARY KEY, x REAL NOT NULL DEFAULT 0, y REAL NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_health      (entity_id INTEGER PRIMARY KEY, hp REAL NOT NULL DEFAULT 100, maxhp REAL NOT NULL DEFAULT 100)`,
		`CREATE TABLE comp_goblinstats (entity_id INTEGER PRIMARY KEY, speed REAL NOT NULL DEFAULT 2, aggrorange REAL NOT NULL DEFAULT 80, patience REAL NOT NULL DEFAULT 0, target_x REAL NOT NULL DEFAULT 0, target_y REAL NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_burning     (entity_id INTEGER PRIMARY KEY, duration REAL NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	if err := storage.EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	return db
}

func insertEntity(t *testing.T, db *sql.DB, entityType string) int64 {
	t.Helper()
	res, err := db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES (?, 0)", entityType)
	if err != nil {
		t.Fatalf("insertEntity: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("insertEntity LastInsertId: %v", err)
	}
	return id
}

// ── Schema ────────────────────────────────────────────────────────────────────

// goblinSchema covers all components used by wandering_goblin and burning tests.
func goblinSchema() schema.DatabaseSchema {
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
					"hp":    {Type: "number"},
					"maxhp": {Type: "number"},
				},
			},
			"GoblinStats": {
				Type: "object",
				Properties: map[string]schema.Property{
					"speed":      {Type: "number"},
					"aggroRange": {Type: "number"},
					"patience":   {Type: "number"},
					"target_x":   {Type: "number"},
					"target_y":   {Type: "number"},
				},
			},
			"Burning": {
				Type:     "object",
				Behavior: "burning",
				Properties: map[string]schema.Property{
					"duration": {Type: "number"},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{},
	}
}

// ── Machine loading ───────────────────────────────────────────────────────────

func loadMachine(t *testing.T, path string) *agent.MachineDefinition {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("loadMachine %s: %v", path, err)
	}
	def, err := agent.ParseMachine(data)
	if err != nil {
		t.Fatalf("ParseMachine %s: %v", path, err)
	}
	return def
}

func validateMachine(t *testing.T, def *agent.MachineDefinition, reg *agent.Registry) {
	t.Helper()
	errs := agent.ValidateMachine(def, reg, goblinSchema())
	if len(errs) > 0 {
		t.Fatalf("ValidateMachine: %v", errs)
	}
}

// ── Transaction helpers ───────────────────────────────────────────────────────

func startAgentTx(t *testing.T, db *sql.DB, a *agent.Agent, reg *agent.Registry) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	w := storage.NewTxWorldWriter(tx)
	r := storage.NewTxWorldReader(tx)
	mw := storage.NewMachineWriter(tx)
	if err := agent.StartAgent(a, reg, 0, w, r, mw); err != nil {
		tx.Rollback()
		t.Fatalf("StartAgent: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit StartAgent: %v", err)
	}
}

func sendEvent(t *testing.T, db *sql.DB, a *agent.Agent, ev agent.Event, tick int64, reg *agent.Registry) { //nolint:unused
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	w := storage.NewTxWorldWriter(tx)
	r := storage.NewTxWorldReader(tx)
	mw := storage.NewMachineWriter(tx)
	if err := agent.SendEvent(a, ev, tick, reg, w, r, mw); err != nil {
		tx.Rollback()
		t.Fatalf("SendEvent(%q): %v", ev.Type, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit SendEvent: %v", err)
	}
}

// currentStates reads and decodes current_states from behavior_components.
func currentStates(t *testing.T, db *sql.DB, entityID int64, machineID string) []string {
	t.Helper()
	var raw string
	err := db.QueryRow(
		"SELECT current_states FROM behavior_components WHERE entity_id=? AND machine_id=?",
		entityID, machineID,
	).Scan(&raw)
	if err != nil {
		t.Fatalf("query behavior_components: %v", err)
	}
	var states []string
	if err := json.Unmarshal([]byte(raw), &states); err != nil {
		t.Fatalf("unmarshal current_states: %v", err)
	}
	return states
}

// ── Wandering goblin: startup ─────────────────────────────────────────────────

func TestWanderingGoblin_StartCreatesRow(t *testing.T) {
	db := setupIntegrationDB(t)
	reg := builtins.NewRegistry()
	def := loadMachine(t, "testdata/behaviors/wandering_goblin.json")
	validateMachine(t, def, reg)

	entityID := insertEntity(t, db, "Goblin")
	if _, err := db.Exec("INSERT INTO comp_position (entity_id) VALUES (?)", entityID); err != nil {
		t.Fatalf("setup position: %v", err)
	}
	if _, err := db.Exec("INSERT INTO comp_health (entity_id) VALUES (?)", entityID); err != nil {
		t.Fatalf("setup health: %v", err)
	}

	a := agent.NewAgent(def, entityID, "", 50)
	startAgentTx(t, db, a, reg)

	states := currentStates(t, db, entityID, "wandering_goblin")
	if len(states) == 0 || !strings.HasSuffix(states[0], ".idle") {
		t.Errorf("current_states = %v, want [...idle]", states)
	}
}

func TestWanderingGoblin_SetTimerOnEntry(t *testing.T) {
	// StartAgent enters idle; idle entry runs setTimer(patience, 40).
	// GoblinStats.patience must equal 40 after startup.
	db := setupIntegrationDB(t)
	reg := builtins.NewRegistry()
	def := loadMachine(t, "testdata/behaviors/wandering_goblin.json")
	validateMachine(t, def, reg)

	entityID := insertEntity(t, db, "Goblin")
	if _, err := db.Exec("INSERT INTO comp_position (entity_id) VALUES (?)", entityID); err != nil {
		t.Fatalf("setup position: %v", err)
	}
	if _, err := db.Exec("INSERT INTO comp_health (entity_id) VALUES (?)", entityID); err != nil {
		t.Fatalf("setup health: %v", err)
	}

	a := agent.NewAgent(def, entityID, "", 50)
	startAgentTx(t, db, a, reg)

	var patience float64
	if err := db.QueryRow("SELECT patience FROM comp_goblinstats WHERE entity_id=?", entityID).Scan(&patience); err != nil {
		t.Fatalf("query patience: %v", err)
	}
	if patience != 40 {
		t.Errorf("patience = %v, want 40 (set by setTimer on idle entry)", patience)
	}
}
