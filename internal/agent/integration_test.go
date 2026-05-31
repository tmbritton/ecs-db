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

func sendEvent(t *testing.T, db *sql.DB, a *agent.Agent, ev agent.Event, tick int64, reg *agent.Registry) {
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

// ── Wandering goblin: event delivery ─────────────────────────────────────────

func TestWanderingGoblin_TimerExpiredTransition(t *testing.T) {
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

	// Zero patience so timerExpired guard returns true on next TICK.
	if _, err := db.Exec("UPDATE comp_goblinstats SET patience=0 WHERE entity_id=?", entityID); err != nil {
		t.Fatalf("zero patience: %v", err)
	}

	sendEvent(t, db, a, agent.Event{Type: "TICK"}, 1, reg)

	states := currentStates(t, db, entityID, "wandering_goblin")
	if len(states) == 0 || !strings.HasSuffix(states[0], ".wandering") {
		t.Errorf("after TICK with patience=0: states=%v, want [...wandering]", states)
	}
}

func TestWanderingGoblin_TargetlessMoveTowardTarget(t *testing.T) {
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

	// Move to wandering via timer expiry.
	if _, err := db.Exec("UPDATE comp_goblinstats SET patience=0 WHERE entity_id=?", entityID); err != nil {
		t.Fatalf("zero patience: %v", err)
	}
	sendEvent(t, db, a, agent.Event{Type: "TICK"}, 1, reg)

	// Set a far-away target so atTarget returns false → targetless TICK fires.
	if _, err := db.Exec("UPDATE comp_goblinstats SET target_x=9999, target_y=9999 WHERE entity_id=?", entityID); err != nil {
		t.Fatalf("set far target: %v", err)
	}
	sendEvent(t, db, a, agent.Event{Type: "TICK"}, 2, reg)

	states := currentStates(t, db, entityID, "wandering_goblin")
	if len(states) == 0 || !strings.HasSuffix(states[0], ".wandering") {
		t.Errorf("after targetless TICK: states=%v, want [...wandering]", states)
	}

	// Confirm moveTowardTarget actually ran — position must have moved toward (9999,9999).
	var newX, newY float64
	if err := db.QueryRow("SELECT x, y FROM comp_position WHERE entity_id=?", entityID).Scan(&newX, &newY); err != nil {
		t.Fatalf("query position after targetless TICK: %v", err)
	}
	if newX == 0 && newY == 0 {
		t.Errorf("position unchanged after targetless TICK: moveTowardTarget did not run")
	}
}

func TestWanderingGoblin_PlayerNearbyTransition(t *testing.T) {
	db := setupIntegrationDB(t)
	reg := builtins.NewRegistry()
	def := loadMachine(t, "testdata/behaviors/wandering_goblin.json")
	validateMachine(t, def, reg)

	entityID := insertEntity(t, db, "Goblin")
	if _, err := db.Exec("INSERT INTO comp_position (entity_id) VALUES (?)", entityID); err != nil {
		t.Fatalf("setup goblin position: %v", err)
	}
	if _, err := db.Exec("INSERT INTO comp_health (entity_id) VALUES (?)", entityID); err != nil {
		t.Fatalf("setup goblin health: %v", err)
	}

	// Create a Player so setPursueTarget (pursuing entry action) can resolve "$player".
	playerID := insertEntity(t, db, "Player")
	if _, err := db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (?, 50, 50)", playerID); err != nil {
		t.Fatalf("setup player position: %v", err)
	}

	a := agent.NewAgent(def, entityID, "", 50)
	startAgentTx(t, db, a, reg)

	sendEvent(t, db, a, agent.Event{Type: "PLAYER_NEARBY"}, 1, reg)

	states := currentStates(t, db, entityID, "wandering_goblin")
	if len(states) == 0 || !strings.HasSuffix(states[0], ".pursuing") {
		t.Errorf("after PLAYER_NEARBY: states=%v, want [...pursuing]", states)
	}
}

func TestWanderingGoblin_TransitionRecordAppended(t *testing.T) {
	db := setupIntegrationDB(t)
	reg := builtins.NewRegistry()
	def := loadMachine(t, "testdata/behaviors/wandering_goblin.json")
	validateMachine(t, def, reg)

	entityID := insertEntity(t, db, "Goblin")
	if _, err := db.Exec("INSERT INTO comp_position (entity_id) VALUES (?)", entityID); err != nil {
		t.Fatalf("setup goblin position: %v", err)
	}
	if _, err := db.Exec("INSERT INTO comp_health (entity_id) VALUES (?)", entityID); err != nil {
		t.Fatalf("setup goblin health: %v", err)
	}

	playerID := insertEntity(t, db, "Player")
	if _, err := db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (?, 50, 50)", playerID); err != nil {
		t.Fatalf("setup player position: %v", err)
	}

	a := agent.NewAgent(def, entityID, "", 50)
	startAgentTx(t, db, a, reg)

	sendEvent(t, db, a, agent.Event{Type: "PLAYER_NEARBY"}, 1, reg)

	// Query the transitions table — from_states and to_states are stored as JSON arrays.
	// Filter by event name so extra preceding transitions don't affect the assertion.
	var fromRaw, toRaw, event string
	err := db.QueryRow(
		"SELECT from_states, to_states, event FROM transitions WHERE entity_id=? AND machine_id=? AND event=? ORDER BY id DESC LIMIT 1",
		entityID, "wandering_goblin", "PLAYER_NEARBY",
	).Scan(&fromRaw, &toRaw, &event)
	if err != nil {
		t.Fatalf("query transitions: %v", err)
	}
	var fromArr, toArr []string
	if err := json.Unmarshal([]byte(fromRaw), &fromArr); err != nil {
		t.Fatalf("unmarshal from_states: %v", err)
	}
	if err := json.Unmarshal([]byte(toRaw), &toArr); err != nil {
		t.Fatalf("unmarshal to_states: %v", err)
	}
	if len(fromArr) == 0 || !strings.HasSuffix(fromArr[0], ".idle") {
		t.Errorf("from_states = %v, want [...idle]", fromArr)
	}
	if len(toArr) == 0 || !strings.HasSuffix(toArr[0], ".pursuing") {
		t.Errorf("to_states = %v, want [...pursuing]", toArr)
	}
	if event != "PLAYER_NEARBY" {
		t.Errorf("event = %q, want PLAYER_NEARBY", event)
	}
}

// ── Burning component lifecycle ───────────────────────────────────────────────

func TestBurningLifecycle_StartCreatesRow(t *testing.T) {
	db := setupIntegrationDB(t)
	reg := builtins.NewRegistry()
	def := loadMachine(t, "testdata/behaviors/burning.json")
	validateMachine(t, def, reg)

	entityID := insertEntity(t, db, "Goblin")
	// Simulate AttachComponent("Burning") having just occurred.
	if _, err := db.Exec("INSERT INTO comp_burning (entity_id, duration) VALUES (?, 5)", entityID); err != nil {
		t.Fatalf("setup burning: %v", err)
	}

	// ActivatedByComponent="Burning" so the machine knows to detach on final state.
	a := agent.NewAgent(def, entityID, "Burning", 50)
	startAgentTx(t, db, a, reg)

	states := currentStates(t, db, entityID, "burning")
	if len(states) == 0 || !strings.HasSuffix(states[0], ".active") {
		t.Errorf("current_states = %v, want [...active]", states)
	}
}

func TestBurningLifecycle_FinalStateDetachesComponent(t *testing.T) {
	// EXTINGUISH → extinguished (final) → DetachComponent("Burning") → comp_burning row deleted.
	db := setupIntegrationDB(t)
	reg := builtins.NewRegistry()
	def := loadMachine(t, "testdata/behaviors/burning.json")
	validateMachine(t, def, reg)

	entityID := insertEntity(t, db, "Goblin")
	if _, err := db.Exec("INSERT INTO comp_burning (entity_id, duration) VALUES (?, 5)", entityID); err != nil {
		t.Fatalf("setup burning: %v", err)
	}

	a := agent.NewAgent(def, entityID, "Burning", 50)
	startAgentTx(t, db, a, reg)

	sendEvent(t, db, a, agent.Event{Type: "EXTINGUISH"}, 1, reg)

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM comp_burning WHERE entity_id=?", entityID).Scan(&count); err != nil {
		t.Fatalf("query comp_burning: %v", err)
	}
	if count != 0 {
		t.Errorf("comp_burning rows = %d after final state, want 0 (detached)", count)
	}
}

// ── Stately round-trip ────────────────────────────────────────────────────────

func TestStatelyRoundTrip_ParseSucceeds(t *testing.T) {
	data, err := os.ReadFile("testdata/stately-export.json")
	if err != nil {
		t.Fatalf("read stately-export.json: %v", err)
	}
	def, err := agent.ParseMachine(data)
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	if def.ID == "" {
		t.Error("parsed machine has no ID")
	}
	if def.Initial == "" {
		t.Error("parsed machine has no Initial state")
	}
}

func TestStatelyRoundTrip_ValidateSupported(t *testing.T) {
	// traffic_light has no context, actions, or guards — passes with empty schema+registry.
	data, err := os.ReadFile("testdata/stately-export.json")
	if err != nil {
		t.Fatalf("read stately-export.json: %v", err)
	}
	def, err := agent.ParseMachine(data)
	if err != nil {
		t.Fatalf("ParseMachine: %v", err)
	}
	errs := agent.ValidateMachine(def, agent.NewRegistry(), schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	})
	if len(errs) > 0 {
		t.Errorf("ValidateMachine on supported machine: %v", errs)
	}
}

// ── Validation rejection ──────────────────────────────────────────────────────

func TestValidation_InvokeRejectedAtParse(t *testing.T) {
	raw := `{"id":"m","initial":"s","states":{"s":{"invoke":{"src":"someService"}}}}`
	_, err := agent.ParseMachine([]byte(raw))
	if err == nil {
		t.Fatal("expected error for invoke, got nil")
	}
	if !strings.Contains(err.Error(), "invoke") {
		t.Errorf("error = %q, want message containing 'invoke'", err.Error())
	}
}

func TestValidation_UnknownActionRejected(t *testing.T) {
	raw := `{"id":"m","initial":"s","states":{"s":{"entry":["unknownFoo"]}}}`
	def, err := agent.ParseMachine([]byte(raw))
	if err != nil {
		t.Fatalf("ParseMachine unexpectedly failed: %v", err)
	}
	errs := agent.ValidateMachine(def, agent.NewRegistry(), schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	})
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "unknownFoo") {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error mentioning 'unknownFoo', got none")
	}
}

func TestValidation_UnknownGuardRejected(t *testing.T) {
	raw := `{"id":"m","initial":"s","states":{"s":{"on":{"E":[{"target":"s","cond":"noSuchGuard"}]}}}}`
	def, err := agent.ParseMachine([]byte(raw))
	if err != nil {
		t.Fatalf("ParseMachine unexpectedly failed: %v", err)
	}
	errs := agent.ValidateMachine(def, agent.NewRegistry(), schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	})
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "noSuchGuard") {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error mentioning 'noSuchGuard', got none")
	}
}

func TestValidation_UndefinedTargetRejected(t *testing.T) {
	raw := `{"id":"m","initial":"s","states":{"s":{"on":{"E":"doesNotExist"}}}}`
	def, err := agent.ParseMachine([]byte(raw))
	if err != nil {
		t.Fatalf("ParseMachine unexpectedly failed: %v", err)
	}
	errs := agent.ValidateMachine(def, agent.NewRegistry(), schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	})
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "doesNotExist") || strings.Contains(e.Message, "unknown target") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected validation error for undefined target, errs=%v", errs)
	}
}

func TestValidation_AmbiguousContextKeyRejected(t *testing.T) {
	// "speed" appears in GoblinStats and a second component → ambiguous.
	raw := `{"id":"m","initial":"s","context":{"speed":1},"states":{"s":{}}}`
	def, err := agent.ParseMachine([]byte(raw))
	if err != nil {
		t.Fatalf("ParseMachine unexpectedly failed: %v", err)
	}
	ambig := goblinSchema()
	ambig.Components["Movement"] = schema.Component{
		Type: "object",
		Properties: map[string]schema.Property{
			"speed": {Type: "number"},
		},
	}
	errs := agent.ValidateMachine(def, agent.NewRegistry(), ambig)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "ambiguous") && strings.Contains(e.Field, "speed") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ambiguous-context error for 'speed', errs=%v", errs)
	}
}
