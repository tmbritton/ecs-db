// Package migration answers one question for the editor: if the engine
// restarted right now against this database, what would it do to it?
//
// Forge never migrates anything. The engine owns that, at its own startup,
// through storage.MigrationRunner. What Forge owns is telling the author
// before they save that the edit in front of them drops a column — because
// after the engine has run, the data that was in it is gone and no amount of
// editing schema.json brings it back.
//
// The answer therefore comes from the engine's own generator rather than from
// anything reimplemented here. A preview that classifies destructiveness by
// its own rules is a preview that can disagree with what actually happens,
// and a warning that is wrong in the safe direction is worse than none.
package migration

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"

	_ "modernc.org/sqlite" // database/sql driver
)

// Preview is what the engine would do to the database on its next start.
//
// Available separates "nothing will happen" from "we could not look", which
// render differently and must never be conflated: an empty Statements slice
// on an unavailable preview means we do not know, not that it is safe.
type Preview struct {
	Available  bool
	Reason     string // why not, when Available is false
	Statements []storage.Statement

	// Failed distinguishes "we looked and could not read it" from "there is
	// nothing to look at". Both leave Available false, but only one of them
	// means a save is safe: an absent database has no data to destroy, while a
	// database we could not inspect may have plenty.
	Failed bool

	// SnapshotUnknown means the file as last saved could not be parsed, so
	// SnapshotVersion says nothing and Stale() must not draw a conclusion from
	// it. Distinct from a snapshot version that is genuinely zero.
	SnapshotUnknown bool

	// DBVersion is the schema_version the database records; FileVersion is the
	// one being edited, and SnapshotVersion the one last saved.
	DBVersion       int
	FileVersion     int
	SnapshotVersion int

	// Problems are changes the engine's own generator could not turn into
	// DDL — an unknown component, a property type with no SQL mapping. They
	// carry a description and no SQL, so they cannot be shown as statements,
	// and they matter more than the ones that can: each is a migration that
	// would fail at the engine's next start.
	Problems []string
}

// WillMigrate reports whether the engine would actually run this plan.
//
// It only migrates when the database's recorded schema_version differs from
// schema.json's — storage.checkAndMigrate returns early when they match, so an
// edit saved without a version bump is never applied. The statements below are
// real and the engine will run none of them, which is the one case where a
// correct list of changes is still a misleading answer.
// See TestEngine_SkipsMigrationWhenTheVersionIsUnchanged.
func (p Preview) WillMigrate() bool {
	return p.Available && p.DBVersion != p.FileVersion
}

// Stale reports that the database already disagreed with the file as last
// saved, before this edit — a different fact from anything the current edit
// does, and the same distinction the engine-status readout makes.
func (p Preview) Stale() bool {
	if !p.Available || p.SnapshotUnknown || p.SnapshotVersion == 0 {
		return false
	}
	return p.DBVersion != p.SnapshotVersion
}

// Destructive reports whether any statement would take data with it.
func (p Preview) Destructive() bool {
	for _, st := range p.Statements {
		if st.Destructive {
			return true
		}
	}
	return false
}

// DestructiveStatements returns only the statements that lose data — the ones
// a confirmation dialog exists to show.
func (p Preview) DestructiveStatements() []storage.Statement {
	out := make([]storage.Statement, 0, len(p.Statements))
	for _, st := range p.Statements {
		if st.Destructive {
			out = append(out, st)
		}
	}
	return out
}

// open connects to the game's database the only way Forge ever may: read-only,
// through the single DSN the tool defines. Inspecting a database in order to
// warn about it must not be able to change it.
//
// It is its own function so a test can attempt a write through exactly the
// connection Check uses, rather than through a second one built to match.
func open(dbPath string) (*sql.DB, error) {
	return storage.OpenReadOnly(dbPath)
}

// partition splits the generator's output into statements the author can read
// as SQL and problems that have none.
//
// It is separate from Check because the generator's error paths are defensive
// — no edit reachable through the editor produces one today — and a guard with
// no test is a guard that quietly stops working. This one is testable directly.
func partition(stmts []storage.Statement) Preview {
	p := Preview{Available: true}
	for _, st := range stmts {
		if st.Kind == "error" || st.SQL == "" {
			p.Problems = append(p.Problems, st.Description)
			continue
		}
		p.Statements = append(p.Statements, st)
	}
	return p
}

// Holds reports whether a save has to stop and ask first.
//
// Destructive changes are the obvious half. The other half is a check that
// could not run: an unreadable or locked database is not evidence that a save
// is safe, and treating it as such is the one place this package could fail
// open. An absent database is different again — there is nothing there to
// destroy — and is the reason Failed exists separately from Available.
func (p Preview) Holds() bool { return p.Destructive() || p.Failed }

// Check compares the database as built against the schema the editor holds.
//
// current is what is being edited; snapshot is the file on disk, needed only
// for entity-type change detection, which the database cannot report because
// entity-type specs are not stored in it.
//
// Every failure is a reason rather than an error: an absent database is the
// ordinary case before a game has ever been run, and the editor must keep
// working. The caller gets a Preview it can render either way.
func Check(dbPath string, current, snapshot schema.DatabaseSchema) Preview {
	if dbPath == "" {
		return Preview{Reason: "no database configured"}
	}
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return Preview{Reason: "no database yet — the engine will create one on first run"}
		}
		return Preview{Failed: true, Reason: fmt.Sprintf("cannot read %s: %v", dbPath, err)}
	}

	db, err := open(dbPath)
	if err != nil {
		return Preview{Failed: true, Reason: fmt.Sprintf("cannot open %s: %v", dbPath, err)}
	}
	defer func() { _ = db.Close() }()

	domain, err := storage.IntrospectAll(db)
	if err != nil {
		return Preview{Failed: true, Reason: fmt.Sprintf("cannot read the database's shape: %v", err)}
	}

	// nil, not &snapshot: this must predict what the engine will do, and its
	// runner passes nil here. The third argument only enables ChangedEntityType
	// detection, which produces no DDL either way — so passing the snapshot
	// would add changes to the plan that no migration would ever act on.
	changes := schema.Diff(domain.ToDiffSchema(), &current, nil)

	// StrictDrop: true asks the generator for everything, including the
	// destructive statements it would otherwise filter out. That is the whole
	// point here — those are exactly the ones worth showing.
	gen := storage.NewGenerator(&current, domain, storage.Config{StrictDrop: true})

	p := partition(gen.Generate(changes))
	p.DBVersion = domain.SchemaVersion
	p.FileVersion = current.SchemaVersion
	p.SnapshotVersion = snapshot.SchemaVersion
	return p
}
