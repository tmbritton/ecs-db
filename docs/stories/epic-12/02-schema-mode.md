# Story 2: SCHEMA mode — components and their fields

**Epic:** 12 — Forge: SCHEMA & ENTS modes  
**Status:** 🔲 Not started  
**Priority:** High — the first mode that edits anything

**Depends on:** Story 1

## Context

The left rail lists the components; the centre edits the selected one. A component has a shape (`object`, `entity-ref`, `array`, or one of the four scalars), an optional bound behaviour machine, and — when it is an object — a table of fields that become typed SQL columns.

Order matters here in a way it usually does not. `Position, Health, Sprite` sit together in the file because they belong together, and Epic 11 Story 2 went to some trouble to preserve that. The list must render in `ComponentOrder`, and a new component must land where the ordering rule says it will, or the first save reorders a file the user did not reorder.

`"Behavior"` is a reserved component name — `ValidateSchema` rejects it case-insensitively — and the editor should say so at the point of typing rather than at the point of saving.

## Acceptance Criteria

- [ ] Component list in **authored order**, not alphabetical, with the `⛃` glyph and a `ƒ` badge on components that bind a behaviour
- [ ] `v<N>` schemaVersion badge; clicking it bumps the version, with the consequence stated (it is a migration marker, not a label)
- [ ] Selecting a component shows its editor; the selection is a URL (`/forge/schema?component=Position`) so it survives a reload and can be linked
- [ ] Shape control offering exactly the shapes the engine supports: `object`, `entity-ref`, `array`, `string`, `integer`, `number`, `boolean`
- [ ] Behaviour binding: a dropdown of machines resolved from the project's mods, plus "none"
- [ ] Fields table for object components — name, type, NOT NULL, delete — rendering in **authored property order**
- [ ] Add field, delete field, rename field, change type; each marks the session dirty
- [ ] `"Behavior"` is refused as a component name, in the editor, with the reason — not silently accepted and rejected on save
- [ ] Adding a component places it where the ordering rule says (appended, sorted among other new ones), and this is asserted against the saved file
- [ ] Delete component, behind a confirmation that names what it will do
- [ ] Non-object shapes do not show a fields table; they have no properties
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/12-schema-mode.spec.js`.

- [ ] The list renders in authored order — assert `Position` precedes `Health`, which alphabetical would reverse
- [ ] Selecting a component updates the URL, and reloading that URL keeps the selection
- [ ] Adding a field marks the footer dirty and the field appears in the table
- [ ] Renaming a field to `Behavior`... is fine — the reservation is on *component* names; assert a **component** named `Behavior` is refused with a visible reason
- [ ] Changing shape from `object` to `string` hides the fields table
- [ ] Deleting a component asks first, and cancelling changes nothing
- [ ] After a save, the file on disk contains the change and **only** the change — the diff-stability property from Epic 11 Story 2, asserted end to end for the first time
- [ ] Accessibility: the component list is a real list of controls, the fields table is a table, and every input has a label

## Notes

- **Do not sort the component list.** It is the most natural-looking wrong thing to do here, and it silently undoes Epic 11 Story 2.
- Shape cycling in the prototype is a click-through control. A `Dropdown` is better: seven shapes is too many to cycle, and a dropdown is keyboard-operable for free. Follow the design's look, not its interaction, and say so.
- A component's `behavior` binds a machine to the component's *lifecycle* (attach/detach); an entity type's `behavior` runs on spawn. They are different fields with the same name and the copy should not blur them.
- Renaming a field is a destructive migration. This story only edits; Story 4 is where that consequence gets surfaced.
- The engine lowercases property names into column names (`strings.ToLower`), so `maxHp` and `maxhp` collide in SQL while remaining distinct in JSON. Worth a check here rather than a confusing DDL error later.
