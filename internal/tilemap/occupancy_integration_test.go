package tilemap

import (
	"context"
	"database/sql"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	_ "modernc.org/sqlite"
)

func TestReadSpace_RebuildsFootprintsAndCapabilitiesFromCurrentDatabaseState(t *testing.T) {
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
		`CREATE TABLE comp_flying (entity_id INTEGER PRIMARY KEY, value INTEGER NOT NULL)`,
		`CREATE TABLE comp_walking (entity_id INTEGER PRIMARY KEY, value INTEGER NOT NULL)`,
		`CREATE TABLE comp_phased (entity_id INTEGER PRIMARY KEY, value INTEGER NOT NULL)`,
		`CREATE TABLE comp_burrowing (entity_id INTEGER PRIMARY KEY, value INTEGER NOT NULL)`,
		`CREATE TABLE comp_swimming (entity_id INTEGER PRIMARY KEY, value INTEGER NOT NULL)`,
		`CREATE TABLE comp_nightvision (entity_id INTEGER PRIMARY KEY, value INTEGER NOT NULL)`,
		`INSERT INTO entities VALUES (1,'Player'),(2,'Wall'),(3,'River')`,
		`INSERT INTO comp_position VALUES (1,0,0),(2,1,0),(3,2,0)`,
		`INSERT INTO comp_passability VALUES (2,'solid'),(3,'liquid')`,
		`INSERT INTO comp_visibility VALUES (2,'opaque')`,
		`INSERT INTO comp_occupiedcells VALUES (3,'[{"x":0,"y":0},{"x":1,"y":1}]')`,
		`INSERT INTO comp_flying VALUES (1,1)`,
		`INSERT INTO comp_walking VALUES (1,1)`,
		`INSERT INTO comp_nightvision VALUES (1,0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}
	read := func() Space {
		t.Helper()
		policy := exampleInteractions()
		policy.Components["Walking"] = schema.Component{Type: "boolean"}
		s, err := ReadSpace(context.Background(), db, policy, 5, 3)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := read()
	if len(s.Entities) != 3 || !s.Entities[0].Capabilities["Flying"] || !s.Entities[0].Capabilities["Walking"] || s.Entities[0].Capabilities["NightVision"] {
		t.Fatalf("entities/capabilities = %+v, want three with Flying and Walking active, NightVision inactive", s.Entities)
	}
	if ok, err := s.CanEnter(SpatialEntity{ID: 1}, Point{3, 1}); err != nil || ok {
		t.Fatalf("river's offset does not restrict (3,1): %v,%v", ok, err)
	}
	if _, err := db.Exec(`UPDATE comp_flying SET value=0 WHERE entity_id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO comp_burrowing VALUES (1,1)`); err != nil {
		t.Fatal(err)
	}
	s = read()
	mover, found := s.Entity(1)
	if !found {
		t.Fatal("player missing from live spatial snapshot")
	}
	if ok, err := s.CanEnter(mover, Point{1, 0}); err != nil || !ok || mover.Capabilities["Flying"] {
		t.Fatalf("mod-defined Burrowing did not cross wall independently of Flying: %v,%v, %+v", ok, err, mover.Capabilities)
	}
	if _, err := db.Exec(`UPDATE comp_position SET x = 0 WHERE entity_id = 3`); err != nil {
		t.Fatal(err)
	}
	s = read()
	if ok, err := s.CanEnter(SpatialEntity{ID: 1}, Point{3, 1}); err != nil || !ok {
		t.Fatalf("old river cells still restrict after move: %v,%v", ok, err)
	}
	if ok, err := s.CanEnter(SpatialEntity{ID: 1}, Point{1, 1}); err != nil || ok {
		t.Fatalf("new river cells not restricted: %v,%v", ok, err)
	}
	if _, err := db.Exec(`DELETE FROM entities WHERE id = 3`); err != nil {
		t.Fatal(err)
	}
	s = read()
	if len(s.Entities) != 2 {
		t.Fatalf("deleted river still indexed: %+v", s.Entities)
	}
	if _, err := db.Exec(`UPDATE comp_passability SET kind='' WHERE entity_id=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE comp_visibility SET kind='' WHERE entity_id=2`); err != nil {
		t.Fatal(err)
	}
	s = read()
	if can, err := s.CanEnter(SpatialEntity{ID: 99}, Point{1, 0}); err == nil || can {
		t.Fatalf("stored empty passability silently became open: %v,%v", can, err)
	}
	observer, _ := s.Entity(1)
	if seen, err := s.CanSee(observer, Point{2, 0}, 3); err == nil || seen {
		t.Fatalf("stored empty Visibility silently became transparent: %v,%v", seen, err)
	}
}

func TestReadSpace_SeesMovementWithinUncommittedTickTransaction(t *testing.T) {
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
		`INSERT INTO entities VALUES (7, 'Wall')`,
		`INSERT INTO comp_position VALUES (7,1,0)`,
		`INSERT INTO comp_passability VALUES (7,'solid')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	policy := exampleInteractions()
	// No capability tables are needed for this isolated restriction rule.
	policy.Components = map[string]schema.Component{
		"Passability": {Type: "object"}, "Visibility": {Type: "object"},
		"OccupiedCells": {Type: "array"},
	}
	policy.Interactions = map[string]map[string]schema.InteractionRule{
		"Passability": {"solid": {}}, "Visibility": {"opaque": {}},
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE comp_position SET x = 2 WHERE entity_id = 7`); err != nil {
		t.Fatal(err)
	}
	space, err := ReadSpace(context.Background(), tx, policy, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	if space.Entities[0].Position != (Point{2, 0}) {
		t.Fatalf("snapshot did not see uncommitted Position: %+v", space.Entities)
	}
}

func TestReadSpace_SchemasWithoutRestrictionOrFootprintComponentsRemainUsable(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT NOT NULL)`,
		`CREATE TABLE comp_position (entity_id INTEGER PRIMARY KEY, x REAL NOT NULL, y REAL NOT NULL)`,
		`INSERT INTO entities VALUES (1,'Player')`,
		`INSERT INTO comp_position VALUES (1,0,0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	s, err := ReadSpace(context.Background(), db, schema.DatabaseSchema{}, 3, 2)
	if err != nil {
		t.Fatalf("valid non-spatial schema cannot read map bounds: %v", err)
	}
	mover, ok := s.Entity(1)
	if !ok || mover.Cells != nil {
		t.Fatalf("unrestricted actor has no anchor-only footprint: %+v", mover)
	}
	if can, err := s.CanEnter(mover, Point{1, 0}); err != nil || !can {
		t.Fatalf("empty grid cell should be enterable without spatial components: %v,%v", can, err)
	}
}

func TestReadSpace_OnlyActiveMapsSpawnsAndUnownedRuntimeEntitiesAffectTraversal(t *testing.T) {
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
		`CREATE TABLE spawns (map TEXT, object_id INTEGER, entity_id INTEGER)`,
		`INSERT INTO entities VALUES (1,'Wall'),(2,'Wall'),(3,'RuntimeWall')`,
		`INSERT INTO comp_position VALUES (1,1,0),(2,2,0),(3,0,1)`,
		`INSERT INTO comp_passability VALUES (1,'solid'),(2,'solid'),(3,'solid')`,
		`INSERT INTO comp_occupiedcells VALUES (1,'[{"x":99,"y":0}]')`,
		`INSERT INTO spawns VALUES ('map-a',1,1),('map-b',2,2)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	rules := exampleInteractions()
	rules.Components = map[string]schema.Component{
		"Passability": {Type: "object"}, "Visibility": {Type: "object"}, "OccupiedCells": {Type: "array"},
	}
	rules.Interactions = map[string]map[string]schema.InteractionRule{"Passability": {"solid": {}}, "Visibility": {"opaque": {}}}
	s, err := ReadSpace(context.Background(), db, rules, 4, 2, "map-b")
	if err != nil {
		t.Fatalf("foreign map's out-of-bounds wall poisoned active map: %v", err)
	}
	if len(s.Entities) != 2 {
		t.Fatalf("active map snapshot = %+v, want map-b and runtime Wall only", s.Entities)
	}
	for _, tt := range []struct {
		at   Point
		want bool
	}{
		{Point{1, 0}, true}, {Point{2, 0}, false}, {Point{0, 1}, false},
	} {
		got, err := s.CanEnter(SpatialEntity{ID: 99}, tt.at)
		if err != nil || got != tt.want {
			t.Errorf("cell %v enterable=%v,%v; want %v", tt.at, got, err, tt.want)
		}
	}
}
