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

var cfgPath string

var rootCmd = &cobra.Command{
	Use:   "game",
	Short: "ECS-in-SQLite game",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return config.Init(cfgPath)
	},
	RunE: runGame,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "./game.toml", "path to TOML config file")
	rootCmd.AddCommand(schemaCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

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

	var (
		grid *tilemap.TileGrid
		tr   *renderer.TilemapRenderer
	)
	if cfg.Map.Path != "" {
		g, err := tilemap.LoadMap(ctx, svc, store.DB(), cfg.Map.Path)
		if err != nil {
			return fmt.Errorf("loading map: %w", err)
		}
		grid = g
		t, err := renderer.NewTilemapRenderer(store.DB(), cfg.Window.Width, cfg.Window.Height, cfg.Window.TileSize)
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
	go func() {
		if err := watcher.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "watcher: %v\n", err)
		}
	}()

	playerID, err := ensurePlayerEntity(ctx, svc, store.DB())
	if err != nil {
		return fmt.Errorf("ensuring player entity: %w", err)
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

	imageCache := renderer.NewImageCache()
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
