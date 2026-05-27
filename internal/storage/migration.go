package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// SchemaMigrationError is returned when a migration fails at a specific DDL statement.
type SchemaMigrationError struct {
	Change       string // component or property name affected
	ChangeKind   string // "create_table", "alter_add_column", "rebuild_table", "drop_table"
	SQL          string // the statement that failed
	Underlying   error  // driver error
	StatementIdx int    // zero-based index within the statement batch
	TotalStmts   int    // total statements in the batch
}

func (e *SchemaMigrationError) Error() string {
	return fmt.Sprintf(
		"migration failed: %s %q — statement %d/%d\nSQL: %s\nunderlying: %v",
		e.ChangeKind, e.Change, e.StatementIdx+1, e.TotalStmts, e.SQL, e.Underlying,
	)
}

// Unwrap allows errors.Is/As to reach the underlying driver error.
func (e *SchemaMigrationError) Unwrap() error {
	return e.Underlying
}

// MigrationPolicy controls whether the runner proceeds through destructive
// schema changes automatically or requires explicit confirmation.
type MigrationPolicy string

const (
	// MigrationAuto always runs migrations without prompting, even for
	// destructive changes (DROP TABLE, column removal, type change).
	MigrationAuto MigrationPolicy = "auto"
	// MigrationConfirm blocks when destructive changes are present.
	// RunMigrate returns *MigrationRequiresConfirmation for the caller
	// to handle (CLI prompt, config flag, etc.).
	MigrationConfirm MigrationPolicy = "confirm"
)

// MigrationRequiresConfirmation is returned by the runner when
// policy=confirm and destructive changes are detected.
type MigrationRequiresConfirmation struct {
	// DestructiveStatements is a copy of the destructive statements for review.
	DestructiveStatements []Statement
}

func (e *MigrationRequiresConfirmation) Error() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "migration requires confirmation: %d destructive change(s):\n",
		len(e.DestructiveStatements))
	for _, s := range e.DestructiveStatements {
		fmt.Fprintf(&sb, "  - %s: %s\n", s.Kind, s.Description)
	}
	return sb.String()
}

// MigrationLogger receives structured events from the runner.
// Implement it to forward to your logging library; use NopLogger() to discard.
type MigrationLogger interface {
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
}

type nopLogger struct{}

func (nopLogger) Infof(_ string, _ ...interface{}) {}
func (nopLogger) Warnf(_ string, _ ...interface{}) {}

// NopLogger returns a MigrationLogger that discards all output.
func NopLogger() MigrationLogger { return nopLogger{} }

// MigrationRunner orchestrates the full migration pipeline:
// introspect → diff → generate DDL → execute in one transaction → update meta.
type MigrationRunner struct {
	db     *sql.DB
	file   schema.DatabaseSchema
	policy MigrationPolicy
	logger MigrationLogger
}

// NewMigrationRunner creates a MigrationRunner. If logger is nil, NopLogger is used.
func NewMigrationRunner(
	db *sql.DB,
	file schema.DatabaseSchema,
	policy MigrationPolicy,
	logger MigrationLogger,
) *MigrationRunner {
	if logger == nil {
		logger = NopLogger()
	}
	return &MigrationRunner{
		db:     db,
		file:   file,
		policy: policy,
		logger: logger,
	}
}

// Run executes the migration pipeline. Returns nil if the database is already
// up to date or if migration succeeds. Returns *SchemaMigrationError if a DDL
// statement fails (with full rollback), or *MigrationRequiresConfirmation when
// policy=confirm and destructive changes are present.
func (r *MigrationRunner) Run() error {
	// 1. Introspect current DB state.
	domain, err := IntrospectAll(r.db)
	if err != nil {
		return fmt.Errorf("introspecting db: %w", err)
	}

	// 2. Compute structural diff.
	changes := schema.Diff(domain.ToDiffSchema(), &r.file, nil)

	// Nothing to do when versions already match and structure is identical.
	if len(changes) == 0 && domain.SchemaVersion == r.file.SchemaVersion {
		return nil
	}

	// 3. Generate DDL from structural changes.
	gen := NewGenerator(&r.file, domain, Config{StrictDrop: true})
	stmts := gen.Generate(changes)

	// 4. Check policy against destructive statements.
	if r.policy == MigrationConfirm {
		var destructive []Statement
		for _, s := range stmts {
			if s.Destructive {
				destructive = append(destructive, s)
			}
		}
		if len(destructive) > 0 {
			return &MigrationRequiresConfirmation{DestructiveStatements: destructive}
		}
	}

	// 5. If any statement is a table rebuild, PRAGMA foreign_keys must be
	// toggled on the same connection that runs the transaction. PRAGMA
	// foreign_keys is per-connection and database/sql uses a pool, so issuing
	// it on *sql.DB and then calling Begin() may land on different underlying
	// connections, leaving FK enforcement active during the DROP TABLE.
	needsFKToggle := false
	for _, s := range stmts {
		if s.Kind == "rebuild_table" {
			needsFKToggle = true
			break
		}
	}

	// 6. Begin transaction — all DDL + meta update commit atomically.
	var tx *sql.Tx
	if needsFKToggle {
		conn, err := r.db.Conn(context.Background())
		if err != nil {
			return fmt.Errorf("acquiring pinned connection for rebuild: %w", err)
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.ExecContext(context.Background(), "PRAGMA foreign_keys = OFF"); err != nil {
			return fmt.Errorf("disabling foreign keys for rebuild: %w", err)
		}
		defer func() {
			if _, err := conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
				r.logger.Warnf("re-enabling foreign keys after rebuild: %v", err)
			}
		}()
		tx, err = conn.BeginTx(context.Background(), nil)
		if err != nil {
			return fmt.Errorf("beginning migration transaction: %w", err)
		}
	} else {
		var err error
		tx, err = r.db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration transaction: %w", err)
		}
	}

	// 7. Execute each DDL statement.
	for i, stmt := range stmts {
		if _, err := tx.Exec(stmt.SQL); err != nil {
			_ = tx.Rollback()
			return &SchemaMigrationError{
				Change:       stmt.Component,
				ChangeKind:   stmt.Kind,
				SQL:          stmt.SQL,
				Underlying:   err,
				StatementIdx: i,
				TotalStmts:   len(stmts),
			}
		}
		r.logger.Infof("migration: executed %s on %s", stmt.Kind, stmt.Component)
	}

	// 8. Update meta inside the same transaction.
	versionRes, err := tx.Exec(
		"UPDATE meta SET value = ? WHERE key = 'schema_version'",
		fmt.Sprintf("%d", r.file.SchemaVersion),
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("updating meta schema_version: %w", err)
	}
	if n, _ := versionRes.RowsAffected(); n == 0 {
		_ = tx.Rollback()
		return fmt.Errorf("updating meta schema_version: row missing — database may be corrupt")
	}
	if _, err := tx.Exec(
		"UPDATE meta SET value = ? WHERE key = 'build_time'",
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("updating meta build_time: %w", err)
	}

	// 9. Commit.
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing migration: %w", err)
	}

	r.logger.Infof("migration complete: %d statements applied, version %d → %d",
		len(stmts), domain.SchemaVersion, r.file.SchemaVersion)

	return nil
}
