// Package project resolves a Forge project: a game.toml, the schema it names,
// and the mods that contribute behavior machines.
//
// It is a domain package — a path in, a value out, no HTTP and no templates.
// Every load-order decision is delegated to agent.Loader rather than
// reimplemented here: two implementations of "which mod's file wins" is exactly
// the bug that would make Forge show one machine while the game runs another.
package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

// Mod is one entry from game.toml's [[mods]], in load order.
type Mod struct {
	Name      string
	Behaviors string // absolute path to the behaviors directory
}

// Machine is one behavior machine as resolved for this project.
type Machine struct {
	ID         string
	Path       string // the file it was actually loaded from
	Mod        string // the mod that contributed the winning file
	Overrides  bool   // shadows an earlier mod's machine of the same ID
	Definition *agent.MachineDefinition
}

// Problem is something wrong with the project that does not prevent opening it.
// Forge's job includes showing you the broken file so you can fix it; refusing
// to open the project would make that impossible.
type Problem struct {
	Path string
	Err  error
}

func (p Problem) String() string { return p.Path + ": " + p.Err.Error() }

// Project is everything Forge needs to know about what it is editing. It is a
// value, constructed from a path and injected where needed — there is no
// package-level state to reset between tests or projects.
type Project struct {
	ConfigPath string
	SchemaPath string
	DBPath     string
	Schema     schema.DatabaseSchema
	Mods       []Mod
	Machines   []Machine // sorted by ID, so a list renders in a stable order
	Problems   []Problem
}

// Open resolves the project rooted at the given game.toml.
//
// It fails only on a config or schema it cannot load — those describe
// everything else, so there is nothing to show without them. Every other
// difficulty is a Problem.
func Open(configPath string) (*Project, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("project: %w", err)
	}

	// Paths in game.toml are relative to the file that names them, not to the
	// process's working directory — Forge may be started from anywhere.
	root := filepath.Dir(configPath)
	abs := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Clean(filepath.Join(root, p))
	}

	p := &Project{
		ConfigPath: configPath,
		SchemaPath: abs(cfg.Schema.Path),
		DBPath:     abs(cfg.Database.Path),
	}

	raw, err := os.ReadFile(p.SchemaPath)
	if err != nil {
		return nil, fmt.Errorf("project: reading %s: %w", filepath.Base(p.SchemaPath), err)
	}
	loaded, err := schema.LoadSchema(raw)
	if err != nil {
		return nil, fmt.Errorf("project: loading %s: %w", filepath.Base(p.SchemaPath), err)
	}
	if err := schema.ValidateSchema(loaded); err != nil {
		return nil, fmt.Errorf("project: validating %s: %w", filepath.Base(p.SchemaPath), err)
	}
	p.Schema = loaded

	for _, m := range cfg.Mods {
		mod := Mod{Name: m.Name}
		// Left empty rather than resolved: abs("") is the project root, and a
		// mod that declares no behaviors directory has none, not that one.
		if m.Behaviors != "" {
			mod.Behaviors = abs(m.Behaviors)
		}
		p.Mods = append(p.Mods, mod)
	}

	p.Machines, p.Problems = loadMachines(p.Mods, loaded, cfg.Map.Path != "")
	return p, nil
}

// loadMachines scans each mod in order through agent.Loader, which owns the
// precedence rule. Override detection is derived from what the loader actually
// did — a source that changed after scanning a later mod was overridden — so
// there is no second copy of the rule to disagree with the first.
func loadMachines(mods []Mod, s schema.DatabaseSchema, hasMap bool) ([]Machine, []Problem) {
	loader := agent.NewLoader(buildRegistry(hasMap), s)

	var problems []Problem
	overridden := map[string]bool{}
	scanned := map[string]bool{} // behaviors directories already walked

	for _, mod := range mods {
		// A mod that contributes no behaviors is a first-class case — an
		// assets-only mod, which run.go also skips. Without this, abs("")
		// resolves to the project root and every stray .json in it, schema.json
		// included, gets parsed as a behavior machine.
		if mod.Behaviors == "" {
			continue
		}
		if scanned[mod.Behaviors] {
			// Two mods pointing at one directory would report every file
			// error twice and mark every machine as overriding itself, since
			// only the mod-name half of the source string changed.
			continue
		}
		scanned[mod.Behaviors] = true

		if _, err := os.Stat(mod.Behaviors); err != nil {
			// Reported, not fatal: a mod whose behaviors directory does not
			// exist yet is a normal state for a project under construction, and
			// the rest of it still loads.
			problems = append(problems, Problem{Path: mod.Behaviors, Err: err})
			continue
		}

		before := loader.Sources()
		if _, err := loader.ScanDir(mod.Behaviors, mod.Name); err != nil {
			// ScanDir loads what it can and returns the failures aggregated
			// into one error naming the directory. Forge needs to point at the
			// file, so the failures are re-attributed per file — on the error
			// path only, and without touching ScanDir's precedence rule, which
			// stays the single implementation of "which mod's file wins".
			problems = append(problems, attributeFailures(loader, mod, err)...)
		}
		// Two files in one directory declaring the same machine id is an
		// authoring mistake — one of them silently never loads. It cannot be
		// seen from the snapshots either side of the scan, because both files
		// are read inside a single ScanDir call, so it is detected directly.
		problems = append(problems, duplicateIDs(mod)...)

		for id, src := range loader.Sources() {
			if prev, existed := before[id]; existed && prev != src {
				overridden[id] = true
			}
		}
	}

	sources := loader.Sources()
	var machines []Machine
	for id, def := range loader.List() {
		src, ok := sources[id]
		if !ok {
			// Loaded but with no recorded source: only reachable if a file
			// failed during ScanDir and succeeded on the attribution retry.
			// A machine with no path is not something a panel can render.
			continue
		}
		// The owning mod is the first half of the source string the loader
		// recorded. Tracking it separately is what made every machine appear to
		// come from whichever mod was scanned last.
		modName, path := splitSource(src)
		machines = append(machines, Machine{
			ID:         id,
			Path:       path,
			Mod:        modName,
			Overrides:  overridden[id],
			Definition: def,
		})
	}
	sort.Slice(machines, func(i, j int) bool { return machines[i].ID < machines[j].ID })
	return machines, problems
}

// duplicateIDs reports machine ids declared by more than one file in a mod.
//
// Only the id is read: a file that fails to parse properly is already reported
// by the scan, and re-validating here would double the work and the noise.
func duplicateIDs(mod Mod) []Problem {
	entries, err := os.ReadDir(mod.Behaviors)
	if err != nil {
		return nil // already reported by the caller's os.Stat
	}
	byID := map[string][]string{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(mod.Behaviors, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var head struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &head); err != nil || head.ID == "" {
			continue
		}
		byID[head.ID] = append(byID[head.ID], e.Name())
	}

	var problems []Problem
	for id, files := range byID {
		if len(files) < 2 {
			continue
		}
		sort.Strings(files)
		problems = append(problems, Problem{
			Path: mod.Behaviors,
			Err: fmt.Errorf("machine %q is declared by %d files in mod %q (%s); only one is loaded",
				id, len(files), mod.Name, strings.Join(files, ", ")),
		})
	}
	sort.Slice(problems, func(i, j int) bool { return problems[i].Err.Error() < problems[j].Err.Error() })
	return problems
}

// buildRegistry mirrors what the running engine registers for this project.
//
// The mirroring is the point, and it is why hasMap is threaded down here.
// cmd/ecs-db/run.go registers the pathfinding and line-of-sight builtins only
// when a map is configured, because their closures capture a *tilemap.TileGrid
// that does not exist otherwise. A Forge that registered them unconditionally
// would report a machine using computePath as valid, and the engine would then
// refuse to load it — the same "editor and game disagree" failure this package
// is built to avoid, in the opposite direction.
//
// The grid here is a throwaway: Forge never ticks a machine and only wants the
// metadata. That the engine's action vocabulary depends on map configuration at
// all is arguably an engine defect, but it is the engine's behaviour today and
// Forge's job is to describe it accurately, not to improve on it silently.
func buildRegistry(hasMap bool) *agent.Registry {
	registry := builtins.NewRegistry()
	if hasMap {
		grid := tilemap.NewTileGrid(0, 0)
		builtins.RegisterPathfinding(registry, grid)
		builtins.RegisterLineOfSight(registry, grid)
	}
	return registry
}

// attributeFailures re-tries each file in the directory individually, so a
// failure can be reported against the file that caused it rather than against
// the whole mod. Retrying a file that already loaded is harmless: LoadMachine
// re-parses it to the same definition.
func attributeFailures(loader *agent.Loader, mod Mod, scanErr error) []Problem {
	entries, err := os.ReadDir(mod.Behaviors)
	if err != nil {
		return []Problem{{Path: mod.Behaviors, Err: err}}
	}
	var problems []Problem
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(mod.Behaviors, e.Name())
		if _, err := loader.LoadMachine(path); err != nil {
			problems = append(problems, Problem{Path: path, Err: err})
		}
	}
	if len(problems) == 0 {
		// ScanDir failed but no individual file does. Report what ScanDir
		// actually said rather than inventing a replacement for it.
		return []Problem{{Path: mod.Behaviors, Err: scanErr}}
	}
	return problems
}

// splitSource unpacks the loader's "modName:filepath" source string. The path
// may itself contain a colon, so split on the first one only.
func splitSource(src string) (mod, path string) {
	for i := range len(src) {
		if src[i] == ':' {
			return src[:i], src[i+1:]
		}
	}
	return "", src
}
