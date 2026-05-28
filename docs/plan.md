# Plan: Process-separated, database-as-contract game engine

Implementation plan derived from the architecture doc. Build the schema system as a complete unit first, then the agents runtime, then a monolithic prototype, then split processes one at a time, then polish. Each epic will be refined into concrete tasks in a follow-up pass.

Language-agnostic by intent — interpreter language (Go/Rust) and renderer language (Odin/Rust/JS/Python) are decisions deferred to the relevant epic.

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

- [ ] **Interpreter-managed tables and schema extensions** — Foundation for everything else.
  - Create `behavior_components` (composite PK `entity_id, machine_id`), `transitions`, `event_queue` at interpreter startup (`CREATE TABLE IF NOT EXISTS`), not via schema.json DDL path
  - Add `"behavior"` field support to component and entity type definitions in schema.json
  - Schema validation: reject any user component named `"Behavior"`; if `"behavior"` declared on a component, verify the machine file exists at startup
  - Entity types with `"behavior"` activate their primary machine on entity creation

- [ ] **Machine parser and StateNode tree** — Parse full XState v4 JSON into an in-memory tree.
  - Support all node types: atomic, compound (hierarchical), parallel, final, history
  - Reject `invoke` at any level with a clear error
  - Tolerate unknown top-level fields (Stately adds `description`, `meta`, `tags`)

- [ ] **Registry and context types** — The action/guard dispatch layer.
  - `ActionHandler` / `GuardHandler` interfaces (not bare func types — enables future Lua handlers)
  - Registry stores metadata (description, param schemas) for future visual editor introspection
  - `WorldWriter` / `WorldReader` domain-level interfaces — actions/guards never call raw SQL
  - `ActionContext` and `GuardContext` carry entity ID, tick, world interface, static params, event

- [ ] **Load-time validator** — Fail loud, fail early.
  - Every `cond.type` and action `type` exists in the registries
  - Every transition `target` resolves to a defined state
  - Every `context` key matches exactly one component field in schema.json (ambiguous = error)
  - Malformed file: log warning, skip, retain previous in-memory version — game keeps running

- [ ] **SCXML microstep interpreter** — One transactional unit per event.
  - Full SCXML microstep algorithm: exit set, entry set, parallel regions, history restoration
  - Machine startup: attach missing context-declared components, seed initial values
  - Component-machine lifecycle: `attachComponent` activates behavior machine if declared; machine reaching final state triggers component detach
  - Wrap one event delivery in one SQLite transaction — crash mid-event leaves DB consistent
  - Write `behavior_components` and `transitions` rows per event

- [ ] **Delayed transitions (`after`)** — Behaviors need timers.
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

## Epic 5: Interpreter tick loop & monolithic prototype

End-to-end working prototype in a single process. The renderer is embedded for now — it will be extracted in Epic 7. Goal: one entity wandering on screen, and editing its agent JSON visibly changes the behavior.

- [ ] **Tick loop skeleton** — The interpreter's heartbeat.
  - Drain unconsumed `input_events` → dispatch to a game-specific input-to-event mapper
  - Drain due `event_queue` rows (where tick ≤ current_tick)
  - Deliver `TICK` to every entity with a `Behavior` component
  - Advance `world.current_tick`, bump `world.world_version`

- [ ] **Input-to-game-event mapping layer** — Keep this in the interpreter, by design.
  - Game-specific module reads raw `input_events`, writes meaningful game events to `event_queue`
  - Mark `input_events.consumed = 1` after dispatch
  - Renderer never interprets input — it only records

- [ ] **Transitions audit table writes** — The debugging goldmine.
  - Every successful transition: `tick`, `wall_ms`, `entity_id`, `machine_id`, `from_state`, `to_state`, `event`, `guard_result`, `actions_run`
  - `actions_run` is a JSON array of action names — feeds Epic 8 effects

- [ ] **Embedded stub renderer** — Just enough to see things move.
  - Minimal in-process renderer; whatever's fastest to wire
  - Reads entities + drawable components keyed off `world_version`
  - Explicit non-goal: production-quality rendering. This gets replaced in Epic 7.

- [ ] **Smoke test: wandering goblin** — The prototype passes when this works end-to-end.
  - Schema: `Position`, `Sprite`, `Behavior` declared; `Goblin` entity type
  - One `Goblin` running the `wandering_goblin` agent from the architecture doc
  - Visibly wanders, idles, repeats
  - Edit the agent JSON, save, see behavior change without restart

---

## Epic 6: Extract debugger process

The easiest split: read-only, no write contention. Validates the database-as-contract claim with low risk before tackling the renderer split. Small HTTP server + a single HTML page.

- [ ] **HTTP server binary** — Whatever language has the easiest HTTP story (Go is the natural choice).
  - Opens `world.sqlite` read-only with WAL
  - Refuses to start on `schema_version` mismatch

- [ ] **Endpoint: entities** — Roster view.
  - All entities with their type and a summary of attached components

- [ ] **Endpoint: components** — Per-entity drill-down.
  - Full component data for a given entity_id

- [ ] **Endpoint: transitions** — The "why did the goblin attack?" view.
  - Most recent N, newest first, filterable by `entity_id` and `machine_id`
  - Include `context` snapshot if cheap

- [ ] **Endpoint: schema** — Reference data.
  - Serve current `schema.json` contents verbatim

- [ ] **Single-page HTML UI** — Polls the endpoints, no build step.
  - Auto-refreshes
  - Per-entity drill-in: state, context, recent transitions
  - Filter transitions by entity / machine / event

- [ ] **Remote debugging verified** — A claim only worth making if tested.
  - Confirm working over Tailscale and over SSH tunnel from a phone

---

## Epic 7: Extract renderer process

The bigger split: input flow becomes asynchronous, the renderer gets its own language and graphics library choice, and the three-process architecture is real. After this epic, crash isolation is no longer a claim.

- [ ] **Choose renderer language + graphics library** — Make and document the call.
  - Candidates: Odin + Raylib, Rust + Macroquad, JS + Phaser (browser path), Python (terminal)
  - Document the decision and the constraints that drove it

- [ ] **Renderer binary opens `world.sqlite` as WAL reader** — One connection per process.
  - Schema_version check on startup

- [ ] **`world_version` polling loop** — Cheap watermark, expensive query only when needed.
  - Each frame: read `world_version`; if unchanged, redraw last snapshot
  - On change: query entities joined with drawable components, rebuild local snapshot

- [ ] **Generic drawing from entity type + Sprite** — Renderer is dumb on purpose.
  - Default: draw sprite at position for any entity with `Position` + `Sprite`
  - Placeholder visual for entity types the renderer doesn't recognize specifically
  - No game-logic awareness beyond entity type and components

- [ ] **Input capture to `input_events`** — Record, don't interpret.
  - Each input gets `wall_ms` timestamp, `kind`, `payload` JSON, `consumed=0`
  - Renderer is the sole writer of `input_events`

- [ ] **Dev supervisor wiring** — Run all three processes together.
  - Procfile / overmind / foreman / systemd user units
  - Interleaved logs with wall-clock timestamps for cross-process correlation
  - Restarting one process leaves the others running

- [ ] **Verify crash isolation** — The whole pitch hinges on this.
  - Kill renderer → interpreter keeps simulating, debugger keeps showing state, restart catches up via `world_version`
  - Kill interpreter → renderer keeps last frame up, debugger keeps serving last DB state
  - Document the recovery semantics

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

## Epic 9: Second renderer

Prove the multi-renderer thesis. Also a genuinely useful debugging tool — a terminal ASCII view next to the main view tells you what the game thinks is happening regardless of sprite or shader bugs.

- [ ] **Pick the second renderer type** — Decide what gives the most leverage.
  - Candidates: terminal ASCII view, top-down minimap, headless screenshot-on-tick renderer for tests

- [ ] **Implement against the same contract** — No new IPC.
  - Same `world_version` polling, same component queries
  - Same `input_events` writes if interactive

- [ ] **Run both renderers simultaneously** — The actual proof.
  - Both see consistent world state
  - Both can write inputs without contention (within the one-writer-per-table rule)
  - Document any caveats discovered

---

## Epic 10: Process supervision & packaging

Run reliably during development; ship cleanly. Small but unglamorous work that decides whether the architecture is pleasant to live with day to day.

- [ ] **Dev supervisor config** — Already wired in Epic 7, polish here.
  - Procfile or systemd user unit, whichever fits the dev environment
  - Interleaved logs with consistent wall-clock timestamps

- [ ] **Launcher binary for shipped builds** — One executable the user double-clicks.
  - Spawns interpreter and renderer subprocesses
  - Tears down children cleanly on exit / crash
  - Debugger optional and off by default in shipped builds

- [ ] **Startup schema-check coordination** — All three processes refuse mismatched DBs with the same clear error.

- [ ] **Logging conventions** — Cross-process correlation should be a grep, not a forensics project.
  - `wall_ms` on every log line and on every cross-process-relevant DB row
  - Per-process tag in log output

---

## Deferred / not yet epics

Future-directions material from the architecture doc. Listed here so they aren't forgotten, but explicitly out of scope until earlier epics are real:

- WASM browser deployment (interpreter to wasm32, SQLite-WASM, JS renderer like Phaser)
- WASM custom actions to extend the action library for mods
- Lua actions and guards (registry already designed for this; add `LuaActionHandler`)
- Visual state machine editor (web UI; introspects registry and schema.json for action/guard pickers)
- Replay and time-travel debugging from the `transitions` + `event_queue` log
- Schema visual editor with automatic migration generation
- Networked multiplayer via replicated `event_queue` and deterministic lockstep
