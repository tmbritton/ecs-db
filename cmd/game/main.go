//go:build ebitengine

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/spf13/cobra"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/config"
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

	ebiten.SetTPS(60)
	ebiten.SetWindowSize(cfg.Window.Width, cfg.Window.Height)
	ebiten.SetWindowTitle(cfg.Window.Title)

	game := renderer.NewGame(store.DB(), loader, registry, cfg.Window.Width, cfg.Window.Height, grid, tr)
	return ebiten.RunGame(game)
}
