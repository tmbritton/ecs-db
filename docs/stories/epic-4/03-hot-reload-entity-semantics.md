# Story 3: Hot-Reload Entity Semantics

**Epic:** 4 — Behavior hot reload  
**Status:** 🔲 Not started  
**Priority:** Medium — completes the hot-reload experience; entities must not get stranded

**Depends on:** Story 2 (watcher fires after a machine definition is swapped)

## Context

When a machine definition is hot-reloaded (Story 2), entities that are currently running that machine may be in a state that no longer exists in the new definition. Without reconciliation, those entities would be stranded: their `behavior_components.active_states` row references a state the interpreter cannot find, causing errors or silent broken behaviour on the next tick.

This story adds reconciliation logic that runs immediately after a hot-reload swap. For each active entity running the reloaded machine, the interpreter checks whether its current state(s) are still valid. If they are, nothing changes — the entity continues from exactly where it was. If any active state has been removed, the entity is reset to the machine's `initial` state and the reset is logged.

## Acceptance Criteria

- [ ] `Loader.ReloadFile` (from Story 2) accepts an optional reconciliation callback: `type ReconcileFunc func(machineID string, validStates map[string]bool)`
- [ ] `internal/agent/reconcile.go` — `Reconciler` type with method `Reconcile(ctx context.Context, db *sql.DB, machineID string, validStates map[string]bool, initial string) error`
  - Queries `behavior_components` for all rows where `machine_id = machineID`
  - For each row, parses `active_states` (JSON array of state names)
  - If all active states are in `validStates`: no-op
  - If any active state is absent from `validStates`: resets `active_states` to `[initial]` and writes the row back in a transaction
  - Logs every reset: `[hot-reload] entity <id> machine "<machine_id>" reset to "<initial>" (removed states: [<list>])`
  - Logs every skipped entity: `[hot-reload] entity <id> machine "<machine_id>" state valid, no reset`
- [ ] Reconciliation runs within one SQLite transaction per machine (all entity resets for that machine are atomic)
- [ ] If the reconciliation transaction fails, it is rolled back; the old `behavior_components` rows are preserved; error is logged
- [ ] `internal/agent/reconcile_test.go` — tests using a real in-memory SQLite database
  - Seed `behavior_components` rows with known active states
  - Call `Reconcile` with a `validStates` set that excludes some of those states
  - Assert rows with removed states are reset to `initial`
  - Assert rows with valid states are unchanged
- [ ] All new code has tests; `go test ./...` passes

## Notes

- `behavior_components` schema (from Epic 3, Story 1): `entity_id INTEGER`, `machine_id TEXT`, `active_states TEXT` (JSON array), `history_states TEXT` (JSON object), `context TEXT` (JSON object). Primary key is `(entity_id, machine_id)`.
- `validStates` is built from the new `MachineDefinition`'s state map — all state IDs reachable in the tree. The `Loader.AllStateIDs(machineID string) map[string]bool` helper (or equivalent) is the right place to produce this set.
- History states (`history_states`) may also reference removed states. Clear the history entry for any history node whose recorded target is no longer valid — the machine will fall back to the history node's default target on next entry.
- Reconciliation is a best-effort operation: it runs after the swap, not instead of it. Even if reconciliation partially fails, the new definition is already in memory and the interpreter can keep ticking.
- The `Reconciler` is wired into the watcher callback in the interpreter (Epic 5). For now, expose and test it in isolation via direct calls.
- Do not attempt to preserve mid-transition state (e.g., active timers in `event_queue` for removed states). Cancel pending `event_queue` rows for the affected entity where `payload` references the removed state. This prevents stale timers firing after reset.
  - `DELETE FROM event_queue WHERE entity_id = ? AND machine_id = ?` for each reset entity is the safe default — the reset to `initial` re-establishes any timers the `initial` state needs on next entry.
