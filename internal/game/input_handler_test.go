package game_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/game"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

func setupHandlerDB(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	stmts := []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY AUTOINCREMENT, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_position (entity_id INTEGER PRIMARY KEY, x REAL NOT NULL DEFAULT 0, y REAL NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_sprite (entity_id INTEGER PRIMARY KEY, sheet TEXT NOT NULL DEFAULT '', animation TEXT NOT NULL DEFAULT '', flip_x INTEGER NOT NULL DEFAULT 0)`,
		`INSERT INTO entities (entity_type) VALUES ('Player')`,
		`INSERT INTO comp_position (entity_id, x, y) VALUES (1, 2, 2)`,
		`INSERT INTO comp_sprite (entity_id, sheet, animation, flip_x) VALUES (1, '', 'player_idle', 0)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return db, 1
}

// smallGrid returns a 5×5 grid: only (2,2) and (3,2) are passable.
func smallGrid() *tilemap.TileGrid {
	g := tilemap.NewTileGrid(5, 5)
	g.SetPassable(2, 2, true)
	g.SetPassable(3, 2, true)
	return g
}

func runHandler(t *testing.T, db *sql.DB, playerID int64, grid *tilemap.TileGrid, events []agent.InputEvent) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { tx.Rollback() })

	w := storage.NewTxWorldWriter(tx)
	r := storage.NewTxWorldReader(tx)
	h := game.NewPlayerInputHandler(playerID, grid)

	if err := h.Handle(events, w, r); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func readPosition(t *testing.T, db *sql.DB, entityID int64) (x, y float64) {
	t.Helper()
	if err := db.QueryRow("SELECT x, y FROM comp_position WHERE entity_id = ?", entityID).Scan(&x, &y); err != nil {
		t.Fatalf("read position: %v", err)
	}
	return
}

func readSprite(t *testing.T, db *sql.DB, entityID int64) (animation string, flipX int) {
	t.Helper()
	if err := db.QueryRow("SELECT animation, flip_x FROM comp_sprite WHERE entity_id = ?", entityID).Scan(&animation, &flipX); err != nil {
		t.Fatalf("read sprite: %v", err)
	}
	return
}

func TestPlayerInputHandler_NoEvents(t *testing.T) {
	db, playerID := setupHandlerDB(t)
	grid := smallGrid()

	runHandler(t, db, playerID, grid, nil)

	x, y := readPosition(t, db, playerID)
	if x != 2 || y != 2 {
		t.Errorf("position = (%.0f,%.0f), want (2,2)", x, y)
	}
	anim, _ := readSprite(t, db, playerID)
	if anim != "player_idle" {
		t.Errorf("animation = %q, want player_idle", anim)
	}
}

func TestPlayerInputHandler_MoveRight_Passable(t *testing.T) {
	db, playerID := setupHandlerDB(t)
	grid := smallGrid() // (3,2) is passable

	events := []agent.InputEvent{
		{ID: 1, Kind: "key_held", Payload: `{"key":"ArrowRight"}`},
	}
	runHandler(t, db, playerID, grid, events)

	x, y := readPosition(t, db, playerID)
	if x != 3 || y != 2 {
		t.Errorf("position = (%.0f,%.0f), want (3,2)", x, y)
	}
	anim, flip := readSprite(t, db, playerID)
	if anim != "player_walk" {
		t.Errorf("animation = %q, want player_walk", anim)
	}
	if flip != 0 {
		t.Errorf("flip_x = %d, want 0 (moving right)", flip)
	}
}

func TestPlayerInputHandler_MoveRight_Wall(t *testing.T) {
	db, playerID := setupHandlerDB(t)
	// Grid where (3,2) is NOT passable — player starts at (2,2), wall to the right.
	grid := tilemap.NewTileGrid(5, 5)
	grid.SetPassable(2, 2, true)
	// (3,2) remains false

	events := []agent.InputEvent{
		{ID: 1, Kind: "key_held", Payload: `{"key":"ArrowRight"}`},
	}
	runHandler(t, db, playerID, grid, events)

	x, y := readPosition(t, db, playerID)
	if x != 2 || y != 2 {
		t.Errorf("position = (%.0f,%.0f), want (2,2) — wall should block", x, y)
	}
	anim, _ := readSprite(t, db, playerID)
	if anim != "player_idle" {
		t.Errorf("animation = %q, want player_idle (blocked by wall)", anim)
	}
}
