# Story 0: Push, not poll

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** ✅ Complete  
**Priority:** Highest — every later story lands on this architecture

**Depends on:** Story 3

## Context

Forge feels sluggish on localhost against a Go + SQLite backend that answers in
single-digit milliseconds. Measured against a running server before writing a
line of this:

| | |
|---|---|
| Server render, MAP page (79 KB, 302 cells) | **4 ms** TTFB |
| Server render, other modes | 2–9 ms |
| One click in MAP (palette tile / zoom / layer eye) | **~92 ms** — full document teardown + SSE reconnect |
| SSE burst on every stream connect | **5 patches, 76.6 KB**, then silence |
| Every mutation route (24 of them) | returns `204`, change lands **on the next 2 s poll tick** |

The server is not the bottleneck. Three things sit on top of it.

**Edits wait for a poll.** Every `POST` handler writes and answers `204 No
Content`. Nothing pushes. The change reaches the browser when
`handleModeEvents`' 2-second ticker next fires — uniformly distributed, so
**~1 s average and 2 s worst case on every edit**. The comment at
`server.go:1010` states the intent correctly — "the changed footer and the save
report reach the page on the SSE stream it already holds… One push path, not
two" — and the design is right. The push is a poll wearing its clothes.

**Patches are the whole page.** The stream re-renders and patches all of
`<main>`: 75 KB on MAP, of which **70 KB is the canvas**. Everything else on the
page — map list, layers, palette, toolbar, status — is ~5 KB combined. A save,
an engine-status flip or a palette click re-morphs all 302 cells.

**Every navigation renders twice.** The stream's `last*` strings start empty, so
the first tick always patches all five regions. The browser parses 79 KB, then
immediately morphs `<main>` to byte-identical content from a 76 KB SSE burst.

MAP compounds this by not using Datastar at all: `map.templ` has five `href`s
and **zero** `data-on` handlers, so selecting a tile, changing zoom, toggling a
layer's eye and choosing the active layer are full document navigations that
also tear down and reopen the SSE stream. The slowest mode in the app is the one
that does not use the framework the app is built on.

This story is Story 0 rather than Story 10 because Story 4's paint routes should
be written against the pushed, region-patched architecture rather than converted
afterwards.

## Acceptance Criteria

- [x] An edit reaches the browser without waiting for a tick — provable by
      freezing the engine poller and asserting the DOM updates anyway
- [x] No stream sends anything while nothing is changing; an idle Forge is
      silent on the wire
- [x] A page that has just rendered gets **nothing** on stream connect, rather
      than a byte-identical copy of itself
- [x] Closing a tab removes its subscriber — 50 opened and closed tabs leave the
      bus empty, because a leaked subscriber degrades into a save that waits
      `PublishTimeout` per abandoned tab rather than into a visible break
- [x] A burst of events causes one re-render, not one per event
- [x] An interaction patches only the region it changed; the canvas is not
      touched unless the canvas changed
- [x] Region ids are present on every render of a mode, including the
      no-project stub — an id that appears and disappears is an id a patch
      cannot target
- [x] The database is opened once per poll interval for the whole server, not
      once per interval per open tab
- [x] MAP has no `<a href>` left except choosing which map to edit
- [x] Zoom, selected tile, layer visibility and active layer survive a
      re-render, because the server no longer renders them
- [x] What navigation remains does not read as a hard refresh
- [x] A fine-grained edit is **not** animated — a view transition has a
      duration, and spending it on a keystroke puts back the latency this story
      removes
- [x] `go test ./...` passes

## As Implemented

Five phases, each measured before and after.

**The numbers.** Edit-to-screen went from ~1000ms average to 22.8ms. A page that
has just rendered receives 0 bytes on connect where it used to receive 76,509. A
refused map edit sends 1,882 bytes where it used to re-send 75KB and re-morph
three hundred cells. MAP's four view controls and the statechart's selection
cost no page load at all, where each was ~92ms and a fresh SSE connection.

**Two places the plan was wrong, both found by looking rather than reasoning.**
It said to give `#mode-content`, `#map-canvas` and the save footer a
`view-transition-name`; naming the footer scaled MAP's into SCHEMA's and
ballooned "Discard" and "Save" to twice their size in the middle of every mode
switch. A screenshot taken mid-transition caught it. And it justified moving
MAP's view state to signals partly on payload, which the region split had
already made false — the real reason is that a stream's subscription is fixed
when the page loads, so anything the server renders from it is frozen there.

**Two costs view transitions add**, neither of which existed before. Real pointer
input does not reach the DOM while a transition animates, so for ~140ms after a
navigation the page looks ready and is not — which broke the statechart drag and
will meet Story 5's paint tools. And a skipped transition rejects promises
nobody observes; `viewtransition.js` catches them.

**One property deliberately given up.** The statechart selection is no longer in
the URL, so it is not bookmarkable and does not survive a reload. It was a link
because the inspector's contents depend on it and only the server can render
those. Keeping the URL in sync was considered and rejected: a click could sync
it, but a server-initiated move — renaming an event, deleting a transition —
could not, and a URL that is sometimes right is worse than one that never claims
to be.

**Still per-server, and now hit far more often:** `editProblem` and
`canvasMenu`. Every node click in one tab closes another tab's context menu and
clears its error banner. `pageStates` is the shape that fixes it.

## Notes

**The bus API is copied, not designed.** It comes from `pkg/eventbus` in
`tmbritton/fancykaraoke-go`, which is to be extracted into a shared library for
all of Tom's Go applications. Keeping it source-compatible is worth more than
trimming it to what Forge uses, because the trimmed version is the one that
cannot be swapped for the library later. `eventbus.go` copies over;
`global.go` does not, because `AGENTS.md` forbids package-level singletons and
Forge already takes its sessions through `server.Config`.

Three changes the original needs, all of which are worth making upstream — Forge
is the first consumer whose subscribers come and go:

- `Unsubscribe` does not exist. Forge subscribes once per SSE stream, which is
  once per page load. Without it Forge leaks a channel per page load, and once a
  dead subscriber's 100-slot buffer fills, `Publish` waits `PublishTimeout` on
  it — so N abandoned tabs put **5N seconds on every save**.
- `Publish` blocks for up to `PublishTimeout` per subscriber, and Forge calls it
  synchronously inside the HTTP handler for a mutation. A slow subscriber must
  not be able to stall someone's save. Forge wants a non-blocking send that
  drops on a full buffer: a stream that is behind does not need the event it
  missed, it needs to know something changed, which the next event also says.
- `Subscribe` wants to be variadic, so one channel can carry several event
  types and the stream loop does not grow a `select` case per type.

**One publish site.** `sameOriginOnly` already wraps all 24 mutation routes and
nothing else — every `POST` except `/dev/noop`. Publishing there keeps the
property the current design has and is worth keeping: the server cannot forget
to publish. The event type is derived from the route prefix rather than
annotated per handler, so it stays one call site and the types are still real.

**Targeting comes from the region split, not the payload.** Re-rendering a
region and comparing strings is what makes the server unable to forget an
update, and it costs 4 ms. Keep it; just stop running it when nothing has
changed, and make the regions small enough that the diff is worth having.
`Payload` stays nil — it exists for the shared-library shape.

**View state is not document state.** `?map=` says which file you are editing
and belongs in the URL. Zoom, selected tile, layer visibility and active layer
do not — and leaving them there is not merely slow, it is wrong under region
patching: `streamQuery` builds the subscription URL at render time and
**that URL cannot change afterwards** (`server.go:254`), so the stream would
re-render the old view and clobber the new one on the next event. Moving them to
signals takes them out of server-rendered HTML entirely. The server renders the
document; the client renders the view; a re-render has nothing to clobber. It
also collapses `streamQuery` to `?map=`, retiring the class of bug its own
comments record being bitten by three times.

**View transitions are scoped or they are worse than nothing.** An unscoped
`startViewTransition` snapshots the whole document — on MAP a 1920×1440 canvas
of 302 elements, captured twice. Scope every one to the region being patched,
keep the duration near 120–150 ms against a design system whose motion is terse,
and honour the `prefers-reduced-motion` block that `forge.css` already has. A
`view-transition-name` collision throws and drops silently to an instant swap,
so name regions and never cells.

**What this story does not do.** The element-per-cell canvas stays. 302 divs is
fine; 10,000 (2.2 MB) is not, but that ceiling is a separate decision best taken
with Story 5, when whether a stroke patches cells or repaints a `<canvas>` is on
the table. Splitting the canvas into its own region is the prerequisite either
way, and this story does that much.
