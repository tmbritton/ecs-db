package storage

import (
	"database/sql"
	"net/url"
	"strings"
)

// The connection settings every writer of a game database needs.
//
// These are in the DSN rather than issued as statements after opening because
// all three are **per-connection** state in SQLite and *sql.DB is a pool. Four
// db.Exec calls against the pool set them on whichever connection happened to
// be idle at open time and on none of the others — measured, before this was
// fixed, as foreign_keys 1 on the first connection and 0 on the next two.
//
// The driver runs every _pragma parameter on every connection it opens, which
// is the only mechanism that reaches connections the pool creates later.
// database/sql offers no per-connection hook short of writing a
// driver.Connector, which would re-implement this and add a type assertion to
// get wrong.
//
// busy_timeout is first deliberately — the driver sorts it there — so the
// others cannot fail with SQLITE_BUSY on a database somebody else is writing.
//
// **A mistake in this list is silent.** SQLite does not error on an unknown
// pragma name or an unrecognised value, and the driver only reports what SQLite
// reports — so "foriegn_keys(ON)" opens cleanly and sets nothing, and so does
// swapping to a driver that spells these parameters differently
// (mattn/go-sqlite3 uses _foreign_keys, _busy_timeout, _synchronous). The only
// thing standing between this list and a silent revert is
// TestNewSQLiteStore_EveryPooledConnectionCarriesThePragmas, which reads each
// setting back off four live connections. Anything added here needs a line
// there.
var writePragmas = []string{
	// Wait for a writer rather than failing. The engine writes from the tick
	// loop while Forge reads the same file; without this, ordinary WAL
	// contention is an immediate SQLITE_BUSY.
	"busy_timeout(5000)",
	// ON DELETE CASCADE is declared on every comp_*.entity_id and on
	// behavior_components, and it does nothing without this. The schema claims
	// the database enforces referential integrity; this is what makes that
	// true rather than decorative.
	"foreign_keys(ON)",
	// One fsync per checkpoint rather than per commit, which is the difference
	// the tick loop feels.
	//
	// This is a trade, not a free win, and it is worth being exact about which
	// way. Under WAL, NORMAL guarantees the database will not be *corrupted* by
	// a power loss or a hard reboot — but it explicitly allows recently
	// committed transactions to be lost. FULL loses nothing and fsyncs every
	// commit.
	//
	// Chosen because this is a game's world state, written every tick: losing
	// the last few ticks of a session to a power cut is a cost worth paying to
	// keep the tick off the disk, and the alternative is an fsync sixty times a
	// second. Note that the defect this file fixes was hiding the choice —
	// before it, most connections ran at FULL because they never received the
	// pragma at all, so the engine was quietly paying for durability nobody had
	// asked for.
	"synchronous(NORMAL)",
}

// readPragmas is what a reader needs.
//
// foreign_keys and synchronous both describe writing, and are left off for that
// reason: a reader gains nothing from either.
//
// query_only is here because mode=ro alone is weaker than it sounds. It bounds
// the *main* database and nothing else, so a mode=ro connection will happily
// CREATE TEMP TABLE, and ATTACH a second database and write to that. Nothing in
// Forge does either — its three read paths issue fixed SQL — but "Forge cannot
// write" is a contract this project states out loud, and it should be enforced
// by the connection rather than by everyone remembering.
var readPragmas = []string{"busy_timeout(5000)", "query_only(true)"}

// DSN is the connection string for opening a game database read-write.
//
// One builder, in the package that owns database access, because the read-only
// form already carried the argument for it: a second connection string written
// somewhere else is a second chance to forget one of these.
func DSN(path string) string { return dsn(path, writePragmas, "") }

// ReadOnlyDSN is the connection string for reading a game database without
// being able to change it.
//
// Read-only is a correctness requirement rather than a nicety: the architecture
// makes the interpreter the sole writer of world state, so an authoring tool
// attaching read-write would be the exact bug the one-writer-per-table contract
// exists to prevent.
func ReadOnlyDSN(path string) string { return dsn(path, readPragmas, "mode=ro") }

// dsn builds a file: URI with the path percent-encoded.
//
// Encoded rather than concatenated, and this is not tidiness. The driver
// truncates a DSN that is not a URI at its first '?', so a database at
// "/srv/save?1/world.sqlite" used to open "/srv/save" — creating it, finding no
// tables, bootstrapping a second database and reporting success. A path is
// user-supplied config and may hold '?', '#', '%' or a space; the URI form
// opens the file that was asked for in every case.
//
// The file: prefix is what makes SQLite parse the rest as a URI. Without it the
// driver strips the query off the filename, which works, but only for a path
// that has no '?' in it.
func dsn(path string, pragmas []string, extra string) string {
	// A path beginning with two or more slashes would make everything up to the
	// next one look like a URI authority, and SQLite refuses any authority that
	// is not empty or "localhost" — so "//srv/game.db" fails with "invalid uri
	// authority: srv" rather than opening a file that is sitting right there.
	//
	// Collapsed to one slash rather than escaped or made relative: on Linux and
	// macOS a leading run of slashes names the same file as a single one, so
	// this changes the spelling and not the target. Only the leading run —
	// filepath.Clean would also rewrite "a/../b", which is a lexical guess
	// about a path that may contain symlinks, and not this function's business.
	if strings.HasPrefix(path, "//") {
		path = "/" + strings.TrimLeft(path, "/")
	}
	q := make([]string, 0, len(pragmas)+1)
	if extra != "" {
		q = append(q, extra)
	}
	for _, p := range pragmas {
		q = append(q, "_pragma="+url.QueryEscape(p))
	}
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + strings.Join(q, "&")
}

// OpenReadOnly opens a game database for reading and nothing else.
//
// The one place a read connection is made, so "Forge never writes to the game
// database" is a property of a single function rather than of every caller
// remembering to pass the right DSN. There is nothing to get wrong at the call
// site: it takes a path, and there is no argument that could make it writable.
//
// The connection is lazy, as sql.Open always is — a path that is not a database
// fails at the first query, not here.
func OpenReadOnly(path string) (*sql.DB, error) {
	return sql.Open("sqlite", ReadOnlyDSN(path))
}
