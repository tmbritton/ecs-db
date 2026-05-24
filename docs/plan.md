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

- [ ] **Entity creation with type validation** — Enforce contracts at attachment time, not query time.
  - Required components present at creation
  - No disallowed components when `allowExtraComponents=false`
  - Honor `validationLevel`: `strict` refuses, `warning` logs and proceeds

- [ ] **Component attach/detach with type validation** — Same rules apply post-creation.
  - Reject attaching unknown components
  - Reject attaching disallowed components on strict types
  - Detaching a required component is an error

- [ ] **world.sqlite bootstrap** — Cleanly create or open the database.
  - Create on first run, write `schema_version` to `meta`
  - On open, compare `meta.schema_version` to `schema.json` — trigger migrations on mismatch (see Epic 2)
  - Record build info in `meta` for debugging

---

## Epic 2: Schema versioning & migrations

Finish the schema system as a coherent unit. Versioning, migrations, and mod compatibility belong with the data model itself, not bolted on after the renderer ships. The architecture doc suggested deferring this; the tradeoff is doing the work before the exact pain shape is known, in exchange for never having a `TODO: migrations` flag hanging in the codebase.

- [ ] **Migration file format** — Versioned, ordered, up-only initially.
  - `migrations/N_to_N+1/` directories or numbered files
  - DDL change + optional data backfill
  - Down migrations deferred until a concrete need appears

- [ ] **Migration runner** — Apply transactionally.
  - On startup mismatch, compute path from db `schema_version` → current `schema.json` version
  - Apply migrations in order, each in a single transaction
  - Update `meta.schema_version` on success
  - Fail loud and roll back on any step failure — no partial migrations

- [ ] **Backup before migrate** — Cheap insurance.
  - Copy `world.sqlite` to `world.sqlite.bak.{version}` before applying
  - Configurable retention (keep last N backups)

- [ ] **Migration authoring workflow** — Document the cycle while the system is fresh.
  - Bump `schema.json` version
  - Write DDL change
  - Write data backfill if needed
  - Test against a snapshot of the previous version
  - README in `migrations/` documenting the pattern

- [ ] **Mod pack compatibility** — Mods are versioned too.
  - Mod packs declare a target schema version
  - Reject loading mods built against an incompatible schema with a clear message
  - Decide policy: hard refuse, or allow with a warning when only additive changes have happened since the mod's target

- [ ] **Smoke test: round-trip a migration** — Prove it works before relying on it.
  - Create DB at version N, add a component to schema, write a migration, run, verify data is intact and queryable
  - Verify the backup file exists and is openable

---

## Epic 3: Agents (behavior-as-data) runtime

XState-subset state machine interpreter. Agents are sandboxed by construction: they can only invoke registered actions and guards, so a modder's JSON can never execute arbitrary code. This epic delivers the runtime; hot reload and the tick loop come in epics 4 and 5.

- [ ] **Parse XState-subset agent JSON** — Lock the supported feature set.
  - Supported: `states`, `initial`, `on`, `entry`, `exit`, `after`, `guard`, `target`, `actions`, `context`
  - Out of scope (for now): hierarchical states, parallel states, invoked services
  - Stately Studio export must round-trip through the parser

- [ ] **Action registry** — Code lives in the interpreter, never in JSON.
  - Built-ins: `moveTowardTarget`, `dealDamage`, `spawnEntity`, `attachComponent`, `setTimer`, `log`, `pickRandomTarget`, `setPursueTarget`
  - Host app registers game-specific actions before interpreter starts
  - Action call context: entity id, current tick, active DB transaction, machine context, static params, triggering event payload

- [ ] **Guard registry** — Same shape, read-only.
  - Built-ins: `timerExpired`, `atTarget`, `inRange`, `hasComponent`, `healthAbove`
  - Same context as actions but mutations are rejected
  - Guards return bool; the result is recorded in `transitions.guard_result`

- [ ] **Agent validation at load time** — Fail loud, fail early.
  - Every `actions[].type` and `guard.type` name exists in the registries
  - Every transition `target` resolves to a defined state in the same machine
  - Malformed file: log warning, skip, retain previous in-memory version — game keeps running

- [ ] **State machine execution** — One transactional unit per event.
  - On event delivery: evaluate guards in declared order, run exit/transition/entry actions, persist new state and context
  - Wrap one event delivery in one SQLite transaction — crash mid-event leaves DB consistent
  - Append a row to `transitions` with `from_state`, `to_state`, `event`, `guard_result`, `actions_run`

- [ ] **Delayed transitions (`after`)** — Behaviors need timers.
  - Schedule into `event_queue` with target tick
  - Cancel pending timers on state exit so a stale `after` doesn't fire after the state has changed

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
- Hierarchical and parallel states in agents
- Replay and time-travel debugging from the `transitions` + `event_queue` log
- Visual behavior editor with live state overlay on top of Stately's renderer
- Schema visual editor with automatic migration generation
- Networked multiplayer via replicated `event_queue` and deterministic lockstep
