// The statechart canvas's pointer handling, and nothing else.
//
// This is the only hand-written client JS in Forge, and it is here for a reason
// that does not generalise: a node under the pointer has to move at pointer
// speed, and a server round trip per pixel is not a design anyone would defend.
//
// So it owns pointer state and stops there. It never fetches, never renders,
// and does not know what a machine is. On drop it dispatches one CustomEvent
// carrying the result, and a Datastar expression on the canvas turns that into
// the same @post every other control in Forge uses. Anything else would be
// either a second source of truth for the statechart or a reimplementation of
// the part of Datastar that applies patches.
//
// Bound once, from the document. The mode content is replaced by the page
// stream whenever anything changes, so a listener attached to a canvas element
// dies with it — and a module that re-attached on each patch is how you get two
// handlers and two requests per drag.

/**
 * What a completed drag means, given where it started and where it ended.
 *
 * Pure, and separated out so the arithmetic is testable without a browser:
 * Playwright is then left to prove the wiring rather than the sums. It knows
 * about pixels and element roles, and nothing about states or transitions.
 *
 * @param {{kind: string, path: string, startX: number, startY: number}} drag
 * @param {{x: number, y: number, overPath: string|null, inCanvas?: boolean}} drop
 * @returns {{action: string, [k: string]: unknown}|null} null when nothing should be posted
 */
export function dragResult(drag, drop) {
  if (!drag || !drag.kind) return null;

  if (drag.kind === "move") {
    // Released outside the canvas — over the machine list, the mode rail, the
    // footer. That is a drag abandoned, and the story says so: it leaves the
    // machine and the layout untouched, exactly as Escape does.
    if (drop.inCanvas === false) return null;
    const dx = drop.x - drag.startX;
    const dy = drop.y - drag.startY;
    // A drag that ended where it started is a click. Posting it would write the
    // position a node already had, mark the machine unsaved, and teach you that
    // selecting a state dirties the file.
    if (dx === 0 && dy === 0) return null;
    return { action: "move", path: drag.path, dx, dy };
  }

  if (drag.kind === "connect") {
    // Dropped on empty space, or back on the state it came from. Both are how
    // a connection is abandoned, and neither is a request.
    if (!drop.overPath || drop.overPath === drag.path) return null;
    return { action: "connect", from: drag.path, to: drop.overPath };
  }

  return null;
}

const NODE = "[data-path]";
const HEAD = ".chart-node__head";
const PORT = ".chart-node__port";
const CANVAS = ".chart";

let drag = null;

function chartOf(el) {
  return el.closest(CANVAS);
}

// pathAt is which state is under a point.
//
// elementFromPoint answers with whatever takes the pointer there, and on this
// canvas that is only a node's header and its port — the rest of a box, and the
// whole interior of a compound state, deliberately let clicks through to the
// ground beneath. So a hit test on elements alone reported "nothing" for most
// of every node, and a connection could only be dropped on a target's title
// bar. Falling back to the boxes' own rectangles is what makes the rest of a
// state a drop target.
//
// The innermost, because a compound state's box contains its children and
// connecting to the parent when the pointer is over a child is not what anyone
// meant. Children come after their parent in document order, so the last match
// is the deepest.
function pathAt(x, y) {
  const under = document.elementFromPoint(x, y);
  const node = under && under.closest(NODE);
  if (node) return node.dataset.path;

  let found = null;
  for (const box of document.querySelectorAll(NODE)) {
    const r = box.getBoundingClientRect();
    if (x >= r.left && x <= r.right && y >= r.top && y <= r.bottom) found = box.dataset.path;
  }
  return found;
}

// overCanvas is whether a point is inside the canvas at all.
//
// A move released over the machine list, the mode rail or the footer is a drag
// abandoned, not a drag to somewhere. Without this it posted, and a node
// dropped on the left rail took a large negative coordinate — which then makes
// the chart shift everything to bring it back on screen, moving every other
// node on the canvas.
function overCanvas(canvas, x, y) {
  const r = canvas.getBoundingClientRect();
  return x >= r.left && x <= r.right && y >= r.top && y <= r.bottom;
}

function begin(event) {
  // Left button only. A right-click opens the context menu, which is
  // Datastar's, and a middle-click should do nothing at all.
  if (event.button !== 0) return;
  const port = event.target.closest(PORT);
  const head = event.target.closest(HEAD);
  const node = (port || head) && event.target.closest(NODE);
  if (!node) return;
  const canvas = chartOf(node);
  if (!canvas) return;

  drag = {
    kind: port ? "connect" : "move",
    path: node.dataset.path,
    startX: event.clientX,
    startY: event.clientY,
    node,
    canvas,
    // Where the box was when the drag started, so the live preview can be
    // written back exactly if the drag is abandoned.
    originLeft: node.offsetLeft,
    originTop: node.offsetTop,
  };
  canvas.classList.add("chart--dragging");
  // pointercancel matters as much as pointerup: touch and pen input, a
  // browser-initiated scroll or back-gesture, and the OS taking the pointer all
  // end a gesture that way. Without it the node went on following the cursor
  // and the *next* click anywhere posted the accumulated move.
  document.addEventListener("pointermove", move);
  document.addEventListener("pointerup", end);
  document.addEventListener("pointercancel", cancel);
  document.addEventListener("keydown", cancelOnEscape);
  event.preventDefault();
}

function move(event) {
  if (!drag) return;
  // Captured on the first movement and not on pointerdown, so that a plain
  // click still reaches the link underneath: capturing straight away retargets
  // the pointer events and the click that selects a state never fires.
  //
  // Once a drag is genuinely under way, capture is what makes it end when the
  // pointer leaves the window — document listeners alone do not see a pointerup
  // out there, whatever an earlier comment here claimed.
  if (!drag.captured) {
    drag.captured = true;
    try {
      drag.node.setPointerCapture(event.pointerId);
    } catch {
      // A synthetic event with no live pointer, which the unit tests use. The
      // document listeners still do the work.
    }
  }
  if (drag.kind !== "move") return;
  drag.node.style.left = `${drag.originLeft + (event.clientX - drag.startX)}px`;
  drag.node.style.top = `${drag.originTop + (event.clientY - drag.startY)}px`;
}

function cancelOnEscape(event) {
  if (event.key === "Escape") cancel();
}

// cancel puts the box back and posts nothing. The machine and the layout are
// untouched, which is the whole of what Escape promises.
function cancel() {
  if (!drag) return;
  if (drag.kind === "move") {
    drag.node.style.left = `${drag.originLeft}px`;
    drag.node.style.top = `${drag.originTop}px`;
  }
  finish();
}

function finish() {
  if (drag) drag.canvas.classList.remove("chart--dragging");
  drag = null;
  document.removeEventListener("pointermove", move);
  document.removeEventListener("pointerup", end);
  document.removeEventListener("pointercancel", cancel);
  document.removeEventListener("keydown", cancelOnEscape);
}

function end(event) {
  if (!drag) return;
  const current = drag;
  // The node being dragged is under the pointer and would answer every
  // hit test, so it is hidden for the one measurement that asks.
  const previous = current.node.style.visibility;
  if (current.kind === "connect") current.node.style.visibility = "hidden";
  const overPath = pathAt(event.clientX, event.clientY);
  current.node.style.visibility = previous;

  const result = dragResult(current, {
    x: event.clientX,
    y: event.clientY,
    overPath,
    inCanvas: overCanvas(current.canvas, event.clientX, event.clientY),
  });
  finish();
  if (!result) {
    if (current.kind === "move") {
      current.node.style.left = `${current.originLeft}px`;
      current.node.style.top = `${current.originTop}px`;
    }
    return;
  }
  // The Datastar binding is on the canvas. Dispatching rather than calling is
  // the whole boundary: from here it is an ordinary Forge action.
  current.canvas.dispatchEvent(new CustomEvent("canvasdrop", { detail: result, bubbles: true }));
}

document.addEventListener("pointerdown", begin);
