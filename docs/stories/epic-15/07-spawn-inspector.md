# Story 7: Spawn inspector

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** 🔲 Not started  
**Priority:** Medium — placement without editing is half a spawn

**Depends on:** Story 6

## Context

The right-hand panel for a selected spawn: which components it has, which are
locked because its type requires them, which are seeded by its machine's
context, and what its properties are set to. Adding an optional component,
detaching one, and editing a value all write `Component.property` object
properties, which is exactly the convention Epic 14 Story 6 defined and
`spawnComponents` reads.

**One thing in the design prototype does not exist, and it is the first control
on the panel.** The mock opens with a `Behavior` dropdown on the spawn. Nothing
reads a per-spawn behaviour: `game.SyncBehaviors` binds a machine to an entity
by looking up **its entity type's** `behavior` in `schema.json` (Epic 14
Story 7), and no object property can feed it — `spawnComponents` refuses every
property that does not name a component. A dropdown here would write a value the
engine never reads, and the author would find their goblin running the wrong
machine with the editor insisting otherwise.

So behaviour is shown, attributed, and read-only, with the place it can actually
be changed one click away. That is a smaller feature than the mock and a true
one.

## Acceptance Criteria

- [ ] The inspector shows the selected spawn's entity type and object id
- [ ] Components required by the type carry the `🔒` and cannot be detached;
      optional ones carry `✕` and can
- [ ] `+ ADD COMPONENT` offers exactly what the type permits — its optional
      components, and anything at all only when the type sets
      `allowExtraComponents`
- [ ] Property values are editable and written as `Component.property`, typed as
      the schema declares
- [ ] A scalar component is addressed by its storage column, not by an invented
      property name — `Label.value`, not `Label.text` — because that is what the
      engine reads and what `SetComponentValues` requires
- [ ] `Position` is shown as the cell the object sits in, read-only, with the
      reason: moving the object is the gesture that moves the spawn
- [ ] A field the bound machine seeds carries the purple `ƒ ctx` badge, from
      `MachineDefinition.ContextManifest`, and says what it means
- [ ] **Behaviour is read-only**, named from the entity type, attributed to
      `schema.json`, with a link to ENTS
- [ ] A type whose bound machine seeds a component the type forbids is warned
      about here, because that is the refusal `game.SyncBehaviors` will make at
      load and this is where it is visible before it happens
- [ ] `Sprite.sheet` is not offered as an editable field, or is offered with the
      warning that `animations.toml` overrides it at start-up
- [ ] An edit that would make the spawn invalid is refused with the engine's own
      verdict, from `world.ValidateEntityCreation` — the same function that will
      judge it at load
- [ ] A type at `validationLevel: "warning"` shows the warning and allows the
      edit, because that is what the schema said to do
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/15-spawn-inspector.spec.js`.

- [ ] Selecting the shipped goblin shows `Goblin`, its object id, and its
      components
- [ ] `Position` is present and not editable
- [ ] `Health` is locked; an optional component is detachable
- [ ] Editing `Health.hp` dirties the map, saves, and the engine loads the new
      value
- [ ] `+ ADD COMPONENT` offers only what `Goblin` permits
- [ ] The behaviour readout names `goblin`, is not a dropdown, and links to ENTS
- [ ] A field the goblin machine seeds carries the `ƒ ctx` badge

## Notes

- **The inspector's verdicts must be the engine's.** Every judgement on this
  panel — required, optional, permitted, refused — comes from
  `world.ValidateEntityCreation` and the schema, never from a rule written here.
  A second implementation of the entity contract is a second thing to be wrong.
- Epic 12 Story 5 already renders context seeds for ENTS. Reuse that component
  rather than writing a second one; the badge means the same thing in both
  places and it should look like it.
