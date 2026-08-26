# Epic 15 — Forge: MAP mode (AUTHORED)

The core map editor: layers, tile painting, spawn placement and spawn editing.
This is the epic Epic 14 existed to make possible — the engine now reads Tiled
maps, and nothing yet writes one.

**This is the second and last hand-written client-JS surface**, for the same
reason the statechart canvas was the first: a stamp under a dragging pointer has
to follow it, and a round trip per cell is not a design anyone would defend.
The canvas is split across two stories on Epic 13's model — painting is made
correct server-side first, and pointer handling is added on top of something
already known to work.

## Verified before planning

`docs/plan.md`'s Epic 15 was written before Epic 14 was built. Every claim it
makes was checked against the code as it now stands. **The load-bearing one is
false**, and it becomes this epic's Story 1.

| Claim | Verified |
|---|---|
| **"Engine touchpoints: Epic 14's TMX writer path"** | **False.** There is no writer. `internal/tiled` has `Parse`, `ParseTileset` and `ResolveTilesets` and no `Emit`, `Write`, `Marshal` or `Encode` of any kind — `grep -rn "func Emit\|func Write\|func Marshal\|func Encode" internal/tiled internal/tilemap` finds nothing. Epic 14's nine stories are all read-side. |
| The engine reads multiple tile layers | True — `tilemap.cellAt` (`tiled.go:116`) walks `m.Layers` from the top down and the topmost non-empty tile decides the cell. A layer panel has something real to write to. |
| Spawns serialize as TMX objects | True — `tilemap.SyncSpawns` reads every `ObjectGroup`, and an object's *class* is its entity type (`spawnable`, `spawn.go:476`). |
| Spawn identity survives a re-import | True — an engine-owned `spawns` table keyed `(map, object_id)` (`storage/tables.go:57`). **The object id is the spawn's identity**, which makes id allocation a correctness concern rather than a formality. |
| The palette can be fed from `schema.json` | True — `session.Session` already holds the editable schema, and both SCHEMA and ENTS render from it. |
| The `ƒ ctx` badge has a data source | True — `project.Machine.Definition.ContextManifest`, already rendered by ENTS's context-seeds panel (Epic 12 Story 5). |
| `+ ADD COMPONENT` can know what is required and what is optional | True — `schema.EntityType.RequiredComponents` / `OptionalComponents`, and `world.ValidateEntityCreation` gives the same verdict the engine will. |
| Saving a map hot-reloads the running game | **False, and correctly so.** The watcher watches behaviours directories only (`run.go:120-135`), plus `animations.toml` and the sprites directory. A map takes effect on the next `ecs-db run`. `docs/plan.md`'s own done criterion says exactly that; the design prototype's "hot-reload respawns edited spawns" caption does not, and the UI must not repeat it. |
| `project.Project` knows which map the project has | **False.** `Open` reads `cfg.Map.Path` only to decide `hasMap` for the registry and then drops it (`project.go:105`). The path is not a field. Story 2's. |
| Forge can serve a tileset image to the browser | **False.** `staticHandler` serves the embedded asset FS and nothing else (`server.go:1003`). Drawing a map needs a route that serves project files, and it is the first route in Forge that reads a path from a file the user controls. Story 3's, with the traversal check that implies. |

### The writer is the whole risk, and it is Epic 13 Story 1 again, larger

`tiled.Map` is a *reading* model. It keeps what the engine needs and drops the
rest, which was right for Epic 14 and is fatal for a writer: emitting a file
from it deletes everything it did not keep. Probed against a Tiled-shaped map
carrying an image layer, a layer folder, a polygon object and a text object:

```
map: 2x2 tile 32x32 orientation="orthogonal" renderorder="right-down"
tilesets: 1  layers: 1  objectgroups: 1
  layer id=1 name="ground" vis=true op=1 props=map[]
  group id=3 name="spawns" vis=true objects=2 props=map[]
    obj id=7 name="g" type="Goblin" x=32 y=64 w=0 h=0 gid=0 props=map[Health.hp:{int 5}]
    obj id=8 name="note" type="" x=0 y=0 w=0 h=0 gid=0 props=map[]
```

Everything absent from that dump is absent from the model: `version`,
`tiledversion`, `nextlayerid`, **`nextobjectid`**, `backgroundcolor`,
`<editorsettings>`, the layer's `offsetx`/`offsety`/`parallaxx`/`tintcolor`/
`class`, the object group's `color` and `draworder`, object `<polygon>` and
`<text>` bodies — and the `<imagelayer>` entirely, which is why the dump says one
layer for a map that has two. A second probe confirms `<group>` folders are
flattened: a map whose only layer sits inside a folder parses as a top-level
layer with no record that the folder existed.

So a naive `EmitMap(m)` opened in Tiled would show the author their map with the
sky gone, their folders unpacked, their collision polygons deleted and their
object-id counter reset — the last of which silently retargets an existing
entity the next time the engine loads the file. `ParseMachine`'s "unknown fields
are silently ignored" cost Epic 13 one story to fix. This is the same defect
with an order of magnitude more surface, and it is Story 1 because every story
after it ships a save button.

## Three places the design outruns the engine

Called out here so the stories that hit them are making decisions rather than
discovering surprises.

- **A spawn cannot name a behaviour.** The prototype's spawn inspector opens with
  a `Behavior` dropdown. Nothing reads one: `game.SyncBehaviors` binds a machine
  to an entity by looking up **its entity type's** `behavior` in `schema.json`
  (Epic 14 Story 7), and no object property feeds it — `spawnComponents` refuses
  any property that does not name a component. A per-spawn dropdown would write
  a value the engine never reads, which is the editor-and-game-disagree failure
  the whole approach exists to avoid. Story 7 renders the bound machine
  read-only, attributed to the type, with a link to ENTS where it *can* be
  changed.
- **The four-layer sidebar is prototype furniture.** `Terrain / Props /
  Collision / Objects` describes an engine that does not exist here: there is no
  collision layer, because passability is a per-tile property of the *tileset*
  (Epic 14 Story 2), and "Objects" is not a tile layer but an object group.
  Story 3's panel lists the layers the file actually has, in file order, which
  is the order that decides which tile wins.
- **`QUERY LAYERS ◂ world.sqlite`, the `● LIVE` and `REPLAY` lens positions, the
  timeline dock and the debugger inspector are not this epic.** They are Epics
  18, 19 and 20. The lens control renders — it is the spine of the mode and
  leaving a hole where it goes makes the layout a lie — with LIVE and REPLAY
  disabled and each saying which epic owns it, exactly as the mode stubs do.

## Two rules specific to this epic

**A save must never make the file worse.** Everything Forge did not author stays
byte-for-byte where it was. That is Story 1's contract and every later story
inherits it: a story that cannot express an edit without dropping something
leaves the edit unimplemented and says so.

**An object id is never reused and never renumbered.** `spawns` is keyed
`(map, object_id)`, so an id handed to a new spawn that an old spawn once held
retargets that entity instead of creating one, and renumbering on save moves
every entity in the level onto the wrong object. New ids come from the map's
`nextobjectid` and it only ever goes up.

## Stories

| # | Story | Delivers |
|---|---|---|
| 1 | [TMX writer & round-trip fidelity](01-tmx-writer.md) | A writer that preserves what it does not understand; `nextobjectid` |
| 2 | [Map editing session](02-map-session.md) | Which maps a project has; open, save, discard, reload, conflict |
| 3 | [The map renders](03-map-rendering.md) | Tile grid from tileset images, layer panel, palette, the lens control |
| 4 | [Painting, server-side](04-painting.md) | Stamp, rect, erase, rotate/flip as operations on the session |
| 5 | [Painting, the pointer surface](05-paint-pointer.md) | The second hand-written JS file; drag-paint as one operation |
| 6 | [Spawn placement](06-spawn-placement.md) | Drag a type onto the canvas; move and delete a spawn |
| 7 | [Spawn inspector](07-spawn-inspector.md) | Components, required locks, `ƒ ctx`, property editing; behaviour read-only |
| 8 | [Context menus](08-context-menus.md) | Layers, tiles and spawns, on Epic 13's canvas-menu pattern |
| 9 | [Inline validation](09-inline-validation.md) | Every reason the engine would refuse this map, while you are editing it |

`docs/plan.md` lists five bullets; this is nine. Four differences, all
deliberate:

- **The writer is a story, and the first one.** The plan assumed Epic 14 had
  built one. It did not, and building it as a side effect of the paint canvas
  would settle the round-trip question by accident.
- **The editing session is a story**, as it was in Epics 12 and 13. Maps are the
  hardest case yet: `machines.Session` holds N files that all parse into one
  type, while a map holds a reference to tilesets that are themselves files, and
  `project.Project` does not currently know where any of them are.
- **Rendering is separated from painting**, and painting from its pointer
  surface — the same three-way split Epic 13 used, for the same reason: a canvas
  that draws the wrong map, a canvas that paints the wrong cell and a canvas you
  cannot drag are three unrelated bugs, and only the last needs a browser to
  find.
- **Validation is a story.** Epics 12 and 13 both ended with one and both were
  right to. Epic 14 Story 9 left this epic an explicit inheritance: *"the engine
  cannot tell two maps sharing an id from a rename — it sees one map at a time.
  Forge can, across a project."*

## Process

Per `AGENTS.md`: TDD, a fresh-context code review before each commit, and
`make test` + `make e2e` + the linter green on both tag sets. Each story carries
a **Playwright steps** section written before the code, and an implementation
plan in this directory written before the story is started.
