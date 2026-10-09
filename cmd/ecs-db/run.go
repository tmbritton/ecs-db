//go:build ebitengine

package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/spf13/cobra"

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

const tickDurationMs = int64(1000 / renderer.TicksPerSecond)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the game",
	RunE:  runGame,
}

func init() { rootCmd.AddCommand(runCmd) }

func runGame(cmd *cobra.Command, args []string) error {
	cfg := config.Get()

	schemaBytes, err := os.ReadFile(cfg.Schema.Path)
	if err != nil {
		return fmt.Errorf("loading schema: %w", err)
	}
	dbSchema, err := schema.LoadSchema(schemaBytes)
	if err != nil {
		return fmt.Errorf("parsing schema: %w", err)
	}
	if err := schema.ValidateSchema(dbSchema); err != nil {
		return fmt.Errorf("validating schema: %w", err)
	}

	hash := sha256.Sum256(schemaBytes)
	schemaHash := hex.EncodeToString(hash[:])

	store, err := storage.NewSQLiteStore(cfg.Database.Path, dbSchema, schemaHash)
	if err != nil {
		return fmt.Errorf("initializing database: %w", err)
	}
	defer func() { _ = store.Close() }()

	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		return fmt.Errorf("ensuring interpreter tables: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := world.NewEntityService(store)
	svc.SetSchema(dbSchema)

	// One image cache for the whole process. Created here rather than beside
	// the sprite renderer that used to own it, because the tilemap now loads
	// through it too and a second cache would decode the same PNGs twice and
	// miss half the hot-reload evictions.
	imageCache := renderer.NewImageCache()

	var (
		grid *tilemap.TileGrid
		tr   *renderer.TilemapRenderer
	)
	if cfg.Map.Path != "" {
		g, src, err := tilemap.LoadMap(ctx, svc, store.DB(), cfg.Map.Path)
		if err != nil {
			return fmt.Errorf("loading map: %w", err)
		}
		grid = g
		// The map's own cell size has to be the one everything else uses.
		//
		// Tiles are placed from the map file now; entities are placed from
		// window.tileSize, as they always were. Before this they could not
		// disagree, because both came from the config. A 32px map in a 16px
		// project would now draw its tiles on one grid and its player on
		// another, with nothing to say so — so it is refused, naming both
		// numbers, rather than half-drawn.
		if src.TileWidth != cfg.Window.TileSize || src.TileHeight != cfg.Window.TileSize {
			return fmt.Errorf(
				"map %s has %dx%d pixel tiles and window.tileSize is %d; "+
					"entities are placed on the second and tiles on the first, so they must agree",
				cfg.Map.Path, src.TileWidth, src.TileHeight, cfg.Window.TileSize)
		}
		// The renderer reads current tile entities; the parsed map is used only
		// for startup validation and the same stable identity as the importer.
		mapID := tilemap.MapID(cfg.Map.Path, src)
		t, err := renderer.NewTilemapRenderer(store.DB(), mapID, imageCache)
		if err != nil {
			return fmt.Errorf("building tilemap renderer: %w", err)
		}
		tr = t
	}

	registry := builtins.NewRegistry()
	if grid != nil {
		builtins.RegisterPathfinding(registry, grid)
		builtins.RegisterLineOfSight(registry, grid)
	}
	loader := agent.NewLoader(registry, dbSchema)

	var watchedDirs []agent.WatchedDir
	for _, mod := range cfg.Mods {
		if mod.Behaviors == "" {
			continue
		}
		n, err := loader.ScanDir(mod.Behaviors, mod.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: loading behaviors for mod %q: %v\n", mod.Name, err)
		}
		if n > 0 {
			fmt.Printf("Loaded %d behavior(s) from mod %q\n", n, mod.Name)
		}
		watchedDirs = append(watchedDirs, agent.WatchedDir{Path: mod.Behaviors, ModName: mod.Name})
	}

	watcher := agent.NewWatcher(loader, watchedDirs, 0)
	// Hot-swapping a machine can leave an entity sitting in a state the new
	// definition no longer has. The Reconciler has handled that since Epic 4
	// and has never had a caller; Forge makes the scenario reachable through
	// the UI, since deleting a state is an ordinary edit.
	watcher.SetReconcileFunc(agent.ReconcileOnReload(ctx, store.DB(), loader, &agent.Reconciler{}))
	go func() {
		if err := watcher.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "watcher: %v\n", err)
		}
	}()

	// Every entity whose type declares a "behavior" gets that machine, which is
	// what makes schema.EntityType.Behavior mean anything. It used to be one
	// hand-written call for the goblin, by name, for one entity.
	//
	// Here rather than where the entity is created, and the reason is the two
	// lines above: a machine's entry actions may be pathfinding actions, and
	// those do not exist until the registry has been given the map's grid —
	// which comes from the same LoadMap that made the entities. See
	// game.SyncBehaviors.
	tick, err := store.GetCurrentTick(ctx)
	if err != nil {
		return fmt.Errorf("reading the current tick: %w", err)
	}
	activeMapID := ""
	if grid != nil {
		activeMapID = grid.MapID()
	}
	behaviors, err := game.SyncBehaviors(ctx, game.BehaviorSync{
		DB:             store.DB(),
		Schema:         dbSchema,
		Loader:         loader,
		Registry:       registry,
		MapID:          activeMapID,
		Tick:           tick,
		TickDurationMs: tickDurationMs,
	})
	if err != nil {
		return fmt.Errorf("starting behaviors: %w", err)
	}
	for _, problem := range behaviors.Problems {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", problem)
	}
	for _, warning := range behaviors.Warnings {
		fmt.Fprintf(os.Stderr, "Warning: started anyway: %s\n", warning)
	}
	// Said on the way past, like the behaviour count above it. A start-up where
	// the binding did nothing and one where it did everything looked identical,
	// and "the goblin is not moving" is the symptom of both a machine that did
	// not start and a machine that did.
	if behaviors.Started > 0 || behaviors.Running > 0 {
		fmt.Printf("Behaviors: %d started, %d already running\n",
			behaviors.Started, behaviors.Running)
	}

	// The entities come from the map's object layer, spawned by LoadMap above.
	// What is left here is finding the one the engine still refers to by name:
	// the player, because the input handler is bound to an entity and nothing
	// in a map file says "this is the one the keyboard drives". That is the
	// player-input story's to remove, not this one's.
	playerID := int64(0)
	if grid != nil {
		playerID, err = findEntityOfTypeInMap(ctx, store.DB(), "Player", grid.MapID())
	} else {
		playerID, err = findEntityOfType(ctx, store.DB(), "Player")
	}
	if err != nil {
		return fmt.Errorf("looking up the player: %w", err)
	}

	var inputHandler agent.InputHandler
	if grid != nil && playerID != 0 {
		inputHandler = game.NewPlayerInputHandler(playerID, grid)
	}
	if playerID == 0 && cfg.Map.Path != "" {
		// Said once and plainly. A map with no Player object draws fine and
		// does not respond to a key, and "nothing happens when I press an arrow"
		// is not a symptom anybody traces back to a missing spawn.
		//
		// Only when a map is configured: a project with no [map] section has
		// nowhere to add the object, and telling it to "add a Player to " with
		// nothing after it is worse than saying nothing.
		fmt.Fprintf(os.Stderr,
			"Warning: no Player entity — nothing to control. Add an object of class Player to %s\n",
			cfg.Map.Path)
	}

	var animPath, spritesDir string
	for _, mod := range cfg.Mods {
		if mod.Assets != "" {
			animPath = filepath.Join(mod.Assets, "animations.toml")
			spritesDir = filepath.Join(mod.Assets, "sprites")
			break
		}
	}
	animLoader := renderer.NewAnimLoader()
	if err := animLoader.SetProjectRoot(filepath.Dir(cfgPath)); err != nil {
		return fmt.Errorf("setting animation project root: %w", err)
	}
	if animPath != "" {
		if err := animLoader.Load(animPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: loading animations: %v\n", err)
		} else if err := animLoader.SyncToDatabase(ctx, store.DB()); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: syncing asset sheets: %v\n", err)
		}
		go func() {
			if err := animLoader.WatchWithDatabase(ctx, animPath, store.DB()); err != nil {
				fmt.Fprintf(os.Stderr, "anim watcher: %v\n", err)
			}
		}()
	}

	if spritesDir != "" {
		go func() {
			if err := imageCache.WatchDir(ctx, spritesDir); err != nil {
				fmt.Fprintf(os.Stderr, "image cache watcher: %v\n", err)
			}
		}()
	}

	ebiten.SetTPS(60)
	ebiten.SetWindowSize(cfg.Window.Width, cfg.Window.Height)
	ebiten.SetWindowTitle(cfg.Window.Title)

	g := renderer.NewGame(renderer.NewGameParams{
		DB:         store.DB(),
		Loader:     loader,
		Registry:   registry,
		Width:      cfg.Window.Width,
		Height:     cfg.Window.Height,
		TileSize:   cfg.Window.TileSize,
		Grid:       grid,
		Tilemap:    tr,
		Handler:    inputHandler,
		AnimLoader: animLoader,
		ImageCache: imageCache,
	})
	return ebiten.RunGame(g)
}

// findEntityOfType is the id of an entity of this type, or 0 when there is none.
//
// What is left of ensurePlayerEntity and ensureGoblinEntity, which created a
// Player at (2,2) and a Goblin at (15,12) in Go under a TODO from Epic 5. The
// map's object layer is that scene loader; this is only the lookup half, and
// only for the player, because the input handler is bound to one entity and no
// map file says which one the keyboard drives. Zero rather than an error for
// "no such entity": a map without a Player is a map somebody is still building,
// not a failure to start.
func findEntityOfType(ctx context.Context, db *sql.DB, entityType string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx,
		`SELECT id FROM entities WHERE entity_type = ? ORDER BY id LIMIT 1`, entityType).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

func findEntityOfTypeInMap(ctx context.Context, db *sql.DB, entityType, mapID string) (int64, error) {
	id, err := storage.FindEntityByTypeInMap(ctx, db, entityType, mapID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}
