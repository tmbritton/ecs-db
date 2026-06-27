package agent

// InputEvent is a single row drained from the input_events table.
type InputEvent struct {
	ID      int64
	Kind    string
	Payload string
}

// InputHandler processes a batch of input events within a tick transaction.
type InputHandler interface {
	Handle(events []InputEvent, world WorldWriter, reader WorldReader) error
}
