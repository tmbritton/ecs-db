# ECS Database

A game engine where **SQLite is the IPC contract**. Game state lives in a database. Behaviors are JSON state machines. The interpreter, renderer, and debugger are separate processes that never call each other — they read and write tables. The schema is the API. The result is crash-isolated, language-polyglot, moddable with a text editor.

## Thesis

```
┌─────────────────────────┐  writes input_events
│  Renderer               │ ─────────────────────┐
│  any language           │                      │
│  any graphics lib       │ ◄── reads world ─────┤
└─────────────────────────┘                      │
                                                 ▼
┌─────────────────────────┐                ┌──────────────────┐
│  Interpreter            │ ◄── reads ─────│  world.sqlite    │
│  state machine engine   │     input      │                  │
│  writes world state     │ ──── writes ──►│  (one writer     │
└─────────────────────────┘     world      │   per table)     │
        │                                  └──────────────────┘
        │ reads schema + behaviors
        ▼
┌─────────────────────────┐
│  mods/                  │
│   ├── schema.json       │
│   └── behaviors/*.json  │
└─────────────────────────┘
```

Three key ideas:

1. **The database is the contract.** Every process opens the same `world.sqlite` file. No HTTP, no gRPC, no message bus. Cross-process communication is a SQL query.
2. **The schema is the mod surface.** A single `schema.json` file declares components and entity types. The interpreter derives the SQLite DDL from it. Modders change the schema and behavior JSON files — no compilation needed.
3. **Behaviors are JSON state machines.** Agents conform to a subset of XState: states, transitions, guards, actions, timers. Modders design in Stately Studio and drop the exported JSON into `mods/behaviors/`. The interpreter validates and runs them.

## Why This Architecture?

| Benefit | How |
|---------|-----|
| **Crash isolation** | Renderer segfault doesn't lose game state. Interpreter panic doesn't blank the screen. Restart one process, the others don't notice. |
| **Language polyglot for real** | Renderer in the language with the best graphics bindings. Interpreter in the language best for state machines. Debugger in the language with the easiest HTTP story. |
| **Modding with a text editor** | Edit `schema.json` and a behavior JSON, save, the interpreter picks up the change. No toolchain, no compilation. |
| **WAL mode means parallel readers** | Renderer reads while interpreter writes. Neither blocks the other on most frames. |
| **Multiple renderers simultaneously** | A 2D view AND a terminal ASCII view AND a minimap — three processes, one world file. |
| **Remote debugging by default** | The debugger is HTTP and SQLite is read-only-shareable. Tunnel over SSH or Tailscale, inspect from your phone. |

## The Data Model

`schema.json` is the declarative source of truth. It declares:

- **Components** — strongly typed data that can be attached to entities. The interpreter generates one `comp_*` table per component with columns derived from the component's property types.
- **Entity types** — named templates that declare which components are required, which are optional, whether extras are allowed, and whether violations are hard errors or warnings.

From this single file, the interpreter produces the full SQLite schema: fixed system tables (`meta`, `world`, `entities`, `event_queue`, `input_events`, `transitions`) plus generated `comp_*` tables for each declared component. Every process checks `meta.schema_version` on startup to ensure compatibility.

## Full Roadmap

See [`docs/plan.md`](docs/plan.md) for details and [`docs/game-engine-arch.md`](docs/game-engine-arch.md) for the full architecture document.

## Quick Start

```bash
# Clone the repository
git clone https://github.com/tmbritton/ecs-db
cd ecs-db

# Build and run
make build
./bin/ecs-db
```

This loads `schema.json`, creates (or opens) the SQLite database with generated tables, and exits. The CLI is still early — command processing, entity operations, and a full tick loop come in later epics.

## Why Go?

This project is built in Go for several key reasons:
- **Fast iteration**: Simple build, no external runtime, compiles to a single binary
- **SQLite support**: Mature, battle-tested driver (`database/sql` + `go-sqlite3`)
- **Portability**: Compiles to native Linux, macOS, Windows binaries and to WASM
- **Performance**: More than fast enough for single-process game simulation at 20-60Hz

The architecture explicitly leaves the renderer and debugger languages open — Odin + Raylib, Rust + Macroquad, or a TypeScript + Phaser web renderer are all valid.

## Project Structure

```
ecs-db/
├── docs/
│   ├── game-engine-arch.md    # Full architecture document
│   └── plan.md                # Implementation roadmap (10 epics)
│   └── stories/               # Refined story files per epic
│       └── epic-1/            # Epic 1 stories with acceptance criteria
├── cmd/cli/main.go            # CLI entry point
├── internal/
│   ├── schema/                # Schema loading, validation, type definitions
│   └── storage/               # SQLite operations, DDL generation
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
