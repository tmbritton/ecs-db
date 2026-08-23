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

- [ ] **Statechart canvas: direct manipulation** — Drag nodes, drag-port-to-connect, double-click to add, right-click menu. The JS owns pointer state and nothing else.

- [ ] **State inspector** — Name, entry/exit actions from `Registry.Actions()`, set-initial. Action names are chosen, never typed.

- [ ] **Transition inspector** — Event, target, `cond` from `Registry.Guards()`, and a param form generated from the registered schema.

- [ ] **Inline validation** — `agent.ValidateMachine` returns every error at once, each already carrying the state and field it is about; render them against the node or edge that caused them.

---

## Epic 14: Tiled map & tileset formats

An engine epic, and the largest prerequisite in the Forge sequence. The map is a bespoke TOML file of `.` and `#` characters with no tilesets, no layers and no spawns; Forge's MAP and TILES modes have no format to write to until this lands.

There is a subtler problem: `LoadMap` is a one-time bootstrap that skips entirely if any `Tile` entity exists, so after first run the database — not the file — is the source of truth. A map edited in Forge would appear to do nothing.

- [ ] **TMX/TMJ parser** — Multiple tile layers, CSV and base64/zlib encodings, map properties.

- [ ] **TSX tileset parser** — Image reference, tile size, margin/spacing, per-tile custom properties for collision / terrain / class / animation.

- [ ] **Passability from tile properties** — `TileGrid.Rebuild` stops keying off `'#'`.

- [ ] **Map re-import semantics** — Diff the file's tiles against `comp_tile` instead of skipping when tiles exist.

- [ ] **Tileset rendering** — Draw tile layers from tileset images rather than colouring by `tile_type` string.

- [ ] **Object-layer spawns** — Entity type + component overrides at startup, replacing `ensurePlayerEntity`/`ensureGoblinEntity`.

- [ ] **Entity-type `behavior` honoured at spawn** — Removes `ensureGoblinBehavior`; `Goblin` declares `"behavior": "goblin"` in `schema.json`.

- [ ] **Migrate `level1.toml` → `level1.tmx`** — Plus a starter tileset and a `game.toml` update.

---

## Epic 15: Forge — MAP mode (AUTHORED)

The core map editor: layers, tile painting, spawn placement and spawn editing. The paint canvas is the second and last hand-written client-JS surface.

- [ ] **Layer panel & tileset palette** — Visibility toggles, active row, tile selection.

- [ ] **Paint canvas** — Stamp / rect / eraser / select, rotate and flip, grid and snap toggles.

- [ ] **Spawn placement** — Drag from the entity-type palette onto the canvas; serialized as TMX objects.

- [ ] **Spawn inspector** — Behavior dropdown, component list with required locks and context-seed badges, attach/detach.

- [ ] **Context menus** — Layers, tiles, spawns.

---

## Epic 16: Forge — TILES & SPRT modes

Tileset metadata authoring and sprite-sheet slicing. Both write formats the engine already hot-reloads.

- [ ] **TILES mode** — Tileset grid and enlarged tile with Collision / Animation / Terrain / Class tabs, written as TSX per-tile properties.

- [ ] **SPRT mode** — Slice a sheet, author named animations, write `animations.toml`.
  - Constrain the UI to what the renderer supports: 1×N horizontal strips of `tileSize` squares, frames as column indices. Multi-row grids are renderer work.

- [ ] **Import Sprite Sheet dialog** — Wired to the slicing flow.

---

## Epic 17: Forge — dialogs & preferences

The modal set, each computing its live derived values, backed by a persisted preferences store.

- [ ] **Map dialogs** — New Map, Open Map, Map Properties, Resize Map (9-point anchor + clip warning).

- [ ] **Asset dialogs** — Add Tileset (live tile-count grid), Import Sprite Sheet (frame count, grid, loop duration).

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
