package tilemap

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestNewTileGrid_Dimensions(t *testing.T) {
	g := NewTileGrid(5, 3)
	if g.Width != 5 || g.Height != 3 {
		t.Errorf("got %d×%d, want 5×3", g.Width, g.Height)
	}
}

func TestIsPassable_DefaultsFalse(t *testing.T) {
	g := NewTileGrid(3, 3)
	if g.IsPassable(1, 1) {
		t.Error("new grid cell should be impassable by default")
	}
}

func TestSetPassable_UpdatesCell(t *testing.T) {
	g := NewTileGrid(3, 3)
	g.SetPassable(1, 1, true)
	if !g.IsPassable(1, 1) {
		t.Error("cell should be passable after SetPassable(true)")
	}
}

func TestIsPassable_OutOfBounds(t *testing.T) {
	g := NewTileGrid(3, 3)
	g.SetPassable(0, 0, true)
	cases := [][2]int{{-1, 0}, {0, -1}, {3, 0}, {0, 3}, {-1, -1}}
	for _, c := range cases {
		if g.IsPassable(c[0], c[1]) {
			t.Errorf("IsPassable(%d,%d) = true, want false (out of bounds)", c[0], c[1])
		}
	}
}

func TestSetPassable_OutOfBounds_NoOp(t *testing.T) {
	g := NewTileGrid(3, 3)
	g.SetPassable(-1, 0, true)
	g.SetPassable(0, 3, true)
}

func newGridDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/grid.sqlite")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY AUTOINCREMENT, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_tile (entity_id INTEGER PRIMARY KEY, x INTEGER NOT NULL, y INTEGER NOT NULL, passable INTEGER NOT NULL, tile_type TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return db
}

func seedTile(t *testing.T, db *sql.DB, x, y int, passable bool) {
	t.Helper()
	res, err := db.Exec(`INSERT INTO entities (entity_type, created_tick) VALUES ('Tile', 0)`)
	if err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	id, _ := res.LastInsertId()
	p := 0
	if passable {
		p = 1
	}
	if _, err := db.Exec(`INSERT INTO comp_tile (entity_id, x, y, passable, tile_type) VALUES (?, ?, ?, ?, ?)`,
		id, x, y, p, "floor"); err != nil {
		t.Fatalf("insert comp_tile: %v", err)
	}
}

func TestTileGrid_Rebuild_PopulatesPassable(t *testing.T) {
	db := newGridDB(t)
	seedTile(t, db, 1, 0, true)
	seedTile(t, db, 2, 0, false)

	g := NewTileGrid(4, 2)
	if err := g.Rebuild(context.Background(), db); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if !g.IsPassable(1, 0) {
		t.Error("(1,0) should be passable")
	}
	if g.IsPassable(2, 0) {
		t.Error("(2,0) should be impassable")
	}
	if g.IsPassable(0, 0) {
		t.Error("unseeded (0,0) should be impassable")
	}
}

func TestTileGrid_Rebuild_ResetsStaleState(t *testing.T) {
	db := newGridDB(t)
	seedTile(t, db, 0, 0, true)

	g := NewTileGrid(3, 3)
	g.SetPassable(1, 1, true)

	if err := g.Rebuild(context.Background(), db); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if g.IsPassable(1, 1) {
		t.Error("Rebuild should have reset (1,1) which has no DB row")
	}
	if !g.IsPassable(0, 0) {
		t.Error("(0,0) should be passable after Rebuild")
	}
}
