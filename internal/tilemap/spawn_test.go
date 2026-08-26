package tilemap

import (
	"context"
	"database/sql"
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

// An entity that has moved is not moved back. The file says where a world
// starts, not what it currently is.
func TestSyncSpawns_DoesNotMoveAnEntityThatHasMoved(t *testing.T) {
	svc, db := spawnFixture(t)
	m := spawnMap(goblinAt(1, 32, 48))
	mustSpawn(t, svc, db, m)

	if _, err := db.Exec(`UPDATE comp_position SET x = 9, y = 9`); err != nil {
		t.Fatalf("moving the goblin: %v", err)
	}
	// The map moves it too, which must not follow.
	moved := spawnMap(goblinAt(1, 0, 0))
	mustSpawn(t, svc, db, moved)

	rows := entityRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("stored %d entities, want 1", len(rows))
	}
	if rows[0].X != 9 || rows[0].Y != 9 {
		t.Errorf("the goblin is at (%d,%d), want (9,9) — the map moved an entity that had walked away",
			rows[0].X, rows[0].Y)
	}
}

// A spawn removed from the map does not delete the entity it made. Deleting is
// a level reset, which is a different operation from re-importing a file.
func TestSyncSpawns_DoesNotDeleteAnEntityWhoseSpawnIsGone(t *testing.T) {
	svc, db := spawnFixture(t)
	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0), goblinAt(2, 32, 0)))

	mustSpawn(t, svc, db, spawnMap(goblinAt(1, 0, 0)))

	if n := len(entityRows(t, db)); n != 2 {
		t.Errorf("%d entities remain, want 2 — removing a spawn killed its entity", n)
	}
}

// A spawned entity that has died does not come back. The row records that the
// import happened, which stays true after the entity is gone.
func TestSyncSpawns_DoesNotRespawnAnEntityThatDied(t *testing.T) {
	svc, db := spawnFixture(t)
	m := spawnMap(goblinAt(1, 0, 0))
	mustSpawn(t, svc, db, m)

	if _, err := db.Exec(`DELETE FROM entities`); err != nil {
		t.Fatalf("killing the goblin: %v", err)
	}

	res := mustSpawn(t, svc, db, m)
	if res.Created != 0 {
		t.Errorf("a dead spawn came back: %+v", res)
	}
	// And the record of it says its entity is gone rather than pointing at one.
	var entityID sql.NullInt64
	if err := db.QueryRow(`SELECT entity_id FROM spawns WHERE object_id = 1`).Scan(&entityID); err != nil {
		t.Fatalf("reading the spawn row: %v", err)
	}
	if entityID.Valid {
		t.Errorf("the spawn still points at entity %d, which no longer exists", entityID.Int64)
	}
}

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

// A map with objects but no engine tables is a caller's mistake; a map with no
// objects must not need them at all.
func TestSyncSpawns_AMapWithNoObjectsDoesNotNeedTheSpawnsTable(t *testing.T) {
	ds := spawnSchema()
	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.sqlite", ds, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	// EnsureInterpreterTables deliberately not called.
	if _, err := SyncSpawns(context.Background(), svc, store.DB(), "level.tmx",
		&tiled.Map{Name: "level.tmx", Width: 4, Height: 4, TileWidth: 16, TileHeight: 16}); err != nil {
		t.Errorf("a map with no objects asked for the spawns table: %v", err)
	}
}
