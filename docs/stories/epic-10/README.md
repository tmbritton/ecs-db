# Epic 10 — Forge foundation

Stories and implementation plans for the Forge app shell, toolchain and design
system. Read `docs/plan.md` (Epic 10) for the epic-level goal and story list.

## The design reference

Every visual story here checks itself against the Forge design handoff, which
lives in **Claude Design**, not in this repo:

<https://claude.ai/design/p/d2402254-cab9-4972-9987-bf689dc6329a>

Append `?file=<name>` to open a specific prototype, e.g.
[`?file=Forge+Editor.dc.html`](https://claude.ai/design/p/d2402254-cab9-4972-9987-bf689dc6329a?file=Forge+Editor.dc.html).
The bundle contains:

| File | What it is |
|---|---|
| `README.md` | The UI spec — palette, layout, modes, interactions |
| `Forge Design System.dc.html` | Palette / type / component reference |
| `Forge Editor.dc.html` | Full editor prototype, all six modes + dialogs |
| `Tile Editor Wireframes.dc.html` | Earlier lo-fi tile-editing flow |
| `ARCHITECTURE.md` | Near-duplicate of `docs/game-engine-arch.md` — prefer the tracked copy |
| `EPICS.md` | Superseded by Epics 10–20 in `docs/plan.md` |

The prototypes are design references, **not code to port**. Recreate the intent
in Templ + Datastar; keep the palette, type treatment and 2px-hard-border shape
language exactly.

## Working offline / capturing screenshots

`scripts/screenshot.js` needs local files, so for side-by-side comparison put a
copy of the bundle at `docs/design_handoff_forge_editor/` — that path is
`.gitignore`d, since ~430K of vendored `.dc.html` plus its JS runtime does not
belong in a Go repo. Nothing builds or tests against it; only the
"compare against the prototype" verification steps use it.

The `.dc.html` files load `support.js` as a sibling, so serve them over HTTP
rather than opening with `file://`:

```bash
cd docs/design_handoff_forge_editor && python3 -m http.server 8099
```

```bash
node scripts/screenshot.js "http://localhost:8099/Forge%20Editor.dc.html" desktop
node scripts/screenshot.js "http://localhost:8099/Forge%20Design%20System.dc.html" desktop --full
```

Shots land in `tmp/shots/`, which is also untracked.

## Where these plans diverge from AGENTS.md

`AGENTS.md` describes conventions the codebase does not currently follow. Where
they conflict, **these plans follow the code**. Flagged here so the divergence
is a decision rather than an accident:

| `AGENTS.md` says | Reality, and what these plans do |
|---|---|
| Google Wire for DI; wire sets in `cmd/*/wire.go` | Wire is not a dependency and no `wire.go` exists; `cmd/game/main.go` hand-wires the object graph. These plans hand-wire too, and inject config explicitly into the Forge server. |
| Cobra tree at `cmd/<binary>/cmd/root.go` | The tree is flat in `cmd/game/`. Story 1 keeps it flat at `cmd/ecs-db/`. |
| "No package-level `var` singletons" | `internal/config` is a `Get()`/`Init()` singleton. These plans add two read-only package-level values — `mode.All` (the mode table) and `web.Static` (the embedded FS). Both are immutable data, not mutable state, which is the spirit of the rule. |
| "Current State: working through Epic 1" | Epics 1–5 are complete. `docs/plan.md` is the authoritative status. |

If Wire should genuinely be introduced, it belongs as its own story ahead of
Story 2, not retrofitted afterwards.

These implementation plans also drop epic-5's `> **For agentic workers:**`
blockquote, which names a sub-skill that is not available here, and add a
`## Files` table and `**Architecture:**` line instead. That change is deliberate.
