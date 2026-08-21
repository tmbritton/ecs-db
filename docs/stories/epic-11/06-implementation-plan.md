# Epic 11 Story 6: Hot-reload feedback — Implementation Plan

**Goal:** after a save, say what happened to it.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/templates/components/savereport.templ` | The per-file outcome |
| Modify | `internal/forge/server/server.go` | Push the report on the page stream |
| Modify | `cmd/ecs-db/run.go` | Wire `Reconciler.Reconcile` |
| Modify | `internal/agent/watcher.go` | Remove the stale "nil until wired" comment |

---

## Task 1: Be precise about the claim

Forge and the engine are separate processes. Forge cannot observe the engine's
reload, and the architecture deliberately keeps it read-only on the database.
What it can do is run the same validation the engine will — `ValidateMachine`,
`ValidateSchema` — and report *that*.

So there are three outcomes, and the copy must not blur them:

| Outcome | Meaning | Copy |
|---|---|---|
| valid, engine running | saved; the watcher will pick it up | `saved · hot-reload live` |
| valid, no engine | saved; nothing is listening | `saved · no game running` |
| invalid | not written; here is why | the error list |

"The engine accepted this" is a stronger claim than Forge can make. Do not make
it.

"Engine running" reuses `internal/forge/status` — it is the same question the
menu bar readout already answers, and answering it twice differently would be
worse than not answering it here at all.

## Task 2: Errors, plural

`ValidateMachine` returns `[]error` precisely so all of them can be shown.
Render the list. A first-failure-only report sends someone round the loop once
per mistake.

## Task 3: The reconciler

`cmd/ecs-db/run.go` builds the watcher. Wire:

```go
watcher.SetReconcileFunc(reconciler.Reconcile)
```

Then delete the "nil until wired in Epic 5" comment in `watcher.go`, which has
been wrong since Epic 5 shipped.

The test that makes it meaningful: create an entity in state `chasing`, reload
the machine with `chasing` removed, and assert the entity is reconciled to a
valid state rather than left pointing at one that no longer exists. This is
reachable through the UI for the first time in this epic — deleting a state is
an ordinary edit in Epic 13's canvas.

---

## Verification

`go test ./...`, then the loop by hand: `ecs-db run` in one terminal, `ecs-db
forge` in another, save a valid change and watch the game pick it up; save an
invalid one and watch Forge report it while the game keeps running the old
version.

Then `make e2e`, including the new spec.
