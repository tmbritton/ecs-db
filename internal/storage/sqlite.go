package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
	"github.com/tmbritton/ecs-db/internal/schema"
)

// SQLiteStore handles database connections and operations
type SQLiteStore struct {
	db *sql.DB
}

func InitDb(path string, schema schema.DatabaseSchema) (*SQLiteStore, error) {
	// Ensure directory exists
	dbDir := filepath.Dir(path)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection
	db, err := sql.Open("sqlite3", path+"-"+schema.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create tables if they don't exist
	if err := initSchema(db, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func initSchema(db *sql.DB, schema schema.DatabaseSchema) error {
	sql := `
    -- Entities Table
    CREATE TABLE IF NOT EXISTS entities (
      id TEXT PRIMARY KEY,
      type TEXT,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );

    CREATE INDEX IF NOT EXISTS idx_entity_type ON entities(type);

    -- Schema Table
    CREATE TABLE IF NOT EXISTS schema (
      id TEXT PRIMARY KEY,
      version TEXT,
      definition TEXT
    );

    CREATE INDEX IF NOT EXISTS idx_schema_version on schema(version);
  `

	_, err := db.Exec(sql)
	return err
}
