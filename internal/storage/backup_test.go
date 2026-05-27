package storage

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

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
			name:      "overwrites pre-existing backup",
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

			if tt.preExist {
				p := fmt.Sprintf("%s.bak.v%d", destPath, tt.version)
				if err := os.WriteFile(p, []byte("old content"), 0o644); err != nil {
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

			wantPath := fmt.Sprintf("%s.bak.v%d", destPath, tt.version)
			if got != wantPath {
				t.Errorf("backup path = %q, want %q", got, wantPath)
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
