package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// backupDatabase copies the database to {dbPath}.bak.v{version}-{timestamp}
// using VACUUM INTO, which flushes WAL and produces a standalone valid SQLite
// file. Returns the backup path on success.
//
// The timestamp is in the name because the version is no longer unique. Backups
// used to be taken only when a migration moved the database from one
// schemaVersion to another, so ".bak.v3" meant "the database as it last was at
// v3" and there could only be one — and the old name was deleted before writing
// to make that true. Since Story 11 the engine also migrates at a constant
// version, so a second repair at v3 overwrote the restore point the first one
// had made: delete a component without bumping the version and the backup holds
// its table, until any later migration replaces that backup with one that does
// not. Retention could not help, because it counts version-named files and
// there was only ever one.
//
// Sub-second resolution is deliberate: two migrations inside one second is not
// a realistic engine start, but it is an entirely realistic test.
func backupDatabase(db *sql.DB, dbPath string, version int) (string, error) {
	backupPath := fmt.Sprintf("%s.bak.v%d-%s", dbPath, version,
		time.Now().UTC().Format("20060102T150405.000000000Z"))
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

	// Oldest first: by version, then by the timestamp within a version.
	//
	// The version is compared as a number, not as text. Sorting the whole name
	// lexically would put ".bak.v10" before ".bak.v9", and the oldest backups
	// are the ones this deletes. The timestamp is written in a format that
	// sorts the way it reads, so comparing it as text is comparing it as time.
	//
	// Names that do not parse are skipped rather than deleted. A file this
	// function does not understand is not one it should remove.
	type backup struct {
		path    string
		version int
		stamp   string
	}
	prefix := dbPath + ".bak.v"
	backups := make([]backup, 0, len(matches))
	for _, m := range matches {
		v, stamp, ok := parseBackupSuffix(strings.TrimPrefix(m, prefix))
		if !ok {
			continue
		}
		backups = append(backups, backup{path: m, version: v, stamp: stamp})
	}
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].version != backups[j].version {
			return backups[i].version < backups[j].version
		}
		return backups[i].stamp < backups[j].stamp
	})

	if len(backups) <= retention {
		return
	}
	for _, b := range backups[:len(backups)-retention] {
		if err := os.Remove(b.path); err != nil {
			logger.Warnf("backup pruning: failed to remove %s: %v", b.path, err)
		}
	}
}

// parseBackupSuffix splits the "3-20260825T101500.000000000Z" that follows
// ".bak.v" into its version and its timestamp.
//
// A bare version with no timestamp is accepted, with an empty stamp that sorts
// before every real one: backups written before the timestamp was added are
// still backups, and pruning has to be able to remove them rather than keeping
// them forever alongside the ones it understands.
func parseBackupSuffix(suffix string) (version int, stamp string, ok bool) {
	digits := suffix
	if i := strings.IndexByte(suffix, '-'); i >= 0 {
		digits, stamp = suffix[:i], suffix[i+1:]
	}
	v, err := strconv.Atoi(digits)
	if err != nil {
		return 0, "", false
	}
	return v, stamp, true
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
