package world

// Entity represents a single game entity — a unique identity with a
// declared type and a creation tick. In pure ECS, entities are opaque
// IDs carrying no data of their own; here we track the entity type so
// the reader understands "what" an entity is at a glance.
type Entity struct {
	ID          int64
	EntityType  string
	CreatedTick int64
}

// EntityComponent carries the component data to persist when creating an
// entity. Name matches a top-level key in schema.json's "components" map,
// and Values maps property names to their initial values.
type EntityComponent struct {
	Name   string
	Values map[string]interface{}
}
