package machines

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/project"
)

// Inspection is what the engine says about one machine as it stands right now.
//
// Both halves come out of a single ValidateMachine call, deliberately. "Is this
// machine valid" and "where does its context come from" are the same question
// asked twice — the engine only works the manifest out when the answer to the
// first is yes — and computing them separately is how a panel comes to show a
// manifest beside a list of reasons the machine cannot load.
type Inspection struct {
	// Definition is the value that was validated: a copy of the machine's
	// working value, carrying the manifest below when there is one.
	//
	// Handed back rather than fetched separately by the caller, because the
	// panel renders context *keys and values* from the definition and their
	// *components* from the manifest. Two calls means two acquisitions of the
	// session lock, and a key added between them renders with no component
	// against it — a blank cell in the one place this story is about not
	// leaving blank.
	Definition *agent.MachineDefinition
	// Errors is every reason the machine would be refused, not the first.
	Errors []agent.ValidationError
	// Manifest maps each context key to the component that declares the field
	// it seeds. It is meaningful only when Computed is true.
	Manifest map[string]string
	// Computed is whether the engine worked the manifest out at all.
	//
	// A field rather than len(Errors) == 0 re-derived at each call site,
	// because the distinction it carries is the one the panel exists to make:
	// an empty Manifest with Computed true is a machine that seeds nothing,
	// and an empty Manifest with Computed false is a machine whose seeds are
	// unknown. Anything that collapses the two makes the panel assert the
	// first when the truth is the second.
	Computed bool
}

// Inspect validates one machine's working value and reports what came of it.
func (s *Session) Inspect(path string) (Inspection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.files[path]
	if !ok {
		return Inspection{}, fmt.Errorf("machines: no open machine at %s", path)
	}
	// A copy, because agent.ValidateMachine *writes* ContextManifest onto
	// whatever it is handed. Inspecting runs on every render; handing it the
	// session's own working value would have a page render mutating the thing
	// being edited, which is the race Story 2's review found once already.
	probe, err := clone(f.Current)
	if err != nil {
		return Inspection{}, err
	}
	errs := agent.ValidateMachine(probe, project.BuildRegistry(s.cfg.HasMap), s.cfg.Schema())
	if len(errs) > 0 {
		return Inspection{Definition: probe, Errors: errs}, nil
	}
	return Inspection{Definition: probe, Manifest: probe.ContextManifest, Computed: true}, nil
}

// Invalid is how many validation errors each machine a Save would write has.
//
// The dirty set, minus the reformat-only ones — exactly what Save iterates,
// because that is exactly what could make it fail. A machine Save does not
// write cannot make it fail, and blocking the button on one would take the Save
// away for a file this footer does not touch.
//
// Both filters do work, and it is worth being exact about which does what.
// dirty() iterates s.order — the *resolved* set — while s.files is wider: it
// also holds machines reload() kept back because they had unsaved work and
// stopped resolving. So dropping dirty() would start counting a stranded
// machine's problems, which no Save this footer offers would write. It is also
// the cheap filter: without it every held machine would be cloned through the
// emitter and the parser and validated on every tick of a two-second stream.
//
// reformatOnly then removes what is dirty in layout only, which is what Save
// skips for the same reason: nobody edited it.
func (s *Session) Invalid() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()

	dirty, err := s.dirty()
	if err != nil {
		// A machine that will not serialise cannot be saved either. Reporting
		// nothing would leave the footer offering a Save that fails; the
		// per-machine answer below is unavailable, so say so at the top.
		return map[string]int{"": 1}
	}
	// Both are loop invariants, and Schema() is not cheap: it takes the schema
	// session's lock and deep-copies the whole schema, so inside the loop it
	// was one clone and one lock acquisition per dirty machine, per tick, for
	// the same value.
	registry := project.BuildRegistry(s.cfg.HasMap)
	current := s.cfg.Schema()
	out := map[string]int{}
	for _, path := range dirty {
		if s.reformatOnly(path) {
			continue
		}
		f, ok := s.files[path]
		if !ok {
			continue
		}
		// A copy, for the reason Inspect makes one: ValidateMachine *writes*
		// ContextManifest onto whatever it is handed, and this runs on a render.
		probe, err := clone(f.Current)
		if err != nil {
			out[path] = 1
			continue
		}
		if n := len(agent.ValidateMachine(probe, registry, current)); n > 0 {
			out[path] = n
		}
	}
	return out
}

// Held is every path the session holds an open file for, resolved or not.
//
// Paths is the resolved set and is what rename and delete work against; this is
// wider by exactly the machines reload() kept back because they had unsaved
// work and stopped resolving. Discard needs it: without it that work is kept
// and unreachable, which is a promise to preserve something and no way to
// collect it.
func (s *Session) Held() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := append([]string(nil), s.order...)
	return append(out, s.strandedPaths()...)
}

// Stranded is every machine held with unsaved work that no longer resolves.
func (s *Session) Stranded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.strandedPaths()
}

// strandedPaths is the held set minus the resolved set. Caller holds s.mu.
//
// Derived from the two sets rather than recorded when reload() keeps a file:
// a flag would have to be cleared by everything that can put a machine back in
// the resolved set, and the one that got missed would leave a machine reported
// as stranded while it sat in the list.
func (s *Session) strandedPaths() []string {
	resolved := make(map[string]bool, len(s.order))
	for _, path := range s.order {
		resolved[path] = true
	}
	var out []string
	for _, path := range sortedKeys(s.files) {
		if !resolved[path] {
			out = append(out, path)
		}
	}
	return out
}

// FreeID proposes a machine id nothing has claimed.
//
// It proposes only: Create still refuses a real collision, so the guard stays
// in one place. What this stops is the create button that works once — the
// skeleton posted a fixed id, so a second press was refused until the first
// machine had been renamed.
//
// Both sources of a claim are consulted. A file on disk owns its id even if it
// does not load (Create reads the files for exactly that reason), and an open
// machine's *working* id is claimed too — an unsaved rename would collide the
// moment it was saved.
func (s *Session) FreeID(base string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	// The whole set once, rather than one lookup per candidate. This runs on
	// every AGENTS render, including every tick of every open page's stream,
	// and each lookup reads and parses every file in every behaviours
	// directory — so asking per candidate turns a proposal into N directory
	// walks a second.
	claimed := s.claimedIDs()
	if !claimed[base] {
		return base
	}
	for n := 2; n < 1000; n++ {
		candidate := base + strconv.Itoa(n)
		if !claimed[candidate] {
			return candidate
		}
	}
	// Nothing free in a thousand tries is not a state worth encoding a third
	// answer for: hand back the base and let Create give its own refusal.
	return base
}

// claimedIDs is every id something already owns. Caller holds s.mu.
func (s *Session) claimedIDs() map[string]bool {
	out := map[string]bool{}
	for _, mod := range s.modsThatCanHold() {
		for id := range declaredIn(mod.Behaviors) {
			out[id] = true
		}
	}
	// An unsaved rename has claimed its new name as surely as a file has: it
	// would collide with anything created under it the moment it was saved.
	for _, f := range s.files {
		if f.Current != nil {
			out[f.Current.ID] = true
		}
	}
	return out
}

// sortedKeys keeps every listing this file produces in a stable order. A map
// range would make the stranded list — and the problem panel built from it —
// re-shuffle on every render, which patches the page for no reason.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
