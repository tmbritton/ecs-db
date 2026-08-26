package game_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/game"
	"github.com/tmbritton/ecs-db/internal/renderer"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/tilemap"
	"github.com/tmbritton/ecs-db/internal/world"
)

// The files this repository actually ships, loaded the way cmd/ecs-db loads
// them.
//
// Every other test in this epic runs against a fixture written for it, and each
// one can pass while the game is broken: a tileset that does not resolve, a gid
// that is off by one, a spawn whose properties the schema refuses, a behaviour
// binding naming a machine nobody shipped. All of those are one file's contents
// and none of them are a Go change, so nothing else in the suite would notice.
//
// The paths come from game.toml, read the way `ecs-db run -c` reads it, because
// "game.toml points at the new map" is half of what this story had to get right
// and a hardcoded path here asserts none of it: reverting [map] to level1.toml,
// or setting [window] tileSize back to 16, would leave this file green and the
// game refusing to start.
const configPath = "../../game.toml"

func shippedConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("loading %s: %v", configPath, err)
	}
	if cfg.Map.Path == "" {
		t.Fatal("game.toml configures no map, so the engine starts with no world at all")
	}
	return cfg
}

func loadShippedProject(t *testing.T) (*storage.SQLiteStore, schema.DatabaseSchema) {
	t.Helper()
	cfg := shippedConfig(t)
	ds, err := schema.InitSchema(cfg.Schema.Path)
	if err != nil {
		t.Fatalf("loading the shipped schema: %v", err)
	}
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "level.sqlite"), ds, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	grid, src, err := tilemap.LoadMap(context.Background(), svc, store.DB(), cfg.Map.Path)
	if err != nil {
		t.Fatalf("loading %s: %v", cfg.Map.Path, err)
	}
	if !src.Drawable() {
		t.Error("the shipped map resolves no tileset, so the game draws nothing")
	}
	// The picture itself, which nothing else here would notice. Resolving a
	// tileset opens the .tsx and not the image it names — the image is opened
	// by the renderer, which is behind //go:build ebitengine and which no test
	// can compile. So renaming or losing starter.png leaves every assertion in
	// this file passing and the game drawing a black screen.
	//
	// Checked here rather than in the loader: a map fixture that names an image
	// nobody wrote is how most of this epic's tests are built, and refusing one
	// would be refusing a map for something only the renderer needs.
	for _, ref := range src.Tilesets {
		if ref.Tileset == nil || ref.Tileset.Image.Path == "" {
			continue
		}
		if _, err := os.Stat(ref.Tileset.Image.Path); err != nil {
			t.Errorf("tileset %q names an image that is not there: %v", ref.Tileset.Name, err)
		}
	}
	if src.Properties.Get("mapId") == "" {
		t.Error("the shipped map declares no mapId, so renaming it would spawn its world twice")
	}
	// The layout the character format held, spot-checked at the corners of the
	// two rooms: a map that loaded but drew a different level would pass every
	// other assertion here.
	for _, c := range []struct {
		x, y     int
		passable bool
	}{
		{0, 0, false},   // the wall round the outside
		{19, 14, false}, //
		{1, 1, true},    // the floor just inside it
		{2, 2, true},    // where the player stands
		{15, 12, true},  // where the goblin starts
		{5, 2, false},   // the three-cell block near the top
		{7, 4, false},   // the pillar
		{4, 7, false},   // the long wall
		{9, 10, false},  // the short one
	} {
		if got := grid.IsPassable(c.x, c.y); got != c.passable {
			t.Errorf("(%d,%d) passable = %v, want %v", c.x, c.y, got, c.passable)
		}
	}
	if grid.Width != 20 || grid.Height != 15 {
		t.Errorf("grid = %d×%d, want the 20×15 the character format held", grid.Width, grid.Height)
	}
	// Against the configured size and not against 32, because this is the check
	// run makes: tiles are placed from the map file and entities from
	// window.tileSize, so a disagreement draws the level on one grid and the
	// player on another.
	if src.TileWidth != cfg.Window.TileSize || src.TileHeight != cfg.Window.TileSize {
		t.Errorf("tiles are %d×%d px and window.tileSize is %d; run refuses a mismatch",
			src.TileWidth, src.TileHeight, cfg.Window.TileSize)
	}

	registry := builtins.NewRegistry()
	builtins.RegisterPathfinding(registry, grid)
	builtins.RegisterLineOfSight(registry, grid)
	loader := agent.NewLoader(registry, ds)
	for _, mod := range cfg.Mods {
		if mod.Behaviors == "" {
			continue
		}
		if _, err := loader.ScanDir(mod.Behaviors, mod.Name); err != nil {
			t.Fatalf("loading the behaviors of mod %q: %v", mod.Name, err)
		}
	}

	// The arguments run passes, not plausible stand-ins for them: Tick and
	// TickDurationMs are the two fields whose whole justification is written
	// into BehaviorSync's doc comments, and this is the test claiming fidelity.
	tick, err := store.GetCurrentTick(context.Background())
	if err != nil {
		t.Fatalf("reading the current tick: %v", err)
	}
	res, err := game.SyncBehaviors(context.Background(), game.BehaviorSync{
		DB: store.DB(), Schema: ds, Loader: loader, Registry: registry,
		Tick: tick, TickDurationMs: int64(1000 / renderer.TicksPerSecond),
	})
	if err != nil {
		t.Fatalf("SyncBehaviors: %v", err)
	}
	if len(res.Problems) != 0 {
		t.Fatalf("the shipped project could not start its behaviours: %v", res.Problems)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("the shipped project started a machine it should not have: %v", res.Warnings)
	}
	if res.Started != 1 {
		t.Errorf("started %d machines, want the goblin's one", res.Started)
	}
	return store, ds
}

func TestShippedProject_SpawnsThePlayerAndTheGoblinWhereTheMapPutsThem(t *testing.T) {
	store, _ := loadShippedProject(t)
	db := store.DB()

	for _, want := range []struct {
		entityType     string
		x, y           float64
		hp, maxHP      int
		animation      string
		machine        string
		initialStateIn string
	}{
		{"Player", 2, 2, 10, 10, "player_idle", "", ""},
		{"Goblin", 15, 12, 5, 5, "goblin_idle", "goblin", "idle"},
	} {
		t.Run(want.entityType, func(t *testing.T) {
			// One of it, and the id of that one. QueryRow silently takes the
			// first of however many there are, so a map that spawned the
			// player twice would satisfy every assertion below.
			var id, n int64
			if err := db.QueryRow(
				`SELECT COUNT(*) FROM entities WHERE entity_type = ?`, want.entityType).Scan(&n); err != nil {
				t.Fatalf("counting %s entities: %v", want.entityType, err)
			}
			if n != 1 {
				t.Fatalf("%d %s entities, want exactly one", n, want.entityType)
			}
			if err := db.QueryRow(
				`SELECT id FROM entities WHERE entity_type = ?`, want.entityType).Scan(&id); err != nil {
				t.Fatalf("no %s spawned: %v", want.entityType, err)
			}

			var x, y float64
			if err := db.QueryRow(
				`SELECT x, y FROM comp_position WHERE entity_id = ?`, id).Scan(&x, &y); err != nil {
				t.Fatalf("reading position: %v", err)
			}
			if x != want.x || y != want.y {
				t.Errorf("at (%v,%v), want (%v,%v)", x, y, want.x, want.y)
			}

			var hp, maxHP int
			if err := db.QueryRow(
				`SELECT hp, maxhp FROM comp_health WHERE entity_id = ?`, id).Scan(&hp, &maxHP); err != nil {
				t.Fatalf("reading health: %v", err)
			}
			if hp != want.hp || maxHP != want.maxHP {
				t.Errorf("health %d/%d, want %d/%d", hp, maxHP, want.hp, want.maxHP)
			}

			// All three of Sprite's properties, because the map declares all
			// three: the sheet is left empty on purpose — animations.toml maps
			// an entity type to its sheet and stamps it at start-up — and a
			// spawn that filled it in would quietly override that.
			var animation, sheet string
			var flipX bool
			if err := db.QueryRow(
				`SELECT animation, sheet, flip_x FROM comp_sprite WHERE entity_id = ?`,
				id).Scan(&animation, &sheet, &flipX); err != nil {
				t.Fatalf("reading sprite: %v", err)
			}
			if animation != want.animation {
				t.Errorf("animation %q, want %q", animation, want.animation)
			}
			if sheet != "" {
				t.Errorf("sheet %q, want it left to animations.toml", sheet)
			}
			if flipX {
				t.Error("flip_x is set, and the map says false")
			}

			var machine, states string
			err := db.QueryRow(
				`SELECT machine_id, current_states FROM behavior_components WHERE entity_id = ?`,
				id).Scan(&machine, &states)
			if want.machine == "" {
				if err == nil {
					t.Errorf("runs machine %q, and its type declares none", machine)
				}
				return
			}
			if err != nil {
				t.Fatalf("no machine for the %s: %v", want.entityType, err)
			}
			if machine != want.machine {
				t.Errorf("runs machine %q, want %q", machine, want.machine)
			}
			if !strings.Contains(states, want.initialStateIn) {
				t.Errorf("current_states = %q, want the machine's initial state %q", states, want.initialStateIn)
			}
		})
	}
}

// Loading the shipped project twice is what a second `ecs-db run` is, and it
// must leave the same world: the same entities, at the same ids, running the
// same machines. Every id in behavior_components, event_queue and transitions
// points at one.
func TestShippedProject_ASecondLoadChangesNothing(t *testing.T) {
	store, ds := loadShippedProject(t)
	db := store.DB()

	before := snapshot(t, db)

	svc := world.NewEntityService(store)
	svc.SetSchema(ds)
	cfg := shippedConfig(t)
	grid, _, err := tilemap.LoadMap(context.Background(), svc, db, cfg.Map.Path)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	registry := builtins.NewRegistry()
	builtins.RegisterPathfinding(registry, grid)
	builtins.RegisterLineOfSight(registry, grid)
	loader := agent.NewLoader(registry, ds)
	for _, mod := range cfg.Mods {
		if mod.Behaviors == "" {
			continue
		}
		if _, err := loader.ScanDir(mod.Behaviors, mod.Name); err != nil {
			t.Fatalf("loading behaviors: %v", err)
		}
	}
	res, err := game.SyncBehaviors(context.Background(), game.BehaviorSync{
		DB: db, Schema: ds, Loader: loader, Registry: registry,
		TickDurationMs: int64(1000 / renderer.TicksPerSecond),
	})
	if err != nil {
		t.Fatalf("SyncBehaviors: %v", err)
	}
	if res.Started != 0 || res.Running != 1 {
		t.Errorf("second load: %+v, want nothing started and the goblin still running", res)
	}

	if after := snapshot(t, db); after != before {
		t.Errorf("a second load rebuilt the world:\n before %s\n after  %s", before, after)
	}
}

// snapshot is the identity of everything the map made, which is what a reload
// must not disturb.
func snapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	var out string
	rows, err := db.Query(`SELECT entities.id, entities.entity_type, spawns.map, spawns.object_id
	                         FROM spawns JOIN entities ON entities.id = spawns.entity_id
	                        ORDER BY spawns.object_id`)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var entityType, mapKey string
		var objectID int
		if err := rows.Scan(&id, &entityType, &mapKey, &objectID); err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		out += fmt.Sprintf("%s#%d@%s:%d ", entityType, id, mapKey, objectID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return out
}
