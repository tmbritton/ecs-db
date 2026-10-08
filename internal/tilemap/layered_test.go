package tilemap

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/world"
)

func TestMapID_UsesAuthoredIDOrCleanedPath(t *testing.T) {
	tests := []struct {
		name string
		m    *tiled.Map
		want string
	}{
		{name: "authored identity", m: &tiled.Map{Properties: tiled.Properties{
			tiled.PropMapID: {Value: "room"},
		}}, want: "room"},
		{name: "path fallback", m: &tiled.Map{}, want: "maps/level.tmx"},
		{name: "no parsed map", want: "maps/level.tmx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapID("maps/./level.tmx", tt.m); got != tt.want {
				t.Fatalf("MapID = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadMap_RepoSchemaImportsAllLayerTileComponents(t *testing.T) {
	raw, err := os.ReadFile("../../schema.json")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := schema.LoadSchema(raw)
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewSQLiteStore(t.TempDir()+"/world.sqlite", ds, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatal(err)
	}
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("floor", 1, 1, `id="1"`, "1"),
		layerOf("wall", 1, 1, `id="2"`, "2")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err != nil {
		t.Fatal(err)
	}
	if n := tileCount(t, store.DB()); n != 2 {
		t.Fatalf("repo schema imported %d tile entities, want two", n)
	}
	rows, err := store.DB().Query(`SELECT p.x, p.y, refs.value FROM comp_tilelayer l
		JOIN comp_position p ON p.entity_id=l.entity_id
		JOIN comp_tilereferences refs ON refs.entity_id=l.entity_id
		ORDER BY l.layer_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	var artIDs []int64
	for rows.Next() {
		var x, y int
		var refs string
		if err := rows.Scan(&x, &y, &refs); err != nil || x != 0 || y != 0 {
			t.Fatalf("Tile Position/TileReferences = %d,%d,%q; err %v", x, y, refs, err)
		}
		ids, err := ParseTileReferences(refs)
		if err != nil || len(ids) != 1 {
			t.Fatalf("Tile references = %q,%v; want one referenced art entity", refs, err)
		}
		artIDs = append(artIDs, ids[0])
		count++
	}
	if count != 2 {
		t.Fatalf("%d placed Tiles have Position and TileReferences, want two", count)
	}
	for i, id := range artIDs {
		var kind string
		wantType := []string{"Floor", "Wall"}[i]
		if err := store.DB().QueryRow(`SELECT e.entity_type FROM entities e JOIN comp_tilevisual v ON v.entity_id=e.id
			JOIN comp_passability p ON p.entity_id=e.id JOIN comp_visibility sight ON sight.entity_id=e.id
			WHERE e.id=?`, id).Scan(&kind); err != nil || kind != wantType {
			t.Fatalf("Tile references entity %d of type %q, err %v; want %s with art and rules", id, kind, err, wantType)
		}
	}
	var directVisuals int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM comp_tilevisual WHERE entity_id IN (SELECT entity_id FROM comp_tilelayer)`).Scan(&directVisuals); err != nil || directVisuals != 0 {
		t.Fatalf("%d Tile entities own artwork directly; err %v", directVisuals, err)
	}
}

func TestLoadMap_OwnedReferenceReconcilesAuthoredOptionalComponents(t *testing.T) {
	ds := tileSchema()
	floor := ds.EntityTypes["Floor"]
	floor.RequiredComponents = []string{"TileVisual"}
	floor.OptionalComponents = []string{"TileEntityOwner", "Position", "Passability", "Visibility", "RuntimeNote"}
	ds.EntityTypes["Floor"] = floor
	store, err := storage.NewSQLiteStore(t.TempDir()+"/world.sqlite", ds, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatal(err)
	}
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")), "level.tmx")
	load := func() {
		t.Helper()
		if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err != nil {
			t.Fatal(err)
		}
	}
	load()
	tileID := layerTileID(t, store.DB(), 1)
	artID := ownedArtID(t, store.DB(), tileID)
	if err := svc.AttachComponent(context.Background(), artID, "Position", world.ComponentValues{"x": 4, "y": 5}); err != nil {
		t.Fatal(err)
	}
	withNote := strings.Replace(twoTileTSX, `<property name="Visibility.kind" value="clear"/>`,
		`<property name="Visibility.kind" value="clear"/><property name="RuntimeNote.value" value="authored"/>`, 1)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "dungeon.tsx"), []byte(withNote), 0o644); err != nil {
		t.Fatal(err)
	}
	load()
	if ownedArtID(t, store.DB(), tileID) != artID {
		t.Fatal("adding an optional component replaced the referenced entity")
	}
	var note string
	if err := store.DB().QueryRow(`SELECT value FROM comp_runtimenote WHERE entity_id=?`, artID).Scan(&note); err != nil || note != "authored" {
		t.Fatalf("newly authored optional component = %q, %v", note, err)
	}
	withoutAuthored := strings.Replace(withNote, `<property name="RuntimeNote.value" value="authored"/>`, "", 1)
	withoutAuthored = strings.Replace(withoutAuthored, `<property name="Passability.kind" value="open"/>`, "", 1)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "dungeon.tsx"), []byte(withoutAuthored), 0o644); err != nil {
		t.Fatal(err)
	}
	load()
	for _, table := range []string{"comp_runtimenote", "comp_passability"} {
		var n int
		if err := store.DB().QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE entity_id=?`, artID).Scan(&n); err != nil || n != 0 {
			t.Fatalf("removed authored %s persists: %d, %v", table, n, err)
		}
	}
	var x, y float64
	if err := store.DB().QueryRow(`SELECT x,y FROM comp_position WHERE entity_id=?`, artID).Scan(&x, &y); err != nil || x != 4 || y != 5 {
		t.Fatalf("runtime-only component was lost: Position=%g,%g, %v", x, y, err)
	}
	if ownedArtID(t, store.DB(), tileID) != artID {
		t.Fatal("removing optional components replaced the referenced entity")
	}
}

func layerTileID(t *testing.T, db *sql.DB, layerID int) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT entity_id FROM comp_tilelayer WHERE layer_id = ?`, layerID).Scan(&id); err != nil {
		t.Fatalf("reading layer %d tile: %v", layerID, err)
	}
	return id
}

func TestLoadMap_StackedAndHiddenLayerTilesHaveDistinctEntities(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("ground", 1, 1, `id="11"`, "1"),
		layerOf("hidden", 1, 1, `id="12" visible="0"`, "2")), "level.tmx")

	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	if n := tileCount(t, db); n != 2 {
		t.Fatalf("tiles = %d, want both authored layer tiles", n)
	}
	if a, b := layerTileID(t, db, 11), layerTileID(t, db, 12); a == b {
		t.Fatal("stacked tiles share an entity ID")
	}
	var visible int
	if err := db.QueryRow(`SELECT visible FROM comp_tilelayer WHERE entity_id = ?`, layerTileID(t, db, 12)).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatal("hidden layer tile is drawn")
	}
}

func TestLoadMap_ReimportRestoresAuthoredPositionAndReferences(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(2, 1,
		layerOf("ground", 2, 1, `id="1"`, "1,0")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	id := layerTileID(t, db, 1)
	artID := ownedArtID(t, db, id)
	entity, err := db.Exec(`INSERT INTO entities(entity_type, created_tick) VALUES ('RuntimeWall', 0)`)
	if err != nil {
		t.Fatal(err)
	}
	wallID, err := entity.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE comp_position SET x=1 WHERE entity_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE comp_tilereferences SET value=? WHERE entity_id=?`, fmt.Sprintf("[%d]", wallID), id); err != nil {
		t.Fatal(err)
	}
	if err := svc.AttachComponent(context.Background(), id, "RuntimeNote", world.ComponentValues{"value": "keep me"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	var x int
	var refs string
	if err := db.QueryRow(`SELECT p.x, r.value FROM comp_position p JOIN comp_tilereferences r ON p.entity_id=r.entity_id WHERE p.entity_id=?`, id).Scan(&x, &refs); err != nil {
		t.Fatal(err)
	}
	if x != 0 || refs != fmt.Sprintf("[%d]", artID) || layerTileID(t, db, 1) != id {
		t.Fatalf("Tile re-import x=%d refs=%q ID=%d, want authored Position/references and ID %d", x, refs, layerTileID(t, db, 1), id)
	}
	var note string
	if err := db.QueryRow(`SELECT value FROM comp_runtimenote WHERE entity_id=?`, id).Scan(&note); err != nil || note != "keep me" {
		t.Fatalf("re-import lost unrelated runtime component: %q,%v", note, err)
	}
}

func ownedArtID(t *testing.T, db *sql.DB, tileID int64) int64 {
	t.Helper()
	var artID int64
	if err := db.QueryRow(`SELECT entity_id FROM comp_tileentityowner WHERE target_entity_id=?`, tileID).Scan(&artID); err != nil {
		t.Fatalf("Tile %d has no referenced art entity: %v", tileID, err)
	}
	return artID
}

func TestLoadMap_ReorderingAndEditingOneLayerKeepsBothIdentities(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("ground", 1, 1, `id="11"`, "1"),
		layerOf("walls", 1, 1, `id="12"`, "2")), "level.tmx")
	load := func() {
		t.Helper()
		if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
			t.Fatal(err)
		}
	}
	write := func(layers ...string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(mapOf(1, 1, layers...)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	load()
	ground, walls := layerTileID(t, db, 11), layerTileID(t, db, 12)
	if err := svc.AttachComponent(context.Background(), ground, "RuntimeNote", world.ComponentValues{"value": "keep me"}); err != nil {
		t.Fatal(err)
	}
	write(layerOf("walls", 1, 1, `id="12"`, "1"), layerOf("ground", 1, 1, `id="11"`, "2"))
	load()
	if ground != layerTileID(t, db, 11) || walls != layerTileID(t, db, 12) {
		t.Fatal("reordering replaced tile entities")
	}
	var topID int64
	if err := db.QueryRow(`SELECT entity_id FROM comp_tilelayer ORDER BY layer_order DESC LIMIT 1`).Scan(&topID); err != nil || topID != ground {
		t.Fatalf("top tile = %d, err %v; want ground entity %d", topID, err, ground)
	}
	write(layerOf("walls", 1, 1, `id="12"`, "2"), layerOf("ground", 1, 1, `id="11"`, "2"))
	load()
	if ground != layerTileID(t, db, 11) || walls != layerTileID(t, db, 12) {
		t.Fatal("changing art replaced an entity")
	}
	var runtimeNote string
	if err := db.QueryRow(`SELECT value FROM comp_runtimenote WHERE entity_id = ?`, ground).Scan(&runtimeNote); err != nil || runtimeNote != "keep me" {
		t.Fatalf("runtime-only component was lost on re-import: %q, %v", runtimeNote, err)
	}
	write(layerOf("walls", 1, 1, `id="12"`, "0"), layerOf("ground", 1, 1, `id="11"`, "2"))
	load()
	if tileCount(t, db) != 1 || layerTileID(t, db, 11) != ground {
		t.Fatal("removing one stacked tile removed or retargeted the other")
	}
}

func TestLoadMap_TilesetRuleEditUpdatesReferencedEntityInPlace(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("ground", 1, 1, `id="1"`, "1")), "level.tmx")
	load := func() {
		t.Helper()
		if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
			t.Fatal(err)
		}
	}
	load()
	tileID := layerTileID(t, db, 1)
	visualID := ownedArtID(t, db, tileID)
	modified := strings.Replace(twoTileTSX, `name="Passability.kind" value="open"`, `name="Passability.kind" value="solid"`, 1)
	modified = strings.Replace(modified, `name="Visibility.kind" value="clear"`, `name="Visibility.kind" value="opaque"`, 1)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "dungeon.tsx"), []byte(modified), 0o644); err != nil {
		t.Fatal(err)
	}
	load()
	if layerTileID(t, db, 1) != tileID || ownedArtID(t, db, tileID) != visualID {
		t.Fatal("editing referenced entity rules replaced the Tile or its same-type Floor reference")
	}
	var pass, sight string
	if err := db.QueryRow(`SELECT kind FROM comp_passability WHERE entity_id=?`, visualID).Scan(&pass); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT kind FROM comp_visibility WHERE entity_id=?`, visualID).Scan(&sight); err != nil {
		t.Fatal(err)
	}
	if pass != "solid" || sight != "opaque" {
		t.Fatalf("edited Floor restrictions = %q,%q; want solid,opaque", pass, sight)
	}
}

func TestLoadMap_ChangingAssignedEntityTypeReplacesOnlyOwnedReference(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("ground", 1, 1, `id="1"`, "1")), "level.tmx")
	load := func() {
		t.Helper()
		if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
			t.Fatal(err)
		}
	}
	load()
	tileID := layerTileID(t, db, 1)
	floorID := ownedArtID(t, db, tileID)
	modified := strings.Replace(twoTileTSX, `name="entityType" value="Floor"`, `name="entityType" value="Wall"`, 1)
	modified = strings.Replace(modified, `name="Passability.kind" value="open"`, `name="Passability.kind" value="solid"`, 1)
	modified = strings.Replace(modified, `name="Visibility.kind" value="clear"`, `name="Visibility.kind" value="opaque"`, 1)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "dungeon.tsx"), []byte(modified), 0o644); err != nil {
		t.Fatal(err)
	}
	load()
	newID := ownedArtID(t, db, tileID)
	if layerTileID(t, db, 1) != tileID || newID == floorID {
		t.Fatalf("tile ID replaced or type-changing reference reused Floor ID: tile=%d reference=%d", tileID, newID)
	}
	var kind string
	if err := db.QueryRow(`SELECT entity_type FROM entities WHERE id=?`, newID).Scan(&kind); err != nil || kind != "Wall" {
		t.Fatalf("new reference type = %q,%v; want Wall", kind, err)
	}
	var oldCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id=?`, floorID).Scan(&oldCount); err != nil || oldCount != 0 {
		t.Fatalf("replaced Floor ID %d still exists: count=%d,%v", floorID, oldCount, err)
	}
}

func TestLoadMap_RemovingPaintedTileAlsoRemovesItsOwnedEntity(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("ground", 1, 1, `id="1"`, "1")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	tileID := layerTileID(t, db, 1)
	floorID := ownedArtID(t, db, tileID)
	if err := os.WriteFile(path, []byte(mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "0"))), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id IN (?, ?)`, tileID, floorID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("removed Tile %d and owned Floor %d left %d entities: %v", tileID, floorID, remaining, err)
	}
}

func TestLoadMap_MapIdSurvivesFileRename(t *testing.T) {
	svc, db := syncFixture(t)
	mapData := strings.Replace(mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")),
		`<tileset firstgid`, `<properties><property name="mapId" value="room"/></properties><tileset firstgid`, 1)
	path := tiledMap(t, twoTileTSX, mapData, "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	id := layerTileID(t, db, 1)
	renamed := filepath.Join(filepath.Dir(path), "renamed.tmx")
	if err := os.Rename(path, renamed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(context.Background(), svc, db, renamed); err != nil {
		t.Fatal(err)
	}
	if layerTileID(t, db, 1) != id || tileCount(t, db) != 1 {
		t.Fatal("mapId did not preserve the tile identity across a file rename")
	}
	if draws, err := ReadTileDraws(context.Background(), db, "room"); err != nil || len(draws) != 1 {
		t.Fatalf("draws under mapId = %v, %v; want one", draws, err)
	}
}

func TestLoadMap_AddingMapIdAdoptsPathKeyedTileWithoutLosingRuntimeState(t *testing.T) {
	svc, db := syncFixture(t)
	file := mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1"))
	path := tiledMap(t, twoTileTSX, file, "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	id := layerTileID(t, db, 1)
	if err := svc.AttachComponent(context.Background(), id, "RuntimeNote", world.ComponentValues{"value": "keep me"}); err != nil {
		t.Fatal(err)
	}
	file = strings.Replace(file, `<tileset firstgid`, `<properties><property name="mapId" value="room"/></properties><tileset firstgid`, 1)
	if err := os.WriteFile(path, []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	var mapID, note string
	if err := db.QueryRow(`SELECT map_id FROM comp_tilelayer WHERE entity_id = ?`, id).Scan(&mapID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT value FROM comp_runtimenote WHERE entity_id = ?`, id).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if mapID != "room" || layerTileID(t, db, 1) != id || note != "keep me" || tileCount(t, db) != 1 {
		t.Fatalf("map ID adoption replaced tile: id %d, map %q, note %q", id, mapID, note)
	}
}

func TestLoadMap_LegacyUnattributedTileRefusesWithoutDeletion(t *testing.T) {
	svc, db := syncFixture(t)
	row, err := db.Exec(`INSERT INTO entities(entity_type,created_tick) VALUES ('Tile',0)`)
	if err != nil {
		t.Fatal(err)
	}
	id, err := row.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO comp_tile(entity_id,x,y,tile_type) VALUES (?,0,0,'floor')`, id); err != nil {
		t.Fatal(err)
	}
	path := tiledMap(t, twoTileTSX, mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err == nil || !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("LoadMap = %v, want legacy ownership refusal", err)
	}
	var still int64
	if err := db.QueryRow(`SELECT entity_id FROM comp_tile`).Scan(&still); err != nil || still != id || tileCount(t, db) != 1 {
		t.Fatalf("legacy tile was deleted or replaced: id %d, err %v", still, err)
	}
}

func TestLoadMap_AmbiguousPathAndMapIdTilesRefuseWithoutDeletingEither(t *testing.T) {
	svc, db := syncFixture(t)
	file := mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1"))
	path := tiledMap(t, twoTileTSX, file, "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	second := LayerTile{
		MapID: "room", LayerID: 1, State: TileState{TileType: "floor"},
		Visual: tiled.Draw{Image: "floor.png", SW: 8, SH: 8, Alpha: 1}, Visible: true,
	}
	ctx := context.Background()
	if err := svc.InTx(ctx, func(tx world.Tx) error {
		_, err := svc.CreateEntityInTx(ctx, tx, "Tile", []world.EntityComponent{
			{Name: "Tile", Values: tileValues(Point{}, second.State)},
			{Name: "TileLayer", Values: layerValues(second)},
			{Name: "Position", Values: world.ComponentValues{"x": 0, "y": 0}},
			{Name: "TileReferences", Values: world.ComponentValues{"value": []int64{}}},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	file = strings.Replace(file, `<tileset firstgid`, `<properties><property name="mapId" value="room"/></properties><tileset firstgid`, 1)
	if err := os.WriteFile(path, []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(ctx, svc, db, path); err == nil || !strings.Contains(err.Error(), "both path-keyed and mapId-keyed") {
		t.Fatalf("LoadMap = %v, want ambiguous identity refusal", err)
	}
	if tileCount(t, db) != 2 {
		t.Fatal("ambiguous map ID adoption deleted a tile")
	}
}

func TestLoadMap_ImportingOneMapDoesNotDeleteAnotherMapsTiles(t *testing.T) {
	svc, db := syncFixture(t)
	pathA := tiledMap(t, twoTileTSX, mapOf(1, 1, layerOf("a", 1, 1, `id="1"`, "1")), "a.tmx")
	pathB := tiledMap(t, twoTileTSX, mapOf(1, 1, layerOf("b", 1, 1, `id="1"`, "2")), "b.tmx")
	var gridB *TileGrid
	for _, path := range []string{pathA, pathB} {
		grid, _, err := LoadMap(context.Background(), svc, db, path)
		if err != nil {
			t.Fatal(err)
		}
		if path == pathB {
			gridB = grid
		}
	}
	if n := tileCount(t, db); n != 2 {
		t.Fatalf("two maps with the same layer ID have %d tiles, want two", n)
	}
	var foreignTile int64
	if err := db.QueryRow(`SELECT entity_id FROM comp_tilelayer WHERE map_id=?`, pathA).Scan(&foreignTile); err != nil {
		t.Fatal(err)
	}
	space, err := gridB.SpaceFrom(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := space.Entity(ownedArtID(t, db, foreignTile)); found {
		t.Fatal("map-b spatial snapshot contains map-a's owned referenced artwork/rules")
	}
	if err := os.WriteFile(pathA, []byte(mapOf(1, 1, layerOf("a", 1, 1, `id="1"`, "0"))), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(context.Background(), svc, db, pathA); err != nil {
		t.Fatal(err)
	}
	if draws, err := ReadTileDraws(context.Background(), db, pathB); err != nil || len(draws) != 1 || tileCount(t, db) != 1 {
		t.Fatalf("import of map A deleted map B's tile: %v, %v", draws, err)
	}
}

func TestLoadMap_InvalidLayerIDsRefuseBeforeWriting(t *testing.T) {
	tests := []struct {
		name   string
		layers []string
	}{
		{name: "missing", layers: []string{layerOf("floor", 1, 1, "", "1")}},
		{name: "zero", layers: []string{layerOf("floor", 1, 1, `id="0"`, "1")}},
		{name: "duplicate", layers: []string{layerOf("floor", 1, 1, `id="2"`, "1"), layerOf("wall", 1, 1, `id="2"`, "2")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, db := syncFixture(t)
			// Do not use mapOf: it assigns IDs to older test fixtures that do
			// not exercise validation of missing ones.
			file := `<map orientation="orthogonal" width="1" height="1" tilewidth="8" tileheight="8">` +
				`<tileset firstgid="1" source="dungeon.tsx"/>` + strings.Join(tt.layers, "") + `</map>`
			path := tiledMap(t, twoTileTSX, file, "level.tmx")
			if _, _, err := LoadMap(context.Background(), svc, db, path); err == nil || !strings.Contains(err.Error(), "ID") {
				t.Fatalf("LoadMap error = %v, want layer ID refusal", err)
			}
			if n := tileCount(t, db); n != 0 {
				t.Fatalf("failed import wrote %d tile entities", n)
			}
		})
	}
}

func TestLoadMap_MissingVisualRefusesWithoutReplacingIdentity(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	id := layerTileID(t, db, 1)
	if _, err := db.Exec(`DELETE FROM comp_tilevisual WHERE entity_id = ?`, ownedArtID(t, db, id)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(context.Background(), svc, db, path); err == nil || !strings.Contains(err.Error(), "TileVisual") {
		t.Fatalf("LoadMap = %v, want missing visual refusal", err)
	}
	if n := tileCount(t, db); n != 1 || layerTileID(t, db, 1) != id {
		t.Fatal("missing visual caused a previously identified tile to be replaced")
	}
}

func TestLoadMap_TwoOwnedVisualEntitiesCannotEraseTheirTileOnReimport(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	tileID := layerTileID(t, db, 1)
	oldFloor := ownedArtID(t, db, tileID)
	row, err := db.Exec(`INSERT INTO entities(entity_type, created_tick) VALUES ('Floor',0)`)
	if err != nil {
		t.Fatal(err)
	}
	extraFloor, err := row.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO comp_tileentityowner(entity_id,target_entity_id) VALUES (?,?)`, extraFloor, tileID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO comp_tilevisual(entity_id,image,source_x,source_y,source_w,source_h,
		dest_x,dest_y,flip_h,flip_v,flip_d,alpha,visible)
		SELECT ?, image, source_x, source_y, source_w, source_h,
		dest_x, dest_y, flip_h, flip_v, flip_d, alpha, visible FROM comp_tilevisual WHERE entity_id=?`, extraFloor, oldFloor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(context.Background(), svc, db, path); err == nil || !strings.Contains(err.Error(), "multiple owned") {
		t.Fatalf("LoadMap = %v, want ambiguous owner refusal", err)
	}
	if tileCount(t, db) != 1 || layerTileID(t, db, 1) != tileID {
		t.Fatal("duplicate owned visuals caused the Tile entity to be removed")
	}
}

func TestLoadMap_RepairingDuplicateTileDeletesItsOwnedArtAndSnapshot(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	wantedTileID := layerTileID(t, db, 1)
	wantedArtID := ownedArtID(t, db, wantedTileID)
	tile := LayerTile{
		MapID: path, LayerID: 1, X: 0, Y: 0, CellWidth: 8, CellHeight: 8, Opacity: 1, Visible: true,
		State: TileState{TileType: "floor", EntityType: "Floor"}, Visual: tiled.Draw{Image: "duplicate.png", SW: 8, SH: 8, Alpha: 1},
	}
	duplicate, err := svc.CreateEntity(context.Background(), "Tile", []world.EntityComponent{
		{Name: "Tile", Values: tileValues(Point{}, tile.State)},
		{Name: "TileLayer", Values: layerValues(tile)},
		{Name: "Position", Values: world.ComponentValues{"x": 0, "y": 0}},
		{Name: "TileReferences", Values: world.ComponentValues{"value": []int64{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	art, err := svc.CreateEntity(context.Background(), "Floor", []world.EntityComponent{
		{Name: "TileVisual", Values: visualValues(tile)},
		{Name: "Passability", Values: world.ComponentValues{"kind": "open"}},
		{Name: "Visibility", Values: world.ComponentValues{"kind": "clear"}},
		{Name: "TileEntityOwner", Values: world.ComponentValues{"target_entity_id": duplicate.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.InTx(context.Background(), func(tx world.Tx) error {
		if err := tx.SetComponentValues(context.Background(), duplicate.ID, "TileReferences", world.ComponentValues{"value": []int64{art.ID}}); err != nil {
			return err
		}
		return tx.SetTileArtComponents(context.Background(), art.ID, []string{"TileVisual", "TileEntityOwner", "Passability", "Visibility"})
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	if layerTileID(t, db, 1) != wantedTileID || ownedArtID(t, db, wantedTileID) != wantedArtID {
		t.Fatal("repair replaced the original Tile or its art")
	}
	for _, id := range []int64{duplicate.ID, art.ID} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id=?`, id).Scan(&count); err != nil || count != 0 {
			t.Fatalf("discarded duplicate entity %d survived: %d, %v", id, count, err)
		}
	}
	var metadata int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tile_art_components WHERE entity_id=?`, art.ID).Scan(&metadata); err != nil || metadata != 0 {
		t.Fatalf("orphaned authored component snapshot: %d, %v", metadata, err)
	}
}

func TestLoadMap_MigratesOldVisualOffTheTileWithoutReplacingTileID(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("ground", 1, 1, `id="1"`, "1")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	tileID := layerTileID(t, db, 1)
	oldVisual := ownedArtID(t, db, tileID)
	if err := svc.InTx(context.Background(), func(tx world.Tx) error {
		return tx.DeleteEntity(context.Background(), oldVisual)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM comp_tilereferences WHERE entity_id=?`, tileID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO comp_tilevisual VALUES (?, 'old.png', 0,0,8,8,0,0,0,0,0,1,1)`, tileID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	if layerTileID(t, db, 1) != tileID || ownedArtID(t, db, tileID) == oldVisual {
		t.Fatal("old visual migration replaced Tile identity or reused deleted art ID")
	}
	var directlyAttached int
	if err := db.QueryRow(`SELECT COUNT(*) FROM comp_tilevisual WHERE entity_id=?`, tileID).Scan(&directlyAttached); err != nil || directlyAttached != 0 {
		t.Fatalf("legacy TileVisual still attached to Tile: %d,%v", directlyAttached, err)
	}
}

func TestLoadMap_RepairsMissingTileReferenceComponentWithoutReplacingIdentity(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	id := layerTileID(t, db, 1)
	if _, err := db.Exec(`DELETE FROM comp_tilereferences WHERE entity_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatalf("reimport refused repairable TileReferences: %v", err)
	}
	if layerTileID(t, db, 1) != id {
		t.Fatal("repair replaced the Tile entity")
	}
	var raw string
	if err := db.QueryRow(`SELECT value FROM comp_tilereferences WHERE entity_id=?`, id).Scan(&raw); err != nil || raw != fmt.Sprintf("[%d]", ownedArtID(t, db, id)) {
		t.Fatalf("repaired TileReferences = %q,%v; want owned entity", raw, err)
	}
}

func TestLoadMap_MissingTileComponentRefusesWithoutDuplicatingEntity(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	id := layerTileID(t, db, 1)
	if _, err := db.Exec(`DELETE FROM comp_tile WHERE entity_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(context.Background(), svc, db, path); err == nil || !strings.Contains(err.Error(), "Tile component") {
		t.Fatalf("LoadMap = %v, want missing Tile component refusal", err)
	}
	if tileCount(t, db) != 1 || layerTileID(t, db, 1) != id {
		t.Fatal("missing Tile component duplicated or replaced the entity")
	}
}

func TestLoadMap_IdenticalLayerImportWritesNothing(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE visual_writes (entity_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER watch_visual AFTER UPDATE ON comp_tilevisual BEGIN INSERT INTO visual_writes VALUES (NEW.entity_id); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	var writes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM visual_writes`).Scan(&writes); err != nil || writes != 0 {
		t.Fatalf("unchanged tile visuals written %d time(s): %v", writes, err)
	}
}

func TestLoadMap_LayerImportRollsBackEveryTileWhenAWriteFails(t *testing.T) {
	svc, db := syncFixture(t)
	if _, err := db.Exec(`CREATE TRIGGER refuse_second_tile BEFORE INSERT ON comp_tile
		WHEN NEW.x = 1 BEGIN SELECT RAISE(ABORT, 'deliberately refused'); END`); err != nil {
		t.Fatal(err)
	}
	path := tiledMap(t, twoTileTSX, mapOf(2, 1,
		layerOf("ground", 2, 1, `id="1"`, "1,1")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err == nil || !strings.Contains(err.Error(), "deliberately refused") {
		t.Fatalf("LoadMap = %v, want tile insert refusal", err)
	}
	if n := tileCount(t, db); n != 0 {
		t.Fatalf("failed import committed %d of two tile entities", n)
	}
}

func TestLoadMap_RejectsTilesetEntityTemplateMistakesBeforeCommittingTiles(t *testing.T) {
	for _, tt := range []struct {
		name string
		tsx  string
		want string
	}{
		{"unknown entity type", strings.Replace(twoTileTSX, `name="entityType" value="Floor"`, `name="entityType" value="Unshipped"`, 1), "Unshipped"},
		{"missing required visibility", strings.Replace(twoTileTSX, `<property name="Visibility.kind" value="clear"/>`, "", 1), "Visibility"},
		{"unknown taxonomy category", strings.Replace(twoTileTSX, `name="Passability.kind" value="open"`, `name="Passability.kind" value="opne"`, 1), "opne"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, db := syncFixture(t)
			path := tiledMap(t, tt.tsx, mapOf(2, 1, layerOf("ground", 2, 1, `id="1"`, "2,1")), "level.tmx")
			if _, _, err := LoadMap(context.Background(), svc, db, path); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadMap = %v, want %q", err, tt.want)
			}
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type IN ('Tile','Floor','Wall')`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("invalid template committed %d entities: %v", count, err)
			}
		})
	}
}

func TestLoadMap_EditorOnlyTileMetadataDoesNotBecomeAnEntityComponent(t *testing.T) {
	svc, db := syncFixture(t)
	tsx := strings.Replace(twoTileTSX, `<property name="entityType" value="Floor"/>`,
		`<property name="entityType" value="Floor"/><property name="artistNote" value="do not import"/>`, 1)
	path := tiledMap(t, tsx, mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatalf("editor-only TSX property prevented a valid Floor import: %v", err)
	}
	if tileCount(t, db) != 1 {
		t.Fatal("Tile vanished because TSX had an artist note")
	}
}

func TestReadTileDraws_RuntimeVisualChangesAndDeletion(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("ground", 1, 1, `id="11"`, "1")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	assertDraw := func(wantImage string, wantCount int) {
		t.Helper()
		draws, err := ReadTileDraws(context.Background(), db, path)
		if err != nil {
			t.Fatal(err)
		}
		if len(draws) != wantCount {
			t.Fatalf("draws = %v, want %d", draws, wantCount)
		}
		if wantCount > 0 && draws[0].Image != wantImage {
			t.Fatalf("draw image = %q, want %q", draws[0].Image, wantImage)
		}
	}
	id := layerTileID(t, db, 11)
	artID := ownedArtID(t, db, id)
	if _, err := db.Exec(`UPDATE comp_tilevisual SET image = 'changed.png' WHERE entity_id = ?`, artID); err != nil {
		t.Fatal(err)
	}
	assertDraw("changed.png", 1)
	if _, err := db.Exec(`UPDATE comp_tilevisual SET visible = 0 WHERE entity_id = ?`, artID); err != nil {
		t.Fatal(err)
	}
	assertDraw("", 0)
	if err := svc.InTx(context.Background(), func(tx world.Tx) error { return tx.DeleteEntity(context.Background(), id) }); err != nil {
		t.Fatal(err)
	}
	assertDraw("", 0)
}

func TestReadTileDraws_LayerOrderHiddenTilesAndFlips(t *testing.T) {
	svc, db := syncFixture(t)
	path := tiledMap(t, twoTileTSX, mapOf(1, 1,
		layerOf("floor", 1, 1, `id="1"`, "1"),
		layerOf("hidden", 1, 1, `id="2" visible="0"`, "1"),
		layerOf("upper", 1, 1, `id="3" opacity="0.5"`, "2147483650"),
		layerOf("transparent", 1, 1, `id="4" opacity="0"`, "1")), "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
		t.Fatal(err)
	}
	draws, err := ReadTileDraws(context.Background(), db, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(draws) != 2 || draws[0].SX != 0 || draws[1].SX != 8 || !draws[1].FlipH || draws[1].Alpha != 0.5 {
		t.Fatalf("draw order, source, flip or alpha lost: %+v", draws)
	}
	if tileCount(t, db) != 4 {
		t.Fatal("hidden or transparent authored tile did not survive import")
	}
}
