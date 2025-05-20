package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tmbritton/ecs-db/internal/storage"
)

const version = "0.1.0"

func main() {
	// Define command-line flags
	versionFlag := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	// Check for version flag
	if *versionFlag {
		fmt.Printf("ECS Database CLI v%s\n", version)
		os.Exit(0)
	}

	fmt.Println("ECS Database CLI - Starting up")

	// TODO: Initialize your database here
	db, err := storage.InitDb("./ecs.db")
	if err != nil {
		fmt.Printf("Error initializing database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	fmt.Println("Database initialized")

	// TODO: Add command processing here
	fmt.Println("Ready for commands (not implemented yet)")
}
