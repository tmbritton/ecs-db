# Story 1: TMX writer & round-trip fidelity

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** 🔲 Not started  
**Priority:** Highest — every story after this one ships a save button

**Depends on:** Epic 14 (the parser it writes against)

## Context

`docs/plan.md` lists this epic's engine touchpoint as "Epic 14's TMX writer
path". There is no writer. `internal/tiled` parses and resolves; nothing in the
repository has ever produced a `.tmx`, and `mods/map/level1.tmx` was written by
hand.

That would be a small gap if `tiled.Map` were a faithful model of a Tiled file.
It is not, and was never meant to be — Epic 14 built a *reading* model that keeps
what the engine needs. Probed against a map with an image layer, a layer folder,
a polygon object and a text object, everything outside the engine's interest is
simply absent: `nextobjectid`, `backgroundcolor`, `<editorsettings>`, layer
offsets, parallax, tint, the object group's `color` and `draworder`, object
shapes and text bodies, and the image layer entirely. `<group>` folders are
flattened.

So the obvious writer — walk the model, emit XML — hands the author back a file
with their sky deleted, their folders unpacked and their collision polygons
gone. The `nextobjectid` loss is worse than cosmetic: reset it and the next
spawn Forge places takes an id an existing spawn already holds, and `spawns` is
keyed `(map, object_id)`, so the engine retargets that entity instead of making
one.

This is Epic 13 Story 1 again — "unknown fields are silently ignored" — with an
order of magnitude more surface. It gets the same treatment: settle it before
anything is built on top of a format that cannot be re-written.

## Acceptance Criteria

- [ ] Forge can write a `.tmx` that Tiled opens without complaint
- [ ] **A load-and-save with no edit is byte-identical**, for `level1.tmx` and
      for a map carrying every construct the parser drops
- [ ] An edited map keeps every construct Forge does not model: image layers,
      `<group>` folders and their nesting, object shapes (`polygon`,
      `polyline`, `ellipse`, `point`, `text`), templates, `<editorsettings>`,
      `backgroundcolor`, layer `offsetx`/`offsety`/`parallax`/`tintcolor`/
      `class`, object-group `color` and `draworder`, and any attribute or
      element this list forgot
- [ ] `nextobjectid` and `nextlayerid` are preserved and maintained: placing an
      object takes `nextobjectid` and increments it, and neither counter ever
      goes down
- [ ] An id is never reused and objects are never renumbered on save
- [ ] Writing is atomic — `atomicfile`, as every other Forge write is, so the
      engine or Tiled never sees half a map
- [ ] A map Forge writes parses back through `tiled.Parse` to a `tiled.Map`
      equal to the one it was asked to write
- [ ] The tile-data encoding a file arrived in is the encoding it leaves in —
      a CSV map does not silently become base64+zlib, because that is an
      unreviewable diff in someone's repository
- [ ] `.tmj` is decided explicitly: either written with the same fidelity
      guarantee, or not offered at all, with the reason recorded. A half-written
      second serialisation is worse than one
- [ ] `go test ./...` passes

## Playwright steps

None. This story has no UI. It is proven in Go, and the acceptance criterion
that matters — *byte-identical* — is a test no browser can run.

## Notes

- **The likely shape is a document, not a model.** Keep the source XML and
  rewrite only the parts that changed, rather than regenerating from
  `tiled.Map`. That inverts the usual instinct and is what makes "preserve
  everything I do not understand" the default instead of a list to maintain.
  The alternative — extending `tiled.Map` until it is lossless — is a promise
  that has to be re-made every time Tiled adds a feature, and it will be broken
  silently. Weigh both in the implementation plan and record the choice; the
  criterion above is a property, not an implementation.
- **A brand-new map has no source document.** Whatever the answer is, creating a
  map from nothing has to produce a valid file — that path is Epic 17's New Map
  dialog, but the writer it calls is this one, so leave it reachable.
- **A map Forge creates must declare `mapId`.** Epic 14 Story 9 made spawn
  identity a map property and warned about a map without one; a map *Forge*
  writes without one would be Forge shipping the trap. Story 9 validates it;
  the writer should not be able to omit it by accident.
- The engine does not watch map files, so nothing here is about hot reload. A
  saved map takes effect on the next `ecs-db run`, and the UI says so rather
  than repeating the design prototype's "hot-reload respawns edited spawns".
