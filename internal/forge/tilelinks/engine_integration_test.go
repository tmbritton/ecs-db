package tilelinks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/tilemap"
	"github.com/tmbritton/ecs-db/internal/world"
)

func TestAuthoredTileLinks_ImportOneRiverWithDistinctPerCellArtAndStableIdentity(t *testing.T) {
	rawSchema, err := os.ReadFile("../../../schema.json")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := schema.LoadSchema(rawSchema)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store, err := storage.NewSQLiteStore(filepath.Join(dir, "world.sqlite"), ds, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatal(err)
	}
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)
	const tsx = `<tileset name="cells" tilewidth="8" tileheight="8" tilecount="2" columns="2">
  <image source="cells.png" width="16" height="8"/>
  <properties><property name="entityType" value="Floor"/><property name="Passability.kind" value="open"/><property name="Visibility.kind" value="clear"/></properties>
</tileset>`
	if err := os.WriteFile(filepath.Join(dir, "cells.tsx"), []byte(tsx), 0o600); err != nil {
		t.Fatal(err)
	}
	const source = `<map orientation="orthogonal" width="2" height="2" tilewidth="8" tileheight="8" infinite="0" nextobjectid="6">
  <properties><property name="mapId" value="river-demo"/></properties>
  <tileset firstgid="1" source="cells.tsx"/>
  <layer id="1" name="ground" width="2" height="2"><data encoding="csv">1,0,
0,2</data></layer>
  <objectgroup id="2" name="spawns"><object id="5" type="River" x="0" y="0"><properties>
    <property name="Passability.kind" value="liquid"/>
    <property name="Visibility.kind" value="clear"/>
  </properties></object></objectgroup>
</map>`
	d, err := tiled.ParseDocument([]byte(source), "river.tmx")
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []Cell{{0, 0}, {1, 1}} {
		if changed, err := Link(d, 5, 1, at); !changed || err != nil {
			t.Fatalf("linking %+v: changed=%v err=%v", at, changed, err)
		}
	}
	path := filepath.Join(dir, "river.tmx")
	check := func(wantDraws int) int64 {
		t.Helper()
		if err := os.WriteFile(path, d.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		grid, _, err := tilemap.LoadMap(context.Background(), svc, store.DB(), path)
		if err != nil {
			t.Fatal(err)
		}
		var id, count int64
		if err := store.DB().QueryRow(`SELECT MIN(id), COUNT(*) FROM entities WHERE entity_type='River'`).Scan(&id, &count); err != nil || count != 1 {
			t.Fatalf("shared River = %d entities, id=%d err=%v", count, id, err)
		}
		draws, err := tilemap.ReadTileDraws(context.Background(), store.DB(), "river-demo")
		if err != nil || len(draws) != wantDraws {
			t.Fatalf("art at %d Tiles = %+v, %v", wantDraws, draws, err)
		}
		if wantDraws == 2 && draws[0].SX == draws[1].SX {
			t.Fatalf("one River erased distinct painted art slices: %+v", draws)
		}
		space, err := grid.SpaceFrom(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if can, err := space.CanEnter(tilemap.SpatialEntity{ID: 99}, tilemap.Point{X: 1, Y: 1}); err != nil || can {
			t.Fatalf("shared River lost its linked cell (1,1): %v, %v", can, err)
		}
		if visible, err := space.CanSee(tilemap.SpatialEntity{ID: 99, Position: tilemap.Point{X: 0, Y: 1}}, tilemap.Point{X: 1, Y: 1}, -1); err != nil || !visible {
			t.Fatalf("river blocked sight despite clear Visibility: %v, %v", visible, err)
		}
		if wantDraws == 1 {
			if can, err := space.CanEnter(tilemap.SpatialEntity{ID: 99}, tilemap.Point{X: 0, Y: 0}); err != nil || !can {
				t.Fatalf("erased River link still blocks its former Tile: %v, %v", can, err)
			}
		}
		return id
	}
	id := check(2)
	if err := Move(d, 5, 1, 0); err != nil {
		t.Fatal(err)
	}
	if moved := check(2); moved != id {
		t.Fatalf("moving authored River changed entity ID %d to %d", id, moved)
	}
	if err := SetLayerData(d, 0, []uint32{0, 0, 0, 2}); err != nil {
		t.Fatal(err)
	}
	if afterErase := check(1); afterErase != id {
		t.Fatalf("erasing one linked Tile changed shared River ID %d to %d", id, afterErase)
	}
}
