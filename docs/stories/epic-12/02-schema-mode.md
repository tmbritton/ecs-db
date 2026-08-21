# Story 2: SCHEMA mode — components and their fields

**Epic:** 12 — Forge: SCHEMA & ENTS modes  
**Status:** ✅ Complete  
**Priority:** High — the first mode that edits anything

**Depends on:** Story 1

## Context

The left rail lists the components; the centre edits the selected one. A component has a shape (`object`, `entity-ref`, `array`, or one of the four scalars), an optional bound behaviour machine, and — when it is an object — a table of fields that become typed SQL columns.

Order matters here in a way it usually does not. `Position, Health, Sprite` sit together in the file because they belong together, and Epic 11 Story 2 went to some trouble to preserve that. The list must render in `ComponentOrder`, and a new component must land where the ordering rule says it will, or the first save reorders a file the user did not reorder.

`"Behavior"` is a reserved component name — `ValidateSchema` rejects it case-insensitively — and the editor should say so at the point of typing rather than at the point of saving.

## Acceptance Criteria

- [x] Component list in **authored order**, not alphabetical, with the `⛃` glyph and a `ƒ` badge on components that bind a behaviour
- [x] `v<N>` schemaVersion badge; clicking it bumps the version, with the consequence stated (it is a migration marker, not a label)
- [x] Selecting a component shows its editor; the selection is a URL (`/forge/schema?component=Position`) so it survives a reload and can be linked
- [x] Shape control offering exactly the shapes the engine supports: `object`, `entity-ref`, `array`, `string`, `integer`, `number`, `boolean`
- [x] Behaviour binding: a dropdown of machines resolved from the project's mods, plus "none"
- [x] Fields table for object components — name, type, NOT NULL, delete — rendering in **authored property order**
- [x] Add field, delete field, rename field, change type; each marks the session dirty
- [x] `"Behavior"` is refused as a component name, in the editor, with the reason — not silently accepted and rejected on save
- [x] Adding a component places it where the ordering rule says (appended, sorted among other new ones), and this is asserted against the saved file
- [x] Delete component, behind a confirmation that names what it will do
- [x] Non-object shapes do not show a fields table; they have no properties
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/12-schema-mode.spec.js`.

- [x] The list renders in authored order — assert `Position` precedes `Health`, which alphabetical would reverse
- [x] Selecting a component updates the URL, and reloading that URL keeps the selection
- [x] Adding a field marks the footer dirty and the field appears in the table
- [x] Renaming a field to `Behavior`... is fine — the reservation is on *component* names; assert a **component** named `Behavior` is refused with a visible reason
- [x] Changing shape from `object` to `string` hides the fields table
- [x] Deleting a component asks first, and cancelling changes nothing
- [x] After a save, the file on disk contains the change and **only** the change — the diff-stability property from Epic 11 Story 2, asserted end to end for the first time
- [x] Accessibility: the component list is a real list of controls, the fields table is a table, and every input has a label

## Notes

- **Do not sort the component list.** It is the most natural-looking wrong thing to do here, and it silently undoes Epic 11 Story 2.
- Shape cycling in the prototype is a click-through control. A `Dropdown` is better: seven shapes is too many to cycle, and a dropdown is keyboard-operable for free. Follow the design's look, not its interaction, and say so.
- A component's `behavior` binds a machine to the component's *lifecycle* (attach/detach); an entity type's `behavior` runs on spawn. They are different fields with the same name and the copy should not blur them.
- Renaming a field is a destructive migration. This story only edits; Story 4 is where that consequence gets surfaced.
- The engine lowercases property names into column names (`strings.ToLower`), so `maxHp` and `maxhp` collide in SQL while remaining distinct in JSON. Worth a check here rather than a confusing DDL error later.

## As Implemented

`internal/forge/templates/modes/schema.templ` renders the list and editor; `internal/forge/server/schemaedit.go` holds the edits, kept out of the handlers so they can be tested without an HTTP request. Every edit goes through `Session.Edit` — nothing here touches `schema.Marshal` or the filesystem, per the epic rule.

### The gap this story exposed: mode content was not on the stream

The page's SSE stream carried the engine status, the save reports and the save footer — but **not the mode's own content**. So an edit changed the session and nothing on screen moved until a reload. The e2e caught it on the first interactive test.

Mode content is now a fourth live region. The selection travels with the subscription (`@get('/forge/schema/events?component=Position')`) because the events request is a separate HTTP request and knows nothing about the page's URL otherwise.

### Two bugs my own tests caught

- **A variable shadow in `deleteComponent`.** `for name, et := range d.EntityTypes` rebound `name`, so it stripped each entity type's *own* name from its component lists and left the dangling reference behind — producing a schema that no longer validates. Caught by asserting `ValidateSchema` after the delete rather than just that the component was gone.
- **`uniqueName` swallowed a real collision.** It treated "invalid for any reason" as "taken", so adding a field `HP` beside an existing `hp` silently became `HP2` instead of reporting that they are the same SQL column. It now uniquifies against exact duplicates only.

### Datastar: the same mistake as Epic 10 Story 4

The rename inputs carried a plain HTML `name="to"` attribute. Actions post the page's **signals**; a `name` contributes nothing outside a form submission, which this architecture does not have — exactly the finding from Story 4 of Epic 10, made again.

Binding to signals would have needed a unique signal per control, because a fields table has one rename input and one type dropdown per row and a shared name makes every row write to the same place. `evt` is in scope in any Datastar expression, so `valueAction` reads from the control that changed and there is no namespace to manage.

### Divergences from the plan

- **Shape is a `Dropdown`, not the prototype's cycling click-target.** Seven shapes is too many to cycle, and a dropdown is keyboard-operable and screen-reader-legible for free. The design's look is kept; the interaction is not.
- **There is no NOT NULL toggle.** The generator makes every column `NOT NULL` and the panel reports what the generator does — a control that appeared to change it would be lying. The column says "always" with the reason on hover.
- **The last field of an object component cannot be deleted.** An object with no properties fails validation, so allowing it would produce a schema that can never be saved.
- **A refused edit answers 204, not 422,** and the reason renders in the editor. A 4xx would surface in the browser console as a failed request for something working exactly as intended, and — more importantly — a control that silently does nothing teaches you it is broken.
- **Switching away from `object` clears the fields.** An earlier version kept them, on the stated theory that `Marshal` omits properties for a non-object shape — it does not, deliberately, so they were being written into the user's file as dead weight. Losing them on a shape change is the lesser harm and is at least visible.
- **Component and field names must be SQL identifiers**, and may not collide with an existing name only by case. The generator concatenates them into DDL unquoted and lowercases them, so anything else is a table nobody can create or two components sharing one.

### Two test-isolation fixes this forced

- `data-active` is a generic marker and the component list uses it too, so Story 5's "exactly one rail button is active" was counting two. Scoped to the rail.
- All stateful specs now run in one serialised Playwright project. Per-file `serial` mode only serialises *within* a file, and two files editing the same server session in parallel workers interfere exactly as two tests in one file would.

### What the review caught

Two of these corrupted data.

**Renaming any component but the first silently re-targeted the editor.** A page subscribes to its stream naming the component it shows, and that URL cannot change afterwards. After a rename the stream asked for a name that no longer existed, `selectComponent` fell back to the *first* component, and the editor swapped to it — while the address bar still named the old one. The next edit then hit whatever the editor had swapped to, renaming a component the user never selected. The server now follows renames, chains included.

Why nothing caught it: every rename test renamed `Position`, the first component, where the fallback happens to land on the renamed one. The fix includes tests that rename a non-first component in both Go and the browser.

**Every field-type dropdown shared one Datastar signal.** Datastar's bind plugin seeds a signal from the first element bound to a path and then drives every element bound to it — so with one dropdown per row, all rows displayed row one's type. A false picture of the schema about to be saved. This is precisely the failure the section above says was avoided, and then the `Dropdown` got a shared signal anyway. They carry no signal now: `valueAction` already carries the value from the control that changed, so the binding was dead weight as well as wrong.

**`setShape`'s justification was false.** The comment claimed `Marshal` omits properties for a non-object shape. It does the opposite, deliberately — `writeComponent` emits properties and items independently so a component carrying both round-trips. So switching a component to `array` wrote dead properties into the user's file, permanently, beside the items. Leaving `object` now clears them, and the three comments repeating the wrong claim are fixed.

**Component names colliding only by case were accepted**, and table names are `comp_` + `strings.ToLower(name)` — so `Health` and `health` are distinct JSON keys and the same SQL table, the second `CREATE TABLE IF NOT EXISTS` silently doing nothing. `validFieldName` already guarded exactly this for columns; `validComponentName` did not.

**Names that are not SQL identifiers were accepted** and interpolated unquoted into DDL. The storage hole is pre-existing, but this is the first UI that invites typing the name, so the guard belongs here.

Also: deleting the last component left an unsaveable schema, and a refused edit's message was global — visible in every tab and surviving reloads. A full page load now starts clean.

### Thirty-five of fifty-seven mutations survived

That is the headline, and it is a fault in the tests rather than in the code. The pattern, repeated across this project: the test asserts the *neighbour* of the property rather than the property.

- `TestSetShape` looped all seven shapes and asserted only that `ValidateSchema` still passed — which it does when the shape never changed at all.
- Two order tests added `Sprite` and `z`, which sort last anyway, so `jsonorder.Apply`'s "unrecorded keys appended sorted" gave the right answer whether or not the order slice was appended to. They use `Alpha` and `a` now.
- The fixture had the deleted component only in `optionalComponents` and the renamed one only in `requiredComponents`, so each test covered one branch and the mirror image survived.
- `ValidateSchema` is the wrong assertion for "this is saveable": component structure is checked in `Component.UnmarshalJSON`, which `ValidateSchema` never runs. An edit producing a structurally broken component would be written to disk and refused on load. Tests now round-trip through `schema.Marshal` and `schema.LoadSchema`.
- `action`, `valueAction`, `urlValue`, `jsString` and `componentHref` build URLs and JavaScript by hand from names the user types, and had **no test at all** — the escaping test only checked two substrings that templ's own escaping already prevents, so it proved templ works rather than that this package does.
- The handlers had almost no Go coverage: the origin guard was tested on two of seven endpoints, and nothing checked that the shape, rename target or field type arguments were used at all.

### Verified by mutation

| Mutation | Caught by |
|---|---|
| the component list is sorted | authored-order and selection tests |
| the fields table is sorted | the field-order test |
| the reserved name is allowed | the reserved-name test |
| lowercase column collisions are allowed | the collision test |
| a rename moves the component to the end | the rename test |
| a rename does not follow references | the rename test |
| the rename is not followed by the page | the rename-follow test |
| field dropdowns share a signal | the shared-signal test |
| a case-colliding component name is allowed | the case-collision test |
| a non-identifier name is allowed | the SQL-identifier test |
| leaving `object` keeps the properties | the shape-clears test |
| the shape is never actually assigned | the shape-applied test |
| an unknown property type is accepted | the retype test |
| the last component can be deleted | the last-component test |
| the origin guard is dropped from any endpoint | the cross-origin table |
| an argument is ignored by its handler | the arguments test |
| the edit problem is never recorded or cleared | the problem test |
| `urlValue` / `jsString` / `action` / `valueAction` | their own tests |

The bottom thirteen all **survived** the original suite.
