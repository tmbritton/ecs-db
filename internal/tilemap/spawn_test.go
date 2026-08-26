package tilemap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/world"
)

// spawnSchema has a type with three required components, like the real one, so
// a spawn that names only its position is refused the way a real one would be.
func spawnSchema() schema.DatabaseSchema {
	s := tileSchema()
	s.Components["Position"] = schema.Component{Type: "object", Properties: map[string]schema.Property{
		"x": {Type: "integer"}, "y": {Type: "integer"},
	}}
	s.Components["Health"] = schema.Component{Type: "object", Properties: map[string]schema.Property{
		"hp": {Type: "integer"}, "maxHp": {Type: "integer"},
	}}
	s.Components["Sprite"] = schema.Component{Type: "object", Properties: map[string]schema.Property{
		"sheet": {Type: "string"}, "animation": {Type: "string"}, "flip_x": {Type: "boolean"},
	}}
	s.Components["Speed"] = schema.Component{Type: "object", Properties: map[string]schema.Property{
		"value": {Type: "number"},
	}}
	s.EntityTypes["Goblin"] = schema.EntityType{
		RequiredComponents: []string{"Position", "Health", "Sprite"},
		OptionalComponents: []string{"Speed"},
		ValidationLevel:    "strict",
	}
	return s
}

func spawnFixture(t *testing.T) (*world.EntityService, *sql.DB) {
	t.Helper()
	ds := spawnSchema()
	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.sqlite", ds, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)
	return svc, store.DB()
}

// goblinProps is what a spawn has to carry for a type requiring three
// components: Position comes from the object's own coordinates, the rest are
// addressed as Component.property.
func goblinProps(extra ...[2]string) tiled.Properties {
	p := tiled.Properties{
		"Health.hp":        {Type: "int", Value: "5"},
		"Health.maxHp":     {Type: "int", Value: "5"},
		"Sprite.sheet":     {Type: "string", Value: ""},
		"Sprite.animation": {Type: "string", Value: "goblin_idle"},
		"Sprite.flip_x":    {Type: "bool", Value: "false"},
	}
	for _, e := range extra {
		p[e[0]] = tiled.Property{Value: e[1]}
	}
	return p
}

func spawnMap(objects ...tiled.Object) *tiled.Map {
	return &tiled.Map{
		Name: "level.tmx", Width: 20, Height: 20, TileWidth: 16, TileHeight: 16,
		Orientation:  "orthogonal",
		ObjectGroups: []tiled.ObjectGroup{{ID: 1, Name: "spawns", Visible: true, Objects: objects}},
	}
}

func goblinAt(id int, px, py float64) tiled.Object {
	return tiled.Object{ID: id, Name: "gob", Type: "Goblin", X: px, Y: py, Visible: true, Properties: goblinProps()}
}

func mustSpawn(t *testing.T, svc *world.EntityService, db *sql.DB, m *tiled.Map) SpawnResult {
	t.Helper()
	res, err := SyncSpawns(context.Background(), svc, db, "mods/map/level.tmx", m)
	if err != nil {
		t.Fatalf("SyncSpawns: %v", err)
	}
	return res
}

func entityRows(t *testing.T, db *sql.DB) []struct {
	ID   int64
	Type string
	X, Y int
} {
	t.Helper()
	rows, err := db.Query(`SELECT entities.id, entities.entity_type, comp_position.x, comp_position.y
		FROM entities JOIN comp_position ON entities.id = comp_position.entity_id
		ORDER BY entities.id`)
	if err != nil {
		t.Fatalf("querying entities: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []struct {
		ID   int64
		Type string
		X, Y int
	}
	for rows.Next() {
		var r struct {
			ID   int64
			Type string
			X, Y int
		}
		if err := rows.Scan(&r.ID, &r.Type, &r.X, &r.Y); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		out = append(out, r)
	}
	return out
}

// An object becomes an entity of the type it names, at the cell it sits in.
func TestSyncSpawns_CreatesAnEntityPerObject(t *testing.T) {
	svc, db := spawnFixture(t)

	res := mustSpawn(t, svc, db, spawnMap(goblinAt(1, 32, 48), goblinAt(2, 0, 0)))

	if res.Created != 2 || len(res.Refused) != 0 {
		t.Fatalf("result = %+v, want two creates and no refusals", res)
	}
	rows := entityRows(t, db)
	if len(rows) != 2 {
		t.Fatalf("stored %d entities, want 2", len(rows))
	}
	if rows[0].Type != "Goblin" {
		t.Errorf("entity type = %q, want Goblin", rows[0].Type)
	}
	// 32px and 48px in a 16px grid.
	if rows[0].X != 2 || rows[0].Y != 3 {
		t.Errorf("the first goblin is at (%d,%d), want (2,3)", rows[0].X, rows[0].Y)
	}
}

// A tile object is positioned by its bottom-left corner, so its row is one above
// the one the pixel coordinate divides into. Getting this wrong puts every tile
// spawn one row down, which reads as an off-by-one in the map.
func TestSyncSpawns_ATileObjectIsPositionedByItsBottomEdge(t *testing.T) {
	svc, db := spawnFixture(t)

	tileObj := goblinAt(1, 32, 64)
	tileObj.GID = 5 // any non-zero gid makes it a tile object
	rect := goblinAt(2, 32, 64)

	mustSpawn(t, svc, db, spawnMap(tileObj, rect))

	rows := entityRows(t, db)
	if len(rows) != 2 {
		t.Fatalf("stored %d entities, want 2", len(rows))
	}
	if rows[0].Y != 3 {
		t.Errorf("the tile object is at row %d, want 3 — its y is its bottom edge", rows[0].Y)
	}
	if rows[1].Y != 4 {
		t.Errorf("the rectangle is at row %d, want 4 — its y is its top edge", rows[1].Y)
	}
}

// Custom properties become component values, converted against the schema's
// declared types rather than Tiled's.
func TestSyncSpawns_PropertiesBecomeComponentValues(t *testing.T) {
	svc, db := spawnFixture(t)

	obj := goblinAt(1, 0, 0)
	obj.Properties["Speed.value"] = tiled.Property{Type: "float", Value: "2.5"}
	mustSpawn(t, svc, db, spawnMap(obj))

	var hp, maxHp int
	if err := db.QueryRow(`SELECT hp, maxHp FROM comp_health`).Scan(&hp, &maxHp); err != nil {
		t.Fatalf("reading health: %v", err)
	}
	if hp != 5 || maxHp != 5 {
		t.Errorf("health = %d/%d, want 5/5", hp, maxHp)
	}
	var anim string
	var flip int
	if err := db.QueryRow(`SELECT animation, flip_x FROM comp_sprite`).Scan(&anim, &flip); err != nil {
		t.Fatalf("reading sprite: %v", err)
	}
	if anim != "goblin_idle" || flip != 0 {
		t.Errorf("sprite = %q/%d, want goblin_idle/0", anim, flip)
	}
	var speed float64
	if err := db.QueryRow(`SELECT value FROM comp_speed`).Scan(&speed); err != nil {
		t.Fatalf("reading speed: %v", err)
	}
	if speed != 2.5 {
		t.Errorf("speed = %v, want 2.5", speed)
	}
}

// ── Refusals ─────────────────────────────────────────────────────────

// One bad spawn does not stop the others, and the refusal says which object.
func TestSyncSpawns_RefusesOneAndKeepsGoing(t *testing.T) {
	svc, db := spawnFixture(t)

	bad := goblinAt(1, 16, 16)
	bad.Type = "Wyvern"
	res := mustSpawn(t, svc, db, spawnMap(bad, goblinAt(2, 32, 32)))

	if res.Created != 1 {
		t.Errorf("created %d entities, want the one that is valid", res.Created)
	}
	if len(res.Refused) != 1 {
		t.Fatalf("refused %d objects, want 1: %v", len(res.Refused), res.Refused)
	}
	for _, want := range []string{"Wyvern", "1", "1,1"} {
		if !strings.Contains(res.Refused[0], want) {
			t.Errorf("the refusal does not mention %q: %q", want, res.Refused[0])
		}
	}
}

// A type whose required components the object does not carry is refused by the
// same rules that refuse it anywhere else.
func TestSyncSpawns_RefusesAnObjectMissingARequiredComponent(t *testing.T) {
	svc, db := spawnFixture(t)

	bare := tiled.Object{ID: 1, Type: "Goblin", X: 0, Y: 0, Visible: true}
	res := mustSpawn(t, svc, db, spawnMap(bare))

	if res.Created != 0 || len(res.Refused) != 1 {
		t.Fatalf("result = %+v, want one refusal", res)
	}
	if !strings.Contains(res.Refused[0], "Health") {
		t.Errorf("the refusal does not name the missing component: %q", res.Refused[0])
	}
}

// A property that names no component is refused rather than ignored: an author
// who writes "hp" meaning "Health.hp" would otherwise get silence, and silence
// is what this epic keeps having to go back and fix.
func TestSyncSpawns_RefusesAPropertyThatNamesNoComponent(t *testing.T) {
	svc, db := spawnFixture(t)

	obj := goblinAt(1, 0, 0)
	obj.Properties["hp"] = tiled.Property{Value: "9"}
	res := mustSpawn(t, svc, db, spawnMap(obj))

	if len(res.Refused) != 1 {
		t.Fatalf("result = %+v, want one refusal", res)
	}
	if !strings.Contains(res.Refused[0], "hp") {
		t.Errorf("the refusal does not name the property: %q", res.Refused[0])
	}
}

// A value the schema's type cannot hold is refused where it is read, naming the
// property — not passed down to fail as a database type error.
func TestSyncSpawns_RefusesAValueThatIsNotItsDeclaredType(t *testing.T) {
	svc, db := spawnFixture(t)

	obj := goblinAt(1, 0, 0)
	obj.Properties["Health.hp"] = tiled.Property{Value: "plenty"}
	res := mustSpawn(t, svc, db, spawnMap(obj))

	if len(res.Refused) != 1 {
		t.Fatalf("result = %+v, want one refusal", res)
	}
	if !strings.Contains(res.Refused[0], "Health.hp") || !strings.Contains(res.Refused[0], "plenty") {
		t.Errorf("the refusal does not say what could not be read: %q", res.Refused[0])
	}
}

// ── Identity ─────────────────────────────────────────────────────────

// Running twice does not spawn twice.
func TestSyncSpawns_DoesNotSpawnTheSameObjectTwice(t *testing.T) {
	svc, db := spawnFixture(t)
	m := spawnMap(goblinAt(1, 32, 48), goblinAt(2, 0, 0))

	first := mustSpawn(t, svc, db, m)
	second := mustSpawn(t, svc, db, m)

	if first.Created != 2 {
		t.Fatalf("the first run created %d, want 2", first.Created)
	}
	if second.Created != 0 {
		t.Errorf("the second run created %d entities, want none", second.Created)
	}
	if n := len(entityRows(t, db)); n != 2 {
		t.Errorf("%d entities exist after two runs, want 2", n)
	}
}

// Story 6 had three tests here asserting that a re-import never moves, never
// deletes and never recreates. Story 8 reversed that rule — the file wins, as it
// does for tiles — and their opposites are in the Story 8 block below.

// Two maps can hold objects with the same id, because Tiled numbers them per
// file.
func TestSyncSpawns_ObjectIDsAreScopedToTheirMap(t *testing.T) {
	svc, db := spawnFixture(t)
	m := spawnMap(goblinAt(1, 0, 0))

	mustSpawn(t, svc, db, m)
	res, err := SyncSpawns(context.Background(), svc, db, "mods/map/other.tmx", m)
	if err != nil {
		t.Fatalf("SyncSpawns: %v", err)
	}
	if res.Created != 1 {
		t.Errorf("a second map's object 1 created %d entities, want 1", res.Created)
	}
	if n := len(entityRows(t, db)); n != 2 {
		t.Errorf("%d entities exist, want 2", n)
	}
}

// An object layer nobody can see still spawns. Hiding a layer is what the editor
// shows you; the same argument the tile loader makes about Visible.
func TestSyncSpawns_AnInvisibleObjectLayerStillSpawns(t *testing.T) {
	svc, db := spawnFixture(t)
	m := spawnMap(goblinAt(1, 0, 0))
	m.ObjectGroups[0].Visible = false

	if res := mustSpawn(t, svc, db, m); res.Created != 1 {
		t.Errorf("an invisible object layer spawned %d entities, want 1", res.Created)
	}
}

// A map with no object layers is not a fault, and writes nothing.
func TestSyncSpawns_AMapWithNoObjectsWritesNothing(t *testing.T) {
	svc, db := spawnFixture(t)

	res := mustSpawn(t, svc, db, &tiled.Map{Name: "level.tmx", TileWidth: 16, TileHeight: 16})
	if res.Created != 0 || len(res.Refused) != 0 {
		t.Errorf("result = %+v, want nothing", res)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM spawns`).Scan(&n); err != nil {
		t.Fatalf("counting spawns: %v", err)
	}
	if n != 0 {
		t.Errorf("%d spawn rows were written for a map with no objects", n)
	}
}

// Refusals come out in file order, which is the order the author reads their own
// map in. The ids run 7 then 3, so file order and any sort of the text disagree
// — without that this test would pass for either.
func TestSyncSpawns_RefusalsAreInFileOrder(t *testing.T) {
	svc, db := spawnFixture(t)

	a := goblinAt(7, 0, 0)
	a.Type = "Wyvern"
	b := goblinAt(3, 16, 0)
	b.Type = "Dragon"
	res := mustSpawn(t, svc, db, spawnMap(a, b))

	if len(res.Refused) != 2 {
		t.Fatalf("refused %d, want 2: %v", len(res.Refused), res.Refused)
	}
	if !strings.Contains(res.Refused[0], "object 7") {
		t.Errorf("the first refusal is %q, want the first object in the file", res.Refused[0])
	}
	if !strings.Contains(res.Refused[1], "object 3") {
		t.Errorf("the second refusal is %q, want the second object in the file", res.Refused[1])
	}
}

// An object with two bad properties is refused for the same one every run,
// rather than whichever the map's property table happened to yield first.
func TestSyncSpawns_ARefusalDoesNotDependOnMapOrder(t *testing.T) {
	svc, db := spawnFixture(t)

	first := ""
	for range 20 {
		obj := goblinAt(1, 0, 0)
		obj.Properties["zzz"] = tiled.Property{Value: "1"}
		obj.Properties["aaa"] = tiled.Property{Value: "1"}
		res := mustSpawn(t, svc, db, spawnMap(obj))
		if len(res.Refused) != 1 {
			t.Fatalf("result = %+v, want one refusal", res)
		}
		if first == "" {
			first = res.Refused[0]
			continue
		}
		if res.Refused[0] != first {
			t.Fatalf("two runs refuse different properties:\n %s\n %s", first, res.Refused[0])
		}
	}
}

// ── The survivors of the mutation battery ────────────────────────────

// A property may name its component in any casing, and the value still lands in
// the component the schema declares — the map is written by hand and "health.hp"
// is what somebody types.
func TestSyncSpawns_AComponentNameIsMatchedWhateverItsCasing(t *testing.T) {
	svc, db := spawnFixture(t)

	obj := tiled.Object{ID: 1, Type: "Goblin", Visible: true, Properties: tiled.Properties{
		"health.hp":        {Value: "7"},
		"HEALTH.maxHp":     {Value: "9"},
		"Sprite.sheet":     {Value: ""},
		"sprite.animation": {Value: "goblin_idle"},
		"Sprite.flip_x":    {Value: "false"},
	}}
	res := mustSpawn(t, svc, db, spawnMap(obj))
	if res.Created != 1 {
		t.Fatalf("result = %+v, want one create", res)
	}

	var hp, maxHp int
	if err := db.QueryRow(`SELECT hp, maxHp FROM comp_health`).Scan(&hp, &maxHp); err != nil {
		t.Fatalf("reading health: %v", err)
	}
	if hp != 7 || maxHp != 9 {
		t.Errorf("health = %d/%d, want 7/9", hp, maxHp)
	}
	var anim string
	if err := db.QueryRow(`SELECT animation FROM comp_sprite`).Scan(&anim); err != nil {
		t.Fatalf("reading sprite: %v", err)
	}
	if anim != "goblin_idle" {
		t.Errorf("animation = %q, want goblin_idle", anim)
	}
}

// A number that is not a number is refused where it is read. SQLite would take
// "fast" into a REAL column without complaint and hand it back as text, so this
// is not a fault the database catches for us.
func TestSyncSpawns_RefusesANumberThatIsNotOne(t *testing.T) {
	svc, db := spawnFixture(t)

	obj := goblinAt(1, 0, 0)
	obj.Properties["Speed.value"] = tiled.Property{Value: "fast"}
	res := mustSpawn(t, svc, db, spawnMap(obj))

	if res.Created != 0 || len(res.Refused) != 1 {
		t.Fatalf("result = %+v, want one refusal", res)
	}
	if !strings.Contains(res.Refused[0], "Speed.value") || !strings.Contains(res.Refused[0], "fast") {
		t.Errorf("the refusal does not say what could not be read: %q", res.Refused[0])
	}
	if !strings.Contains(res.Refused[0], "not a number") {
		t.Errorf("the refusal does not say it wanted a number: %q", res.Refused[0])
	}
}

// A property naming a component the schema does not have says so, naming the
// component. Falling through to "which component %q does not declare" with an
// empty name is technically a refusal and tells nobody anything.
func TestSyncSpawns_RefusalNamesTheUndeclaredComponent(t *testing.T) {
	svc, db := spawnFixture(t)

	obj := goblinAt(1, 0, 0)
	obj.Properties["Wings.span"] = tiled.Property{Value: "3"}
	res := mustSpawn(t, svc, db, spawnMap(obj))

	if len(res.Refused) != 1 {
		t.Fatalf("result = %+v, want one refusal", res)
	}
	if !strings.Contains(res.Refused[0], `"Wings"`) {
		t.Errorf("the refusal does not name the component it could not find: %q", res.Refused[0])
	}
	if !strings.Contains(res.Refused[0], "does not declare") {
		t.Errorf("the refusal does not say the schema lacks it: %q", res.Refused[0])
	}
}

// Loading a map spawns its objects. Every test above calls SyncSpawns directly,
// so all of them passed with LoadMap wired to spawn nothing at all.
func TestLoadMap_SpawnsTheMapsObjects(t *testing.T) {
	ds := spawnSchema()
	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.sqlite", ds, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	defer func() { _ = store.Close() }()
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	const withSpawn = `<?xml version="1.0" encoding="UTF-8"?>
<map version="1.10" orientation="orthogonal" width="3" height="2" tilewidth="8" tileheight="8">
 <tileset firstgid="1" source="dungeon.tsx"/>
 <layer name="ground" width="3" height="2"><data encoding="csv">2,1,2,
2,2,2</data></layer>
 <objectgroup id="2" name="spawns">
  <object id="4" name="gob" type="Goblin" x="8" y="8">
   <properties>
    <property name="Health.hp" type="int" value="5"/>
    <property name="Health.maxHp" type="int" value="5"/>
    <property name="Sprite.sheet" value=""/>
    <property name="Sprite.animation" value="goblin_idle"/>
    <property name="Sprite.flip_x" type="bool" value="false"/>
   </properties>
  </object>
 </objectgroup>
</map>`

	path := tiledMap(t, twoTileTSX, withSpawn, "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err != nil {
		t.Fatalf("LoadMap: %v", err)
	}

	var entityType string
	var x, y int
	if err := store.DB().QueryRow(
		`SELECT entities.entity_type, comp_position.x, comp_position.y
		 FROM entities JOIN comp_position ON entities.id = comp_position.entity_id`).
		Scan(&entityType, &x, &y); err != nil {
		t.Fatalf("the map's object did not become an entity: %v", err)
	}
	if entityType != "Goblin" || x != 1 || y != 1 {
		t.Errorf("spawned a %s at (%d,%d), want a Goblin at (1,1)", entityType, x, y)
	}

	// And loading it again does not spawn a second one.
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err != nil {
		t.Fatalf("second LoadMap: %v", err)
	}
	var n int
	if err := store.DB().QueryRow(
		`SELECT COUNT(*) FROM entities WHERE entity_type = 'Goblin'`).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 1 {
		t.Errorf("%d goblins after loading the map twice, want 1", n)
	}
}

// ── The review's findings ────────────────────────────────────────────

// Two objects claiming one id is a bad map, not a broken database.
//
// The id defaults to zero when Tiled's attribute is absent, which a generated
// or hand-written file produces easily. The second insert used to collide on the
// primary key and abort the load — having already created an entity that no
// spawn row pointed at, which the *next* load duplicated and never mentioned
// again, because the object still looked unspawned.
func TestSyncSpawns_RefusesASecondObjectWithTheSameID(t *testing.T) {
	svc, db := spawnFixture(t)

	res, err := SyncSpawns(context.Background(), svc, db, "level.tmx",
		spawnMap(goblinAt(0, 0, 0), goblinAt(0, 16, 0)))
	if err != nil {
		t.Fatalf("a duplicate object id failed the whole load: %v", err)
	}
	if res.Created != 1 {
		t.Errorf("created %d entities, want 1", res.Created)
	}
	if n := len(entityRows(t, db)); n != 1 {
		t.Errorf("%d entities exist, want 1 — one was written with no record of it", n)
	}
	// And said out loud. Passing over the second object as "already spawned"
	// would be right about the outcome and silent about a broken map.
	if len(res.Refused) != 1 {
		t.Fatalf("refused %v, want the duplicate named", res.Refused)
	}
	if !strings.Contains(res.Refused[0], "belongs to another object") {
		t.Errorf("the refusal does not say the id is a duplicate: %q", res.Refused[0])
	}

	// And the next load of the *same* map does not find an unspawned object to
	// duplicate. The same path, not mustSpawn's — a different path is a
	// different map, which is what this test first got wrong.
	again, err := SyncSpawns(context.Background(), svc, db, "level.tmx",
		spawnMap(goblinAt(0, 0, 0), goblinAt(0, 16, 0)))
	if err != nil {
		t.Fatalf("SyncSpawns: %v", err)
	}
	if again.Created != 0 {
		t.Errorf("the second load created %d entities", again.Created)
	}
}

// The entity and the record of it commit together, so nothing that fails
// between them can leave an entity nobody knows about.
func TestSyncSpawns_TheSpawnRowPointsAtItsEntity(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))

	var entityID sql.NullInt64
	if err := db.QueryRow(`SELECT entity_id FROM spawns WHERE object_id = 1`).Scan(&entityID); err != nil {
		t.Fatalf("reading the spawn row: %v", err)
	}
	if !entityID.Valid {
		t.Fatal("the spawn records no entity — the column the whole SET NULL argument rests on is never written")
	}
	rows := entityRows(t, db)
	if len(rows) != 1 || rows[0].ID != entityID.Int64 {
		t.Errorf("the spawn points at entity %d, and the entity is %+v", entityID.Int64, rows)
	}
}

// A property may name its property in any casing too. Every fixture above uses
// the declared spelling, so the canonical-name fix this story made to
// PropertyByName was untested by all of them.
func TestSyncSpawns_APropertyNameIsMatchedWhateverItsCasing(t *testing.T) {
	svc, db := spawnFixture(t)

	obj := goblinAt(1, 0, 0)
	delete(obj.Properties, "Health.maxHp")
	obj.Properties["Health.MAXHP"] = tiled.Property{Value: "9"}
	res := mustSpawn(t, svc, db, spawnMap(obj))
	if res.Created != 1 {
		t.Fatalf("result = %+v, want one create", res)
	}

	var maxHp int
	if err := db.QueryRow(`SELECT maxHp FROM comp_health`).Scan(&maxHp); err != nil {
		t.Fatalf("reading health: %v", err)
	}
	if maxHp != 9 {
		t.Errorf("maxHp = %d, want 9", maxHp)
	}
}

// An object with no class is not a spawn. Region markers, camera bounds and
// notes are the ordinary contents of an object layer, and refusing each of them
// once per start forever is not a diagnostic, it is noise.
func TestSyncSpawns_AnObjectWithNoClassIsNotASpawn(t *testing.T) {
	svc, db := spawnFixture(t)

	marker := tiled.Object{ID: 3, Name: "trigger area", X: 0, Y: 0, Width: 64, Height: 64, Visible: true}
	res := mustSpawn(t, svc, db, spawnMap(marker, goblinAt(1, 0, 0)))

	if len(res.Refused) != 0 {
		t.Errorf("an object with no class was refused: %v", res.Refused)
	}
	if res.Skipped != 1 {
		t.Errorf("skipped %d objects, want 1", res.Skipped)
	}
	if res.Created != 1 {
		t.Errorf("created %d entities, want the one goblin", res.Created)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM spawns`).Scan(&n); err != nil {
		t.Fatalf("counting spawns: %v", err)
	}
	if n != 1 {
		t.Errorf("%d spawn rows, want 1 — a non-spawn was recorded as one", n)
	}
}

// A type whose validationLevel is "warning" is created with whatever it was
// given, which is what that level means. What it must not be is silent: the
// warnings live in EntityService for one call only, so anything not taken
// straight away is gone.
func TestSyncSpawns_ReportsAnEntityCreatedWithoutItsRequiredComponents(t *testing.T) {
	ds := spawnSchema()
	lenient := ds.EntityTypes["Goblin"]
	lenient.ValidationLevel = "warning"
	ds.EntityTypes["Goblin"] = lenient

	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.sqlite", ds, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	bare := tiled.Object{ID: 1, Name: "gob", Type: "Goblin", Visible: true}
	res, err := SyncSpawns(context.Background(), svc, store.DB(), "level.tmx", spawnMap(bare))
	if err != nil {
		t.Fatalf("SyncSpawns: %v", err)
	}
	if res.Created != 1 {
		t.Fatalf("result = %+v, want one create — warning level allows it", res)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("an entity was created without its required components and nothing said so")
	}
	said := strings.Join(res.Warnings, "\n")
	for _, want := range []string{"Health", "Sprite", "object 1"} {
		if !strings.Contains(said, want) {
			t.Errorf("the warnings do not mention %q:\n%s", want, said)
		}
	}
}

// ── Placement ────────────────────────────────────────────────────────

// An object off the edge of the map is refused rather than stored where nothing
// can reach it. Truncation used to put one nudged just off the left edge
// *inside* the map at column 0.
func TestSyncSpawns_RefusesAnObjectOutsideTheMap(t *testing.T) {
	cases := map[string]tiled.Object{
		"off the left":        goblinAt(1, -8, 0),
		"off the top":         goblinAt(2, 0, -8),
		"a whole cell off":    goblinAt(3, -16, 0),
		"past the right edge": goblinAt(4, 20*16, 0),
		"far outside on both": goblinAt(5, 4000, 4000),
	}
	for name, obj := range cases {
		t.Run(name, func(t *testing.T) {
			svc, db := spawnFixture(t)
			res := mustSpawn(t, svc, db, spawnMap(obj))
			if res.Created != 0 {
				t.Errorf("an object outside a 20x20 map was stored: %+v", entityRows(t, db))
			}
			if len(res.Refused) != 1 {
				t.Fatalf("result = %+v, want one refusal", res)
			}
			if !strings.Contains(res.Refused[0], "outside the map") {
				t.Errorf("the refusal does not say why: %q", res.Refused[0])
			}
		})
	}
}

// A tile object that is not grid-aligned still lands in the cell its bottom edge
// closes. Dividing and then subtracting a row is only right when it is aligned.
func TestSyncSpawns_ATileObjectOffTheGridLandsInTheCellItsBottomClose(t *testing.T) {
	svc, db := spawnFixture(t)

	// Bottom edge at y=8 on a 16px grid: inside row 0, not row -1.
	obj := goblinAt(1, 0, 8)
	obj.GID = 5
	res := mustSpawn(t, svc, db, spawnMap(obj))
	if res.Created != 1 {
		t.Fatalf("result = %+v, want one create", res)
	}
	rows := entityRows(t, db)
	if rows[0].Y != 0 {
		t.Errorf("a tile object whose bottom is at y=8 landed in row %d, want 0", rows[0].Y)
	}
}

// ── Position ─────────────────────────────────────────────────────────

// A property that sets Position is refused. The object's own coordinates are
// where the map shows the spawn, so a property overruling them silently would
// put the entity somewhere the author cannot see.
func TestSyncSpawns_RefusesAPropertyThatSetsPosition(t *testing.T) {
	svc, db := spawnFixture(t)

	obj := goblinAt(1, 32, 32)
	obj.Properties["Position.x"] = tiled.Property{Value: "17"}
	res := mustSpawn(t, svc, db, spawnMap(obj))

	if len(res.Refused) != 1 {
		t.Fatalf("result = %+v, want one refusal", res)
	}
	if !strings.Contains(res.Refused[0], "Position") || !strings.Contains(res.Refused[0], "move the object") {
		t.Errorf("the refusal does not say what to do instead: %q", res.Refused[0])
	}
}

// ── Values ───────────────────────────────────────────────────────────

func TestSyncSpawns_PropertyValues(t *testing.T) {
	cases := map[string]struct {
		prop, value string
		refused     string // the substring a refusal must carry, or "" to accept
	}{
		"a boolean Tiled would write":   {"Sprite.flip_x", "true", ""},
		"a boolean spelled as a number": {"Sprite.flip_x", "1", "not true or false"},
		"a boolean spelled as a letter": {"Sprite.flip_x", "t", "not true or false"},
		"a number that is not finite":   {"Speed.value", "NaN", "not a finite number"},
		"a number that is infinite":     {"Speed.value", "Inf", "not a finite number"},
		"a number with a Go underscore": {"Speed.value", "1_000", "not a number"},
		"an integer that is a float":    {"Health.hp", "2.5", "not a whole number"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			svc, db := spawnFixture(t)
			obj := goblinAt(1, 0, 0)
			obj.Properties[tc.prop] = tiled.Property{Value: tc.value}
			res := mustSpawn(t, svc, db, spawnMap(obj))

			if tc.refused == "" {
				if res.Created != 1 {
					t.Fatalf("result = %+v, want one create", res)
				}
				return
			}
			if len(res.Refused) != 1 {
				t.Fatalf("result = %+v, want one refusal", res)
			}
			if !strings.Contains(res.Refused[0], tc.refused) {
				t.Errorf("the refusal is %q, want it to say %q", res.Refused[0], tc.refused)
			}
		})
	}
}

// A string keeps its spaces. Every other type trims, because no number is
// spelled with one; an animation named " idle" is a mistake worth seeing.
func TestSyncSpawns_AStringKeepsItsSpaces(t *testing.T) {
	svc, db := spawnFixture(t)

	obj := goblinAt(1, 0, 0)
	obj.Properties["Sprite.animation"] = tiled.Property{Value: " goblin_idle "}
	mustSpawn(t, svc, db, spawnMap(obj))

	var anim string
	if err := db.QueryRow(`SELECT animation FROM comp_sprite`).Scan(&anim); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if anim != " goblin_idle " {
		t.Errorf("animation = %q, want its spaces kept", anim)
	}
}

// A JSON column is checked here, because the insert path stores whatever it is
// given — an earlier comment claimed otherwise and "this is not json at all"
// went into the column verbatim.
func TestSyncSpawns_RefusesTextThatIsNotJSONForAJSONColumn(t *testing.T) {
	ds := spawnSchema()
	ds.Components["Loot"] = schema.Component{Type: "object", Properties: map[string]schema.Property{
		"items": {Type: "array", Items: &schema.Property{Type: "string"}},
	}}
	gob := ds.EntityTypes["Goblin"]
	gob.OptionalComponents = append(gob.OptionalComponents, "Loot")
	ds.EntityTypes["Goblin"] = gob

	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.sqlite", ds, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	bad := goblinAt(1, 0, 0)
	bad.Properties["Loot.items"] = tiled.Property{Value: "this is not json at all"}
	res, err := SyncSpawns(context.Background(), svc, store.DB(), "level.tmx", spawnMap(bad))
	if err != nil {
		t.Fatalf("SyncSpawns: %v", err)
	}
	if res.Created != 0 || len(res.Refused) != 1 {
		t.Fatalf("result = %+v, want one refusal", res)
	}
	if !strings.Contains(res.Refused[0], "is not JSON") {
		t.Errorf("the refusal does not say what is wrong: %q", res.Refused[0])
	}

	good := goblinAt(2, 16, 0)
	good.Properties["Loot.items"] = tiled.Property{Value: `["gold","rope"]`}
	res, err = SyncSpawns(context.Background(), svc, store.DB(), "level.tmx", spawnMap(good))
	if err != nil {
		t.Fatalf("SyncSpawns: %v", err)
	}
	if res.Created != 1 {
		t.Fatalf("valid JSON was refused: %+v", res)
	}
}

// Loading a Tiled map needs the engine's tables, and says so rather than
// failing somewhere further in.
//
// Story 6 had this test the other way round — a map with no objects must not
// need them — which was reachable while a re-import only ever created. It is not
// reachable now: deleting the entity of an object the map no longer has means
// reading what this map spawned, and an empty map is exactly the case where
// everything it spawned should go.
func TestSyncSpawns_ATiledMapNeedsTheEngineTables(t *testing.T) {
	ds := spawnSchema()
	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.sqlite", ds, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	// EnsureInterpreterTables deliberately not called.
	_, err = SyncSpawns(context.Background(), svc, store.DB(), "level.tmx",
		&tiled.Map{Name: "level.tmx", Width: 4, Height: 4, TileWidth: 16, TileHeight: 16})
	if err == nil {
		t.Fatal("spawning against a store with no engine tables did not say so")
	}
	// The wrapper's own words as well as the driver's, so this passes for a
	// reason this package owns rather than for one SQLite happens to phrase
	// with the table's name in it.
	if !strings.Contains(err.Error(), "reading spawns for") {
		t.Errorf("the error does not say what this package was doing: %v", err)
	}
}

// ── Story 8: the file wins, as it does for tiles ─────────────────────

func spawnRow(t *testing.T, db *sql.DB, objectID int) (int64, bool) {
	t.Helper()
	var id sql.NullInt64
	err := db.QueryRow(`SELECT entity_id FROM spawns WHERE object_id = ?`, objectID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	}
	if err != nil {
		t.Fatalf("reading the spawn row: %v", err)
	}
	return id.Int64, id.Valid
}

// An object that moved moves its entity. Story 6 left it where it was, which is
// the one thing an author dragging a goblin in the editor is trying to change.
func TestSyncSpawns_AnObjectThatMovedMovesItsEntity(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 32, 48)))

	before := entityRows(t, db)
	res := mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 16)))

	if res.Updated != 1 || res.Created != 0 {
		t.Fatalf("result = %+v, want one update", res)
	}
	after := entityRows(t, db)
	if len(after) != 1 {
		t.Fatalf("stored %d entities, want 1", len(after))
	}
	if after[0].X != 0 || after[0].Y != 1 {
		t.Errorf("the goblin is at (%d,%d), want (0,1)", after[0].X, after[0].Y)
	}
	// The id survives: behavior_components and transitions name it, and a
	// machine's running state hangs off it.
	if after[0].ID != before[0].ID {
		t.Errorf("the entity id changed from %d to %d — a move became a delete and an insert",
			before[0].ID, after[0].ID)
	}
}

// A property that changed is re-applied.
func TestSyncSpawns_AChangedPropertyIsReapplied(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))

	edited := goblinAt(1, 0, 0)
	edited.Properties["Health.maxHp"] = tiled.Property{Value: "50"}
	res := mustSpawn(t, svc, db, spawnMap(edited))

	if res.Updated != 1 {
		t.Fatalf("result = %+v, want one update", res)
	}
	var maxHp int
	if err := db.QueryRow(`SELECT maxHp FROM comp_health`).Scan(&maxHp); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if maxHp != 50 {
		t.Errorf("maxHp = %d, want 50 — the file's value did not win", maxHp)
	}
}

// What the file says nothing about is left alone. The object names hp, so hp is
// overwritten; it says nothing about the Speed somebody attached at runtime, so
// Speed keeps its value.
func TestSyncSpawns_LeavesWhatTheFileDoesNotDescribe(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))

	rows := entityRows(t, db)
	if err := svc.AttachComponent(context.Background(), rows[0].ID, "Speed",
		world.ComponentValues{"value": 3.5}); err != nil {
		t.Fatalf("attaching Speed at runtime: %v", err)
	}
	if _, err := db.Exec(`UPDATE comp_health SET hp = 1`); err != nil {
		t.Fatalf("wounding the goblin: %v", err)
	}

	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))

	var speed float64
	if err := db.QueryRow(`SELECT value FROM comp_speed`).Scan(&speed); err != nil {
		t.Fatalf("Speed did not survive the re-import: %v", err)
	}
	if speed != 3.5 {
		t.Errorf("speed = %v, want 3.5 — a component the file never mentioned was overwritten", speed)
	}
	// hp *is* described by the object, so it goes back to what the file says.
	var hp int
	if err := db.QueryRow(`SELECT hp FROM comp_health`).Scan(&hp); err != nil {
		t.Fatalf("reading hp: %v", err)
	}
	if hp != 5 {
		t.Errorf("hp = %d, want the file's 5", hp)
	}
}

// A component the object newly mentions is attached to the entity that is
// already there.
func TestSyncSpawns_AttachesAComponentTheObjectNewlyMentions(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))

	edited := goblinAt(1, 0, 0)
	edited.Properties["Speed.value"] = tiled.Property{Value: "2.5"}
	if res := mustSpawn(t, svc, db, spawnMap(edited)); res.Updated != 1 {
		t.Fatalf("result = %+v, want one update", res)
	}

	var speed float64
	if err := db.QueryRow(`SELECT value FROM comp_speed`).Scan(&speed); err != nil {
		t.Fatalf("the newly declared component was not attached: %v", err)
	}
	if speed != 2.5 {
		t.Errorf("speed = %v, want 2.5", speed)
	}
}

// An object removed from the map deletes the entity it made. The half Story 6
// got most wrong: an author deleting a goblin in the editor has no other gesture.
func TestSyncSpawns_AnObjectRemovedFromTheMapDeletesItsEntity(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0), goblinAt(2, 32, 0)))

	res := mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))

	if res.Deleted != 1 {
		t.Fatalf("result = %+v, want one delete", res)
	}
	rows := entityRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("%d entities remain, want 1", len(rows))
	}
	if _, ok := spawnRow(t, db, 2); ok {
		t.Error("the deleted spawn's row is still there")
	}
}

// An entity killed at runtime comes back while its object is still in the map,
// for the same reason a door opened at runtime closes again.
func TestSyncSpawns_RecreatesAnEntityThatDiedWhileItsObjectRemains(t *testing.T) {
	svc, db := spawnFixture(t)
	m := spawnMap(goblinAt(1, 0, 0))
	mustSpawn(t, svc, db, m)

	if _, err := db.Exec(`DELETE FROM entities`); err != nil {
		t.Fatalf("killing the goblin: %v", err)
	}

	res := mustSpawn(t, svc, db, m)
	if res.Created != 1 {
		t.Fatalf("result = %+v, want the goblin back", res)
	}
	if n := len(entityRows(t, db)); n != 1 {
		t.Errorf("%d entities, want 1", n)
	}
	if _, ok := spawnRow(t, db, 1); !ok {
		t.Error("the spawn row does not point at the recreated entity")
	}
}

// Entities nobody spawned are never touched, whatever the map says.
func TestSyncSpawns_LeavesEntitiesNobodySpawned(t *testing.T) {
	svc, db := spawnFixture(t)

	hand, err := svc.CreateEntity(context.Background(), "Goblin", []world.EntityComponent{
		{Name: "Position", Values: world.ComponentValues{"x": 7, "y": 7}},
		{Name: "Health", Values: world.ComponentValues{"hp": 3, "maxHp": 3}},
		{Name: "Sprite", Values: world.ComponentValues{"sheet": "", "animation": "a", "flip_x": false}},
	})
	if err != nil {
		t.Fatalf("creating an entity by hand: %v", err)
	}

	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))
	// And now a map that describes nothing at all.
	res := mustSpawn(t, svc, db, spawnMap())

	if res.Deleted != 1 {
		t.Errorf("result = %+v, want only the spawned one deleted", res)
	}
	var alive int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, hand.ID).Scan(&alive); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if alive != 1 {
		t.Error("an entity nobody spawned was deleted by a map that never mentioned it")
	}
}

// Loading a map that has not changed writes nothing at all — SyncTiles'
// promise that "a restart with no edit to the map is a read", which this had
// better keep too, since the whole story is that the two follow one rule.
//
// Asserted through triggers rather than through the result, because the result
// is what would be wrong if the code re-applied everything and called it
// unchanged. A trigger fires on a write or it does not.
func TestSyncSpawns_AnUnchangedMapWritesNothing(t *testing.T) {
	svc, db := spawnFixture(t)
	m := spawnMap(goblinAt(1, 0, 0), goblinAt(2, 32, 0))
	mustSpawn(t, svc, db, m)

	if _, err := db.Exec(`CREATE TABLE writes (what TEXT)`); err != nil {
		t.Fatalf("creating the witness table: %v", err)
	}
	for _, table := range []string{"comp_position", "comp_health", "comp_sprite"} {
		for _, verb := range []string{"UPDATE", "INSERT", "DELETE"} {
			if _, err := db.Exec(fmt.Sprintf(
				`CREATE TRIGGER w_%s_%s AFTER %s ON %s BEGIN INSERT INTO writes VALUES ('%s %s'); END`,
				table, strings.ToLower(verb), verb, table, verb, table)); err != nil {
				t.Fatalf("creating a trigger: %v", err)
			}
		}
	}

	res := mustSpawn(t, svc, db, m)

	if res.Unchanged != 2 {
		t.Errorf("result = %+v, want two unchanged", res)
	}
	if res.Created != 0 || res.Updated != 0 || res.Deleted != 0 {
		t.Errorf("result = %+v, want nothing created, updated or deleted", res)
	}

	var writes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM writes`).Scan(&writes); err != nil {
		t.Fatalf("counting writes: %v", err)
	}
	if writes != 0 {
		var what string
		_ = db.QueryRow(`SELECT group_concat(what, ", ") FROM writes`).Scan(&what)
		t.Errorf("loading an unedited map wrote %d time(s): %s", writes, what)
	}
}

// A spawn deleted because its object went takes its whole entity, not just the
// components the object described.
func TestSyncSpawns_ADeletedSpawnTakesItsComponents(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))

	mustSpawn(t, svc, db, spawnMap())

	for _, table := range []string{"comp_position", "comp_health", "comp_sprite"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s still has %d row(s) after its entity was deleted", table, n)
		}
	}
}

// ForgetSpawn's own contract, which the cascade normally satisfies before it is
// reached: deleting the entity takes the row with it, so a test that only ever
// deletes through SyncSpawns cannot tell whether this does anything. It matters
// on a database opened without foreign keys enforced, where the cascade does not
// fire and the object would look spawned forever.
func TestTx_ForgetSpawnRemovesTheRow(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0), goblinAt(2, 32, 0)))

	if err := svc.InTx(context.Background(), func(tx world.Tx) error {
		return tx.ForgetSpawn(context.Background(), "mods/map/level.tmx", 1)
	}); err != nil {
		t.Fatalf("ForgetSpawn: %v", err)
	}

	if _, ok := spawnRow(t, db, 1); ok {
		t.Error("the row is still there")
	}
	if _, ok := spawnRow(t, db, 2); !ok {
		t.Error("the other map object's row went too")
	}
	// The entity is untouched: forgetting a spawn is not deleting one.
	if n := len(entityRows(t, db)); n != 2 {
		t.Errorf("%d entities remain, want 2 — ForgetSpawn deleted one", n)
	}
}

// ── The review's findings ────────────────────────────────────────────

// An object that could not be read is refused, and refusing it must not be the
// same as the map no longer having it. Without the guard that marks it seen, a
// typo in one property deletes the entity instead of complaining about it —
// which is the whole difference between a diagnostic and data loss.
func TestSyncSpawns_ARefusedObjectIsNotDeleted(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0), goblinAt(2, 32, 0)))

	broken := goblinAt(1, 0, 0)
	broken.Properties["Health.hp"] = tiled.Property{Value: "plenty"}
	res := mustSpawn(t, svc, db, spawnMap(broken, goblinAt(2, 32, 0)))

	if len(res.Refused) != 1 {
		t.Fatalf("result = %+v, want one refusal", res)
	}
	if res.Deleted != 0 {
		t.Errorf("result = %+v — a refused object was treated as one the map no longer has", res)
	}
	if n := len(entityRows(t, db)); n != 2 {
		t.Errorf("%d entities remain, want 2 — a typo deleted a goblin", n)
	}
}

// The update path validates what the create path validates, so the same file
// makes the same world whether or not it has been loaded before.
func TestSyncSpawns_AnUpdateIsValidatedLikeACreate(t *testing.T) {
	cases := map[string]func(tiled.Object) tiled.Object{
		"a component the type does not allow": func(o tiled.Object) tiled.Object {
			o.Properties["Tile.x"] = tiled.Property{Value: "1"}
			return o
		},
		"a required component the object stopped naming": func(o tiled.Object) tiled.Object {
			delete(o.Properties, "Health.hp")
			delete(o.Properties, "Health.maxHp")
			return o
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			svc, db := spawnFixture(t)
			mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))

			res := mustSpawn(t, svc, db, spawnMap(edit(goblinAt(1, 0, 0))))
			if len(res.Refused) != 1 {
				t.Fatalf("result = %+v, want one refusal — an update skipped the contract", res)
			}
			if res.Updated != 0 {
				t.Errorf("result = %+v, want nothing updated", res)
			}
		})
	}
}

// The same edits are refused on a database that has never seen the map, which
// is the property the two paths together are for.
func TestSyncSpawns_TheSameFileIsRefusedWhetherOrNotItRanBefore(t *testing.T) {
	obj := goblinAt(1, 0, 0)
	obj.Properties["Tile.x"] = tiled.Property{Value: "1"}

	fresh, freshDB := spawnFixture(t)
	first := mustSpawn(t, fresh, freshDB, spawnMap(obj))

	used, usedDB := spawnFixture(t)
	mustSpawn(t, used, usedDB, spawnMap(goblinAt(1, 0, 0)))
	second := mustSpawn(t, used, usedDB, spawnMap(obj))

	if len(first.Refused) != len(second.Refused) {
		t.Errorf("a fresh database refused %d and a used one %d:\n %v\n %v",
			len(first.Refused), len(second.Refused), first.Refused, second.Refused)
	}
}

// Retyping an object is a different entity, not a changed one: the class decides
// which components are required, so writing the new class's components onto an
// entity still typed as the old one makes a row that breaks its own contract.
func TestSyncSpawns_AChangedClassReplacesTheEntity(t *testing.T) {
	ds := spawnSchema()
	ds.EntityTypes["Orc"] = ds.EntityTypes["Goblin"]
	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.sqlite", ds, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)
	db := store.DB()

	if _, err := SyncSpawns(context.Background(), svc, db, "level.tmx",
		spawnMap(goblinAt(1, 0, 0))); err != nil {
		t.Fatalf("SyncSpawns: %v", err)
	}
	before := entityRows(t, db)

	orc := goblinAt(1, 0, 0)
	orc.Type = "Orc"
	res, err := SyncSpawns(context.Background(), svc, db, "level.tmx", spawnMap(orc))
	if err != nil {
		t.Fatalf("SyncSpawns: %v", err)
	}
	if res.Created != 1 || res.Deleted != 1 {
		t.Fatalf("result = %+v, want the goblin replaced by an orc", res)
	}

	after := entityRows(t, db)
	if len(after) != 1 {
		t.Fatalf("%d entities, want 1", len(after))
	}
	if after[0].Type != "Orc" {
		t.Errorf("the entity is a %s, want an Orc — the class was not re-applied", after[0].Type)
	}
	if after[0].ID == before[0].ID {
		t.Error("the entity kept its id through a class change; it is not the same entity")
	}
}

// A component that is not an object keeps its value under the name its single
// column has. The insert path takes a lone key of any name and
// SetComponentValues does not, so a scalar component spawned once and was then
// refused on every load.
func TestSyncSpawns_AScalarComponentSurvivesAReimport(t *testing.T) {
	ds := spawnSchema()
	ds.Components["Label"] = schema.Component{Type: "string"}
	gob := ds.EntityTypes["Goblin"]
	gob.OptionalComponents = append(gob.OptionalComponents, "Label")
	ds.EntityTypes["Goblin"] = gob

	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.sqlite", ds, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)
	db := store.DB()

	withLabel := func(v string) *tiled.Map {
		o := goblinAt(1, 0, 0)
		o.Properties["Label.value"] = tiled.Property{Value: v}
		return spawnMap(o)
	}

	if _, err := SyncSpawns(context.Background(), svc, db, "level.tmx", withLabel("boss")); err != nil {
		t.Fatalf("first import: %v", err)
	}
	res, err := SyncSpawns(context.Background(), svc, db, "level.tmx", withLabel("boss"))
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if len(res.Refused) != 0 {
		t.Fatalf("re-importing a scalar component was refused: %v", res.Refused)
	}
	if res.Unchanged != 1 {
		t.Errorf("result = %+v, want it unchanged", res)
	}

	// And an edit to it lands.
	if _, err := SyncSpawns(context.Background(), svc, db, "level.tmx", withLabel("minion")); err != nil {
		t.Fatalf("third import: %v", err)
	}
	var label string
	if err := db.QueryRow(`SELECT value FROM comp_label`).Scan(&label); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if label != "minion" {
		t.Errorf("label = %q, want minion", label)
	}
}

// Clearing an object's class deletes its entity, which is right under the rule
// and is a lot of consequence for emptying a field. It is said rather than done
// quietly.
func TestSyncSpawns_ClearingAClassSaysTheEntityIsGoing(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))

	declassed := goblinAt(1, 0, 0)
	declassed.Type = ""
	res := mustSpawn(t, svc, db, spawnMap(declassed))

	if res.Deleted != 1 {
		t.Fatalf("result = %+v, want the entity deleted", res)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "no class") {
		t.Errorf("nothing said the entity was going: %v", res.Warnings)
	}
}

// A row whose entity is not there does not count as spawned. The foreign key
// should make that impossible; a connection opened without foreign keys enforced
// can produce it, and every load then reported success for a goblin that did not
// exist.
func TestSyncSpawns_ARowWithNoEntityIsNotSpawned(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))

	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disabling foreign keys: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM entities`); err != nil {
		t.Fatalf("deleting the entity without the cascade: %v", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("re-enabling: %v", err)
	}

	res := mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))
	if res.Created != 1 {
		t.Errorf("result = %+v, want the goblin created — a row pointing at nothing counted as spawned", res)
	}
}

// A component that is not an object holds its value in one column, so
// "Label.value" addresses it and "Label.text" does not — the property half of
// the name is the column, not something the schema declares.
func TestSyncSpawns_AScalarComponentIsAddressedByItsColumn(t *testing.T) {
	ds := spawnSchema()
	ds.Components["Label"] = schema.Component{Type: "string"}
	gob := ds.EntityTypes["Goblin"]
	gob.OptionalComponents = append(gob.OptionalComponents, "Label")
	ds.EntityTypes["Goblin"] = gob

	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.sqlite", ds, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	obj := goblinAt(1, 0, 0)
	obj.Properties["Label.text"] = tiled.Property{Value: "boss"}
	res, err := SyncSpawns(context.Background(), svc, store.DB(), "level.tmx", spawnMap(obj))
	if err != nil {
		t.Fatalf("SyncSpawns: %v", err)
	}
	if len(res.Refused) != 1 {
		t.Fatalf("result = %+v, want the wrong column name refused", res)
	}
	for _, want := range []string{"Label.text", `"value"`} {
		if !strings.Contains(res.Refused[0], want) {
			t.Errorf("the refusal does not mention %q: %q", want, res.Refused[0])
		}
	}
}

// ── Story 9: a map says which map it is ──────────────────────────────

func withMapID(m *tiled.Map, id string) *tiled.Map {
	m.Properties = tiled.Properties{tiled.PropMapID: {Value: id}}
	return m
}

func spawnRowMaps(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT DISTINCT map FROM spawns ORDER BY map`)
	if err != nil {
		t.Fatalf("reading spawn maps: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		out = append(out, m)
	}
	return out
}

// A map with an id is keyed by it, so the path is not part of its identity.
func TestSyncSpawns_AMapWithAnIDIsKeyedByIt(t *testing.T) {
	svc, db := spawnFixture(t)

	mustSpawn(t, svc, db, withMapID(spawnMap(goblinAt(1, 0, 0)), "level1"))

	if got := spawnRowMaps(t, db); len(got) != 1 || got[0] != "level1" {
		t.Errorf("spawns are filed under %v, want [level1]", got)
	}
}

// Renaming or moving a map changes nothing about its world.
func TestSyncSpawns_RenamingAMapWithAnIDChangesNothing(t *testing.T) {
	svc, db := spawnFixture(t)
	m := func() *tiled.Map { return withMapID(spawnMap(goblinAt(1, 0, 0)), "level1") }

	if _, err := SyncSpawns(context.Background(), svc, db, "mods/map/level1.tmx", m()); err != nil {
		t.Fatalf("first load: %v", err)
	}
	before := entityRows(t, db)

	// The same map, renamed and moved.
	res, err := SyncSpawns(context.Background(), svc, db, "mods/levels/first.tmx", m())
	if err != nil {
		t.Fatalf("after renaming: %v", err)
	}
	if res.Created != 0 {
		t.Errorf("result = %+v — renaming the file spawned the world again", res)
	}
	after := entityRows(t, db)
	if len(after) != 1 {
		t.Fatalf("%d entities after a rename, want 1", len(after))
	}
	if after[0].ID != before[0].ID {
		t.Errorf("the entity id changed from %d to %d across a rename", before[0].ID, after[0].ID)
	}

	// And the author can still delete it from the renamed file.
	res, err = SyncSpawns(context.Background(), svc, db, "mods/levels/first.tmx",
		withMapID(spawnMap(), "level1"))
	if err != nil {
		t.Fatalf("deleting: %v", err)
	}
	if res.Deleted != 1 {
		t.Errorf("result = %+v — the entity is unreachable from the renamed file", res)
	}
	if n := len(entityRows(t, db)); n != 0 {
		t.Errorf("%d entities remain", n)
	}
}

// A map with no id keys by its path, as it did before.
func TestSyncSpawns_AMapWithNoIDKeysByItsPath(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))

	if got := spawnRowMaps(t, db); len(got) != 1 || got[0] != "mods/map/level.tmx" {
		t.Errorf("spawns are filed under %v, want the path", got)
	}
}

// ...and says so, because the trap only springs on a rename and by then the
// duplicates cannot be reached.
func TestSyncSpawns_AMapWithNoIDSaysSo(t *testing.T) {
	svc, db := spawnFixture(t)

	res := mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))
	if len(res.Warnings) != 1 {
		t.Fatalf("result = %+v, want one warning about the missing id", res)
	}
	for _, want := range []string{"mapId", "renam"} {
		if !strings.Contains(res.Warnings[0], want) {
			t.Errorf("the warning does not mention %q: %q", want, res.Warnings[0])
		}
	}

	// Once for the map, not once per object.
	res = mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0), goblinAt(2, 32, 0), goblinAt(3, 64, 0)))
	if len(res.Warnings) != 1 {
		t.Errorf("result = %+v, want the warning once for the map", res)
	}
}

// A map that gains an id adopts the rows it had under its path. Without this,
// adding the property is itself a rename and spawns the world twice — the fix
// introducing the bug it fixes.
func TestSyncSpawns_AMapThatGainsAnIDAdoptsItsRows(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0), goblinAt(2, 32, 0)))
	before := entityRows(t, db)

	res := mustSpawn(t, svc, db, withMapID(spawnMap(goblinAt(1, 0, 0), goblinAt(2, 32, 0)), "level1"))

	if res.Created != 0 {
		t.Errorf("result = %+v — adding an id spawned the world again", res)
	}
	after := entityRows(t, db)
	if len(after) != 2 {
		t.Fatalf("%d entities, want 2", len(after))
	}
	for i := range before {
		if after[i].ID != before[i].ID {
			t.Errorf("entity %d changed id from %d to %d", i, before[i].ID, after[i].ID)
		}
	}
	if got := spawnRowMaps(t, db); len(got) != 1 || got[0] != "level1" {
		t.Errorf("spawns are filed under %v, want [level1] — the old rows were left behind", got)
	}
}

// Where both keys hold the same object, the id-keyed row is the live one and the
// path-keyed entity is a duplicate of it.
func TestSyncSpawns_AdoptionRemovesDuplicates(t *testing.T) {
	svc, db := spawnFixture(t)

	// Reaching a state where one object has a row under both keys takes a
	// second path, because adoption clears the first one on sight:
	//
	//   a.tmx with no id      → rows under "a.tmx"
	//   a.tmx with id "level1" → adopted to "level1"
	//   b.tmx with no id      → a second, duplicate world under "b.tmx"
	//   b.tmx with id "level1" → both keys hold object 1
	load := func(path string, m *tiled.Map) {
		t.Helper()
		if _, err := SyncSpawns(context.Background(), svc, db, path, m); err != nil {
			t.Fatalf("loading %s: %v", path, err)
		}
	}
	load("a.tmx", spawnMap(goblinAt(1, 0, 0)))
	load("a.tmx", withMapID(spawnMap(goblinAt(1, 0, 0)), "level1"))
	load("b.tmx", spawnMap(goblinAt(1, 0, 0), goblinAt(2, 32, 0)))
	if n := len(entityRows(t, db)); n != 3 {
		t.Fatalf("%d entities before adoption, want 3", n)
	}

	res, err := SyncSpawns(context.Background(), svc, db, "b.tmx",
		withMapID(spawnMap(goblinAt(1, 0, 0), goblinAt(2, 32, 0)), "level1"))
	if err != nil {
		t.Fatalf("adopting: %v", err)
	}

	if res.Created != 0 {
		t.Errorf("result = %+v, want nothing created", res)
	}
	if n := len(entityRows(t, db)); n != 2 {
		t.Errorf("%d entities after adoption, want 2 — the duplicate survived", n)
	}
	if got := spawnRowMaps(t, db); len(got) != 1 || got[0] != "level1" {
		t.Errorf("spawns are filed under %v, want [level1]", got)
	}
}

// An id is not adopted from a path that never spawned anything, so a fresh
// database is not touched by the adoption path.
func TestSyncSpawns_AdoptionDoesNothingOnAFreshDatabase(t *testing.T) {
	svc, db := spawnFixture(t)

	res := mustSpawn(t, svc, db, withMapID(spawnMap(goblinAt(1, 0, 0)), "level1"))
	if res.Created != 1 {
		t.Fatalf("result = %+v, want one create", res)
	}
	if got := spawnRowMaps(t, db); len(got) != 1 || got[0] != "level1" {
		t.Errorf("spawns are filed under %v", got)
	}
}

// A map whose id happens to be its path is not adopted from itself.
//
// Adoption reads the rows under the old key and the new one and treats an object
// present in both as a duplicate to delete. When the two keys are the same
// string every object is its own duplicate, and the map deletes its whole world
// on load. Nothing sensible sets mapId to a path, which is exactly why the guard
// against it needs a test rather than a reader's good intentions.
func TestSyncSpawns_AMapWhoseIDIsItsPathIsNotAdoptedFromItself(t *testing.T) {
	svc, db := spawnFixture(t)
	const path = "mods/map/level.tmx"

	m := func() *tiled.Map { return withMapID(spawnMap(goblinAt(1, 0, 0)), path) }
	if _, err := SyncSpawns(context.Background(), svc, db, path, m()); err != nil {
		t.Fatalf("first load: %v", err)
	}
	before := entityRows(t, db)
	if len(before) != 1 {
		t.Fatalf("%d entities after the first load, want 1", len(before))
	}

	res, err := SyncSpawns(context.Background(), svc, db, path, m())
	if err != nil {
		t.Fatalf("second load: %v", err)
	}

	// The count is the wrong thing to look at: self-adoption deletes the entity
	// and the ordinary create path immediately makes another, so one goes in and
	// one comes out. What it destroys is the entity's *identity* — the goblin a
	// machine and every transition row were pointing at.
	if res.Unchanged != 1 {
		t.Errorf("result = %+v, want the object unchanged", res)
	}
	after := entityRows(t, db)
	if len(after) != 1 {
		t.Fatalf("%d entities after the second load, want 1", len(after))
	}
	if after[0].ID != before[0].ID {
		t.Errorf("the entity id changed from %d to %d — the map adopted itself, deleting its own world and rebuilding it",
			before[0].ID, after[0].ID)
	}
}
