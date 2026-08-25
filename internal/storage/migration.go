package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// SchemaMigrationError is returned when a migration fails at a specific DDL statement.
type SchemaMigrationError struct {
	Change       string // component or property name affected
	ChangeKind   string // the failing Statement's Kind
	SQL          string // the statement that failed
	Underlying   error  // driver error
	StatementIdx int    // zero-based index within the statement batch
	TotalStmts   int    // total statements in the batch

	// Hint is what to do about it, for the failures where the driver's sentence
	// does not say. The store returns this error from every open, so whoever
	// reads it is looking at a database that will not start and needs to know
	// which file to edit — not the name of a temporary table.
	Hint string
}

func (e *SchemaMigrationError) Error() string {
	msg := fmt.Sprintf(
		"migration failed: %s %q — statement %d/%d\nSQL: %s\nunderlying: %v",
		e.ChangeKind, e.Change, e.StatementIdx+1, e.TotalStmts, e.SQL, e.Underlying,
	)
	if e.Hint != "" {
		msg += "\n" + e.Hint
	}
	return msg
}

// reportSubstitutions says how many rows are about to have a value invented for
// them, and what it will be.
//
// The migration runs under MigrationAuto by default, so nothing asks anybody
// first — the log is where this is mentioned or it is not mentioned at all. A
// failure to count is not a failure to migrate: the count is commentary, and
// losing it is not worth rolling back a repair for.
func reportSubstitutions(tx *sql.Tx, stmt Statement, logger MigrationLogger) {
	for _, sub := range stmt.Substitutions {
		table := "comp_" + stmt.Component
		var n int
		q := fmt.Sprintf(`SELECT COUNT(*) FROM %q WHERE %q IS NULL`, table, sub.Column)
		if err := tx.QueryRow(q).Scan(&n); err != nil {
			logger.Warnf("migration: could not count the NULLs in %s.%s: %v", table, sub.Column, err)
			continue
		}
		if n == 0 {
			continue
		}
		logger.Warnf("migration: %s.%s no longer accepts NULL — %s taking the value %s, "+
			"which is what adding the column would have given them",
			table, sub.Column, rowsPhrase(n), sub.Default)
	}
}

func rowsPhrase(n int) string {
	if n == 1 {
		return "1 row is"
	}
	return fmt.Sprintf("%d rows are", n)
}

// migrationHint explains the one failure that leaves a database unopenable
// rather than merely unmigrated.
//
// A rebuild's copy fails on NOT NULL when the column being rebuilt holds NULLs
// and there is no value to put there instead. Since Story 12 that is one column:
// the target_entity_id of a component whose type is entity-ref, where there is
// no honest substitute — a Carrier pointing at nothing is not a Carrier — so
// nothing invents an entity id for it. The engine cannot produce such a row,
// because the column refuses it; a hand-edited database can.
//
// The migration rolls back, so no data is lost, but NewSQLiteStore returns this
// error on every subsequent open and nothing in the engine can repair it. So the
// error says what to do rather than naming a temporary table.
func migrationHint(err error, stmt Statement) string {
	if stmt.Kind != "rebuild_table" || !strings.Contains(err.Error(), "NOT NULL constraint failed") {
		return ""
	}
	return fmt.Sprintf(
		"hint: comp_%s has rows whose new column is NULL, and the rebuilt table does not allow it. "+
			"A rebuild puts a column's default where a NULL used to be, so this is the one column "+
			"with no default to put there: the reference a component of type entity-ref is made of, "+
			"which has no honest value when it is missing. "+
			"The database is unchanged and will fail to open until %q is put back in schema.json as it was.",
		stmt.Component, stmt.Component)
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

// MigrationPlan is what a migration would do: the shape the database is in,
// the differences from the file, and the statements that close them.
//
// Separate from applying it so a caller can look first — the backup in
// checkAndMigrate has to happen between deciding there is work and doing it,
// and Forge shows the same list to somebody who has not saved yet.
type MigrationPlan struct {
	Domain     *DomainSchema
	Changes    []schema.Change
	Statements []Statement

	// FromVersion is what the database records, ToVersion what the file says.
	FromVersion int
	ToVersion   int
}

// Empty reports that applying this plan would do nothing at all.
//
// Deliberately about statements rather than changes. DomainSchema.EntityTypeNames
// comes from SELECT DISTINCT entity_type FROM entities, so a database with no
// entities in it reports every entity type in the file as newly added — changes
// that produce no DDL. A changes-based test would call every such database
// stale, and back it up and migrate it on every single open.
func (p *MigrationPlan) Empty() bool {
	return p == nil || (len(p.Statements) == 0 && p.FromVersion == p.ToVersion)
}

// Plan introspects the database and works out what would have to happen to it,
// without touching it.
func (r *MigrationRunner) Plan() (*MigrationPlan, error) {
	domain, err := IntrospectAll(r.db)
	if err != nil {
		return nil, fmt.Errorf("introspecting db: %w", err)
	}
	changes := schema.Diff(domain.ToDiffSchema(), &r.file, nil)

	// The generator has to see the database as the renames leave it, not as
	// introspection found it.
	//
	// Diff renames a copy — ToDiffSchema's — and this one was handed the
	// original. So a component renamed *and* rebuilt in one migration had the
	// rebuild look for a table under its new name in a snapshot that still held
	// the old one, and produce an error statement; and a renamed column's copy
	// looked for its nullable source under the new name, found nothing, and
	// emitted no COALESCE. Both fail the migration, which the store then returns
	// from every subsequent open.
	//
	// Replayed from the changes rather than worked out again, so there is one
	// statement of what a rename does and this cannot drift from it.
	domain = domain.withRenames(changes)

	stmts := NewGenerator(&r.file, domain, Config{StrictDrop: true}).Generate(changes)
	return &MigrationPlan{
		Domain:      domain,
		Changes:     changes,
		Statements:  stmts,
		FromVersion: domain.SchemaVersion,
		ToVersion:   r.file.SchemaVersion,
	}, nil
}

// Run executes the migration pipeline. Returns nil if the database is already
// up to date or if migration succeeds. Returns *SchemaMigrationError if a DDL
// statement fails (with full rollback), or *MigrationRequiresConfirmation when
// policy=confirm and destructive changes are present.
func (r *MigrationRunner) Run() error {
	plan, err := r.Plan()
	if err != nil {
		return err
	}
	if plan.Empty() {
		return nil
	}
	return r.Apply(plan)
}

// Apply runs a plan: every statement and the meta update in one transaction, so
// a failure leaves the database as it was.
func (r *MigrationRunner) Apply(plan *MigrationPlan) error {
	if plan == nil {
		return nil
	}
	stmts := plan.Statements

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
		// A statement the generator could not build carries Kind "error" and
		// no SQL, and tx.Exec("") succeeds — so the migration used to run every
		// other statement, commit, and report success. When the generator
		// cannot express half a change, the half it could express is the
		// dangerous half: a DROP with no CREATE behind it.
		if stmt.Kind == "error" {
			_ = tx.Rollback()
			return &SchemaMigrationError{
				Change:       stmt.Component,
				ChangeKind:   stmt.Kind,
				SQL:          stmt.SQL,
				Underlying:   errors.New(stmt.Description),
				StatementIdx: i,
				TotalStmts:   len(stmts),
			}
		}
		// Destructive statements are logged as warnings, because under
		// MigrationAuto — the default, and what the engine runs — this is the
		// only notice anybody gets that a table went.
		if stmt.Destructive {
			r.logger.Warnf("migration: %s on %s is destructive: %s", stmt.Kind, stmt.Component, stmt.Description)
		}
		// Counted before the copy runs, because afterwards there is nothing
		// left to count: the NULLs are gone. Only for the columns the generator
		// actually substituted into, so an ordinary rebuild asks nothing.
		reportSubstitutions(tx, stmt, r.logger)

		if _, err := tx.Exec(stmt.SQL); err != nil {
			_ = tx.Rollback()
			return &SchemaMigrationError{
				Change:       stmt.Component,
				ChangeKind:   stmt.Kind,
				SQL:          stmt.SQL,
				Underlying:   err,
				StatementIdx: i,
				TotalStmts:   len(stmts),
				Hint:         migrationHint(err, stmt),
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
		len(stmts), plan.FromVersion, plan.ToVersion)

	return nil
}
