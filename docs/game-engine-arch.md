# Architecture: An ECS-in-SQLite game engine with declarative behaviors

## Thesis

SQLite is the game state substrate. Game behavior is JSON state machines (XState-compatible) attached per-entity. The engine ships as a single Go binary — interpreter and Ebitengine renderer in the same process — with the debugger as an optional, separate read-only process.

The engine follows the Dwarf Fortress tradition: simulation-first, presentation-as-a-view. Headline capabilities that fall out for free: **trivial save games** (the database file is the save), **time-travel debugging** (replay any session from the `transitions` audit log), **fully inspectable state** (SQL queries against live game data), **moddable with a text editor** (schema and behaviors are JSON), and **clockwork determinism** (every transition recorded, every guard result stored).

The database-as-contract discipline is maintained within the monolith by convention — interpreter code writes world state, renderer code reads it — rather than enforced by process boundaries. Process separation is reserved for the debugger, where it genuinely earns its keep: read-only, different lifecycle, optional in shipped builds, and accessible remotely.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  Game Process (Go binary)                               │
│                                                         │
│  Interpreter (state machines, sole world writer)        │
│      │ Update() writes world state                      │
│      ▼                                                  │
│  Renderer (Ebitengine, reads entities + components;     │
│            writes input_events synchronously)           │
│      │ Draw() reads from same SQLite connection         │
│      ▼                                                  │
│  shared SQLite handle                                   │
└──────────────────────────────┬──────────────────────────┘
                               ▼
                    ┌─────────────────────┐
                    │   world.sqlite      │
                    └─────────────────────┘
                               ▲
                               │ read-only
                    ┌─────────────────────┐
                    │   Debugger          │
                    │   separate process  │
                    │   HTTP + web UI     │
                    └─────────────────────┘

┌─────────────────────────┐
│  mods/                  │
│   ├── schema.json       │ ──loads──► Interpreter
│   └── behaviors/*.json  │
│  hot-reloaded           │
└─────────────────────────┘
```

## The data model: ECS via schema.json

The engine implements an Entity-Component-System architecture where the data model is declarative. A single `schema.json` file defines the components and entity types of the game. The interpreter derives the SQLite schema from this file and enforces validation against it.

### Components

A component is a strongly-typed piece of data that can be attached to an entity. Components are declared in `schema.json` with their data shape. The interpreter generates one SQL table per component type at startup, with typed columns matching the declaration. Components without declarations cannot be attached.

### Entity types

Entities are unique identifiers with no inherent data. Their structure comes from the components attached to them. To make games readable and catch bugs early, the engine uses entity types — named templates that declare which components an entity of that type must have, which it may have, and whether additional components are permitted.

An entity declares its type at creation. The interpreter validates that required components are attached, and that no disallowed components are present, according to the type's validation rules.

This is a deliberate departure from pure ECS, which treats entities as free-form bags of components. Entity types add a small amount of structure that pays for itself in readability ("this is a goblin") and in catching schema mistakes at attachment time rather than at query time.

### Validation

Validation is **strict by default**: the interpreter refuses operations that violate the schema. A type can opt in to `allowExtraComponents` for flexibility, and a type's `validationLevel` can be set to `warning` to log violations instead of refusing them. Strict validation makes modder mistakes loud — a typo in a behavior file that tries to attach a nonexistent component fails immediately with a clear error, rather than silently producing broken entities.

### Schema versioning and evolution

`schema.json` includes a version number. The compiled SQLite database records the schema version it was created with. On startup, the interpreter compares the current `schema.json` version against the database's recorded version. Mismatches require an explicit migration (see _Schema evolution_ in future directions).

The schema is intended to evolve during development. Adding new components or entity types is cheap. Removing or renaming components is a migration. Modders evolve their own `schema.json` files alongside their behaviors; mod packs are versioned by the schema they target.

### Example schema.json

```json
{
  "schemaVersion": 3,
  "components": {
    "Position": {
      "type": "object",
      "properties": {
        "x": { "type": "number" },
        "y": { "type": "number" }
      }
    },
    "Velocity": {
      "type": "object",
      "properties": {
        "vx": { "type": "number" },
        "vy": { "type": "number" }
      }
    },
    "Health": {
      "type": "object",
      "properties": {
        "hp": { "type": "integer" },
        "maxHp": { "type": "integer" }
      }
    },
    "Sprite": {
      "type": "object",
      "properties": {
        "imageId": { "type": "string" },
        "frame": { "type": "integer" }
      }
    },
    "Inventory": {
      "type": "array",
      "items": { "type": "entity-ref" }
    },
    "Wielder": {
      "type": "entity-ref"
    }
  },
  "entityTypes": {
    "Player": {
      "requiredComponents": ["Position", "Health", "Sprite", "Inventory"],
      "optionalComponents": ["Velocity"],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    },
    "Goblin": {
      "requiredComponents": ["Position", "Health", "Sprite"],
      "optionalComponents": ["Velocity", "Wielder"],
      "behavior": "wandering_goblin",
      "allowExtraComponents": false,
      "validationLevel": "strict"
    },
    "Weapon": {
      "requiredComponents": ["Sprite"],
      "optionalComponents": ["Position", "Wielder"],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    },
    "Particle": {
      "requiredComponents": ["Position", "Sprite"],
      "optionalComponents": ["Velocity"],
      "allowExtraComponents": true,
      "validationLevel": "warning"
    }
  }
}
```

A `Weapon` exists in the world (has `Position`) when it's on the ground, or has a `Wielder` (no `Position`) when it's held. A `Goblin` declares `"behavior": "wandering_goblin"` — the interpreter activates that machine automatically when a Goblin entity is created. `"Behavior"` is a reserved name; schema validation rejects any user-defined component with that name.

## The contract

The SQLite schema is the IPC contract. Changes are versioned. Every process checks the schema version on startup and refuses to run against incompatible versions. The contract has five conventions:

**One writer per table.** Avoids SQLite write contention. The interpreter owns world state; the renderer owns input events; the debugger writes nothing. Multiple writers to the same table is the architectural bug.

**WAL mode plus busy_timeout.** Many concurrent readers, a brief wait on contended writes. The renderer reads while the interpreter writes; neither blocks the other for normal operation.

**A `world_version` watermark.** A single integer in the `world` table that increments on every world-state write. The debugger polls this cheaply on each refresh and skips the full query when nothing has changed. The in-process renderer has no need to poll — it draws after the interpreter writes within the same tick.

**JSON payloads for extensibility.** Structural fields (entity id, component type, event kind) are typed SQL columns. Per-feature data (event payload, machine context, action params) is a JSON column.

**Append-or-upsert, never destructive cross-process writes.** The renderer never deletes from `entities`. The debugger never writes anywhere. Each process's write surface is narrow and well-defined.

### Table ownership

|Table|Writer|Readers|Purpose|
|---|---|---|---|
|`meta`|init/migrate|all|Schema version, build info|
|`world`|interpreter|debugger|Current tick, world_version watermark|
|`entities`|interpreter|renderer (in-process), debugger|Entity registry with entity type|
|`comp_*`|interpreter|renderer (in-process), debugger|Component data (one table per component, generated from schema.json)|
|`behavior_components`|interpreter|renderer (in-process), debugger|Active machine state per entity; interpreter-managed, not in schema.json|
|`event_queue`|interpreter|(drained by interpreter)|Internal game events and scheduled `after` timers|
|`input_events`|renderer (in-process)|interpreter (drains, same tick)|Append-only input log; synchronous within the game process; useful for replay|
|`transitions`|interpreter|debugger, renderer (effects)|Audit trail of every state transition; source of truth for time-travel|

### Schema sketch

The SQLite schema is derived from `schema.json`. The fixed-shape tables (below) are the parts that exist regardless of game content. The `comp_*` tables are generated from the component declarations in `schema.json`.

```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;

CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
-- meta holds schema_version (must match schema.json), build info.

CREATE TABLE world (key TEXT PRIMARY KEY, value TEXT NOT NULL);
-- world holds current_tick and world_version (incremented on every write).

CREATE TABLE entities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL,          -- must match a type in schema.json
    created_tick INTEGER NOT NULL
);

-- Component tables are generated from schema.json. For the example above:
CREATE TABLE comp_position (
    entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
    x REAL NOT NULL,
    y REAL NOT NULL
);
CREATE TABLE comp_health (
    entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
    hp INTEGER NOT NULL,
    max_hp INTEGER NOT NULL
);
CREATE TABLE comp_sprite (
    entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
    image_id TEXT NOT NULL,
    frame INTEGER NOT NULL DEFAULT 0
);
-- ... one table per component declared in schema.json

-- behavior_components is interpreter-managed, NOT generated from schema.json.
-- Composite PK supports multiple concurrent machines per entity.
-- "Behavior" is a reserved name; user components with that name are rejected.
CREATE TABLE behavior_components (
    entity_id      INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    machine_id     TEXT NOT NULL,
    current_states TEXT NOT NULL,        -- JSON array of active state IDs
    updated_at     INTEGER NOT NULL,     -- tick
    PRIMARY KEY (entity_id, machine_id)
);

CREATE TABLE event_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tick INTEGER NOT NULL,
    target_entity INTEGER,
    kind TEXT NOT NULL,
    payload TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE input_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    received_at_ms INTEGER NOT NULL,
    kind TEXT NOT NULL,
    payload TEXT NOT NULL DEFAULT '{}',
    consumed INTEGER NOT NULL DEFAULT 0
);

-- The debugging goldmine. Every state transition is recorded with why.
-- Also the source of presentation effects (see Effects).
-- from_states/to_states are JSON arrays to support hierarchical and parallel states.
CREATE TABLE transitions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    tick        INTEGER NOT NULL,
    wall_ms     INTEGER NOT NULL,
    entity_id   INTEGER NOT NULL,
    machine_id  TEXT NOT NULL,
    from_states TEXT NOT NULL,          -- JSON array of active state IDs
    to_states   TEXT NOT NULL,          -- JSON array of active state IDs
    event       TEXT NOT NULL,
    cond_result INTEGER,                -- 1/0/NULL
    actions_run TEXT                    -- JSON array of action names
);
```

## Agents: behavior as data

Game behavior is defined per-entity by JSON state machines, called **agents**. Agent definitions conform to the XState v4 spec (excluding `invoke`): states, transitions, conditions (`cond`), actions, context, entry/exit actions, delayed (`after`) transitions, hierarchical states, parallel states, final states, and history states.

Agent definitions live in `mods/behaviors/*.json`. Agent files are valid XState v4 input and can be authored in Stately Studio — export from Stately and drop the file in; no manual editing required.

### Machines and entities

A machine maps 1:1 to an entity and defines all behavior for that entity. An entity runs one **primary machine** (declared on its entity type) plus zero or more **behavior-component machines** activated when certain components are attached.

`Behavior` is a reserved engine-managed concept. The interpreter maintains its own `behavior_components` table (not generated from `schema.json`) with a composite primary key of `(entity_id, machine_id)`, allowing multiple machines to run concurrently per entity.

### Component-machine binding

A component in `schema.json` can declare an optional `"behavior"` field naming a machine file:

```json
{
  "name": "Burning",
  "behavior": "burning",
  "fields": [
    { "name": "burn_damage",     "type": "integer", "nullable": false },
    { "name": "ticks_remaining", "type": "integer", "nullable": false }
  ]
}
```

When `attachComponent("Burning")` fires for an entity, the interpreter activates `mods/behaviors/burning.json` for that entity. When the burning machine reaches a final state, the interpreter detaches the component. Lifecycle is bidirectional. Components without `"behavior"` are pure data.

### Context as component manifest

The machine JSON's `context` block is the complete manifest of components this machine manages. Each context key is matched by field name to a component field in `schema.json`. At machine startup the interpreter attaches any declared component the entity doesn't yet have, seeded with the initial value from the `context` block. Components already present are left unchanged.

This means machine state is fully observable: there is no private context blob. Everything lives in component tables, readable by the renderer and debugger.

Cross-entity writes (e.g. `dealDamage` writing to a target entity's `Health`) use an explicit entity ID and are not declared in the context manifest.

### Example agent

```json
{
  "id": "wandering_goblin",
  "initial": "idle",
  "context": {
    "speed": 2.0,
    "aggroRange": 80,
    "target_x": null,
    "target_y": null
  },
  "states": {
    "idle": {
      "entry": [{ "type": "setTimer", "params": { "key": "patience", "ticks": 40 } }],
      "on": {
        "TICK": [
          {
            "target": "wandering",
            "cond": { "type": "timerExpired", "params": { "key": "patience" } }
          }
        ],
        "PLAYER_NEARBY": "pursuing"
      }
    },
    "wandering": {
      "entry": [{ "type": "pickRandomTarget", "params": { "radius": 100 } }],
      "on": {
        "TICK": [
          { "target": "idle", "cond": "atTarget" },
          { "actions": [{ "type": "moveTowardTarget" }] }
        ],
        "PLAYER_NEARBY": "pursuing"
      }
    },
    "pursuing": {
      "entry": [{ "type": "setPursueTarget" }],
      "on": {
        "TICK": [
          {
            "target": "attacking",
            "cond": { "type": "inRange", "params": { "distance": 16 } }
          },
          { "actions": [{ "type": "moveTowardTarget", "params": { "speed_mult": 1.5 } }] }
        ],
        "PLAYER_FAR": "idle"
      }
    },
    "attacking": {
      "entry": [
        { "type": "dealDamage", "params": { "amount": 5, "target": "$player" } },
        "playAttackSound"
      ],
      "after": { "500": "pursuing" }
    }
  }
}
```

### Actions and guards

Agents reference actions and guards by name (`"moveTowardTarget"`, `"timerExpired"`). The interpreter holds two registries — action handlers and guard predicates — populated at startup with built-in implementations. Actions and guards are implementation code, not data; they live in the interpreter source.

Each action receives: the current entity ID, the current tick, a domain-level world-write interface (`WorldWriter`) backed by the active transaction, the action's static params from the JSON, and the triggering event payload. Guards receive the same minus the write interface (they get a read-only `WorldReader`). Neither receives a raw machine context object — all state is read and written through the world interfaces, which map to component tables.

Built-in actions handle the common cases (move toward target, deal damage, spawn entity, attach/detach component, set timer, log). Built-in guards handle the common predicates (timer expired, at target, in range, has component, health above threshold). Game-specific actions and guards are registered by the host application before the interpreter starts.

The registry also stores metadata for each action and guard (description, parameter schema). This introspection surface supports tooling — a future visual editor can enumerate available actions and guards directly from the running interpreter.

Critically: **agents cannot execute arbitrary code.** They can only invoke actions and guards that have been registered. This makes them sandboxed by construction — no WASM isolation needed. A modder cannot write an agent that exfiltrates data, opens a socket, or crashes the engine.

## Component responsibilities

### Interpreter

The interpreter is the sole writer of world state and the only process that knows about state machines. It is a normal native executable in whatever language fits — Go for fast iteration and a simple build, Rust for stronger compile-time guarantees. Because its only external dependency is SQLite, the interpreter is portable: it compiles to native binaries on Linux, macOS, and Windows, and to WebAssembly if a browser deployment is wanted.

On startup the interpreter:

1. Loads `schema.json` and validates it. Rejects any component named "Behavior" (reserved).
2. Opens or creates `world.sqlite`. Generates SQL DDL from the schema if creating; checks `schema_version` on open. Creates interpreter-managed tables (`behavior_components`, `transitions`, `event_queue`) with `CREATE TABLE IF NOT EXISTS`.
3. Loads all `mods/behaviors/*.json` and validates them against registered actions, guards, and `schema.json` component fields. Reports errors loudly; skips invalid files.
4. For each entity type that declares `"behavior"`, ensures active entities of that type have a corresponding `behavior_components` row.
5. Starts the filesystem watcher for hot reload.
6. Enters its tick loop.

Its tick loop:

1. Drain unconsumed rows from `input_events`. For each, dispatch to a game-specific handler that translates raw input into game events.
2. Drain due events from `event_queue` (where `target_tick` ≤ current tick).
3. For each entity with an active machine in `behavior_components`, deliver a `TICK` event to each of its machines.
4. For each delivered event, run the SCXML microstep: evaluate guards, compute exit and entry sets, run exit → transition → entry actions, write new active states to `behavior_components`, append a row to `transitions`. All mutations in one SQLite transaction per event.
5. Advance the world tick. Bump `world_version`.

All mutations inside a single event delivery run in one SQLite transaction. Either the entity moves cleanly to its new state or nothing changes. Crash mid-tick and the database is consistent on restart.

### Renderer

The renderer is Ebitengine, running in the same process as the interpreter. Ebitengine's lifecycle provides natural sequencing: `Update()` runs the interpreter tick (state machine evaluation, world writes, input drain), then `Draw()` queries the database and renders the result. No polling watermarks, no cross-process coordination — the renderer sees a consistent post-update snapshot every frame because it reads after the write within the same tick.

The renderer is intentionally dumb. It does not know what an entity _means_ beyond its type and components. It knows the schema: "entities of type Goblin have Position and Sprite, draw the sprite at the position." Add a new entity type in `schema.json` and the renderer handles it generically (draw the sprite) or shows a placeholder until you teach it something specific.

User input is captured in Ebitengine's `Update()` phase and written to the `input_events` table before the interpreter tick runs. The table is an append-only log rather than a purely ephemeral queue — the record survives to support replay and audit. The interpreter drains and processes it in the same tick.

### Effects: presentation as a view of the audit log

Visual effects, sound effects, and other ephemeral presentation — screen shake, particle bursts, sound triggers, fades, flashes — are not world state. They are _observations_ of state transitions that have already happened. The renderer drives effects by polling the `transitions` table for rows newer than its last-seen ID and interpreting them.

This means effects are declared as part of agents, not as a parallel event channel. A transition with `actions: ["dealDamage", "playSwingSound"]` records both action names in `actions_run`. The interpreter executes `dealDamage` (which mutates health) and may execute `playSwingSound` as a no-op or skip it entirely — the action's presence in `actions_run` is what matters. The renderer reads `actions_run`, recognizes `playSwingSound`, plays the sound.

Three patterns for effects, in increasing power:

**Implicit effects from transition shape.** The renderer can play effects from generic transition data without explicit declaration: "whenever any entity transitions to a state named `dead`, play the death sound and spawn dust particles at its position." No annotation in behaviors needed.

**Named effect actions in the JSON.** When an agent author wants specific effects, they include named actions in the transition. The renderer reads `actions_run`, parses the names, plays the effects. The agent author controls presentation from JSON without the interpreter caring about presentation.

**Effect rule files.** A separate JSON file maps transition patterns to effects, loaded by the renderer at startup. Modders can retheme the game — different sounds, different particles — without touching agents or code.

The architectural property this preserves: the interpreter knows about game state; it does not know about presentation. Effects are the renderer's interpretation of the audit log. Multiple renderers see the same transitions and each makes its own presentation decisions — the 2D view animates a death, the terminal view prints `goblin died at (243, 119)`, the debugger logs it.

Effects that persist across frames (a 200ms screen shake) are renderer-local state: the transition triggers the shake, the renderer maintains intensity and time-remaining locally and decays over frames. The database has no business knowing about shake.

A "catch-up" policy handles the case where the renderer was paused (minimized window) and resumes with a backlog of transitions: if more than N seconds behind, the renderer skips ephemeral effects and just catches up world state. Renderer-side concern, doesn't touch the schema.

### Behavior library (in-process)

A small component of the interpreter, not a separate process. Watches `mods/behaviors/` for file changes and maintains an in-memory map of machine IDs to parsed agent definitions. Validates references to actions and guards against the registries at load time.

Behavior files with malformed JSON, references to unknown actions or guards, or transitions to undefined states log a warning and the file is skipped, leaving the previous version in memory. The game keeps running while a modder fixes their typo.

### Debugger

A separate HTTP server process — the one place process separation genuinely earns its keep. Read-only by construction, optional in shipped builds, different lifecycle from the game, and accessible remotely without any special client software.

Opens `world.sqlite` read-only. Polls `world_version` on each refresh; skips the full query when nothing has changed. Serves a web UI from a single binary with no build step for the frontend.

**Endpoints:**
- **Entities** — Roster of every entity with type and attached component summary.
- **Components** — Per-entity drill-down of full component data.
- **Transitions** — The "why did the goblin attack?" view. Most recent N, filterable by `entity_id`, `machine_id`, event type. Shows `from_states`, `to_states`, guard result, and `actions_run`.
- **Schema** — Current `schema.json` verbatim, for reference.
- **ASCII view** — Terminal-style live view: entity positions, types, and health rendered as text. Serves the multi-renderer use case (visual cross-check) without a second runtime process.

**Remote debugging** is trivial: tunnel the port over SSH or Tailscale and inspect a game running on another machine from a phone. No special client — it's a web page.

**Time-travel** is the headline investment: the `transitions` log is a complete audit trail of every state change since the game started. The debugger exposes a timeline scrubber that replays from any checkpoint database snapshot. Jump to tick 4,312, see exactly what the goblin was doing, then step forward. See _Milestone order_ for the full scope.

## Process supervision

In development, a Procfile-style supervisor (overmind, foreman, or systemd user units) manages two processes: the game binary and the debugger. Logs are interleaved with wall-clock timestamps. The debugger can be restarted independently without touching the game process.

In a packaged game, the launcher is simple: spawn the game binary. The debugger is an optional sidecar, off by default in shipped builds.

## Why this architecture is the move

**Trivial save games.** The database file is the save. Copy it and you have a save. Open it later and you resume. No serialization layer, no save-format versioning separate from the schema.

**Time-travel debugging.** The `transitions` table is a complete, append-only log of every state change the game has ever made. Replay any session from a checkpoint. Step forward tick by tick. This is a free feature given the architecture already records everything.

**Fully inspectable state.** At any moment, `sqlite3 world.sqlite` gives you the full game state as relational data. No binary format to decode, no proprietary tooling. Write a SQL query, understand the game.

**Clockwork determinism.** Every guard result is recorded. Every action is named in `actions_run`. The same event sequence against the same state always produces the same outcome. Bugs are reproducible.

**Remote debugging by default.** The debugger is HTTP and the database is read-only-shareable. Tunnel over SSH or Tailscale and inspect a game running on another machine from a phone.

**The contract is auditable within the monolith.** Cross-subsystem communication is a SQL query, not a function call with hidden coupling. The sqlite3 CLI is a debugger you already have.

**Mods need only a text editor.** No toolchain, no compilation. Edit `schema.json` and `behaviors/*.json`, save, see the change. The lowest possible barrier to participation.

**The data model is declarative and readable.** `schema.json` is the ground truth of what the game is made of. A new contributor reads it and the behaviors directory and understands the game's structure without diving into engine code.

## Honest tradeoffs

**Renderer and interpreter crash together.** Same process means a graphics panic or a runaway game loop kills everything. Unlike the process-separated design, there is no "renderer stays up while interpreter restarts." The debugger remains isolated and keeps serving the last DB state, but the game window goes dark. Acceptable for a single-developer project; the loss of crash isolation between interpreter and renderer is a deliberate tradeoff against simplicity.

**Renderer is Ebitengine-specific.** The multi-process design allowed the renderer to be written in any language. The monolith commits to Go + Ebitengine. The database-as-contract discipline is preserved internally, but the choice of graphics library is now fixed.

**No push notifications.** SQLite has no LISTEN/NOTIFY. The debugger polls `world_version` each refresh. For a read-only inspector this is fine; if it ever isn't, a sidecar that watches the WAL and pushes deltas is a future option.

**Input latency is at least one tick.** Input is captured and written to `input_events` synchronously within `Update()`, then the interpreter processes it in the same tick. At 20Hz that's still 50ms minimum for the next visual response after input. Acceptable for turn-based, simulation, and slow-action games; bad for twitchy action games.

**Schema evolution requires migration discipline.** Adding components or entity types is easy. Renaming, restructuring, or removing them is a migration. The unified `schema.json` plus generated SQL means migrations must be expressed at both levels. Boring, correct, well-understood, but real work.

**Two log streams.** Game process and debugger process. A supervisor that interleaves them helps. Wall-clock timestamps on every cross-subsystem DB row let you correlate across processes.

## Milestone order

The architecture is intentionally monolithic first. Get a working game before adding complexity.

1. **Interpreter tick loop + Ebitengine monolith.** Full working game: one entity wandering on screen via the `wandering_goblin` agent. Schema, behaviors, hot reload, and rendering all wired together. The prototype passes when the goblin wanders, and editing the agent JSON changes its behavior without a restart.

2. **Debugger as a separate process.** The one natural process boundary. Read-only HTTP server with entity/component/transition endpoints and the ASCII view route. Validates the "database as contract" claim with low risk. Remote debugging verified over SSH.

3. **Time-travel debugging.** Checkpoint the database at regular intervals. Debugger gains a timeline scrubber that replays a session from any checkpoint forward through the `transitions` log. The headline capability of the architecture, delivered as a debugger feature.

4. **Effects system.** Wire the transitions-as-effect-source flow. Implicit effects first (transition-to-`dead` triggers death effect), then named effect actions in agent JSON, then effect rule files for reskinning.

5. **Process supervision and packaging.** Polish the two-process dev experience (Procfile + supervisor). Launcher binary for shipped builds (game binary + optional debugger sidecar). Startup schema-check coordination.


## Future directions

**Process-separated renderer.** The "database as contract" discipline is maintained within the monolith by convention. Extracting the renderer into its own process is a well-defined future step if a concrete need surfaces (different language for graphics, crash isolation, multi-renderer). The architecture supports it; it is not currently scheduled.

**Browser deployment via WASM.** The interpreter is pure logic with one well-defined dependency (SQLite). It compiles to `wasm32` and runs in a browser tab alongside a SQLite-WASM build and a JavaScript renderer. Same `schema.json`, same agents, same contract.

**Lua for custom actions.** When a modder wants a behavior the built-in action library can't express, a Lua sandbox can expose named functions registered as additional actions. Agents reference them by name (`"type": "mymod::cast_spell"`). The `ActionHandler` and `GuardHandler` interfaces are already designed for this — a `LuaActionHandler` is a natural addition. Extending the action library without touching the behavior language or engine code.

**Visual behavior editor.** Stately Studio works today for editing the JSON. A bespoke editor that also visualizes live game state on the machine graph would close the loop between authoring and observing.

**Schema editor and visualizer.** `schema.json` is data; a tool that renders entity types as nodes with their component edges, lets modders edit visually, and generates migrations between versions, is a natural follow-on.

**Networked multiplayer (lockstep).** Replicate the `event_queue` (not world state) across machines, run identical interpreters on each, converge via deterministic execution. The database-as-replicated-log shape used by RTS engines and SpacetimeDB, arrived at from a different direction.
