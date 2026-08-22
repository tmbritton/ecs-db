# Epic 13 Story 2: Machine editing session — Implementation Plan

**Goal:** the spine every AGENTS panel writes through — many files, not one.

---

## The decision the story leaves open

> *"A machine that will not validate cannot be saved, which is correct and is
> also a trap … 'disabled for minutes at a time while you work' is not an
> answer."*

**Saving is per machine.** Save writes every dirty machine that validates, and
refuses the ones that do not, individually and with their reasons. You are never
blocked from saving finished work by one half-built machine sitting on the
canvas.

That is a different answer from `schema.json`'s, and the difference is real
rather than a preference. An invalid `schema.json` stops the engine starting, so
blocking the save protects the whole project. An invalid *machine* is rejected
by the loader on its own: the game runs, the other machines work, and the one
entity bound to it does nothing. The blast radius is one file, so the refusal
should be one file too.

The footer therefore reports `2 saved · 1 refused` rather than going dead. Story
8 renders *why* against the node that caused it; this story reports it as text
and does not pretend otherwise.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/machines/machines.go` | The session: N files, one lock |
| Create | `internal/forge/machines/machines_test.go` | |
| Edit | `internal/forge/project/project.go` | Export `loadMachines` as `ResolveMachines` |
| Create | `internal/forge/server/machineedit.go` | Routes for create/rename/delete/save/discard |
| Create | `internal/forge/server/machineedit_test.go` | |
| Edit | `internal/forge/templates/modes/agents.templ` | A machine list with the controls |
| Edit | `internal/forge/templates/savefooter.go` | A footer for the machines set |
| Edit | `internal/forge/server/server.go`, `cmd/ecs-db/forge.go` | Wiring |
| Create | `e2e/specs/13-machine-session.spec.js` | |

---

## Task 1: The session

```go
type Session struct {
    mu    sync.Mutex
    files map[string]*editable.File[*agent.MachineDefinition] // keyed by path
    order []string                                            // resolved order

    mods    []project.Mod
    hasMap  bool
    schema  func() schema.DatabaseSchema // the *working* schema, not the one on disk
}
```

Keyed by **path**, not by id. A machine's id lives inside the file and this story
can change it; a map keyed by something the user can edit rewrites its own keys
mid-operation. The path is stable for everything except a file rename, which is
the one operation that has to update the key deliberately.

`schema` is a function rather than a value because machine validation depends on
the schema — a context key has to match exactly one component field — and the
user may have unsaved schema edits. Validating machines against the file on disk
would report problems that the editor's own state says are already fixed.

Methods: `Read`, `Edit(path, fn)`, `Dirty`, `Save`, `SaveOne`, `Discard`,
`Reload`, `Create`, `RenameID`, `RenameFile`, `Delete`, `Machines`, `Problems`.

## Task 2: Two renames, told apart

`RenameID` edits the `id` **inside** the file. That is what every entity type's
`behavior` binding resolves through, so it can break bindings — and the session
reports which entity types bind the old id *before* doing it.

`RenameFile` moves the file. Nothing resolves through a filename, so nothing can
break; it is a tidiness operation. Both exist because a user who has just
renamed a machine will expect the file to follow, and doing it silently would be
Forge making a decision about their working tree.

## Task 3: Refresh, against the working schema

`project.loadMachines` becomes exported `ResolveMachines(mods, schema, hasMap)`.
The session calls it after every mutation and holds the result.

The server then reads machines from the session rather than from
`Config.Machines`, which was a startup snapshot. That closes the staleness Epic
12 Story 7 documented and worded its message around: the "no machine with this
id is loaded" warning currently has to hedge that the list may be older than the
file. Once it refreshes, the hedge can go — **and that message must be updated in
the same change**, or it will be lying in the other direction.

## Task 4: Creating

A new machine needs a mod. When more than one mod declares a behaviours
directory the caller names it; the session refuses rather than picking. It also
refuses an id that another machine already declares, since two files with one id
is an existing `project.Problem` and Forge must not be able to author one.

The new file gets an id, an `initial`, and one state — a machine with no states
does not validate, so an empty one could never be saved and would be a file that
exists only to be broken.

## Task 5: The footer

`SaveFooterProps.File` is display-only, so no new field: the machines footer
passes `"3 unsaved machines"` where the schema footer passes `"schema.json"`.

The footer is shell-level and currently always the schema's. It becomes
mode-aware — AGENTS saves machines, SCHEMA and ENTS save `schema.json` — because
one Save button must do one thing. Unsaved work in the *other* place is still
reported, as a count, so switching modes cannot hide it.

## Task 6: Minimal AGENTS surface

Enough to drive the story: the machine list, and controls to create, rename,
delete, save and discard. Story 3 adds the mod/override tags, the context
manifest and the validity readout — this is deliberately the skeleton it fills
in, not a throwaway.

---

## Verification

- Unit: every method, including the refusals. A rename that would break a
  binding, a create with a duplicate id, a create with no mod named.
- **Only the dirty file is written.** Assert on mtime *and* bytes of an untouched
  machine — bytes alone would pass against a save that rewrote it identically,
  which is exactly what Story 1's byte-stability makes possible.
- The engine's watcher picks up a save and hot-swaps, driven end to end.
- Mutation-check: per-machine save falling back to all-or-nothing; refresh not
  happening; the two renames doing the same thing; the duplicate-id guard.
