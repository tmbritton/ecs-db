package storage

import (
	"fmt"

	"github.com/tmbritton/ecs-db/internal/schema"
)

func CreateSqlForComponent(name string, comp schema.Component) (string, error) {
	compType := comp.GetType()

	switch compType {
	case "text":
		// create sql statement to create table for text type

		return "", fmt.Errorf("Unknown component type: %s", compType)
	case "integer":
		return "", fmt.Errorf("Unknown component type: %s", compType)
	// create sql statement to create table to integer type
	case "reference":
		return "", fmt.Errorf("Unknown component type: %s", compType)
	// create sql statement to create toable for reference type
	case "datetime":
		return "", fmt.Errorf("Unknown component type: %s", compType)
	// create sql statement to create table for datetime type
	case "url":
		return "", fmt.Errorf("Unknown component type: %s", compType)
	// create sql statement to create table for url type
	case "email":
		return "", fmt.Errorf("Unknown component type: %s", compType)
	// create sql statement to create table for email type
	default:
		return "", fmt.Errorf("Unknown component type: %s", compType)
	}
}

func CreateSqlForTextComponent(name string, comp schema.TextComponent) string {
	sql := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS component_%s (
		id TEXT PRIMARY KEY,
		entity_id TEXT,
		value TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`, name)

	sql += fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_entity_id ON component_%s(entity_id)`, name)

	return sql
}
