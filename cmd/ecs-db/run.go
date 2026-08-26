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
		if src != nil && (src.TileWidth != cfg.Window.TileSize || src.TileHeight != cfg.Window.TileSize) {
			return fmt.Errorf(
				"map %s has %dx%d pixel tiles and window.tileSize is %d; "+
					"entities are placed on the second and tiles on the first, so they must agree",
				cfg.Map.Path, src.TileWidth, src.TileHeight, cfg.Window.TileSize)
		}
		// The parsed map goes to the renderer as well as the database: the
		// database holds one row per cell and cannot say what is stacked on it.
		t, err := renderer.NewTilemapRenderer(store.DB(), src, imageCache,
			cfg.Window.Width, cfg.Window.Height, cfg.Window.TileSize)
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

	// The entities come from the map's object layer now, spawned by LoadMap
	// above. What is left here is finding the two the engine still refers to by
	// name — which is the half of the old ensure functions worth keeping, and
	// goes when entity-type behaviour and player input stop being special-cased.
	playerID, err := findEntityOfType(ctx, store.DB(), "Player")
	if err != nil {
		return fmt.Errorf("looking up the player: %w", err)
	}
	goblinID, err := findEntityOfType(ctx, store.DB(), "Goblin")
	if err != nil {
		return fmt.Errorf("looking up the goblin: %w", err)
	}
	if goblinID != 0 {
		if err := ensureGoblinBehavior(ctx, goblinID, loader, registry, store.DB()); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: goblin behavior: %v\n", err)
		}
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
	if animPath != "" {
		if err := animLoader.Load(animPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: loading animations: %v\n", err)
		} else if err := animLoader.SyncToDatabase(ctx, store.DB()); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: syncing asset sheets: %v\n", err)
		}
		go func() {
			if err := animLoader.Watch(ctx, animPath); err != nil {
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
// exists because the input handler and the goblin's machine still name their
// entity by type. Zero rather than an error for "no such entity": a map without
// a Player is a map somebody is still building, not a failure to start.
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

// ensureGoblinBehavior starts the "goblin" machine for goblinID if it is not already running.
// Must be called after loader.ScanDir so loader.Get("goblin") resolves.
func ensureGoblinBehavior(ctx context.Context, goblinID int64, loader *agent.Loader, registry *agent.Registry, db *sql.DB) error {
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM behavior_components WHERE entity_id = ? AND machine_id = 'goblin'`,
		goblinID).Scan(&count); err != nil || count > 0 {
		return err
	}
	def, ok := loader.Get("goblin")
	if !ok {
		return fmt.Errorf("goblin machine not loaded")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Ignored deliberately and said so: this rolls back only when the commit
	// below did not happen, and a rollback that fails after a failure has
	// nothing left to report to.
	defer func() { _ = tx.Rollback() }()
	a := agent.NewAgent(def, goblinID, "", tickDurationMs)
	if err := agent.StartAgent(a, registry, 0,
		storage.NewTxWorldWriter(tx),
		storage.NewTxWorldReader(tx),
		storage.NewMachineWriter(tx)); err != nil {
		return fmt.Errorf("starting goblin agent: %w", err)
	}
	return tx.Commit()
}
