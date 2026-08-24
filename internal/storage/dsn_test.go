package storage

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

func pragmaSchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {Type: "object", Properties: map[string]schema.Property{
				"x": {Type: "integer"}, "y": {Type: "integer"},
			}},
		},
		EntityTypes: map[string]schema.EntityType{
			"Thing": {RequiredComponents: []string{"Position"}, ValidationLevel: "strict"},
		},
	}
}

// pragmasPerConnection holds n connections open at once and reports what each
// says, so the pool is forced to open more than one rather than handing back
// the same one every time — which is the only way to see the defect this file
// exists for.
func pragmasPerConnection(t *testing.T, db *sql.DB, n int, names ...string) []map[string]string {
	t.Helper()
	ctx := context.Background()
	out := make([]map[string]string, 0, n)
	conns := make([]*sql.Conn, 0, n)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	for i := 0; i < n; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("opening connection %d: %v", i+1, err)
		}
		conns = append(conns, c)
		got := make(map[string]string, len(names))
		for _, name := range names {
			var v string
			if err := c.QueryRowContext(ctx, "PRAGMA "+name).Scan(&v); err != nil {
				t.Fatalf("reading %s on connection %d: %v", name, i+1, err)
			}
			got[name] = v
		}
		out = append(out, got)
	}
	return out
}

// The defect this story exists for. The pragmas used to be four db.Exec calls
// against the pool, so they landed on whichever connection was idle at open
// time: foreign_keys read 1 on the first and 0 on every other, which means the
// cascade the schema declares fires on one connection and no other.
func TestNewSQLiteStore_EveryPooledConnectionCarriesThePragmas(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "w.sqlite"), pragmaSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	want := map[string]string{
		"foreign_keys": "1",
		"busy_timeout": "5000",
		"synchronous":  "1", // NORMAL
		"journal_mode": "wal",
	}
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}

	// Four, because one is the number that used to pass.
	for i, got := range pragmasPerConnection(t, store.DB(), 4, names...) {
		for name, expected := range want {
			if got[name] != expected {
				t.Errorf("connection %d: %s = %q, want %q", i+1, name, got[name], expected)
			}
		}
	}
}

// journal_mode is not in the DSN and does not need to be: WAL is recorded in
// the database file, so it is set once and read back by every connection that
// opens afterwards — including one opened by something that is not this
// process.
func TestDSN_JournalModeIsNotAConnectionSetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	store, err := NewSQLiteStore(path, pragmaSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	_ = store.Close()

	if strings.Contains(DSN(path), "journal_mode") {
		t.Error("journal_mode is in the DSN, where it would run once per connection")
	}

	// A plain connection, no pragmas at all, still finds WAL.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("reading journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q on a fresh connection, want wal", mode)
	}
}

// The pragmas are only worth anything if the cascade they enable actually
// fires, on a connection that is not the first one.
func TestNewSQLiteStore_TheCascadeFiresOnALaterConnection(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "w.sqlite"), pragmaSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer func() { _ = store.Close() }()
	db := store.DB()

	// Hold the first connection so the work below cannot land on it.
	held, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("holding a connection: %v", err)
	}
	defer func() { _ = held.Close() }()

	if _, err := db.Exec(`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`); err != nil {
		t.Fatalf("inserting entity: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO comp_position (entity_id, x, y) VALUES (1, 0, 0)`); err != nil {
		t.Fatalf("inserting component: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM entities WHERE id = 1`); err != nil {
		t.Fatalf("deleting entity: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM comp_position WHERE entity_id = 1`).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 0 {
		t.Errorf("%d component rows outlived their entity — ON DELETE CASCADE did not fire", n)
	}
}

// And that a reference to an entity that does not exist is refused, which is
// the other half of what foreign_keys buys.
func TestNewSQLiteStore_AComponentCannotNameAnEntityThatIsNotThere(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "w.sqlite"), pragmaSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	held, err := store.DB().Conn(context.Background())
	if err != nil {
		t.Fatalf("holding a connection: %v", err)
	}
	defer func() { _ = held.Close() }()

	_, err = store.DB().Exec(`INSERT INTO comp_position (entity_id, x, y) VALUES (999, 0, 0)`)
	if err == nil {
		t.Fatal("a component row named an entity that does not exist and was accepted")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("error = %q, want it to name the constraint", err)
	}
}

// The path is user-supplied config. Concatenating it into a DSN truncates at
// the first '?', which does not fail — it opens a different file, creates it,
// and reports success. That is true of the code this story replaces.
func TestDSN_OpensTheFileTheCallerNamed(t *testing.T) {
	for _, name := range []string{
		"plain.sqlite",
		"with space.sqlite",
		"with#hash.sqlite",
		"with?question.sqlite",
		"with&amp.sqlite",
		"ünïcode.sqlite",
		"with%25percent.sqlite",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			store, err := NewSQLiteStore(path, pragmaSchema(), "")
			if err != nil {
				t.Fatalf("NewSQLiteStore: %v", err)
			}
			defer func() { _ = store.Close() }()

			if _, err := os.Stat(path); err != nil {
				t.Fatalf("the database is not at the path that was asked for: %v", err)
			}
			// And it is a database, not an empty file the driver made on the
			// way past.
			var n int
			if err := store.DB().QueryRow(
				`SELECT COUNT(*) FROM sqlite_master WHERE name = 'entities'`).Scan(&n); err != nil {
				t.Fatalf("querying: %v", err)
			}
			if n != 1 {
				t.Error("the file that was opened is not the database that was bootstrapped")
			}
		})
	}
}

// One builder, because the read-only DSN's own comment argued for it: a second
// connection string written somewhere else is a second chance to forget
// mode=ro.
func TestReadOnlyDSN_IsReadOnlyAndSetsTheWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	store, err := NewSQLiteStore(path, pragmaSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	_ = store.Close()

	db, err := sql.Open("sqlite", ReadOnlyDSN(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`); err == nil {
		t.Error("a read-only connection wrote to the database")
	}
	for i, got := range pragmasPerConnection(t, db, 3, "busy_timeout") {
		if got["busy_timeout"] != "5000" {
			t.Errorf("connection %d: busy_timeout = %q, want 5000", i+1, got["busy_timeout"])
		}
	}
}

// The other half of the split: journal_mode is a statement, and a statement
// that fails has to be reported rather than passed over. Reaching it takes some
// arranging — Ping catches a file that is not a database, and switching a
// database that is already in WAL to WAL succeeds without writing — so the case
// is a rollback-journal database in a directory that cannot be written to.
// Opening it reads fine and only the mode switch fails, which is exactly the
// gap between "the file opened" and "the file is usable".
func TestNewSQLiteStore_ReportsADatabaseThatWillNotGoIntoWAL(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which ignores the directory permission this rests on")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "w.sqlite")

	seed, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := seed.Exec(`PRAGMA journal_mode = DELETE; CREATE TABLE t (x)`); err != nil {
		t.Fatalf("seeding a non-WAL database: %v", err)
	}
	_ = seed.Close()

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("making the directory read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	store, err := NewSQLiteStore(path, pragmaSchema(), "")
	if err == nil {
		_ = store.Close()
		t.Fatal("a database that cannot be put into WAL was opened anyway")
	}
	if !strings.Contains(err.Error(), "journal_mode") {
		t.Errorf("error = %q, want it to name the pragma that failed", err)
	}
}

// OpenReadOnly is the one place a read connection is made, so this is the one
// place the contract has to hold.
//
// mode=ro is weaker than it sounds: it bounds the main database and nothing
// else, so a connection carrying only mode=ro creates temp tables and will
// ATTACH a second database and write to that. "Forge never writes to the game
// database" is a contract this project states out loud, so the connection
// enforces it rather than every caller remembering to.
func TestOpenReadOnly_CannotWriteAnywhere(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "w.sqlite")
	store, err := NewSQLiteStore(path, pragmaSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	_ = store.Close()

	db, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Reading is the whole point, so it has to still work.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities`).Scan(&n); err != nil {
		t.Fatalf("a read-only connection cannot read: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'Thing', 0)`); err == nil {
		t.Error("wrote to the main database")
	}
	if _, err := db.Exec(`CREATE TEMP TABLE scratch (x)`); err == nil {
		t.Error("created a temp table, which mode=ro alone allows")
	}
	side := filepath.Join(dir, "side.sqlite")
	if _, err := db.Exec(`ATTACH DATABASE ? AS side`, side); err == nil {
		if _, err := db.Exec(`CREATE TABLE side.t (x)`); err == nil {
			t.Error("created and wrote a second database through a read-only connection")
		}
	}
}

// The DSN embeds a user-supplied path. A project directory holding a character
// with meaning in a URI would otherwise truncate or corrupt it — and the
// truncation does not fail, it opens a different file. This lived in
// internal/forge/status until the builder moved here; it is the read side's
// half of what TestDSN_OpensTheFileTheCallerNamed proves for the write side.
func TestReadOnlyDSN_EscapesAwkwardPaths(t *testing.T) {
	for _, path := range []string{
		"/tmp/plain/ecs.db",
		"//tmp/leading-double-slash/ecs.db",
		"/tmp/a b/ecs.db",
		"/tmp/what?/ecs.db",
		"/tmp/hash#tag/ecs.db",
		"/tmp/perc%20ent/ecs.db",
		"/tmp/amp&and/ecs.db",
		"/tmp/plus+x/ecs.db",
		"/tmp/semi;colon/ecs.db",
	} {
		t.Run(path, func(t *testing.T) {
			got := ReadOnlyDSN(path)

			// The driver splits the DSN at the first literal '?', so exactly
			// one may survive escaping: the one introducing the parameters.
			if n := strings.Count(got, "?"); n != 1 {
				t.Errorf("ReadOnlyDSN(%q) = %q has %d '?', want exactly 1", path, got, n)
			}
			// It must round-trip back to the path we asked for — except for a
			// leading run of slashes, which is collapsed to one so SQLite does
			// not read the first segment as a URI authority. That names the
			// same file, which TestDSN_OpensAPathThatBeginsWithTwoSlashes
			// proves by opening it.
			u, err := url.Parse(got)
			if err != nil {
				t.Fatalf("ReadOnlyDSN(%q) = %q is not a valid URI: %v", path, got, err)
			}
			decoded := u.Path
			if decoded == "" {
				decoded = u.Opaque
			}
			if want := "/" + strings.TrimLeft(path, "/"); decoded != path && decoded != want {
				t.Errorf("ReadOnlyDSN(%q) decodes to %q", path, decoded)
			}
			if u.Query().Get("mode") != "ro" {
				t.Errorf("ReadOnlyDSN(%q) is not read-only: %q", path, got)
			}
			// The parameter as the driver will read it, not as it is spelled:
			// url.QueryEscape is free to encode the parens and the driver
			// decodes them again, so asserting the literal would pin a
			// spelling rather than the setting.
			want := []string{"busy_timeout(5000)", "query_only(true)"}
			if got := u.Query()["_pragma"]; !reflect.DeepEqual(got, want) {
				t.Errorf("_pragma = %q, want %q", got, want)
			}
		})
	}
}

// A path beginning "//" makes everything up to the next slash look like a URI
// authority, and SQLite refuses any authority that is not empty or "localhost"
// — "//srv/game.db" fails with "invalid uri authority: srv". On Linux "//tmp/x"
// and "/tmp/x" are the same file, so the fix has to open it rather than refuse.
func TestDSN_OpensAPathThatBeginsWithTwoSlashes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "w.sqlite")
	doubled := "/" + path // "//tmp/..." — the same file, spelled awkwardly

	store, err := NewSQLiteStore(doubled, pragmaSchema(), "")
	if err != nil {
		t.Fatalf("NewSQLiteStore(%q): %v", doubled, err)
	}
	defer func() { _ = store.Close() }()

	if _, err := os.Stat(path); err != nil {
		t.Errorf("the database is not at %q: %v", path, err)
	}
}

// A relative path is ordinary: game.toml's own [database].path is "./ecs.db",
// and config resolution is what usually makes it absolute — usually, not
// always, since NewSQLiteStore is called directly by tools and tests.
//
// It matters here because the guard above rewrites a path that begins with two
// slashes, and a guard that fired on everything would turn "ecs.db" into
// "/ecs.db" — a file at the root of the filesystem, which is not where anybody
// meant.
func TestDSN_OpensARelativePathWhereItWasAsked(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	for _, rel := range []string{"ecs.db", "./nested/ecs.db"} {
		t.Run(rel, func(t *testing.T) {
			store, err := NewSQLiteStore(rel, pragmaSchema(), "")
			if err != nil {
				t.Fatalf("NewSQLiteStore(%q): %v", rel, err)
			}
			defer func() { _ = store.Close() }()

			if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
				t.Errorf("%q did not land beside the working directory: %v", rel, err)
			}
		})
	}
}
