# Epic 13 Story 5: Statechart canvas — direct manipulation — Implementation Plan

**Goal:** drag a state to move it, drag a port to connect two, double-click to
add, right-click for the rest. The first hand-written client JS in Forge, and as
little of it as the job actually needs.

---

## Verified before planning

| Claim | Verified |
|---|---|
| Datastar binds a custom event and `evt.detail` survives | **True, probed in a browser before designing around it.** `data-on:canvasdrop` and `data-on:canvas-drop` both fire from a dispatched `CustomEvent`, and `evt.detail` arrives whole — strings and floats. So JS can own pointer state and hand the result to Datastar without doing the request itself. |
| `evt` is in scope for `dblclick` and `contextmenu` too | True — it is in scope in every Datastar expression, so `evt.offsetX`, `evt.preventDefault()` and `evt.button` need no JS at all. |
| `Session.Edit` clones, applies, and swaps under one lock | True — `machines.go:242`. **But "use `Edit`" is not sufficient on its own, which the first version of this got wrong:** a mutation that *reads* through one call and *writes* through another is a read-modify-write across two acquisitions, and eight concurrent drags lost five. The move and the add both read first. They do the reading inside the edit callback now. |
| A machine edit answers 204 and the page stream redraws | True — `handleMachineEdit` writes `StatusNoContent`; the 2-second stream carries the new markup. So "redrawn from the server" means within one tick, as it does for every other edit in Forge. |
| `ContextMenu` exists as a primitive and is wired nowhere | True — `components/menu.templ`, rendered only on `/dev/components`. Story 5 is its first real caller. |
| Forge's own JS has somewhere to live | `static/js/vendor/` holds the Datastar bundle; `web.Static` embeds the tree and `TestStatic_RequiredAssetsArePresent` guards it. Ours goes in `static/js/`, not `vendor/`, which is for third-party code. |
| A state name may contain a dot, and that breaks the chart's node identity | True, and Story 4 recorded it rather than fixing it. **This is the story that creates and renames states, so this is where it gets refused.** |

---

## Design

### Where the boundary sits

The JS owns pointer state and nothing else. It never fetches, never renders,
never knows what a machine is. On pointer-up it dispatches one `CustomEvent`
carrying the *result*, and a Datastar expression on the canvas turns that into a
request — the same `@post` every other control in Forge uses.

That split is what the probe was for. Without it the alternative is JS calling
`fetch` and then either ignoring the SSE patches in the response or
reimplementing the part of Datastar that applies them.

Three of the five interactions need no JS at all:

| Interaction | How |
|---|---|
| Drag a node | JS: pointer capture, live `style.left/top`, one event on drop |
| Drag a port to a node | JS: same, with the drop target read from `elementFromPoint` |
| Double-click empty space | Datastar: `data-on:dblclick` with `evt.offsetX/offsetY` |
| Right-click anything | Datastar: `data-on:contextmenu` with `evt.preventDefault()` |
| Every menu item | Datastar: the existing `ContextMenu` primitive |

**Delegated from `document`, bound once**, with pointer capture taken on the
first movement rather than on pointer-down — capturing straight away retargets
the pointer events and the click that selects a state never fires. The mode content is replaced by the
page stream every time anything changes, so a listener attached to a canvas
element dies with it — and a module that re-attached on each patch is how you
get two handlers and two requests per drag. One `pointerdown` listener on the
document, for the life of the page.

### The JS posts a delta, not a position

A node's rendered position is its recorded position plus up to three offsets:
the chart's `OffsetX/OffsetY` (Story 4's normalisation), and for a nested node
its parent's inner margin and title height. The client knows none of that and
should not learn it.

So the drag posts `dx`/`dy` — pure pointer arithmetic — and the server adds it
to the coordinate the file records. To make that possible the chart gains
`Node.RecordedX/RecordedY`: the value that, written into `meta`, reproduces
where the node is now. It is captured in `buildLevel` at the moment a position
is assigned, before either offset is applied, so a node laid out by the fallback
grid has one too and dragging it writes a position for the first time.

### Mutations

All in `internal/forge/machines/states.go`, all through `Session.Edit`, none of
them touching a file — the canvas cannot write and cannot call `EmitMachine`,
which is what the AC asks and what `Edit` enforces by construction.

```go
func (s *Session) MoveState(path, statePath string, x, y float64) error
func (s *Session) AddState(path string, x, y float64) (string, error)
func (s *Session) RenameState(path, statePath, to string) error
func (s *Session) DeleteState(path, statePath string) error
func (s *Session) SetInitial(path, statePath string) error
func (s *Session) AddTransition(path, from, to string) (string, error)
func (s *Session) DeleteTransition(path, from string, kind, event string, index int) error
func (s *Session) TransitionsTargeting(path, statePath string) []string
```

**A transition is addressed by its parts, not by the chart's edge id.** The id
is `from|kind|event|index` and both a state name and an event name may contain a
bar, so parsing it back is ambiguous. The DOM keeps the id for selection; the
endpoint takes four parameters.

**Writing layout merges into `meta`.** Story 1 settled that canvas coordinates
live under a `forge` key inside each state's own `meta`, and that Forge must
merge rather than replace, because a user may have their own keys there. The
merge preserves key order with `jsonorder`, like every other order-preserving
site. A `meta` that is not an object cannot be merged into without destroying
it, so a move on such a state is **refused with a message** rather than
silently overwriting someone's data.

**A state name may not contain a dot.** The chart keys nodes on a dotted path
and XState uses the dot as its own path separator, so `a.b` is ambiguous to the
engine's resolver too. Story 4 pinned what happens; this story stops Forge
authoring it.

### The context menu

Server-rendered, positioned where the click was, held in the same
per-server place the edit-problem banner already lives. That is a real
limitation — like the banner, it is not per-page — and it is recorded rather
than dressed up.

Opened by `contextmenu`, which browsers also fire for the keyboard Menu key on
the focused element, so the menu is reachable without a pointer. That matters:
rename, set-initial and delete live in it until Story 6 gives them an inspector.

### Deleting a state that others target

`TransitionsTargeting` answers which transitions would be left dangling, and the
confirmation names them — the existing `confirmAction` helper, the same shape as
the delete-machine warning. Dangling is not fatal, since Story 4 draws a dangling
edge rather than dropping it; the point is to say so before, not after.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Edit | `internal/forge/chart/chart.go` | `RecordedX/RecordedY` |
| Create | `internal/forge/machines/states.go` | The seven mutations and the `meta` merge |
| Create | `internal/forge/machines/states_test.go` | |
| Create | `internal/forge/server/canvasedit.go` | `/forge/agents/state`, `/forge/agents/transition`, `/forge/agents/menu` |
| Create | `internal/forge/server/canvasedit_test.go` | |
| Create | `internal/forge/web/static/js/canvas.js` | Pointer state, and a pure function under it |
| Edit | `internal/forge/templates/layout.templ` | Load it |
| Edit | `internal/forge/templates/modes/canvas.templ` | Ports, drag hooks, dblclick, contextmenu, the menu |
| Edit | `internal/forge/templates/modes/canvas.go` | The action expressions |
| Edit | `internal/forge/templates/modes/data.go` | The open menu |
| Edit | `internal/forge/web/static/css/forge.css` | Ports, dragging, menu placement |
| Create | `e2e/specs/13-canvas-editing.spec.js` | |

---

## Tests, and the mutation that has to kill each one

| Guard | Mutation it must fail against |
|---|---|
| A move writes the recorded position, not the rendered one | Write the posted delta as an absolute position |
| A move merges into `meta` | Replace `meta` wholesale |
| A move on a non-object `meta` is refused | Overwrite it |
| A new state gets a name nothing has | Always name it `state` |
| A state name may not contain a dot | Accept it |
| Adding a transition names an event the machine does not use | Always `EVENT` |
| Deleting a transition deletes the one named | Delete the first on that event |
| Deleting the last transition on an event removes the event key | Leave an empty array |
| Set-initial moves the machine's initial | Set the state's own |
| Set-initial on a nested state moves its *parent's* initial | Set the machine's |
| Delete names the transitions that would dangle | Report a count |
| Every mutation goes through `Edit` | Call `saveOne` |
| The JS binds once | Attach on each patch |
| A drop on empty space posts nothing | Post with an empty target |

---

## Playwright

`e2e/specs/13-canvas-editing.spec.js`, with the accessibility block.

The story names the trap: "a drag handler that never binds renders perfectly and
does nothing". So every assertion is on the **file on disk** or on a counted
request, never on a CSS transform — a transform is what the JS did, not what the
machine is.
