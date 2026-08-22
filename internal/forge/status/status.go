// Package status reports whether the game database Forge is pointed at is
// present and compatible.
//
// It is deliberately a pure domain package: paths in, a value out, no HTTP and
// no templates, so its whole state space is unit-testable against temp files.
package status

import (
	"database/sql"
	"log/slog"
	"net/url"
	"os"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"

	_ "modernc.org/sqlite" // SQLite driver
)

// State is what Forge can honestly say about the engine from outside its
// process. The zero value is StateOffline on purpose: a caller rendering a
// Status before the first check has run must not claim a connection.
type State int

const (
	// StateOffline covers every "there is nothing usable here" case — no file,
	// an unreadable file, a database mid-bootstrap with no meta table yet.
	// None of them is an error; a game that has never been run is normal.
	StateOffline State = iota
	// StateSchemaUnreadable means schema.json could not be read or parsed.
	// Reported on its own because the alternative is actively misleading: with
	// no version to compare, every database looks like a mismatch, and the
	// readout ends up blaming the database for a broken file the engine may
	// well be reading perfectly well.
	StateSchemaUnreadable
	// StateMismatch means a database exists but records a different
	// schema_version than the loaded schema.json. Reported separately because
	// it means something quite different: the engine migrates the database on
	// its next start rather than running against it as it is.
	StateMismatch
	// StateConnected means the database opened read-only and the versions
	// agree. Note what it does NOT mean — see Status.
	StateConnected
)

func (s State) String() string {
	switch s {
	case StateConnected:
		return "connected"
	case StateMismatch:
		return "mismatch"
	case StateSchemaUnreadable:
		return "schema-unreadable"
	default:
		return "offline"
	}
}

// Config is what a check needs.
//
// SchemaPath rather than a pre-read version, deliberately, and against the
// story's original shape. Forge exists to edit schema.json; a version captured
// once at startup goes stale the first time someone bumps it, and the readout
// would then report a mismatch that does not exist — in the one tool whose job
// is making that edit. Re-reading per check costs a small file parse every poll
// and is always current. When Epic 11 introduces a project model, this becomes
// its accessor rather than a path.
type Config struct {
	DBPath     string
	SchemaPath string
	ModName    string
}

// Status is the result of one check.
//
// "Connected" is a deliberately modest claim: the database file exists, opens
// read-only, and its recorded schema_version matches. It does NOT mean a game
// is running — SQLite gives a reader no way to know whether a writer is live,
// and overstating it would defeat the point of the readout, which is to tell
// someone whether a save will actually reach anything.
type Status struct {
	State         State
	SchemaVersion int    // from schema.json
	DBVersion     int    // from meta.schema_version; 0 when offline
	ModName       string // first configured mod
}

// ReadOnlyDSN is the connection string every read of the game database uses.
//
// Exported because the migration preview opens the same database for the same
// reason, and a second DSN written elsewhere is a second chance to forget
// mode=ro — which is the one thing that must never be wrong here.
func ReadOnlyDSN(path string) string { return dsn(path) }

// dsn is the connection string Check opens with. Read-only is a correctness
// requirement, not a nicety: the architecture makes the interpreter the sole
// writer of world state, so an authoring tool attaching read-write would be the
// exact bug the one-writer-per-table contract exists to prevent.
//
// The path is URL-escaped because it is user-supplied config; a project
// directory containing '?' or '#' would otherwise truncate or corrupt the DSN.
func dsn(path string) string {
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?mode=ro&_pragma=busy_timeout(5000)"
}

// Check reports whether the game database is present and compatible. It never
// creates or migrates anything, and deliberately does not go through
// storage.NewSQLiteStore, which MkdirAlls the parent and then bootstraps or
// migrates — which is what an authoring tool must never do to a running game's
// database.
//
// It opens and closes per call rather than holding a connection. A held read
// connection against a WAL database is harmless but pins -wal and -shm files,
// which is a confusing thing to leave in someone's project directory.
func Check(cfg Config) Status {
	s := Status{ModName: cfg.ModName}

	// The schema is the thing being compared against, so a failure to read it
	// is its own answer rather than a version of zero to compare with.
	v, err := readSchemaVersion(cfg.SchemaPath)
	if err != nil {
		slog.Debug("engine status: schema unreadable", "path", cfg.SchemaPath, "err", err)
		s.State = StateSchemaUnreadable
		return s
	}
	s.SchemaVersion = v

	// mode=ro on a missing file is an error rather than an empty database, but
	// checking first keeps "no game has been run" off the error path entirely.
	if _, err := os.Stat(cfg.DBPath); err != nil {
		slog.Debug("engine status: no database", "path", cfg.DBPath, "err", err)
		return s
	}

	db, err := sql.Open(cfg.driverName(), dsn(cfg.DBPath))
	if err != nil {
		slog.Debug("engine status: opening database", "path", cfg.DBPath, "err", err)
		return s
	}
	defer func() { _ = db.Close() }()

	// sql.Open is lazy, so this is the call that actually surfaces a bad file.
	// Its error is treated as offline rather than propagated: a half-written
	// database during game startup is a transient condition, not a failure of
	// Forge's, and there is no useful third thing to tell the user.
	dbVersion, err := storage.ReadSchemaVersion(db)
	if err != nil {
		// Logged rather than silently collapsed: a database that exists but is
		// unreadable, or whose recorded version is not a number, renders the
		// same sentence as "no game has ever run" and is otherwise
		// undiagnosable.
		slog.Debug("engine status: reading schema_version", "path", cfg.DBPath, "err", err)
		return s
	}
	s.DBVersion = dbVersion

	if dbVersion == s.SchemaVersion {
		s.State = StateConnected
	} else {
		s.State = StateMismatch
	}
	return s
}

// driverName exists so a test can point Check at a different driver
// registration without the production path taking a parameter it never varies.
func (Config) driverName() string { return "sqlite" }

// readSchemaVersion parses schema.json through the engine's own loader rather
// than pulling one field out with encoding/json. If the engine cannot load the
// file, neither should Forge claim to know its version — the loader's stricter
// checks (duplicate keys, shape) are exactly the ones worth honouring here.
func readSchemaVersion(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	loaded, err := schema.LoadSchema(raw)
	if err != nil {
		return 0, err
	}
	return loaded.SchemaVersion, nil
}
