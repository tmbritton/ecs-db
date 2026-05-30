# Plan: ECS-in-SQLite game engine with declarative behaviors

Implementation plan derived from the architecture doc. Build the schema system as a complete unit first, then the agents runtime, then a working monolithic game (interpreter + Ebitengine renderer in one binary), then the debugger as the one natural process boundary, then time-travel, effects, and polish. Each epic will be refined into concrete tasks in a follow-up pass.

---

## Epic 1: Schema-driven data foundation

**Refined into stories:** See [`docs/stories/epic-1/`](docs/stories/epic-1/).

Establish `schema.json` as the declarative source of truth for components and entity types. Generate the SQLite schema from it. Validate strictly by default so modder mistakes are loud. Until this works, nothing else can.

- [x] **Define schema.json document shape** — Lock the format before anything reads it.
  - Component declarations with typed properties (number, integer, string, object, array, entity-ref)
  - Entity type templates: `requiredComponents`, `optionalComponents`, `allowExtraComponents`, `validationLevel`
  - Top-level `schemaVersion` integer
  - JSON Schema (or equivalent) for self-validation of the file itself

- [x] **schema.json loader & validator** — Refuse to start on malformed input with clear errors.
  - Reject malformed JSON with line/column
  - Validate every `entityTypes.*.*Components` reference points to a declared component
  - Validate component property types are supported by the SQL generator
  - Reject duplicate component or entity-type names

- [x] **SQL DDL generation from schema.json** — One source of truth, two representations.
  - Fixed tables: `meta`, `world`, `entities`, `event_queue`, `input_events`, `transitions`
  - One `comp_*` table per declared component with typed columns
  - Pragmas at init: WAL, `synchronous=NORMAL`, `busy_timeout=5000`, `foreign_keys=ON`
  - `ON DELETE CASCADE` from `entities(id)` to all `comp_*.entity_id`

- [x] **Entity creation with type validation** — Enforce contracts at attachment time, not query time.
  - Required components present at creation
  - No disallowed components when `allowExtraComponents=false`
  - Honor `validationLevel`: `strict` refuses, `warning` logs and proceeds
  - Coverage: `world` 98.5%, `storage` 81.8%, `schema` 93.1%

- [x] **Component attach/detach with type validation** — Enable entities to gain and lose components post-creation.
  - `EntityService.AttachComponent` with schema validation, duplicate prevention, and transactional insert
  - `EntityService.DetachComponent` with required-component guard, transactional delete
  - `EntityStore.GetEntityType` and `EntityStore.HasComponent` lookup methods
  - Validation honors `validationLevel` on attach; detach of required components always errors
  - Coverage: `world` 95.0%, `storage` 80.1%

- [x] **Component attach/detach with type validation** — Same rules apply post-creation.
  - Reject attaching unknown components
  - Reject attaching disallowed components on strict types
  - Detaching a required component is an error

- [x] **world.sqlite bootstrap** — Cleanly create or open the database.
  - Create on first run, write `schema_version` to `meta`
  - On open, compare `meta.schema_version` to `schema.json` — returns `ErrSchemaVersionMismatch` on mismatch (Epic 2 will trigger migrations)
  - Record build info (`build_time`, optional `schema_hash`) in `meta` for debugging
  - Coverage: `storage` 80.5%

---

## Epic 2: Schema versioning & migrations

Automatic migrations driven purely by `schema.json` changes. The user edits the schema, bumps `schemaVersion`, and the engine brings the database up to date on startup. No migration files, no SQL authoring — the engine computes the diff, generates DDL, and applies it transactionally.

Refined into stories: See [`docs/stories/epic-2/`](docs/stories/epic-2/).

- [x] **Database introspection** — Reconstruct the current database schema from SQLite.
  - Discover all `comp_*` tables via `sqlite_master`
  - Recover column names and SQL types via `PRAGMA table_info`
  - Read `schema_version` from `meta`
  - Entity types are NOT introspectable (metadata-only in `schema.json`)

- [x] **Schema diff computation** — Compare the as-built database schema against `schema.json`.
  - Detect new/removed components, added/removed properties, SQL type changes
  - Detect entity type changes (new, removed, requirement changes — metadata only)
  - Produce a deterministic, safely-ordered list of changes

- [x] **DDL generation from diff** — Translate each change type into SQL.
  - New components → `CREATE TABLE` (reuse existing `componentTableSQL`)
  - New properties → `ALTER TABLE ADD COLUMN`
  - Removed properties / type changes → table-rebuild sequence (SQLite lacks `DROP COLUMN`)
  - Removed components → `DROP TABLE`
  - Destructive changes flagged for configurable warning/confirmation

- [x] **Auto-migration runner** — Integrate into `NewSQLiteStore` startup flow.
  - On version mismatch: introspect → diff → generate DDL → execute in one transaction
  - Update `meta.schema_version` and `meta.build_time` on success
  - Structured error reporting on failure (which change, which statement)
  - `MigrationPolicy` (`auto` / `confirm`) controls destructive change behavior

- [x] **Smoke test: round-trip a migration** — Prove the full pipeline works.
  - Create DB at version 1 with an entity, add a component in version 2
  - Reopen with new schema, verify table created, original data intact, `meta` updated
  - Second test: add a property to an existing component, verify column added

- [x] **Backup before migrate** — Cheap insurance.
  - Copy `world.sqlite` to `world.sqlite.bak.v{version}` before applying DDL
  - Configurable retention (keep last N backups)
  - Backup failure → warning logged, migration proceeds
  - Coverage: `storage` 89.3%

---

## Epic 3: Agents (behavior-as-data) runtime

Full XState v4 state machine interpreter (minus `invoke`). Uses `cond` terminology; Stately Studio v4 exports work with zero manual editing. Agents are sandboxed by construction: they can only invoke registered actions and guards, so a modder's JSON can never execute arbitrary code. This epic delivers the runtime; hot reload and the tick loop come in epics 4 and 5.

Design spec: [`docs/superpowers/specs/2026-05-27-epic3-state-machine-design.md`](superpowers/specs/2026-05-27-epic3-state-machine-design.md)

- [x] **Interpreter-managed tables and schema extensions** — Foundation for everything else.
  - Create `behavior_components` (composite PK `entity_id, machine_id`), `transitions`, `event_queue` at interpreter startup (`CREATE TABLE IF NOT EXISTS`), not via schema.json DDL path
  - Add `"behavior"` field support to component and entity type definitions in schema.json
  - Schema validation: reject any user component named `"Behavior"`; if `"behavior"` declared on a component, verify the machine file exists at startup
  - Entity types with `"behavior"` activate their primary machine on entity creation

- [x] **Machine parser and StateNode tree** — Parse full XState v4 JSON into an in-memory tree.
  - Support all node types: atomic, compound (hierarchical), parallel, final, history
  - Reject `invoke` at any level with a clear error
  - Tolerate unknown top-level fields (Stately adds `description`, `meta`, `tags`)

- [x] **Registry and context types** — The action/guard dispatch layer.
  - `ActionHandler` / `GuardHandler` interfaces (not bare func types — enables future Lua handlers)
  - Registry stores metadata (description, param schemas) for future visual editor introspection
  - `WorldWriter` / `WorldReader` domain-level interfaces — actions/guards never call raw SQL
  - `ActionContext` and `GuardContext` carry entity ID, tick, world interface, static params, event

- [x] **Load-time validator** — Fail loud, fail early.
  - Every `cond.type` and action `type` exists in the registries
  - Every transition `target` resolves to a defined state
  - Every `context` key matches exactly one component field in schema.json (ambiguous = error)
  - Malformed file: log warning, skip, retain previous in-memory version — game keeps running

- [x] **SCXML microstep interpreter** — One transactional unit per event.
  - Full SCXML microstep algorithm: exit set, entry set, parallel regions, history restoration
  - Machine startup: attach missing context-declared components, seed initial values
  - Component-machine lifecycle: `attachComponent` activates behavior machine if declared; machine reaching final state triggers component detach
  - Wrap one event delivery in one SQLite transaction — crash mid-event leaves DB consistent
  - Write `behavior_components` and `transitions` rows per event

- [x] **Delayed transitions (`after`)** — Behaviors need timers.
  - Schedule into `event_queue` with target tick; cancel on state exit
  - `after` durations converted to tick counts at load time

- [ ] **Built-in actions and guards** — Standard library via WorldWriter/WorldReader (never raw SQL).
  - Actions: `moveTowardTarget`, `dealDamage`, `spawnEntity`, `attachComponent`, `detachComponent`, `setTimer`, `log`, `pickRandomTarget`, `setPursueTarget`
  - Guards: `timerExpired`, `atTarget`, `inRange`, `hasComponent`, `healthAbove`

- [ ] **Integration tests and Stately round-trip** — Prove it works end-to-end.
  - `wandering_goblin` fixture: load → deliver events → assert `behavior_components` and `transitions`
  - Component lifecycle: attach behavior-bearing component → machine activates; final state → detach
  - Real Stately v4 export in `testdata/`, parsed and validated in CI

---

## Epic 4: Behavior hot reload

Filesystem watcher so editing `mods/behaviors/*.json` updates the running game with no restart. Small but materially changes the development experience.

- [ ] **Filesystem watcher on `mods/behaviors/`** — Watch, debounce, reload.
  - Debounce rapid writes (editor save bursts)
  - Re-validate changed files against the registries before swapping in
  - Atomic swap of the in-memory machine definition

- [ ] **Hot-reload entity semantics** — Don't strand entities in deleted states.
  - Entities pick up the new definition on next state evaluation
  - If an entity's current state was removed in the reload, reset to the machine's `initial` state
  - Log every reload attempt and outcome (success, validation failure, retained previous version)

---

## Epic 5: Interpreter tick loop & Ebitengine monolith

End-to-end working game in a single binary: interpreter and Ebitengine renderer together. Goal: one entity wandering on screen, editing its agent JSON visibly changes behavior.

- [ ] **Tick loop skeleton** — The interpreter's heartbeat, driven by Ebitengine's `Update()`.
  - Drain `input_events` written by the renderer in the same `Update()` call
  - Dispatch raw input to a game-specific input-to-event mapper → write to `event_queue`
  - Drain due `event_queue` rows (where `target_tick` ≤ `current_tick`)
  - Deliver `TICK` to every entity with an active `behavior_components` row
  - Advance `world.current_tick`, bump `world.world_version`

- [ ] **Input capture layer** — Renderer writes, interpreter drains, same tick.
  - Ebitengine input callbacks write to `input_events` (kind, payload JSON, wall_ms) before interpreter tick runs
  - Game-specific mapper translates raw input rows into game events; marks `consumed = 1`
  - Table doubles as an append-only audit log useful for replay

- [ ] **Transitions audit table writes** — Every successful transition recorded.
  - `tick`, `wall_ms`, `entity_id`, `machine_id`, `from_states`, `to_states`, `event`, `cond_result`, `actions_run`
  - `actions_run` is a JSON array of action names — feeds Epic 8 effects

- [ ] **Ebitengine renderer** — Draw entities from the database each frame.
  - `Draw()` queries entities joined with drawable components after `Update()` completes
  - Default: draw sprite at position for any entity with `Position` + `Sprite`
  - No `world_version` polling — renderer reads directly from the shared SQLite connection after writes
  - Placeholder visual for unrecognized entity types

- [ ] **Smoke test: wandering goblin** — The prototype passes when this works end-to-end.
  - Schema: `Position`, `Sprite` declared; `Goblin` entity type with `behavior: wandering_goblin`
  - One `Goblin` running the `wandering_goblin` agent from the architecture doc
  - Visibly wanders, idles, repeats
  - Edit the agent JSON, save, see behavior change without restart

---

## Epic 6: Debugger process

The one natural process boundary: read-only, different lifecycle, optional in shipped builds, remotely accessible. This is where the "SQLite as game state" benefits become most tangible. The debugger is where you answer "what happened and why?" without touching the game.

- [ ] **HTTP server binary** — Go, using standard library `net/http` and a pure-Go SQLite driver.
  - Opens `world.sqlite` read-only with WAL
  - Polls `world_version` on each refresh; skips full query when unchanged
  - Refuses to start on `schema_version` mismatch

- [ ] **Endpoint: entities** — Roster view.
  - All entities with type and a summary of attached components

- [ ] **Endpoint: components** — Per-entity drill-down.
  - Full component data for a given `entity_id`

- [ ] **Endpoint: transitions** — The "why did the goblin attack?" view.
  - Most recent N, newest first, filterable by `entity_id`, `machine_id`, event type
  - Shows `from_states`, `to_states`, `cond_result`, `actions_run`

- [ ] **Endpoint: schema** — Reference data.
  - Serve current `schema.json` verbatim

- [ ] **Endpoint: ASCII view** — Terminal-style live world view.
  - Entity positions, types, and key component values rendered as text
  - Replaces a separate "second renderer process" — same information, one HTTP route
  - Works with `curl` and `watch` for zero-setup monitoring

- [ ] **Single-page HTML UI** — Polls the endpoints, no build step.
  - Auto-refreshes; per-entity drill-in: state, context, recent transitions
  - Filter transitions by entity / machine / event

- [ ] **Remote debugging verified** — A claim only worth making if tested.
  - Confirm working over Tailscale and over SSH tunnel from a phone

---

## Epic 7: Time-travel debugging

Promoted from "future directions" to a headline capability. The `transitions` table is a complete, append-only audit trail of every state change the engine has ever made. This epic turns that log into an interactive debugging tool: checkpoint the database, replay sessions forward, and scrub the timeline in the debugger UI.

- [ ] **Checkpoint infrastructure** — Periodic database snapshots the replay engine can start from.
  - Interpreter writes checkpoint files (SQLite backup API) at configurable tick intervals
  - Checkpoints stored alongside `world.sqlite`; retention policy configurable (keep last N)
  - Checkpoint metadata table: `tick`, `wall_ms`, `file_path`

- [ ] **Replay engine** — Re-run a session from a checkpoint through the `transitions` log.
  - Open a checkpoint as a read-only base
  - Walk `transitions` rows in tick order, applying each to reconstruct world state at any tick
  - Expose as a debugger-callable interface: `replay(checkpoint, target_tick) → world_snapshot`

- [ ] **Debugger timeline endpoint** — Query replay state.
  - `GET /timeline` — List available checkpoints with tick ranges
  - `GET /timeline/:tick` — Reconstruct and return world snapshot at given tick
  - `GET /timeline/:tick/entities` — Entity roster at that tick
  - `GET /timeline/:tick/transitions?entity_id=N` — Transitions around that tick

- [ ] **Debugger timeline UI** — Scrubber and step controls.
  - Timeline scrubber showing tick range and checkpoint markers
  - Step forward / backward through transitions for a selected entity
  - Entity state panel updates to match selected tick
  - Works without pausing the live game

- [ ] **Smoke test: reproduce a bug via replay** — The capability only counts if it works.
  - Record a session; identify a tick where an entity entered an unexpected state
  - Replay from checkpoint to that tick; confirm entity state matches live recording
  - Step through preceding transitions to find the cause

---

## Epic 8: Effects system

Visual and audio effects as the renderer's interpretation of the `transitions` audit log. The interpreter knows about game state; it does not know about presentation. Three patterns of increasing power.

- [ ] **Renderer polls transitions table** — Effects are observations, not events.
  - Track last-seen `transitions.id`, poll newer rows each frame
  - Catch-up policy: if more than N seconds behind (minimized window), skip ephemeral effects, just catch up world state

- [ ] **Implicit effects from transition shape** — Pattern 1: no agent annotation needed.
  - Example: any transition to a state named `dead` triggers death sound + dust particles at entity position
  - Rules live in the renderer, not the agents

- [ ] **Named effect actions in transitions** — Pattern 2: agent author opts in.
  - Agents include named actions (e.g. `playSwingSound`) in transition `actions`
  - Interpreter records the name in `actions_run` and otherwise no-ops them
  - Renderer reads `actions_run` and triggers the corresponding presentation

- [ ] **Effect rule files** — Pattern 3: retheme without touching agents.
  - Separate JSON file mapping transition patterns → effects
  - Loaded by the renderer at startup
  - Enables full visual/audio reskinning by modders with no agent edits

- [ ] **Renderer-local ephemeral state** — The DB has no business knowing about screen shake.
  - Shake intensity/decay, particle systems, fades, flashes all live in renderer memory
  - Transition triggers the start; renderer animates over frames

---

## Epic 9: Process supervision & packaging

Run reliably during development; ship cleanly. Two processes: the game binary and the optional debugger.

- [ ] **Dev supervisor config** — Two processes, one command.
  - Procfile or systemd user unit for game + debugger
  - Interleaved logs with consistent wall-clock timestamps and per-process tags
  - Restarting the debugger leaves the game running

- [ ] **Launcher binary for shipped builds** — One executable the user double-clicks.
  - Spawns the game binary; debugger is off by default
  - Tears down cleanly on exit / crash
  - Optional `--debug` flag to launch the debugger sidecar

- [ ] **Startup schema-check coordination** — Both processes refuse mismatched DBs with the same clear error.

- [ ] **Logging conventions** — Cross-process correlation should be a grep.
  - `wall_ms` on every log line and on every cross-process-relevant DB row
  - Per-process tag in log output (`[game]`, `[debugger]`)

---

## Deferred / not yet epics

Listed here so they aren't forgotten, but explicitly out of scope until earlier epics are real:

- **Extract renderer process** — The "database as contract" convention within the monolith can be promoted to an enforced process boundary if a concrete need surfaces (different graphics language, crash isolation requirements, multi-renderer). The architecture supports it; it is not currently scheduled.
- WASM browser deployment (interpreter to wasm32, SQLite-WASM, JS renderer like Phaser)
- Lua actions and guards — extend the action library for mods without WASM; registry already designed for this (`LuaActionHandler` implements `ActionHandler`)
- Visual state machine editor (web UI; introspects registry and schema.json for action/guard pickers)
- Schema visual editor with automatic migration generation
- Networked multiplayer via replicated `event_queue` and deterministic lockstep
