# Epic 13 — Forge: AGENTS mode

Visual authoring of the behaviour machines the agent runtime executes: a machine
list per mod, a statechart canvas, and inspectors whose dropdowns are fed from
the engine's own action and guard registry.

**This is the first epic with a hand-written client-JS surface.** Everything
Forge has built so far is server-rendered HTML patched down an SSE stream, and
that is still true of every panel here — but a node you drag has to move at
pointer speed, and a round trip per pixel is not a design anyone would defend.
The canvas is therefore split across two stories: the chart renders and selects
server-side first, and direct manipulation is added on top of something already
known to be correct.

## Verified before planning

Every claim `docs/plan.md` makes about the engine was checked against the code
rather than trusted, the same discipline Epic 12 used. Most hold. Two do not,
and both change the plan.

| Claim | Verified |
|---|---|
| `Registry.Actions()`/`Guards()` expose `ParamSchema` metadata and have never had a caller | True — referenced only from `registry_test.go`. This epic is the caller. |
| Every built-in carries a written `Description` and its params | True (`builtins/register.go`) — 14 actions and guards, all documented. |
| `MachineDefinition.ContextManifest` is populated by `ValidateMachine` | True — **but only when validation fully succeeds** (`validator.go:75`). See below. |
| `Loader.List()`/`Sources()`/`Get()` exist for the machine-list panel | True (`loader.go:142,155,167`), added in Epic 11. |
| `StateNode.Parent` is a back-pointer that defeats `json.Marshal` | True (`machine.go:51`). |
| An XState emitter exists | True — `agent.EmitMachine` (`emit.go:34`), hand-written and byte-stable. Epic 11 Story 3. |
| `ParseMachine` rejects `invoke` at every nesting level | True (`machine.go:116,196`). |
| `ValidateMachine` returns every error rather than the first | True, and `ValidateMachineError` already joins them into the shape `editable.Codec.Validate` wants. |
| `after` keys are validated as durations | True — `ParseDurationMs` via `validateStateNode` (`validator.go:174`). |
| `editable.File[T]` can hold a machine | True, and its doc comment names `*agent.MachineDefinition` as an intended user. |
| `agent.Reconciler.Reconcile` is wired | True — `cmd/ecs-db/run.go:117`. Epic 11 Story 6 did it; the plan's "never had a caller" is stale. |
| `buildRegistry(hasMap)` mirrors what `run.go` registers | True — pathfinding and line-of-sight are registered only when a map is configured. |

### The round trip is lossy, and the epic's headline claim depends on it

`ParseMachine`'s own doc says "unknown fields are silently ignored", and
`EmitMachine` writes only what it knows. Together that means **a Forge save
deletes anything in the file Forge does not model.** Probed with a Stately-shaped
export carrying `description`, `tags` and `meta`:

```
$ go test ./internal/agent -run RoundTrip
round-tripped:
{
  "id": "goblin",
  "initial": "idle",
  "context": { "hp": 0 },
  "states": { "idle": { "on": { "SPOTTED": [{ "target": "chasing" }] } }, "chasing": {} }
}
round trip dropped "description"
round trip dropped "tags"
round trip dropped "meta"
```

"Round-trips with Stately Studio" is the sentence on the panel in the design
prototype, and today it is false in the direction that matters: importing an
exported machine and saving it silently throws work away. `docs/plan.md` has
this as the *last* bullet of the epic. It is Story 1 here, because every story
after it ships a save button.

It also changes a design decision. `meta` is XState's sanctioned place for
arbitrary per-state data, so once unknown fields survive, layout has somewhere
to live inside the machine file — which is worth weighing against the plan's
sidecar before writing one.

### A layout sidecar in `behaviors/` would register itself as a machine

`Loader.ScanDir` loads **every `*.json`** in a behaviours directory
(`loader.go:82`). The plan proposes persisting canvas layout in
`behaviors/<id>.layout.json`. Probed:

```
ScanDir loaded 2, err=<nil>
  registered machine id="goblin" from core:…/goblin.json
  registered machine id=""       from core:…/goblin.layout.json
```

It does not fail — it *succeeds*, registering a nameless machine. Two sidecars
would collide on `""` and one would silently shadow the other, and the machine
list would carry an entry with no name. Story 1 picks where layout goes with
this on the table.

### `ContextManifest` is empty exactly when you need it

`ValidateMachine` populates the manifest only if it found no errors at all. So
the CONTEXT MANIFEST panel is blank precisely when the machine is broken — which
is when someone is looking at it. Story 3 renders the absence as absence rather
than as "this machine seeds nothing".

## Stories

| # | Story | Delivers |
|---|---|---|
| 1 | [Round-trip fidelity](01-round-trip-fidelity.md) | Unknown fields survive parse → emit; where layout lives |
| 2 | [Machine editing session](02-machine-session.md) | Many files, not one: open, create, rename, delete, save, hot-swap |
| 3 | [Machine list & context manifest](03-machine-list.md) | Machines per mod with `override`; the manifest, including when it is missing |
| 4 | [Statechart canvas: rendering](04-canvas-rendering.md) | The chart, server-rendered from the machine and its layout; selection |
| 5 | [Statechart canvas: direct manipulation](05-canvas-editing.md) | Drag, connect, add, context menus — the client-JS surface |
| 6 | [State inspector](06-state-inspector.md) | Name, entry/exit actions from the registry, set-initial |
| 7 | [Transition inspector](07-transition-inspector.md) | Event, guard, and a param form generated from the guard's schema |
| 8 | [Inline validation](08-inline-validation.md) | Every `ValidateMachine` error at once, against the node that caused it |

`docs/plan.md` lists six bullets; this is eight. Three differences, all
deliberate:

- **Round-trip fidelity moves from last to first.** It is a precondition, not a
  finishing touch: a save that silently deletes `meta` is worse the longer it
  ships, and the canvas cannot choose where to keep layout until it is settled.
- **The editing session is a story.** Epic 12 learned this the expensive way and
  gave it Story 1 there too, and machines are harder: `schema.json` is one file
  that always exists, while machines are N files that can be created, renamed,
  deleted, and shadowed by a later mod.
- **The canvas is two stories.** Rendering it correctly and manipulating it
  directly fail in completely different ways, and only the second needs JS.

## Process

Per `AGENTS.md`: TDD, a fresh-context code review before each commit, and
`make test` + `make e2e` + the linter green. Each story carries a **Playwright
steps** section written before the code.

### Two rules specific to this epic

**No story may re-derive the built-in catalogue.** Actions and guards come from
`agent.Registry`, built exactly as `internal/forge/project.buildRegistry` builds
it — including its `hasMap` gate, which is why `computePath` is offered on a
project with a map and not on one without. A dropdown that offers a name the
engine will not register produces a machine the engine then refuses to load,
which is the editor-and-game-disagree failure the whole approach exists to
avoid.

**The canvas's JS owns pointer state and nothing else.** Where a node sits while
it is being dragged is the client's business; what the machine *is* stays on the
server and arrives as a patch, like everything else. A canvas that keeps its own
model of the statechart is a second source of truth, and the first thing it will
disagree with is the file on disk.
