package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture builds a project on disk: a game.toml, a schema.json, and a
// behaviors directory per mod. Mods are given in load order.
type fixture struct {
	schema string            // schema.json body; a default is used when empty
	mods   []string          // mod names, in load order
	files  map[string]string // "mod/name.json" → machine body
	omit   []string          // mod names whose directory is deliberately absent
	// assetsOnly names mods declaring no behaviors directory at all — an
	// artpack, say. They must contribute nothing rather than resolving to the
	// project root.
	assetsOnly []string
	extra      map[string]string // stray files at the project root
	mapPath    string            // [map].path, relative to the config file
}

const validSchema = `{
  "schemaVersion": 3,
  "components": {
    "Position": {"type": "object", "properties": {"x": {"type": "number"}}}
  },
  "entityTypes": {
    "Thing": {"requiredComponents": ["Position"]}
  }
}`

func machine(id string) string {
	return `{"id":"` + id + `","initial":"idle","states":{"idle":{}}}`
}

func (f fixture) write(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	body := f.schema
	if body == "" {
		body = validSchema
	}
	if err := os.WriteFile(filepath.Join(dir, "schema.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("writing schema: %v", err)
	}

	omitted := map[string]bool{}
	for _, m := range f.omit {
		omitted[m] = true
	}
	assets := map[string]bool{}
	for _, m := range f.assetsOnly {
		assets[m] = true
	}

	for name, content := range f.extra {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	var toml strings.Builder
	toml.WriteString("[database]\npath = \"./ecs.db\"\n\n[schema]\npath = \"./schema.json\"\n")
	if f.mapPath != "" {
		toml.WriteString("\n[map]\npath = \"" + f.mapPath + "\"\n")
	}
	for _, mod := range f.mods {
		toml.WriteString("\n[[mods]]\nname = \"" + mod + "\"\n")
		if assets[mod] {
			toml.WriteString("assets = \"./" + mod + "-art/\"\n")
			continue
		}
		toml.WriteString("behaviors = \"./" + mod + "/\"\n")
		if !omitted[mod] {
			if err := os.MkdirAll(filepath.Join(dir, mod), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", mod, err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "game.toml"), []byte(toml.String()), 0o600); err != nil {
		t.Fatalf("writing game.toml: %v", err)
	}

	for rel, content := range f.files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
	}
	return filepath.Join(dir, "game.toml")
}

func TestOpen_SingleMod(t *testing.T) {
	cfg := fixture{
		mods:  []string{"core"},
		files: map[string]string{"core/wander.json": machine("wander")},
	}.write(t)

	p, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if p.Schema.SchemaVersion != 3 {
		t.Errorf("SchemaVersion = %d, want 3", p.Schema.SchemaVersion)
	}
	if len(p.Mods) != 1 || p.Mods[0].Name != "core" {
		t.Errorf("Mods = %+v, want one named core", p.Mods)
	}
	if len(p.Machines) != 1 {
		t.Fatalf("Machines = %+v, want 1", p.Machines)
	}
	m := p.Machines[0]
	if m.ID != "wander" || m.Mod != "core" || m.Overrides {
		t.Errorf("machine = %+v, want wander from core with Overrides false", m)
	}
	if !strings.HasSuffix(m.Path, filepath.Join("core", "wander.json")) {
		t.Errorf("Path = %q, does not name the file it came from", m.Path)
	}
	if len(p.Problems) != 0 {
		t.Errorf("Problems = %+v, want none", p.Problems)
	}
}

// Load order is the engine's: later mods win. A project model that disagreed
// would show one machine in the editor while the game ran another.
func TestOpen_LaterModOverridesEarlier(t *testing.T) {
	cfg := fixture{
		mods: []string{"core", "expansion"},
		files: map[string]string{
			"core/wander.json":      machine("wander"),
			"core/idle.json":        machine("idle"),
			"expansion/wander.json": machine("wander"),
		},
	}.write(t)

	p, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(p.Machines) != 2 {
		t.Fatalf("Machines = %+v, want 2 (wander and idle)", p.Machines)
	}

	byID := map[string]Machine{}
	for _, m := range p.Machines {
		byID[m.ID] = m
	}

	w := byID["wander"]
	if w.Mod != "expansion" {
		t.Errorf("wander came from %q, want expansion — the later mod must win", w.Mod)
	}
	if !w.Overrides {
		t.Error("wander should be marked as overriding core's copy")
	}
	if !strings.Contains(w.Path, "expansion") {
		t.Errorf("wander Path = %q, want the expansion file", w.Path)
	}

	if byID["idle"].Overrides {
		t.Error("idle is not overridden by anything and must not be flagged")
	}
}

// A project with an empty or absent mod directory is normal, not broken.
func TestOpen_MissingModDirectoryIsNotFatal(t *testing.T) {
	cfg := fixture{
		mods:  []string{"core", "absent"},
		omit:  []string{"absent"},
		files: map[string]string{"core/wander.json": machine("wander")},
	}.write(t)

	p, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v — a missing mod directory must not fail the load", err)
	}
	if len(p.Machines) != 1 {
		t.Errorf("Machines = %+v, want core's one machine", p.Machines)
	}
	if len(p.Problems) != 1 {
		t.Fatalf("Problems = %+v, want one naming the absent directory", p.Problems)
	}
	if !strings.Contains(p.Problems[0].Path, "absent") {
		t.Errorf("Problem does not name the absent mod: %+v", p.Problems[0])
	}
}

// Forge's job includes showing you the broken file. Refusing to open the
// project would make that impossible.
func TestOpen_BrokenMachineIsAProblemNotAFailure(t *testing.T) {
	cfg := fixture{
		mods: []string{"core"},
		files: map[string]string{
			"core/good.json": machine("good"),
			// An unregistered action. Note that an `initial` naming a state
			// that does not exist is NOT rejected by ValidateMachine — see the
			// finding recorded in docs/stories/epic-11/README.md — so it would
			// not serve as a "broken machine" fixture.
			"core/broken.json": `{"id":"broken","initial":"idle","states":{"idle":{"entry":"noSuchAction"}}}`,
		},
	}.write(t)

	p, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v — a broken machine must not fail the load", err)
	}
	if len(p.Machines) != 1 || p.Machines[0].ID != "good" {
		t.Errorf("Machines = %+v, want only the good one", p.Machines)
	}
	if len(p.Problems) != 1 {
		t.Fatalf("Problems = %+v, want one", p.Problems)
	}
	if !strings.Contains(p.Problems[0].Path, "broken.json") {
		t.Errorf("Problem does not name the file: %+v", p.Problems[0])
	}
	if p.Problems[0].Err == nil {
		t.Error("Problem carries no error to show the user")
	}
}

// The schema is the one thing that must load: everything else is described in
// terms of it.
func TestOpen_BrokenSchemaIsFatal(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{name: "not json", schema: "{ not json"},
		{name: "not a schema", schema: `{"nope": true}`},
		// Rejected by LoadSchema before validation is reached.
		{name: "version below one", schema: `{"schemaVersion": 0, "components": {}, "entityTypes": {}}`},
		// Loads cleanly and fails ValidateSchema — without this row that branch
		// is never executed, and removing the ValidateSchema call entirely
		// leaves every other row green.
		{name: "loads but does not validate", schema: `{
			"schemaVersion": 1,
			"components": {"Position": {"type": "object", "properties": {"x": {"type": "number"}}}},
			"entityTypes": {"Thing": {"requiredComponents": ["Nope"]}}
		}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := fixture{schema: tt.schema, mods: []string{"core"}}.write(t)
			p, err := Open(cfg)
			if err == nil {
				t.Fatalf("Open succeeded with a broken schema: %+v", p)
			}
			if !strings.Contains(err.Error(), "schema.json") {
				t.Errorf("error does not name the file: %v", err)
			}
		})
	}
}

func TestOpen_MissingConfigIsFatal(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Error("Open succeeded with no config file")
	}
}

// Machines render in a list, so their order must not come from a map.
func TestOpen_MachinesAreSortedByID(t *testing.T) {
	cfg := fixture{
		mods: []string{"core"},
		files: map[string]string{
			// Four, not three: with three, a no-op sort comparator comes out
			// in the right order often enough to pass a single run.
			"core/zebra.json": machine("zebra"),
			"core/apple.json": machine("apple"),
			"core/mango.json": machine("mango"),
			"core/kiwi.json":  machine("kiwi"),
		},
	}.write(t)

	p, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var ids []string
	for _, m := range p.Machines {
		ids = append(ids, m.ID)
	}
	want := []string{"apple", "kiwi", "mango", "zebra"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("machine order = %v, want %v", ids, want)
	}
}

// Every machine must report the mod that actually contributed it. Tracking
// ownership separately from the loader's own record made the last mod scanned
// appear to own every machine in the project — so the panel would have shown
// "idle — from artpack" beside a file in core/.
func TestOpen_EachMachineReportsItsOwnMod(t *testing.T) {
	cfg := fixture{
		mods: []string{"core", "middle", "last"},
		files: map[string]string{
			"core/idle.json":  machine("idle"),
			"middle/mid.json": machine("mid"),
			"last/final.json": machine("final"),
		},
	}.write(t)

	p, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	want := map[string]string{"idle": "core", "mid": "middle", "final": "last"}
	for _, m := range p.Machines {
		if want[m.ID] != m.Mod {
			t.Errorf("%s.Mod = %q, want %q", m.ID, m.Mod, want[m.ID])
		}
		// Mod and Path must agree — that they disagreed is what made the bug
		// visible, and either alone would have looked plausible.
		if !strings.Contains(m.Path, string(filepath.Separator)+m.Mod+string(filepath.Separator)) {
			t.Errorf("%s: Mod %q does not match Path %q", m.ID, m.Mod, m.Path)
		}
	}
}

// A mod that declares no behaviors directory contributes no machines. Resolving
// its empty path produced the project root, so every stray .json there —
// schema.json included — was parsed as a behavior machine.
func TestOpen_AssetsOnlyModContributesNothing(t *testing.T) {
	cfg := fixture{
		mods:       []string{"core", "artpack"},
		assetsOnly: []string{"artpack"},
		files:      map[string]string{"core/idle.json": machine("idle")},
		extra:      map[string]string{"tileset.json": `{"not":"a machine"}`},
	}.write(t)

	p, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if len(p.Machines) != 1 || p.Machines[0].ID != "idle" {
		t.Fatalf("Machines = %+v, want only core's idle", p.Machines)
	}
	if p.Machines[0].Mod != "core" {
		t.Errorf("idle.Mod = %q, want core", p.Machines[0].Mod)
	}
	for _, m := range p.Machines {
		if m.ID == "" {
			t.Errorf("a nameless machine was loaded from a stray file: %+v", m)
		}
	}
	if len(p.Problems) != 0 {
		t.Errorf("Problems = %+v; an assets-only mod is not a problem", p.Problems)
	}
}

// Forge must offer the same actions the engine will accept. run.go registers
// the pathfinding and line-of-sight builtins only when a map is configured, so
// a Forge that always registered them would call a machine valid that the
// engine then refuses to load.
func TestOpen_RegistryMirrorsTheEngine(t *testing.T) {
	usesPathfinding := `{"id":"walker","initial":"idle","states":{"idle":{"entry":"computePath"}}}`

	t.Run("no map configured: the action is not available", func(t *testing.T) {
		cfg := fixture{
			mods:  []string{"core"},
			files: map[string]string{"core/walker.json": usesPathfinding},
		}.write(t)

		p, err := Open(cfg)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if len(p.Problems) != 1 {
			t.Fatalf("Problems = %+v, want the machine rejected as the engine would reject it", p.Problems)
		}
		if !strings.Contains(p.Problems[0].Err.Error(), "computePath") {
			t.Errorf("problem does not name the action: %v", p.Problems[0].Err)
		}
	})

	t.Run("map configured: the action is available", func(t *testing.T) {
		cfgPath := fixture{
			mods:  []string{"core"},
			files: map[string]string{"core/walker.json": usesPathfinding},
		}.write(t)
		appendMapSection(t, cfgPath)

		p, err := Open(cfgPath)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if len(p.Problems) != 0 {
			t.Errorf("Problems = %+v, want none once a map is configured", p.Problems)
		}
		if len(p.Machines) != 1 {
			t.Errorf("Machines = %+v, want the walker", p.Machines)
		}
	})
}

func appendMapSection(t *testing.T, cfgPath string) {
	t.Helper()
	f, err := os.OpenFile(cfgPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("opening config: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString("\n[map]\npath = \"./level.toml\"\n"); err != nil {
		t.Fatalf("appending [map]: %v", err)
	}
}

// Two files in one mod declaring the same machine id is an authoring mistake:
// one of them silently never loads. The engine prints a line about it; Forge
// showed nothing at all.
func TestOpen_DuplicateMachineIDWithinOneMod(t *testing.T) {
	cfg := fixture{
		mods: []string{"core"},
		files: map[string]string{
			"core/aaa.json": machine("dup"),
			"core/zzz.json": machine("dup"),
		},
	}.write(t)

	p, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(p.Problems) != 1 {
		t.Fatalf("Problems = %+v, want one naming the duplicate", p.Problems)
	}
	if !strings.Contains(p.Problems[0].Err.Error(), "dup") {
		t.Errorf("problem does not name the machine: %v", p.Problems[0].Err)
	}
	// It is not an override — no later mod shadowed anything.
	for _, m := range p.Machines {
		if m.Overrides {
			t.Errorf("%s marked as overriding, but only one mod is involved", m.ID)
		}
	}
}

// Two mods pointing at the same directory reported every file error twice and
// marked every machine as overriding itself, because only the mod-name half of
// the loader's source string changed.
func TestOpen_TwoModsSharingOneBehaviorsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "shared"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schema.json"), []byte(validSchema), 0o600); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shared", "idle.json"), []byte(machine("idle")), 0o600); err != nil {
		t.Fatalf("machine: %v", err)
	}
	toml := `[database]
path = "./ecs.db"

[schema]
path = "./schema.json"

[[mods]]
name = "one"
behaviors = "./shared/"

[[mods]]
name = "two"
behaviors = "./shared/"
`
	cfg := filepath.Join(dir, "game.toml")
	if err := os.WriteFile(cfg, []byte(toml), 0o600); err != nil {
		t.Fatalf("config: %v", err)
	}

	p, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(p.Machines) != 1 {
		t.Fatalf("Machines = %+v, want one", p.Machines)
	}
	if p.Machines[0].Overrides {
		t.Error("the machine is marked as overriding itself")
	}
	if len(p.Problems) != 0 {
		t.Errorf("Problems = %+v, want none", p.Problems)
	}
}

// The map path is what MAP mode opens and what decides whether the pathfinding
// builtins exist. It was read for the second question and dropped, so Forge
// knew a project had a map and not which one.
func TestOpen_CarriesTheConfiguredMapPath(t *testing.T) {
	cfg := fixture{mapPath: "./maps/level1.tmx"}.write(t)

	p, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := filepath.Join(filepath.Dir(cfg), "maps", "level1.tmx")
	if p.MapPath != want {
		t.Errorf("MapPath = %q, want %q", p.MapPath, want)
	}
}

// Resolved by config.Load against the file that declared it, once — the same
// rule every other path follows, so the engine and Forge cannot disagree about
// which file a config means.
func TestOpen_AProjectWithNoMapHasNoMapPath(t *testing.T) {
	p, err := Open(fixture{}.write(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if p.MapPath != "" {
		t.Errorf("MapPath = %q, want empty", p.MapPath)
	}
}
