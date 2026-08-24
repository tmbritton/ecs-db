// Package usage answers "who uses this" for a component or an entity type.
//
// It answers in two kinds, and keeps them apart. Which entity types declare a
// component is a statement about the schema: derived from the file alone,
// always available, and true until someone edits it. How many entities exist of
// a type is a statement about a running world, and will be different in a
// second.
//
// Running them together into one number would invite reading a live count as a
// design fact, which is exactly the mistake this panel exists to prevent — the
// question it answers is whether something is safe to delete.
package usage

import (
	"database/sql"
	"os"
	"regexp"
	"strings"

	"github.com/tmbritton/ecs-db/internal/jsonorder"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"

	_ "modernc.org/sqlite" // database/sql driver
)

// UsedBy lists the entity types that declare a component, required or optional,
// in the schema's authored order.
//
// From the file alone, deliberately: this is the fact you need most when
// deciding whether deleting a component is safe, and needing a database for it
// would make it unavailable exactly when no game has ever been run.
func UsedBy(s schema.DatabaseSchema, component string) []string {
	var out []string
	for _, name := range jsonorder.Apply(s.EntityTypeOrder, s.EntityTypes) {
		et := s.EntityTypes[name]
		if declares(et, component) {
			out = append(out, name)
		}
	}
	return out
}

func declares(et schema.EntityType, component string) bool {
	for _, c := range et.RequiredComponents {
		if c == component {
			return true
		}
	}
	for _, c := range et.OptionalComponents {
		if c == component {
			return true
		}
	}
	return false
}

// Counts is what the database holds right now.
//
// Available is false when there is nothing to ask, and that is a real answer
// rather than a count of zero — "nothing is using this" and "nobody could say"
// lead to opposite decisions about deleting it.
type Counts struct {
	Available bool
	Reason    string

	// ByType is entities per entity type; ByComponent is rows per component
	// table. Two different questions, and the panel that asks about a
	// component must not be answered with the first: a component optional on a
	// type is carried by some of that type's entities, not all of them.
	ByType      map[string]int
	ByComponent map[string]int

	// Stale means the database records a different schemaVersion than the
	// schema being edited, so its table shapes and type names describe an
	// older world. The numbers are what the file holds; what they are counting
	// is not what is on screen.
	Stale     bool
	DBVersion int
}

// OfType returns the entities of one type, and whether there is a count to
// give.
func (c Counts) OfType(entityType string) (int, bool) {
	if !c.Available {
		return 0, false
	}
	return c.ByType[entityType], true
}

// OfComponent returns the rows in one component's table.
//
// Absent from the map means the table is not there — a component added in the
// editor and not yet migrated — which is not the same as a table with no rows,
// and must not read as one.
func (c Counts) OfComponent(component string) (int, bool) {
	if !c.Available {
		return 0, false
	}
	n, ok := c.ByComponent[component]
	return n, ok
}

// identifier matches what can safely be concatenated into SQL.
//
// A table name cannot be a bound parameter, so the component's name is
// interpolated — and the name comes from a file anyone can edit. Anything that
// is not a plain identifier is refused rather than quoted, because the engine's
// own generator would refuse it too.
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// AgainstVersion marks the counts stale when the database was built to a
// different schemaVersion than the one being edited.
//
// A stale database is still counted — the numbers are real rows — but they
// count an older world, whose type names and table shapes are not the ones on
// screen. Renaming a type in the editor is the everyday case: the rows are
// still filed under the old name, and an unqualified "0 entities of this type"
// would read as "nothing here" when the entities are simply elsewhere.
func (c Counts) AgainstVersion(fileVersion int) Counts {
	if c.Available && fileVersion != 0 && c.DBVersion != fileVersion {
		c.Stale = true
	}
	return c
}

// Read counts the population, plus the rows in one component's table when a
// component is named.
//
// The type counts are one grouped query for every type at once rather than one
// per row of the panel. The component count is a second query only because it
// reads a different table; there is no combined form that would not be a join
// across every comp_ table in the schema.
func Read(dbPath, component string) Counts {
	if dbPath == "" {
		return Counts{Reason: "no database is configured for this project"}
	}
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return Counts{Reason: "no database yet — the engine creates one on its first run"}
		}
		return Counts{Reason: "the database cannot be read; the log has the details"}
	}

	// Read-only, through the one DSN the tool defines. Forge never writes to
	// the game's database.
	db, err := storage.OpenReadOnly(dbPath)
	if err != nil {
		return Counts{Reason: "the database cannot be opened; the log has the details"}
	}
	defer func() { _ = db.Close() }()

	byType, err := countTypes(db)
	if err != nil {
		// Deliberately not the driver's sentence. This panel is read carefully
		// by someone deciding whether to delete something, and "SQL logic
		// error: no such table: entities (1)" is not a sentence — the usual
		// cause is a database mid-bootstrap, which is transient.
		return Counts{Reason: "the database has no entities to count yet, or is still being created"}
	}

	c := Counts{Available: true, ByType: byType, ByComponent: map[string]int{}}
	if component != "" {
		if n, ok := countComponent(db, component); ok {
			c.ByComponent[component] = n
		}
	}
	if v, err := storage.ReadSchemaVersion(db); err == nil {
		c.DBVersion = v
	}
	return c
}

func countTypes(db *sql.DB) (map[string]int, error) {
	rows, err := db.Query("SELECT entity_type, COUNT(*) FROM entities GROUP BY entity_type")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	byType := map[string]int{}
	for rows.Next() {
		// entity_type is scanned as a nullable string: one NULL row would
		// otherwise fail the scan and take the whole panel unavailable, when
		// the honest answer is that the other types counted fine.
		var name sql.NullString
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			return nil, err
		}
		if name.Valid {
			byType[name.String] = n
		}
	}
	// A partial count read as a whole one would understate what is out there,
	// which is the direction that gets data deleted.
	return byType, rows.Err()
}

// countComponent counts the rows in one component's table.
//
// Absent rather than zero when the table is not there: a component added in
// the editor has no table until the engine migrates, and reporting that as "0
// rows" would say the data is gone rather than not yet created.
func countComponent(db *sql.DB, component string) (int, bool) {
	if !identifier.MatchString(component) {
		return 0, false
	}
	// The engine's own naming rule, from storage: comp_ plus the lowercased
	// component name.
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM "comp_` + strings.ToLower(component) + `"`).Scan(&n)
	if err != nil {
		return 0, false
	}
	return n, true
}
