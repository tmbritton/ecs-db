# Story 3: Hot-Reload Entity Semantics

**Epic:** 4 — Behavior hot reload  
**Status:** ✅ Complete  
**Priority:** Medium — completes the hot-reload experience; entities must not get stranded

**Depends on:** Story 2 (watcher fires after a machine definition is swapped)

## Context

When a machine definition is hot-reloaded (Story 2), entities that are currently running that machine may be in a state that no longer exists in the new definition. Without reconciliation, those entities would be stranded: their `behavior_components.current_states` row references a state the interpreter cannot find, causing errors or silent broken behaviour on the next tick.

This story adds reconciliation logic that runs immediately after a hot-reload swap. For each active entity running the reloaded machine, the interpreter checks whether its current state(s) are still valid. If they are, nothing changes — the entity continues from exactly where it was. If any active state has been removed, the entity is reset to the machine's `initial` state and the reset is logged.

## Acceptance Criteria

- [x] `Loader.ReloadFile` (from Story 2) accepts an optional reconciliation callback: `type ReconcileFunc func(machineID string, validStates map[string]bool)`
- [x] `internal/agent/reconcile.go` — `Reconciler` type with method `Reconcile(ctx context.Context, db *sql.DB, machineID string, validStates map[string]bool, initial string) error`
  - Queries `behavior_components` for all rows where `machine_id = machineID`
  - For each row, parses `current_states` (JSON array of state names)
  - If all current states are in `validStates`: no-op
  - If any current state is absent from `validStates`: resets `current_states` to `[initial]` and writes the row back in a transaction
  - Logs every reset: `[hot-reload] entity <id> machine "<machine_id>" reset to "<initial>" (removed states: [<list>])`
  - Logs every skipped entity: `[hot-reload] entity <id> machine "<machine_id>" state valid, no reset`
- [x] Reconciliation runs within one SQLite transaction per machine (all entity resets for that machine are atomic)
- [x] If the reconciliation transaction fails, it is rolled back; the old `behavior_components` rows are preserved; error is returned
- [x] `internal/agent/reconcile_test.go` — tests using a real in-memory SQLite database
  - Seed `behavior_components` rows with known current states
  - Call `Reconcile` with a `validStates` set that excludes some of those states
  - Assert rows with removed states are reset to `initial`
  - Assert rows with valid states are unchanged
- [x] All new code has tests; `go test ./...` passes

## Notes

- `behavior_components` schema: `entity_id INTEGER`, `machine_id TEXT`, `current_states TEXT` (JSON array), `updated_at INTEGER`. Primary key is `(entity_id, machine_id)`. There is no `history_states` or `context` column — those live elsewhere.
- `validStates` is built from `collectStateIDs(def.States)` inside `ReloadFile` and passed to the `ReconcileFunc` callback. No separate `AllStateIDs` helper was needed.
- Reconciliation is a best-effort operation: it runs after the swap, not instead of it. Even if reconciliation partially fails, the new definition is already in memory and the interpreter can keep ticking.
- The `Reconciler` is wired into the watcher callback in the interpreter (Epic 5). For now, it is exposed and tested in isolation via direct calls.
- Pending `event_queue` rows for reset entities are deleted (`DELETE FROM event_queue WHERE entity_id = ? AND machine_id = ?`) — the reset to `initial` re-establishes any timers the `initial` state needs on next entry.
- The SELECT cursor is closed explicitly before `BeginTx` to avoid `SQLITE_BUSY` on the shared `*sql.DB` connection pool.
