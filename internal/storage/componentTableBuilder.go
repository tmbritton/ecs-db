package storage

import (
	"fmt"

	"github.com/tmbritton/ecs-db/internal/schema"
)

type ComponentTableBuilder struct {
	name      string
	component schema.Component
	sql       string
}

func NewComponentTableBuilder(name string, component schema.Component) *ComponentTableBuilder {
	return &ComponentTableBuilder{
		name:      name,
		component: component,
		sql:       "",
	}
}

func (ctb *ComponentTableBuilder) addName() {
	ctb.sql = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS component_%s (`, ctb.name)
}

func (ctb *ComponentTableBuilder) addColumns() {
	// Default columns
	ctb.sql += `
		id TEXT PRIMARY KEY,
		entity_id TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	`

	if textComp, ok := ctb.component.(*schema.TextComponent); ok {
		ctb.sql += `value TEXT`
		if textComp.MinLength != nil {
			ctb.sql += fmt.Sprintf(` CHECK (length(value) >= %d)`, *textComp.MinLength)
		}
		if textComp.MaxLength != nil {
			ctb.sql += fmt.Sprintf(` CHECK (length(value) <= %d)`, *textComp.MaxLength)
		}

		ctb.sql += ","
	}

	if intComp, ok := ctb.component.(*schema.IntegerComponent); ok {
		if intComp.Min != nil && intComp.Max != nil {
			ctb.sql += fmt.Sprintf(`value INTEGER CHECK (value >= %d AND value <= %d),`, *intComp.Min, *intComp.Max)
		} else if intComp.Min != nil {
			ctb.sql += fmt.Sprintf(`value INTEGER CHECK (value >= %d),`, *intComp.Min)
		} else if intComp.Max != nil {
			ctb.sql += fmt.Sprintf(`value INTEGER CHECK (value <= %d),`, *intComp.Max)
		} else {
			ctb.sql += "value INTEGER,"
		}
	}

	if _, ok := ctb.component.(*schema.ReferenceComponent); ok {
		ctb.sql += `value TEXT UNIQUE NOT NULL,`
	}

	if _, ok := ctb.component.(*schema.BoolComponent); ok {
		ctb.sql += `
			value INTEGER CHECK (value IN (0, 1)),
		`
	}
}

func (ctb *ComponentTableBuilder) closeColumns() {
	ctb.sql = ctb.sql + ");"
}

func (ctb *ComponentTableBuilder) BuildSql() string {
	ctb.addName()
	ctb.addColumns()
	ctb.closeColumns()
	return ctb.sql
}
