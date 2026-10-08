package game_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/game"
	"github.com/tmbritton/ecs-db/internal/schema"
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
		`CREATE TABLE comp_passability (entity_id INTEGER PRIMARY KEY, kind TEXT NOT NULL)`,
		`CREATE TABLE comp_visibility (entity_id INTEGER PRIMARY KEY, kind TEXT NOT NULL)`,
		`CREATE TABLE comp_occupiedcells (entity_id INTEGER PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE comp_flying (entity_id INTEGER PRIMARY KEY, value INTEGER NOT NULL)`,
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

// The example policy lives in the test's schema. A different boolean
// capability can be substituted without editing the handler.
func smallGrid(db *sql.DB) *tilemap.TileGrid {
	rules := schema.DatabaseSchema{Components: map[string]schema.Component{
		"Passability": {Type: "object"}, "Visibility": {Type: "object"},
		"OccupiedCells": {Type: "array"}, "Flying": {Type: "boolean"},
	}, Interactions: map[string]map[string]schema.InteractionRule{
		"Passability": {"solid": {Allows: []string{"Flying"}}},
		"Visibility":  {"opaque": {}},
	}}
	return tilemap.NewTileGridForWorld(5, 5, db, rules)
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
	grid := smallGrid(db)

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
	grid := smallGrid(db) // (3,2) is in bounds with no blocker

	events := []agent.InputEvent{
		{ID: 1, Kind: "key_held", Payload: `{"key":"ArrowRight"}`},
	}
	runHandler(t, db, playerID, grid, events)

	x, y := readPosition(t, db, playerID)
	if x != 3 || y != 2 {
		t.Errorf("position = (%.0f,%.0f), want (3,2)", x, y)
	}
	anim, _ := readSprite(t, db, playerID)
	if anim != "player_walk_right" {
		t.Errorf("animation = %q, want player_walk_right", anim)
	}
}

func TestPlayerInputHandler_MoveRight_Wall(t *testing.T) {
	db, playerID := setupHandlerDB(t)
	grid := smallGrid(db)
	for _, stmt := range []string{
		`INSERT INTO entities (entity_type) VALUES ('Wall')`,
		`INSERT INTO comp_position (entity_id,x,y) VALUES (2,3,2)`,
		`INSERT INTO comp_passability (entity_id,kind) VALUES (2,'solid')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	events := []agent.InputEvent{
		{ID: 1, Kind: "key_held", Payload: `{"key":"ArrowRight"}`},
	}
	runHandler(t, db, playerID, grid, events)

	x, y := readPosition(t, db, playerID)
	if x != 2 || y != 2 {
		t.Errorf("position = (%.0f,%.0f), want (2,2) — wall should block", x, y)
	}
	anim, _ := readSprite(t, db, playerID)
	if anim != "player_walk_right" {
		t.Errorf("animation = %q, want player_walk_right (key held, movement blocked)", anim)
	}
}

func TestPlayerInputHandler_AbilityFromDatabaseOverridesWallRestriction(t *testing.T) {
	db, playerID := setupHandlerDB(t)
	for _, stmt := range []string{
		`INSERT INTO entities (entity_type) VALUES ('Wall')`,
		`INSERT INTO comp_position (entity_id,x,y) VALUES (2,3,2)`,
		`INSERT INTO comp_passability (entity_id,kind) VALUES (2,'solid')`,
		`INSERT INTO comp_flying (entity_id,value) VALUES (1,1)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	runHandler(t, db, playerID, smallGrid(db), []agent.InputEvent{{Kind: "key_held", Payload: `{"key":"ArrowRight"}`}})
	if x, y := readPosition(t, db, playerID); x != 3 || y != 2 {
		t.Fatalf("capable mover stayed at (%.0f,%.0f), want (3,2)", x, y)
	}
}
