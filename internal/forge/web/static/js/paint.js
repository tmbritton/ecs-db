// The map canvas's pointer handling, and nothing else.
//
// The second and last hand-written client JS in Forge, here for the same reason
// canvas.js is and no other: a stamp under a dragging pointer has to follow it
// at pointer speed, and a server round trip per cell is not a design anyone
// would defend.
//
// So it owns pointer state and stops there. It never fetches, never renders,
// and does not know what a map is. Concretely it may read the canvas's client
// rect, the drawn size of one cell, and which element the pointer is over; it
// may write custom properties and classes on that canvas; and on release it
// dispatches one CustomEvent, which a Datastar expression turns into the same
// @post every other control in Forge uses.
//
// It may not know a gid, a layer, a tool or a tileset. Every one of those is a
// signal, and the expression the server wrote over it is what decides whether a
// gesture means a rectangle or a trail, what the ghost says, and where the
// stroke is sent. A canvas that kept its own model of the map would be a second
// source of truth, and the first thing it would disagree with is the file.
//
// Bound once, from the document. The canvas is replaced by the page stream
// whenever anything changes, so a listener attached to it dies with it — and a
// module that re-attached on each patch is how you get two handlers and two
// requests per stroke.

/**
 * Which cell of the canvas a point is in.
 *
 * The canvas box is already at drawn scale — the zoom is a CSS transform on the
 * box *inside* it — so this is a division and nothing about the map's own pixel
 * size. How many cells there are comes from the box and the cell, for the same
 * reason: the number of columns is not something this file is allowed to know.
 *
 * @param {{x: number, y: number}} point client coordinates
 * @param {{left: number, top: number, width: number, height: number}} box
 * @param {{w: number, h: number}} cell the drawn size of one cell, in pixels
 * @returns {{x: number, y: number}|null} null when the point is not on the canvas
 */
export function cellAt(point, box, cell) {
  if (!point || !box || !cell) return null;
  if (!(cell.w > 0) || !(cell.h > 0)) return null;
  const x = Math.floor((point.x - box.left) / cell.w);
  const y = Math.floor((point.y - box.top) / cell.h);
  // Rounded, not floored: a 6-cell map at 3x is 288.0 wide in theory and
  // 287.99998 in a browser, and flooring that loses the last column.
  const cols = Math.round(box.width / cell.w);
  const rows = Math.round(box.height / cell.h);
  if (x < 0 || y < 0 || x >= cols || y >= rows) return null;
  return { x, y };
}

/**
 * The cells on the straight line between two cells, both ends included.
 *
 * A pointer sampled at frame rate skips cells whenever it moves faster than one
 * cell per frame — a 100px flick at 1x zoom crosses six 16px cells and reports
 * one — so something has to join them up. Bresenham, because it visits each
 * cell of the line exactly once and never two at a time, which is what a brush
 * stroke looks like.
 *
 * @param {{x: number, y: number}} from
 * @param {{x: number, y: number}} to
 * @returns {Array<{x: number, y: number}>}
 */
export function lineCells(from, to) {
  const out = [];
  let x = from.x;
  let y = from.y;
  const dx = Math.abs(to.x - x);
  const dy = -Math.abs(to.y - y);
  const sx = x < to.x ? 1 : -1;
  const sy = y < to.y ? 1 : -1;
  let err = dx + dy;
  for (;;) {
    out.push({ x, y });
    if (x === to.x && y === to.y) return out;
    const e2 = 2 * err;
    if (e2 >= dy) {
      err += dy;
      x += sx;
    }
    if (e2 <= dx) {
      err += dx;
      y += sy;
    }
  }
}

/**
 * The cells a sampled pointer path covers: consecutive samples joined, and each
 * cell named once.
 *
 * Named once because the list is what gets painted, and a cell painted twice is
 * a longer request for the same result. It is also what bounds the request: a
 * stroke can wander for a minute and still never name more cells than the map
 * has.
 *
 * Deduplicating is only sound *after* the joining. Dropping repeats from the
 * raw samples and letting the server join what was left would draw a line
 * between two cells the pointer never travelled between, and paint a diagonal
 * across the map on the way back to a cell already visited.
 *
 * @param {Array<{x: number, y: number}>} points
 * @returns {Array<{x: number, y: number}>}
 */
export function trail(points) {
  const out = [];
  const seen = new Set();
  const push = (c) => {
    const key = `${c.x},${c.y}`;
    if (seen.has(key)) return;
    seen.add(key);
    out.push(c);
  };
  let previous = null;
  for (const point of points) {
    if (point) {
      if (previous) for (const c of lineCells(previous, point)) push(c);
      else push(point);
    }
    previous = point;
  }
  return out;
}

/**
 * What a completed gesture means.
 *
 * Pure, and separated out so the arithmetic is testable without a browser
 * driving it: Playwright is then left to prove the wiring rather than the sums.
 *
 * All three descriptions of the gesture travel together — where it started,
 * where it ended, and every cell it covered — because which of them is meant is
 * the tool's business, and the tool is a signal this file does not read.
 *
 * @param {{points: Array<{x: number, y: number}>}} drag
 * @param {{cell: {x: number, y: number}|null}} drop
 * @returns {{x: number, y: number, x2: number, y2: number, cells: string}|null}
 *   null when nothing should be posted
 */
export function strokeResult(drag, drop) {
  if (!drag || !drag.points || drag.points.length === 0) return null;
  // Released off the canvas — over the layer panel, the palette, the mode rail.
  // That is a gesture abandoned, and the story says so: nothing posted, nothing
  // changed, exactly as Escape does.
  if (!drop || !drop.cell) return null;

  const points = drag.points.concat([drop.cell]);
  const start = points[0];
  const cells = trail(points);
  return {
    x: start.x,
    y: start.y,
    x2: drop.cell.x,
    y2: drop.cell.y,
    cells: cells.map((c) => `${c.x},${c.y}`).join(","),
  };
}

const CANVAS = ".map-canvas";

let drag = null;
let hovered = null;

// cellSize is the drawn size of one cell, which the server binds from the zoom
// signal. Absent means Datastar has not run yet — on a page that has not
// hydrated there is no tool, no tile and no layer either, so a stroke would
// have nothing to say.
function cellSize(canvas) {
  const w = Number(canvas.getAttribute("data-cell-w"));
  const h = Number(canvas.getAttribute("data-cell-h"));
  if (!(w > 0) || !(h > 0)) return null;
  return { w, h };
}

// canvasOf is the canvas an event happened on, or null. Both entry points go
// through it so neither can be the one that forgets the guard: pointer events
// target Elements, but a synthetic one dispatched at the document does not, and
// a TypeError in a pointermove listener takes the whole surface out silently.
function canvasOf(event) {
  const target = event.target;
  return target && target.closest ? target.closest(CANVAS) : null;
}

function cellUnder(canvas, event) {
  const cell = cellSize(canvas);
  if (!cell) return null;
  return cellAt({ x: event.clientX, y: event.clientY }, canvas.getBoundingClientRect(), cell);
}

// The ghost and the marquee are written as custom properties rather than
// signals. They move on every pointer event, and a signal write is a
// re-evaluation of every expression bound to it — sixty times a second, for
// something no other expression reads.
//
// A patch replacing the canvas takes them with it. That is correct rather than
// merely tolerable: the next pointer move puts the ghost back, and a stroke
// that was in flight when the map changed underneath it should not finish.
function showGhost(canvas, cell) {
  canvas.style.setProperty("--ghost-x", cell.x);
  canvas.style.setProperty("--ghost-y", cell.y);
  canvas.classList.add("map-canvas--hovering");
}

function hideGhost(canvas) {
  canvas.classList.remove("map-canvas--hovering");
}

function showMarquee(canvas, from, to) {
  canvas.style.setProperty("--sel-x", Math.min(from.x, to.x));
  canvas.style.setProperty("--sel-y", Math.min(from.y, to.y));
  canvas.style.setProperty("--sel-w", Math.abs(to.x - from.x) + 1);
  canvas.style.setProperty("--sel-h", Math.abs(to.y - from.y) + 1);
  canvas.classList.add("map-canvas--dragging");
}

function begin(event) {
  // Left button only. A right-click is Story 8's context menu and a middle
  // click should do nothing at all.
  if (event.button !== 0) return;
  // A page-stream patch can replace the canvas while it holds capture. If the
  // release happened outside the window, no pointerup need reach the document.
  if (drag && !drag.canvas.isConnected) finish();
  if (drag) return;
  const canvas = canvasOf(event);
  if (!canvas) return;
  const cell = cellUnder(canvas, event);
  if (!cell) return;

  drag = { canvas, pointerId: event.pointerId, points: [cell] };
  showGhost(canvas, cell);
  showMarquee(canvas, cell, cell);
  document.addEventListener("pointermove", move);
  document.addEventListener("pointerup", end);
  document.addEventListener("pointercancel", cancelPointer);
  document.addEventListener("keydown", cancelOnEscape);
  canvas.addEventListener("lostpointercapture", cancelPointer);
  // Or the browser starts a text selection across the whole panel, which then
  // eats the pointerup.
  event.preventDefault();
}

// hover is the ghost following the pointer when nothing is being dragged. It is
// on the document rather than the canvas because the canvas is replaced by the
// page stream, and it is the same listener that has to notice the pointer
// leaving.
function hover(event) {
  if (drag && !drag.canvas.isConnected) finish();
  if (drag) return;
  const canvas = canvasOf(event);
  const cell = canvas && cellUnder(canvas, event);
  // The last canvas the ghost was shown on, remembered rather than looked up:
  // this runs on every pointer move anywhere on the page, and a querySelector
  // per move to turn off something that is usually already off is a lot of work
  // to do nothing.
  if (hovered && hovered !== canvas) hideGhost(hovered);
  hovered = cell ? canvas : null;
  if (cell) showGhost(canvas, cell);
  else if (canvas) hideGhost(canvas);
}

function move(event) {
  if (!drag || event.pointerId !== drag.pointerId) return;
  if (!drag.canvas.isConnected) {
    finish();
    return;
  }
  // Captured on the first movement rather than on pointerdown, so a plain click
  // is still a plain click: capturing straight away retargets the pointer
  // events and the click never reaches anything underneath.
  //
  // Once a drag is genuinely under way, capture is what makes it end when the
  // pointer leaves the window.
  if (!drag.captured) {
    drag.captured = true;
    try {
      drag.canvas.setPointerCapture(event.pointerId);
    } catch {
      // A synthetic event with no live pointer, which the unit tests use. The
      // document listeners still do the work.
    }
  }
  const cell = cellUnder(drag.canvas, event);
  if (!cell) {
    // Off the canvas mid-drag. The gesture is not abandoned yet — coming back
    // is ordinary — but nothing is added and nothing is shown out there.
    hideGhost(drag.canvas);
    if (drag.points[drag.points.length - 1] !== null) drag.points.push(null);
    return;
  }
  const last = drag.points[drag.points.length - 1];
  if (!last || cell.x !== last.x || cell.y !== last.y) drag.points.push(cell);
  showGhost(drag.canvas, cell);
  showMarquee(drag.canvas, drag.points[0], cell);
}

function cancelOnEscape(event) {
  if (event.key === "Escape") cancel();
}

function cancelPointer(event) {
  if (drag && event.pointerId === drag.pointerId) cancel();
}

// cancel posts nothing and leaves the map exactly as it was, which is the whole
// of what Escape promises. Nothing has to be put back: unlike a dragged node,
// a stroke changes nothing on screen until the server answers.
function cancel() {
  finish();
}

function finish() {
  if (drag) {
    drag.canvas.classList.remove("map-canvas--dragging");
    hideGhost(drag.canvas);
    drag.canvas.removeEventListener("lostpointercapture", cancelPointer);
  }
  drag = null;
  document.removeEventListener("pointermove", move);
  document.removeEventListener("pointerup", end);
  document.removeEventListener("pointercancel", cancelPointer);
  document.removeEventListener("keydown", cancelOnEscape);
}

function end(event) {
  if (!drag || event.pointerId !== drag.pointerId) return;
  const current = drag;
  const cell = current.canvas.isConnected ? cellUnder(current.canvas, event) : null;
  const result = strokeResult(current, { cell });
  finish();
  if (cell) showGhost(current.canvas, cell);
  if (!result) return;
  // The Datastar binding is on the canvas. Dispatching rather than calling is
  // the whole boundary: from here it is an ordinary Forge action.
  current.canvas.dispatchEvent(new CustomEvent("paintstroke", { detail: result, bubbles: true }));
}

document.addEventListener("pointerdown", begin);
document.addEventListener("pointermove", hover);
