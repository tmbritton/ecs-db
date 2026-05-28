# Architecture: A process-separated, database-as-contract game engine

## Thesis

Game state lives in a SQLite database with structure defined by a declarative `schema.json` file. Game behavior is JSON state machines (XState-compatible) attached per-entity. Each component of the engine — interpreter, renderer, debugger, tooling — is a separate OS process communicating only through the database. The schema is the API. The JSON behaviors and schema are the mod surface. Everything else is replaceable.

The result is an engine that is local-first, fully inspectable, moddable with a text editor, language-polyglot by design, crash-isolated between components, and built from boring, long-lived technology (SQLite, JSON, a graphics library, plus one process per language of choice).

This document is language-agnostic. Go and Rust are both reasonable implementation choices for the interpreter; renderers can be written in whatever pairs well with the chosen graphics library (Odin with Raylib, Rust with Macroquad, JavaScript with Phaser, etc.).

## Architecture

```
┌─────────────────────────┐  writes input_events
│  Renderer               │ ─────────────────────┐
│  graphics library +     │                      │
│  any language           │ ◄── reads world ─────┤
└─────────────────────────┘                      │
                                                 ▼
┌─────────────────────────┐                ┌──────────────────┐
│  Interpreter            │ ◄── reads ─────│  world.sqlite    │
│  state machine engine   │     input      │                  │
│  writes world state     │ ──── writes ──►│  (one writer     │
└─────────────────────────┘     world      │   per table)     │
        │                                  │                  │
        │ reads schema + behaviors         │                  │
        ▼                                  └──────────────────┘
┌─────────────────────────┐                       ▲
│  mods/                  │                       │ reads transitions
│   ├── schema.json       │                       │ reads world
│   └── behaviors/*.json  │                       │
│  hot-reloaded           │              ┌──────────────────┐
└─────────────────────────┘              │ Debugger         │
                                         │ web UI, terminal │
                                         └──────────────────┘
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

**A `world_version` watermark.** A single integer in the `world` table that increments on every world-state write. Pollers read this cheaply each frame and skip the full query when nothing has changed. Trades one tiny query for many large ones.

**JSON payloads for extensibility.** Structural fields (entity id, component type, event kind) are typed SQL columns. Per-feature data (event payload, machine context, action params) is a JSON column.

**Append-or-upsert, never destructive cross-process writes.** The renderer never deletes from `entities`. The debugger never writes anywhere. Each process's write surface is narrow and well-defined.

### Table ownership

|Table|Writer|Readers|Purpose|
|---|---|---|---|
|`meta`|init/migrate|all|Schema version, build info|
|`world`|interpreter|all|Current tick, world_version watermark|
|`entities`|interpreter|renderer, debugger|Entity registry with entity type|
|`comp_*`|interpreter|renderer, debugger|Component data (one table per component, generated from schema.json)|
|`behavior_components`|interpreter|renderer, debugger|Active machine state per entity; interpreter-managed, not in schema.json|
|`event_queue`|interpreter|(drained by interpreter)|Internal game events and scheduled `after` timers|
|`input_events`|renderer|interpreter (drains)|Raw input from user|
|`transitions`|interpreter|debugger, renderer (effects)|Audit trail of every state transition|

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

The renderer is intentionally dumb. It does three things:

1. Each frame, check `world_version`. If unchanged from last frame, skip the expensive query and redraw the last snapshot.
2. If changed, query the world (entities joined with the components needed for drawing) and rebuild the local snapshot.
3. Capture user input and write it to `input_events`.

The renderer does not know what an entity _means_ in game terms beyond its entity type and components. It knows the schema: "entities of type Goblin have Position and Sprite, draw the sprite at the position." Add a new entity type in `schema.json` and the renderer either handles it generically (draw the sprite) or shows a placeholder until you teach it something specific.

The renderer can be written in any language with a SQLite binding and a graphics library. Odin with Raylib is a natural fit; so is Rust with Macroquad, or a terminal renderer in Python for headless debugging. Multiple renderer processes can run against the same database — a 2D top-down view and a terminal ASCII view side by side, both reading the same `world.sqlite`.

A browser-based renderer is a special and useful case: Phaser (or any canvas/WebGL library) running in a tab, with a WASM build of SQLite holding the database in OPFS or memory, and the interpreter compiled to WASM running alongside. The renderer code is structurally identical to a native one — query entities, draw them, write inputs — just in JavaScript against SQLite-WASM instead of native SQLite. Same architecture, same agents, same contract.

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

A small HTTP server (any language; Go is a natural choice for its standard library `net/http` and pure-Go SQLite drivers). Opens `world.sqlite` read-only, serves a single HTML page that polls a few endpoints:

- **Entities**: current state of every entity, including type and machine context.
- **Components**: per-entity component data.
- **Transitions**: most recent N transitions, ordered newest first.
- **Schema**: the current `schema.json`, for reference.

The page auto-refreshes. The "why did the goblin attack?" question becomes one click: filter transitions to that entity, see the sequence of state changes with the triggering events. Combined with the `context` column you can see what the entity was thinking at every step.

Because the debugger is HTTP and the database is read-only-shareable, remote debugging is trivial: tunnel the port over SSH or Tailscale and inspect a game running on another machine from your phone.

The debugger is the easiest component to extend. A behavior visualizer (render the active state machine as a graph using Stately's renderer), a time-travel UI (jump to any past tick), a component diff viewer, a schema inspector — all additions to the debugger, none requiring changes to the interpreter or renderer.

## Process supervision

In development, a Procfile-style supervisor (overmind, foreman, or systemd user units) brings the three processes up together and interleaves their logs. Restarting one process doesn't touch the others. Iterating on the renderer means restarting just the renderer while the interpreter keeps simulating — the renderer reopens the database and catches up via `world_version`.

In a packaged game, a small launcher binary spawns the interpreter and renderer subprocesses and tears them down on exit. No containers, no orchestration. The debugger is optional in shipped builds.

## Why this architecture is the move

**Crash isolation.** Renderer segfault doesn't lose game state. Interpreter panic doesn't blank the screen — the last frame stays up until restart. Debugger crashes don't matter.

**Language polyglot for real.** Renderer in the language with the best graphics bindings. Interpreter in the language that best fits its data-shuffling and state-machine workload. Debugger in the language with the easiest HTTP story. Each component in the language that fits.

**Parallel hardware.** WAL mode means the renderer reads while the interpreter writes. On a multi-core machine each process gets its own core for free.

**Hot-swap a component.** Restart one process, the others don't notice. This is materially faster than monolithic-engine development.

**Multiple renderers simultaneously.** Run a 2D view AND a terminal ASCII view AND a top-down minimap — three processes, all reading the same world. No coordination needed.

**Remote debugging by default.** The debugger is HTTP. Over Tailscale, inspect a game running on another machine from your phone.

**The contract is auditable.** Every IPC mechanism is a SQL query. No serialization formats, no RPC stubs, no protobuf compilation. The sqlite3 CLI is a debugger you already have.

**Mods need only a text editor.** No toolchain, no compilation. Edit `schema.json` and `behaviors/*.json`, save, see the change. The lowest possible barrier to participation.

**The data model is declarative and readable.** `schema.json` is the ground truth of what the game is made of. A new contributor reads `schema.json` and the behaviors directory and understands the game's structure without diving into engine code.

## Honest tradeoffs

**Process startup cost.** Three processes to launch instead of one. Mitigated by a supervisor and the fact that you rarely cold-start during development.

**SQLite writer contention.** Two processes writing the same database serialize through SQLite's file lock. The "one writer per table" convention keeps real contention rare, but the renderer (writing input_events) and the interpreter (writing world state) can occasionally hit the same lock. WAL plus a 5-second busy_timeout makes this invisible in practice for non-pathological write rates.

**No push notifications.** SQLite has no LISTEN/NOTIFY. Renderers poll `world_version` every frame, the debugger polls every half-second. For real-time single-player games this is fine; if it ever isn't, a sidecar that watches the WAL and pushes deltas is a future option.

**Input latency is at least one tick.** The renderer records input asynchronously; the interpreter consumes it on the next tick. At a 20Hz interpreter, that's 50ms minimum input-to-response. Acceptable for turn-based, simulation, and slow-action games. Bad for twitchy action games, which would require either a faster interpreter tick rate or local prediction in the renderer.

**Cross-process input-to-game-event mapping lives in the interpreter.** Correct (the interpreter owns world state) but means the renderer cannot interpret input itself. For predictive UI, the renderer needs its own local state.

**Schema evolution requires migration discipline.** Adding components or entity types is easy. Renaming, restructuring, or removing them is a migration. The unified `schema.json` plus generated SQL means migrations must be expressed at both levels — bump `schema.json` version, write the SQL DDL change, write any data backfill. Boring, correct, well-understood, but real work.

**Three log streams to read when things go wrong.** A supervisor that interleaves them helps. Tagging every event with wall-clock timestamps (which `input_events` and `transitions` both do) lets you correlate across processes.

## Milestone order

The architecture is splittable but should not be split prematurely. Premature splitting is a refactor without a working baseline.

1. **Monolithic prototype.** A single process containing interpreter, renderer, and behavior loading. Prove the schema-driven SQL generation works. Get one entity wandering. Hot-reload its behavior file. Edit `schema.json` and see the database change correctly on restart.
    
2. **Extract the debugger.** The easiest split: read-only, no contention concerns. A small HTTP binary serving an HTML page. The experience of splitting is small and rewarding and validates the "database as contract" claim.
    
3. **Extract the renderer.** The bigger split: input flow becomes asynchronous, the input-event table comes into use, the renderer gains its own language choice. Three processes, one game.
    
4. **Add effects.** Wire up the transitions-as-effect-source flow. Implement implicit effects first (transition-to-`dead` triggers death effect), then named effect actions, then effect rule files.
    
5. **Add a second renderer.** Terminal ASCII view or a minimap. Proves the multi-renderer thesis and gives a genuinely useful debugging tool.
    
6. **Schema versioning and migrations.** Once you've changed `schema.json` in anger a few times, you'll know what the migration story needs. Build it then.
    

## Future directions

**Browser deployment via WASM.** The interpreter is pure logic with one well-defined dependency (SQLite). It compiles unchanged to `wasm32` and runs in a browser tab alongside a SQLite-WASM build and a JavaScript renderer such as Phaser. Same `schema.json`, same agents, same contract — the renderer process is simply replaced by a JavaScript runtime. Free web playable demos of any game built on the engine.

**WASM for custom actions.** When a modder wants a behavior the built-in action library can't express, load a WASM module that exports named functions and register them as additional actions. Agents reference them by name (`"type": "mymod::cast_spell"`) and it just works. A narrow, well-justified use of WASM: extending the action library, not replacing the behavior language.

**Replay and time-travel debug.** The `transitions` table plus the `event_queue` history is a complete log of what happened. A debugger mode that re-runs a recorded session against a checkpoint database would be a powerful debugging tool — a free feature given the architecture already records everything.

**Visual behavior editor.** Stately Studio works today for editing the JSON. A bespoke editor that also visualizes live game state on the machine graph would close the loop between authoring and observing.

**Schema editor and visualizer.** `schema.json` is data; a tool that renders entity types as nodes with their component edges, lets modders edit visually, and generates migrations between versions, is a natural follow-on.

**Networked multiplayer (lockstep).** The architecture has a natural seam for lockstep simulation: replicate the `event_queue` (not the world state) across machines, run identical interpreters on each, converge to the same world via deterministic action and guard execution. This is the database-as-replicated-log shape used by RTS engines and SpacetimeDB, arrived at from a different direction.
