package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
)

var cfgPath string

var rootCmd = &cobra.Command{
	Use:   "ecs-db",
	Short: "ECS-in-SQLite game engine",
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
		return fmt.Errorf("loading schema from %q: %w", cfg.Schema.Path, err)
	}
	dbSchema, err := schema.LoadSchema(schemaBytes)
	if err != nil {
		return fmt.Errorf("parsing schema from %q: %w", cfg.Schema.Path, err)
	}
	if err := schema.ValidateSchema(dbSchema); err != nil {
		return fmt.Errorf("validating schema from %q: %w", cfg.Schema.Path, err)
	}

	hash := sha256.Sum256(schemaBytes)
	schemaHash := hex.EncodeToString(hash[:])

	db, err := storage.NewSQLiteStore(cfg.Database.Path, dbSchema, schemaHash)
	if err != nil {
		return fmt.Errorf("initializing database: %w", err)
	}
	defer func() { _ = db.Close() }()
	fmt.Printf("Database initialized (schema version %d)\n", dbSchema.SchemaVersion)

	loader := agent.NewLoader(builtins.NewRegistry(), dbSchema)
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
	}

	fmt.Println("Ready for commands (not implemented yet)")
	return nil
}
