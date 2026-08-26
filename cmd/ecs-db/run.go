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

	playerID, err := ensurePlayerEntity(ctx, svc, store.DB())
	if err != nil {
		return fmt.Errorf("ensuring player entity: %w", err)
	}

	goblinID, err := ensureGoblinEntity(ctx, svc, store.DB())
	if err != nil {
		return fmt.Errorf("ensuring goblin entity: %w", err)
	}
	if err := ensureGoblinBehavior(ctx, goblinID, loader, registry, store.DB()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: goblin behavior: %v\n", err)
	}

	var inputHandler agent.InputHandler
	if grid != nil {
		inputHandler = game.NewPlayerInputHandler(playerID, grid)
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

// TODO: prototype bootstrap — replace with a proper scene/level loader before shipping a real game.
// ensurePlayerEntity returns the existing Player entity ID, or creates one at (2,2) if absent.
// The sheet field is intentionally empty — SyncToDatabase stamps the correct path on every startup.
func ensurePlayerEntity(ctx context.Context, svc *world.EntityService, db *sql.DB) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM entities WHERE entity_type = 'Player' LIMIT 1`).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("looking up player: %w", err)
	}
	e, err := svc.CreateEntity(ctx, "Player", []world.EntityComponent{
		{Name: "Position", Values: world.ComponentValues{"x": 2, "y": 2}},
		{Name: "Sprite", Values: world.ComponentValues{"sheet": "", "animation": "player_idle", "flip_x": false}},
		{Name: "Health", Values: world.ComponentValues{"hp": 10, "maxHp": 10}},
	})
	if err != nil {
		return 0, fmt.Errorf("creating player: %w", err)
	}
	return e.ID, nil
}

// TODO: prototype bootstrap — replace with a proper scene/level loader before shipping a real game.
// ensureGoblinEntity returns the existing Goblin entity ID, or creates one at (15,12) if absent.
// The sheet field is intentionally empty — SyncToDatabase stamps the correct path on every startup.
func ensureGoblinEntity(ctx context.Context, svc *world.EntityService, db *sql.DB) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM entities WHERE entity_type = 'Goblin' LIMIT 1`).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("looking up goblin: %w", err)
	}
	e, err := svc.CreateEntity(ctx, "Goblin", []world.EntityComponent{
		{Name: "Position", Values: world.ComponentValues{"x": 15, "y": 12}},
		{Name: "Sprite", Values: world.ComponentValues{"sheet": "", "animation": "goblin_idle", "flip_x": false}},
		{Name: "Health", Values: world.ComponentValues{"hp": 5, "maxHp": 5}},
	})
	if err != nil {
		return 0, fmt.Errorf("creating goblin: %w", err)
	}
	return e.ID, nil
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
