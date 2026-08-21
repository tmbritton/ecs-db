# Story 1: Editing session — one editable schema, saved once

**Epic:** 12 — Forge: SCHEMA & ENTS modes  
**Status:** ✅ Complete  
**Priority:** High — both modes edit the same file through this

**Depends on:** Epic 11 (complete)

## Context

`schema.json` has two halves and Forge shows them in two modes, but it is one file. SCHEMA edits `components`, ENTS edits `entityTypes`, and both save the same document. If each mode owned its own copy, saving one would clobber the other's unsaved work — and the bug would only appear when someone edited both before saving, which is the normal way to add a component and immediately use it.

So there is one editing session per project, and both modes are views onto it. Epic 11 built every piece: `editable.File[schema.DatabaseSchema]` with `schema.Marshal`, `LoadSchema` and `ValidateSchema` as its codec, dirty tracking by byte comparison, atomic writes, conflict detection, and `savereport` for the outcome. This story assembles them and puts a working save footer on the screen.

It is also where Forge does its first mutating request. That shape is worth getting right once: Datastar posts the page's signals to an action endpoint, the server mutates the session, and the response is a patch stream that re-renders whatever changed. Every editing story after this one copies it.

## Acceptance Criteria

- [x] One `editable.File[schema.DatabaseSchema]` per project, held by the server and shared by every mode
- [x] Codec is the real one: `schema.Marshal`, `schema.LoadSchema`, `schema.ValidateSchema` — no bespoke serialisation anywhere in this epic
- [x] `POST /forge/schema/save` and `POST /forge/schema/discard` — actions, not pages; the response is a Datastar patch stream
- [x] The save footer shows the file name and real dirty state, and both buttons are disabled when clean
- [x] Saving reports through `savereport`, on the page-level SSE stream, with the outcome wording Epic 11 Story 6 established
- [x] A conflict offers both resolutions (`Reload` / `SaveOverwriting`); neither is taken silently
- [x] Discard restores the last saved state and the footer returns to clean
- [x] A save that fails validation writes nothing and keeps the edit
- [x] The session is safe for concurrent requests — `editable.File` is documented as not safe for concurrent use, so the thing that owns it provides the lock
- [x] A project that failed to open leaves the modes readable and says why, rather than 500ing
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/12-editing-session.spec.js`.

- [x] The footer is clean on load and names `schema.json`
- [x] Both footer buttons are disabled when clean
- [x] After an edit the footer becomes dirty **without a page reload** — the patch arrives on the page's existing SSE stream
- [x] Editing back to the original value returns the footer to clean. A mutation flag passes every other step and fails this one
- [x] Save writes the file and the footer returns to clean
- [x] Discard restores the displayed value and returns to clean
- [x] The save report appears with the wording for a database that is present and matching (`hot-reload live`)
- [x] The page still holds exactly **one** event-stream request afterwards
- [x] Switching to ENTS and back does not lose an unsaved SCHEMA edit — the two modes share one session, and a full page load between them must not reset it

That last one is the point of the story and the easiest to get wrong: mode switching is a full page load, so the session has to live on the server, not in the page.

## Notes

- **The whole epic funnels through here.** No mode calls `schema.Marshal` or touches the filesystem itself; see the rule in the epic README.
- Forge is a local single-user tool, so a mutex around the session is the right amount of concurrency control. Do not build a document-versioning scheme.
- Mode switching is a full page load (Epic 10 Story 5). The session outliving the request is what makes unsaved edits survive it.
- `editable.File.Current` is replaced wholesale by `Discard` and `Reload`, so nothing may cache a pointer into it across a request.
- The save endpoint lives under `/forge/schema/` even though ENTS also saves: there is one file, and inventing a second path for the same document would imply otherwise.

## As Implemented

`internal/forge/session` owns the one editable `schema.json`; the server holds it, both modes render from it, and four action endpoints mutate it. Coverage: `session` 86.2%, `server` 84.3%.

### The two design decisions that carry the story

**`Edit` takes a callback, not a pointer.** `editable.File.Current` is replaced wholesale by `Discard` and `Reload`, so a handler holding a pointer holds a stale one — a hazard Epic 11 Story 4's review found. Routing every mutation through a callback also means no caller can forget the lock. The edit runs against a copy and is adopted only on success, so a handler that validates halfway through cannot leave the schema half-changed.

**`Read` hands out a deep copy.** `DatabaseSchema` is a struct, but its `Components` and `EntityTypes` are maps — so a plain struct copy still shares them, and `delete(d.Components, …)` inside a `Read` would silently mutate the session without taking the lock. Verified by mutation: handing out `s.file.Current` directly fails the test.

Actions answer 204 and the page learns on the SSE stream it already holds. One push path rather than two, and it keeps the save footer, the save report and the engine status arriving by the same route.

### A real bug the e2e found: two path-resolution rules

The spec failed to render a footer at all, and the reason was not in the spec.

**The engine read config paths verbatim — relative to the working directory. `project.Open` resolved them relative to the `game.toml` that declared them.** They disagreed, and the repo's own project hid it: `game.toml` sits at the repo root with `./schema.json`, where both readings coincide. It only surfaced for a config somewhere else, which the e2e fixture is — the fixture's repo-root-relative paths got prefixed with the config's directory and became `e2e/fixtures/project/e2e/fixtures/project/schema.json`.

**Fixed, in `config.Load` rather than in `run.go`.** Calling it a one-line change was wrong: seven distinct path fields are read verbatim — schema, database, map, and four per mod — so fixing only `cfg.Schema.Path` would have left six still disagreeing, including `mod.Behaviors`, which is how Forge finds machines. Resolving once at load makes every consumer agree by construction rather than by each remembering to, and it also makes `ecs-db run -c ../other/game.toml` work at all. `project.Open`'s own resolution is gone: two places claiming that rule is how they drift.

Empty stays empty, deliberately — resolving `""` would yield the project directory, and an assets-only mod would suddenly declare a behaviors directory whose every stray `.json` gets scanned as a machine.

The fixture is also self-contained now — schema, behaviors and database all inside `e2e/fixtures/project/` — which it needed to be regardless, since its paths were relative to the wrong root.

### Two smaller findings, recorded

- **A stale database reports as "no game running".** After bumping `schemaVersion` and saving, the save report says `saved · no game running` — but a database *is* present, it is merely now out of date. `savereport.Observe` folds `StateMismatch` in with `StateOffline`. The consequence is the same (nothing will pick the save up) but the reason is not, and "your database is now stale" is the more actionable thing to say. It belongs to Epic 11 Story 6's outcome set rather than here.
- **`Reload` now clears the file's save report.** Taking what is on disk makes the previous outcome describe a version of the file that is no longer being edited. Found through e2e cross-spec leakage — the editing tests left reports that a later spec saw — but the fix is behavioural rather than a test workaround.

### The dev trigger

`POST /dev/schema/bump` makes one real edit through `Session.Edit`, so the editing path can be driven end to end before Story 2 puts controls on the page. It exercises production code rather than standing in for it, and it disappears when SCHEMA mode has a version badge to click. Without it a browser test of the session would have had to mock the thing under test.

### What the review caught

Three findings, two of them data loss.

**A conflict was an inescapable dead end, and the one control on offer destroyed the edit.** `Reload` and `SaveOverwriting` existed as endpoints with no control anywhere on the page, so after a conflict the only button was Discard — which restores the *stale* snapshot, deleting the user's work, and then reports `Dirty() == false` so the footer renders `✓ saved` over a file it never wrote. I had ticked the AC for this. The conflict report now renders both resolutions, and Discard confirms first, since it throws away work with no undo.

**`clone` was almost entirely unverified: seven of eight mutations survived.** Both fixtures were flat object components with alphabetically-ordered primitive properties, no `behavior`, no array, no nesting — so every branch beyond the trivial one was dead in tests. Because `Edit` *adopts* the clone, each of those was a field silently erased from the user's file on the next save: a dropped `PropertyOrder` reorders the whole file, a dropped `Behavior` unbinds a machine, a dropped `Items` deletes an array's element type. A fixture exercising all of it now kills all seven.

**`clone` failed open on a field added later** — a fresh struct literal drops what it does not name, and nothing noticed. It now copies the value and overrides only the reference fields, so a new scalar is carried by default and a new reference field *aliases* (a leak, which the mutable-path tests catch) rather than *vanishing* (a loss, which nothing catches). Plus a reflect tripwire pinning the field list of all four schema types; verified it is the only test in the repo that fires when a field is added.

### The bug the new fixture found: nested properties were alphabetised on save

The richer fixture failed immediately on a **no-op edit rewriting the file**. Epic 11 Story 2 recorded order for components and their top-level properties but not for nested ones, and documented the asymmetry as acceptable "unless Epic 12 lets someone reorder nested fields".

It did not need to wait for that. A nested block was sorted on *any* save, so opening a file with non-alphabetical nested properties and pressing save rewrote a block nobody touched — the exact churn the order tracking exists to prevent, one level down. `schema.Property` now records its own order through a custom `UnmarshalJSON`, which makes it work at any nesting depth for free.

### The first write endpoints had no cross-origin guard

Forge binds loopback, which is not a security boundary. A POST with no custom headers is a "simple request", so any page the user visits could `fetch('http://127.0.0.1:7777/forge/schema/save')` with no preflight and land the side effect — and `/dev/schema/bump` made it worse by performing a real edit. The five write endpoints now refuse a request whose `Sec-Fetch-Site` says cross-site. A missing header is allowed, because that means a non-browser client; this is a same-origin check, not authentication.

### Smaller, from the same review

- Discard and reload no longer *could* report a save outcome — flipping their `reports` flag survived the whole suite, and would have shown `saved · hot-reload live` for a file Forge never wrote.
- The Save button being wired to the *overwrite* endpoint survived too, which would have clobbered an external edit with nothing noticing.
- `renderFooter` and the dev trigger had no Go coverage; both now do.
- The e2e restores the tracked fixture in `afterAll` as well as `afterEach`, so an interrupted run cannot leave it modified. `.stash` leftovers are gitignored — `*.db` does not match `.db.stash`.
- `make test` now runs `-race` over the packages with concurrency to get wrong. Without it `TestSession_IsSafeForConcurrentUse` proved nothing.

### Verified by mutation

| Mutation | Caught by |
|---|---|
| `Read` hands out the session's own maps | the mutable-path test |
| `Edit` mutates in place, so a failed edit half-applies | the failed-edit test |
| `Read` without the lock | `-race`, via the concurrency test |
| a save records no report | the refused-save and reload tests |
| the footer always reports clean | the footer test |
| the footer is always dirty | the footer test |
| no fallback when there is no session | two page tests |
| `Reload` leaves the stale report | the stale-report test |
| `clone` drops nested `PropertyOrder` | the no-op-edit test |
| `cloneProperty` does not recurse | the mutable-path test |
| `Component.Items` is aliased | the mutable-path test |
| `cloneStrings` aliases instead of copying | the mutable-path test |
| a field added to a schema type | the reflect tripwire |
| no cross-origin guard | the cross-site test |
| Save skips the conflict check | the conflict-path test |
| discard reports a save it never made | the non-writing-actions test |
| `Marshal` drops nested property order | the nested-order test, and the no-op edit |

The bottom ten all **survived** the original suite.
