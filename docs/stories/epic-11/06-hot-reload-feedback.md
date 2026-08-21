# Story 6: Hot-reload feedback

**Epic:** 11 — Forge: project model & engine file I/O  
**Status:** ✅ Complete  
**Priority:** Medium — closes the loop this epic opens

**Depends on:** Stories 4 and 5

## Context

This epic's promise is a loop: edit in Forge, save, the running engine picks the change up. Stories 1–5 build the first half. Nothing yet tells you the second half happened.

Epic 10's engine-status readout says whether the database is present and compatible. That is a standing condition. This is different and narrower: *the file I just saved was reloaded, or it was rejected, and here is why.* A save that the engine refuses is the most important thing Forge can tell you, and today it goes to the game process's stdout where nobody editing is looking.

There is also a loose end worth closing here. `agent.Reconciler.Reconcile` was written and tested in Epic 4 and has never had a caller — `watcher.go` still carries the comment "nil until wired in Epic 5". It exists to fix up entities whose current state no longer exists after a machine is hot-swapped, which is exactly what happens when someone deletes a state in Forge and saves. Wiring it via `Watcher.SetReconcileFunc` turns a latent correctness bug into working behaviour, and this is the story where the scenario becomes reachable through the UI.

## Acceptance Criteria

- [x] After a save, Forge reports the outcome for that file: reloaded, or rejected with the validation errors
- [x] Rejection shows **all** errors, not the first — `ValidateMachine` returns a slice for that reason
- [x] The report is pushed down the existing page-level SSE stream, not a new endpoint
- [x] The report distinguishes "the engine is not running, so nothing reloaded" from "the engine rejected this" — a save with no game running is not a failure
- [x] `agent.Reconciler.Reconcile` is wired via `Watcher.SetReconcileFunc` in the engine's composition root, and `watcher.go`'s stale "nil until wired in Epic 5" comment is removed
- [x] A test covering the scenario that makes reconciliation matter: an entity sits in a state, the machine is reloaded without that state, and the entity is reconciled rather than left pointing at nothing
- [x] Save feedback is per-file, so saving one machine does not clear the error shown for another
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/11-hot-reload-feedback.spec.js`. This is the first Forge feature whose
correctness depends on something happening in *another* process, so the browser
test needs the engine's file watcher in the loop, not a mock of it.

- [ ] Saving a valid change reports success, without a page reload
- [ ] Saving a change that fails validation reports the errors, and reports **all** of them
- [ ] A rejected save leaves the previous version in service — the engine keeps running the old machine
- [ ] Feedback for one file is not cleared by saving another
- [ ] With no game running, a save reports "saved, nothing listening" rather than an error
- [ ] The report arrives on the page's existing stream — assert the page still holds exactly one event-stream request, as Story 5 of Epic 10 does

The fixture project is the place to do this: `e2e/fixtures/project/behaviors/`
already holds `e2e-wander.json`, and the suite can write a deliberately invalid
machine into it and restore it afterwards. Follow the pattern
`06-engine-status.spec.js` established — destructive specs run in their own
Playwright project with a `dependencies` edge so they cannot race the read-only
ones.

## Notes

- **"Nothing was listening" is not an error and must not look like one.** Forge is useful with no game running; that is the normal case while authoring. Reporting a save as failed because nothing reloaded it would be false and would train people to ignore the readout.
- Forge cannot observe the engine's reload directly — separate processes, and the architecture keeps Forge read-only on the database. What it *can* do is validate the same way the engine will, using the same `ValidateMachine` and `ValidateSchema` calls, and report that. Be precise in the UI copy about which of the two is being claimed; "the engine accepted this" is a stronger claim than Forge can make, and "this would validate" is the honest one.
- The reconciler wiring is engine-side and belongs in `cmd/ecs-db/run.go`, not in Forge. Forge is the reason the scenario became reachable, not the place it is fixed.

## As Implemented

`internal/forge/savereport` decides what to say; `components.SaveReports` renders it; the server holds the latest report per file and pushes it down the page-level SSE stream alongside the engine status. `agent.ReconcileOnReload` finally gives `Reconciler.Reconcile` the caller it has been waiting for since Epic 4.

Coverage: `savereport` 95.8%, `agent` 88.4%.

### Being precise about the claim

Forge and the engine are separate processes, and the architecture keeps Forge read-only on the database, so Forge **cannot observe a reload**. What it can do is run the same validation the engine will and report that. The wording follows:

| Outcome | What Forge actually knows |
|---|---|
| `saved · hot-reload live` | written; a compatible database is present, so something is listening |
| `saved · no game running` | written; nothing is listening |
| `not saved · N problems` | it would not validate, so nothing was written |
| `not saved · changed on disk since you opened it` | someone else edited it |

`TestReport_Summary` asserts the connected wording does **not** contain "engine accepted" or "reloaded" — those are stronger claims than Forge can make, and the test exists to stop a future copy edit quietly making one.

"Nothing was listening" is deliberately not styled as a failure. Forge is useful with no game running; that is the normal case while authoring, and reporting it as an error would train people to ignore the readout. It gets amber, not red.

### The reconciler wiring

`ReconcileFunc` takes a machine ID and its valid states; `Reconciler.Reconcile` also needs a context, a database and the machine's initial state. `ReconcileOnReload` bridges them, recovering the initial state from the loader, which by then holds the newly loaded definition.

It lives in `internal/agent` rather than in the composition root so it can be tested: `cmd/ecs-db/run.go` is behind the `ebitengine` tag and needs X11 to compile. The end-to-end scenario is covered — an entity sits in `chasing`, the machine is reloaded without `chasing`, and the entity is reset to the machine's initial state rather than left pointing at nothing.

Errors are logged rather than returned, because there is nobody to return them to: this runs on the watcher goroutine after the file has already been accepted. The awkward cases are handled rather than assumed away, since a panic there would take the watcher down — an unknown machine, a closed database, and a nil `Reconciler` (which yields a nil callback, not one that panics on first use).

`watcher.go`'s "nil until wired in Epic 5" comment, wrong since Epic 5 shipped, is gone.

### The browser spec is partly deferred, deliberately

Story 6's Playwright steps assume a save button, which is Epic 12. What could be tested in a browser now is tested: the report container is asserted present in the **server-rendered HTML** on first paint, because a Datastar patch finds its target by id and a page that rendered nothing until the first save would have nowhere to put it. The rest — saving a valid change, saving an invalid one, feedback for one file surviving another's save — is covered by Go tests driving the real SSE stream through `httptest`, and the browser spec lands with its trigger in Epic 12.

`Server.ReportSave` therefore has no production caller yet. That is a deliberate choice rather than an oversight: the shape this story exists to establish is *one stream, per file, suppressed when unchanged*, and Epic 12 should inherit a working pattern rather than invent one under time pressure.

### One thing this changed elsewhere

The page now has two live regions, so the opening burst on the stream is legitimately two patches rather than one. Both the Go test and the e2e spec that assert "an unchanged status is not re-patched" were counting frames in total; they now count **per element**, which is what they meant all along and which keeps catching a stream that re-sends.

### What the review caught

**A corruption bug in the reconciler wiring.** A machine with every state deleted still validates — `validateInitial` only demands an `initial` when there are children — so `def.Initial` comes back empty, and reconciling against it wrote `[""]` into `current_states`. That leaves the entity pointing at a state that does not exist, which is precisely what the reconciler exists to prevent, and `LoadAgent` silently skips unknown state IDs so nothing would have surfaced. It is reachable exactly as this story argues: deleting states is an ordinary edit, and deleting the last one is one more click. The callback now refuses to move an entity when there is nowhere valid to move it to — leaving it where it is beats moving it nowhere.

**The "all errors" criterion was ticked and unreachable.** Nothing in production produced a joined error: `ValidateSchema` short-circuits on its first phase, and `ValidateMachine` returns a slice whose only caller already collapses it to a semicolon-joined string. So the UI would have rendered one bullet containing everything, and the two branches of `flatten` that handle joins both survived deletion — including one whose test comment described a fixture that was not there ("a joined error nested inside a wrap, which is what editable.Save produces" was passing a bare join with no wrap).

Rather than un-ticking it, `agent.ValidateMachineError` now returns `errors.Join` of every failure, which is the shape a validation hook can take and a caller can pull apart. `flatten` is tested against the real shapes: a join inside a wrap, and a join inside a join.

`schema.ValidateSchema` still short-circuits by phase. That is engine behaviour, not Forge's, and changing it is not this story's business — but it means a schema save reports the first phase's failures only, and Epic 12 should know that before wiring its validator.

**Two render paths had no Go coverage.** Deleting `@components.SaveReports` from the shell left every Go test green, so every pushed patch would have landed on nothing — the exact silent failure this story's design notes warn about — and only `make e2e` caught it. Passing `nil` instead of the real reports left the suite green too, so a page loaded after a failed save would have dropped the report until the next stream tick.

**And a doc claim of mine was wrong.** I wrote that both the Go test and the e2e spec now count patches per element. Only the Go one did; the spec still counted totals and had merely changed `1` to `2`, which passes just as happily if one region is sent twice and the other never. Fixing it surfaced the **third instance in this project of the same prefix collision**: `data-testid="engine-status"` ends with the substring `id="engine-status"`, so the unanchored regex counted both attributes and reported two patches where there was one. After `checkbox__box` in Epic 10 and the readout's own id in Story 6, that pattern is worth naming: any `id="x"` match needs anchoring while `data-testid="x"` exists alongside it.

Smaller: `plural()` always saying "s" passed everything, because the only rejection fixture had two problems — `1 problems` would have shipped. `Report.Rendered()` was dead code that existed only to be tested; the template now uses it, so an outcome-less report is skipped instead of rendering as a blank chip. And a pre-existing test lacked `defer cancel()`, so any `t.Fatal` before its explicit cancel would hang the package until the ten-minute timeout rather than failing.

### Verified by mutation

| Mutation | Caught by |
|---|---|
| report only the first validation error | the all-errors case |
| invert the engine-running distinction | the nothing-listening case |
| report a conflict as a plain rejection | the conflict case |
| a new report clears every other file's | the per-file test, and the SSE test |
| call a save with no engine a failure | the summary test |
| reset a reconciled entity to the wrong initial state | the deleted-state test |
| reset to an empty initial state when every state is deleted | the reset-to-nowhere test |
| drop the save-report target from the shell | the patch-target test, and the page test |
| do not pass existing reports to the shell | the page test |
| `ValidateMachineError` returns only the first failure | the join test |
| `flatten` stops recursing | the join-inside-a-join case |
| `plural` always says "s" | the one-problem case |

The bottom seven all **survived** the original suite.
