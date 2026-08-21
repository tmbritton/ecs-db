# Epic 12 Story 2: SCHEMA mode — Implementation Plan

**Goal:** edit components and their fields, without disturbing the file's arrangement.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/templates/modes/schema.templ` | List, editor, fields table |
| Create | `internal/forge/templates/modes/schema_test.go` | Render assertions |
| Modify | `internal/forge/server/server.go` | Component/field actions |
| Create | `e2e/specs/12-schema-mode.spec.js` | |

---

## Task 1: Selection is a URL

`/forge/schema?component=Position`. Mode switching is a full page load, so
selection has to survive one; a signal would not. It also makes a component
linkable, which matters the moment someone wants to point at one.

An unknown or absent `component` selects the first in authored order rather
than erroring — a schema always has one, and a 404 for a stale bookmark would
be unhelpful.

## Task 2: Rendering in authored order

```templ
for _, name := range jsonorder.Apply(s.ComponentOrder, s.Components) {
```

Not `for name := range s.Components`, and not sorted. `jsonorder.Apply` is
already the one rule for this, from Epic 11.

Same inside the fields table with `comp.PropertyOrder`.

**Test it by mutation**: sorting the list must fail a test. `Position` before
`Health` is the assertion — alphabetical reverses them.

## Task 3: Edits

Each is an action posting the page's signals:

| Action | Effect |
|---|---|
| `POST /forge/schema/component` | add / rename / delete a component |
| `POST /forge/schema/shape` | change shape |
| `POST /forge/schema/behavior` | bind or unbind a machine |
| `POST /forge/schema/field` | add / rename / retype / delete a field |
| `POST /forge/schema/version` | bump `schemaVersion` |

All go through `Session.Edit`. None touches the filesystem.

Adding a component appends to `Components` and leaves `ComponentOrder` alone —
`jsonorder.Apply` then places it after the recorded ones, sorted among other new
ones. That is the rule already; do not special-case it here.

## Task 4: The reserved name

`ValidateSchema` rejects a component named `Behavior` case-insensitively. Check
it in the action and report it in the editor rather than letting it through to
be refused at save. Reuse the engine's own comparison
(`strings.EqualFold(name, "Behavior")`) rather than restating the rule.

Also check for a lowercased-name collision: the generator does
`strings.ToLower(propName)` for column names, so `maxHp` and `maxhp` are
distinct properties and the same column.

## Task 5: Shape control

A `Dropdown`, not the prototype's cycling click-target. Seven shapes is too many
to cycle through, and a dropdown is keyboard-operable and screen-reader-legible
for nothing. Keep the design's *look*; change the interaction and say so in the
story's As Implemented.

Changing to a non-object shape leaves `Properties` in place in memory but
`Marshal` omits it — so switching away and back does not lose the fields. Check
that against `schema.Marshal`'s `omitempty` behaviour rather than assuming.

---

## Verification

`go test ./internal/forge/...`, then the e2e spec. The end-to-end assertion that
matters: after a save, `git diff` on the fixture schema shows only the intended
change — the diff-stability property from Epic 11 Story 2, exercised through the
UI for the first time.
