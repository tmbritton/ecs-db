# Story 6: Engine-status readout

**Epic:** 10 — Forge: foundation, toolchain & design system  
**Status:** 🔲 Not started  
**Priority:** Medium — completes the shell; first live SSE path in Forge

**Depends on:** Story 5 (menu bar status slot)

## Context

Forge writes files; the engine watches them and hot-reloads. That loop is invisible unless the tool says something about it, and the difference matters: when a game is running with its watcher live, a save takes effect in seconds; when nothing is running, the same save just sits on disk until someone starts the game. The readout in the top-right of the menu bar tells the user which world they are in.

Connected shows a green square and `schema.json v3 · mods/core · hot-reload live`. Disconnected shows a red square and `watcher offline · edits queue until game restarts`. Both are JetBrains Mono, because every value in them comes from the engine rather than the user.

"Connected" is a deliberately modest claim: the database file named by `cfg.Database.Path` exists, opens read-only, and its recorded `schema_version` matches the `schema.json` Forge has loaded. That is all Forge can honestly know from outside the game process — SQLite has no way to notify a reader that a writer is live. A version mismatch is reported distinctly from absence, because it means something quite different: a stale database that the engine itself would refuse to start against.

This is the first thing to travel down Forge's SSE stream, and it is deliberately the simplest — a low-frequency status check, rendered server-side and pushed as an HTML patch. It shares the page-level stream Story 5 stubbed rather than opening one of its own: Datastar pages subscribe once and receive every update over that one connection. Getting the lifecycle right on this quiet stream makes Epic 18's much busier `world_version` traffic a variation on a working pattern rather than a new problem.

## Acceptance Criteria

- [ ] `internal/forge/status` exposes a checker with no HTTP or template knowledge:
  ```go
  type State int
  const (
      StateOffline State = iota // no database file
      StateMismatch             // schema_version disagrees with schema.json
      StateConnected            // opened read-only, versions agree
  )

  type Status struct {
      State         State
      SchemaVersion int    // from schema.json
      DBVersion     int    // from meta.schema_version; 0 when offline
      ModName       string // first configured mod
  }

  func Check(cfg Config) Status
  ```
- [ ] The database is opened **read-only** (`file:<path>?mode=ro`), never through `storage.NewSQLiteStore`, which creates directories and would bootstrap or migrate
- [ ] Version read via the existing `storage.ReadSchemaVersion`
- [ ] A missing database file is `StateOffline`, not an error
- [ ] Renders in the menu bar's right slot:
  - connected → green `■` + `schema.json v<N> · mods/<name> · hot-reload live`
  - mismatch → red `■` + `schema v<N> ≠ db v<M> · engine would refuse this database`
  - offline → red `■` + `watcher offline · edits queue until game restarts`
- [ ] Status patches are pushed down the **page-level stream** (`GET /forge/{mode}/events`, stubbed in Story 5), patching `#engine-status` — not a second SSE endpoint of its own
- [ ] Status flips within one poll interval (default 2s) when a game starts or stops
- [ ] The stream ends cleanly when the client disconnects — no leaked goroutine or ticker
- [ ] Table-driven tests covering all three states, using temp-file databases
- [ ] `go test ./...` passes

## Notes

- Read-only is a correctness requirement, not a nicety. The architecture's one-writer-per-table contract makes the interpreter the sole writer of world state; Forge attaching read-write would be the architectural bug the contract exists to prevent. Add a test that asserts a write through this connection fails.
- Do not report "connected" as "the game is running" in the UI copy. It means the database is present and compatible. Overstating it is worse than saying less, because the whole point of the readout is to tell the user whether a save will take effect.
- The poll interval is a preference in the prototype (`reconnectSec`, default 2). Take it from config now with a 2s default rather than hard-coding, so Epic 17's Preferences dialog has something to bind to.
- Open the connection per check and close it, rather than holding one open. A held read connection against a WAL database is harmless but keeps `-shm`/`-wal` files pinned, which is a confusing thing for an authoring tool to do to a project directory.
- Use `sse.Context()` to detect disconnect and stop the ticker; the SDK wires it to the request context.
- Only push when the rendered status actually changes. A 2s tick that patches identical HTML forever is wasted work on both ends, and it makes the browser's SSE log useless for debugging Epic 18.
