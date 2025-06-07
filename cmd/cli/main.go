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
		fmt.Printf("Error loading schema from schema.json")
		os.Exit(1)
	}

	// Initialize database
	db, err := storage.InitDb("./ecs.db", dbSchema)
	if err != nil {
		fmt.Printf("Error initializing database: %v\n", err)
		os.Exit(1)
	}

	defer db.Close()
	fmt.Println("Database initialized")

	// TODO: Add command processing here
	fmt.Println("Ready for commands (not implemented yet)")
}
