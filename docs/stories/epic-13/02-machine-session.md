# Story 2: Machine editing session

**Epic:** 13 — Forge: AGENTS mode  
**Status:** ✅ Complete  
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

- [x] One `editable.File[*agent.MachineDefinition]` per resolved machine, held by
      a session type that owns the lock — the same shape as
      `internal/forge/session`, which is single-file and stays that way
- [x] `Marshal: agent.EmitMachine`, `Unmarshal: agent.ParseMachine`,
      `Validate: agent.ValidateMachineError` — no third implementation of any of
      them
- [x] The registry the validator uses is built exactly as
      `project.buildRegistry` builds it, `hasMap` gate included
- [x] ~~`editable.Set` drives the footer~~ — **amended.** `Set` answers "which
      paths are dirty"; the footer needs "which are dirty and *why*", because a
      machine authored in another whitespace style is dirty on open and is not
      pending work. `Session.Changes` answers both. `Set` also has no removal,
      and machines come and go. Using it would have meant rebuilding it on every
      re-resolve and still writing the loop that classifies
- [x] Save writes only the dirty files; a machine nobody touched is not
      rewritten, and its mtime does not move — including the file that is dirty
      *on open* because it was authored in another style
- [x] Creating a machine: a new file in a named mod's behaviours directory, with
      an id, an initial state and one state — a machine with no states does not
      validate and so could never be saved
- [x] The mod a new machine lands in is **chosen**, not defaulted silently, when
      the project has more than one that could take it
- [x] Renaming distinguishes the two operations and says which is which: the
      machine's `id` (which every `behavior` binding resolves through) and the
      file's name (which nothing resolves through)
- [x] Renaming an `id` that an entity type binds reports what will break, before
      it breaks — Epic 12's inline validation is what will report it afterwards
- [x] Deleting a machine asks first, and says which entity types bind it
- [x] The resolved machine set is refreshed after a create, rename or delete, so
      Epic 12's binding dropdowns and its "no machine with this id is loaded"
      warning stop being stale the moment this story can change them
- [x] A save is observed by the running engine's watcher and hot-swaps, with no
      restart — driven end to end, not asserted from the fact that a file changed
- [x] Two machines declaring one id is already a `project.Problem`; creating one
      through Forge must not be able to produce it
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/13-machine-session.spec.js`. The fixture project has one machine
(`e2e-wander`); this story is where it gains a second, so the spec seeds and
restores the behaviours directory the way the Epic 12 specs seed and restore
`schema.json`.

- [x] Editing a machine marks the session dirty and names the file in the footer
- [x] Saving writes only the machine that changed — a second machine's bytes and
      mtime are untouched
- [x] Discard restores the working value without touching disk
- [x] Creating a machine adds a file and the machine appears in the list
- [x] Creating a machine when the project has two mods asks which one
- [x] Renaming the `id` and renaming the file are separate controls with separate
      results, and the spec asserts on the file on disk for both
- [x] Deleting asks first, and cancelling leaves the file where it was
- [x] A machine created in Forge is immediately bindable from ENTS — the
      staleness Epic 12 Story 7 documented is gone
- [x] Two tabs, one file: the conflict path from Epic 11 still applies here, and
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

## As Implemented

`internal/forge/machines` is a sibling of `internal/forge/session`, not a
generalisation of it. One `editable.File[*agent.MachineDefinition]` per resolved
machine, one lock, and the create/rename/delete operations that a directory of
files needs and a single file does not.

Coverage: `machines` 77.0%, `server` 84.7%. 133 browser tests, up from 123.

### The decision the story left open

**Saving is per machine.** Save writes every dirty machine that validates and
refuses the rest individually, with their reasons.

That is a different answer from `schema.json`'s all-or-nothing, and the
difference is real rather than a preference. An invalid `schema.json` stops the
engine starting, so blocking the whole save protects the project. An invalid
*machine* is rejected by the loader on its own: the game runs, the other
machines work, and the one entity bound to it does nothing. The blast radius is
one file, so the refusal is one file — and half-built work on the canvas cannot
hold finished work hostage, which is the trap the story's Notes named.

### Three bugs a browser found and no Go test could

**The shell's SSE subscription carried `?component=` and nothing else.**
Invisible for two epics, because SCHEMA and ENTS both select with it. AGENTS
selects with `?machine=`, so the stream re-rendered with nothing named every
tick, the fallback picked the first machine, and the page silently swapped under
the user seconds after they clicked one. The shell now takes a ready-made query
the server composes — it must not know which parameter a mode selects with,
which is the property that made the bug possible to write in the first place.

**The machine list showed the id on disk while the editor showed the working
one.** The resolved set comes from re-reading the files, so an unsaved rename
left the two disagreeing about which machine you were editing.

**Renaming a machine's file moved the path the page subscribed with** — the same
failure Epic 12 hit with component renames, in a third namespace, and fixed the
same way.

### Story 1's bill came due

Story 1 recorded that a machine authored in another whitespace style differs
from its own file the moment Forge opens it, and left the consequence here.
Reporting that as "unsaved changes" is how a tool teaches you to ignore the word
"unsaved", so `Change.Reformatting` tells the two apart by comparing what the
file *means* with what the session holds — both through the emitter, so the
comparison is of content rather than layout.

The footer says `patrol.json · would be reformatted`, does not count it as
pending work, and **a bulk save skips it**: rewriting a file nobody touched
would move its mtime and put a whole-file diff in the user's tree as a side
effect of saving something else. `SaveOne` still reformats it for a caller that
asks.

### What the review caught

Ten defects, four of them serious enough to block.

**A data race, with one tab open and no adversarial timing.** `Machines()`
copied `f.Current` — a live pointer into an open file — out under the lock, and
`agent.ValidateMachine` *writes* `ContextManifest` onto whatever it validates.
So every render raced every save. `Read` and `Edit` clone precisely to prevent
this and the doc comment claimed the type had the property; `Machines()` was the
hole in it.

**And the same line broke a panel Epic 12 shipped.** `f.Current` is
parse-derived, and `ParseMachine` never populates `ContextManifest` — only
validation does. Swapping it in made ENTS report *every* context seed on *every*
entity type as "no longer in the schema", a message written for a component that
has actually been removed. `Machines()` now overrides the id and nothing else.

**The AGENTS footer said "✓ saved" over an unsaved `schema.json`, with Save
disabled.** My own plan said unsaved work elsewhere would be reported as a
count, and it was not. That is a way to lose work, not a wording miss: switch
mode, be told everything is saved, and be given no control that would save it.
Both footers now name what the other one owns, and neither renders `✓ saved`
while anything is dirty.

**Forge could still author the duplicate-id state the story forbids.** The guard
checked the *resolved* set, and a file that declares an id and fails to validate
is not in it — so creating a second file with that id succeeded, and the
duplicate appeared the moment someone fixed the broken one. It now reads the id
out of every file in every behaviours directory, which is what the engine's own
duplicate check does.

Also fixed: `Delete` destroyed unsaved work without asking, where `RenameFile`
refuses and says so; every project problem was reported twice, because the
session's list and the startup snapshot were unioned rather than deduplicated;
`dirtyNames` ranged a map, so the footer re-rendered differently on a random
fraction of SSE ticks and was re-patched forever while more than one machine was
unsaved; and `streamQuery` put an absolute filesystem path into the subscription
URL of *every* mode, including the four that have no use for it.

Two of the review's smaller findings were about code that should not exist:
`Session.BoundBy`/`Bindings` had no production caller — the UI has its own copy
— so the test covering them was testing nothing that runs. Deleted, along with
the five helpers that existed only for them.

### The claim about staleness was too strong

Epic 12 Story 7 hedged its "no machine with this id is loaded" message with "or
it was added or fixed after Forge started, which reads the project's machines
once". This story made every create, rename and delete re-resolve, so I deleted
the hedge.

That was half right. Forge's *own* changes are never stale now — but nothing
watches the behaviours directory, and editing a machine file in a text editor is
a first-class workflow in this project. The message would have told those users
flatly that their id was wrong, sending them to check something already correct:
the exact failure the original wording was written to avoid, in the other
direction. It names both causes again, with the second one narrowed to what is
actually true.

### What the tests caught

Twenty mutations in the battery, all caught by tests — four initially only broke
the build, and were rewritten to compile so the result meant something. Five
survived the first pass and every one was a real gap, including a duplicate-id
guard whose test passed with the guard removed because its fixture used a
machine whose filename matched its id, so the *file-exists* check answered
instead.

Three more mutations were run against the review fixes. One survived: keeping a
dirty file whose machine has left the resolved set had no test, and now has one.

### An acceptance criterion I had missed

*"A save is observed by the running engine's watcher and hot-swaps, with no
restart — driven end to end, not asserted from the fact that a file changed."*

The clause was written to prevent exactly the hand-wave I was about to commit.
`TestSave_IsPickedUpByTheEnginesWatcher` runs the engine's own `agent.Watcher`
and `Loader` over the directory the session writes to and asks the *engine* what
it thinks the machine is afterwards. It is also the only test that exercises why
`internal/forge/atomicfile` exists: a truncate-then-write leaves a window where
the watcher reads a half-file, keeps the stale definition, and looks exactly
like a save that never happened.

### Left for later

- **Nothing watches `behaviors/`.** Forge's own changes re-resolve; an external
  edit does not appear until something calls reload. A watcher on the Forge side
  is the honest fix and belongs with whichever story finds it costly.
- **A machine stranded by a re-resolve** — shadowed by a later mod while Forge
  held unsaved work on it — keeps its edit and is reported as a problem, but has
  no UI to recover it beyond discarding. Story 3 owns the problem list.
- **The two-mod create flow and the conflict path are covered in Go, not in a
  browser.** The e2e fixture project has one mod, and giving it a second would
  change what every other spec sees. The dropdown branch of `newMachineControl`
  is therefore unexercised by Playwright.
- **`add-machine` hardcodes the id `NewMachine`**, so a second create is refused
  until the first is renamed. The skeleton is Story 3's to replace.
- **`data-machine` is the filename**, not the machine id, so the e2e helper that
  selects by it depends on the two coinciding in the fixture.
