package storage

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// The generator implements the layout and the diff predicts it. Two places
// saying the same thing is the drift Stories 8 and 9 both had to close, so this
// derives one from the other rather than restating it: for every component type
// the generator can build, build a table and check the columns are the ones
// schema.StorageLayout claims.
//
// If someone adds a component type, this fails until the layout knows about it
// — which is the moment to decide, rather than the moment a migration decides
// for you.
func TestStorageLayout_IsWhatTheGeneratorActuallyBuilds(t *testing.T) {
	cases := []struct {
		compType string
		comp     schema.Component
		want     []string
	}{
		{schema.ComponentTypeObject, schema.Component{
			Type:       schema.ComponentTypeObject,
			Properties: map[string]schema.Property{"a": {Type: schema.PropertyTypeInteger}},
		}, []string{"a"}},
		{schema.ComponentTypeEntityRef, schema.Component{Type: schema.ComponentTypeEntityRef}, []string{"target_entity_id"}},
		{schema.ComponentTypeString, schema.Component{Type: schema.ComponentTypeString}, []string{"value"}},
		{schema.ComponentTypeInteger, schema.Component{Type: schema.ComponentTypeInteger}, []string{"value"}},
		{schema.ComponentTypeNumber, schema.Component{Type: schema.ComponentTypeNumber}, []string{"value"}},
		{schema.ComponentTypeBoolean, schema.Component{Type: schema.ComponentTypeBoolean}, []string{"value"}},
		{schema.ComponentTypeArray, schema.Component{
			Type: schema.ComponentTypeArray, Items: &schema.Property{Type: schema.PropertyTypeString},
		}, []string{"value"}},
	}

	// Every type a schema may declare has a case here, taken from the schema
	// package's own list rather than from a copy of it. Without this, adding a
	// component type with a new shape passes silently: StorageLayout falls
	// through to LayoutValue, the generator builds something else, and nothing
	// compares them until a migration does.
	covered := make(map[string]bool, len(cases))
	for _, tc := range cases {
		covered[tc.compType] = true
	}
	for _, declared := range schema.ComponentTypes() {
		if !covered[declared] {
			t.Errorf("component type %q has no case here, so nothing checks that "+
				"StorageLayout agrees with the columns the generator builds for it", declared)
		}
	}

	for _, tc := range cases {
		t.Run(tc.compType, func(t *testing.T) {
			s := schema.DatabaseSchema{
				SchemaVersion: 1,
				Components:    map[string]schema.Component{"Probe": tc.comp},
				EntityTypes: map[string]schema.EntityType{
					"Thing": {RequiredComponents: []string{"Probe"}, ValidationLevel: "strict"},
				},
			}
			store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "w.sqlite"), s, "")
			if err != nil {
				t.Fatalf("building a %s component: %v", tc.compType, err)
			}
			defer func() { _ = store.Close() }()

			rows, err := store.DB().Query(`SELECT name FROM pragma_table_info('comp_probe') WHERE name != 'entity_id'`)
			if err != nil {
				t.Fatalf("reading columns: %v", err)
			}
			defer func() { _ = rows.Close() }()
			var got []string
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					t.Fatalf("scanning: %v", err)
				}
				got = append(got, name)
			}
			sort.Strings(got)

			// What the columns say the layout is, against what StorageLayout
			// predicts for the same type.
			var built string
			switch {
			case len(got) == 1 && got[0] == "value":
				built = schema.LayoutValue
			case len(got) == 1 && got[0] == "target_entity_id":
				built = schema.LayoutTargetEntityID
			default:
				built = schema.LayoutColumns
			}
			if want := schema.StorageLayout(tc.compType); built != want {
				t.Errorf("a %s component builds columns %v (the %q layout), and StorageLayout says %q",
					tc.compType, got, built, want)
			}
			// And the columns really are the ones expected, so "the layout
			// agrees" cannot be satisfied by both sides being wrong.
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("a %s component builds %v, want %v", tc.compType, got, tc.want)
			}
		})
	}
}

// A statement the generator could not build carries Kind "error" and no SQL,
// and tx.Exec("") succeeds. So a migration whose CREATE the generator refused
// used to run the matching DROP, commit, and report success — losing the table
// and telling nobody. The half a broken change can still express is the
// dangerous half.
func TestMigration_AStatementTheGeneratorCouldNotBuildStopsTheMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.sqlite")
	mk := func(v int, comp schema.Component) schema.DatabaseSchema {
		return schema.DatabaseSchema{
			SchemaVersion: v,
			Components:    map[string]schema.Component{"X": comp},
			EntityTypes: map[string]schema.EntityType{
				"T": {RequiredComponents: []string{"X"}, ValidationLevel: "strict"},
			},
		}
	}
	store, err := NewSQLiteStore(path, mk(1, schema.Component{Type: schema.ComponentTypeEntityRef}), "")
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	if _, err := store.DB().Exec(
		`INSERT INTO entities (id, entity_type, created_tick) VALUES (1, 'T', 0)`); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if _, err := store.DB().Exec(
		`INSERT INTO comp_x (entity_id, target_entity_id) VALUES (1, 1)`); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	_ = store.Close()

	// A type the generator cannot build. ValidateSchema refuses one too, so
	// this can only arrive from a caller that skipped validation — which is
	// exactly the caller the generator's own refusal exists for.
	_, err = NewSQLiteStore(path, mk(2, schema.Component{Type: "date"}), "")
	if err == nil {
		t.Fatal("a migration the generator could not build reported success")
	}

	// And the table it would have dropped is still there, with its row.
	back, err := NewSQLiteStore(path, mk(2, schema.Component{Type: schema.ComponentTypeEntityRef}), "")
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer func() { _ = back.Close() }()
	var n int
	if err := back.DB().QueryRow(`SELECT COUNT(*) FROM comp_x`).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 1 {
		t.Errorf("%d rows left; the failed migration took the table with it", n)
	}
}

// Under MigrationAuto — the default, and what the engine runs — a dropped table
// is announced only in the log. Announcing it at the same level as every
// ordinary statement makes it the one line nobody reads.
func TestMigration_ADroppedTableIsWarnedAbout(t *testing.T) {
	mk := func(v int, comp schema.Component) schema.DatabaseSchema {
		return schema.DatabaseSchema{
			SchemaVersion: v,
			Components:    map[string]schema.Component{"X": comp},
			EntityTypes: map[string]schema.EntityType{
				"T": {RequiredComponents: []string{"X"}, ValidationLevel: "strict"},
			},
		}
	}
	path := filepath.Join(t.TempDir(), "w.sqlite")
	s1, err := NewSQLiteStore(path, mk(1, schema.Component{Type: schema.ComponentTypeString}), "")
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	_ = s1.Close()

	log := &capturingLogger{}
	s2, err := NewSQLiteStoreWithConfig(path, StoreConfig{
		Schema:          mk(2, schema.Component{Type: schema.ComponentTypeEntityRef}),
		MigrationPolicy: MigrationAuto,
		Logger:          log,
	})
	if err != nil {
		t.Fatalf("migrating: %v", err)
	}
	defer func() { _ = s2.Close() }()

	var warnedAboutTheDrop bool
	for _, w := range log.warnings {
		if strings.Contains(w, "drop_table") && strings.Contains(w, "destructive") {
			warnedAboutTheDrop = true
		}
	}
	if !warnedAboutTheDrop {
		t.Errorf("warnings = %v, want one naming the drop as destructive", log.warnings)
	}
}

// capturingLogger keeps what it was told, so a test can assert the level as
// well as the message.
type capturingLogger struct {
	infos    []string
	warnings []string
}

func (l *capturingLogger) Infof(format string, args ...interface{}) {
	l.infos = append(l.infos, fmt.Sprintf(format, args...))
}

func (l *capturingLogger) Warnf(format string, args ...interface{}) {
	l.warnings = append(l.warnings, fmt.Sprintf(format, args...))
}
