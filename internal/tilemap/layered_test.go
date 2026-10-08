package tilemap

import (
	"context"
	"database/sql"
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
	if err := db.QueryRow(`SELECT visible FROM comp_tilevisual WHERE entity_id = ?`, layerTileID(t, db, 12)).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatal("hidden layer tile is drawn")
	}
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
	if _, err := SyncTiles(context.Background(), svc, db, map[Point]TileState{
		{X: 0, Y: 0}: {Passable: true, TileType: "floor"},
	}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.QueryRow(`SELECT entity_id FROM comp_tile`).Scan(&id); err != nil {
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
		MapID: "room", LayerID: 1, State: TileState{Passable: true, TileType: "floor"},
		Visual: tiled.Draw{Image: "floor.png", SW: 8, SH: 8, Alpha: 1}, Visible: true,
	}
	ctx := context.Background()
	if err := svc.InTx(ctx, func(tx world.Tx) error {
		_, err := svc.CreateEntityInTx(ctx, tx, "Tile", []world.EntityComponent{
			{Name: "Tile", Values: tileValues(Point{}, second.State)},
			{Name: "TileLayer", Values: layerValues(second)},
			{Name: "TileVisual", Values: visualValues(second)},
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
	for _, path := range []string{pathA, pathB} {
		if _, _, err := LoadMap(context.Background(), svc, db, path); err != nil {
			t.Fatal(err)
		}
	}
	if n := tileCount(t, db); n != 2 {
		t.Fatalf("two maps with the same layer ID have %d tiles, want two", n)
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
	if _, err := db.Exec(`DELETE FROM comp_tilevisual WHERE entity_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMap(context.Background(), svc, db, path); err == nil || !strings.Contains(err.Error(), "visual") {
		t.Fatalf("LoadMap = %v, want missing visual refusal", err)
	}
	if n := tileCount(t, db); n != 1 || layerTileID(t, db, 1) != id {
		t.Fatal("missing visual caused a previously identified tile to be replaced")
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
	if _, err := db.Exec(`UPDATE comp_tilevisual SET image = 'changed.png' WHERE entity_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	assertDraw("changed.png", 1)
	if _, err := db.Exec(`UPDATE comp_tilevisual SET visible = 0 WHERE entity_id = ?`, id); err != nil {
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
