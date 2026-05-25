package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
)

func main() {
	fmt.Println("ECS Database CLI - Starting up")

	// Load schema
	schemaPath := "./schema.json"
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading schema from schema.json: %v\n", err)
		os.Exit(1)
	}
	dbSchema, err := schema.LoadSchema(schemaBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing schema from schema.json: %v\n", err)
		os.Exit(1)
	}
	if err := schema.ValidateSchema(dbSchema); err != nil {
		fmt.Fprintf(os.Stderr, "Error validating schema from schema.json: %v\n", err)
		os.Exit(1)
	}

	// Compute hash for build metadata.
	hash := sha256.Sum256(schemaBytes)
	schemaHash := hex.EncodeToString(hash[:])

	// Initialize database
	db, err := storage.NewSQLiteStore("./ecs.db", dbSchema, schemaHash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing database: %v\n", err)
		os.Exit(1)
	}

	defer func() { _ = db.Close() }()
	fmt.Printf("Database initialized (schema version %d)\n", dbSchema.SchemaVersion)

	// TODO: Add command processing here
	fmt.Println("Ready for commands (not implemented yet)")
}
