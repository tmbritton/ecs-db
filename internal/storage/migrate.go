package storage

import (
	"github.com/tmbritton/ecs-db/internal/schema"
)

// MigrateComponent builds a CREATE TABLE statement for a named component
// with the given definition. It delegates to componentTableSQL, the
// authoritative source for DDL generation.
func MigrateComponent(name string, comp schema.Component) (string, error) {
	return componentTableSQL(name, comp)
}
