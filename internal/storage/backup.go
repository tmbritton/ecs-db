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
	pattern := dbPath + ".bak.v*"
	matches, err := filepath.Glob(pattern)
	if err != nil {
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
