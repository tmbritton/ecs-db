package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// openFileDB opens a real file-backed SQLite database with pragmas applied.
func openFileDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("openFileDB: %v", err)
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			t.Fatalf("openFileDB pragma %s: %v", pragma, err)
		}
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestBackupDatabase(t *testing.T) {
	emptySchema := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components:    map[string]schema.Component{},
		EntityTypes:   map[string]schema.EntityType{},
	}

	tests := []struct {
		name       string
		version    int
		badDestDir bool // derive a backup path whose parent directory does not exist
		preExist   bool // pre-create a file at the expected backup path before calling backup
		wantErr    bool
		wantValid  bool // open backup and verify it is a queryable SQLite database
	}{
		{
			name:      "creates valid SQLite file",
			version:   1,
			wantValid: true,
		},
		{
			name:    "path follows basename.bak.vN pattern",
			version: 7,
		},
		{
			name:      "keeps a pre-existing backup at the same version",
			version:   1,
			preExist:  true,
			wantValid: true,
		},
		{
			name:       "fails when backup destination directory does not exist",
			version:    1,
			badDestDir: true,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			srcPath := dir + "/world.sqlite"

			// destPath is the dbPath argument to backupDatabase. The backup file
			// will be at destPath + ".bak.v" + version.
			destPath := srcPath
			if tt.badDestDir {
				destPath = dir + "/nonexistent/subdir/world.sqlite"
			}

			db := openFileDB(t, srcPath)
			bootstrapMigrationDB(t, db, emptySchema)

			// A backup that is already there. It used to be deleted before
			// writing, which is what made a second migration at one version
			// destroy the first one's restore point.
			preExisting := fmt.Sprintf("%s.bak.v%d-20200101T000000.000000000Z", destPath, tt.version)
			if tt.preExist {
				if err := os.WriteFile(preExisting, []byte("old content"), 0o644); err != nil {
					t.Fatalf("pre-creating backup file: %v", err)
				}
			}

			got, err := backupDatabase(db, destPath, tt.version)
			if tt.wantErr {
				if err == nil {
					t.Errorf("backupDatabase: want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("backupDatabase: %v", err)
			}

			// The name carries the version and then a timestamp, so it is
			// checked in the two parts that mean something rather than as a
			// literal. The timestamp is what stops a second migration at the
			// same version from overwriting the first one's restore point, so
			// it has to be there and it has to parse.
			wantPrefix := fmt.Sprintf("%s.bak.v%d-", destPath, tt.version)
			if !strings.HasPrefix(got, wantPrefix) {
				t.Errorf("backup path = %q, want it to start with %q", got, wantPrefix)
			}
			v, stamp, ok := parseBackupSuffix(strings.TrimPrefix(got, destPath+".bak.v"))
			if !ok {
				t.Fatalf("backup path %q does not parse as a backup name", got)
			}
			if v != tt.version {
				t.Errorf("backup names version %d, want %d", v, tt.version)
			}
			if _, err := time.Parse("20060102T150405.000000000Z", stamp); err != nil {
				t.Errorf("backup timestamp %q does not parse: %v", stamp, err)
			}

			if tt.preExist {
				kept, err := os.ReadFile(preExisting)
				if err != nil {
					t.Fatalf("the backup that was already there is gone: %v", err)
				}
				if string(kept) != "old content" {
					t.Error("an existing backup was overwritten by a new one at the same version")
				}
			}

			if tt.wantValid {
				bdb, err := sql.Open("sqlite", got)
				if err != nil {
					t.Fatalf("opening backup: %v", err)
				}
				defer func() { _ = bdb.Close() }()
				var version string
				if err := bdb.QueryRow("SELECT value FROM meta WHERE key = 'schema_version'").Scan(&version); err != nil {
					t.Fatalf("querying backup meta: %v", err)
				}
				if version != "1" {
					t.Errorf("backup schema_version = %q, want 1", version)
				}
			}
		})
	}
}

func TestPruneBackups(t *testing.T) {
	tests := []struct {
		name            string
		numericVersions []int    // numeric-versioned backup files to create (e.g. .bak.v1)
		extraSuffixes   []string // additional non-standard suffixes to create
		retention       int
		wantDeleted     []int // versions expected to be removed
		wantKept        []int // versions expected to remain
	}{
		{
			name:            "keeps newest N when more exist",
			numericVersions: []int{1, 2, 3, 4, 5},
			retention:       3,
			wantDeleted:     []int{1, 2},
			wantKept:        []int{3, 4, 5},
		},
		{
			name:            "no-op when below retention",
			numericVersions: []int{1, 2},
			retention:       3,
			wantKept:        []int{1, 2},
		},
		{
			name:      "empty directory",
			retention: 3,
		},
		{
			name:            "skips non-numeric suffixes; second guard fires when numeric count equals retention",
			numericVersions: []int{1, 2},
			extraSuffixes:   []string{".bak.vfoo", ".bak.vbar"},
			retention:       2,
			wantKept:        []int{1, 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			dbPath := dir + "/world.sqlite"

			for _, v := range tt.numericVersions {
				p := fmt.Sprintf("%s.bak.v%d", dbPath, v)
				if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
					t.Fatalf("creating backup v%d: %v", v, err)
				}
			}
			for _, s := range tt.extraSuffixes {
				p := dbPath + s
				if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
					t.Fatalf("creating %s: %v", s, err)
				}
			}

			pruneBackups(dbPath, tt.retention, NopLogger())

			for _, v := range tt.wantDeleted {
				p := fmt.Sprintf("%s.bak.v%d", dbPath, v)
				if _, err := os.Stat(p); err == nil {
					t.Errorf("backup v%d should be deleted but still exists", v)
				}
			}
			for _, v := range tt.wantKept {
				p := fmt.Sprintf("%s.bak.v%d", dbPath, v)
				if _, err := os.Stat(p); err != nil {
					t.Errorf("backup v%d should exist but was deleted: %v", v, err)
				}
			}
			for _, s := range tt.extraSuffixes {
				p := dbPath + s
				if _, err := os.Stat(p); err != nil {
					t.Errorf("non-numeric file %s should not be touched: %v", s, err)
				}
			}
		})
	}
}

// TestPruneBackups_NoOpOnGlobError covers the filepath.Glob error path, which
// fires when the constructed pattern is malformed (a "[" without a closing "]").
func TestPruneBackups_NoOpOnGlobError(t *testing.T) {
	dir := t.TempDir()
	// "[" in the path without "]" makes the .bak.v* glob pattern malformed.
	malformedPath := dir + "/world[db.sqlite"
	pruneBackups(malformedPath, 3, NopLogger())
}

// testLogger captures Warnf calls for assertion in tests.
type testLogger struct {
	warned bool
}

func (l *testLogger) Infof(_ string, _ ...interface{}) {}
func (l *testLogger) Warnf(_ string, _ ...interface{}) { l.warned = true }

// TestPruneBackups_LogsWarningOnRemoveFailure verifies that a failed os.Remove
// emits a Warnf via the logger rather than returning an error.
func TestPruneBackups_LogsWarningOnRemoveFailure(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/world.sqlite"

	for i := 1; i <= 3; i++ {
		p := fmt.Sprintf("%s.bak.v%d", dbPath, i)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("creating backup: %v", err)
		}
	}

	// Replace v1 (oldest, will be pruned) with a non-empty directory.
	// os.Remove on a non-empty directory fails with ENOTEMPTY.
	v1 := dbPath + ".bak.v1"
	if err := os.Remove(v1); err != nil {
		t.Fatalf("removing v1 to replace with dir: %v", err)
	}
	if err := os.MkdirAll(v1+"/child", 0o755); err != nil {
		t.Fatalf("creating non-empty dir at v1: %v", err)
	}

	logger := &testLogger{}
	pruneBackups(dbPath, 1, logger)

	if !logger.warned {
		t.Error("expected Warnf call when os.Remove fails on a non-empty directory")
	}
}

// The backup path is built from the database path, which is user-supplied
// config. A project directory holding a glob metacharacter used to make the
// prune match nothing — and the error was swallowed, so the only symptom was
// backups accumulating forever with nothing said about it.
func TestPruneBackups_FindsBackupsBesideAPathHoldingGlobSyntax(t *testing.T) {
	for _, dirName := range []string{"plain", "star*dir", "quest?dir", "brack[et]dir"} {
		t.Run(dirName, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), dirName)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			dbPath := filepath.Join(dir, "world.sqlite")

			// Four backups, retention of one: three should go.
			for v := 1; v <= 4; v++ {
				name := fmt.Sprintf("%s.bak.v%d", dbPath, v)
				if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
					t.Fatalf("writing %s: %v", name, err)
				}
			}

			pruneBackups(dbPath, 1, NopLogger())

			left, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("ReadDir: %v", err)
			}
			var backups []string
			for _, e := range left {
				if strings.Contains(e.Name(), ".bak.v") {
					backups = append(backups, e.Name())
				}
			}
			if len(backups) != 1 {
				t.Errorf("%d backups left, want 1: %v", len(backups), backups)
			}
			if len(backups) == 1 && !strings.HasSuffix(backups[0], ".v4") {
				t.Errorf("kept %q, want the newest", backups[0])
			}
		})
	}
}

// filepath.Glob returns cleaned paths, and the version is read back by trimming
// the raw dbPath off each match. A "./ecs.db" — which is what config.Defaults
// hands out when there is no game.toml — therefore matched four files, trimmed
// nothing off any of them, failed to parse a version for each, and pruned none.
func TestPruneBackups_PrunesForAPathThatIsNotAlreadyClean(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	for _, dbPath := range []string{"./ecs.db", "sub/../ecs.db"} {
		t.Run(dbPath, func(t *testing.T) {
			for _, f := range []string{"ecs.db.bak.v1", "ecs.db.bak.v2", "ecs.db.bak.v3"} {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
					t.Fatalf("writing %s: %v", f, err)
				}
			}
			pruneBackups(dbPath, 1, NopLogger())

			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("ReadDir: %v", err)
			}
			var left []string
			for _, e := range entries {
				if strings.Contains(e.Name(), ".bak.v") {
					left = append(left, e.Name())
				}
			}
			if len(left) != 1 {
				t.Errorf("%d backups left for %q, want 1: %v", len(left), dbPath, left)
			}
			for _, f := range left {
				_ = os.Remove(filepath.Join(dir, f))
			}
		})
	}
}
