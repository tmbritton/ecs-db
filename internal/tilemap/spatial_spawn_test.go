package tilemap

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/world"
)

func spatialSpawnSchema() schema.DatabaseSchema {
	s := spawnSchema()
	s.Components["Passability"] = schema.Component{Type: "object", Properties: map[string]schema.Property{"kind": {Type: "string"}}}
	s.Components["Visibility"] = schema.Component{Type: "object", Properties: map[string]schema.Property{"kind": {Type: "string"}}}
	s.Components["OccupiedCells"] = schema.Component{Type: "array", Items: &schema.Property{Type: "object", Properties: map[string]schema.Property{
		"x": {Type: "integer"}, "y": {Type: "integer"},
	}}}
	s.Components["Flying"] = schema.Component{Type: "boolean"}
	s.Components["Swimming"] = schema.Component{Type: "boolean"}
	s.EntityTypes["Wall"] = schema.EntityType{
		RequiredComponents: []string{"Position", "Passability"},
		OptionalComponents: []string{"Visibility", "OccupiedCells", "TileVisual", "TileEntityOwner"},
		ValidationLevel:    schema.ValidationStrict,
	}
	s.EntityTypes["River"] = schema.EntityType{
		RequiredComponents: []string{"Position", "Passability"},
		OptionalComponents: []string{"Visibility", "Swimming", "OccupiedCells", "TileVisual", "TileEntityOwner"},
		ValidationLevel:    schema.ValidationStrict,
	}
	s.Interactions = map[string]map[string]schema.InteractionRule{
		"Passability": {"open": {Open: true}, "solid": {Allows: []string{"Flying"}}, "liquid": {Allows: []string{"Swimming", "Flying"}}},
		"Visibility":  {"clear": {Open: true}, "opaque": {}},
	}
	return s
}

func TestLoadMap_TileLinkReferencesOneWallInsteadOfDuplicatingIt(t *testing.T) {
	ds := spatialSpawnSchema()
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
	file := strings.Replace(mapOf(2, 1, layerOf("ground", 2, 1, `id="1"`, "1,1")), "</map>",
		`<objectgroup id="2"><object id="5" type="Wall" x="8" y="0"><properties>`+
			`<property name="Passability.kind" value="solid"/>`+
			`<property name="TileLink.layerID" type="int" value="1"/>`+
			`</properties></object></objectgroup></map>`, 1)
	path := tiledMap(t, twoTileTSX, file, "level.tmx")
	grid, _, err := LoadMap(context.Background(), svc, store.DB(), path)
	if err != nil {
		t.Fatal(err)
	}
	var wallID int64
	if err := store.DB().QueryRow(`SELECT id FROM entities WHERE entity_type='Wall'`).Scan(&wallID); err != nil {
		t.Fatal(err)
	}
	var references string
	var tileID int64
	if err := store.DB().QueryRow(`SELECT r.entity_id, r.value FROM comp_tilereferences r JOIN comp_tile t ON r.entity_id=t.entity_id WHERE t.x=1 AND t.y=0`).Scan(&tileID, &references); err != nil {
		t.Fatal(err)
	}
	if references != fmt.Sprintf("[%d,%d]", ownedArtID(t, store.DB(), tileID), wallID) {
		t.Fatalf("TileReferences = %q, want owned Floor and Wall ID %d", references, wallID)
	}
	space, err := grid.SpaceFrom(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if can, err := space.CanEnter(SpatialEntity{ID: 99}, Point{1, 0}); err != nil || can {
		t.Fatalf("Tile-linked Wall did not block: %v,%v", can, err)
	}
	if _, err := store.DB().Exec(`UPDATE comp_position SET x=0 WHERE entity_id=?`, wallID); err != nil {
		t.Fatal(err)
	}
	space, err = grid.SpaceFrom(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if can, err := space.CanEnter(SpatialEntity{ID: 99}, Point{0, 0}); err != nil || !can {
		t.Fatalf("referenced Wall also blocked its own Position: %v,%v", can, err)
	}
}

func TestLoadMap_TileLinkCellsShareOneRiverEntity(t *testing.T) {
	ds := spatialSpawnSchema()
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
	file := strings.Replace(mapOf(3, 2, layerOf("ground", 3, 2, `id="1"`, "1,1,1,\n1,1,1")), "</map>",
		`<objectgroup id="2"><object id="5" type="River" x="0" y="0"><properties>`+
			`<property name="Passability.kind" value="liquid"/>`+
			`<property name="Visibility.kind" value="clear"/>`+
			`<property name="TileVisual.image" value="river.png"/>`+
			`<property name="TileVisual.source_x" type="int" value="0"/>`+
			`<property name="TileVisual.source_y" type="int" value="0"/>`+
			`<property name="TileVisual.source_w" type="int" value="8"/>`+
			`<property name="TileVisual.source_h" type="int" value="8"/>`+
			`<property name="TileVisual.dest_x" type="int" value="0"/>`+
			`<property name="TileVisual.dest_y" type="int" value="0"/>`+
			`<property name="TileVisual.flip_h" type="bool" value="false"/>`+
			`<property name="TileVisual.flip_v" type="bool" value="false"/>`+
			`<property name="TileVisual.flip_d" type="bool" value="false"/>`+
			`<property name="TileVisual.alpha" type="float" value="1"/>`+
			`<property name="TileVisual.visible" type="bool" value="true"/>`+
			`<property name="TileLink.layerID" type="int" value="1"/>`+
			`<property name="TileLink.cells" value='[{"x":0,"y":0},{"x":1,"y":0},{"x":1,"y":1}]'/>`+
			`</properties></object></objectgroup></map>`, 1)
	path := tiledMap(t, twoTileTSX, file, "river.tmx")
	grid, _, err := LoadMap(context.Background(), svc, store.DB(), path)
	if err != nil {
		t.Fatal(err)
	}
	var riverID, riverCount int64
	if err := store.DB().QueryRow(`SELECT MIN(id), COUNT(*) FROM entities WHERE entity_type='River'`).Scan(&riverID, &riverCount); err != nil || riverCount != 1 {
		t.Fatalf("River instances = %d, err %v; want one", riverCount, err)
	}
	var links int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM comp_tilereferences r JOIN json_each(r.value) j WHERE CAST(j.value AS INTEGER)=?`, riverID).Scan(&links); err != nil || links != 3 {
		t.Fatalf("references to River %d = %d,%v; want three", riverID, links, err)
	}
	draws, err := ReadTileDraws(context.Background(), store.DB(), path)
	if err != nil {
		t.Fatal(err)
	}
	riverVisuals := 0
	for _, draw := range draws {
		if draw.Image == "river.png" {
			riverVisuals++
		}
	}
	if riverVisuals != 3 {
		t.Fatalf("one River visual was placed at %d linked Tiles, want three", riverVisuals)
	}
	space, err := grid.SpaceFrom(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []Point{{0, 0}, {1, 0}, {1, 1}} {
		if can, err := space.CanEnter(SpatialEntity{ID: 99}, at); err != nil || can {
			t.Errorf("linked River did not block %+v: %v,%v", at, can, err)
		}
	}
	if can, err := space.CanEnter(SpatialEntity{ID: 99}, Point{2, 1}); err != nil || !can {
		t.Errorf("river blocked unlinked cell (2,1): %v,%v", can, err)
	}
}

func TestLoadMap_PaintedRiverTilesShareAuthoredRiverInsteadOfCloningIt(t *testing.T) {
	ds := spatialSpawnSchema()
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
	const riverTSX = `<tileset name="river" tilewidth="8" tileheight="8" tilecount="1" columns="1">
 <image source="river.png" width="8" height="8"/>
 <tile id="0" type="river"><properties>
  <property name="entityType" value="River"/>
  <property name="Passability.kind" value="liquid"/>
  <property name="Visibility.kind" value="clear"/>
 </properties></tile></tileset>`
	file := strings.Replace(mapOf(3, 2, layerOf("water", 3, 2, `id="1"`, "1,1,0,\n0,1,0")), "</map>",
		`<objectgroup id="2"><object id="5" type="River" x="0" y="0"><properties>`+
			`<property name="Passability.kind" value="liquid"/>`+
			`<property name="Visibility.kind" value="clear"/>`+
			`<property name="TileVisual.image" value="river.png"/>`+
			`<property name="TileVisual.source_x" type="int" value="0"/>`+
			`<property name="TileVisual.source_y" type="int" value="0"/>`+
			`<property name="TileVisual.source_w" type="int" value="8"/>`+
			`<property name="TileVisual.source_h" type="int" value="8"/>`+
			`<property name="TileVisual.dest_x" type="int" value="0"/>`+
			`<property name="TileVisual.dest_y" type="int" value="0"/>`+
			`<property name="TileVisual.flip_h" type="bool" value="false"/>`+
			`<property name="TileVisual.flip_v" type="bool" value="false"/>`+
			`<property name="TileVisual.flip_d" type="bool" value="false"/>`+
			`<property name="TileVisual.alpha" type="float" value="1"/>`+
			`<property name="TileVisual.visible" type="bool" value="true"/>`+
			`<property name="TileLink.layerID" type="int" value="1"/>`+
			`<property name="TileLink.cells" value='[{"x":0,"y":0},{"x":1,"y":0},{"x":1,"y":1}]'/>`+
			`</properties></object></objectgroup></map>`, 1)
	path := tiledMap(t, riverTSX, file, "river.tmx")
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err != nil {
		t.Fatal(err)
	}
	var riverID int64
	var count int
	if err := store.DB().QueryRow(`SELECT MIN(id), COUNT(*) FROM entities WHERE entity_type='River'`).Scan(&riverID, &count); err != nil || count != 1 {
		t.Fatalf("painted shared river produced %d River entities: id %d, err %v", count, riverID, err)
	}
	var links int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM comp_tilereferences r JOIN json_each(r.value) j WHERE CAST(j.value AS INTEGER)=?`, riverID).Scan(&links); err != nil || links != 3 {
		t.Fatalf("shared River referenced by %d Tiles, err %v; want three", links, err)
	}
	var owned int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM comp_tileentityowner`).Scan(&owned); err != nil || owned != 0 {
		t.Fatalf("shared River Tiles created %d private copies: %v", owned, err)
	}
	draws, err := ReadTileDraws(context.Background(), store.DB(), path)
	if err != nil || len(draws) != 3 {
		t.Fatalf("one River visual drawn %d times, err %v", len(draws), err)
	}
	writes := watchTiles(t, store.DB())
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err != nil {
		t.Fatalf("re-importing shared River: %v", err)
	}
	if got := writes(); len(got) != 0 {
		t.Fatalf("unchanged shared River re-import rewrote Tiles: %v", got)
	}
	var sameID int64
	if err := store.DB().QueryRow(`SELECT MIN(id), COUNT(*) FROM entities WHERE entity_type='River'`).Scan(&sameID, &count); err != nil || count != 1 || sameID != riverID {
		t.Fatalf("re-import changed shared River %d to %d (%d instances): %v", riverID, sameID, count, err)
	}
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM comp_tilereferences r JOIN json_each(r.value) j WHERE CAST(j.value AS INTEGER)=?`, riverID).Scan(&links); err != nil || links != 3 {
		t.Fatalf("re-import linked shared River to %d Tiles: %v", links, err)
	}
}

func TestLoadMap_SharedPaintedEntityWithoutVisualRefusesBeforeImport(t *testing.T) {
	ds := spatialSpawnSchema()
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
	file := strings.Replace(mapOf(1, 1, layerOf("walls", 1, 1, `id="1"`, "2")), "</map>",
		`<objectgroup id="2"><object id="5" type="Wall" x="0" y="0"><properties>`+
			`<property name="Passability.kind" value="solid"/>`+
			`<property name="TileLink.layerID" type="int" value="1"/>`+
			`</properties></object></objectgroup></map>`, 1)
	path := tiledMap(t, twoTileTSX, file, "walls.tmx")
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err == nil || !strings.Contains(err.Error(), "TileVisual") {
		t.Fatalf("LoadMap with unrendered shared Wall = %v, want TileVisual refusal", err)
	}
	if n := tileCount(t, store.DB()); n != 0 {
		t.Fatalf("unrendered shared Wall committed %d Tiles", n)
	}
}

func TestLoadMap_SharedPaintedEntityWithIncompleteVisualRefusesBeforeImport(t *testing.T) {
	ds := spatialSpawnSchema()
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
	file := strings.Replace(mapOf(1, 1, layerOf("walls", 1, 1, `id="1"`, "2")), "</map>",
		`<objectgroup id="2"><object id="5" type="Wall" x="0" y="0"><properties>`+
			`<property name="Passability.kind" value="solid"/>`+
			`<property name="TileVisual.image" value="wall.png"/>`+
			`<property name="TileLink.layerID" type="int" value="1"/>`+
			`</properties></object></objectgroup></map>`, 1)
	path := tiledMap(t, twoTileTSX, file, "walls.tmx")
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err == nil {
		t.Fatal("LoadMap accepted a shared Wall with an incomplete visual")
	}
	if n := tileCount(t, store.DB()); n != 0 {
		t.Fatalf("unspawnable shared Wall committed %d Tiles", n)
	}
}

func TestLoadMap_TileLinkObjectIDSharedWithOrdinarySpawnRefusesBeforeImport(t *testing.T) {
	ds := spatialSpawnSchema()
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
	file := strings.Replace(mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")), "</map>",
		`<objectgroup id="2">`+
			`<object id="5" type="Wall" x="0" y="0"><properties><property name="Passability.kind" value="solid"/></properties></object>`+
			`<object id="5" type="Wall" x="0" y="0"><properties><property name="Passability.kind" value="solid"/>`+
			`<property name="TileLink.layerID" type="int" value="1"/></properties></object>`+
			`</objectgroup></map>`, 1)
	path := tiledMap(t, twoTileTSX, file, "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate TileLink object ID = %v, want refusal", err)
	}
	if n := tileCount(t, store.DB()); n != 0 {
		t.Fatalf("duplicate TileLink object ID committed %d Tiles", n)
	}
}

func TestLoadMap_SharedPaintedEntityRejectsDifferentCellArtwork(t *testing.T) {
	ds := spatialSpawnSchema()
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
	file := strings.Replace(mapOf(2, 1, layerOf("water", 2, 1, `id="1"`, "1,2")), "</map>",
		`<objectgroup id="2"><object id="5" type="Wall" x="0" y="0"><properties>`+
			`<property name="Passability.kind" value="solid"/>`+
			`<property name="TileVisual.image" value="wall.png"/>`+
			`<property name="TileVisual.source_x" type="int" value="0"/><property name="TileVisual.source_y" type="int" value="0"/>`+
			`<property name="TileVisual.source_w" type="int" value="8"/><property name="TileVisual.source_h" type="int" value="8"/>`+
			`<property name="TileVisual.dest_x" type="int" value="0"/><property name="TileVisual.dest_y" type="int" value="0"/>`+
			`<property name="TileVisual.flip_h" type="bool" value="false"/><property name="TileVisual.flip_v" type="bool" value="false"/>`+
			`<property name="TileVisual.flip_d" type="bool" value="false"/><property name="TileVisual.alpha" type="float" value="1"/>`+
			`<property name="TileVisual.visible" type="bool" value="true"/>`+
			`<property name="TileLink.layerID" type="int" value="1"/>`+
			`<property name="TileLink.cells" value='[{"x":0,"y":0},{"x":1,"y":0}]'/>`+
			`</properties></object></objectgroup></map>`, 1)
	tsx := strings.Replace(twoTileTSX, `name="entityType" value="Floor"`, `name="entityType" value="Wall"`, 1)
	path := tiledMap(t, tsx, file, "river.tmx")
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err == nil || !strings.Contains(err.Error(), "distinct artwork") {
		t.Fatalf("shared Wall with two painted segments = %v, want distinct artwork refusal", err)
	}
	if n := tileCount(t, store.DB()); n != 0 {
		t.Fatalf("shared Wall with different painted art committed %d Tiles", n)
	}
}

func TestLoadMap_DifferentCellArtworkCanShareOneRestrictionOnlyRiver(t *testing.T) {
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
	file := strings.Replace(mapOf(2, 1, layerOf("water", 2, 1, `id="1"`, "1,2")), "</map>",
		`<objectgroup id="2"><object id="5" type="River" x="0" y="0"><properties>`+
			`<property name="Passability.kind" value="liquid"/>`+
			`<property name="Visibility.kind" value="clear"/>`+
			`<property name="TileLink.layerID" type="int" value="1"/>`+
			`<property name="TileLink.cells" value='[{"x":0,"y":0},{"x":1,"y":0}]'/>`+
			`</properties></object></objectgroup></map>`, 1)
	path := tiledMap(t, twoTileTSX, file, "river.tmx")
	grid, _, err := LoadMap(context.Background(), svc, store.DB(), path)
	if err != nil {
		t.Fatal(err)
	}
	var riverCount int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type='River'`).Scan(&riverCount); err != nil || riverCount != 1 {
		t.Fatalf("shared restriction-only River count = %d, %v", riverCount, err)
	}
	draws, err := ReadTileDraws(context.Background(), store.DB(), path)
	if err != nil || len(draws) != 2 || draws[0].SX == draws[1].SX {
		t.Fatalf("two differently painted cells produced artwork %+v, %v", draws, err)
	}
	space, err := grid.SpaceFrom(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for x := 0; x < 2; x++ {
		if can, err := space.CanEnter(SpatialEntity{ID: 99}, Point{x, 0}); err != nil || can {
			t.Errorf("River did not restrict painted cell (%d,0): %v, %v", x, can, err)
		}
	}
}

func TestLoadMap_InvalidTileLinkRefusesBeforeCreatingAnyEntities(t *testing.T) {
	for _, tt := range []struct {
		name, layer, cells, want string
	}{
		{"missing layer", "99", "", "tile layer"},
		{"empty painted cell", "1", "", "empty"},
		{"duplicate footprint", "1", `[{"x":0,"y":0},{"x":0,"y":0}]`, "duplicate"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ds := spatialSpawnSchema()
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
			cellProperty := ""
			if tt.cells != "" {
				cellProperty = `<property name="TileLink.cells" value='` + tt.cells + `'/>`
			}
			file := strings.Replace(mapOf(2, 1, layerOf("ground", 2, 1, `id="1"`, "1,0")), "</map>",
				`<objectgroup id="2"><object id="5" type="Wall" x="8" y="0"><properties>`+
					`<property name="Passability.kind" value="solid"/>`+
					`<property name="TileLink.layerID" type="int" value="`+tt.layer+`"/>`+
					cellProperty+`</properties></object></objectgroup></map>`, 1)
			path := tiledMap(t, twoTileTSX, file, "level.tmx")
			if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadMap = %v, want %q", err, tt.want)
			}
			if n := tileCount(t, store.DB()); n != 0 {
				t.Fatalf("bad TileLink committed %d Tiles", n)
			}
		})
	}
}

func TestTileLinksOfMap_UsesCaseInsensitiveReservedMetadata(t *testing.T) {
	m := spawnMap(tiled.Object{
		ID: 7, Type: "Wall", X: 0, Y: 0,
		Properties: tiled.Properties{"tilelink.LAYERID": {Value: "2"}},
	})
	m.Width, m.Height = 1, 1
	m.Layers = []tiled.Layer{{ID: 2, Name: "art", Width: 1, Height: 1, Data: []uint32{1}}}
	links, err := tileLinksOfMap(m)
	if err != nil || len(links) != 1 || links[0].layerID != 2 || links[0].objectID != 7 {
		t.Fatalf("case-insensitive TileLink metadata = %+v,%v; want one link", links, err)
	}
}

func TestLoadMap_OneRiverEntityMovesItsIrregularFootprintAndKeepsIdentityOnReimport(t *testing.T) {
	ds := spatialSpawnSchema()
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
	base := mapOf(4, 3, layerOf("ground", 4, 3, `id="1"`, "1,1,1,1,\n1,1,1,1,\n1,1,1,1"))
	file := strings.Replace(base, "</map>", `<objectgroup id="2" name="terrain">
 <object id="5" type="River" x="8" y="8"><properties>
 <property name="Passability.kind" value="liquid"/>
 <property name="OccupiedCells.value" value='[{"x":0,"y":0},{"x":1,"y":0},{"x":0,"y":1}]'/>
 </properties></object></objectgroup></map>`, 1)
	path := tiledMap(t, twoTileTSX, file, "river.tmx")
	grid, _, err := LoadMap(context.Background(), svc, store.DB(), path)
	if err != nil {
		t.Fatal(err)
	}
	var riverID int64
	if err := store.DB().QueryRow(`SELECT id FROM entities WHERE entity_type='River'`).Scan(&riverID); err != nil {
		t.Fatal(err)
	}
	if err := svc.AttachComponent(context.Background(), riverID, "Swimming", world.ComponentValues{"value": true}); err != nil {
		t.Fatal(err)
	}
	check := func(cell Point, want bool) {
		t.Helper()
		s, err := grid.SpaceFrom(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		can, err := s.CanEnter(SpatialEntity{ID: -1}, cell)
		if err != nil || can != want {
			t.Fatalf("cell %+v enterable=%v,%v; want %v", cell, can, err, want)
		}
	}
	check(Point{1, 1}, false)
	check(Point{2, 1}, false)
	check(Point{1, 2}, false)
	check(Point{2, 2}, true)
	if _, err := store.DB().Exec(`UPDATE comp_position SET x=0 WHERE entity_id=?`, riverID); err != nil {
		t.Fatal(err)
	}
	check(Point{0, 1}, false)
	check(Point{2, 1}, true)
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err != nil {
		t.Fatal(err)
	}
	var after int64
	if err := store.DB().QueryRow(`SELECT id FROM entities WHERE entity_type='River'`).Scan(&after); err != nil || after != riverID {
		t.Fatalf("reimport replaced river ID %d with %d: %v", riverID, after, err)
	}
	var swimming bool
	if err := store.DB().QueryRow(`SELECT value FROM comp_swimming WHERE entity_id=?`, riverID).Scan(&swimming); err != nil || !swimming {
		t.Fatalf("reimport dropped runtime component: %v,%v", swimming, err)
	}
	check(Point{2, 1}, false) // authored origin wins again after re-import
	for _, stmt := range []string{
		`CREATE TABLE river_writes (entity_id INTEGER)`,
		`CREATE TRIGGER watch_river_position AFTER UPDATE ON comp_position BEGIN INSERT INTO river_writes VALUES (NEW.entity_id); END`,
		`CREATE TRIGGER watch_river_cells AFTER UPDATE ON comp_occupiedcells BEGIN INSERT INTO river_writes VALUES (NEW.entity_id); END`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err != nil {
		t.Fatal(err)
	}
	var writes int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM river_writes`).Scan(&writes); err != nil || writes != 0 {
		t.Fatalf("unchanged river re-import wrote %d components: %v", writes, err)
	}
}

func TestLoadMap_SpatialSnapshotOnlyUsesTheActiveMapsAuthoredOccupants(t *testing.T) {
	ds := spatialSpawnSchema()
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
	load := func(id string, wallX int) *TileGrid {
		t.Helper()
		file := mapOf(3, 1, layerOf("ground", 3, 1, `id="1"`, "1,1,1"))
		file = strings.Replace(file, `<tileset firstgid`, `<properties><property name="mapId" value="`+id+`"/></properties><tileset firstgid`, 1)
		file = strings.Replace(file, "</map>", fmt.Sprintf(`<objectgroup id="2"><object id="1" type="Wall" x="%d" y="0"><properties>`+
			`<property name="Passability.kind" value="solid"/></properties></object></objectgroup></map>`, wallX*8), 1)
		path := tiledMap(t, twoTileTSX, file, id+".tmx")
		grid, _, err := LoadMap(context.Background(), svc, store.DB(), path)
		if err != nil {
			t.Fatal(err)
		}
		return grid
	}
	a := load("map-a", 1)
	b := load("map-b", 2)
	for _, tt := range []struct {
		name          string
		grid          *TileGrid
		free, blocked Point
	}{
		{"map-a", a, Point{2, 0}, Point{1, 0}},
		{"map-b", b, Point{1, 0}, Point{2, 0}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, err := tt.grid.SpaceFrom(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, cell := range []struct {
				at   Point
				want bool
			}{{tt.free, true}, {tt.blocked, false}} {
				can, err := s.CanEnter(SpatialEntity{ID: 999}, cell.at)
				if err != nil || can != cell.want {
					t.Errorf("%v enterable=%v,%v; want %v", cell.at, can, err, cell.want)
				}
			}
		})
	}
}

func TestLoadMap_InvalidSpatialSpawnRefusesBeforeImportingTiles(t *testing.T) {
	ds := spatialSpawnSchema()
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
	base := mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1"))
	bad := strings.Replace(base, "</map>", `<objectgroup id="2" name="walls">
 <object id="1" type="Wall" x="0" y="0"><properties>
 <property name="Passability.kind" value="solidd"/>
 </properties></object></objectgroup></map>`, 1)
	path := tiledMap(t, twoTileTSX, bad, "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err == nil || !strings.Contains(err.Error(), "solidd") {
		t.Fatalf("LoadMap = %v, want invalid spatial rule refusal", err)
	}
	if tileCount(t, store.DB()) != 0 {
		t.Fatal("invalid spatial object left committed tile entities")
	}
	// The saved authored file is unchanged by the refusal.
	if contents, err := os.ReadFile(path); err != nil || string(contents) != bad {
		t.Fatalf("refused import edited map file: %v", err)
	}
}

func TestLoadMap_SpatialSpawnMissingRequiredComponentRefusesBeforeTiles(t *testing.T) {
	ds := spatialSpawnSchema()
	wall := ds.EntityTypes["Wall"]
	wall.RequiredComponents = append(wall.RequiredComponents, "Visibility")
	wall.OptionalComponents = []string{"OccupiedCells"}
	ds.EntityTypes["Wall"] = wall
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
	file := strings.Replace(mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")), "</map>",
		`<objectgroup id="2"><object id="1" type="Wall" x="0" y="0"><properties>`+
			`<property name="Passability.kind" value="solid"/>`+
			`</properties></object></objectgroup></map>`, 1)
	path := tiledMap(t, twoTileTSX, file, "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err == nil || !strings.Contains(err.Error(), "Visibility") {
		t.Fatalf("LoadMap = %v, want missing Visibility refusal", err)
	}
	if tileCount(t, store.DB()) != 0 {
		t.Fatal("missing Wall visibility left the art committed without its blocker")
	}
}

func TestLoadMap_RequiredSpatialTypeWithoutAnySpatialPropertiesRefusesBeforeTiles(t *testing.T) {
	ds := spatialSpawnSchema()
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
	file := strings.Replace(mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")), "</map>",
		`<objectgroup id="2"><object id="1" type="Wall" x="0" y="0"/></objectgroup></map>`, 1)
	path := tiledMap(t, twoTileTSX, file, "level.tmx")
	if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err == nil || !strings.Contains(err.Error(), "Passability") {
		t.Fatalf("LoadMap = %v, want missing required spatial component refusal", err)
	}
	if tileCount(t, store.DB()) != 0 {
		t.Fatal("invalid Wall left tile artwork committed without its occupant")
	}
}

func TestLoadMap_SpatialPropertyTyposRefuseBeforeTiles(t *testing.T) {
	for _, tt := range []struct {
		name, property, value, want string
	}{
		{"case-insensitive category", "passability.KIND", "solidd", "solidd"},
		{"misspelled optional field", "Passability.knd", "solid", "knd"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ds := spatialSpawnSchema()
			ds.EntityTypes["Decor"] = schema.EntityType{
				RequiredComponents: []string{"Position"},
				OptionalComponents: []string{"Passability"},
				ValidationLevel:    schema.ValidationStrict,
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
			file := strings.Replace(mapOf(1, 1, layerOf("ground", 1, 1, `id="1"`, "1")), "</map>",
				`<objectgroup id="2"><object id="1" type="Decor" x="0" y="0"><properties>`+
					`<property name="`+tt.property+`" value="`+tt.value+`"/>`+
					`</properties></object></objectgroup></map>`, 1)
			path := tiledMap(t, twoTileTSX, file, "level.tmx")
			if _, _, err := LoadMap(context.Background(), svc, store.DB(), path); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadMap = %v, want spatial typo %q refusal", err, tt.want)
			}
			if tileCount(t, store.DB()) != 0 {
				t.Fatal("invalid optional restriction committed Tile artwork")
			}
		})
	}
}

func TestLoadMap_BindsLiveOccupantsIndependentlyOfTileArtwork(t *testing.T) {
	ds := spatialSpawnSchema()
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
	base := mapOf(2, 1, layerOf("ground", 2, 1, `id="1"`, "1,1"))
	file := strings.Replace(base, "</map>", `<objectgroup id="2" name="walls">
 <object id="1" type="Wall" x="8" y="0"><properties>
 <property name="Passability.kind" value="solid"/>
 </properties></object></objectgroup></map>`, 1)
	path := tiledMap(t, twoTileTSX, file, "level.tmx")
	grid, _, err := LoadMap(context.Background(), svc, store.DB(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := grid.EntityAt(1, 0); !exists {
		t.Fatal("fixture has no art at (1,0); occupant test proves nothing")
	}
	snapshot, err := grid.SpaceFrom(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if can, err := snapshot.CanEnter(SpatialEntity{ID: 999}, Point{1, 0}); err != nil || can {
		t.Fatalf("wall did not restrict enterable artwork: %v,%v", can, err)
	}
	if _, err := store.DB().Exec(`UPDATE comp_position SET x=0 WHERE entity_id IN (SELECT entity_id FROM comp_passability)`); err != nil {
		t.Fatal(err)
	}
	snapshot, err = grid.SpaceFrom(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if can, err := snapshot.CanEnter(SpatialEntity{ID: 999}, Point{1, 0}); err != nil || !can {
		t.Fatalf("moving wall left stale obstacle at (1,0): %v,%v", can, err)
	}
}

func TestValidateSpawn_RejectsUnknownRestrictionAndInvalidFootprint(t *testing.T) {
	s := spatialSpawnSchema()
	tests := []struct {
		name       string
		properties tiled.Properties
		wantErr    string
	}{
		{name: "valid wall", properties: tiled.Properties{"Passability.kind": {Value: "solid"}}},
		{name: "unknown taxonomy value", properties: tiled.Properties{"Passability.kind": {Value: "solidd"}}, wantErr: "solidd"},
		{name: "unknown visibility value", properties: tiled.Properties{"Passability.kind": {Value: "solid"}, "Visibility.kind": {Value: "transparant"}}, wantErr: "transparant"},
		{name: "irregular footprint", properties: tiled.Properties{"Passability.kind": {Value: "solid"}, "OccupiedCells.value": {Value: `[{"x":0,"y":0},{"x":2,"y":1}]`}}},
		{name: "duplicate footprint cell", properties: tiled.Properties{"Passability.kind": {Value: "solid"}, "OccupiedCells.value": {Value: `[{"x":0,"y":0},{"x":0,"y":0}]`}}, wantErr: "duplicate"},
		{name: "offmap footprint cell", properties: tiled.Properties{"Passability.kind": {Value: "solid"}, "OccupiedCells.value": {Value: `[{"x":99,"y":0}]`}}, wantErr: "outside"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := tiled.Object{ID: 1, Type: "Wall", X: 16, Y: 16, Properties: tt.properties}
			_, err := ValidateSpawn(&s, spawnMap(obj), obj)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("valid spawn refused: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("ValidateSpawn = %v, want error %q", err, tt.wantErr)
			}
		})
	}
}
