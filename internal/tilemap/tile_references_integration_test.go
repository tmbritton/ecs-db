package tilemap

import (
	"context"
	"database/sql"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	_ "modernc.org/sqlite"
)

func TestReadSpace_TileReferencesPlaceSharedUnpositionedOccupants(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT NOT NULL)`,
		`CREATE TABLE comp_position (entity_id INTEGER PRIMARY KEY, x REAL NOT NULL, y REAL NOT NULL)`,
		`CREATE TABLE comp_tilelayer (entity_id INTEGER PRIMARY KEY, map_id TEXT NOT NULL,
			layer_id INTEGER NOT NULL, layer_order INTEGER NOT NULL, draw_order INTEGER NOT NULL,
			cell_w INTEGER NOT NULL, cell_h INTEGER NOT NULL, visible INTEGER NOT NULL, opacity REAL NOT NULL)`,
		`CREATE TABLE comp_tilereferences (entity_id INTEGER PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE comp_tilevisual (entity_id INTEGER PRIMARY KEY, image TEXT, source_x INTEGER,
			source_y INTEGER, source_w INTEGER, source_h INTEGER, dest_x INTEGER, dest_y INTEGER,
			flip_h INTEGER, flip_v INTEGER, flip_d INTEGER, alpha REAL, visible INTEGER)`,
		`CREATE TABLE comp_passability (entity_id INTEGER PRIMARY KEY, kind TEXT NOT NULL)`,
		`CREATE TABLE comp_visibility (entity_id INTEGER PRIMARY KEY, kind TEXT NOT NULL)`,
		`CREATE TABLE comp_occupiedcells (entity_id INTEGER PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE spawns (map TEXT NOT NULL, object_id INTEGER NOT NULL, entity_id INTEGER)`,
		`INSERT INTO entities VALUES (1,'Player'),(10,'Wall'),(20,'River'),(100,'Tile'),(101,'Tile'),(102,'Tile')`,
		`INSERT INTO comp_position VALUES (1,0,0),(100,1,0),(101,2,0),(102,1,1)`,
		`INSERT INTO comp_tilelayer VALUES (100,'map',1,0,0,1,1,1,1),(101,'map',1,0,1,1,1,1,1),(102,'map',1,0,2,1,1,1,1)`,
		`INSERT INTO comp_tilereferences VALUES (100,'[10,20]'),(101,'[20]'),(102,'[20]')`,
		`INSERT INTO comp_tilevisual VALUES (20,'river.png',0,0,1,1,0,0,0,0,0,1,1)`,
		`INSERT INTO comp_passability VALUES (10,'solid'),(20,'liquid')`,
		`INSERT INTO comp_visibility VALUES (10,'opaque')`,
		`INSERT INTO spawns VALUES ('map',1,1),('map',2,10),('map',3,20)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	rules := exampleInteractions()
	rules.Components = map[string]schema.Component{
		"Passability": {Type: "object"}, "Visibility": {Type: "object"},
		"OccupiedCells": {Type: "array"}, "TileReferences": {Type: "array"},
		"TileLayer": {Type: "object"},
	}
	rules.Interactions = map[string]map[string]schema.InteractionRule{
		"Passability": {"solid": {}, "liquid": {}}, "Visibility": {"opaque": {}},
	}
	s, err := ReadSpace(context.Background(), db, rules, 4, 2, "map")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Entities) != 6 {
		t.Fatalf("tile + referenced occupants count = %d, want 6", len(s.Entities))
	}
	draws, err := ReadTileDraws(context.Background(), db, "map")
	if err != nil || len(draws) != 3 || draws[0].DX != 1 || draws[1].DX != 2 || draws[2].DX != 1 || draws[2].DY != 1 {
		t.Fatalf("one River visual across three referencing Tiles = %+v,%v", draws, err)
	}
	for _, tt := range []struct {
		at   Point
		want bool
	}{
		{Point{1, 0}, false}, {Point{2, 0}, false}, {Point{1, 1}, false}, {Point{3, 0}, true},
	} {
		allowed, err := s.CanEnter(SpatialEntity{ID: 99}, tt.at)
		if err != nil || allowed != tt.want {
			t.Errorf("at %v allowed=%v,%v; want %v", tt.at, allowed, err, tt.want)
		}
	}
	if seen, err := s.CanSee(SpatialEntity{ID: 1}, Point{3, 0}, 999); err != nil || seen {
		t.Fatalf("sight did not stop at referenced Wall Tile: %v,%v", seen, err)
	}
	if _, err := db.Exec(`UPDATE comp_position SET x=3 WHERE entity_id=101`); err != nil {
		t.Fatal(err)
	}
	s, err = ReadSpace(context.Background(), db, rules, 4, 2, "map")
	if err != nil {
		t.Fatal(err)
	}
	if can, err := s.CanEnter(SpatialEntity{ID: 99}, Point{2, 0}); err != nil || !can {
		t.Fatalf("shared River remained at moved Tile's old cell: %v,%v", can, err)
	}
	draws, err = ReadTileDraws(context.Background(), db, "map")
	if err != nil || len(draws) != 3 || draws[1].DX != 3 {
		t.Fatalf("moving Tile did not move shared River artwork: %+v,%v", draws, err)
	}
}

func TestReadSpace_RuntimeEntityLinkedToOneMapDoesNotBlockAnother(t *testing.T) {
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
		`CREATE TABLE comp_tilelayer (entity_id INTEGER PRIMARY KEY, map_id TEXT NOT NULL,
			layer_order INTEGER, draw_order INTEGER, cell_w INTEGER, cell_h INTEGER, opacity REAL, visible INTEGER)`,
		`CREATE TABLE comp_tilereferences (entity_id INTEGER PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE comp_tileentityowner (entity_id INTEGER PRIMARY KEY, target_entity_id INTEGER NOT NULL)`,
		`CREATE TABLE comp_tilevisual (entity_id INTEGER PRIMARY KEY, image TEXT, source_x INTEGER,
			source_y INTEGER, source_w INTEGER, source_h INTEGER, dest_x INTEGER, dest_y INTEGER,
			flip_h INTEGER, flip_v INTEGER, flip_d INTEGER, alpha REAL, visible INTEGER)`,
		`CREATE TABLE spawns (map TEXT, object_id INTEGER, entity_id INTEGER)`,
		`INSERT INTO entities VALUES (1,'Tile'),(2,'RuntimeWall')`,
		`INSERT INTO comp_position VALUES (1,2,0),(2,0,0)`,
		`INSERT INTO comp_passability VALUES (2,'solid')`,
		`INSERT INTO comp_tilelayer VALUES (1,'map-a',0,0,1,1,1,1)`,
		`INSERT INTO comp_tilereferences VALUES (1,'[2]')`,
		`INSERT INTO comp_tilevisual VALUES (2,'wall.png',0,0,1,1,0,0,0,0,0,1,1)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	rules := schema.DatabaseSchema{Components: map[string]schema.Component{
		"Passability": {Type: "object"}, "TileLayer": {Type: "object"},
		"TileReferences": {Type: "array"}, "TileEntityOwner": {Type: "entity-ref"},
	}, Interactions: map[string]map[string]schema.InteractionRule{
		"Passability": {"solid": {}},
	}}
	for _, tt := range []struct {
		mapID       string
		blockedCell Point
		openCell    Point
	}{
		{"map-a", Point{2, 0}, Point{0, 0}},
		{"map-b", Point{-1, -1}, Point{0, 0}},
	} {
		space, err := ReadSpace(context.Background(), db, rules, 3, 1, tt.mapID)
		if err != nil {
			t.Fatal(err)
		}
		if tt.blockedCell.X >= 0 {
			if can, err := space.CanEnter(SpatialEntity{ID: 99}, tt.blockedCell); err != nil || can {
				t.Errorf("%s linked wall does not block %v: %v, %v", tt.mapID, tt.blockedCell, can, err)
			}
		}
		if can, err := space.CanEnter(SpatialEntity{ID: 99}, tt.openCell); err != nil || !can {
			t.Errorf("%s runtime wall leaked to %v: %v, %v", tt.mapID, tt.openCell, can, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO spawns VALUES ('map-b',1,2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSpace(context.Background(), db, rules, 3, 1, "map-a"); err == nil {
		t.Fatal("foreign-map reference unexpectedly became a local restriction")
	}
	space, err := ReadSpace(context.Background(), db, rules, 3, 1, "map-b")
	if err != nil {
		t.Fatal(err)
	}
	if wall, found := space.Entity(2); !found || wall.Position != (Point{0, 0}) {
		t.Fatalf("map-b spawn vanished because map-a references it: %+v, %v", wall, found)
	}
	if draws, err := ReadTileDraws(context.Background(), db, "map-a"); err != nil || len(draws) != 0 {
		t.Fatalf("foreign-map referenced artwork drawn %d times: %v", len(draws), err)
	}
	for _, stmt := range []string{
		`DELETE FROM spawns`,
		`INSERT INTO entities VALUES (3,'Tile')`,
		`INSERT INTO comp_position VALUES (3,1,0)`,
		`INSERT INTO comp_tilelayer VALUES (3,'map-b',0,0,1,1,1,1)`,
		`INSERT INTO comp_tilereferences VALUES (3,'[]')`,
		`INSERT INTO comp_tileentityowner VALUES (2,3)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ReadSpace(context.Background(), db, rules, 3, 1, "map-a"); err == nil {
		t.Fatal("foreign-map owned entity unexpectedly became a local restriction")
	}
	if draws, err := ReadTileDraws(context.Background(), db, "map-a"); err != nil || len(draws) != 0 {
		t.Fatalf("foreign-map owned artwork drawn %d times: %v", len(draws), err)
	}
}
