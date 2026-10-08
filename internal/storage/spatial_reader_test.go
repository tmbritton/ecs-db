package storage

import (
	"context"
	"database/sql"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tilemap"
	_ "modernc.org/sqlite"
)

func TestNewTxWorldReader_SpatialSnapshotSeesUncommittedOccupantMove(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT NOT NULL)`,
		`CREATE TABLE comp_position (entity_id INTEGER PRIMARY KEY, x REAL NOT NULL, y REAL NOT NULL)`,
		`CREATE TABLE comp_passability (entity_id INTEGER PRIMARY KEY, kind TEXT NOT NULL)`,
		`CREATE TABLE comp_visibility (entity_id INTEGER PRIMARY KEY, kind TEXT NOT NULL)`,
		`CREATE TABLE comp_occupiedcells (entity_id INTEGER PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO entities VALUES (1,'Wall')`,
		`INSERT INTO comp_position VALUES (1,1,0)`,
		`INSERT INTO comp_passability VALUES (1,'solid')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE comp_position SET x=2 WHERE entity_id=1`); err != nil {
		t.Fatal(err)
	}
	reader := NewTxWorldReader(tx)
	snapshotter, ok := reader.(tilemap.SpaceQuerier)
	if !ok {
		t.Fatal("tick reader has no spatial snapshot port")
	}
	rules := schema.DatabaseSchema{Interactions: map[string]map[string]schema.InteractionRule{
		"Passability": {"solid": {}}, "Visibility": {"opaque": {}},
	}}
	s, err := tilemap.ReadSpace(context.Background(), snapshotter, rules, 4, 2)
	if err != nil || len(s.Entities) != 1 || s.Entities[0].Position != (tilemap.Point{X: 2, Y: 0}) {
		t.Fatalf("tick spatial snapshot = %+v, %v; want wall at (2,0)", s.Entities, err)
	}
}

func TestNewTxWorldReaderForMap_ResolvesPlayerFromActiveMap(t *testing.T) {
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
		`INSERT INTO entities VALUES (1,'Player'),(2,'Player'),(3,'Player')`,
		`INSERT INTO spawns VALUES ('map-a',1,1),('map-b',1,2)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	reader := NewTxWorldReaderForMap(tx, "map-b")
	id, err := reader.FindEntityByType("Player")
	if err != nil || id != 2 {
		t.Fatalf("active map player = %d,%v; want entity 2", id, err)
	}
	defaultReader := NewTxWorldReader(tx)
	id, err = defaultReader.FindEntityByType("Player")
	if err != nil || id != 1 {
		t.Fatalf("no-map reader changed its existing first-player rule: %d,%v", id, err)
	}
}

func TestNewTxWorldReaderForMap_ExcludesForeignTileOwnedFallback(t *testing.T) {
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
		`INSERT INTO entities VALUES (1,'Player'),(2,'Tile')`,
		`INSERT INTO comp_tilelayer VALUES (2,'map-a')`,
		`INSERT INTO comp_tileentityowner VALUES (1,2)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if id, err := NewTxWorldReaderForMap(tx, "map-b").FindEntityByType("Player"); err == nil {
		t.Fatalf("foreign Tile-owned Player %d selected as runtime fallback", id)
	}
}

func TestNewTxWorldReaderForMap_RuntimeTileLinkScopesTypeLookup(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT NOT NULL)`,
		`CREATE TABLE spawns (map TEXT, object_id INTEGER, entity_id INTEGER)`,
		`CREATE TABLE comp_tilelayer (entity_id INTEGER PRIMARY KEY, map_id TEXT NOT NULL)`,
		`CREATE TABLE comp_tileentityowner (entity_id INTEGER PRIMARY KEY, target_entity_id INTEGER NOT NULL)`,
		`CREATE TABLE comp_tilereferences (entity_id INTEGER PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO entities VALUES (2,'Player'),(3,'Tile')`,
		`INSERT INTO comp_tilelayer VALUES (3,'map-a')`,
		`INSERT INTO comp_tilereferences VALUES (3,'[2]')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if id, err := NewTxWorldReaderForMap(tx, "map-b").FindEntityByType("Player"); err == nil {
		t.Fatalf("map-b selected map-a linked runtime Player %d", id)
	}
	if id, err := NewTxWorldReaderForMap(tx, "map-a").FindEntityByType("Player"); err != nil || id != 2 {
		t.Fatalf("map-a cannot resolve its linked runtime Player: %d, %v", id, err)
	}
	if _, err := tx.Exec(`INSERT INTO entities VALUES (1,'Player')`); err != nil {
		t.Fatal(err)
	}
	if id, err := NewTxWorldReaderForMap(tx, "map-b").FindEntityByType("Player"); err != nil || id != 1 {
		t.Fatalf("map-b cannot resolve its unlinked runtime Player: %d, %v", id, err)
	}
	if id, err := NewTxWorldReaderForMap(tx, "map-a").FindEntityByType("Player"); err != nil || id != 2 {
		t.Fatalf("map-a preferred older global Player over its linked Player: %d, %v", id, err)
	}
}
