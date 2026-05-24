package main

import (
	"fmt"
	"os"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
)

func main() {
	fmt.Println("ECS Database CLI - Starting up")

	// Load schema
	dbSchema, err := schema.InitSchema("./schema.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading schema from schema.json: %v\n", err)
		os.Exit(1)
	}

	// Initialize database
	db, err := storage.NewSQLiteStore("./ecs.db", dbSchema)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing database: %v\n", err)
		os.Exit(1)
	}

	defer func() { _ = db.Close() }()
	fmt.Printf("Database initialized (schema version %d)\n", dbSchema.SchemaVersion)

	// TODO: Add command processing here
	fmt.Println("Ready for commands (not implemented yet)")
}
