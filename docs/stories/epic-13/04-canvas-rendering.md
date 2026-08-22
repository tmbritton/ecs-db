# Story 4: Statechart canvas — rendering

**Epic:** 13 — Forge: AGENTS mode  
**Status:** 🔲 Not started  
**Priority:** High — the centre of the mode

**Depends on:** Stories 1 and 3

## Context

The canvas draws the machine: a node per state, an edge per transition, the
initial state tagged, entry actions listed under the node name, and the whole
thing laid out at the coordinates Story 1 decided where to keep.

This story renders it and lets you select things. It does not let you move
anything. That split is deliberate: a chart that draws the wrong edges and a
chart you cannot drag fail in completely different ways, and only the second
needs a line of JavaScript. Getting the rendering right first means the story
that adds pointer handling is adding it to something already known to be
correct — and if the chart is wrong, that is found in `go test` rather than
through a browser.

Everything here is therefore server-rendered and patched down the page stream,
like every other panel in Forge. Selection is a URL, the same as choosing a
component in SCHEMA: it survives a reload, it can be linked, and the inspectors
in Stories 6 and 7 read it from the request rather than from client state.

## Acceptance Criteria

- [ ] A node per state, drawn from the machine, with its name and its entry
      actions
- [ ] The initial state carries the prototype's `◉` tag
- [ ] An edge per transition, labelled with its event; `after` transitions
      labelled with their duration, since that is what the key is
- [ ] A transition with a `cond` is visually distinguishable from one without —
      a guard is the difference between "this happens" and "this might"
- [ ] Compound states render their children nested, because the machine is a
      tree and a flat drawing of it is a different machine
- [ ] History nodes are drawn as history nodes, not as ordinary states
- [ ] Layout comes from wherever Story 1 put it; a machine with no layout yet is
      laid out by a deterministic fallback, so two renders of an unpositioned
      machine agree
- [ ] Selecting a node or an edge is a URL parameter, resolved server-side, and
      survives a reload
- [ ] Exactly one thing is selected at a time, and what is selected is readable
      from the DOM rather than only visible
- [ ] Two renders of one machine are byte-identical — the chart is on a 2-second
      stream with identical patches suppressed, so any map-iteration order
      leaking into the output both flickers the canvas and defeats the
      suppression
- [ ] A machine that does not validate still draws. This is the editor for
      fixing it
- [ ] A transition whose target does not exist draws as a dangling edge rather
      than being dropped — a transition you cannot see is one you cannot fix
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/13-canvas-rendering.spec.js`.

- [ ] The fixture's machine draws its two states and the transitions between
      them, and the node count matches the machine
- [ ] The initial state is marked, and it is the one the file names
- [ ] Clicking a node selects it, the URL changes, and a reload keeps it selected
- [ ] Clicking an edge selects the edge, not the node under it
- [ ] Selecting one thing deselects the other
- [ ] An edit made elsewhere redraws the canvas on the stream, without a reload
- [ ] A machine with a transition to a state that does not exist still renders,
      and the dangling edge is visible
- [ ] Nothing on the canvas moves between two renders of an unchanged machine —
      count `datastar-patch-elements` frames and assert the count stops growing,
      the way `06-engine-status.spec.js` does

## Notes

- **Draw the chart from the machine, not from the layout.** Layout says where a
  node sits; it must never be able to say *whether* a node exists. A state
  missing from the layout file needs a position, not an omission — otherwise a
  stale layout silently hides part of someone's machine.
- The reverse too: a layout entry for a state that has been deleted is ignored,
  not drawn. Neither direction may be an error, because both are ordinary
  results of editing the file in another tool.
- SVG or positioned HTML is an open choice. SVG gives edge routing and markers
  for free; HTML gives the design system's borders, fonts and hover states for
  free. Whichever, the nodes must be real elements with test ids — the suite
  selects by test id, and a canvas painted into a `<canvas>` element is opaque
  to every assertion in this file.
- Edge routing is the part most likely to eat time. Straight lines with a
  midpoint label are acceptable for this story; the design's curves are worth
  nothing if the wrong states are connected.
- Do not add zoom or pan here. They are pointer features and belong with the
  pointer story, and adding them now means adding them twice.
