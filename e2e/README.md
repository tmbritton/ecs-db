# Forge end-to-end suite

```bash
make e2e                    # everything
./scripts/e2e.sh 05-app     # one spec, by filename fragment
./scripts/e2e.sh --ui       # interactive runner
make e2e-seed               # rebuild the fixture database on its own
```

The suite builds the headless binary, reseeds the fixture database, starts Forge
against the fixture project, and drives it in Chromium. `mise install` covers
the Node and Playwright side; the build step needs the same Go toolchain and
`make` that the rest of the repo does. The Chromium bundle is installed on first
run.

## Why it exists

Forge's characteristic failure is **silent**. A Datastar attribute whose plugin
name does not resolve is skipped with no error, no console warning and no visual
difference — the control simply never does anything. That has happened twice:

- Story 4 shipped every primitive with `data-on-click`, which parses as a plugin
  named `on-click`. Every handler in the package was dead markup.
- Story 5's plan called for `data-on-load`, which does not exist at all. Every
  mode page would have rendered perfectly and never opened its SSE stream.

Neither is visible to `go test`, `go vet`, `golangci-lint`, or a screenshot. A
screenshot cannot see a handler that never fires. Only a browser can.

So the rule for this suite is: **assert on effects, not on markup.** A request on
the wire, a class that changed, a URL that moved. Go tests already check what is
rendered; if a spec here only checks that an attribute is present, it is
testing the wrong layer and will pass against a broken page.

## Selectors

Select by `data-testid`. It survives the copy and markup churn the modes will go
through, and it says plainly that an element is depended on.

```js
const { byTestId } = require("../fixtures");
await byTestId(page, "rail-schema").click();
```

Test IDs live on the shell and on the `/dev/tokens` gallery cards, not inside the
primitives — the props structs stay free of test concerns. A mode that needs to
address a specific instance wraps it the same way the gallery does.

`internal/forge/templates/testid_test.go` pins the shell's test IDs from the Go
side, so a rename fails in seconds rather than as a browser timeout minutes
later. Add to it when you add a test ID.

One consequence worth naming: test IDs do **not** notice accessibility
regressions the way role- and name-based selectors do by accident. Each spec
therefore carries an explicit `accessibility` block. Those assertions are not
decoration — every one of them corresponds to a defect found in review: an icon
button with no accessible name, a row that was a click-handling div, rail links
announced as `SPRT` and `ENTS`.

## The fixture project

`e2e/fixtures/project/` is a complete, self-contained engine project — schema,
behaviors, `game.toml` — and `e2e/fixtures/seed` bootstraps its database. Forge
reads real engine files, so the suite gives it real ones; mocking the transport
would stand in for exactly the thing under test.

Every value in it is deliberately **distinctive**: `schemaVersion` 7 rather than
the repo's 3, components and entity types named `E2EProbe`, `TestDummy`,
`TestGoblin`, a mod called `e2e-core`. A test asserting on those is also
asserting that Forge read the fixture. If a path-resolution bug made it fall
back to the repo's own `schema.json`, the assertions fail loudly instead of
passing against the wrong data.

The database is generated, never committed — it is a binary artefact, `*.db` is
gitignored, and regenerating it is how the fixture stays in step with the
engine's own bootstrap code. The seed is deterministic: three `TestDummy`
entities and one `TestGoblin`, counts chosen so they can never be confused with
an off-by-one.

Forge runs on port **7788**, not the default 7777, so a suite run never binds
over an instance you are using.

## Writing a spec for a new story

Every story from Epic 10 Story 5 onward carries a **Playwright steps** section in
its story file, written before the code. It lists what only a browser can
confirm. Then:

1. Add the test IDs the spec will select on, and extend `testid_test.go`.
2. Write `e2e/specs/NN-<story>.spec.js`.
3. **Break the feature on purpose and watch the spec fail.** A spec that passes
   against a deliberately broken implementation is worse than no spec: it is a
   false assurance in exactly the place assurances are hardest to come by. Four
   examples, all from this suite's own first day:

   - A test named *"the stream is a live text/event-stream, not a one-shot
     response"* passed against a server that hung up instantly — the SDK
     flushes the headers before the handler body runs, so status and
     content-type prove nothing.
   - *"Exactly one SSE subscription"* passed against a page opening two.
     `expect.poll` stops at the first success, so a stream opened later is
     never seen. Use `expectSettled` from `fixtures.js`: reach the value, let
     the page settle, then assert it still holds.
   - Every layout assertion was upper-bound-only, so a shell collapsed to 40px
     on a 900px viewport passed the whole spec. Bound measurements on *both*
     sides.
   - A stream-counting check silently found nothing, because Datastar appends
     `?datastar={}` to the URL and the matcher used `endsWith("/events")`.

   "Found nothing" and "nothing to find" look identical from the outside.
4. Run `make e2e` before the code review.

## Layout

| Path | What it holds |
|---|---|
| `playwright.config.js` | runner config, and the build → seed → serve chain |
| `fixtures.js` | the `page` override that watches the browser's error channels, plus `byTestId`, `watchEventStreams`, `MODES` |
| `specs/` | one spec per story |
| `fixtures/project/` | the engine project Forge is pointed at |
| `fixtures/seed/` | builds the fixture database |

`fixtures.js` fails any test whose page reported a console error, an uncaught
exception, a failed request, or an HTTP status ≥ 400 — for its whole lifetime,
not just at an assertion. A test that needs a deliberate failure wraps that one
navigation in `page.expectPageErrors(...)`.
