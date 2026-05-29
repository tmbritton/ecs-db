package agent

// Event is the event that triggered a transition. Payload carries
// arbitrary JSON-decoded data from the event source.
type Event struct {
	Type    string
	Payload map[string]any
}

// WorldWriter is the write-side interface that actions use to mutate world state.
// The concrete implementation (backed by *sql.Tx) lives in internal/storage.
// Agent code never imports storage directly.
type WorldWriter interface {
	SpawnEntity(entityType string) (int64, error)
	AttachComponent(entityID int64, compName string, values map[string]any) error
	DetachComponent(entityID int64, compName string) error
	SetComponentValue(entityID int64, compName, field string, value any) error
}

// WorldReader is the read-side interface that guards use to inspect world state.
// The concrete implementation (backed by *sql.DB) lives in internal/storage.
type WorldReader interface {
	GetComponentValue(entityID int64, compName, field string) (any, error)
	HasComponent(entityID int64, compName string) (bool, error)
}

// ActionHandler is implemented by Go code that executes a named XState action.
type ActionHandler interface {
	Run(ActionContext) error
}

// GuardHandler is implemented by Go code that evaluates a named XState guard condition.
type GuardHandler interface {
	Evaluate(GuardContext) bool
}

// ActionContext is passed to ActionHandler.Run.
type ActionContext struct {
	EntityID int64
	Tick     int64
	World    WorldWriter
	Params   map[string]any // static params from the machine JSON action spec
	Event    Event
}

// GuardContext is passed to GuardHandler.Evaluate.
type GuardContext struct {
	EntityID int64
	Tick     int64
	World    WorldReader
	Params   map[string]any // static params from the machine JSON cond spec
	Event    Event
}
