package builtins_test

import (
	"context"
	"database/sql"
	"math"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/storage"
	_ "modernc.org/sqlite"
)

// ── Test DB setup ─────────────────────────────────────────────────────────────

func setupBuiltinsDB(t *testing.T) *sql.DB {
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
		`CREATE TABLE comp_goblinstats (entity_id INTEGER PRIMARY KEY, speed REAL NOT NULL DEFAULT 2, aggrorange REAL NOT NULL DEFAULT 80, target_x REAL NOT NULL DEFAULT 0, target_y REAL NOT NULL DEFAULT 0, patience REAL NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return db
}

func insertEntity(t *testing.T, db *sql.DB, entityType string) int64 {
	t.Helper()
	res, err := db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES (?, 0)", entityType)
	if err != nil {
		t.Fatalf("insertEntity: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// goblinManifest maps the wandering_goblin context keys to GoblinStats component.
var goblinManifest = map[string]string{
	"speed":      "GoblinStats",
	"aggrorange": "GoblinStats",
	"target_x":   "GoblinStats",
	"target_y":   "GoblinStats",
	"patience":   "GoblinStats",
}

// runAction creates a tx, runs fn(writer, reader), commits, then runs check(db).
func runAction(t *testing.T, db *sql.DB, fn func(w agent.WorldWriter, r agent.WorldReader)) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	fn(storage.NewTxWorldWriter(tx), storage.NewTxWorldReader(tx))
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func actx(entityID int64, w agent.WorldWriter, r agent.WorldReader, params map[string]any) agent.ActionContext {
	return agent.ActionContext{
		EntityID:        entityID,
		Tick:            1,
		World:           w,
		Reader:          r,
		Params:          params,
		Event:           agent.Event{Type: "TICK"},
		ContextManifest: goblinManifest,
	}
}

// ── Action tests ──────────────────────────────────────────────────────────────

func TestAction_setTimer(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_goblinstats (entity_id) VALUES (?)", entityID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{"key": "patience", "ticks": float64(40)})
		handler, _ := r.GetAction("setTimer")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("setTimer: %v", err)
		}
	})

	var patience float64
	db.QueryRow("SELECT patience FROM comp_goblinstats WHERE entity_id = ?", entityID).Scan(&patience)
	if patience != 40 {
		t.Errorf("patience = %v, want 40", patience)
	}
}

func TestAction_moveTowardTarget_MovesStep(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_position    (entity_id, x, y)                              VALUES (?, 0, 0)", entityID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, speed, target_x, target_y)         VALUES (?, 2, 10, 0)", entityID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, nil)
		handler, _ := r.GetAction("moveTowardTarget")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("moveTowardTarget: %v", err)
		}
	})

	var x, y float64
	db.QueryRow("SELECT x, y FROM comp_position WHERE entity_id = ?", entityID).Scan(&x, &y)
	// direction is (10,0), speed=2 → new position (2, 0)
	if math.Abs(x-2.0) > 0.001 || math.Abs(y) > 0.001 {
		t.Errorf("position = (%v, %v), want (2, 0)", x, y)
	}
}

func TestAction_moveTowardTarget_SpeedMult(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_position    (entity_id, x, y)                              VALUES (?, 0, 0)", entityID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, speed, target_x, target_y)         VALUES (?, 2, 10, 0)", entityID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{"speed_mult": float64(1.5)})
		handler, _ := r.GetAction("moveTowardTarget")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("moveTowardTarget: %v", err)
		}
	})

	var x float64
	db.QueryRow("SELECT x FROM comp_position WHERE entity_id = ?", entityID).Scan(&x)
	// speed=2 * mult=1.5 = 3; dist=10 > 3 → x=3
	if math.Abs(x-3.0) > 0.001 {
		t.Errorf("x = %v, want 3.0 (speed_mult=1.5)", x)
	}
}

func TestAction_moveTowardTarget_AlreadyAtTarget(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_position    (entity_id, x, y)                              VALUES (?, 5, 5)", entityID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, speed, target_x, target_y)         VALUES (?, 2, 5, 5)", entityID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, nil)
		handler, _ := r.GetAction("moveTowardTarget")
		_ = handler.Run(ctx)
	})

	var x, y float64
	db.QueryRow("SELECT x, y FROM comp_position WHERE entity_id = ?", entityID).Scan(&x, &y)
	// position unchanged when dist < epsilon
	if math.Abs(x-5.0) > 0.001 || math.Abs(y-5.0) > 0.001 {
		t.Errorf("position = (%v, %v), expected no movement at target", x, y)
	}
}

func TestAction_pickRandomTarget_WithinRadius(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_position    (entity_id, x, y)                              VALUES (?, 50, 50)", entityID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, target_x, target_y)                VALUES (?, 0, 0)", entityID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{"radius": float64(100)})
		handler, _ := r.GetAction("pickRandomTarget")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("pickRandomTarget: %v", err)
		}
	})

	var tx, ty float64
	db.QueryRow("SELECT target_x, target_y FROM comp_goblinstats WHERE entity_id = ?", entityID).Scan(&tx, &ty)
	dx, dy := tx-50, ty-50
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist > 100+0.001 {
		t.Errorf("target at distance %v > radius 100", dist)
	}
}

func TestAction_setPursueTarget(t *testing.T) {
	db := setupBuiltinsDB(t)
	playerID := insertEntity(t, db, "Player")
	goblinID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_position    (entity_id, x, y) VALUES (?, 77, 88)", playerID)
	db.Exec("INSERT INTO comp_position    (entity_id, x, y) VALUES (?, 0, 0)", goblinID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, target_x, target_y) VALUES (?, 0, 0)", goblinID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(goblinID, w, rd, nil)
		handler, _ := r.GetAction("setPursueTarget")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("setPursueTarget: %v", err)
		}
	})

	var tx, ty float64
	db.QueryRow("SELECT target_x, target_y FROM comp_goblinstats WHERE entity_id = ?", goblinID).Scan(&tx, &ty)
	if math.Abs(tx-77) > 0.001 || math.Abs(ty-88) > 0.001 {
		t.Errorf("target = (%v, %v), want (77, 88)", tx, ty)
	}
}

func TestAction_dealDamage_DirectEntityID(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	targetID := insertEntity(t, db, "Player")
	db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (?, 100)", targetID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{"amount": float64(15), "target": float64(targetID)})
		handler, _ := r.GetAction("dealDamage")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("dealDamage: %v", err)
		}
	})

	var hp float64
	db.QueryRow("SELECT hp FROM comp_health WHERE entity_id = ?", targetID).Scan(&hp)
	if math.Abs(hp-85) > 0.001 {
		t.Errorf("hp = %v, want 85", hp)
	}
}

func TestAction_dealDamage_PlayerSentinel(t *testing.T) {
	db := setupBuiltinsDB(t)
	goblinID := insertEntity(t, db, "Goblin")
	playerID := insertEntity(t, db, "Player")
	db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (?, 100)", playerID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(goblinID, w, rd, map[string]any{"amount": float64(5), "target": "$player"})
		handler, _ := r.GetAction("dealDamage")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("dealDamage $player: %v", err)
		}
	})

	var hp float64
	db.QueryRow("SELECT hp FROM comp_health WHERE entity_id = ?", playerID).Scan(&hp)
	if math.Abs(hp-95) > 0.001 {
		t.Errorf("hp = %v, want 95", hp)
	}
}

func TestAction_spawnEntity(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{"entity_type": "Player"})
		handler, _ := r.GetAction("spawnEntity")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("spawnEntity: %v", err)
		}
	})

	var count int
	db.QueryRow("SELECT COUNT(*) FROM entities WHERE entity_type = 'Player'").Scan(&count)
	if count != 1 {
		t.Errorf("Player entities = %d, want 1", count)
	}
}

func TestAction_attachComponent(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{
			"component": "Position",
			"data":      map[string]any{"x": 1.0, "y": 2.0},
		})
		handler, _ := r.GetAction("attachComponent")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("attachComponent: %v", err)
		}
	})

	var x float64
	db.QueryRow("SELECT x FROM comp_position WHERE entity_id = ?", entityID).Scan(&x)
	if math.Abs(x-1.0) > 0.001 {
		t.Errorf("x = %v, want 1.0", x)
	}
}

func TestAction_detachComponent(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (?, 0, 0)", entityID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{"component": "Position"})
		handler, _ := r.GetAction("detachComponent")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("detachComponent: %v", err)
		}
	})

	var count int
	db.QueryRow("SELECT COUNT(*) FROM comp_position WHERE entity_id = ?", entityID).Scan(&count)
	if count != 0 {
		t.Errorf("comp_position rows = %d after detach, want 0", count)
	}
}

func TestAction_log(t *testing.T) {
	r := builtins.NewRegistry()
	handler, ok := r.GetAction("log")
	if !ok {
		t.Fatal("log action not registered")
	}
	ctx := agent.ActionContext{Params: map[string]any{"message": "hello world"}}
	if err := handler.Run(ctx); err != nil {
		t.Errorf("log: %v", err)
	}
}

// ── Guard helpers ─────────────────────────────────────────────────────────────

func gctx(entityID int64, reader agent.WorldReader, params map[string]any) agent.GuardContext {
	return agent.GuardContext{
		EntityID:        entityID,
		Tick:            1,
		World:           reader,
		Params:          params,
		Event:           agent.Event{Type: "TICK"},
		ContextManifest: goblinManifest,
	}
}

func readGuard(t *testing.T, db *sql.DB, fn func(r agent.WorldReader) bool) bool {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()
	return fn(storage.NewTxWorldReader(tx))
}

// ── Guard tests ───────────────────────────────────────────────────────────────

func TestGuard_timerExpired_True(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_goblinstats (entity_id, patience) VALUES (?, 0)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("timerExpired")
		return handler.Evaluate(gctx(entityID, rd, map[string]any{"key": "patience"}))
	})
	if !got {
		t.Error("timerExpired(patience=0) = false, want true")
	}
}

func TestGuard_timerExpired_False(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_goblinstats (entity_id, patience) VALUES (?, 10)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("timerExpired")
		return handler.Evaluate(gctx(entityID, rd, map[string]any{"key": "patience"}))
	})
	if got {
		t.Error("timerExpired(patience=10) = true, want false")
	}
}

func TestGuard_atTarget_True(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	// Position (5, 5), target (5.5, 5) → distance = 0.5 ≤ 1.0
	db.Exec("INSERT INTO comp_position    (entity_id, x, y)               VALUES (?, 5, 5)", entityID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, target_x, target_y) VALUES (?, 5.5, 5)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("atTarget")
		return handler.Evaluate(gctx(entityID, rd, nil))
	})
	if !got {
		t.Error("atTarget at distance 0.5 = false, want true")
	}
}

func TestGuard_atTarget_False(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	// Position (0, 0), target (10, 0) → distance = 10 > 1.0
	db.Exec("INSERT INTO comp_position    (entity_id, x, y)               VALUES (?, 0, 0)", entityID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, target_x, target_y) VALUES (?, 10, 0)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("atTarget")
		return handler.Evaluate(gctx(entityID, rd, nil))
	})
	if got {
		t.Error("atTarget at distance 10 = true, want false")
	}
}

func TestGuard_inRange_True(t *testing.T) {
	db := setupBuiltinsDB(t)
	goblinID := insertEntity(t, db, "Goblin")
	playerID := insertEntity(t, db, "Player")
	// Goblin at (0,0), Player at (5,0) → dist=5 ≤ 10
	db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (?, 0, 0)", goblinID)
	db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (?, 5, 0)", playerID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		ctx := agent.GuardContext{
			EntityID: goblinID, World: rd,
			Params:          map[string]any{"target": "$player", "distance": float64(10)},
			ContextManifest: goblinManifest,
		}
		handler, _ := r.GetGuard("inRange")
		return handler.Evaluate(ctx)
	})
	if !got {
		t.Error("inRange(dist=5, range=10) = false, want true")
	}
}

func TestGuard_inRange_False(t *testing.T) {
	db := setupBuiltinsDB(t)
	goblinID := insertEntity(t, db, "Goblin")
	playerID := insertEntity(t, db, "Player")
	// Goblin at (0,0), Player at (20,0) → dist=20 > 10
	db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (?, 0, 0)", goblinID)
	db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (?, 20, 0)", playerID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		ctx := agent.GuardContext{
			EntityID: goblinID, World: rd,
			Params:          map[string]any{"target": "$player", "distance": float64(10)},
			ContextManifest: goblinManifest,
		}
		handler, _ := r.GetGuard("inRange")
		return handler.Evaluate(ctx)
	})
	if got {
		t.Error("inRange(dist=20, range=10) = true, want false")
	}
}

func TestGuard_hasComponent_True(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (?, 100)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("hasComponent")
		return handler.Evaluate(gctx(entityID, rd, map[string]any{"component": "Health"}))
	})
	if !got {
		t.Error("hasComponent(Health) = false, want true")
	}
}

func TestGuard_hasComponent_False(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("hasComponent")
		return handler.Evaluate(gctx(entityID, rd, map[string]any{"component": "Health"}))
	})
	if got {
		t.Error("hasComponent(Health) on entity without Health = true, want false")
	}
}

func TestGuard_healthAbove_True(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (?, 80)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("healthAbove")
		return handler.Evaluate(gctx(entityID, rd, map[string]any{"threshold": float64(50)}))
	})
	if !got {
		t.Error("healthAbove(hp=80, threshold=50) = false, want true")
	}
}

func TestGuard_healthAbove_False(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (?, 30)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("healthAbove")
		return handler.Evaluate(gctx(entityID, rd, map[string]any{"threshold": float64(50)}))
	})
	if got {
		t.Error("healthAbove(hp=30, threshold=50) = true, want false")
	}
}

// ── Registration ──────────────────────────────────────────────────────────────

func TestRegisterBuiltins_AllPresent(t *testing.T) {
	r := builtins.NewRegistry()

	wantActions := []string{
		"attachComponent", "dealDamage", "detachComponent",
		"log", "moveTowardTarget", "pickRandomTarget",
		"setPursueTarget", "setTimer", "spawnEntity",
	}
	for _, name := range wantActions {
		if _, ok := r.GetAction(name); !ok {
			t.Errorf("action %q not registered", name)
		}
	}

	wantGuards := []string{"atTarget", "hasComponent", "healthAbove", "inRange", "timerExpired"}
	for _, name := range wantGuards {
		if _, ok := r.GetGuard(name); !ok {
			t.Errorf("guard %q not registered", name)
		}
	}
}
