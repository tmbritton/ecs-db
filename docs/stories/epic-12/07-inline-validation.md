# Story 7: Inline validation

**Epic:** 12 — Forge: SCHEMA & ENTS modes  
**Status:** ✅ Complete  
**Priority:** High — closes the loop on the whole epic

**Depends on:** Stories 2 and 5

## Context

Everything in this epic can produce an invalid schema, and the save path refuses one. Refusing at save is correct but late: it means finding out at the end of a train of thought rather than during it.

This story surfaces the same checks while editing. Not new checks — the same functions the engine runs, so what Forge says and what the engine does cannot diverge. `ValidateSchema` covers structure, reserved names, and cross-references. `ValidateBehaviorRefs` checks that a `behavior` field names a machine file that exists; it has been written and tested since Epic 2 and **has never had a caller anywhere in the codebase**. This is that caller.

There is one check worth adding that the engine only performs later and more expensively. `ValidateMachine` treats a context key matching two components' fields as a hard error, because it cannot tell which one to seed from. That failure surfaces at machine-load time, a long way from the schema edit that caused it — adding a `hp` field to a second component. `buildFieldIndex` already computes exactly the map needed to catch it while typing.

## Acceptance Criteria

- [x] Validation runs on every edit, not only on save, and results render inline near what caused them
- [x] Errors come from `schema.ValidateSchema` — no reimplementation
- [x] `schema.ValidateBehaviorRefs` is called, giving it its first caller, and a `behavior` naming a missing machine is reported against that field
      (as a warning, not an error — nothing in the engine reads that field yet)
- [x] A context-key ambiguity warns at schema-edit time, naming both components — before the machine fails to load
- [x] Warnings and errors are distinguishable: an error blocks the save, a warning does not
      — with "error" defined narrowly as *the save itself would be refused*, which
      is a shorter list than this story first assumed. See *As Implemented*
- [x] The save footer reflects validity, so it is clear before pressing save that it will be refused
- [x] ~~`ValidateSchema` short-circuits by phase~~ — it stops at the first *failure*, which
      is stricter still; the UI says so. See *As Implemented*
- [x] Validation state is pushed on the page-level SSE stream
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/12-inline-validation.spec.js`.

- [x] Naming a component `Behavior` shows an error while typing, before any save
- [x] An entity type requiring a component that does not exist is reported
- [x] Binding a behaviour that does not exist is reported against that field
- [x] Adding a field named `hp` to a second component warns about the ambiguity and names both components
- [x] A warning does not disable save; an error does
- [x] Fixing the problem clears the message without a reload
- [x] Errors are associated with their field for assistive tech (`aria-describedby` / `aria-invalid`), not merely positioned nearby
- [x] Added: an `aria-describedby` points at an element that actually exists — the
      half of the pair nothing on screen would reveal as missing
- [x] Added: a list row carrying a problem is marked, so a problem on a row nobody
      has clicked is not invisible behind a disabled Save button

## Notes

- **`ValidateSchema` short-circuits by phase.** Epic 11 Story 6 found this while wiring save reports: it returns the first phase's failures and stops. Fixing that is engine work and is not in this story — but the UI must not imply completeness it does not have. Either say "first problems found" or make the engine change deliberately, as its own piece of work.
- The ambiguity check is a *warning*, not an error. The schema is legal; it is the machine that will later refuse. Blocking a save for it would be Forge inventing a rule the engine does not have.
- Validation runs on every keystroke's worth of state. These schemas are small, so this is affordable — but it is another caller of `schema.Marshal` via the dirty check, so watch that the two together stay cheap.
- Inline validation is the last story of the epic because it needs both modes to exist to be worth anything.

## As Implemented

`internal/forge/validation` collects; it decides nothing. Every rule is the
engine's, asked through the engine's own functions — `schema.ValidateSchema`,
`schema.ValidateBehaviorRefs`, and `agent.FieldIndex` for the one failure the
engine only discovers later. There is no second copy of any rule in the package
to drift from the first.

`schema.ValidateBehaviorRefs` has its first caller in the codebase, four epics
after it was written and tested.

Coverage: `validation` 98.4%, `server` 92.6%, `components` 71.6%, `modes` 68.7%.
123 browser tests, up from 113.

### Recovering *where* and *how many* without re-deciding *what*

`ValidateSchema` returns one error for a whole file. That is enough to refuse a
save and not enough for anything this story needs: it cannot say which control
to hang a message on, and it cannot show a second problem before the first is
fixed.

Both are recovered by asking the same function about smaller pieces of the same
schema. A carrier holding one component and a filler entity type answers exactly
that component's naming and SQL questions; one holding every component and a
single entity type answers exactly that entity type's cross-reference questions.
The verdict on every carrier is still the engine's — only the question is
narrower.

Three things had to be got right, and two of them were got wrong first and
caught by tests:

- **Components are asked first, and alone.** A component that fails on its own
  terms fails inside every entity type's carrier too, so one component named
  `Behavior` came out as a complaint against every entity type in the file. This
  is also the engine's own phase order, and for the same reason: what an entity
  type refers to means nothing while the components it refers to are malformed.
- **With one half of the file empty there is nothing to narrow to.** A schema
  with no components makes every reference in every entity type dangle, and
  narrowing reported each of those instead of the one thing actually missing.
- **A failure no carrier reproduces is reported unattached, never dropped.** That
  covers the file-wide rules, and it is the safety net for any rule the engine
  grows later that this narrowing does not anticipate.

### The story's own premise was slightly off

The story and the plan both say `ValidateSchema` "returns the first phase's
failures and stops". It is stricter than that: each phase returns on its *first*
problem, so the whole function returns exactly one error, ever. The `Partial`
flag and the note it renders are worded for what actually happens.

### Behaviour bindings, in two tiers — both of them warnings

Two things can be wrong with a `behavior` field, and they are reported
differently because they need different fixes:

- **No machine file anywhere.** `schema.ValidateBehaviorRefs` says so, naming
  every directory searched.
- **The file is there but no machine loaded from it.** The file exists, so the
  first check passes; the machine is simply absent from what the project
  resolved.

**Neither blocks the save, and the first one used to.** That was wrong, and the
first version of this section argued for it on a premise that is false: that a
missing file "fails startup validation, so the game does not run". It does not.
`ValidateBehaviorRefs` has no caller in the engine — this package is its first
anywhere, which is the whole point of the story — and nothing outside
`internal/schema` and `internal/forge` reads the `Behavior` field at all. So
today a binding naming a missing file and one naming a rejected file are equally
inert: the game starts, the entity spawns, and it does nothing either way.

Blocking on it was therefore exactly what this package refuses to do for the
ambiguity warning two sections down — invent a rule the engine does not have —
with a worse consequence, because it is reached by doing nothing at all. Open a
project whose `schema.json` already binds a machine someone else deleted, and
Save is dead for every other change in the file until you clear a binding you did
not want to clear.

`Problem.Blocking` now has a definition narrow enough to be checked: **the save
itself would be refused**, which is exactly `schema.ValidateSchema`, the function
`editable.File.Save` validates with. Nothing else sets it, and the generated
corpus asserts that no number of warnings ever adds up to a disabled Save.

The second tier closes the item Story 5 deferred. The seeds panel used to say a
missing machine was "either no behaviour file declares that id, or the file that
does was rejected on the way in — the log says which", which is a sentence that
sends someone to a terminal to find out what the tool already knows. `Project`
records why it dropped each file; that list is now threaded through to the
Behavior field, and the seeds panel points at it instead of at the log.

**The engine's check takes one directory; a project has as many as it has mods.**
`ValidateBehaviorRefs(s, dir)` is written for the interpreter, which is meant to
call it once. The loader accepts a machine contributed by any mod, so the binding
resolves if any directory satisfies the check. When none does, the message names
every directory searched — but only when the complaint is actually *about* a
directory. A behaviour name containing a path separator is rejected before the
filesystem is touched, and attaching "searched /a, /b" to that describes a search
that never happened. Which case it is falls out of the answers rather than being
decided in Forge: a directory-independent complaint is the same sentence from
every directory.

And a project where no mod declares a behaviours directory at all gets a sentence
naming `game.toml` and the `[[mods]]` key, rather than the engine's own answer,
which names `behaviorsDir` — a parameter of a Go function that appears nowhere in
the user's config.

### The one rule not delegated

`agent.buildFieldIndex` is now exported as `agent.FieldIndex`, so the warning and
the error it predicts share one implementation of "which components declare this
field". Sharing the index is not enough on its own, though: the warning could
still describe a failure the engine no longer has. `TestCheck_PredictsTheErrorTheEngineWouldRaise`
asserts both directions against `agent.ValidateMachine` itself — with the second
declaration present the engine rejects the machine, and with it removed neither
complains.

The warning fires only for keys a machine actually seeds. Two components sharing
a field name is ordinary and legal; it is only a problem when something reads it
as a context key, and warning about every duplicate would be Forge inventing a
rule.

### Association, not proximity

`aria-describedby` and `aria-invalid` are wired inside the `Dropdown` primitive
rather than at each call site, so a caller cannot render the message and forget
the attribute — the half of the pair that nothing on screen would reveal as
missing. `ProblemsID` builds the id for both ends, for the same reason: two
format strings spelled out separately eventually stop matching, at which point
the association silently does nothing.

The message list is a **sibling** of the `<label>`, not a child of it. A wrapping
label takes its control's accessible name from its entire subtree, so the first
version appended every error message to the select's name — announced once as the
name and again through `aria-describedby`, and changing whenever validation
changed. It also put flow content inside a phrasing-content element, which made
clicking the error text open the dropdown. Nothing about the rendered page looked
wrong either way, so the test that pins it is structural: the list must appear
after the closing `</label>`.

Component, entity-type and field names are authored and can hold anything a JSON
key can, so every character outside `[A-Za-z0-9]` is escaped to `_XX` — which
means a literal underscore never survives, which is what makes `__` an
unambiguous separator between parts. The `"p"` prefix guarantees no id starts
with a digit, so `#id` selectors work.

`aria-invalid` is set for errors only. A warning's value is acceptable to the
engine, and telling a screen reader otherwise would be a different claim from the
one on screen.

### Nothing is allowed to be invisible

Two ways a problem could be reported and never seen, both fixed:

- **It belongs to a row nobody has clicked.** Problems render inside their
  owner's editor and only one editor is on screen, so a schema with two broken
  entity types showed a disabled Save, a count of two, and nothing else until the
  user clicked through every row. Both list templates now mark the rows that
  carry problems, red for the ones stopping the save and amber for the rest,
  with the count in the title and in screen-reader text.
- **It is attached to a control that is not drawn.** The editor places problems
  on the behaviour dropdown and on fields-table rows; a field problem for a
  property with no row — a hand-edited non-object component that kept its
  `properties`, which `agent.FieldIndex` still reads — had nowhere to go. The
  editor's own list is now computed as *everything the controls did not take*
  rather than as an enumeration of the cases that fall through, so a control
  added later cannot silently start swallowing a message.

The "there may be more than this" caveat renders exactly once per page, in the
file-level block. It used to render in both that block and the editor, which said
the same sentence twice and gave two elements one test id — a Playwright
strict-mode violation waiting for the first page that had both kinds of problem.

### What blocks, and what does not

Save is disabled with the count beside it and the messages against the controls
that caused them — a count rather than the first message, or the footer would
look like the place to read them with the other two invisible. **Discard stays
enabled**: it is the way out of an invalid state, and taking it away leaves
someone with a file they can neither save nor abandon.

The footer is shell-level, so validation is computed for every mode rather than
only the two that edit the file. A Save button that only knows it would be
refused while SCHEMA happens to be open is worse than one that never knew.

### Two states worth naming

- **The reserved name is unreachable, not reported.** `validComponentName`
  already refuses `Behavior` at the edit boundary, so it never reaches the file
  — a stronger guarantee than reporting it afterwards. The browser test asserts
  the refusal rather than a validation message that cannot appear.
- **An invalid schema is a state the editor genuinely starts in.** `editable.Open`
  parses without validating, deliberately: opening a broken schema so it can be
  fixed is what Forge is for. The browser tests reach that state the way a user
  does — the file is edited in another window and taken from disk with Reload.

### Properties, not just cases

The narrowing's risk is subtle enough that enumerating cases by hand is not
convincing, so `agreement_test.go` asserts three properties over ten thousand
generated schemas — half drawn to be mostly sound, half drawn wild, and the test
fails if either side of that split is under-represented, because agreement about
rejection alone is the half that was never in doubt:

- **Forge blocks exactly what the engine refuses.** A false positive is a control
  the user cannot get past for a reason that is not real; a false negative is a
  Save button that fails when pressed.
- **Every attribution names something that exists.** A message hung on a
  component that is not in the schema points at a control that is not on the page.
- **`Check` is deterministic.** The report renders on a 2-second stream with
  identical renders suppressed, so map order leaking into it would both flicker
  the page and defeat the suppression.

A fourth measures a claim rather than a rule. The unattached fallback is
unreachable today — the engine's rules are covered exhaustively by the file-wide
guards and the two carriers — so it cannot be mutation-tested, and it would be
easy to mistake for dead code and delete. Instead the corpus asserts that a
schema with both halves populated and a sane version always gets its message
attached to a control. If the engine grows a rule the narrowing does not
anticipate, that starts failing and the fallback starts mattering, which is
exactly the moment someone needs to know.

### What the tests caught

Forty-nine deliberate defects, each verified to fail against the suite:
eighteen in the validation package, eighteen in the wiring, and thirteen more
against the fixes below. Three earned their keep.

**W6** — dropping the machine list from the validator's input — survived the
entire suite, because no test drove the ambiguity warning through the server;
every test that exercised it built the report by hand. The path from
`Config.Machines` to the fields-table row it lands on now has a test.

**N2** — listing every directory searched except the first — survived too, and
the reason is one I keep repeating: the assertion checked for the directory
anywhere in the message, and the engine's own complaint quotes one of those paths
inside it. So a list that dropped its first entry was reported as containing it.
The test now cuts the message at `(searched ` and asserts on the list itself, and
uses three directories so "all of them" and "all but the first" are different
answers.

Two of the narrowing bugs were found by tests written before the code that would
have hidden them.

### A process failure worth recording

The first wiring battery reported all eighteen mutations as caught, and every
result was worthless. The loop was written for `bash` and run under `zsh`, which
does not word-split an unquoted `$FILES` — so the backup never happened, the
restores never happened, and the mutations stacked. A tree that is already
failing reports every mutation as caught.

The mutation script now verifies two things before it starts: that the backup
files compare equal to the originals, and that **the baseline test run is
green**. The second is the one that matters, and it is the one that was missing.

This is the second time this project's interactive `cp`/`rm` aliases have
silently broken a restore. `command cp` and `rm -f`, always.

### Left for later

- **`ValidateSchema` stopping at the first failure** is engine work with its own
  risk, and it does not belong smuggled into a UI story. The narrowing here
  recovers most of what it costs; the note says the rest.
- **`ValidateBehaviorRefs` still has no engine caller.** Its own doc comment says
  it is meant to be step 2 of interpreter startup validation. Until it is, a
  missing machine file is a warning here. When it lands, the first tier becomes
  blocking and `bindingProblems` is the place that says so.
- **The machine list is read once, when Forge starts.** The file check is live,
  so a machine added or fixed while Forge is running resolves as a file and is
  still absent from the list. The message names both possibilities rather than
  asserting the one that used to send people to check an id that was already
  correct. Refreshing it belongs with Epic 13, which edits machines and will need
  it anyway.
- **The ambiguity warning cannot see a machine that failed to load**, because a
  rejected machine is not in `Project.Machines` and its context is unknown. The
  binding problem covers that case with a different message.
- **Unknown XState fields** (`meta`, `description`, `tags`) are still dropped on
  a machine round trip — Epic 13's problem.
- **`role="alert"` on every problems list** is assertive, and these lists are
  patched down the SSE stream. Whether a morph of an unchanged list re-announces
  depends on the morph implementation; it was not measured in a browser here and
  is worth one pass with a screen reader when Epic 17 does the dialog set.
