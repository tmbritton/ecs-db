# Story 1: TMX writer & round-trip fidelity

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** ✅ Complete  
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

- [x] Forge can write a `.tmx` that Tiled opens without complaint
- [x] **A load-and-save with no edit is byte-identical**, for `level1.tmx` and
      for a map carrying every construct the parser drops
- [x] An edited map keeps every construct Forge does not model: image layers,
      `<group>` folders and their nesting, object shapes (`polygon`,
      `polyline`, `ellipse`, `point`, `text`), templates, `<editorsettings>`,
      `backgroundcolor`, layer `offsetx`/`offsety`/`parallax`/`tintcolor`/
      `class`, object-group `color` and `draworder`, and any attribute or
      element this list forgot
- [x] `nextobjectid` and `nextlayerid` are preserved and maintained: placing an
      object takes `nextobjectid` and increments it, and neither counter ever
      goes down
- [x] An id is never reused and objects are never renumbered on save
- [x] Writing is atomic — `atomicfile`, as every other Forge write is, so the
      engine or Tiled never sees half a map
- [x] A map Forge writes parses back through `tiled.Parse` to a `tiled.Map`
      equal to the one it was asked to write
- [x] The tile-data encoding a file arrived in is the encoding it leaves in —
      a CSV map does not silently become base64+zlib, because that is an
      unreviewable diff in someone's repository
- [x] `.tmj` is decided explicitly: either written with the same fidelity
      guarantee, or not offered at all, with the reason recorded. A half-written
      second serialisation is worse than one
- [x] `go test ./...` passes

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

## As Implemented

**A copy-on-write document, not a model.** `internal/tiled/xmltree.go` parses a
map into a tree in which every element remembers the exact bytes it was read
from. Rendering writes those bytes back — unless the element or something inside
it was edited, in which case it is written from its attributes and children and
each of *those* is again verbatim unless edited. So a save with no edit is
byte-identical, a save with one edit differs only inside the smallest element
containing it, and an `<imagelayer>`, a `<group>`, a `<polygon>` or an attribute
Tiled has not shipped yet survives without anything here knowing it exists.

That last property is why this is a tree of bytes rather than a struct.
"Preserve what I do not understand" is the default instead of a list somebody
maintains. The alternative — extending `tiled.Map` until it were lossless — is a
promise re-made every time Tiled adds an element, and broken silently.

It rests on one fact, probed rather than assumed: `xml.Decoder.InputOffset()`
gives exact byte ranges, and a self-closing tag is detectable because the
decoder synthesises its `EndElement` at the position it already stands at, so
the end token spans zero bytes. There is no other way to tell `<a/>` from
`<a></a>` through the token API, and a re-encoder that guesses gets one of them
wrong in every file.

**The measurable form of the claim:** painting one cell of `mods/map/level1.tmx`
and saving through `editable.File` changes **exactly one line** of the file —
asserted, along with the map's long leading comment surviving, in
`internal/forge/mapfile`.

**`internal/forge/mapfile` is the seam**, and it is here rather than in Story 2
so that "writing is atomic" is proven now instead of asserted for later. It is a
codec and an `Open`, with no map logic in it. Its existence also makes the
dependency explicit in both directions: `editable.File` decides "unsaved" by
serialising and comparing, which is only meaningful because an unedited document
renders back byte-for-byte — so a writer that stopped being faithful would show
up as a map that is permanently unsaved for no visible reason.

**Decisions taken and recorded in the code:**

- **`.tmj` is not written.** Reading it still works; editing one is refused by
  name, pointing at Tiled's *File ▸ Save As ▸ .tmx*. A second serialisation
  means a second document implementation with its own fidelity tests, and
  converting a `.tmj` to `.tmx` on save would silently change a project's file
  layout and break the `game.toml` that names it.
- **`Map()` is memoised and every edit invalidates it**, including any
  `ResolveTilesets` performed on the returned value. Resolution does not survive
  an edit; Story 2's session owns re-resolving.
- **There is no layer-id allocator.** `nextlayerid` is preserved verbatim, which
  is all this story needs; handing one out belongs to the story that adds a
  layer, and an exported function with no caller is a promise nobody has tested.
- **`AddObject` ignores `obj.ID`** so no caller can choose one, and ignores
  `obj.Visible` — Tiled's default is visible and it writes no attribute for it,
  so a zero-valued `Object` is a request for an ordinary spawn rather than an
  invisible one.

Found by review, and both real:

- **A `nextobjectid` that had fallen behind was believed**, so a map saying `2`
  with an object `2` already in it handed out `2` again. Because `spawns` is
  keyed `(map, object_id)`, the second object does not create an entity — it
  moves the one the first made, machine and transitions and all. And it is
  reachable without anyone hand-editing anything: two branches each place a
  spawn, the objects merge cleanly on different lines, and git takes one side of
  the one-line counter. `NextObjectID` now scans the objects every time and
  returns the higher of the two, so the guarantee is a property of the file
  rather than of its bookkeeping being right.
- **A prefixed attribute silently lost its prefix.** The namespace guard refused
  namespaced element names and `xmlns` declarations, and not `xml:space` or
  `xlink:href`, whose prefix Go reports as `Name.Space` and which the tree
  dropped. Exposure was maximal because the root is re-rendered by *every* edit
  — the object counter lives on it — so `<map xml:space="preserve">` became
  `<map space="preserve">` on the first spawn placed. It also falsified
  `parseTree`'s own doc comment. Refused now, with the well-known `xml`
  namespace named back from its URL so the message says what the file says.

Also from review: the remove-path fidelity test compared four substrings and now
compares the whole document; object-group flattening through a `<group>` folder
had no test to match the layer one; `Document.Name()` was exported with no
caller and is gone; `formatCoord` used `'g'`, which writes a coordinate past a
million in exponent notation.

## Notes

- **This story's contract is inherited by every story after it.** Everything
  Forge did not author stays byte-for-byte where it was. A story that cannot
  express an edit without dropping something leaves the edit unimplemented and
  says so.
- **`markDirty` rests on an unwritten invariant** — a dirty element's ancestors
  are all dirty — which is now written on it. Every mutator maintains it by
  going through `markDirty`; code that reaches into `kids` directly must call it
  itself, which `SetLayerData`'s element-form path does.

### Left undone, deliberately

- **`Map()` re-parses the whole file after every edit**, and `editable.Dirty()`
  re-renders it. Measured at 33 ms for `SetLayerData` plus `Map()` on a 500×500
  map, of which `Bytes()` is 49 µs — the cost is `Parse`, not the tree. Nothing
  here is on a hot path yet. `editable`'s "these files are kilobytes" is the
  assumption that stops holding first, and Story 3's canvas is where it will
  show.
- **Only `SetLayerData`, `AddObject` and `RemoveObject` exist.** Moving an
  object, editing its properties, renaming and reordering layers and setting map
  properties are Stories 6, 7, 8 and 9, each of which re-proves this story's
  guarantee for the edit it adds.

### The battery

49 mutations over `xmltree.go`, `encode.go` and `document.go`, run three times.
The first pass caught 35 of 39 and found a real defect the tests had not: the
`<properties>` block was filled before it was attached to its object, so every
property and its closing tag were written hard against the left margin of a file
indented three levels in. Two of the four survivors were dead code — `encoding/
xml` refuses an unclosed root before the guard for it ran, and `setText`
clearing `selfClose` is redundant against `render`'s own children check — and
the other two showed the round-trip fixtures were missing every shape that makes
verbatim copying load-bearing at all: single-quoted attributes, generous
whitespace inside a tag, numeric character references. Third pass: 49 of 49,
no survivors.
