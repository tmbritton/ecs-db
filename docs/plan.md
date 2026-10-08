# Plan: ECS-in-SQLite game engine with declarative behaviors

Implementation plan derived from the architecture doc. Build the schema system as a complete unit first, then the agents runtime, then a working monolithic game (interpreter + Ebitengine renderer in one binary), then the debugger as the one natural process boundary, then time-travel, effects, and polish. Epics 10 onward build **Forge**, the content-authoring front-end, against the file formats and read layer the engine epics establish. Each epic will be refined into concrete tasks in a follow-up pass.

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
  - Pragmas at init: WAL, `synchronous=NORMAL`, `busy_timeout=5000`, `foreign_keys=ON` — "at init" was built as four `db.Exec` calls against the pool, which reached one connection out of it; corrected in Story 7
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

- [x] **Pragmas on every connection** — `foreign_keys`, `busy_timeout` and `synchronous` move into the DSN so the pool cannot hand out a connection without them.
  - Measured: `foreign_keys` 1/0/0 and `busy_timeout` 5000/0/0 across three simultaneous connections, so `ON DELETE CASCADE` was enforced on one connection and no other
  - `journal_mode` stays a single statement, because WAL lives in the database file rather than on the connection
  - The path is percent-encoded into a `file:` URI: a `?` in it used to truncate the filename and silently bootstrap a second database somewhere else
  - One builder and one `OpenReadOnly`, so "Forge never writes to the game database" is a property of a function rather than of every caller
  - Enforcing foreign keys for real means deleting an entity another entity's entity-ref points at is now refused — an improvement over the dangling reference it used to leave, and a schema question that wants its own story

- [x] **Names that reach SQL are identifiers** — Component and property names are validated where the schema is loaded, so the generator and the insert path can rely on it.
  - Three silent failures closed: `two words` built a column called `two` of type `words INTEGER`; `Probe` and `probe` shared one table; `current_time` built, accepted a write, and read back as the clock
  - The reserved list is the 60 keywords SQLite actually refuses plus the 3 that fail silently — not all 147, because 84 of them (`action`, `key`, `first`, `row`) are names a game schema wants
  - A test re-derives that list from SQLite on every run, so a driver upgrade that changes the answer fails a test rather than a map
  - Bootstrap is one transaction: `meta` used to be created outside it, which left behind exactly the table that makes the next open migrate instead of build
  - `NewSQLiteStore("")` refused, and `pruneBackups` escapes and cleans its path — both defaults that quietly did the wrong thing

- [x] **An entity-ref does not outlive its target** — `ON DELETE CASCADE` on every reference to an entity, so deleting the target removes the pointing component and leaves its holder.
  - Replaces a restrict nobody chose: no `ON DELETE` clause meant deleting an entity could fail because of another entity's data
  - An entity-ref *property* had no foreign key at all when its table was created — only when added by `ALTER TABLE`, and a rebuild dropped it again
  - The three paths now agree that a property reference is nullable: declaring it `NOT NULL` on create and rebuild wedged any database where the property had been added to a populated component
  - An existing database is not migrated — constraints are not in the shape the diff introspects — and the gap is pinned as a test rather than left as prose

Stories 10–13 were written here and belong to Epic 2 — they are introspection, diff and DDL generation, not the schema foundation. They have moved; their numbers have not, because the code refers to them by number and Epic 1 already has a Story 10's worth of its own.

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

**Reopened.** The six stories above were complete in the sense that their acceptance criteria were met. They were not correct: nothing had yet run the migration path against adversarial schemas, and Forge — which generates schema edits by the click rather than by hand — found several ways to make a database refuse to open, or to drop data and report success. The four below are the repairs. Numbering continues Epic 1's sequence because that is where they were written and because the code cites them by number.

- [x] **A component that changes shape is rebuilt, not patched** — the diff asks whether the columns a table has can be altered into the ones the file wants, rather than comparing a type the database does not record.
  - Changing a component between `entity-ref` and any other scalar type used to fail the migration and then fail every subsequent open
  - An object whose one property is called `value` builds a table a string component would; the old comparison guessed "number" and dropped it — on an unchanged schema, and it is the shape Forge gives every new component
  - A scalar becoming an object now keeps its rows; the reverse still cannot
  - A statement the generator refused was committed around, because its SQL was empty and `tx.Exec("")` succeeds
  - Destructive statements are warnings, and the confirmation says the shape changed rather than just naming a drop

- [x] **A database is repaired when the generator changes, not only when the version does** — foreign keys are introspected and diffed, and a constraint that differs from what the generator would emit is answered with a table rebuild.
  - Two gates stood between "the generator changed" and "the table is rebuilt": `checkAndMigrate` returned the moment `meta.schema_version` matched the file's, and introspection never read `pragma_foreign_key_list`
  - So Story 9's cascade reached no existing database, and a schema edit saved without a version bump did nothing — which Forge had to warn about rather than rely on
  - One statement of what a reference is: `schema.EntityReference`, written in the form the pragma reports, with the generator's DDL derived from it and a test building a real table for every component and property type
  - `MigrationRunner` splits into `Plan` and `Apply`, so the backup happens between deciding there is work and doing it; a plan is empty on statements rather than changes, because an entity-less database reports every entity type as new
  - One rebuild per component rather than one per change that wants one, carrying every reason
  - Found by review: `LIKE 'comp_%'` has an unescaped wildcard, so a table merely *starting* with "comp" made the migration repeat on every open forever; and `.bak.v{version}` is no longer unique, so a second repair was overwriting the first one's restore point

- [x] **A rebuild carries every row, and nullability is part of a column's shape** — the copy substitutes a column's default where a NULL cannot come across, and a column whose nullability drifted is repaired.
  - An entity-ref property is the only column the generator declares nullable, so retyping one made the rebuild's copy fail — and `NewSQLiteStore` returns that error on *every* subsequent open, so the database never opened again until `schema.json` was edited back
  - The engine already invents a value for exactly this: `ALTER TABLE ADD COLUMN … NOT NULL DEFAULT 0` gives every existing row a `0`. The copy now uses the same function, so adding a property and retyping one cannot disagree
  - `notnull` is introspected and diffed, so a column that should refuse NULLs and does not is repaired — the half Story 11 left open. The primary key is excluded: SQLite reports every `INTEGER PRIMARY KEY` as nullable
  - Loud, because it invents data: the log names the column, the value and how many rows
  - Found by review: the one case documented as needing a hand-edited database was reachable in one save, because a component's *type* can change into the shape whose column it collides with — `target_entity_id` is now a reserved property name
  - Found by review: the nullability rule was written out in five places and derived in none, so the rebuild's belief about a column could contradict the DDL it emitted for it with no test failing

- [x] **A rename keeps its data** — `renamedFrom` on a component, a property or an entity type migrates the table, the column or the rows instead of dropping them.
  - Renaming a component dropped its table and built an empty one; renaming a property dropped the column; renaming an entity type produced no statements at all and stranded every existing row under the old string. All three reported success
  - A rename cannot be inferred — `x → col_x` and "delete x, add col_x" are the same diff — so the author says it, and the format had nowhere to. Unknown property keys already parse, so the field breaks no existing file
  - The diff *applies* the declared renames to the introspected schema before comparing anything, so types, foreign keys and nullability all compare like for like rather than against a column believed dropped
  - A rename nobody declared is still reported: every dropped table and column says what it takes, and an unambiguous one-for-one swap names `renamedFrom` as the fix
  - Found by review: the generator was handed the pre-rename snapshot, so a rename plus any rebuild on the same thing failed the migration and made the database unopenable — reopening the wedge story 12 closed
  - Found by review: an entity-type rename was skipped when the new name already had rows, which is a guard that belongs to tables and not to an `UPDATE`; and a declared rename that could not be applied dropped the table without saying the declaration had been ignored

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

- [x] **Built-in actions and guards** — Standard library via WorldWriter/WorldReader (never raw SQL).
  - Actions: `moveTowardTarget`, `dealDamage`, `spawnEntity`, `attachComponent`, `detachComponent`, `setTimer`, `log`, `pickRandomTarget`, `setPursueTarget`
  - Guards: `timerExpired`, `atTarget`, `inRange`, `hasComponent`, `healthAbove`

- [x] **Integration tests and Stately round-trip** — Prove it works end-to-end.
  - `wandering_goblin` fixture: load → deliver events → assert `behavior_components` and `transitions`
  - Component lifecycle: attach behavior-bearing component → machine activates; final state → detach
  - Real Stately v4 export in `testdata/`, parsed and validated in CI

---

## Epic 4: Behavior hot reload

Filesystem watcher so editing `mods/behaviors/*.json` updates the running game with no restart. Small but materially changes the development experience.

Refined into stories: See [`docs/stories/epic-4/`](docs/stories/epic-4/).

- [x] **Config file and mod directory structure** — TOML config replacing hardcoded paths; multi-mod support.
  - `internal/config` package: `Config`, `DatabaseConfig`, `SchemaConfig`, `ModConfig` structs; `Load()` and `Defaults()`
  - `ModConfig` fields: `name`, `behaviors`, `actions` (Lua, future), `guards` (Lua, future), `assets` (future)
  - `agent.Loader.ScanDir(dir, modName)` — scan a directory, load all `*.json`, last mod wins on duplicate ID
  - CLI `-config` flag defaulting to `./game.toml`; `game.toml` committed to repo

- [x] **Filesystem watcher on `mods/behaviors/`** — Watch, debounce, reload.
  - Debounce rapid writes (editor save bursts)
  - Re-validate changed files against the registries before swapping in
  - Atomic swap of the in-memory machine definition

- [x] **Hot-reload entity semantics** — Don't strand entities in deleted states.
  - Entities pick up the new definition on next state evaluation
  - If an entity's current state was removed in the reload, reset to the machine's `initial` state
  - Log every reload attempt and outcome (success, validation failure, retained previous version)

---

## Epic 5: Interpreter tick loop & Ebitengine monolith

End-to-end working game in a single binary: interpreter and Ebitengine renderer together. Ebitengine runs at 60 TPS; the interpreter tick fires every 3rd `Update()` call (~20 Hz game logic, 60 Hz input sampling). Goal: a goblin wanders a tile-based map, a player walks around, editing the agent JSON visibly changes behavior with no restart.

Design spec: [`docs/superpowers/specs/2026-06-02-epic5-renderer-design.md`](superpowers/specs/2026-06-02-epic5-renderer-design.md)

Refined into stories: See [`docs/stories/epic-5/`](docs/stories/epic-5/).

- [x] **Schema updates** — Evolve `Sprite`; add `Tile`, `Path`, `Speed` components and `Tile` entity type. Auto-migration verifies the pipeline still works.

- [x] **Ebitengine wiring + tick loop** — `cmd/game/main.go`; `Game` struct with `Update()`/`Draw()`; 60 TPS / 20 Hz split via `frameCount % (60/TicksPerSecond)`; interpreter tick (drain `input_events` → drain `event_queue` → deliver `TICK` → advance tick + `world_version`) inside `Update()`.

- [x] **Tilemap + TileGrid** — TOML map file; bootstrap creates `Tile` entities at startup; `internal/tilemap.TileGrid` (`[][]bool`) built from tile entities; static render buffer drawn once to `*ebiten.Image`; invalidated when a tile changes.

- [x] **Input capture + player movement** — `ebiten.IsKeyPressed` sampled 60 Hz; rows appended to `input_events`; game-specific input handler drains rows each tick and moves the `Player` entity one tile per tick (checking `TileGrid` for passability), setting `comp_sprite.animation` and `flip_x`.

- [x] **Sprite renderer + animation system** — Updated `Sprite` component (`sheet`, `animation`, `flip_x`); animation definitions TOML in `mods/assets/` (hot-swappable via watcher); `AnimState` map in renderer (frame timer at 60 Hz); `setAnimation` built-in action; `Draw()` queries all entities with `Position` + `Sprite` and renders the current frame.

- [x] **Asset loader + hot-reload sprite sheets** — `[[entity_asset]]` sections in `animations.toml` map entity types to sprite sheets; `AnimLoader.SyncToDatabase` stamps `comp_sprite.sheet` for all entities on every startup; `ImageCache` (ebitengine-tagged) caches `*ebiten.Image` per path and evicts on fsnotify PNG change events, forcing reload on next Draw.

- [x] **Pathfinding** — `internal/tilemap.AStar`; built-in action `computePath` (reads `comp_position`, runs A*, writes `comp_path`); action `stepAlongPath` (advances `comp_position`, increments `current_index`); guard `pathComplete` (`current_index ≥ len(waypoints)`).

- [x] **Line-of-sight + tile mutation** — Built-in guard `inLineOfSight` (DDA ray walk on `TileGrid`); built-in action `setTilePassable` (writes `comp_tile.passable` + calls `grid.SetPassable`). `TileGrid` stores entity ID per cell (populated by `Rebuild`) so `setTilePassable` can write through to the DB without a separate query. Both registered via `RegisterLineOfSight(r, grid)`.

- [x] **Goblin smoke test** — `behaviors/goblin.json` (idle → pick random target + `computePath` → wandering via `stepAlongPath` / `pathComplete` → idle loop); `GoblinStats` component added to schema (v3) for `target_x`/`target_y` storage; goblin bootstrapped at (15,12) via `ensureGoblinEntity`; behavior started via `ensureGoblinBehavior` which calls `agent.StartAgent`; window shows map + player + autonomous wandering goblin.

---

## Epic 6: Debugger read layer

The one natural process boundary: read-only, different lifecycle, optional in shipped builds, remotely accessible. This is where the "SQLite as game state" benefits become most tangible — where you answer "what happened and why?" without touching the game.

Scope note: this epic delivers the **read layer and its HTTP routes**, not a bespoke UI. Forge's LIVE mode (Epic 18) is the graphical debugger, and consumes the same `internal/inspect` package in-process. The JSON and ASCII routes remain valuable on their own — they are what `curl`, `watch`, and a phone over SSH talk to.

- [ ] **`internal/inspect` read-only query layer** — The reusable core, no HTTP in it.
  - Opens the game database read-only (`file:<path>?mode=ro`), never through `NewSQLiteStore` (which bootstraps and migrates)
  - Reads the `world_version` watermark; callers skip the full query when unchanged
  - Refuses to attach on `schema_version` mismatch (`storage.ReadSchemaVersion`)

- [ ] **HTTP server binary** — Go, standard library `net/http`, pure-Go SQLite driver.

- [ ] **Endpoint: entities** — Roster view.
  - All entities with type and a summary of attached components

- [ ] **Endpoint: components** — Per-entity drill-down.
  - Full component data for a given `entity_id`

- [ ] **Endpoint: transitions** — The "why did the goblin attack?" view.
  - Most recent N, newest first, filterable by `entity_id`, `machine_id`, event type
  - Shows `from_states`, `to_states`, `cond_result`, `actions_run`
  - `transitions` is write-only today; this story adds the first read API

- [ ] **Endpoint: schema** — Reference data.
  - Serve current `schema.json` verbatim

- [ ] **Endpoint: ASCII view** — Terminal-style live world view.
  - Entity positions, types, and key component values rendered as text
  - Replaces a separate "second renderer process" — same information, one HTTP route
  - Works with `curl` and `watch` for zero-setup monitoring

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

## Epic 10: Forge — foundation, toolchain & design system

**Refined into stories:** See [`docs/stories/epic-10/`](stories/epic-10/).

Forge is the content-authoring front-end for the engine — a single-window, six-mode editor for `schema.json`, `behaviors/*.json`, tilemaps, tilesets and sprite animations, served as a webapp from this same binary. The UI is recreated from a design handoff — a written spec plus an interactive HTML prototype — kept in [Claude Design](https://claude.ai/design/p/d2402254-cab9-4972-9987-bf689dc6329a) rather than vendored into this repo. See [`docs/stories/epic-10/README.md`](stories/epic-10/README.md) for what it contains and how to work against it.

This epic is the floor everything else stands on: a `forge` subcommand that builds and runs without X11 or CGO, the Go + Templ + Datastar toolchain, and the token/primitive layer every later mode renders through. The "technical instrument" look — dark, dense, mono-labelled, 2px hard borders, no rounded corners, no gradients — is load-bearing for the product's identity and is specified exactly in the handoff.

- [x] **CLI restructure** — Split the Cobra tree out of the `ebitengine` build tag so Forge can build headless.
  - `cmd/game/` → `cmd/ecs-db/`; root command `Use: "ecs-db"` with `run`, `schema validate`, `forge`
  - Tag-free root; `run` behind `//go:build ebitengine` with a `!ebitengine` stub that errors clearly
  - `make build-headless` proves `CGO_ENABLED=0 go build ./cmd/ecs-db` works with no tags

- [x] **Web toolchain** — templ + Datastar + embedded assets, no frontend build step.
  - `github.com/a-h/templ` as a go.mod `tool` dependency; `templ generate` wired into `make generate`
  - Datastar client JS vendored under `static/js/vendor/`; the Go SDK arrives with the first SSE endpoint in Story 5
  - `internal/forge/web/static/` served from `go:embed`; self-hosted fonts, no CDN
  - `internal/forge/server` — `net/http`, `[forge]` config section, graceful shutdown

- [x] **Design tokens** — The palette and type system as CSS custom properties.
  - Surfaces, borders, four text weights, five semantic accents (amber/green/cyan/violet/red)
  - Chakra Petch for UI, JetBrains Mono for anything the engine owns
  - 2px borders, no border-radius, no gradients, hard offset shadows; `fpulse`/`fblink`/`fdash` keyframes

- [x] **Templ primitives** — Panel, SectionHeading, ListRow, Chip, SegmentedControl, Dropdown, Checkbox, IconButton, ContextMenu, ModalShell, SaveFooter.

- [x] **App shell** — Menu bar, 62px mode rail with the six modes + settings cog, routed mode content.
  - `internal/forge/mode` — one table drives the rail, the routes and the tests
  - `/forge/{mode}` is a full page load; rail buttons are anchors, so history and deep links work unaided
  - One SSE subscription per page (`data-init="@get('/forge/{mode}/events')"`), stubbed for Story 6

- [x] **Engine-status readout** — Connected vs watcher-offline, pushed over SSE.
  - `internal/forge/status` — read-only `mode=ro` check, never through `NewSQLiteStore`
  - Three states: connected, version mismatch, offline — a stale database is not an absent one
  - Rendered server-side on load, then patched down the page-level stream; identical patches suppressed

**Epic 10 is complete.** `ecs-db forge` builds without CGO or X11, serves all six modes, and reports
the engine connection live. The token and primitive layer, the app shell, and a Playwright e2e suite
(`make e2e`) are in place for Epics 11-20 to build on.

---

## Epic 11: Forge — project model & engine file I/O

Forge writes the files the engine reads. This epic is the read/write spine: resolve a project, round-trip its files, track dirty state, and save without ever letting the engine's fsnotify watcher see a half-written file.

The engine has **no file-writing code at all** today — no `schema.json` writer, no XState emitter. Both are built here, and both have traps: Go map marshalling reorders keys, `EntityType` serializes nil slices as `null`, and `StateNode.Parent` is a back-pointer that makes naive `json.Marshal` recurse.

- [x] **Project model** — Resolve `game.toml`, mod load order, and behavior-file override semantics.
  - `agent.Loader.List()`/`Sources()` added, both returning copies
  - Override detection derived from the loader's own behaviour, not a second copy of the rule
  - A broken machine is a reportable problem; a broken schema is fatal

- [x] **`schema.Marshal`** — Serializer for `DatabaseSchema` that produces clean `git diff`s.
  - Authored order recorded at load and honoured on save; unrecorded keys sort and append
  - The authored layout turned out to be deterministic, so the round trip is byte-stable with no reformat

- [x] **XState emitter** — `MachineDefinition` back to JSON that still imports into Stately Studio.
  - Long form emitted consistently: the authored file uses it, and the parsed form cannot recover which was written
  - Key order recorded at parse; `internal/jsonorder` now shared with `schema.Marshal`
  - ⚠ Unknown fields (`meta`, `description`, `tags`) are dropped — `meta` is Stately's layout. See the story.

- [x] **Dirty tracking & save/discard** — Against an on-disk snapshot, driving the shared save footer.
  - Dirty is a comparison of serialised bytes, so an edit and its exact reversal comes out clean
  - Save refuses an invalid value or an externally-modified file, without discarding the edit

- [x] **Atomic writes** — Temp file + rename, so hot reload never sees a partial file.
  - Delegates to `natefinch/atomic`, already a dependency and already correct
  - Its temp names are invisible to the engine watcher's `.json` filter — checked, not assumed

- [x] **Hot-reload feedback** — Surface reload success/failure in the UI, and wire `agent.Reconciler.Reconcile` via `Watcher.SetReconcileFunc` (written and tested in Epic 4, never given a caller).
  - `internal/forge/savereport` — four outcomes, precise about what Forge can actually know
  - Pushed per file down the existing page stream; "nothing listening" is not a failure
  - `agent.ReconcileOnReload` bridges the callback to the reconciler; the stale "nil until wired" comment is gone

**Epic 11 is complete.** Forge can resolve a project, round-trip `schema.json` and the
XState machines byte-stably, know exactly what is unsaved, save without ever showing the
engine a half-written file, and say what became of each save.

---

## Epic 12: Forge — SCHEMA & ENTS modes

**Refined into stories:** See [`docs/stories/epic-12/`](stories/epic-12/).

Both halves of `schema.json`: components with a live generated-DDL preview, and entity types with their component contracts. The DDL panel is driven by the **real** generator, so the preview cannot drift from what the interpreter emits.

This is the first epic where Forge changes anything — everything before it read files or built the frame. The stories add a seventh to the list below: both modes edit one file, so they share one editing session, and building that inside whichever mode landed first is how it would end up shaped for that mode only.

Two places the design outruns the engine, both carried forward from the original plan and both now assigned to the story that hits them: `array‹entity-ref›` junction tables do not exist (arrays are one JSON `TEXT` column), and spawn counts need Epic 14's object layers.

- [x] **Editing session** — One `editable.File[schema.DatabaseSchema]` per project, shared by both modes, saved through Epic 11's machinery.
  - `Edit` takes a callback so no caller can hold a stale pointer or forget the lock
  - Actions answer 204; the footer, the save report and the engine status all arrive on the one page stream
  - Config paths now resolve against the config file, once, in `config.Load` — the engine and Forge had disagreed about which file a `game.toml` meant

- [x] **SCHEMA mode** — Component list, `v<N>` schemaVersion badge, shape cycling, fields table, behavior binding, reserved-`Behavior`-name enforcement.
  - Renders in authored order throughout; selection is a URL so it survives a reload
  - Mode content joined the page stream — an edit was previously invisible until a reload

- [x] **Generated-SQL panel** — Live `CREATE TABLE comp_*` via `storage.MigrateComponent`.
  - Fix first: `componentTableBuilder.go` iterates the properties map unsorted, so column order is non-deterministic and a live preview visibly reshuffles
  - Since Epic 11, the fix is authored order via `Component.PropertyOrder`, not sorting — deterministic *and* it reads like the file

- [x] **Migration framing** — `schema.Diff` against the live DB shape, rendered as the migration warning; `MigrationConfirm` policy surfaces destructive statements in a confirmation dialog.
  - The engine migrates rather than refusing, and only when `schemaVersion` changes — the panel says both
  - The preview is recomputed per render, never cached: it depends on a database another process is writing

- [x] **ENTS mode** — Type list and editor: primary behavior, required/optional component chips, validation level, allow-extras, read-only CONTEXT SEEDS panel.
  - `Config.Machines` had never been wired, so the behaviour dropdown had been empty since Story 2
  - Components and entity types need separate rename namespaces — the two halves may legitimately share a name

- [x] **Usage panel** — Spawn and live-instance counts; degrades gracefully with no database attached.
  - A component's count is its own `comp_*` rows, not the population of the types declaring it — those differ whenever it is optional
  - Spawn counts are absent with a reason rather than zero; zero would be indistinguishable from an answer

- [x] **Inline validation** — `ValidateSchema` + `ValidateBehaviorRefs` (which gets its first caller in the codebase), plus a live warning when a context key matches two components' fields.
  - `ValidateSchema` returns one error for a whole file; narrowing carriers recover which owner and how many, without re-deciding what is valid
  - A missing machine file blocks the save; a file that is present but did not load is a warning naming the reason

---

## Epic 13: Forge — AGENTS mode

**Refined into stories:** See [`docs/stories/epic-13/`](stories/epic-13/).

Visual authoring of behavior machines that round-trips with Stately Studio and hot-swaps into a running game. The statechart canvas is one of only two hand-written client-JS surfaces in Forge; everything else is Datastar-driven hypermedia.

The action/guard dropdowns are fed from `agent.Registry.Actions()`/`Guards()`, which already expose `ParamSchema` metadata and a written description per built-in. Those methods were written for exactly this and still have no caller.

Two checks against the code changed the shape (see the epic README for the probes). The round trip is **lossy today** — `ParseMachine` ignores unknown fields and `EmitMachine` writes only what it knows, so a save silently deletes a Stately export's `description`, `tags` and `meta` — which makes fidelity the first story rather than the last. And the proposed `behaviors/<id>.layout.json` sidecar would be loaded by `ScanDir` as a machine with an empty id, successfully and without an error.

- [x] **Round-trip fidelity** — Unknown fields survive parse → emit, so a Forge save stops deleting what it does not model. Decides where canvas layout lives.
  - Also preserves XState's several spellings of one thing — Stately writes the object form, so the epic's headline claim was failing on the input it names
  - Layout goes in each state's own `meta`: the proposed sidecar would be loaded by `ScanDir` as a machine with an empty id

- [x] **Machine editing session** — Many files rather than one: open, create, rename, delete, save, hot-swap. A machine's identity is the `id` inside the file, not its filename.
  - Saving is per machine: an invalid schema stops the engine, an invalid machine stops one entity, so the refusal is one file
  - The shell's SSE subscription carried `?component=` alone, so AGENTS re-rendered with no machine named and swapped under the user every tick

- [x] **Machine list & context manifest** — Resolved per mod with an `override` tag; manifest from `MachineDefinition.ContextManifest`, which is populated only when validation fully succeeds.
  - The manifest's two absences are different sentences: a machine that seeds nothing and one whose seeds are unknown are the same empty map, and the panel must not give the first answer when the truth is the second
  - Stranded work — held with unsaved changes but no longer resolving — could be reached by nothing, because every route validated its path against the resolved set

- [x] **Statechart canvas: rendering** — Nodes, edges, nesting and selection, server-rendered and patched down the page stream. No JS.
  - A dotted-path target was resolvable by the interpreter and refused by the validator, so a machine with a transition into a nested state could neither run nor be opened to be fixed; both now use one exported `FindState`, which also stopped resolving ambiguous targets in map order
  - The node layer covered the whole canvas and swallowed every click meant for the ground — invisible to `go test`, and the reason the browser suite exists

- [x] **Statechart canvas: direct manipulation** — Drag nodes, drag-port-to-connect, double-click to add, right-click menu. The JS owns pointer state and nothing else.
  - Only the two drags need JavaScript: `evt` is in scope in every Datastar expression, so double-click, both context menus and every menu item are ordinary hypermedia
  - The menu's backdrop covers the viewport and the menu is per-server, so one left open in another tab blocked this page's rail, list and menu bar until it was clicked away

- [x] **State inspector** — Name, entry/exit actions from `Registry.Actions()`, set-initial. Action names are chosen, never typed.
  - The catalogue is gated as the engine gates it: a project with no map is not offered `computePath`, because the engine would not register it and a machine using it would not load
  - A required parameter left empty is a warning and not a refusal — `ValidateMachine` never looks at parameters, so blocking the save would be a rule the engine does not have

- [x] **Transition inspector** — Event, target, `cond` from `Registry.Guards()`, and a param form generated from the registered schema.
  - One generated form, not two: actions and guards both carry `[]ParamSchema`, so Story 6's form took a scope and gained two more callers rather than a twin
  - Transition order is semantics — the interpreter takes the first transition on an event whose guard passes — so reordering is offered, and the panel says what the order means
  - The chart's edge id is positional, so renaming an event, reordering and deleting all move the selection; those three answer with an SSE redirect carrying where it went, because only the server knows and selection lives in the URL

- [x] **Inline validation** — `agent.ValidateMachine` returns every error at once, each already carrying the state and field it is about; render them against the node or edge that caused them.
  - `StateID` cannot name a node — it is the machine id plus the state's *leaf* name, so two states in different branches share one — so `ValidationError` gained a `StatePath` set where the error is made
  - The validator walked three maps, so the same broken machine reported in a different order each time; it now walks in the file's order, which a two-second stream needs and which sorting in Forge could not have given
  - Most state-level errors are unreachable through the UI by now: an action is chosen from the registry and a duration is refused by `ParseDurationMs`, so what the browser can still cause is a transition left pointing at a deleted state
  - `Field` cannot say whether an error is about a state or one of its transitions — an entry action and a transition action are the same action type on the same state — so `ValidationError` gained an `Origin` as well

---

## Epic 14: Tiled map & tileset formats

**Refined into stories:** See [`docs/stories/epic-14/`](stories/epic-14/).

An engine epic, and the largest prerequisite in the Forge sequence. The map is a bespoke TOML file of `.` and `#` characters with no tilesets, no layers and no spawns; Forge's MAP and TILES modes have no format to write to until this lands.

There is a subtler problem: `LoadMap` is a one-time bootstrap that skips tile *creation* if any `Tile` entity exists, so after first run the database — not the file — is the source of truth. A map edited in Forge would appear to do nothing, without an error to notice.

One bullet from the first draft is gone: "passability from tile properties" pointed at `TileGrid.Rebuild`, which reads `comp_tile.passable` from the database and has never seen a `'#'`. The character switch is in `LoadMap` and goes away with the TOML format. See the epic README.

- [x] **TMX/TMJ parser** — Multiple tile layers and layer folders, CSV, base64, base64+zlib/gzip and the XML tile form, objects, map properties.
  - A `<group>` is a layer folder, not a layer: dropping it loaded the map empty, with nothing to notice
  - Checked against the published format rather than against itself — four flag bits since Tiled 1.9, not three

- [x] **TSX tileset parser** — `.tsx` and `.tsj`, sheets and image collections, per-tile properties, and resolving a map's external references.
  - Two kinds of tileset, not one: a collection has no sheet, `columns="0"`, and sparse ids, so the sheet rules refused every one of them
  - `passable` is this epic's own convention and is named in one place; `Passable` says both whether a tile can be walked through and whether the tileset says at all

- [x] **Map re-import semantics** — Diff the file's tiles against `comp_tile` instead of skipping when tiles exist.
  - Recorded: the file wins for what it describes, the database keeps everything else, and the entity id survives either way
  - `world.Tx` had no update and no entity delete anywhere in the repo; both are this story's, along with `EntityService.InTx`
  - A misspelled `rows` key would have deleted the whole map — under a bootstrap that was harmless, under a diff it is 300 tiles and no error
  - `PRAGMA foreign_keys` is set once on a pooled `*sql.DB`, so `ON DELETE CASCADE` is enforced on one connection and no other — measured, reported, and worked around rather than relied on

- [x] **The loader writes tiles from a parsed map** — `LoadMap` takes a Tiled map; `passable` comes from a tileset property rather than a character, and the topmost non-empty tile decides its cell.
  - Recorded: the topmost non-empty tile in a cell *is* the cell's tile, and it decides both `passable` and `tile_type` — `comp_tile` holds one row per cell, so the choice is forced
  - A hidden layer still counts; a tile whose tileset says nothing is passable, with a tileset-level property as the per-set default
  - Story 1's XML reader hoisted `<group>` folders to the end, so the two serialisations of one map disagreed about which tile was on top — fixed here, because this story's rule is a layer-order rule
  - A Tiled map with no declared size imported as zero cells and deleted every tile: the `rowz` bug again, in the format that replaces it
  - A collection tileset's ids are not consecutive, so judging membership by `tilecount` refused its last tile and accepted its holes

- [x] **Tileset rendering** — Draw tile layers from tileset images rather than colouring by `tile_type` string.
  - Drawn from the parsed map, not the database: `comp_tile` holds one row per cell and cannot express a stack, and a stack is most of what layers are for. No schema change and no migration — and nothing in the database can alter a drawn pixel any more, which is the trade
  - Everything decidable lives in an untagged `DrawList`, because the renderer is behind `//go:build ebitengine` and a test can never compile it
  - Found by review, all of them parsed-then-silently-ignored: a map with no pixel tile size drew every tile in one stack off screen; `renderorder` and `opacity` were dropped; a source rectangle past the end of its image drew nothing; a diagonally flipped non-square tile landed a row out
  - `make lint` now runs both tag sets, which is how two findings had sat unseen in the half `golangci-lint run` does not compile
  - Not verified on screen when it landed, because the only map the engine loaded was still the character format. Story 7 migrated it

- [x] **Object-layer spawns** — Entity type + component overrides at startup, replacing `ensurePlayerEntity`/`ensureGoblinEntity`.
  - Identity is an engine-owned `spawns` table keyed `(map, object_id)`: an object has none of its own, because the entity it made has moved or died by the time the file is re-read. Create once, never update, never delete — the file says where a world starts, not what it is
  - `ON DELETE SET NULL`, not `CASCADE`: the row is a fact about the import and stays true after the goblin dies
  - An object with no class is not a spawn, so an object layer can hold markers and bounds; a spawnable type must declare `Position`, and a property setting it is refused
  - Found by review: a duplicate object id left an entity no spawn row pointed at and then hid it forever; entity and record were two transactions; `validationLevel: "warning"` built half an entity in silence; pixel→cell truncated instead of flooring and checked no bounds
  - Not verified on screen when it landed — the configured map was still the character format. Story 7 migrated it

- [x] **A spawn is re-imported like a tile** — supersedes the rule above: the file wins for what the file describes, so an edit in the editor lands in the world.
  - Story 6 said create-once-never-touch and argued it as "an entity stops being the file's the moment the game runs" — which is the sentence `SyncTiles` had already rejected with "corridor" in place of "goblin". People editing map files are making a game, not playing one
  - An object that moved moves its entity, a changed property is re-applied, an object removed from the map deletes its entity, and the entity id survives an update so a running machine keeps pointing at it
  - What the file says nothing about is left alone — the update is partial
  - Found by review: no migration, so every existing database was permanently broken; the update path validated nothing, so the same file made a different world depending on history; "`Tx` has no reader" was a rationalisation for not diffing, and an unedited map wrote six times per load; a scalar component could never be given a value at all

- [x] **A map says which map it is** — spawns are keyed by a `mapId` map property rather than by the file's path.
  - A path is not an identity: renaming or moving a level spawned its world a second time, and the story above made the originals unreachable, because deletion is scoped to the map being loaded
  - A map that gains an id adopts the rows it had under its path, or adding the property would be another rename
  - A map with no id still works and is warned about, because the trap only springs later
  - The engine cannot tell two maps sharing an id from a rename — it sees one map at a time. Forge can, across a project

- [x] **Entity-type `behavior` honoured at spawn** — Removes `ensureGoblinBehavior`; `Goblin` declares `"behavior": "goblin"` in `schema.json`.
  - A load-time reconcile rather than part of creating the entity, and recorded as a choice: a machine's entry actions may be pathfinding actions, which do not exist until the registry has the map's grid — but only `SyncTiles` feeds that grid, so the ordering does not forbid a create hook. What decided it is that an existing database gets its machines, a crash between the two recovers, and nothing below has to learn about machines
  - Found by review, and new surface this story opened: `StartAgent` seeds its context by attaching components through the raw storage port, which validates nothing — so a binding could give an entity a component its own type forbids, written by the engine, in silence
  - It never stops a machine, and that covers retargeting a binding *and removing one*. The composite key exists so an entity may run machines nothing bound it to, so from here the two cannot be told apart

- [x] **Migrate `level1.toml` → `level1.tmx`** — Plus a starter tileset and a `game.toml` update.
  - The character-format reader went with the file it described, and `tiled.LooksLike` with it: `Parse` refuses a file that is neither serialisation better than a boolean can. That made the renderer's colour fallback dead in effect — a map with no tileset imports zero cells, so it could only ever have painted an empty picture
  - The epic's claim is now asserted from the shipped files, through `game.toml`: every other test in it runs against a fixture written for it, and each can pass while the game is broken
  - Found by review: the built-in default map path still named the deleted file, so a run with no `game.toml` failed on a missing map; and the test claiming to load "the files this repository ships" hardcoded them, so reverting `game.toml` left it green

---

## Epic 15: Forge — MAP mode (AUTHORED)

**Refined into stories:** See [`docs/stories/epic-15/`](stories/epic-15/).

The core map editor: layers, tile painting, spawn placement and spawn editing. The paint canvas is the second and last hand-written client-JS surface.

This epic's stated engine touchpoint was "Epic 14's TMX writer path". **There is no writer** — Epic 14's nine stories are all read-side, and `tiled.Map` is a reading model that drops most of a real Tiled file: `nextobjectid`, image layers, `<group>` folder nesting, object polygons and text, layer offsets and tint, `<editorsettings>`. Emitting from it would hand an author back a mangled file, and losing `nextobjectid` silently retargets a live entity, because `spawns` is keyed `(map, object_id)`. That is Story 1, for the reason Epic 13 made round-trip fidelity Story 1: every story after it ships a save button.

Two more corrections. A spawn cannot name a behaviour — `SyncBehaviors` binds by entity *type* from `schema.json` and no object property feeds it, so the prototype's per-spawn dropdown would write a value nothing reads. And saving a map does not hot-reload: the watcher watches behaviours directories, `animations.toml` and the sprites directory, so a map takes effect on the next `ecs-db run`. See the epic README.

- [x] **Push, not poll** — Mutations publish to an event bus and the open pages are told immediately, instead of finding out on the next tick of a two-second poll.
  - Measured before changing anything: the server answered in 2–9ms while an edit took ~1s on average to appear, because nothing pushed — every mutation route answered 204 and waited for a timer
  - Every navigation rendered the page twice: the browser parsed 79KB and the stream then morphed `<main>` into a byte-identical copy of itself (76.5KB). A page that has just rendered now receives nothing
  - The bus is copied from `pkg/eventbus` in `tmbritton/fancykaraoke-go` to stay source-compatible until it is extracted into a shared library. Its `Publish` deadlocks on the subscriber after a slow one, via the pre-Go-1.23 timer drain; it also had no `Unsubscribe`, which Forge needs because it subscribes once per page load
  - Found by review: loading a page resets three fields every *other* page renders, and is a GET, so it was the one mutation that told nobody — leaving a second tab's save confirmation on screen with buttons that no longer did anything
  - Found by review: the poll had been acting as a file watcher by accident, so removing it stopped Forge noticing a map added by Tiled or a `schema.json` rewritten by git. The poller now fingerprints the project's files deliberately
  - A mode is a list of disjoint regions now, each with the id the stream patches. MAP's canvas is 70KB of a 75KB page, so a refusal banner used to re-morph three hundred cells to show one line of text; it costs 1,882 bytes
  - MAP's four view controls — zoom, the tile in hand, the active layer, which layers are drawn — are Datastar signals and cost no request at all. Not a preference: a page's SSE subscription is fixed at load, so anything the server renders from it is frozen there and the next unrelated event undoes whatever changed since
  - Found by review: the zoom transform sized its box from the already-zoomed one, so the canvas panel scrolled eight times too far at 8× on a map that looked perfect
  - The statechart stopped causing page loads too: selecting a node or an edge, clearing a selection, and the two edits that answered with a redirect. Selection could not be a signal — it decides what the *inspector* renders — so the server keeps a per-page record, keyed by an id in the page's own signals
  - Found by review: treating a stream close as a page ending bricked the tab. Datastar aborts the stream when the tab is hidden, so alt-tabbing away and back left every later click answering 204 and changing nothing, for the life of the tab, with nothing logged
  - Found by review: a CSS reset landed after the statechart's own rules and, every selector being a single class, won on source order — edge labels lost their font, box and colour, so selecting one changed nothing visible while `data-selected` stayed correct and the suite stayed green
  - `@view-transition` for the page loads that remain, and deliberately not for the SSE patches: an edit lands in ~20ms and animating it would put the latency back

- [x] **TMX writer & round-trip fidelity** — A writer that preserves everything Forge does not model, byte-for-byte, and maintains `nextobjectid` so an id is never reused.
  - A copy-on-write tree, not an emitter: every element keeps its source bytes and is written verbatim unless an edit reached it, so what this package does not model survives without it knowing the thing exists
  - Painting one cell of the shipped level changes exactly one line of the file, comment and all
  - Found by review: a `nextobjectid` that had fallen behind was believed, and handed out an id already in use — which under `spawns`' `(map, object_id)` key moves a live entity rather than creating one, and needs no hand-editing to happen, only a git merge
  - Found by review: `xml:space` lost its prefix on the root, which every edit re-renders

- [x] **Map editing session** — Which maps a project has; open, save, discard, reload, conflict. `project.Project` did not carry the map path at all.
  - The configured map is marked, because a project can hold five maps and `ecs-db run` reads exactly one — editing another is real work that changes nothing about the game until `game.toml` says so
  - Tilesets resolve on demand and are never held: a `.tsx` is shared with Epic 16's TILES mode and with Tiled in another window, and a tree kept here has no signal to invalidate on
  - A map that vanishes with unsaved work is kept and reported, because work in no tab is reachable from nothing
  - Found by review: the footer's Save and Discard acted on the configured map whatever map was on screen — the confirm dialog naming one file while the request destroyed another's work
  - Found by review, and older than this story: a conflict's "Use theirs" posted to `/forge/schema/reload` whatever the conflict was on, so resolving a map conflict discarded unsaved schema work

- [x] **The map renders** — Tile layers drawn from tileset images, layer panel in file order, tileset palette, map tabs, and the AUTHORED/LIVE/REPLAY lens with the two later lenses disabled.
  - `tiled.DrawList` became a projection of a new `Placements`, so the editor draws a cell the engine skips — an unresolved gid you cannot see is one you cannot fix — from the one walk that decides layer order
  - Found by that refactor: a refused cell was handed whichever reason was last in an aggregated list that keeps one entry per *distinct* reason
  - Forge's first route serving a project file is an allow-list of what the project's tilesets name, plus a containment check. Found by review: the allow-list alone stops the *request* traversing and not the *project* — a `.tsx` naming a symlink out of the tree served `/etc/passwd`
  - Found by review: a layer hidden in Tiled had an eye you could click that changed the URL and nothing else, so the one place you would look at a hidden layer could not show it
  - The e2e fixture draws real CC-BY art now, so the browser suite exercises a sheet's rows and columns rather than two generated colours
  - Zoom, added after the story shipped: the fixed scale it shipped with was chosen for 16px art and drew the engine's own 32px map at 1920×1440. Deferring the control to Story 5 was the wrong call — a canvas you cannot see the map on is not a canvas

- [x] **Painting, server-side** — Stamp, rect, eraser, rotate and flip as operations on the session, proven in Go against map values.
  - Tool state is not the server's, against the story's own wording: a page's SSE subscription is fixed at load, so the tool, the tile in hand and the stamp's orientation are signals the browser sends with each stroke
  - Found by review: the quarter turn handled the four unmirrored orientations and sent every mirrored one back to upright, so mirroring the stamp and then turning it threw the mirror away. Every rotation test started from upright, which is the orbit that worked
  - Found by mutating the tests: the one test claiming to check the rotation "against what the renderer draws" asserted only the translation, which cannot tell a rotation from a reflection — it passed against the wrong cycle as readily as the right one
  - The eight flag combinations are the eight symmetries of a square and a quarter turn permutes all of them, so the fix is a closed form with no default branch to fall through. The orientation labels are generated by walking it rather than written out, after two of them turned out to be swapped
  - A no-op is detected in the domain, not left to the file layer: stamping the tile a cell already holds must not dirty the map, or the footer stops meaning anything

- [x] **Painting, the pointer surface** — The second hand-written JS file, on `canvas.js`'s contract: pointer state only, one event per stroke, no model of the map.
  - Found while wiring the pointer: a stamp drag was the rectangle its two ends span, which is true of a straight drag and false of every other one — so a diagonal across four cells painted sixteen, and the fill tool beside it did exactly the same thing. A stroke now carries the cells the pointer went through, and `Fill` is the one tool that still means the rectangle
  - Every cell of a trail is bounds-checked before anything is written. Checking the two ends is what a rectangle needs; a trail is a list, and nothing about where it started and finished bounds what is between them
  - The joining of skipped cells is the client's, and it is the one place the split bends: a pointer sampled at frame rate misses cells whenever it moves faster than one per frame, and joining on the server would make the request describe where the pointer *was* rather than what to paint — and leave its length unbounded
  - The ghost and the marquee are server-rendered and positioned by the module through custom properties. "Show the rectangle" needs two facts from opposite sides — the pointer knows a drag is happening, the signal knows the tool is fill — so CSS is where they meet
  - Found by mutating the tests: the trail test used `0,0,1,1`, which is symmetric, so reading the coordinate pairs y-first was invisible to it
  - Found by screenshot: the ghost rendered correctly and could not be seen. Everything in the canvas box is positioned with no z-index, so tree order decides what covers what — and the ghost has to come before the scaled box to escape its transform and keep a 2px border at 2px. The grid had been under the tiles since Story 3, which on a fully tiled map means not drawn at all; the toggle added here is what made it noticeable. Nothing that reads the DOM can catch this, so three tests compare pixels
  - Found by the cleanup's own self-check: a test that painted and ended without waiting raced its own stroke, because the dirty flag it was cleaning up after arrives on the stream. It passed twice before failing
  - Two of the prototype's controls are deliberately absent: Select (M) has no tool behind it and no server operation a selection could mean, and Snap has nothing to snap until spawn objects arrive with pixel coordinates in the next story

- [x] **Spawn placement** — Drag from the entity-type palette; move and delete. An object id is a spawn's identity, so allocation is a correctness concern, not a formality.
  - An object group is selected explicitly. Native drag-and-drop carries an opaque type or object id; the existing pointer module translates the drop to a cell, and the server writes the TMX document, not a browser-side model
  - Moves edit only the coordinates of the existing object and preserve its id, its properties and unknown XML; deleting does not recycle the id. A tile object's bottom-left origin stays consistent with the engine importer
  - Required non-Position components are seeded with the generated SQL columns' zero values, so a newly placed Goblin passes the engine's import rather than becoming a visible but refused spawn. A required entity reference has no valid automatic target and is shown as unspawnable
  - Selection is in the URL and the page's stream subscription, so a patch does not clear the inspector. Saving the map takes effect on the next `ecs-db run`, as the mode says
  - Verified: the saved TMX's object is imported by the engine's spawn path into a fresh database at the cell shown in Forge; `make e2e` passes 265 browser checks

- [x] **Spawn inspector** — Components with required locks and `ƒ ctx` badges, property editing as `Component.property`. Behaviour is read-only and comes from the entity type; `Position` comes from where the object sits.
  - The inspector lives in its own patch region beside the map, not underneath it. The engine's property parser and `world.ValidateEntityCreation` judge a candidate before its XML is touched; strict refusals leave it unchanged and warning-level edits are shown and allowed
  - Entity-ref components ask for a target id, missing required components can be repaired together, and map property spellings preserve XML fidelity. Context seeds attach only missing components: existing map values win at startup
  - Verified through a saved TMX imported by the engine's spawn path; `make e2e` exercises the field change, optional attach/detach, context badge and warning/refusal paths
  - Re-import now remembers the components each object authored. Removing an optional component in Forge removes its existing entity's row on the next load without deleting a component attached only at runtime; old databases take a non-destructive baseline before this rule applies

- [x] **Context menus** — Layers, tiles and spawns, on Epic 13's server-state menu pattern.
  - Rename, reorder and delete preserve unknown TMX; moving across layer folders is refused, stale targets cannot edit another layer, and deleting warns that every tile the layer contributed goes away on the next game load. Reorder/delete require unique positive Tiled layer IDs so a view cannot silently retarget after an index shift
  - The layer eye is view-only, the canvas picks the topmost visible tile into the stamp, and a spawn duplicate clones its whole XML subtree with a fresh id. Empty cells and entity types have no empty menus
  - Verified with `make test`, both lint tag sets, both builds and 287 passing browser checks; statement coverage: tiled 94.2%, Forge server 86.8%, mode templates 70.7%, map canvas 94.3%

- [x] **Inline validation** — Engine-backed cell, layer, spawn and tileset findings appear together against their authors; a project-wide duplicate `mapId` names both maps.
  - Missing map identity warns in the engine's words; mapId edits preserve TMX and clear the warning without navigation. Partial previews keep valid cells and spawns visible even when other tilesets fail
  - Strict/warning entity contracts and property spellings use the engine's validator, duplicate object IDs mark both claimants, and a broken map can still be saved. Malformed layer shapes are named by the parser in project problems before a canvas can open
  - Forge now marks tile layers with missing, nonpositive or duplicate IDs and explains why those rows cannot reorder/delete, including in newly generated maps; both duplicate claimants are marked
  - Verified with `make test`, both lint tag sets, both builds and 297 passing browser checks; 4×50×50 validation ~6.1 ms/iteration; statement coverage: tilemap 91.6%, tiled 94.3%, Forge maps 88.8%, mapvalidation 95.5%, server 86.8%, mode templates 70.3%

---

## Epic 16: Forge — TILES & SPRT modes

Entity-backed tile rendering, occupant-aware grid traversal and its MAP
authoring surface, followed by tileset metadata and sprite-sheet slicing.

Two claims here were checked while planning Epic 15 and are false. **There was no TSX writer** — Epic 16 Story 1 added one to preserve wangsets, terrains and per-tile collision object groups even though the initial map does not use polygons. And **the engine does not hot-reload tilesets**: the watcher covers behaviours directories, `animations.toml` and `mods/*/assets/sprites`, which is where SPRT writes and is not where a tileset lives. SPRT hot-reloads; TILES takes effect on the next `ecs-db run`, and the mode must say so. Refined into eleven ordered stories in [`docs/stories/epic-16/`](stories/epic-16/); before TILES authoring, Story 2 makes every authored tile an entity rendered from database state, and Story 3 replaces universal Boolean passability with a rule over the mover and entities occupying each destination cell. Only the first assets mod's `animations.toml` is game-loaded, and sprite frames are a one-row horizontal strip of `window.tileSize` squares.

- [x] **Lossless TSX writer** — External `.tsx` files can now be opened as editable documents without discarding unknown XML, per-tile collision shapes, animations, wangsets or terrain. Sparse collection IDs and both class spellings survive surgical tile edits; `.tsj` and embedded tilesets stay read-only. `make test`, both lint tag sets, both builds and 295 browser checks pass; `internal/tiled` statement coverage 94.3%.

- [x] **Entity-backed tile layers** — Every nonempty authored layer tile is now a stable entity, keyed by `(mapId,layerID,cell)` and rendered from database components instead of a static TMX snapshot. Story 3 subsequently moved `TileVisual` onto referenced entities and retired the old topmost Boolean grid. Story 2 checks passed with 299 browser checks and `internal/tilemap` coverage 91.2% at that checkpoint. See [Epic 16 Story 2](stories/epic-16/02-entity-backed-tiles.md).

- [x] **Occupant-aware traversal and visibility** — The Boolean Tile grid is retired. Independent schema-declared capabilities and categories control movement and sight. Each placed Tile has Position and entity references; referenced Wall/River entities occupy their referencing Tiles' cells, including one River shared across irregular cells. Unreferenced runtime entities use their own Position/OccupiedCells. Tile artwork lives on referenced entities. `make test`, tagged renderer tests, both builds, both lint tag sets and 300 browser checks passed; `internal/tilemap` coverage 88.0%, `internal/schema` 92.2%. See [Epic 16 Story 3](stories/epic-16/03-occupant-traversal.md).

- [ ] **MAP occupant authoring** — Painting a Tile also creates/links the referenced entity instances. Inspect and edit a Tile's references, including sharing one River among Tiles at irregular cells; movement and art placement stay in sync. No polygon collision authoring (Epic 16 Story 4).

- [ ] **TILES mode** — Tileset grid and enlarged tile with class/type and art metadata. The TSX writer is ready; movement restrictions are authored on occupant entities in MAP, not on tile artwork. No polygon collision authoring or global `passable` toggle. Tiled tile animation and collision polygons have no engine consumer yet.

- [ ] **SPRT mode** — Slice a sheet, author named animations, write `animations.toml`.
  - Constrain the UI to what the renderer supports: 1×N horizontal strips of `tileSize` squares, frames as column indices. Multi-row grids are renderer work.

- [ ] **Import Sprite Sheet dialog** — Wired to the slicing flow.

---

## Epic 17: Forge — dialogs & preferences

The modal set, each computing its live derived values, backed by a persisted preferences store.

- [ ] **Map dialogs** — New Map, Open Map, Map Properties, Resize Map (9-point anchor + clip warning).

- [ ] **Asset dialogs** — Add Tileset (live tile-count grid). Import Sprite
      Sheet belongs to Epic 16 Story 11, with the renderer's one-row frame
      constraint rather than a duplicate multi-row dialog here.

- [ ] **Shell dialogs** — Keyboard Shortcuts, Preferences, About.

- [ ] **Preferences persistence** — Written to disk, restored on launch.

---

## Epic 18: Forge — LIVE inspector

The LIVE source lens: attach read-only to a running game and inspect it. This is the graphical debugger the Epic 6 read layer was built for, and the first place Forge touches the database rather than files.

Forge writes nothing here. The one-writer-per-table contract is honoured by construction — a read-only connection, asserted in a test.

- [ ] **Read-only attach** — Via `internal/inspect`; `schema_version` gate; the DB path comes from `cfg.Database.Path`.

- [ ] **`world_version`-gated SSE streaming** — The watermark has had a producer since Epic 5 and no consumer; this is the consumer.

- [ ] **Live map overlays** — Entity positions, hp labels, aggro radii; paint tools gated off; tick readout with step/pause.

- [ ] **Live debugger inspector** — Statechart with the active state lit, plus a filtered transitions history list.

---

## Epic 19: Forge — REPLAY & time-travel

The REPLAY source lens: scrub the `transitions` log with checkpoints, breakpoints and forking. The headline capability of the architecture, delivered as an authoring-tool feature.

- [ ] **Timeline dock** — Transition-density waveform, scrub, checkpoint diamonds, breakpoint markers, playhead.

- [ ] **Fork from tick** — Branch a new session from any point in the log.

- [ ] **Transition history navigation** — Click a transition to jump the timeline; entity trails on the canvas.

---

## Epic 20: Forge — query layers

Saved SQL predicates rendered as map overlays — `hp < 20% · pulse`, aggro ranges — toggled like any other layer. Debug views authored in SQL rather than compiled in.

- [ ] **Query layer authoring** — Named, parameterised, read-only predicates with per-layer styling.

- [ ] **Overlay rendering** — Matching live entities highlighted on the map canvas.

---

## Deferred / not yet epics

Listed here so they aren't forgotten, but explicitly out of scope until earlier epics are real:

- **Extract renderer process** — The "database as contract" convention within the monolith can be promoted to an enforced process boundary if a concrete need surfaces (different graphics language, crash isolation requirements, multi-renderer). The architecture supports it; it is not currently scheduled.
- WASM browser deployment (interpreter to wasm32, SQLite-WASM, JS renderer like Phaser)
- Lua actions and guards — extend the action library for mods without WASM; registry already designed for this (`LuaActionHandler` implements `ActionHandler`)
- Networked multiplayer via replicated `event_queue` and deterministic lockstep
