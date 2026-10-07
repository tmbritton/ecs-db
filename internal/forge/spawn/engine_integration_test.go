package spawn_test

import (
	"context"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/spawn"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/tilemap"
	"github.com/tmbritton/ecs-db/internal/world"
)

func TestSyncSpawns_ForgePlacedObjectLoadsAsAnEntity(t *testing.T) {
	ds := inspectSchema()
	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.sqlite", *ds, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatal(err)
	}
	svc := world.NewEntityService(store)
	svc.SetSchema(*ds)
	doc, err := tiled.NewDocument("level.tmx", tiled.NewMapSpec{
		MapID: "level", Width: 4, Height: 3, TileWidth: 16, TileHeight: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	objectID, err := spawn.Place(doc, ds, 0, "Goblin", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spawn.SetProperty(doc, ds, objectID, "Health", "hp", "7"); err != nil {
		t.Fatal(err)
	}
	m, err := doc.Map()
	if err != nil {
		t.Fatal(err)
	}
	result, err := tilemap.SyncSpawns(context.Background(), svc, store.DB(), "level.tmx", m)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 1 {
		t.Errorf("object %d created %d entities; problems: %+v", objectID, result.Created, result)
	}
	var x, y, hp int
	if err := store.DB().QueryRow(`SELECT p.x, p.y, h.hp FROM spawns s
		JOIN comp_position p ON p.entity_id = s.entity_id
		JOIN comp_health h ON h.entity_id = s.entity_id WHERE s.object_id = ?`, objectID).Scan(&x, &y, &hp); err != nil {
		t.Fatal(err)
	}
	if x != 2 || y != 1 || hp != 7 {
		t.Errorf("engine spawned object %d at %d,%d with hp %d", objectID, x, y, hp)
	}
}
