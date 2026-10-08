//go:build ebitengine

package main

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFindEntityOfTypeInMap_PrefersActiveMapOverOlderAndRuntimeEntities(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT NOT NULL)`,
		`CREATE TABLE spawns (map TEXT NOT NULL, object_id INTEGER NOT NULL, entity_id INTEGER)`,
		`CREATE TABLE comp_tilelayer (entity_id INTEGER PRIMARY KEY, map_id TEXT NOT NULL)`,
		`CREATE TABLE comp_tileentityowner (entity_id INTEGER PRIMARY KEY, target_entity_id INTEGER NOT NULL)`,
		`CREATE TABLE comp_tilereferences (entity_id INTEGER PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO entities VALUES (1,'Player'),(2,'Player'),(3,'Player'),(4,'Tile'),(5,'Player')`,
		`INSERT INTO spawns VALUES ('map-a',1,1),('map-b',1,2)`,
		`INSERT INTO comp_tilelayer VALUES (4,'map-a')`,
		`INSERT INTO comp_tileentityowner VALUES (5,4)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	id, err := findEntityOfTypeInMap(context.Background(), db, "Player", "map-b")
	if err != nil || id != 2 {
		t.Fatalf("active map Player = %d,%v, want id 2", id, err)
	}
	id, err = findEntityOfTypeInMap(context.Background(), db, "Player", "map-c")
	if err != nil || id != 3 {
		t.Fatalf("map without authored Player should use runtime Player 3: %d,%v", id, err)
	}
	if _, err := db.Exec(`INSERT INTO comp_tilereferences VALUES (4,'[3]')`); err != nil {
		t.Fatal(err)
	}
	id, err = findEntityOfTypeInMap(context.Background(), db, "Player", "map-c")
	if err != nil || id != 0 {
		t.Fatalf("foreign Tile-linked Player must not be runtime fallback: %d,%v", id, err)
	}
	if _, err := db.Exec(`DELETE FROM spawns WHERE map='map-a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM comp_tileentityowner WHERE entity_id=5`); err != nil {
		t.Fatal(err)
	}
	id, err = findEntityOfTypeInMap(context.Background(), db, "Player", "map-a")
	if err != nil || id != 3 {
		t.Fatalf("map-a should select its linked Player over global runtime: %d,%v", id, err)
	}
}
