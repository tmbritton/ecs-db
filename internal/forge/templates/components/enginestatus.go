package components

import (
	"fmt"

	"github.com/tmbritton/ecs-db/internal/forge/status"
)

type EngineStatusProps struct {
	Status status.Status
}

// dotClass is green only for a genuine connection. Both failure states share
// red: the sentence beside the dot carries the difference between them, and a
// third hue would claim a distinction the palette does not make.
func dotClass(s status.State) string {
	if s == status.StateConnected {
		return "engine-status__dot--ok"
	}
	return "engine-status__dot--bad"
}

// engineStatusText says what follows from the state, not just what it is.
// "watcher offline" alone leaves the reader to work out whether their save
// landed; the whole point of the readout is to answer that.
func engineStatusText(s status.Status) string {
	switch s.State {
	case status.StateConnected:
		// Deliberately not "the game is running": SQLite gives a reader no way
		// to know whether a writer is live. This says the database is present
		// and compatible, which is all Forge can honestly claim.
		return fmt.Sprintf("schema.json v%d · mods/%s · hot-reload live", s.SchemaVersion, s.ModName)
	case status.StateMismatch:
		return fmt.Sprintf("schema v%d ≠ db v%d · engine would refuse this database",
			s.SchemaVersion, s.DBVersion)
	case status.StateSchemaUnreadable:
		// Names the file that is actually wrong. Reporting this as a mismatch
		// pointed at the database instead — for a file the engine may well be
		// reading perfectly well.
		return "schema.json unreadable · cannot compare against the database"
	default:
		return "watcher offline · edits queue until game restarts"
	}
}
