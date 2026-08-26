# Story 2 — implementation plan

**Story:** [Map editing session](02-map-session.md)

## What was verified first

| Claim | Verified |
|---|---|
| `project.Open` drops the map path | True — `project.go:105` reads `cfg.Map.Path` only to compute `hasMap` for the registry. `Project` has no map field. |
| `editable.File` gives dirty, save, discard, reload and the conflict path | True, and Story 1's `mapfile.Codec` already plugs a `*tiled.Document` into it. |
| `Save` on a *clean* file whose disk copy changed still reports the conflict | True — `checkUnchanged` runs before the "nothing to write" early return (`editable.go:125-150`). That matters here: this story ships no edit, so the conflict path has to be reachable without one. |
| `savereport.Set` holds the latest report per file | True (`savereport.go`), and `Server.saves` is already that set. |
| `tiled.ResolveTilesets` takes the map's directory and an `Opener` | True (`resolve.go:21`). It reads `.tsx` files; it does **not** open the image a tileset names, so a fixture map needs no PNG until Story 3. |
| The footer is shell-level and chosen per mode | True — `Server.footer` switches on the slug, and `SaveFooterProps` already carries `Elsewhere` for unsaved work another mode owns. |
| **The e2e fixture project deliberately has no `[map]`** | True, and its `game.toml` says why: *"naming a file that does not exist would be a trap for the first story that starts resolving it."* This is that story. See below — adding one is not free. |

## Adding a map to the e2e fixture flips a gate two specs assert on

`machines.Config.HasMap` comes from `cfg.Map.Path != ""`, and
`project.buildRegistry` registers `computePath` and `inLineOfSight` only when it
is true — mirroring what `cmd/ecs-db/run.go` registers, so Forge cannot call a
machine valid that the engine will refuse. Two browser specs asserted the
*closed* side of that gate:

- `13-state-inspector.spec.js:96` — "computePath needs a map this project has not got"
- `13-canvas-rendering.spec.js:332` — the same, in a comment on its fixture

Three ways out. A **second fixture project** for MAP is more machinery than the
thing being tested. **Dropping the assertions** loses a real check. So: the
fixture gains a map, and those specs flip to asserting the gate *opens* —
`computePath` is offered now that the project has a map. Nothing is lost,
because the closed side is already covered in Go four times over
(`project_test.go:377`, `inspector_test.go:258`, `canvasedit_test.go:481`,
`transitionedit_test.go:319`), and the browser gains a check it did not have.

## Shape

**`project.Project.MapPath`** — the configured map, resolved by `config.Load`
like every other path, and empty when the project declares none.

**`internal/forge/maps`** — the session, a sibling of `machines` and `session`
rather than a generalisation. (`maps` shadows the stdlib package of that name;
nothing that will import this uses it — only `internal/renderer` does, and it
never will.)

- `Open(cfg Config) (*Session, error)`, `Config{MapPath string}`
- `Paths() []string`, `Maps() []Map` — `Map{Path, Name, Configured}`
- `Read(path, fn)` / `Edit(path, fn)` over the `*tiled.Document`
- `Resolved(path) (*tiled.Map, error)` — parsed **and** tilesets resolved
- `Dirty() ([]string, error)`, `Save(path)`, `Discard(path)`, `Reload(path)`,
  `SaveOverwriting(path)`
- `Problems() []project.Problem`

**Routes** `POST /forge/map/{save,discard,reload,save/overwrite}`, mirroring
`machineedit.go`, with the same "is this a map this project has open" check that
`machinePath` makes — **including its refusal of an empty parameter**, which the
first cut of this got wrong: it defaulted to the first map held, and the footer
then saved a file the user was not looking at — the parameter names a file and must not be able to address
an arbitrary path.

**MAP mode** renders the tab strip, the open map's name and its problems, and
nothing else. The canvas stays a stub naming Story 3.

## Decisions taken up front

- **Which maps a project has:** the configured map, plus the `.tmx` and `.tmj`
  files in its directory. The configured one comes first and is **marked**,
  because a project can hold five maps and `ecs-db run` loads exactly one —
  editing any of the others is real work that changes nothing about the game
  until `game.toml` says so, and a tab strip that hid that would be lying by
  omission. No recursion and no other directories: Epic 17's Open Map dialog is
  where "some other file" belongs.
- **A `.tmj` is listed and cannot be opened for editing.** It is a map the
  project has, and hiding it would be worse than showing it with the reason
  Story 1 gives. It appears as a `Problem`, not as a missing tab.
- **Tilesets are resolved on demand and never held.** *(Amended after review:
  `Resolved` still parses fresh every call, but the resolution **check** that
  feeds the problems panel is recorded when a map is opened rather than redone
  on every render — it was costing 24 ms per 500×500 map on every stream tick of
  every mode. See the story's As Implemented.)* The session's `Resolved`
  parses the document's *current bytes* fresh and resolves against the map's
  directory each time. Holding the tree means a `.tsx` edited by Epic 16, or by
  Tiled in another window, leaves a stale palette with no signal to invalidate
  on. It also keeps resolution off `Document.Map()`, which is memoised — writing
  a resolved tileset into that value would give the resolution a lifetime that
  ends at the next edit for no reason anybody could predict. If the cost shows
  up it shows up in Story 3, which is where Story 1 already recorded that
  parsing is the thing that stops scaling first.
- **A map that will not open is held as a Problem and is not a tab.** Same rule
  `machines.Session` uses for a machine that will not open: reported, not fatal,
  because fixing it is what the editor is for.

## Playwright, and one step that cannot be met here

The story's spec list includes *"An edit made through any Story 4 route flips
the footer to `● unsaved`"*. Story 4 does not exist yet and this story ships no
edit route, so that step moves to Story 4's spec. Everything else this story
owns is reachable: the map opens and is named, the strip lists what the project
has, the configured map is marked, the footer reads `✓ saved`, a map changed on
disk reports the conflict, and a broken map is listed with its reason.

Dirty, discard and reload are proven in Go, where an edit is one call to
`SetLayerData`.

## Risks

- **`Resolved` is called per render in Story 3.** Measured cost belongs there,
  but the shape has to be right here: it must not mutate anything the session
  keeps, or two callers will disagree about whether a tileset is loaded.
- **The fixture change touches three specs.** Run the whole browser suite, not
  the new one.
