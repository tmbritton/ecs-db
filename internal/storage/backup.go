package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// backupDatabase creates a copy of the database at {dbPath}.bak.v{version}
// using VACUUM INTO, which flushes WAL and produces a standalone valid SQLite
// file. Any pre-existing backup at that path is removed first.
// Returns the backup path on success.
func backupDatabase(db *sql.DB, dbPath string, version int) (string, error) {
	backupPath := fmt.Sprintf("%s.bak.v%d", dbPath, version)
	_ = os.Remove(backupPath)
	// Escape single quotes in the path to avoid SQL injection.
	escaped := strings.ReplaceAll(backupPath, "'", "''")
	if _, err := db.Exec("VACUUM INTO '" + escaped + "'"); err != nil {
		return "", fmt.Errorf("VACUUM INTO backup: %w", err)
	}
	return backupPath, nil
}

// pruneBackups removes old backups for the database at dbPath, keeping the
// newest retention backup files. Files that do not match the versioned naming
// pattern are ignored. Deletion failures are logged but do not return an error.
func pruneBackups(dbPath string, retention int, logger MigrationLogger) {
	// The path is escaped before it becomes a glob pattern. It is user-supplied
	// config, and a project directory holding '*', '?' or '[' would otherwise
	// make this match the wrong files or return ErrBadPattern — which used to
	// be returned silently, so the only visible symptom was backups
	// accumulating forever.
	//
	// With the escaping in place ErrBadPattern is unreachable: globEscape emits
	// a well-formed pattern for every input. The check below stays as the thing
	// that would notice if that ever stopped being true, and says which pattern
	// it built rather than which path was configured, because the pattern is
	// what failed.
	// Cleaned first, because filepath.Glob returns cleaned paths and the version
	// is read back by trimming this prefix off each match. With a raw "./ecs.db"
	// — which is exactly what config.Defaults hands out — Glob answered
	// "ecs.db.bak.v1" and the prefix was "./ecs.db.bak.v", so the trim did
	// nothing, the version would not parse, and every backup was skipped. The
	// symptom was the one this function exists to prevent: they accumulate, and
	// nothing says so.
	dbPath = filepath.Clean(dbPath)
	pattern := globEscape(dbPath) + ".bak.v*"
	matches, err := filepath.Glob(pattern)
	if err != nil {
		logger.Warnf("backup pruning: %q is not a usable pattern: %v", pattern, err)
		return
	}

	type backup struct {
		path    string
		version int
	}
	prefix := dbPath + ".bak.v"
	backups := make([]backup, 0, len(matches))
	for _, m := range matches {
		vStr := strings.TrimPrefix(m, prefix)
		v, err := strconv.Atoi(vStr)
		if err != nil {
			continue // skip non-numeric suffixes
		}
		backups = append(backups, backup{path: m, version: v})
	}

	sort.Slice(backups, func(i, j int) bool { return backups[i].version < backups[j].version })

	if len(backups) <= retention {
		return
	}
	for _, b := range backups[:len(backups)-retention] {
		if err := os.Remove(b.path); err != nil {
			logger.Warnf("backup pruning: failed to remove %s: %v", b.path, err)
		}
	}
}

// globEscape quotes the characters filepath.Match treats as syntax, so a
// database path can be used as the literal prefix of a pattern.
//
// filepath.Glob has no escaping helper of its own, and the ones in path/filepath
// are for matching rather than for building. Backslash is the escape character
// on the platforms this engine runs on; on Windows it would be a separator and
// this would need to be a different function, which is a bridge to cross when
// there is a Windows build to cross it for.
func globEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '*', '?', '[', ']', '\\':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
