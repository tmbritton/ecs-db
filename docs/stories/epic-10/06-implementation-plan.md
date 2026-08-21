# Epic 10 Story 6: Engine-status readout — Implementation Plan

**Goal:** the menu bar tells the truth about whether a save will reach a running engine, and updates itself without a reload.

**Architecture:** `internal/forge/status` is a pure domain package — it takes paths, opens a read-only connection, and returns a `Status` value. It imports no HTTP and no templ, so its whole state space is unit-testable against temp files. The server polls it on a ticker inside the page-level SSE stream and pushes rendered HTML as a patch, only when the value changes.

**Depends on:** Story 5.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/status/status.go` | `State`, `Status`, `Check` |
| Create | `internal/forge/status/status_test.go` | All three states + read-only assertion |
| Create | `internal/forge/templates/components/enginestatus.templ` | The readout |
| Modify | `internal/forge/templates/shell.templ` | Render initial status; open the SSE stream |
| Modify | `internal/forge/server/server.go` | Fill in `handleModeEvents` (stubbed in Story 5) |
| Modify | `internal/forge/server/server_test.go` | Stream test |
| Modify | `internal/config/config.go` | `Forge.PollSeconds`, default 2 |

---

## Task 1: The checker

### `internal/forge/status/status.go`

```go
package status

type Config struct {
	DBPath        string
	SchemaVersion int    // from the already-loaded schema.json
	ModName       string
}

// Check reports whether the game database is present and compatible. It never
// creates or migrates anything: the connection is read-only because the
// interpreter is the sole writer of world state.
func Check(cfg Config) Status {
	s := Status{SchemaVersion: cfg.SchemaVersion, ModName: cfg.ModName}

	if _, err := os.Stat(cfg.DBPath); err != nil {
		return s // StateOffline
	}

	db, err := sql.Open("sqlite", "file:"+cfg.DBPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return s
	}
	defer db.Close()

	v, err := storage.ReadSchemaVersion(db)
	if err != nil {
		return s
	}
	s.DBVersion = v

	if v == cfg.SchemaVersion {
		s.State = StateConnected
	} else {
		s.State = StateMismatch
	}
	return s
}
```

Two details worth being deliberate about. `sql.Open` is lazy, so `ReadSchemaVersion` is what actually surfaces a bad file — treat its error as offline rather than propagating it, because a half-written database during game startup is a transient condition, not a Forge failure. And the `mode=ro` DSN is the whole safety story: `storage.NewSQLiteStore` would `MkdirAll` the parent and then bootstrap or migrate, which is exactly what an authoring tool must never do to a running game's database.

### `internal/forge/status/status_test.go`

Table-driven over fixtures built in `t.TempDir()`:

| name | fixture | want |
|---|---|---|
| `offline_no_file` | path that does not exist | `StateOffline`, `DBVersion == 0` |
| `offline_not_a_database` | file of random bytes | `StateOffline` |
| `connected_versions_match` | bootstrapped DB at v3, schema v3 | `StateConnected`, `DBVersion == 3` |
| `mismatch_db_behind` | bootstrapped DB at v3, schema v4 | `StateMismatch` |

Build the real fixtures with `storage.NewSQLiteStore` in the test (not in `Check`), then close it before checking — that also proves `Check` works against a database it did not create.

Add the safety test explicitly:

```go
func TestCheck_ConnectionIsReadOnly(t *testing.T) {
	// Open the same DSN Check uses and confirm a write is rejected,
	// so the one-writer-per-table contract can't regress silently.
}
```

---

## Task 2: The readout component

`EngineStatusProps{Status status.Status}`. One `switch` over `State`:

```templ
templ EngineStatus(props EngineStatusProps) {
	<div id="engine-status" class="engine-status mono">
		switch props.Status.State {
			case status.StateConnected:
				<span class="engine-status__dot engine-status__dot--ok">■</span>
				{ fmt.Sprintf("schema.json v%d · mods/%s · hot-reload live",
					props.Status.SchemaVersion, props.Status.ModName) }
			case status.StateMismatch:
				<span class="engine-status__dot engine-status__dot--bad">■</span>
				{ fmt.Sprintf("schema v%d ≠ db v%d · engine would refuse this database",
					props.Status.SchemaVersion, props.Status.DBVersion) }
			default:
				<span class="engine-status__dot engine-status__dot--bad">■</span>
				{ "watcher offline · edits queue until game restarts" }
		}
	</div>
}
```

CSS: `--green` for `--ok`, `--red` for `--bad`, 11px mono, `margin-left: auto` in the menu bar.

The `id` lives on the component itself so Datastar's default `outer` patch mode morphs it in place.

---

## Task 3: Pushing it down the page stream

Story 5 stubbed `GET /forge/{mode}/events` as the single subscription every Forge page opens. This story gives it its first job. There is no new route and no second stream — a Datastar page subscribes once, and everything that changes on it arrives as a patch on that connection.

```go
func (s *Server) handleModeEvents(w http.ResponseWriter, r *http.Request) {
	m, ok := mode.Lookup(r.PathValue("mode"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	sse := datastar.NewSSE(w, r)

	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	var lastStatus string
	for {
		// Engine status is shown on every mode, so it is pushed regardless of m.
		// Epic 12 and Epic 18 add mode-specific pushes alongside it here.
		if cur, err := s.renderStatus(); err != nil {
			slog.Error("render engine status", "err", err)
		} else if cur != lastStatus {
			if err := sse.PatchElements(cur); err != nil {
				return // client gone
			}
			lastStatus = cur
		}

		select {
		case <-sse.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
```

Three things this shape gets right, all of which matter more once Epic 18 shares the stream:

- **The first iteration runs before any tick,** so a reconnecting client sees current state immediately rather than after a poll interval.
- **`lastStatus` suppresses identical patches.** Without it the endpoint emits forever, and the browser's EventStream log — the main tool for debugging Epic 18's traffic — becomes unreadable.
- **`sse.Context()` is the disconnect signal.** The SDK wires it to the request context, so the ticker and goroutine end when the tab closes.

`renderStatus` calls `status.Check` and renders `EngineStatus` to a buffer, returning the HTML string. `PatchElements` defaults to `outer` mode and matches on the element's own `id`, so `#engine-status` is morphed in place with no selector argument needed.

The shell already renders the status server-side on page load (Task 2), so the bar is never blank on first paint; the stream only takes over from there.

## Task 4: Config

Add `PollSeconds int \`toml:"pollSeconds"\`` to `ForgeConfig`, default `2`, surfaced to the server as a `time.Duration`. Extend the existing config defaults test to cover it, including the omitted-key case — a zero interval would spin `time.NewTicker` into a panic.

---

## Task 5: Server test

Against `httptest.NewServer`, request `/forge/map/events` with a context cancelled after ~3 poll intervals, and parse the SSE frames:

- the first `datastar-patch-elements` event arrives without waiting a full interval
- each event's payload is HTML containing `id="engine-status"` — patches, not JSON
- with a matching database present, the payload contains `hot-reload live`
- with no database, it contains `watcher offline`
- cancelling the request ends the handler — assert with `goleak` or by checking the handler returns within a bounded time

Deleting the database mid-stream and asserting the next event flips to offline is the test that actually pins the behaviour the story promises; it is worth the extra few lines.

---

## Task 6: Mark story complete

Tick `docs/stories/epic-10/06-engine-status.md`, add `## As Implemented`, tick **Engine-status readout** in `docs/plan.md`. Epic 10 is then complete — record that in the Epic 10 section.

---

## Verification

```bash
make generate && go test ./internal/forge/status/... -v
go test ./...
make build && make build-headless
```

Two terminals:

```bash
# 1
./bin/ecs-db-headless forge
# 2
./bin/ecs-db run
```

With Forge open in a browser: the readout goes green within ~2s of the game starting, and red within ~2s of stopping it. In devtools, the EventStream tab on `/forge/map/events` shows one `datastar-patch-elements` frame per *change* — not one per tick. Then force the mismatch — bump `schemaVersion` in `schema.json` without letting the engine migrate — and confirm the third state renders with both version numbers.

Finally, confirm Forge left the project directory alone:

```bash
git status --porcelain   # no new -wal/-shm files, no touched ecs.db
```

Before committing, launch a fresh-context code-review subagent over the staged diff, per `AGENTS.md`.
