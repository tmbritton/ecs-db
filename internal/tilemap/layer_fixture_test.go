package tilemap

import (
	"context"
	"database/sql"
	"testing"

	"github.com/tmbritton/ecs-db/internal/world"
)

func syncFixture(t *testing.T) (*world.EntityService, *sql.DB) {
	t.Helper()
	ds := tileSchema()
	store := newTestStore(t)
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)
	return svc, store.DB()
}

type fixtureTile struct {
	ID   int64
	Type string
}

func tileRows(t *testing.T, db *sql.DB) map[Point]fixtureTile {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT entity_id, x, y, tile_type FROM comp_tile ORDER BY entity_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[Point]fixtureTile{}
	for rows.Next() {
		var id int64
		var x, y int
		var typ string
		if err := rows.Scan(&id, &x, &y, &typ); err != nil {
			t.Fatal(err)
		}
		out[Point{X: x, Y: y}] = fixtureTile{ID: id, Type: typ}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func watchTiles(t *testing.T, db *sql.DB) func() []string {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE tile_writes (op TEXT NOT NULL)`,
		`CREATE TRIGGER watch_tile_insert AFTER INSERT ON comp_tile BEGIN INSERT INTO tile_writes VALUES ('insert'); END`,
		`CREATE TRIGGER watch_tile_update AFTER UPDATE ON comp_tile BEGIN INSERT INTO tile_writes VALUES ('update'); END`,
		`CREATE TRIGGER watch_tile_delete AFTER DELETE ON comp_tile BEGIN INSERT INTO tile_writes VALUES ('delete'); END`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return func() []string {
		rows, err := db.Query(`SELECT op FROM tile_writes ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var ops []string
		for rows.Next() {
			var op string
			if err := rows.Scan(&op); err != nil {
				t.Fatal(err)
			}
			ops = append(ops, op)
		}
		return ops
	}
}
