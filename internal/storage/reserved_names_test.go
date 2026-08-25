package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// sqliteKeywords is SQLite's keyword list, from
// https://sqlite.org/lang_keywords.html. It is here rather than in the schema
// package because this is the test that asks SQLite what it thinks of them.
var sqliteKeywords = strings.Fields(`
ABORT ACTION ADD AFTER ALL ALTER ALWAYS ANALYZE AND AS ASC ATTACH AUTOINCREMENT
BEFORE BEGIN BETWEEN BY CASCADE CASE CAST CHECK COLLATE COLUMN COMMIT CONFLICT
CONSTRAINT CREATE CROSS CURRENT CURRENT_DATE CURRENT_TIME CURRENT_TIMESTAMP
DATABASE DEFAULT DEFERRABLE DEFERRED DELETE DESC DETACH DISTINCT DO DROP EACH
ELSE END ESCAPE EXCEPT EXCLUDE EXCLUSIVE EXISTS EXPLAIN FAIL FILTER FIRST
FOLLOWING FOR FOREIGN FROM FULL GENERATED GLOB GROUP GROUPS HAVING IF IGNORE
IMMEDIATE IN INDEX INDEXED INITIALLY INNER INSERT INSTEAD INTERSECT INTO IS
ISNULL JOIN KEY LAST LEFT LIKE LIMIT MATCH MATERIALIZED NATURAL NO NOT NOTHING
NOTNULL NULL NULLS OF OFFSET ON OR ORDER OTHERS OUTER OVER PARTITION PLAN
PRAGMA PRECEDING PRIMARY QUERY RAISE RANGE RECURSIVE REFERENCES REGEXP REINDEX
RELEASE RENAME REPLACE RESTRICT RETURNING RIGHT ROLLBACK ROW ROWS SAVEPOINT
SELECT SET TABLE TEMP TEMPORARY THEN TIES TO TRANSACTION TRIGGER UNBOUNDED
UNION UNIQUE UPDATE USING VACUUM VALUES VIEW VIRTUAL WHEN WHERE WINDOW WITH
WITHOUT
`)

// usableAsColumn asks SQLite directly: can this name be a column that holds a
// value and gives it back? Three answers, and the middle one is the reason the
// schema package has a list at all.
func usableAsColumn(t *testing.T, col string) (ok bool, readBack string) {
	t.Helper()
	db, err := sql.Open("sqlite", DSN(filepath.Join(t.TempDir(), "probe.db")))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(fmt.Sprintf(
		"CREATE TABLE t (entity_id INTEGER PRIMARY KEY, %s INTEGER NOT NULL)", col)); err != nil {
		return false, ""
	}
	if _, err := db.Exec(fmt.Sprintf("INSERT INTO t (entity_id, %s) VALUES (1, 42)", col)); err != nil {
		return false, ""
	}
	var got any
	if err := db.QueryRow(fmt.Sprintf("SELECT %s FROM t WHERE entity_id = 1", col)).Scan(&got); err != nil {
		return false, ""
	}
	return fmt.Sprint(got) == "42", fmt.Sprint(got)
}

// The schema package refuses a set of property names by name, because it has no
// database to ask. This is the test that asks, so the list cannot drift away
// from what SQLite actually does — a SQLite upgrade that changes the answer
// fails here rather than in somebody's map.
//
// It has to be exact in both directions. Refusing too little is the bug the
// story is about; refusing too much would take away names a game schema wants,
// and 84 of SQLite's 147 keywords — action, key, first, last, row, match,
// range — are perfectly good columns.
func TestReservedColumnNames_MatchesWhatSQLiteActuallyRefuses(t *testing.T) {
	var shouldRefuse, shouldAccept []string
	for _, kw := range sqliteKeywords {
		col := strings.ToLower(kw)
		if ok, _ := usableAsColumn(t, col); ok {
			shouldAccept = append(shouldAccept, col)
		} else {
			shouldRefuse = append(shouldRefuse, col)
		}
	}

	for _, col := range shouldRefuse {
		if schema.ValidColumnName(col) {
			t.Errorf("%q is accepted, and SQLite cannot use it as a column", col)
		}
	}
	for _, col := range shouldAccept {
		// entity_id is not a keyword; it is refused for a different reason.
		if col == "entity_id" {
			continue
		}
		if !schema.ValidColumnName(col) {
			t.Errorf("%q is refused, and SQLite is perfectly happy with it", col)
		}
	}
	sort.Strings(shouldRefuse)
	t.Logf("SQLite refuses %d of %d keywords as column names", len(shouldRefuse), len(sqliteKeywords))
}

// The three that do not fail. They build, they accept a write, and they read
// back as the clock — which is why a list of "words SQLite rejects" would not
// have been enough.
func TestReservedColumnNames_CatchesTheOnesThatFailSilently(t *testing.T) {
	for _, col := range []string{"current_date", "current_time", "current_timestamp"} {
		ok, readBack := usableAsColumn(t, col)
		if ok {
			t.Fatalf("%q round-tripped; this test is asserting the wrong thing", col)
		}
		if readBack == "" {
			t.Errorf("%q failed loudly; it used to build a column and return the clock", col)
		}
		if schema.ValidColumnName(col) {
			t.Errorf("%q is accepted as a property name and stores 42 as %q", col, readBack)
		}
	}
}

// entity_id is the primary key every component table already has, so a property
// of that name is a duplicate column — caught at bootstrap today, by a message
// that names the component and not the property.
func TestReservedColumnNames_RefusesTheGeneratorsOwnColumn(t *testing.T) {
	if schema.ValidColumnName("entity_id") {
		t.Error("entity_id is accepted as a property name")
	}
	s := schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Probe": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"entity_id": {Type: schema.PropertyTypeInteger},
			}},
		},
		EntityTypes: map[string]schema.EntityType{
			"Thing": {RequiredComponents: []string{"Probe"}, ValidationLevel: "strict"},
		},
	}
	err := schema.ValidateSchema(s)
	if err == nil {
		t.Fatal("a property called entity_id validated")
	}
	if !strings.Contains(err.Error(), "entity_id") {
		t.Errorf("error = %q, want it to name the property", err)
	}
}
