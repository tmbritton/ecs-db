# Epic 12 Story 1: Editing session — Implementation Plan

**Goal:** one editable `schema.json` per project, shared by both modes, saved through Epic 11's machinery.

**Architecture:** the session is server-side state owned by the server value and guarded by a mutex. Both modes render from it; both mutate it through actions. Nothing about it lives in the page, because mode switching is a full page load.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/session/session.go` | `Session` — the editable schema plus its lock |
| Create | `internal/forge/session/session_test.go` | Lifecycle and concurrency |
| Modify | `internal/forge/server/server.go` | Hold the session; `POST /forge/schema/{save,discard}` |
| Modify | `internal/forge/templates/shell.templ` | Render the save footer from real state |
| Modify | `cmd/ecs-db/forge.go` | Construct the session from the opened project |

---

## Task 1: The session

```go
// Session owns the one editable schema.json a project has. editable.File is
// documented as not safe for concurrent use; this is the thing that owns it,
// so this is where the lock goes.
type Session struct {
	mu     sync.Mutex
	schema *editable.File[schema.DatabaseSchema]
}

// Edit runs fn against the working value under the lock. Returning the value
// rather than exposing the File keeps every mutation on this path — a caller
// that could reach Current directly would be one that could forget the lock.
func (s *Session) Edit(fn func(*schema.DatabaseSchema) error) error

// Read runs fn against a value nothing else can mutate meanwhile.
func (s *Session) Read(fn func(schema.DatabaseSchema))

func (s *Session) Dirty() (bool, error)
func (s *Session) Save() error
func (s *Session) Discard() error
func (s *Session) Reload() error          // conflict: take theirs
func (s *Session) SaveOverwriting() error // conflict: keep mine
```

`Edit` taking a callback rather than handing out a pointer is the load-bearing
decision. `editable.File.Current` is replaced wholesale by `Discard` and
`Reload`, so a cached pointer is a stale pointer — Epic 11 Story 4's review
found exactly that hazard.

## Task 2: Actions

```go
mux.HandleFunc("POST /forge/schema/save", s.handleSchemaSave)
mux.HandleFunc("POST /forge/schema/discard", s.handleSchemaDiscard)
```

Each mutates the session, records a `savereport`, and returns a Datastar patch
stream re-rendering the footer. They are actions, not pages: there is no
representation of `/forge/schema/save` to GET.

The response can be a 204 with the change arriving on the page-level SSE stream
instead. Prefer that — it keeps one push path, and Epic 11 Story 6 already
plumbed reports through it. Decide once and comment why.

## Task 3: The footer

`SaveFooter` exists from Epic 10 Story 4 with `Dirty`, `File`, `SaveAction` and
`DiscardAction`. Wire it: dirty from the session, file from the project, actions
to the two endpoints. Both buttons disabled when clean — the primitive already
supports it and nothing has used it yet.

## Task 4: Conflicts

A `*editable.ConflictError` from `Save` renders a modal offering both
resolutions. `ModalShell` exists; the two actions map to `Reload` and
`SaveOverwriting`. Neither may be the default, and neither may happen without a
click.

---

## Verification

Go tests over the session's state machine, mirroring Epic 11 Story 4's table —
including the revert-to-clean case, which is what proves the dirty state is a
comparison and not a flag.

Then the e2e spec, whose last step is the one that matters: edit in SCHEMA,
switch to ENTS, switch back, and the edit is still there. That is what makes it
one session rather than two.

Before committing: fresh-context code review, `make test`, `make e2e`, linter.
