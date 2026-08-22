# Story 2: Machine editing session

**Epic:** 13 — Forge: AGENTS mode  
**Status:** 🔲 Not started  
**Priority:** High — the spine every panel in this epic writes through

**Depends on:** Story 1

## Context

Epic 12 edited one file that always exists. This epic edits as many files as the
project has machines, and they can be created, renamed and deleted while Forge
is running — and one mod's machine can shadow another's.

That is enough difference to build deliberately rather than inside whichever
panel lands first. `editable.File[*agent.MachineDefinition]` gives each machine
dirty tracking, atomic writes and the conflict path for free — its codec was
written with this exact value type in mind — and `editable.Set` already answers
"is anything unsaved" across many files, which is what the shared save footer
hangs on.

Three things are genuinely new. **A machine's identity lives inside the file:**
the `id` field is what an entity type's `behavior` binds to, not the filename,
so renaming is two different operations that must not be confused. **A new
machine has to land in some mod**, and a project has several. **The machine list
is read once, when Forge starts** — Epic 12 Story 7 left that as a follow-up for
this epic, and it becomes wrong here for the first time, because this is the
story that creates machines.

## Acceptance Criteria

- [ ] One `editable.File[*agent.MachineDefinition]` per resolved machine, held by
      a session type that owns the lock — the same shape as
      `internal/forge/session`, which is single-file and stays that way
- [ ] `Marshal: agent.EmitMachine`, `Unmarshal: agent.ParseMachine`,
      `Validate: agent.ValidateMachineError` — no third implementation of any of
      them
- [ ] The registry the validator uses is built exactly as
      `project.buildRegistry` builds it, `hasMap` gate included
- [ ] `editable.Set` drives the footer, so "unsaved" is true when *any* machine
      is dirty and the footer names which
- [ ] Save writes only the dirty files; a machine nobody touched is not
      rewritten, and its mtime does not move
- [ ] Creating a machine: a new file in a named mod's behaviours directory, with
      an id, an initial state and one state — a machine with no states does not
      validate and so could never be saved
- [ ] The mod a new machine lands in is **chosen**, not defaulted silently, when
      the project has more than one that could take it
- [ ] Renaming distinguishes the two operations and says which is which: the
      machine's `id` (which every `behavior` binding resolves through) and the
      file's name (which nothing resolves through)
- [ ] Renaming an `id` that an entity type binds reports what will break, before
      it breaks — Epic 12's inline validation is what will report it afterwards
- [ ] Deleting a machine asks first, and says which entity types bind it
- [ ] The resolved machine set is refreshed after a create, rename or delete, so
      Epic 12's binding dropdowns and its "no machine with this id is loaded"
      warning stop being stale the moment this story can change them
- [ ] A save is observed by the running engine's watcher and hot-swaps, with no
      restart — driven end to end, not asserted from the fact that a file changed
- [ ] Two machines declaring one id is already a `project.Problem`; creating one
      through Forge must not be able to produce it
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/13-machine-session.spec.js`. The fixture project has one machine
(`e2e-wander`); this story is where it gains a second, so the spec seeds and
restores the behaviours directory the way the Epic 12 specs seed and restore
`schema.json`.

- [ ] Editing a machine marks the session dirty and names the file in the footer
- [ ] Saving writes only the machine that changed — a second machine's bytes and
      mtime are untouched
- [ ] Discard restores the working value without touching disk
- [ ] Creating a machine adds a file and the machine appears in the list
- [ ] Creating a machine when the project has two mods asks which one
- [ ] Renaming the `id` and renaming the file are separate controls with separate
      results, and the spec asserts on the file on disk for both
- [ ] Deleting asks first, and cancelling leaves the file where it was
- [ ] A machine created in Forge is immediately bindable from ENTS — the
      staleness Epic 12 Story 7 documented is gone
- [ ] Two tabs, one file: the conflict path from Epic 11 still applies here, and
      "keep mine" and "take theirs" both still work

## Notes

- **Do not generalise `internal/forge/session` to cover both.** It is
  `schema.json`'s session and it has one file because that file is one file.
  A type that handles "one or many" handles neither clearly. Build a sibling.
- The save footer is shell-level and already shared. It now has to say "3
  unsaved files" rather than name one — check what `SaveFooterProps.File` should
  become before adding a second field beside it.
- **A machine that will not validate cannot be saved**, which is correct and is
  also a trap: a half-built machine on the canvas is invalid almost continuously
  — a new state with no transitions out of it is fine, but a transition whose
  target you have not picked yet is not. Story 8 renders the errors; this story
  decides what the save button does in the meantime, and "disabled for minutes
  at a time while you work" is not an answer.
- `ReloadFile` exists on the loader and takes a `ReconcileFunc`. Forge is not the
  engine and must not reconcile the database — but it is the same refresh
  problem, and the accessor to reuse is `List()`/`Sources()`, not a second scan.
- Watch for the sidecar decision from Story 1 landing here: if layout is a
  separate file, creating and deleting a machine has to create and delete two,
  and a save that writes one but not the other is a new way to be inconsistent.
