# Epic 13 Story 4: Statechart canvas — rendering — Implementation Plan

**Goal:** draw the machine. A node per state, an edge per transition, nested
compound states, and a selection that lives in the URL. Server-rendered, no
JavaScript, and byte-identical between two renders of an unchanged machine.

---

## Verified before planning

Every claim below was read out of the code rather than trusted, and three of
them changed the design.

| Claim | Verified |
|---|---|
| Layout goes in each state's own `meta`, under a `forge` key | Story 1's decision, `01-round-trip-fidelity.md:242`. `rawStateNode` has no `meta` field, so it lands in `StateNode.Extra` and survives a save. |
| `Extra` keys keep their authored case | True — `extraFields` compares `strings.ToLower(key)` against the known set but stores `key` verbatim. So the lookup is `Extra["meta"]`, exactly, and a file spelled `"Meta"` is a different extra. |
| `StateNode.ID` is a usable node identity | **False, and it decides the model.** Unless a state declares an `id` of its own, `parseStateNode` passes `machineID` down unchanged at every depth, so `ID = machineID + "." + name` for a leaf three levels deep. Two compound states with a same-named child collide on one ID. |
| The engine resolves a transition target deterministically | **False, and it decides the story.** `findState` falls back to `for _, node := range states { findState(node.Children, target) }` — a map range. Two subtrees holding a state of the same name resolve to a different node run to run, in the interpreter, in the running game. |
| `ValidateMachine` and the interpreter agree on what a valid target is | **They did not, and the disagreement was fatal.** The validator checked membership of `collectStateIDs`, which holds bare names and `StateNode.ID`s and therefore **no dotted path at all**; the interpreter traverses dotted paths first. So `"target": "combat.attacking"` — XState v4's own notation — was resolvable at runtime and refused at load, which meant a machine with a transition into a nested state could not run *and* could not be opened in Forge to be fixed. The validator now uses `FindState` too. |
| A machine that fails validation still parses | True — `Session.Working` parses; `Inspect` validates separately. The canvas takes the definition, never the inspection. |
| The mode content is on a 2-second stream with identical patches suppressed | True — Story 3's problem-list fix, and `06-engine-status.spec.js` counts the frames. |

### The two engine findings, and what this story does about them

**Node identity.** `StateNode.ID` cannot name a node, so the chart's identity is
the **dotted path from the machine root** — `idle`, `combat.attacking`. Stable
under everything but a rename, readable in a URL, and the same notation XState
targets are written in. Unique as long as no state name contains a dot, which
XState already requires since the dot is its own path separator; nothing rejects
such a name, and a test pins what happens rather than the comment claiming it
cannot.

**Target resolution.** Writing a second resolver in the chart package is the one
thing this story must not do: the canvas would then be able to call an edge
dangling that the engine resolves, or draw it to a node the engine would not
pick, and the story exists to stop the chart being wrong about the graph. So the
engine's resolver is **exported and made deterministic**:

```go
// FindState returns the state a transition target names and its dotted path
// from the machine root, or (nil, "") if nothing matches.
func FindState(def *MachineDefinition, target string) (*StateNode, string)
```

`resolveTarget` becomes a wrapper over it. The map ranges in `findState` are
replaced by authored order (`StateOrder`, then sorted remainder), which is what
`stateNames` already does in the modes package. That is a behaviour change to
the interpreter, and it is a fix: an ambiguous target currently makes the
*running game* behave differently between two launches of the same file.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Edit | `internal/agent/interpreter.go` | `FindState` exported; deterministic descent |
| Edit | `internal/agent/interpreter_test.go` | The ambiguity that used to flip |
| Create | `internal/forge/chart/chart.go` | `Chart`, `Node`, `Edge`, `Build` |
| Create | `internal/forge/chart/layout.go` | `meta.forge` reader; the deterministic fallback |
| Create | `internal/forge/chart/chart_test.go` | |
| Create | `internal/forge/chart/layout_test.go` | |
| Edit | `internal/forge/templates/modes/data.go` | `Chart` |
| Edit | `internal/forge/templates/modes/agents.templ` | `statechart` replaces the states chips |
| Edit | `internal/forge/templates/modes/agents.go` | Render helpers; `selectHref` |
| Edit | `internal/forge/templates/modes/agents_test.go` | |
| Edit | `internal/forge/server/server.go` | Build the chart; carry `?sel=` on the stream |
| Edit | `internal/forge/server/machineedit.go` | Resolve `?sel=` against the chart |
| Edit | `internal/forge/web/static/css/forge.css` | `.chart*` |
| Edit | `e2e/fixtures/project/behaviors/e2e-wander.json` | Layout in `meta`, a guard, an `after` |
| Create | `e2e/specs/13-canvas-rendering.spec.js` | |

---

## Design

### `chart.Chart`

```go
type Chart struct {
    Nodes  []Node // top-level; each carries its own Children
    Edges  []Edge
    W, H   float64
    Selected string // what "state:…"/"edge:…" actually resolved to, "" if nothing
}

type Node struct {
    Path     string   // "combat.attacking" — the identity
    Name     string   // "attacking"
    Kind     Kind     // atomic | compound | parallel | final | history
    History  string   // "shallow" | "deep", history nodes only
    Initial  bool     // its parent (or the machine) names it
    Entry    []string // entry action labels
    X, Y     float64  // relative to the parent's content box
    W, H     float64
    Children []Node
    Selected bool
}

type Edge struct {
    ID       string // "combat.attacking|on|LOST|0"
    From     string // node path
    To       string // node path; "" when dangling or internal
    Event    string // "LOST", or the raw duration for an after
    Guard    string // "" when unconditional
    Kind     EdgeKind // on | after
    Target   string // the raw target text, so a dangling edge can name it
    Dangling bool
    Internal bool   // target-less: runs actions, changes no state
    X1,Y1,X2,Y2 float64 // absolute, clipped to the two boxes
    Loop     bool
    Selected bool
}
```

**Two coordinate systems, named as such.** Nodes carry positions relative to
their parent, because that is what the DOM needs — a child inside a
`position:relative` compound box is placed by CSS, so moving the parent in
Story 5 moves its children with it and no arithmetic is repeated. Edges carry
absolute coordinates, because an SVG overlay spans the whole canvas. The
absolute rectangle is computed during the same walk that builds the tree, so the
two cannot disagree.

### Drawing: HTML nodes, SVG edges

The nodes are `<a>` elements with the design system's borders and fonts, and
real test ids — a `<canvas>` would be opaque to every assertion in the spec, and
so would an SVG `<text>` node dressed up to look like the prototype's boxes.
Compound children are nested **in the DOM**, not merely drawn inside the parent
box: the machine is a tree, containment is the fact being drawn, and a flat DOM
would make it visible without making it readable.

Edges are `<line>`/`<path>` in one absolutely-positioned SVG layer beneath the
nodes, with arrow markers. The clickable target for an edge is its **label**, a
positioned HTML chip above the node layer — an SVG stroke two pixels wide is not
a pointer target, and the label is where the eye already is.

Straight lines, clipped to each node's rectangle, per the story's explicit
permission. A self-transition draws as a loop over its own node's top edge.

### Layout

```go
func Position(node *agent.StateNode) (x, y float64, ok bool)
```

reads `Extra["meta"]` → `{"forge": {"x": 40, "y": 60}}`. Anything else — no
`meta`, no `forge` key, a non-object, a string where a number belongs — is
`ok == false`, never an error. The file is hand-editable and shared with Stately
Studio, and a chart that refuses to draw because someone's `meta` holds a note
would be worse than one that places the node itself.

**Fallback slots are keyed on authored index, not on "the next free slot".**
Three per row, 200×120 cells. This is the difference between a layout that
settles and one that never does: if unpositioned states filled the gaps left by
positioned ones, dragging one node in Story 5 would move every node after it.

Read-only here. Story 5 writes.

The two directions the story names both fall out of walking the machine rather
than the layout: a state with no `meta` gets a slot, and a `meta.forge` on a
state that no longer exists is never consulted because nothing asks for it.

### Selection

One URL parameter, `?sel=`, carrying a prefixed value: `state:combat.attacking`
or `edge:idle|on|SPOTTED|0`. One parameter rather than two, so "exactly one
thing is selected" is structural rather than enforced — two parameters means a
URL that names both, and then a rule about which wins.

Resolved server-side against the chart that was just built. A `sel` naming
nothing is dropped silently: it is what a bookmark becomes after the state is
renamed, and a 404 for it would be absurd. Switching machines drops it, because
the machine list's links do not carry it.

`streamQuery` gains it, on the same terms as `?machine=` — only on AGENTS, and
only the resolved value, so the stream re-renders the page that is on screen.

### What the canvas replaces

`machineStates` — the chips readout captioned "the canvas arrives in Epic 13
Story 4". The header, rename controls and delete button stay where Story 2 put
them; the inspector rail is Stories 6 and 7.

---

## Tests, and the mutation that has to kill each one

| Guard | Mutation it must fail against |
|---|---|
| A node per state, including nested ones | Walk only `def.States` |
| Nesting is structural | Flatten children onto the root |
| Initial tag is the machine's, and each compound's | Tag the first node |
| Edge per `on` transition, per event, per index | Keep one transition per event |
| `after` edges labelled with the duration | Label them with the event name, which they have none of |
| A guarded edge is distinguishable | Drop `Guard` from the edge |
| Dangling target draws | Skip transitions that do not resolve |
| Target resolution is the engine's | Resolve by top-level name only — a nested target goes dangling |
| Unpositioned states get a slot, positioned ones keep theirs | Ignore `meta.forge` |
| A slot is keyed on authored index | Assign the next free slot |
| Fallback layout is deterministic | Range `def.States` |
| An invalid machine still draws | Build from the inspection instead of the definition |
| History nodes are their own kind | Fall through to atomic |
| Selection resolves server-side | Trust the parameter |
| Exactly one thing selected | Set both a node and an edge |
| Two renders are byte-identical | Any map range anywhere in the builder |

The last one is the whole story's insurance and is asserted twice: in Go, by
rendering the same machine fifty times and comparing; and in the browser, by
counting `datastar-patch-elements` frames the way `06-engine-status.spec.js`
does.

---

## Playwright

`e2e/specs/13-canvas-rendering.spec.js`, with the accessibility block
`AGENTS.md` requires.

The fixture machine grows what the canvas has to draw and nothing more: a
`meta.forge` position on one state and not the other, a guarded transition, an
`after`, and — in a test that writes it, not in the committed fixture — a
transition to a state that does not exist.
