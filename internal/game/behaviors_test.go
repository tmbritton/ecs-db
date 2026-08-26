package game_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/game"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
)

// ── setup ─────────────────────────────────────────────────────────────────────

func setupBehaviorDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY AUTOINCREMENT, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_position (entity_id INTEGER PRIMARY KEY, x REAL NOT NULL DEFAULT 0, y REAL NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_mood (entity_id INTEGER PRIMARY KEY, anger REAL NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	if err := storage.EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	return db
}

func addEntity(t *testing.T, db *sql.DB, entityType string) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO entities (entity_type, created_tick) VALUES (?, 0)`, entityType)
	if err != nil {
		t.Fatalf("insert %s: %v", entityType, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

// contextMachineJSON is a machine whose context seeds one component field, which
// is how a machine acquires the power to write to an entity at all.
func contextMachineJSON(id, field string) string {
	return fmt.Sprintf(`{
	  "id": %q,
	  "initial": "idle",
	  "context": { %q: 0 },
	  "states": { "idle": { "on": {} } }
	}`, id, field)
}

// behaviorSchema binds each named type to the machine it names.
func behaviorSchema(bindings map[string]string) schema.DatabaseSchema {
	types := make(map[string]schema.EntityType, len(bindings))
	for name, machine := range bindings {
		types[name] = schema.EntityType{
			RequiredComponents: []string{"Position"},
			Behavior:           machine,
		}
	}
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Position": {Type: "object", Properties: map[string]schema.Property{
				"x": {Type: "number"}, "y": {Type: "number"},
			}},
			// Declared in the schema and not on the type below, so a machine
			// seeding it is asking to write something the type forbids.
			"Mood": {Type: "object", Properties: map[string]schema.Property{
				"anger": {Type: "number"},
			}},
		},
		EntityTypes: types,
	}
}

// recordAction is an action that does nothing, so a machine can have an entry
// list without the test needing a world.
type recordAction struct{ ran *int }

func (a recordAction) Run(agent.ActionContext) error {
	if a.ran != nil {
		*a.ran++
	}
	return nil
}

// failAction refuses, which is how an author's machine fails at start.
type failAction struct{}

func (failAction) Run(agent.ActionContext) error { return fmt.Errorf("no") }

func behaviorRegistry(t *testing.T, ran *int) *agent.Registry {
	t.Helper()
	r := agent.NewRegistry()
	r.RegisterAction(agent.ActionMeta{Name: "noop"}, recordAction{ran: ran})
	r.RegisterAction(agent.ActionMeta{Name: "boom"}, failAction{})
	return r
}

// loadMachines writes each machine's JSON to a temp dir and scans it, which is
// the only way in — the loader's map is private, deliberately.
func loadMachines(t *testing.T, registry *agent.Registry, s schema.DatabaseSchema, machines map[string]string) *agent.Loader {
	t.Helper()
	dir := t.TempDir()
	for id, body := range machines {
		if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(body), 0o600); err != nil {
			t.Fatalf("writing machine %s: %v", id, err)
		}
	}
	loader := agent.NewLoader(registry, s)
	if _, err := loader.ScanDir(dir, "test"); err != nil {
		t.Fatalf("ScanDir: %v", err)
	}
	return loader
}

func machineJSON(id, entryAction string) string {
	entry := ""
	if entryAction != "" {
		entry = fmt.Sprintf(`"entry": [{"type": %q}],`, entryAction)
	}
	return fmt.Sprintf(`{
	  "id": %q,
	  "initial": "idle",
	  "states": { "idle": { %s "on": {} } }
	}`, id, entry)
}

func machineState(t *testing.T, db *sql.DB, entityID int64, machineID string) (string, int64, bool) {
	t.Helper()
	var states string
	var updated int64
	err := db.QueryRow(
		`SELECT current_states, updated_at FROM behavior_components WHERE entity_id = ? AND machine_id = ?`,
		entityID, machineID).Scan(&states, &updated)
	if err == sql.ErrNoRows {
		return "", 0, false
	}
	if err != nil {
		t.Fatalf("reading behavior_components: %v", err)
	}
	return states, updated, true
}

func sync(t *testing.T, p game.BehaviorSync) game.BehaviorResult {
	t.Helper()
	res, err := game.SyncBehaviors(context.Background(), p)
	if err != nil {
		t.Fatalf("SyncBehaviors: %v", err)
	}
	return res
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestSyncBehaviors_StartsTheMachineATypeDeclares(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Goblin": "goblin"})
	ran := 0
	registry := behaviorRegistry(t, &ran)
	loader := loadMachines(t, registry, s, map[string]string{"goblin": machineJSON("goblin", "noop")})
	id := addEntity(t, db, "Goblin")

	res := sync(t, game.BehaviorSync{
		DB: db, Schema: s, Loader: loader, Registry: registry, Tick: 7, TickDurationMs: 16,
	})

	if res.Started != 1 || res.Running != 0 || len(res.Problems) != 0 {
		t.Fatalf("result = %+v, want 1 started and no problems", res)
	}
	states, updated, ok := machineState(t, db, id, "goblin")
	if !ok {
		t.Fatal("no behavior_components row for the goblin")
	}
	if !strings.Contains(states, "idle") {
		t.Errorf("current_states = %q, want the machine's initial state", states)
	}
	if updated != 7 {
		t.Errorf("updated_at = %d, want the tick it was started at (7)", updated)
	}
	if ran != 1 {
		t.Errorf("entry action ran %d times, want 1", ran)
	}
}

func TestSyncBehaviors_SecondRunStartsNothing(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Goblin": "goblin"})
	ran := 0
	registry := behaviorRegistry(t, &ran)
	loader := loadMachines(t, registry, s, map[string]string{"goblin": machineJSON("goblin", "noop")})
	id := addEntity(t, db, "Goblin")

	p := game.BehaviorSync{DB: db, Schema: s, Loader: loader, Registry: registry, Tick: 7, TickDurationMs: 16}
	sync(t, p)
	// Pretend the machine has since moved on, the way a running game would.
	if _, err := db.Exec(
		`UPDATE behavior_components SET current_states = '["wandering"]', updated_at = 99 WHERE entity_id = ?`,
		id); err != nil {
		t.Fatalf("moving the machine on: %v", err)
	}

	p.Tick = 120
	res := sync(t, p)

	if res.Started != 0 || res.Running != 1 {
		t.Fatalf("result = %+v, want 0 started and 1 already running", res)
	}
	states, updated, _ := machineState(t, db, id, "goblin")
	if states != `["wandering"]` || updated != 99 {
		t.Errorf("machine was restarted: states %q, updated_at %d", states, updated)
	}
	if ran != 1 {
		t.Errorf("entry action ran %d times across two loads, want 1", ran)
	}
}

func TestSyncBehaviors_LeavesAnUnboundTypeAlone(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Goblin": "goblin", "Player": ""})
	registry := behaviorRegistry(t, nil)
	loader := loadMachines(t, registry, s, map[string]string{"goblin": machineJSON("goblin", "")})
	addEntity(t, db, "Goblin")
	player := addEntity(t, db, "Player")

	res := sync(t, game.BehaviorSync{
		DB: db, Schema: s, Loader: loader, Registry: registry, TickDurationMs: 16,
	})

	if res.Started != 1 {
		t.Fatalf("result = %+v, want only the goblin started", res)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM behavior_components WHERE entity_id = ?`, player).Scan(&n); err != nil {
		t.Fatalf("counting the player's machines: %v", err)
	}
	if n != 0 {
		t.Errorf("the player has %d machines, want none — its type declares no behaviour", n)
	}
}

func TestSyncBehaviors_ReportsAMachineThatDoesNotResolve(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Goblin": "goblin", "Orc": "orc"})
	registry := behaviorRegistry(t, nil)
	loader := loadMachines(t, registry, s, map[string]string{"goblin": machineJSON("goblin", "")})
	addEntity(t, db, "Orc")
	addEntity(t, db, "Orc")
	goblin := addEntity(t, db, "Goblin")

	res := sync(t, game.BehaviorSync{
		DB: db, Schema: s, Loader: loader, Registry: registry, TickDurationMs: 16,
	})

	if len(res.Problems) != 1 {
		t.Fatalf("problems = %v, want exactly one — the fault is in the schema, which has one line for it", res.Problems)
	}
	if !strings.Contains(res.Problems[0], "Orc") || !strings.Contains(res.Problems[0], "orc") {
		t.Errorf("problem %q names neither the type nor the machine", res.Problems[0])
	}
	if res.Started != 1 {
		t.Errorf("started = %d, want the goblin to have started anyway", res.Started)
	}
	if _, _, ok := machineState(t, db, goblin, "goblin"); !ok {
		t.Error("the goblin did not start")
	}
}

func TestSyncBehaviors_ReportsAnUnresolvedMachineWithNoEntities(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Orc": "orc"})
	registry := behaviorRegistry(t, nil)
	loader := loadMachines(t, registry, s, nil)

	res := sync(t, game.BehaviorSync{
		DB: db, Schema: s, Loader: loader, Registry: registry, TickDurationMs: 16,
	})

	if len(res.Problems) != 1 {
		t.Fatalf("problems = %v, want one — a binding that cannot resolve is broken before anything is spawned", res.Problems)
	}
}

func TestSyncBehaviors_StartsAlongsideAnotherMachine(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Goblin": "goblin"})
	registry := behaviorRegistry(t, nil)
	loader := loadMachines(t, registry, s, map[string]string{"goblin": machineJSON("goblin", "")})
	id := addEntity(t, db, "Goblin")
	// Something else already runs on this entity — behavior_components is keyed
	// (entity, machine), so "has a machine" is not "has its type's machine".
	if _, err := db.Exec(
		`INSERT INTO behavior_components (entity_id, machine_id, current_states, updated_at) VALUES (?, 'burning', '["alight"]', 3)`,
		id); err != nil {
		t.Fatalf("attaching the other machine: %v", err)
	}

	res := sync(t, game.BehaviorSync{
		DB: db, Schema: s, Loader: loader, Registry: registry, TickDurationMs: 16,
	})

	if res.Started != 1 {
		t.Fatalf("result = %+v, want the goblin machine started beside the burning one", res)
	}
	if _, _, ok := machineState(t, db, id, "goblin"); !ok {
		t.Error("no goblin machine — an unrelated machine was mistaken for it")
	}
	if _, _, ok := machineState(t, db, id, "burning"); !ok {
		t.Error("the burning machine is gone")
	}
}

func TestSyncBehaviors_OneFailingEntryDoesNotStopTheNext(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Goblin": "goblin", "Orc": "orc"})
	registry := behaviorRegistry(t, nil)
	loader := loadMachines(t, registry, s, map[string]string{
		"goblin": machineJSON("goblin", "boom"),
		"orc":    machineJSON("orc", ""),
	})
	goblin := addEntity(t, db, "Goblin")
	orc := addEntity(t, db, "Orc")

	res := sync(t, game.BehaviorSync{
		DB: db, Schema: s, Loader: loader, Registry: registry, TickDurationMs: 16,
	})

	if res.Started != 1 {
		t.Fatalf("result = %+v, want the orc started despite the goblin failing", res)
	}
	if len(res.Problems) != 1 || !strings.Contains(res.Problems[0], fmt.Sprint(goblin)) {
		t.Fatalf("problems = %v, want one naming entity %d", res.Problems, goblin)
	}
	if _, _, ok := machineState(t, db, goblin, "goblin"); ok {
		t.Error("the goblin has a machine row after its entry action failed — half a start was committed")
	}
	if _, _, ok := machineState(t, db, orc, "orc"); !ok {
		t.Error("the orc did not start")
	}
}

func TestSyncBehaviors_RefusesAMachineWhoseContextTheTypeForbids(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Goblin": "goblin"})
	registry := behaviorRegistry(t, nil)
	loader := loadMachines(t, registry, s, map[string]string{"goblin": contextMachineJSON("goblin", "anger")})
	id := addEntity(t, db, "Goblin")

	res := sync(t, game.BehaviorSync{
		DB: db, Schema: s, Loader: loader, Registry: registry, TickDurationMs: 16,
	})

	if res.Started != 0 {
		t.Errorf("started %d machines, want none — the machine writes a component the type does not declare", res.Started)
	}
	if len(res.Problems) != 1 {
		t.Fatalf("problems = %v, want one", res.Problems)
	}
	for _, want := range []string{"Goblin", "goblin", "Mood"} {
		if !strings.Contains(res.Problems[0], want) {
			t.Errorf("problem %q does not name %q", res.Problems[0], want)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM comp_mood WHERE entity_id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("counting mood rows: %v", err)
	}
	if n != 0 {
		t.Errorf("the machine wrote %d Mood rows onto a type that forbids them", n)
	}
}

func TestSyncBehaviors_StartsAMachineWhoseContextTheTypeDeclares(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Goblin": "goblin"})
	et := s.EntityTypes["Goblin"]
	et.OptionalComponents = []string{"Mood"}
	s.EntityTypes["Goblin"] = et
	registry := behaviorRegistry(t, nil)
	loader := loadMachines(t, registry, s, map[string]string{"goblin": contextMachineJSON("goblin", "anger")})
	id := addEntity(t, db, "Goblin")

	res := sync(t, game.BehaviorSync{
		DB: db, Schema: s, Loader: loader, Registry: registry, TickDurationMs: 16,
	})

	if res.Started != 1 || len(res.Problems) != 0 {
		t.Fatalf("result = %+v, want it started — the type declares the component the context seeds", res)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM comp_mood WHERE entity_id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("counting mood rows: %v", err)
	}
	if n != 1 {
		t.Errorf("the machine's context seeded %d Mood rows, want 1", n)
	}
}

func TestSyncBehaviors_ALenientTypeStartsAndSaysSo(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Goblin": "goblin"})
	et := s.EntityTypes["Goblin"]
	et.ValidationLevel = schema.ValidationWarning
	s.EntityTypes["Goblin"] = et
	registry := behaviorRegistry(t, nil)
	loader := loadMachines(t, registry, s, map[string]string{"goblin": contextMachineJSON("goblin", "anger")})
	addEntity(t, db, "Goblin")

	res := sync(t, game.BehaviorSync{
		DB: db, Schema: s, Loader: loader, Registry: registry, TickDurationMs: 16,
	})

	if res.Started != 1 || len(res.Problems) != 0 {
		t.Fatalf("result = %+v, want it started — the schema said to allow it", res)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one saying what was written anyway", res.Warnings)
	}
	for _, want := range []string{"Goblin", "goblin", "Mood"} {
		if !strings.Contains(res.Warnings[0], want) {
			t.Errorf("warning %q does not name %q", res.Warnings[0], want)
		}
	}
}

// Entities are reported in id order, which SQLite happens to give for this
// query whether or not the ORDER BY is there — a table scan comes back in rowid
// order — so removing the clause survives this test. It is here for the day the
// query gains an index or a join and the plan changes underneath it, which is
// when a silent reordering would otherwise arrive.
func TestSyncBehaviors_ReportsEntitiesInIdOrder(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Goblin": "goblin"})
	registry := behaviorRegistry(t, nil)
	// Every one of them fails, so every one of them has a sentence, and the
	// order of the sentences is the order the entities were tried in.
	loader := loadMachines(t, registry, s, map[string]string{"goblin": machineJSON("goblin", "boom")})
	var ids []int64
	for range 4 {
		ids = append(ids, addEntity(t, db, "Goblin"))
	}

	res := sync(t, game.BehaviorSync{
		DB: db, Schema: s, Loader: loader, Registry: registry, TickDurationMs: 16,
	})

	if len(res.Problems) != len(ids) {
		t.Fatalf("problems = %v, want one per goblin", res.Problems)
	}
	for i, id := range ids {
		if !strings.Contains(res.Problems[i], fmt.Sprint(id)) {
			t.Fatalf("problem %d is %q, want it to be about entity %d — the rows arrive in whatever order SQLite likes",
				i, res.Problems[i], id)
		}
	}
}

func TestSyncBehaviors_RefusesWhatItCannotWorkWithout(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Goblin": "goblin"})
	registry := behaviorRegistry(t, nil)
	loader := loadMachines(t, registry, s, map[string]string{"goblin": machineJSON("goblin", "")})
	addEntity(t, db, "Goblin")

	full := game.BehaviorSync{DB: db, Schema: s, Loader: loader, Registry: registry, TickDurationMs: 16}
	for _, c := range []struct {
		name string
		with func(game.BehaviorSync) game.BehaviorSync
	}{
		{"no database", func(p game.BehaviorSync) game.BehaviorSync { p.DB = nil; return p }},
		{"no loader", func(p game.BehaviorSync) game.BehaviorSync { p.Loader = nil; return p }},
		// The registry is the one a nil check earns its keep on: StartAgent
		// hands it to every entry action, so without it the first machine to
		// start takes the process down rather than returning anything.
		{"no registry", func(p game.BehaviorSync) game.BehaviorSync { p.Registry = nil; return p }},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := game.SyncBehaviors(context.Background(), c.with(full)); err == nil {
				t.Fatalf("%s was accepted", c.name)
			}
		})
	}
}

func TestSyncBehaviors_RefusesATickDurationOfZero(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Goblin": "goblin"})
	registry := behaviorRegistry(t, nil)
	loader := loadMachines(t, registry, s, map[string]string{"goblin": machineJSON("goblin", "")})
	addEntity(t, db, "Goblin")

	if _, err := game.SyncBehaviors(context.Background(), game.BehaviorSync{
		DB: db, Schema: s, Loader: loader, Registry: registry,
	}); err == nil {
		t.Fatal("a tick duration of zero was accepted; every after-transition would be scheduled a thousand ticks early")
	}
}

func TestSyncBehaviors_ReportsTypesInADeterministicOrder(t *testing.T) {
	db := setupBehaviorDB(t)
	s := behaviorSchema(map[string]string{"Zombie": "zombie", "Orc": "orc", "Bat": "bat"})
	registry := behaviorRegistry(t, nil)
	loader := loadMachines(t, registry, s, nil)

	// Agreeing with itself is not enough: map iteration is randomised per run
	// of the process, not per call, so five calls in one test agree whatever
	// the order is. The order has to be named.
	want := []string{"Bat", "Orc", "Zombie"}
	for i := range 5 {
		res := sync(t, game.BehaviorSync{
			DB: db, Schema: s, Loader: loader, Registry: registry, TickDurationMs: 16,
		})
		if len(res.Problems) != len(want) {
			t.Fatalf("problems = %v, want one per unresolved binding", res.Problems)
		}
		for j, typeName := range want {
			if !strings.Contains(res.Problems[j], `"`+typeName+`"`) {
				t.Fatalf("run %d: problem %d is %q, want it to be about %s — entity types are reported in order",
					i, j, res.Problems[j], typeName)
			}
		}
	}
}
