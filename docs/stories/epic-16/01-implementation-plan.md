# Story 1 — Lossless TSX writer: implementation plan

## Checked in the code

- `internal/tiled/xmltree.go` parses an XML document into a copy-on-write tree.
  `xelem.src` keeps the exact source bytes; marking a changed element dirty
  re-emits only its ancestors, and unaffected child elements keep their own
  source. `ParseDocument` uses it for TMX but explicitly refuses roots other
  than `<map>`. A new `TilesetDocument` should reuse the tree rather than
  generalizing the map-specific methods into a cross-format type.
- `ParseTileset(data,name,dir)` already reads both TSX and TSJ, and an embedded
  tileset; it is the source of truth for validity and the readback model. Its
  `Tileset.Tiles` map includes only tiles with authored metadata, while a sheet
  also has implicit local IDs `0..TileCount-1`; a collection has sparse IDs and
  per-tile images. Mutators must distinguish these cases.
- `xelem.appendChild`, `setAttr`, `setText` and `removeChild` already preserve
  unrelated source. `Document.SetObjectProperty` has the refusal and
  attribute-vs-element spelling rules needed for a TSX property. Extract a
  narrow shared property helper **with old tests still green**, or use an
  equally tested tileset-owned implementation; do not write a second,
  incompatible way to change nested Tiled property values.
- Tile `type` and `class` both parse into `TilesetTile.Type`. The spelling in
  the TSX is preserved by the source tree and should stay as authored on edit.
  The tileset-level `class` is a different field.

## Red–green sequence

1. Write `internal/tiled/tileset_document_test.go` with a rich TSX carrying
   prolog/comment, sheet metadata, `wangsets`, `terraintypes`, editor settings,
   a tile's collision `<objectgroup>` and `<animation>`, and sparse collection
   tiles. First assert exact unchanged bytes and refusal of `.tsj`, a wrong
   root and a tileset `ParseTileset` rejects. Confirm tests fail for missing
   `ParseTilesetDocument`/`TilesetDocument` before adding them.
2. First test changing an existing `passable` property from true to false:
   only the value bytes may differ. Assert `Tileset()` reads false and a
   second identical assignment is byte-idempotent. Test both value-attribute
   and element-content spellings, escaping, nested-content refusals and
   duplicate properties/IDs. Add the minimum mutator or shared helper.
3. First test adding metadata to a previously implicit sheet tile and editing
   a sparse collection tile. New `<tile id>` nodes belong among tile siblings
   in ID order, not after wangsets; the collection must never invent missing
   IDs or replace an image child. Test boundary and malformed targets leave
   `Bytes()` identical on refusal.
4. First test a tile with `class=` and one with `type=`, plus a tileset-level
   class, to keep the two class scopes and spellings distinct. Add the
   tile-class mutator only after those tests fail.
5. Run targeted `go test -cover ./internal/tiled`, `make test`, both lint tag
   sets and both builds. Review the staged diff with a fresh-context reviewer,
   fix findings, rerun affected checks and commit Story 1. There is no Forge
   browser change in this story, so its first Playwright spec is Story 3's.

## Review follow-up

- A child edit used to respell untouched ancestor attributes. `xtree` now
  preserves each source opening/closing tag and only changes the attribute
  value being edited, retaining single quotes and character references even
  on the target element. The self-closing-to-parent transition keeps a valid
  open tag when adding a first child.
- Dual `type`/`class` attributes are refused before mutation, and sheet tile
  IDs beyond `tilecount` are refused even if a hand-authored `<tile>` exists.
- An unchanged element-content property keeps its original XML entity
  spelling (`&#38;` stays `&#38;`), in both the TSX and shared TMX writer.
- Multiple `<properties>` blocks on the same tile, object or map are refused
  before mutation. Editing the first block while the parser reads the last
  would report success and leave the engine seeing the old value.
