# Story 6: Delayed Transitions (`after`)

**Epic:** 3 — Agents (behavior-as-data) runtime  
**Status:** 🔲 Not started  
**Priority:** High — `after` is used in the wandering_goblin example and most real behaviors

**Depends on:** Story 5 (interpreter entry/exit hooks)

## Context

XState v4's `after` keyword schedules a transition to fire automatically after a delay. The delay is expressed as a duration string (`"500ms"`, `"1s"`, `"2.5s"`) or an integer in milliseconds (`"500"`). In a tick-based game engine with no real-time clock, "time" is ticks.

The scheduler converts `after` durations to tick counts at machine load time and stores them. On state entry, a row is inserted into `event_queue` with `target_tick = current_tick + duration_ticks`. On state exit, any pending `event_queue` rows for that (entity, machine, state) are deleted — a stale `after` must not fire after the state has already changed.

The tick loop (Epic 5) drains `event_queue` rows where `target_tick ≤ current_tick` and delivers them as synthetic events to the interpreter. This story only implements scheduling and cancellation; Epic 5 wires the drain loop.

## Acceptance Criteria

- [ ] `after` duration strings parsed and converted to integer tick counts at machine load time
  - Millisecond integers: `"500"` → 500ms → `ceil(500 / tick_duration_ms)` ticks
  - Duration strings: `"500ms"`, `"1s"`, `"1.5s"`, `"2m"` all handled
  - Invalid duration string → validation error at load time (not runtime)
- [ ] On entering a state with `after` transitions, one `event_queue` row is inserted per `after` entry: `entity_id`, `machine_id`, `event_type = "after:<state-id>:<duration>"`, `target_tick`
- [ ] On exiting a state, all `event_queue` rows for that `(entity_id, machine_id, state-id)` are deleted
- [ ] Scheduling and cancellation happen inside the same `*sql.Tx` as the microstep that caused the entry/exit
- [ ] Tick duration (ms per tick) is configurable; defaults to 50ms (20Hz)
- [ ] A state with multiple `after` entries schedules all of them; only the first to fire (lowest `target_tick`) will have an effect since entering a new state cancels the others
- [ ] Tests: enter state with `after` → row appears in `event_queue`; exit state → row deleted; re-enter state → new row with updated `target_tick`
- [ ] 100% test coverage on scheduling and cancellation logic

## Notes

- `internal/agent/scheduler.go`
- Duration parsing: use `time.ParseDuration` for strings like `"500ms"`, `"1s"`. For bare integer strings (XState's ms shorthand), parse as `int * time.Millisecond`.
- The `event_type` format for after events (`"after:<state-id>:<duration>"`) must be stable — the tick loop uses it to route the event back to the correct state's `after` handler.
- Tick duration is an interpreter-level config, not per-machine. Pass it in as a dependency rather than hardcoding.
- Cancellation on exit is a delete query: `DELETE FROM event_queue WHERE entity_id = ? AND machine_id = ? AND event_type LIKE 'after:<state-id>:%'`. The `state-id` in the pattern must be the XState state ID (full path for nested states).
