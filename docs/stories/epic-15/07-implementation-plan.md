# Story 7 — Spawn inspector: implementation plan

## Verified before planning

- Story 6 selects an object by `?spawn=<id>`; `addMapData` resolves it from the
  working map and `streamQuery` keeps it across SSE patches. The current
  `spawnInspector` in `map.templ` shows only type/id and Delete. Extend that
  panel rather than creating another selection mechanism.
- `tiled.Document` is copy-on-write and already edits object coordinates, but
  has **no object-property mutators**. Editing through `Document.Map()` changes
  only the cached reading model and loses the edit on the next document change.
  Add narrowly scoped property set/remove methods that preserve unknown XML,
  attributes, other objects and `nextobjectid`.
- `tilemap.spawnComponents` is the engine's parser for `Component.property` and
  scalar storage-column names; `checkSpawn` then calls
  `world.ValidateEntityCreation`. Both are private. The world validator checks
  component *sets*, not field values or Tiled property spellings. Reuse the
  importer when validating a prospective edit, otherwise Forge can call the
  right function on the wrong set and still save a spawn the engine refuses.
- Required/optional/extra policy comes from `schema.EntityType` and the engine
  verdict. `Position` is implicit from the object's coordinates; a property
  attempting to set it is refused by the engine. `Sprite.sheet` is stamped at
  startup by `animations.toml` and must not appear as a normal editable value.
- ENTS already has `contextSeedsPanel(data, et)` and `seedsFor`, which use
  `project.Machine.Definition.ContextManifest`. Use that panel in the spawn
  inspector and the same manifest for a field-level `ƒ ctx` badge. The schema
  being edited may differ from the startup manifest, so stale mappings need a
  warning, not a confident badge.
- Story 6's placement seeds required non-Position components with typed zero
  values. Existing Tiled objects need not have them; show the actual file state,
  never fabricate properties in the inspector until the author explicitly adds
  or edits them.
- `maps.Session.Edit` holds a mutex but does not roll back after a callback
  returns an error. Construct and validate the candidate *before* mutating the
  XML tree. A refused edit must leave `Document.Bytes()` unchanged.

## Design

1. Domain (`internal/forge/spawn`): project the object's component names and
   fields in schema order, including implicit `Position`; compute permitted
   additions from the type's optional list plus extras when allowed. A
   required chip is locked; detaching an optional one removes all of its
   `Component.property` entries. A scalar field uses
   `schema.LayoutColumnName(schema.StorageLayout(...))`, not an invented alias.
2. Document (`internal/tiled`): add/replace one object property and remove only
   one component's prefixed properties. Keep unrelated elements verbatim and
   preserve existing spelling/placement where possible. Refuse ambiguous
   duplicate IDs, missing objects and malformed property names before writing.
3. Validation: export a small engine-owned validation entry point in
   `internal/tilemap` wrapping `spawnComponents` and
   `world.ValidateEntityCreation`; use it on the candidate for create, attach,
   detach and field edits. Strict errors refuse; `validationLevel:warning`
   warnings are rendered but do not block. A field value is converted to the
   schema's Tiled type before it reaches the document; JSON object/array
   values and references follow the importer's existing parsing rules.
4. Rendering: extend the existing MAP footer inspector, keep field controls
   inside its own patchable region, use `components.Chip` and reuse
   `contextSeedsPanel`. Behaviour is an attributed read-only value from the
   entity type, linked to `/forge/ents?type=...`. `Position` reads as a cell and
   says to move the object. Badge seeded fields from the manifest and warn
   when the bound machine would attach a component the type disallows.
5. Routes: `POST /forge/map/spawn/component` (add/detach) and
   `POST /forge/map/spawn/property` (set); carry exact map and object id.
   Reject forged type/component/property names against the live editable schema,
   show refusals in MAP's error region, and keep a successful no-op clean.

## Test-first sequence

1. `internal/tiled/document_test.go`: failing fidelity tests for add, replace,
   detach and refusals (including duplicate id, comments, object children and
   byte-identical no-op). Implement minimal XML mutators.
2. `internal/forge/spawn/*_test.go`: table-driven tests for required/optional/
   extras, scalar columns, typed values, Position and Sprite exclusions,
   strict refusal vs warning acceptance, and context-seeded components. Add
   the engine validation seam with its own direct test showing the importer
   reads exactly what the inspector writes.
3. `internal/forge/server/spawnedit_test.go`: route tests against the actual
   map/session for add/detach/set, forged parameters, rejected edits leaving
   bytes and dirty state unchanged, warnings remaining visible.
4. Templ rendering tests pin all Playwright `data-testid`s and check locked and
   removable chips, field-level badges, behaviour attribution, the context
   panel, warning text and read-only Position.
5. Write `e2e/specs/15-spawn-inspector.spec.js` **before** browser wiring:
   select a placed fixture goblin; assert component controls, edit Health.hp,
   save and import through the fixture's engine spawn path; test optional add
   and detach and a bound machine's context badge. Include an explicit
   accessibility block; deliberately break an event handler and watch a
   request/effect assertion fail before trusting the spec.
6. Run `make test`, both linter build-tag sets, `make build`,
   `make build-headless`, `make e2e`; request fresh-context sub-agent review of
   the staged diff, fix findings, then commit. Mark the story and `docs/plan.md`
   only after acceptance criteria and browser steps are verified.

## As implemented

- `Document.SetObjectProperty` and `RemoveObjectComponent` edit the copy-on-write
  XML tree in place. They refuse ambiguous ids, missing objects and nested
  values they cannot preserve. Attribute-form values keep comments and unknown
  children; element-content values stay element-content rather than becoming
  conflicting attribute/text pairs. An unchanged value is byte-idempotent.
- `tilemap.ValidateSpawn` reuses the importer's property parser and
  `world.ValidateEntityCreation`; the domain constructs a candidate object and
  calls it *before* touching XML. A strict refusal leaves the session unchanged;
  warning-level extras are written and remain visibly warned about. Scalar
  fields use `schema.StorageLayout` / `LayoutColumnName` so a Label is written
  as `Label.value` and not an invented property.
- The inspector's rendering is a projection of the actual object and the
  editable schema. `Position` is the object's coordinates, not a custom
  property; `Sprite.sheet` is read-only with the startup override explained.
  ENTS's `contextSeedsPanel` is reused, and the manifest marks individual
  fields and warns when the bound machine seeds a forbidden component.
- A full inspector underneath the map shrank the canvas and was not the design.
  MAP has a fifth, independently patched `map-inspector` region beside the
  canvas. A browser measurement checks the canvas remains usable and the panel
  really is beside it; the stream-count test now expects one initial patch for
  that additional region, not an unexplained extra patch.
- The browser test for Health.hp waits for the actual POST before saving.
  `fill()` followed by a synthetic change had sent two requests; the second
  sometimes arrived after the fixture cleanup and produced a spurious refusal.
  The saved TMX is imported through the engine's `SyncSpawns` in a fresh DB,
  with the new hp asserted from its component row.
- A deliberate mutation replaced the Health.hp change handler with a no-op;
  the browser test failed waiting for its request. The authored fixture map
  begins with no objects to keep Story 6's id tests deterministic; a Go render
  test also opens the actual shipped `mods/map/level1.tmx` goblin (object 2),
  while browser tests place a goblin in the fixture and exercise the wiring.

## Review follow-up

- An element-content TMX property held raw XML in the parser, while the writer
  escaped text. Editing `A & B` would save `A &amp; B` and the engine read the
  escaped spelling as its value. The reader now decodes text entities and keeps
  nested markup raw; a property with an explicit `value=""` does not take an
  adjacent XML comment as its value. Round-trip tests pin all three cases.
- Optional entity references needed an explicit target before they could be
  attached. The inspector offers a target-id input beside the add control, and
  the domain refuses missing/invalid/zero ids before changing the document.
  A positive id must still refer to an entity when the game imports the map.
- Component names in Tiled properties may differ in case. The inspector now
  resolves them through the schema's canonical lookup before deciding whether
  `Health` is attached; a mixed-case fixture would previously show one unknown
  `health` and one missing required `Health`.
- A strict object with two missing required components could repair neither
  individually: each interim state failed `ValidateEntityCreation`. One repair
  action seeds every wholly absent required component, validates the combined
  candidate, then writes them in a stable order. It refuses a required
  reference whose target cannot be invented rather than making a partial edit.
- Context seeds attach *missing components only*; they do not overwrite an
  existing component's map value at startup. The badge now says that, and the
  warning for a machine that seeds a forbidden component uses
  `world.ValidateAttachComponent`, matching the engine's strict refusal versus
  warning-level proceed-with-warning behavior. Both branches have Go and
  browser assertions.
- Review also exposed an engine-contract gap behind "detach": removing an
  optional component's TMX properties removed it for *new* entities but left
  the row on any entity already imported. The engine now records a sorted
  authored-component snapshot in `spawns.components`, in the same transaction
  as the entity's import. Re-import deletes only a component that appeared in
  that object's prior authored snapshot and is now absent; runtime-attached
  components that were never in the file remain. Existing rows get a NULL
  snapshot on migration and take a baseline on first import, without guessing
  that a runtime component was formerly authored. The mapId adoption path
  carries this snapshot to the new key; the old-foreign-key rebuild preserves
  it when present. Engine integration tests pin deletion, preservation,
  baseline and adoption independently.
- An attached reference could be edited to zero or a negative id, which the
  importer parses as an integer but SQLite cannot reference. Field edits now
  require a positive target just as attachments do. Object components with
  several entity-reference fields collect one explicit target per field before
  writing anything; both Go and browser tests pin this path.
