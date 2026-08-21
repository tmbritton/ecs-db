package status

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"

	_ "modernc.org/sqlite"
)

// writeSchema puts a minimal, valid schema.json at version v on disk. Check
// reads it per call, so every case needs one.
func writeSchema(t *testing.T, dir string, v int) string {
	t.Helper()
	p := filepath.Join(dir, "schema.json")
	body := fmt.Sprintf(`{
	  "schemaVersion": %d,
	  "components": {"Position": {"type": "object", "properties": {"x": {"type": "number"}}}},
	  "entityTypes": {"Thing": {"requiredComponents": ["Position"]}}
	}`, v)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("writing schema: %v", err)
	}
	return p
}

// bootstrapDB builds a real database at version v using the engine's own
// bootstrap path, then closes it. Check must work against a database it did
// not create — that is the whole situation it exists for.
func bootstrapDB(t *testing.T, path string, v int) {
	t.Helper()
	s := schema.DatabaseSchema{
		SchemaVersion: v,
		Components: map[string]schema.Component{
			"Position": {Type: "object", Properties: map[string]schema.Property{
				"x": {Type: "number"},
				"y": {Type: "number"},
			}},
		},
		EntityTypes: map[string]schema.EntityType{
			"Thing": {RequiredComponents: []string{"Position"}, ValidationLevel: schema.ValidationStrict},
		},
	}
	store, err := storage.NewSQLiteStore(path, s, "")
	if err != nil {
		t.Fatalf("bootstrapping %s at v%d: %v", path, v, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name          string
		fixture       func(t *testing.T, dir string) string // returns the db path
		schemaVersion int
		wantState     State
		wantDBVersion int
	}{
		{
			name: "offline when there is no database file",
			fixture: func(_ *testing.T, dir string) string {
				return filepath.Join(dir, "absent.db")
			},
			schemaVersion: 3,
			wantState:     StateOffline,
			wantDBVersion: 0,
		},
		{
			// A game that has never been run is the normal case, not an error.
			name: "offline when the directory does not exist either",
			fixture: func(_ *testing.T, dir string) string {
				return filepath.Join(dir, "nope", "deeper", "absent.db")
			},
			schemaVersion: 3,
			wantState:     StateOffline,
			wantDBVersion: 0,
		},
		{
			name: "offline when the file is not a database",
			fixture: func(t *testing.T, dir string) string {
				p := filepath.Join(dir, "junk.db")
				if err := os.WriteFile(p, []byte("this is not a database"), 0o600); err != nil {
					t.Fatalf("writing junk: %v", err)
				}
				return p
			},
			schemaVersion: 3,
			wantState:     StateOffline,
			wantDBVersion: 0,
		},
		{
			// Mid-bootstrap, a real database exists with no meta table yet.
			// Transient, and not something to report as a version mismatch.
			name: "offline when there is no meta table",
			fixture: func(t *testing.T, dir string) string {
				p := filepath.Join(dir, "empty.db")
				db, err := sql.Open("sqlite", p)
				if err != nil {
					t.Fatalf("creating: %v", err)
				}
				if _, err := db.Exec("CREATE TABLE unrelated (id INTEGER)"); err != nil {
					t.Fatalf("creating table: %v", err)
				}
				if err := db.Close(); err != nil {
					t.Fatalf("closing: %v", err)
				}
				return p
			},
			schemaVersion: 3,
			wantState:     StateOffline,
			wantDBVersion: 0,
		},
		{
			name: "connected when the versions agree",
			fixture: func(t *testing.T, dir string) string {
				p := filepath.Join(dir, "ok.db")
				bootstrapDB(t, p, 3)
				return p
			},
			schemaVersion: 3,
			wantState:     StateConnected,
			wantDBVersion: 3,
		},
		{
			// The engine would refuse to start against this, which is a quite
			// different situation from "no game has run yet".
			name: "mismatch when the database is behind the schema",
			fixture: func(t *testing.T, dir string) string {
				p := filepath.Join(dir, "behind.db")
				bootstrapDB(t, p, 3)
				return p
			},
			schemaVersion: 4,
			wantState:     StateMismatch,
			wantDBVersion: 3,
		},
		{
			name: "mismatch when the database is ahead of the schema",
			fixture: func(t *testing.T, dir string) string {
				p := filepath.Join(dir, "ahead.db")
				bootstrapDB(t, p, 5)
				return p
			},
			schemaVersion: 4,
			wantState:     StateMismatch,
			wantDBVersion: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := tt.fixture(t, dir)

			got := Check(Config{
				DBPath:     path,
				SchemaPath: writeSchema(t, dir, tt.schemaVersion),
				ModName:    "core",
			})

			if got.State != tt.wantState {
				t.Errorf("State = %v, want %v", got.State, tt.wantState)
			}
			if got.DBVersion != tt.wantDBVersion {
				t.Errorf("DBVersion = %d, want %d", got.DBVersion, tt.wantDBVersion)
			}
			// These are pass-throughs, but the readout renders them, so a
			// dropped field would show as a blank in the menu bar.
			if got.SchemaVersion != tt.schemaVersion {
				t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, tt.schemaVersion)
			}
			if got.ModName != "core" {
				t.Errorf("ModName = %q, want %q", got.ModName, "core")
			}
		})
	}
}

// Read-only is a correctness requirement, not a nicety: the architecture makes
// the interpreter the sole writer of world state, and Forge attaching
// read-write would be the exact bug the one-writer-per-table contract exists to
// prevent. Check opens the connection, so this pins the DSN it opens with.
func TestCheck_OpensTheDatabaseReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ro.db")
	bootstrapDB(t, path, 3)

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatalf("opening with Check's DSN: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec("CREATE TABLE forge_should_not_be_able_to_do_this (id INTEGER)"); err == nil {
		t.Fatal("a write through Check's connection succeeded; the DSN is not read-only")
	}
	if _, err := db.Exec("INSERT INTO meta (key, value) VALUES ('forge', 'nope')"); err == nil {
		t.Fatal("an insert through Check's connection succeeded; the DSN is not read-only")
	}

	// And it must still be able to read, or the check is useless.
	var v string
	if err := db.QueryRow("SELECT value FROM meta WHERE key = 'schema_version'").Scan(&v); err != nil {
		t.Fatalf("reading through the read-only connection: %v", err)
	}
}

// Forge is an authoring tool pointed at someone's project directory, so what it
// leaves there matters. What it must never do is change the database itself.
//
// The story asked for "no -wal/-shm files left behind"; that is not achievable
// and this test records why rather than quietly dropping the requirement. The
// engine opens with journal_mode = WAL (storage/sqlite.go), and a read-only
// connection to a WAL database *must* create the -shm to read it, then cannot
// checkpoint or delete it on close, because it is read-only. Every way around
// that is worse:
//
//   - immutable=1 creates no sidecars, but tells SQLite the file cannot change
//     and returns stale or torn reads exactly when a game IS running
//   - opening read-write with query_only lets SQLite clean up, but grants
//     write access to files the engine owns and permits checkpointing — the
//     one-writer-per-table violation this design exists to avoid
//   - deleting the sidecars ourselves would be catastrophic against a live
//     engine
//
// So the sidecars are accepted, and this pins the two things that do matter:
// the database is untouched, and nothing beyond SQLite's own read sidecars
// appears. When a game is running they already exist and Forge adds nothing;
// when one is not, the next engine run cleans them up.
func TestCheck_DoesNotDisturbTheProject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.db")
	bootstrapDB(t, path, 3)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the database: %v", err)
	}

	schemaPath := writeSchema(t, t.TempDir(), 3)
	for range 5 {
		Check(Config{DBPath: path, SchemaPath: schemaPath, ModName: "core"})
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-reading the database: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("Check modified the database file")
	}

	allowed := map[string]bool{
		"clean.db": true, "clean.db-wal": true, "clean.db-shm": true,
	}
	for _, name := range lsDir(t, dir) {
		if !allowed[name] {
			t.Errorf("Check left %q in the project directory", name)
		}
	}
}

func lsDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// The zero Status is what a caller gets before the first check runs, and it
// renders in the menu bar. It must be the honest answer, not "connected".
func TestState_ZeroValueIsOffline(t *testing.T) {
	var s Status
	if s.State != StateOffline {
		t.Errorf("zero Status.State = %v, want StateOffline", s.State)
	}
}

// State reaches logs and test failure messages, where "2" says nothing.
func TestState_String(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateOffline, "offline"},
		{StateMismatch, "mismatch"},
		{StateConnected, "connected"},
		{StateSchemaUnreadable, "schema-unreadable"},
		// An out-of-range value must not print as a connection.
		{State(99), "offline"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// A schema.json Forge cannot read must be reported as such. With no version to
// compare, treating it as zero makes every database look stale and the readout
// blames the database — for a file the engine may be reading perfectly well.
func TestCheck_UnreadableSchemaIsNotAMismatch(t *testing.T) {
	tests := []struct {
		name       string
		schemaBody string // "" means: do not create the file at all
	}{
		{name: "missing file"},
		{name: "not json", schemaBody: "this is not json"},
		{name: "json but not a schema", schemaBody: `{"nope": true}`},
		{name: "schema version zero", schemaBody: `{"schemaVersion": 0, "components": {}, "entityTypes": {}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			db := filepath.Join(dir, "ecs.db")
			bootstrapDB(t, db, 3)

			schemaPath := filepath.Join(dir, "schema.json")
			if tt.schemaBody != "" {
				if err := os.WriteFile(schemaPath, []byte(tt.schemaBody), 0o600); err != nil {
					t.Fatalf("writing schema: %v", err)
				}
			}

			got := Check(Config{DBPath: db, SchemaPath: schemaPath, ModName: "core"})

			if got.State != StateSchemaUnreadable {
				t.Errorf("State = %v, want StateSchemaUnreadable", got.State)
			}
			if got.State == StateMismatch {
				t.Error("an unreadable schema is being reported as a stale database")
			}
		})
	}
}

// The version is re-read per check, not captured once. Forge's whole purpose is
// editing schema.json; a cached version would report a mismatch that does not
// exist the moment someone bumps it through the editor.
func TestCheck_PicksUpSchemaEditsWithoutARestart(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "ecs.db")
	bootstrapDB(t, db, 3)
	schemaPath := writeSchema(t, dir, 3)

	cfg := Config{DBPath: db, SchemaPath: schemaPath, ModName: "core"}
	if got := Check(cfg).State; got != StateConnected {
		t.Fatalf("State = %v, want StateConnected", got)
	}

	// Someone edits the schema through Forge and saves.
	writeSchema(t, dir, 4)

	got := Check(cfg)
	if got.State != StateMismatch {
		t.Errorf("State = %v, want StateMismatch after the schema version changed", got.State)
	}
	if got.SchemaVersion != 4 {
		t.Errorf("SchemaVersion = %d, want 4 — the version was cached", got.SchemaVersion)
	}
}

// The DSN embeds a user-supplied path. A project directory containing a
// character with meaning in a URI would otherwise truncate or corrupt it, and
// the readout would report offline for a database that is sitting right there.
func TestDSN_EscapesAwkwardPaths(t *testing.T) {
	for _, path := range []string{
		"/tmp/plain/ecs.db",
		"/tmp/a b/ecs.db",
		"/tmp/what?/ecs.db",
		"/tmp/hash#tag/ecs.db",
		"/tmp/perc%20ent/ecs.db",
		"/tmp/amp&and/ecs.db",
		"/tmp/plus+x/ecs.db",
		"/tmp/semi;colon/ecs.db",
	} {
		t.Run(path, func(t *testing.T) {
			got := dsn(path)

			// The driver splits the DSN at the first literal '?', so exactly
			// one may survive escaping: the one introducing the parameters.
			if n := strings.Count(got, "?"); n != 1 {
				t.Errorf("dsn(%q) = %q has %d '?', want exactly 1", path, got, n)
			}
			if !strings.HasSuffix(got, "?mode=ro&_pragma=busy_timeout(5000)") {
				t.Errorf("dsn(%q) = %q lost its parameters", path, got)
			}

			// And it must round-trip back to the path we asked for.
			u, err := url.Parse(got)
			if err != nil {
				t.Fatalf("dsn(%q) = %q is not a valid URI: %v", path, got, err)
			}
			if u.Path != path {
				t.Errorf("dsn(%q) decodes to %q", path, u.Path)
			}
			if u.Query().Get("mode") != "ro" {
				t.Errorf("dsn(%q) is not read-only: %q", path, got)
			}
		})
	}
}

// End to end, on the paths the engine can actually put a database in.
//
// '?' is deliberately absent: storage.NewSQLiteStore passes a bare path to
// sql.Open, and modernc.org/sqlite truncates that at the first '?', so the
// *engine* cannot create a database under such a directory in the first place.
// Forge's DSN handles it (see above); the limitation is the engine's and
// pre-dates this story.
func TestCheck_WorksUnderAwkwardDirectoryNames(t *testing.T) {
	for _, dirName := range []string{"plain", "a b", "hash#tag", "perc%20ent", "amp&and", "plus+x"} {
		t.Run(dirName, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), dirName)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			db := filepath.Join(dir, "ecs.db")
			bootstrapDB(t, db, 3)

			got := Check(Config{
				DBPath:     db,
				SchemaPath: writeSchema(t, dir, 3),
				ModName:    "core",
			})
			if got.State != StateConnected {
				t.Errorf("State = %v for a database under %q, want StateConnected", got.State, dirName)
			}
		})
	}
}
