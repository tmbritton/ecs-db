package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
)

func main() {
	configPath := flag.String("config", "./game.toml", "path to TOML config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "Config file %q not found, using defaults\n", *configPath)
			cfg = config.Defaults()
		} else {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
	}

	// Load schema
	schemaBytes, err := os.ReadFile(cfg.Schema.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading schema from %q: %v\n", cfg.Schema.Path, err)
		os.Exit(1)
	}
	dbSchema, err := schema.LoadSchema(schemaBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing schema from %q: %v\n", cfg.Schema.Path, err)
		os.Exit(1)
	}
	if err := schema.ValidateSchema(dbSchema); err != nil {
		fmt.Fprintf(os.Stderr, "Error validating schema from %q: %v\n", cfg.Schema.Path, err)
		os.Exit(1)
	}

	hash := sha256.Sum256(schemaBytes)
	schemaHash := hex.EncodeToString(hash[:])

	// Initialize database
	db, err := storage.NewSQLiteStore(cfg.Database.Path, dbSchema, schemaHash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing database: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()
	fmt.Printf("Database initialized (schema version %d)\n", dbSchema.SchemaVersion)

	// Load behavior machines from each mod's behaviors directory
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
}
