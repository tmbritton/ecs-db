# Story 6: Engine-status readout

**Epic:** 10 — Forge: foundation, toolchain & design system  
**Status:** ✅ Complete  
**Priority:** Medium — completes the shell; first live SSE path in Forge

**Depends on:** Story 5 (menu bar status slot)

## Context

Forge writes files; the engine watches them and hot-reloads. That loop is invisible unless the tool says something about it, and the difference matters: when a game is running with its watcher live, a save takes effect in seconds; when nothing is running, the same save just sits on disk until someone starts the game. The readout in the top-right of the menu bar tells the user which world they are in.

Connected shows a green square and `schema.json v3 · mods/core · hot-reload live`. Disconnected shows a red square and `watcher offline · edits queue until game restarts`. Both are JetBrains Mono, because every value in them comes from the engine rather than the user.

"Connected" is a deliberately modest claim: the database file named by `cfg.Database.Path` exists, opens read-only, and its recorded `schema_version` matches the `schema.json` Forge has loaded. That is all Forge can honestly know from outside the game process — SQLite has no way to notify a reader that a writer is live. A version mismatch is reported distinctly from absence, because it means something quite different: a stale database that the engine itself would refuse to start against.

This is the first thing to travel down Forge's SSE stream, and it is deliberately the simplest — a low-frequency status check, rendered server-side and pushed as an HTML patch. It shares the page-level stream Story 5 stubbed rather than opening one of its own: Datastar pages subscribe once and receive every update over that one connection. Getting the lifecycle right on this quiet stream makes Epic 18's much busier `world_version` traffic a variation on a working pattern rather than a new problem.

## Acceptance Criteria

- [x] `internal/forge/status` exposes a checker with no HTTP or template knowledge
      (**four** states, and taking a schema *path* — see *As Implemented*):
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
- [x] The database is opened **read-only** (`file:<path>?mode=ro`), never through `storage.NewSQLiteStore`, which creates directories and would bootstrap or migrate
- [x] Version read via the existing `storage.ReadSchemaVersion`
- [x] A missing database file is `StateOffline`, not an error
- [x] Renders in the menu bar's right slot:
  - connected → green `■` + `schema.json v<N> · mods/<name> · hot-reload live`
  - mismatch → red `■` + `schema v<N> ≠ db v<M> · engine would refuse this database`
  - offline → red `■` + `watcher offline · edits queue until game restarts`
- [x] Status patches are pushed down the **page-level stream** (`GET /forge/{mode}/events`, stubbed in Story 5), patching `#engine-status` — not a second SSE endpoint of its own
- [x] Status flips within one poll interval (default 2s) when a game starts or stops
- [x] The stream ends cleanly when the client disconnects — no leaked goroutine or ticker
- [x] Table-driven tests covering all three states, using temp-file databases
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/06-engine-status.spec.js`, to be written **before** the handler. This
is the first thing to travel down Forge's SSE stream, and the failure mode is
the one the whole suite exists for: a status that never arrives looks exactly
like a status that has not changed.

The fixture project's database is at `tmp/e2e/e2e.db` with `schema_version` 7,
matching `e2e/fixtures/project/schema.json`. All three states are reachable by
manipulating that file, which is why the suite seeds its own rather than
pointing at the developer's.

- [x] Connected: the readout shows the green marker and `hot-reload live`, and
      names the fixture's own values (`v7`, `mods/e2e-core`) — which also proves
      Forge read the fixture project rather than the repo's
- [x] The first patch arrives **without waiting a full poll interval**, so a
      reconnecting client is not briefly blank
- [x] The patch is HTML on the page's existing stream, not a second connection:
      assert the page still holds exactly one event-stream request
- [x] Deleting the database mid-stream flips the readout to `watcher offline`
      within one poll interval — the test that actually pins the story's promise
- [x] Restoring it flips back to connected
- [x] A `schema_version` that disagrees renders the mismatch state with **both**
      version numbers, and is distinguishable from offline
- [x] Identical status does **not** re-patch: count `datastar-patch-elements`
      frames over several intervals and assert the count stops growing once the
      state is steady
- [x] Closing the tab ends the handler — assert the server's open-stream count
      returns to zero
- [x] ~~Forge leaves no `-wal`/`-shm` files behind~~ — **not achievable**; see
      *As Implemented*. Replaced by: Forge never modifies the database file

## Notes

- Read-only is a correctness requirement, not a nicety. The architecture's one-writer-per-table contract makes the interpreter the sole writer of world state; Forge attaching read-write would be the architectural bug the contract exists to prevent. Add a test that asserts a write through this connection fails.
- Do not report "connected" as "the game is running" in the UI copy. It means the database is present and compatible. Overstating it is worse than saying less, because the whole point of the readout is to tell the user whether a save will take effect.
- The poll interval is a preference in the prototype (`reconnectSec`, default 2). Take it from config now with a 2s default rather than hard-coding, so Epic 17's Preferences dialog has something to bind to.
- Open the connection per check and close it, rather than holding one open. A held read connection against a WAL database is harmless but keeps `-shm`/`-wal` files pinned, which is a confusing thing for an authoring tool to do to a project directory.
- Use `sse.Context()` to detect disconnect and stop the ticker; the SDK wires it to the request context.
- Only push when the rendered status actually changes. A 2s tick that patches identical HTML forever is wasted work on both ends, and it makes the browser's SSE log useless for debugging Epic 18.

## As Implemented

`internal/forge/status` is a pure domain package — paths in, a `Status` out, no HTTP and no templates — so its whole state space is unit-testable against temp files. `components.EngineStatus` renders it, the shell renders it server-side on load, and `handleModeEvents` re-checks it on a ticker and patches it down the page-level stream Story 5 stubbed. No new route and no second stream.

Coverage: `status` 95.5%, `server` 91.0%, `mode` 100%, `components` 74.4%.

### The requirement that could not be met

The story asked that Forge leave no `-wal`/`-shm` files beside the project's database. That is not achievable, and `TestCheck_DoesNotDisturbTheProject` records why rather than quietly dropping it.

The engine opens with `journal_mode = WAL`. A **read-only** connection to a WAL database must create the `-shm` to read it at all, and then cannot checkpoint or delete it on close, precisely because it is read-only. Every way around that is worse:

- `immutable=1` creates no sidecars, but tells SQLite the file cannot change and returns stale or torn reads exactly when a game *is* running — the case that matters.
- Opening read-write with `PRAGMA query_only` lets SQLite clean up after itself, but grants write access to files the engine owns and permits checkpointing. That is the one-writer-per-table violation the whole design avoids.
- Deleting the sidecars ourselves would be catastrophic against a live engine.

So the sidecars are accepted and the test pins the two things that actually matter: the database file is byte-identical after repeated checks, and nothing beyond SQLite's own read sidecars appears. When a game is running they already exist and Forge adds nothing; when one is not, the next engine run clears them.

### Divergences from the plan

- **A fourth state, `StateSchemaUnreadable`, and `Config` takes `SchemaPath` rather than `SchemaVersion`.** Two problems with the specified shape, both found in review. First, a schema.json Forge cannot read left the version at zero, which `Check` compared against a real database and reported as `schema v0 ≠ db v7 · engine would refuse this database` — blaming the database for a file the engine may well be reading perfectly well. Second, and worse in the long run: a version captured once at startup goes stale the moment someone edits schema.json, which is precisely what Forge is *for*. Epic 12 would have shipped a readout that lied after every version bump. The check now re-reads the file each poll, through the engine's own `schema.LoadSchema`, and reports an unreadable schema as its own state naming the right file.
- **`StateOffline` absorbs more than "no file".** A file that is not a database, and a real database mid-bootstrap with no `meta` table yet, are both offline rather than errors. A half-written database during game startup is transient, and there is no useful third thing to tell the user.
- **Both failure states share the red dot.** The sentence beside it carries the difference between "no database" and "a stale one"; a third hue would claim a distinction the palette does not make.
- **The schema version is read in `cmd/ecs-db/forge.go`, not in `status`.** It keeps the status package free of file formats. A schema that will not load is logged and reported as offline rather than refusing to start the editor — fixing a broken schema is exactly what Forge is for.
- **`ForgeConfig.PollInterval()` guards its own zero.** A zero interval panics `time.NewTicker`. `config.Load` defaults it, and the server defaults it again, so the boundary that actually ticks cannot be wrong whatever constructed the value.

### What the tests caught

- **A test that held its stream open by accident.** Story 5's `TestShutdown_DoesNotWaitForOpenEventStreams` read one byte from the stream and returned — which only blocked because the stream had nothing to say. The moment this story gave it a payload, the read returned at once, the body closed, and the stream was gone before the assertion ran. It now holds the connection deliberately.
- **A prefix collision, the same shape as Story 4's `checkbox__box`.** `data-testid="engine-status"` ends with the substring `id="engine-status"`, so counting the real `id` attribute needed anchoring on the leading space. A plain `Contains` would have been satisfied by the test id alone — which is not what Datastar patches against.
- **A browser test that measured the wrong thing.** "An unchanged status is not re-patched every tick" originally counted resource entries, which says nothing about patch frames. It now opens a second raw subscription from the page and counts `datastar-patch-elements` frames directly. Verified: with patch suppression removed it reports 4 frames over 6s instead of 1.

### What the second review round caught

Two of my own tests were vacuous, both confirmed by mutation:

- **"The first patch must not wait a tick"** asserted `elapsed < 2s` against a 40ms test poll — it needed roughly fifty consecutive missed intervals to fire. Moving the tick-wait above the first render passed both the Go test and all eight browser tests. It now runs against a 30-second poll, so the tick *cannot* produce that frame.
- **Three browser tests named for first paint were reading the DOM**, which `data-init` has already patched by the time an auto-retrying matcher looks. With the shell rendering a zero `Status`, all eight still passed. They now assert on the navigation response body — the server-rendered HTML, before any script runs.

Also fixed: the SSE frame helper joined multi-line payloads without a separator and never checked `bufio.Scanner.Err()`; and the destructive specs, which rename the fixture database away, now run in their own Playwright project with a `dependencies` edge so they cannot race the read-only specs against the same server.

`dsn()` gained a table test over awkward paths (spaces, `#`, `%`, `&`, `+`, `;`). That test also surfaced a pre-existing engine limitation worth recording: `storage.NewSQLiteStore` passes a bare path to `sql.Open`, and modernc truncates it at the first `?`, so the engine cannot open a database under a directory containing one — even though Forge's escaped DSN handles it.

Every guard here was checked against a deliberate defect: pushing once and never updating (caught by the both-ways test, in Go and in the browser), and re-sending identical patches every tick (caught by the suppression tests, in both).
