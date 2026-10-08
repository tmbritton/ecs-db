# Review: ecs-db as a foundation for turn-based roguelikes

**Verdict:** A promising foundation for a moddable, simulation-heavy tactical game, but not yet a turn-based runtime. The schema, SQLite world, behavior interpreter, Forge, and occupant-aware movement/sight rules are substantial strengths. The next proof point should be a playable encounter, not another general-purpose subsystem. The authoritative demo scope is the [4e-inspired gameplay milestone](plan.md#gameplay-milestone-4e-inspired-point-and-click-adventure-board-game-demo).

This review describes the current implementation, not a decision to remove real-time games from the engine. The existing 20 Hz loop can remain a real-time mode; a turn-based game needs a simulation clock that is independent of render frames.

## What is already useful

- `schema.json` defines explicit entity/component contracts and interaction rules; `internal/tilemap/occupancy.go` checks movement and sight against live occupants and capabilities. `internal/tilemap/astar.go` uses the same `Space.CanEnter` rule as direct movement.
- `internal/agent/interpreter.go` already delivers named events to individual machines via `SendEvent`. State-machine behavior, component state, and SQLite transactions are a useful foundation for observable enemy decisions.
- Tiled map import, object spawns, and Forge authoring make it possible to build and iterate on levels without hard-coding every creature or wall. SQLite makes state inspectable and potentially straightforward to persist.

## Main mismatch: wall-clock ticks currently drive the world

`internal/renderer/game.go` samples held movement keys at 60 FPS and calls `Ticker.RunTick` every third frame. `internal/renderer/tick.go` advances `current_tick` at 20 Hz, drains input and due events, then sends `TICK` to **every** active `behavior_components` row in the current map on every tick. `internal/game/input_handler.go` reduces the accumulated held keys to one direction and writes the player's position directly; it is not a player command delivered to a machine. The shipped goblin machine reacts to `TICK` while wandering and uses an `after: 1000` delay when idle (`behaviors/goblin.json`).

This works for the current wandering demo. It means an enemy can move while the player is thinking, and a key held for longer can advance several world steps. Those are surprising defaults for a conventional turn-based roguelike.

**Recommendation:** Separate *presentation updates* from *simulation advances*. Keep `Update`/`Draw` running for input, animation, and UI, but advance encounter state only on an accepted action or an explicit scheduler event. Define one authoritative, headless resolver: an actor may take several actions within a turn, then **End Turn** passes initiative to the next actor; a round ends after the last actor. Decide explicitly whether a blocked attempted move spends an action. This gives mouse input, tests, and future alternate front ends the same rules.

## Events should wake entities; they need not all have an internal timer

The instinct to prefer targeted events is sound. `TICK` is presently a **broadcast polling convention**, not a requirement of the state-machine interpreter. `SendEvent` accepts any event name and payload (`internal/agent/context.go`); the scheduler chooses to broadcast `TICK`. The existing `event_queue` can target an `(entity_id, machine_id)` at a `target_tick`, and `after` transitions already schedule targeted future events (`internal/storage/machine_writer.go`). In practice that queue is currently used for `after` timers: `MachineWriter` exposes `ScheduleAfterEvent`, not a general game-event API. The tick drain reads `event_type` but does not pass the queue's `payload` column to `SendEvent`, and its due-event and active-machine queries specify no delivery order (`internal/renderer/tick.go`).

**Recommended contract for the point-and-click encounter:**

1. Convert mouse clicks into validated *actions* (`Move`, `Attack`, `Interact`, etc.) at the game boundary. Resolve each action and its consequences without automatically ending the actor's turn. Show remaining standard/move/minor actions and an explicit End Turn control; a click-to-move path cannot silently spend actions on future turns. A held mouse button must not issue repeated actions.
2. Route resulting *events* to explicit recipients, e.g. `ATTACKED` to the defender or `NOISE_HEARD` to affected listeners. Add a general targeted event-enqueue/delivery API, including payload; dispatch in a documented stable order. Events generated during a turn should have an explicit same-turn or next-turn rule and a bound on chains/recursion.
3. Give an actor `ACT` / `YOUR_TURN` **only when the initiative scheduler grants it a turn**, or let it react solely to targeted events if it is passive. A door can respond to `OPEN`; a goblin may take several budgeted actions on its turn; neither needs 20 `TICK`s per second. Off-turn reactions need explicit windows and limits. Do not infer initiative or action costs from frame rate.
4. Retain `after` for genuinely scheduled delays, but define delays in **actor turns or rounds** for this mode. Milliseconds-to-ticks conversion (`internal/agent/scheduler.go`) currently assumes a fixed tick duration; pausing for player input must not expire a delay. Animation and UI timers remain wall-clock presentation concerns.

There is no need to ban `TICK` outright. It can remain for a real-time mode, or an explicitly opted-in *turn pulse* for effects that should update once per world turn. It should not be automatically delivered to every machine just because a render frame elapsed.

## Replay and correctness claims need a narrower contract

`transitions` records machine transitions and action **names**, not every world mutation or the parameters/results of every action. Direct player writes from `PlayerInputHandler` do not create machine transition records. `pickRandomTarget` uses global `rand.Float64()` (`internal/agent/builtins/actions.go`), so re-running a transition from a checkpoint need not choose the same target. The architecture document's claims that the transition log alone provides complete replay/determinism (`docs/game-engine-arch.md`) should be treated as goals, not current guarantees.

**Recommendation:** Persist an ordered record of resolved player commands and any external events, plus a deterministic RNG seed/state or recorded random decisions. Specify the command/event order and tie-breakers (including queries that feed decisions), then replay from a consistent database checkpoint and verify world-state hashes on a short scenario. Treat transitions as a valuable *explanation trace* unless/until that end-to-end replay passes. If implementing checkpoint copies while SQLite is in WAL mode, use a consistent SQLite backup/snapshot rather than assuming a bare copy of the main DB file contains all committed state.

`RunTick` also logs and continues when `SendEvent` fails for a due event or an actor, then commits the enclosing tick (`internal/renderer/tick.go`). Because actions can have written through that transaction before returning an error, the turn-based resolver should define failure semantics: abort/roll back the whole turn, or isolate a delivery in a savepoint and report its failure. Do not silently consume the event and commit partial consequences.

## Suggested implementation sequence

1. **Headless encounter slice:** expose a resolver separate from Ebitengine. Establish initiative and standard/move/minor budgets; accept mouse-selected Move and End Turn; keep the world frozen while the player decides and prove that actions, actor turns and rounds advance independently. Define the blocked-move rule.
2. **Targeted dispatch:** make queued events first-class, preserve payload, order deliveries deterministically, and replace the goblin's unconditional `TICK` behavior in a turn-mode example with scheduler-granted `ACT` plus targeted reactions. Preserve real-time behavior where it is still desired.
3. **One playable encounter:** one enemy chooses actions on its turn; movement and pathfinding agree on occupancy; attacks, damage, death, and a passive world interaction all resolve through the same action/initiative pipeline.
4. **Persistence/replay proof:** save during an encounter, reload, and compare the next turns; replay recorded commands and external events with controlled randomness and compare resulting world state. Only then promise time travel on the strength of a replayable command/event record plus checkpoints.

**Acceptance test worth prioritizing:** in a small map, leave the player choosing an action for several seconds and confirm the active actor, round and enemy position do not change. Click to move (spending a move action but remaining on the player's turn), attack (spending a standard action), then End Turn and confirm the enemy takes its turn in initiative order. Save mid-turn, reload with the remaining action budget intact, and repeat the action sequence with identical outcomes. Keep animation rendering active throughout.
