# ECS-in-SQLite: A Game Engine with Declarative Behaviors

SQLite is the game state. Behaviors are JSON state machines. The result is a game engine where **the save file is the running database**, state is inspectable with any SQL client, and behaviors are authored with a text editor.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  Game Process (Go binary)                               │
│                                                         │
│  Interpreter (state machines, sole world writer)        │
│      │ Update() — runs tick, writes world state         │
│      ▼                                                  │
│  Renderer (Ebitengine, reads entities + components;     │
│            writes input_events synchronously)           │
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
```

Two key ideas:

1. **SQLite is the game state.** A single `world.sqlite` file holds everything: entities, component data, behavior machine state, and a complete audit log of every transition ever made. The save file is just the database. Inspecting state is a SQL query. Time-travel debugging is replaying the `transitions` log.
2. **Behaviors are JSON state machines.** Agents conform to a subset of XState v4: states, transitions, guards, actions, timers, history. Modders design in Stately Studio and drop the exported JSON into `mods/behaviors/`. The interpreter validates and runs them. A modder cannot write an agent that executes arbitrary code — only registered actions and guards can fire.

## Why This Architecture?

| Benefit | How |
|---------|-----|
| **Trivial save games** | The database file is the save. Copy it. Open it later. Done. |
| **Time-travel debugging** | `transitions` is an append-only audit log. Replay any session from a checkpoint. |
| **Fully inspectable state** | `sqlite3 world.sqlite` shows everything. No binary format to decode. |
| **Clockwork determinism** | Every guard result and action stored. Same inputs always produce same outputs. |
| **Moddable with a text editor** | Edit `schema.json` and behavior JSON, save, the interpreter picks up the change. No toolchain, no compilation. |
| **Remote debugging by default** | The debugger is HTTP. Tunnel over SSH or Tailscale, inspect from a phone. |

## The Data Model

`schema.json` is the declarative source of truth. It declares:

- **Components** — strongly typed data attached to entities. The interpreter generates one `comp_*` table per component with typed columns.
- **Entity types** — named templates declaring which components are required, optional, or disallowed.

From this single file the interpreter produces the full SQLite schema: fixed system tables (`meta`, `world`, `entities`, `event_queue`, `input_events`, `transitions`, `behavior_components`) plus generated `comp_*` tables for each declared component.

## Full Roadmap

See [`docs/plan.md`](docs/plan.md) for details and [`docs/game-engine-arch.md`](docs/game-engine-arch.md) for the full architecture document.

## Quick Start

```bash
# Clone the repository
git clone https://github.com/tmbritton/ecs-db
cd ecs-db

# Build and run (uses game.toml in the working directory)
make build
./bin/ecs-db

# Point at a different config file
./bin/ecs-db -config ./my-game/game.toml
```

This reads `game.toml`, loads `schema.json`, creates (or opens) the SQLite database, and scans each mod's `behaviors/` directory for state machine files. The CLI is still early — the full tick loop and Ebitengine renderer come in Epic 5.

## Configuration

The engine is configured by `game.toml` (TOML format). If the file is absent, built-in defaults apply (`./ecs.db`, `./schema.json`, no mods).

```toml
[database]
path = "./ecs.db"

[schema]
path = "./schema.json"

# Mods are loaded in order; later entries override earlier ones on duplicate machine IDs.
[[mods]]
name      = "core"
behaviors = "./behaviors/"   # XState v4 JSON state machines
actions   = "./actions/"     # Lua custom actions (future)
guards    = "./guards/"      # Lua custom guards (future)
assets    = "./assets/"      # Sprites, sounds, etc. (future)
```

Multiple `[[mods]]` entries are supported. They are loaded in array order — a later mod can override a core machine by providing a `behaviors/*.json` file with the same `"id"`. A warning is printed when an override occurs.

## Why Go?

- **Fast iteration**: Simple build, no external runtime, compiles to a single binary
- **SQLite support**: Mature driver (`modernc.org/sqlite`, pure Go — no CGo)
- **Portability**: Compiles to native Linux, macOS, Windows binaries and to WASM
- **Ebitengine**: First-class Go game library, same binary as the interpreter

## Project Structure

```
ecs-db/
├── docs/
│   ├── game-engine-arch.md    # Full architecture document
│   ├── plan.md                # Implementation roadmap
│   └── stories/               # Refined story files per epic
├── cmd/cli/main.go            # CLI entry point (-config flag, startup wiring)
├── internal/
│   ├── agent/                 # State machine interpreter (parser, registry, SCXML engine)
│   ├── config/                # TOML config loader (Config, ModConfig, Load, Defaults)
│   ├── schema/                # Schema loading, validation, type definitions
│   ├── storage/               # SQLite operations, DDL generation, MachineWriter
│   └── world/                 # Domain interfaces (WorldWriter, WorldReader, Tx)
├── behaviors/                 # Core game behavior machines (XState v4 JSON)
├── actions/                   # Core Lua action scripts (future)
├── guards/                    # Core Lua guard scripts (future)
├── assets/                    # Core sprites, sounds, etc. (future)
├── game.toml                  # Default configuration (database, schema, mod paths)
├── schema.json                # Declarative game data model (source of truth)
├── Makefile
└── README.md
```

## License

GNU Affero General Public License v3.0

## Getting Help

- **Issues**: Report bugs or request features via GitHub Issues
- **Discussions**: Ask questions or share ideas in GitHub Discussions
- **Documentation**: See [`docs/`](docs/) for the architecture, plan, and refined stories
