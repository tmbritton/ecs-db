package game

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/world"
)

// BehaviorSync is what SyncBehaviors needs to honour the schema's bindings.
//
// A struct rather than seven parameters, for the reason renderer.NewGameParams
// is one: three of them are pointers to things with similar names and two are
// int64s that mean entirely different quantities.
type BehaviorSync struct {
	DB     *sql.DB
	Schema schema.DatabaseSchema
	Loader *agent.Loader
	// MapID scopes authored entities to the active map. Empty retains the
	// no-map/global behavior for programs without a configured map.
	MapID string
	// Registry is the actions and guards a machine's states may name. It has to
	// be the one the tick loop will use, and it has to already have the map's
	// grid registered into it: without pathfinding in it the loader will not
	// have accepted the goblin's file at all, so there would be nothing to
	// start. See SyncBehaviors for how that shapes where this runs.
	Registry *agent.Registry
	// Tick is the tick the machines are started at, which is the world's
	// current one rather than zero: an after-transition is scheduled at
	// tick + duration, and starting at zero on a world already at tick 40000
	// fires every one of them immediately.
	Tick int64
	// TickDurationMs converts an after-transition's duration into ticks.
	TickDurationMs int64
}

// BehaviorResult is what a sync did.
type BehaviorResult struct {
	// Started counts machines this call started.
	Started int
	// Running counts entities that already had their type's machine, and so
	// were left exactly as they were. A restart would reset a machine
	// mid-flight, which on a saved world means every goblin forgetting where it
	// was going every time the game is opened.
	Running int
	// Problems is one sentence per binding that could not be honoured, in
	// entity-type order. Not errors: a machine that will not start is a broken
	// monster, and the rest of the level should still run.
	Problems []string
	// Warnings are machines that were started and should not have been — a type
	// at validationLevel "warning" bound to a machine whose context seeds a
	// component the type does not declare. The schema said to allow it, so it is
	// not a refusal; it is also not nothing, because the component gets written.
	Warnings []string
}

// SyncBehaviors starts, for every entity whose type declares a "behavior", the
// machine that binding names — unless it is already running for that entity.
//
// This is what makes schema.EntityType.Behavior real. The field, and the
// architecture doc's assumption that it works, predate any code that read it:
// the goblin's machine was started by hand in cmd/, by name, for one entity.
//
// # Why this is a load-time reconcile and not part of creating the entity
//
// The obvious home is EntityService.CreateEntityInTx, where the machine would
// commit with the entity that needs it and every creator would honour the
// binding. **This is a choice and not an impossibility**, and it is worth
// saying which, because the shape of start-up makes it look forced: the goblin
// machine's entry actions are pickRandomTarget and computePath, and until
// builtins.RegisterPathfinding has been given the map's TileGrid the loader
// will not even accept the file — ValidateMachine refuses an action nobody
// registered. That grid comes from LoadMap, and LoadMap is what creates the
// entities, so it reads like a cycle. It is not one: only SyncTiles feeds the
// grid, and building it between SyncTiles and SyncSpawns would leave the
// registry complete before the first spawn existed.
//
// What decided it is what a reconcile buys that a create hook does not:
//
//   - A database that already exists works. An entity spawned before its type
//     declared a behaviour gets its machine on the next run rather than never.
//   - A process killed between the two is recoverable. Inside the create
//     transaction, a crash after commit and before the start would leave an
//     entity the spawns table calls done and no machine will ever be started for.
//   - Nothing below has to learn about machines. A create hook puts an agent
//     dependency into world or tilemap, and Forge and any script create entities
//     with no registry at all to honour a binding with.
//   - It is this epic's shape. SyncTiles, SyncSpawns, SyncBehaviors: read what
//     the files say, compare it against the world, act on the difference.
//
// # What it does not do
//
// **It never stops a machine**, and that covers two edits, not one. A type whose
// behavior changes from "goblin" to "orc" leaves its entities running both — two
// machines writing the same components every tick, and the last row SQLite
// returns wins. A type whose behavior is *removed* keeps running the machine it
// used to name, which is the edit somebody makes when they want a monster to
// stop wandering, and this reports nothing about it.
//
// Deliberate, and unhappily so: behavior_components is keyed
// (entity_id, machine_id) precisely so an entity may run machines nothing bound
// it to — a Burning component starts one — so from here a machine the schema
// does not name is indistinguishable from a machine something else started on
// purpose. Guessing wrong deletes live interpreter state. Stopping a machine is
// a schema-migration operation and wants asking for; the story records it.
//
// **It does not repair a machine whose states the file no longer has.** An
// entity whose current_states name a state deleted while the game was closed is
// left alone by the Running path, and LoadAgent then drops those ids and leaves
// it inert with nothing logged. agent.Reconciler exists for exactly that and is
// wired only to the hot-reload watcher, so it never sees an edit made between
// runs. This is the place that could — it holds every (entity, machine) pair and
// the definitions — and it needs agent to export the valid-state set that
// ReloadFile computes privately. Recorded in the story, not done here.
//
// **It reads entity types exactly.** ValidateEntityCreation looks a type up with
// s.EntityTypes[name] and refuses what it does not find, so entities.entity_type
// always holds a name spelled the way the schema spells it, and folding case
// here would invent a second matching rule for the same string.
func SyncBehaviors(ctx context.Context, p BehaviorSync) (BehaviorResult, error) {
	var res BehaviorResult
	if p.DB == nil || p.Loader == nil || p.Registry == nil {
		return res, fmt.Errorf("game: SyncBehaviors needs a database, a loader and a registry")
	}
	// Refused rather than defaulted. DurationToTicks reads a zero as one
	// millisecond per tick, so the goblin's `after: 1000` would be scheduled a
	// thousand ticks out instead of sixty — a machine that looks like it has
	// hung rather than one that fails.
	if p.TickDurationMs <= 0 {
		return res, fmt.Errorf("game: SyncBehaviors needs a tick duration; %d ms would schedule every after-transition wrong",
			p.TickDurationMs)
	}

	// Sorted, so two runs of the same broken schema report the same sentences
	// in the same order.
	types := make([]string, 0, len(p.Schema.EntityTypes))
	for name := range p.Schema.EntityTypes {
		types = append(types, name)
	}
	sort.Strings(types)

	for _, typeName := range types {
		machineID := p.Schema.EntityTypes[typeName].Behavior
		if machineID == "" {
			continue
		}
		def, ok := p.Loader.Get(machineID)
		if !ok {
			// Once per type, not once per entity: twenty goblins are one fault,
			// and it is in the schema, which has one line for it. Reported
			// before the entities are looked at, so a binding that cannot
			// resolve is heard about on a world that has not spawned yet —
			// which is when it is cheapest to fix.
			res.Problems = append(res.Problems, fmt.Sprintf(
				"entity type %q declares behavior %q, and no machine with that id is loaded",
				typeName, machineID))
			continue
		}

		// What the machine will write, checked against what the type allows,
		// before anything is started.
		//
		// StartAgent seeds its context by attaching components through the raw
		// storage port, which validates nothing: binding a machine with a
		// GoblinStats context to Player — a type with no optional components and
		// allowExtraComponents false — gave a Player a GoblinStats row, written
		// by the engine, with no message. Re-importing the map does not catch it
		// either, because that revalidates what the *object* declares and this
		// component came from the machine.
		//
		// Once per type rather than once per entity: the answer depends on the
		// type and the machine and on nothing about the entity.
		warnings, err := checkContext(&p.Schema, typeName, def)
		if err != nil {
			res.Problems = append(res.Problems, err.Error())
			continue
		}
		res.Warnings = append(res.Warnings, warnings...)

		entities, err := entitiesWithout(ctx, p.DB, typeName, machineID, p.MapID)
		if err != nil {
			return res, err
		}
		for _, e := range entities {
			if e.running {
				res.Running++
				continue
			}
			if err := startBehavior(ctx, p, def, e.id); err != nil {
				// One transaction per entity, which is createSpawn's reason
				// rather than SyncTiles': entry actions are arbitrary
				// author-supplied code, and one goblin whose computePath fails
				// must not stop the other nineteen from starting.
				res.Problems = append(res.Problems, fmt.Sprintf(
					"entity %d of type %q could not start behavior %q: %v",
					e.id, typeName, machineID, err))
				continue
			}
			res.Started++
		}
	}
	return res, nil
}

// checkContext reports whether a machine's context may be seeded onto entities
// of this type, and returns the warnings a lenient type produced.
//
// MachineDefinition.ContextManifest is context key → the component that declares
// a field of that name, which ValidateMachine fills in and which is exactly the
// set of components StartAgent may attach. Each is put to the same question
// AttachComponent asks — so a type at validationLevel "warning" warns and starts,
// and a strict one is refused, which is the graduated answer the schema already
// describes rather than a second rule invented here.
func checkContext(s *schema.DatabaseSchema, entityType string, def *agent.MachineDefinition) ([]string, error) {
	seen := map[string]bool{}
	names := make([]string, 0, len(def.ContextManifest))
	for _, comp := range def.ContextManifest {
		if comp == "" || seen[comp] {
			continue
		}
		seen[comp] = true
		names = append(names, comp)
	}
	// Sorted, so a machine seeding two forbidden components is refused for the
	// same one every run.
	sort.Strings(names)

	var warnings []string
	for _, comp := range names {
		// The verdict is ValidateAttachComponent's and the sentence is this
		// function's: the component is what the reader has to act on, and the
		// port's message is written for an attach nobody asked for here.
		vr := world.ValidateAttachComponent(s, entityType, comp, false)
		if len(vr.Errors) > 0 {
			return warnings, fmt.Errorf(
				"entity type %q declares behavior %q, whose context seeds component %q — which that type does not allow",
				entityType, def.ID, comp)
		}
		if len(vr.Warnings) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"entity type %q is bound to behavior %q, whose context seeds component %q — which that type does not allow, "+
					"and its validationLevel is %q, so it is written anyway",
				entityType, def.ID, comp, schema.ValidationWarning))
		}
	}
	return warnings, nil
}

// bound is an entity of a type that declares a behaviour, and whether that
// behaviour is already running for it.
type bound struct {
	id      int64
	running bool
}

// entitiesWithout is every entity of a type, in id order, each carrying whether
// the named machine is already among the ones it runs.
//
// Both halves in one query rather than a count and a loop: the answer has to be
// per entity, because behavior_components is keyed (entity_id, machine_id) and
// an entity may already run a machine that has nothing to do with its type.
func entitiesWithout(ctx context.Context, db *sql.DB, entityType, machineID, mapID string) ([]bound, error) {
	query := `SELECT entities.id,
		        EXISTS (SELECT 1 FROM behavior_components
		                 WHERE behavior_components.entity_id = entities.id
		                   AND behavior_components.machine_id = ?)
		   FROM entities
		  WHERE entities.entity_type = ?`
	args := []any{machineID, entityType}
	if mapID != "" {
		hasReferences, err := storage.HasTileReferences(ctx, db)
		if err != nil {
			return nil, err
		}
		query += " AND " + storage.ActiveMapEntityClause("entities.id", hasReferences)
		args = append(args, storage.ActiveMapEntityArgs(mapID, hasReferences)...)
	}
	query += " ORDER BY entities.id"
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("game: reading entities of type %q: %w", entityType, err)
	}
	defer func() { _ = rows.Close() }()

	var out []bound
	for rows.Next() {
		var e bound
		if err := rows.Scan(&e.id, &e.running); err != nil {
			return nil, fmt.Errorf("game: scanning an entity of type %q: %w", entityType, err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("game: reading entities of type %q: %w", entityType, err)
	}
	return out, nil
}

// startBehavior starts one machine for one entity, in one transaction.
//
// Both or neither. StartAgent attaches the components the machine's context
// seeds, runs the initial state's entry actions, schedules its after-events and
// writes the machine's state — and an entry action that fails halfway through
// that list has already written to the world. Committing what got done leaves
// an entity with a machine's side effects and no machine, which the next load
// would not notice because there is nothing missing to see.
func startBehavior(ctx context.Context, p BehaviorSync, def *agent.MachineDefinition, entityID int64) error {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Deferred, and ignored on the path where the commit below already ran: a
	// rollback of a committed transaction reports "already done" and there is
	// nothing to tell.
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	a := agent.NewAgent(def, entityID, "", p.TickDurationMs)
	reader := storage.NewTxWorldReader(tx)
	if p.MapID != "" {
		reader = storage.NewTxWorldReaderForMap(tx, p.MapID)
	}
	if err := agent.StartAgent(a, p.Registry, p.Tick,
		storage.NewTxWorldWriter(tx),
		reader,
		storage.NewMachineWriter(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
