# Story 1 — implementation plan

**Story:** [TMX writer & round-trip fidelity](01-tmx-writer.md)

## What was verified first

Every claim the story makes was re-checked against the code before planning.

| Claim | Verified |
|---|---|
| Nothing in the repository writes a `.tmx` | True — no `Emit`/`Write`/`Marshal`/`Encode` in `internal/tiled` or `internal/tilemap`. |
| `tiled.Map` drops most of a Tiled file | True — probed. `nextobjectid`, `nextlayerid`, `version`, `backgroundcolor`, `<editorsettings>`, layer `offsetx/offsety/parallaxx/tintcolor/class`, object-group `color`/`draworder`, object `<polygon>`/`<text>` bodies, and `<imagelayer>` entirely. |
| `<group>` folders are flattened | True — probed: a map whose only layer sits inside a folder parses as one top-level layer with no record the folder existed (`collectXML`, `tmx.go`, recurses and appends to the flat list). |
| `spawns` is keyed `(map, object_id)` | True — `storage/tables.go:57`. A reused id retargets a live entity. |
| `editable.File` writes through `atomicfile` | True — `editable.Save`, and `atomicfile.Write` delegates to `natefinch/atomic`. |
| `Object.Type` may arrive from either `type` or `class` | True — `convertObjectGroup` folds them; Tiled 1.9 renamed it and 1.10 renamed it back. |
| The engine reads four tile-data encodings | True — CSV, base64, base64+zlib, base64+gzip, plus the encoding-less `<tile>` element form. `zstd` and infinite maps are refused by name. |

One new fact, probed for this story and load-bearing for the design:

**`xml.Decoder.InputOffset()` gives exact byte ranges, and a self-closing tag is
detectable.** Walking `<tileset firstgid="1" source="s.tsx"/>` yields a
`StartElement` spanning `[79,117)` and an `EndElement` spanning `[117,117)` — a
zero-length end range is the signal, and there is no other way to tell `<a/>`
from `<a></a>` through the token API. Everything below rests on this.

## The decision: a copy-on-write document, not a model

The story asks for the choice to be recorded. Three were considered.

**Emit from `tiled.Map`.** Rejected — this is the failure the story exists to
prevent. It also cannot be fixed by extending the model: "lossless" would be a
promise re-made every time Tiled adds an element, and broken silently.

**Byte-range splices over the original bytes.** Attractive because a no-edit
save is trivially byte-identical, and rejected because reordering and deleting
layers are overlapping splices, which is where this kind of code goes wrong.

**A copy-on-write XML tree, chosen.** Parse into a tree where every element
records the exact source bytes it came from. Rendering an element writes those
bytes verbatim — *unless* it or a descendant was edited, in which case it
re-renders from its attributes and children, and each of *its* children is again
verbatim unless edited. So:

- a save with no edit is byte-identical, because nothing is dirty;
- a save with one edit is byte-identical everywhere except the smallest element
  containing that edit;
- "preserve what I do not understand" is the default rather than a list.

That last point is what makes this different from every alternative: an
`<imagelayer>`, a `<group>`, a `<polygon>`, an attribute Tiled has not shipped
yet — none of them need to be modelled, because nothing ever asks what they are.

## Shape

New files in `internal/tiled`:

- **`xmltree.go`** — the generic part, ~200 lines, no Tiled knowledge at all.
  - `type xnode` — either verbatim bytes (whitespace, comments, chardata,
    directives, the `<?xml?>` prolog) or an `*xelem`.
  - `type xelem struct { name string; attrs []xattr; kids []xnode; selfClose bool; src []byte; dirty bool; parent *xelem }`
  - `parseTree(data []byte) (*xtree, error)` — one token walk, offsets from
    `InputOffset()`.
  - `render(w)` — verbatim when clean.
  - `markDirty()` — walks to the root, because an ancestor holding a changed
    child cannot be written verbatim.
  - attribute helpers: get, set (in place, preserving order), append (for one
    that was absent), and the escaping Tiled uses.
- **`document.go`** — the Tiled-facing type.
  - `ParseDocument(data []byte, name string) (*Document, error)` — builds the
    tree *and* runs `Parse`, so a `Document` is always a map that reads.
  - `(*Document) Bytes() []byte`
  - `(*Document) Map() (*Map, error)` — memoised; every edit invalidates it.
  - `(*Document) NextObjectID() int`, `AllocateObjectID() (int, error)`,
    `AllocateLayerID() (int, error)`
    - **Built differently.** `allocateObjectID() int`, unexported and
      infallible: it has no way to fail, and exporting it would let a caller
      take an id without placing an object. There is no layer-id allocator —
      see *Decisions taken up front* and the story's As Implemented.
  - `(*Document) SetLayerData(index int, gids []uint32) error`
  - `(*Document) AddObject(group int, obj Object) (int, error)`
  - `(*Document) RemoveObject(id int) error`
  - `NewDocument(opts) (*Document, error)` — a map from nothing, for Epic 17.
- **`encode.go`** — the encoders mirroring `data.go`'s decoders: CSV in Tiled's
  own shape, base64, base64+zlib, base64+gzip, and the `<tile>` element form.

And in `internal/forge/mapfile` — a 30-line seam so this story's guarantee is
proven where it will actually be used: `Codec()` returning
`editable.Codec[*tiled.Document]`, and `Open(path)` returning an
`*editable.File[*tiled.Document]`. That is what makes "writing is atomic"
testable now rather than an assertion deferred to Story 2, and it is the value
Story 2's session will hold.

## Ordering

1. `xmltree.go` and its tests, alone. It is the part that has to be exactly
   right and it has no Tiled in it — a tree that round-trips arbitrary XML
   byte-identically, and re-renders only what was touched.
2. `encode.go` and its tests: encode → `decodeLayerData` → the same gids, for
   every encoding, plus the CSV shape matching what Tiled writes.
3. `document.go`: parse, `Bytes`, `Map`, then the three edits.
4. `internal/forge/mapfile`, and the `editable` round trip.
5. The fidelity battery: `level1.tmx` and a fixture carrying every construct the
   reader drops, both byte-identical through a no-edit save, and both preserved
   under each edit.

## Decisions taken up front

- **`.tmj` is not written.** Reading stays; editing a `.tmj` is refused by name,
  pointing at Tiled's *File ▸ Save As ▸ .tmx*. Writing JSON with the same
  guarantee means a second document implementation with a second set of
  fidelity tests, and converting a `.tmj` to `.tmx` on save would silently
  change a project's file layout and break the `game.toml` that names it.
  Recorded in the package doc, not just here.
- **`Map()` is memoised and every edit invalidates it**, including any
  `ResolveTilesets` done on the returned value. Resolution therefore does not
  survive an edit, and Story 2's session owns re-resolving. Documented on the
  method, because it is a trap otherwise.
- **The returned `*Map` is owned by the document.** Mutating it does not change
  the file and will be discarded by the next edit. Edits go through the
  document.
- **`AllocateObjectID` moves `nextobjectid` and never lowers it**, and there is
  no API that renumbers. A map with no `nextobjectid` attribute — a hand-written
  one — gets it computed as one past the highest object id in the file, and the
  attribute added.
  - **Corrected after review.** This was not enough: the attribute was believed
    whenever it parsed, so a counter that had *fallen behind* its objects handed
    out an id already in use. The objects are scanned every time now, and the
    answer is the higher of the two. See the story's As Implemented.
- **`NewDocument` requires a `mapId`.** Epic 14 Story 9 made spawn identity a
  map property; a map Forge wrote without one would be Forge shipping the trap
  it warns about.

## Risks

- **A tree that renders "mostly" verbatim is worse than one that does not try.**
  The mitigation is the test that matters: byte-identical, asserted on real
  files, including one deliberately full of constructs nothing models.
- **`markDirty` propagating upward means one edit re-renders the root.** That is
  correct and cheap — the root re-renders its own tag and then copies each clean
  child verbatim — but it must not be mistaken for "the whole file is
  regenerated". The test for it is an edit deep in one layer leaving every other
  layer's bytes identical.
- **Namespaces.** Tiled writes none. `xml.Decoder` resolves prefixes and would
  let a namespaced document render differently from its source; refuse a
  document containing a namespace declaration rather than silently rewriting it.
