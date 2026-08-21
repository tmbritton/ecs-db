# Story 6: Hot-reload feedback

**Epic:** 11 — Forge: project model & engine file I/O  
**Status:** 🔲 Not started  
**Priority:** Medium — closes the loop this epic opens

**Depends on:** Stories 4 and 5

## Context

This epic's promise is a loop: edit in Forge, save, the running engine picks the change up. Stories 1–5 build the first half. Nothing yet tells you the second half happened.

Epic 10's engine-status readout says whether the database is present and compatible. That is a standing condition. This is different and narrower: *the file I just saved was reloaded, or it was rejected, and here is why.* A save that the engine refuses is the most important thing Forge can tell you, and today it goes to the game process's stdout where nobody editing is looking.

There is also a loose end worth closing here. `agent.Reconciler.Reconcile` was written and tested in Epic 4 and has never had a caller — `watcher.go` still carries the comment "nil until wired in Epic 5". It exists to fix up entities whose current state no longer exists after a machine is hot-swapped, which is exactly what happens when someone deletes a state in Forge and saves. Wiring it via `Watcher.SetReconcileFunc` turns a latent correctness bug into working behaviour, and this is the story where the scenario becomes reachable through the UI.

## Acceptance Criteria

- [ ] After a save, Forge reports the outcome for that file: reloaded, or rejected with the validation errors
- [ ] Rejection shows **all** errors, not the first — `ValidateMachine` returns a slice for that reason
- [ ] The report is pushed down the existing page-level SSE stream, not a new endpoint
- [ ] The report distinguishes "the engine is not running, so nothing reloaded" from "the engine rejected this" — a save with no game running is not a failure
- [ ] `agent.Reconciler.Reconcile` is wired via `Watcher.SetReconcileFunc` in the engine's composition root, and `watcher.go`'s stale "nil until wired in Epic 5" comment is removed
- [ ] A test covering the scenario that makes reconciliation matter: an entity sits in a state, the machine is reloaded without that state, and the entity is reconciled rather than left pointing at nothing
- [ ] Save feedback is per-file, so saving one machine does not clear the error shown for another
- [ ] `go test ./...` passes

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
