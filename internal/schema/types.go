package schema

// Basic schema structures
type DatabaseSchema struct {
	Version string           `json:"version"`
	Schema  SchemaDefinition `json:"schema"`
}

type SchemaDefinition struct {
	Components map[string]Component `json:"components"`
	Entities   map[string]Entity    `json:"entities"`
}
