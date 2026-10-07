// Epic 15 Story 5 — painting, the pointer surface.
//
// The second and last hand-written client JS in Forge. What only a browser can
// show is everything here: static/js/paint.js binds from the document, reads a
// cell size Datastar wrote onto the canvas, and dispatches an event a Datastar
// expression turns into a request. Every link in that chain fails silently —
// an unregistered plugin is skipped without a word, a missing attribute reads
// as NaN, and a dispatch nobody listens for is simply nothing happening.
//
// It also pays off the debt Story 4 recorded: that spec posted its signals by
// hand, because Datastar keeps no readable signal store and nothing in the
// browser could check that the page sends what it is showing. A real click can.
const { test, expect, byTestId } = require("../fixtures");

test.beforeEach(async ({ page }) => {
  await page.goto("/forge/map");
  await expect(byTestId(page, "map-canvas")).toBeVisible();
  // The zoom signal is what the cell size is computed from, so a canvas without
  // it is a canvas paint.js will decline to act on. Waiting for it here is what
  // stops the first test of a run racing hydration.
  await expect(byTestId(page, "map-canvas")).toHaveAttribute("data-cell-w", /^[1-9]/);
});

// Painting writes to the shared editing session, so a failure here would leave
// every later test looking at an unsaved footer it did not cause. Named
// deliberately: a refusal also answers 204, so an un-named discard is silently
// a no-op.
//
// Every map in the project, not the ones the page is currently marking dirty.
// The dirty flag arrives on the SSE stream, so reading it here is a race with
// the stroke this is cleaning up after — and one that loses in the direction
// that leaves the mess behind. Discarding a map with nothing to discard costs a
// request and is not an error.
test.afterEach(async ({ page, baseURL }) => {
  const held = await page.evaluate(() =>
    [...document.querySelectorAll(".list-row")]
      .map((el) => el.href && new URL(el.href).searchParams.get("map"))
      .filter(Boolean),
  );
  for (const path of held) {
    const resp = await fetch(`${baseURL}/forge/map/discard?map=${encodeURIComponent(path)}`, {
      method: "POST",
    });
    if (resp.status !== 204) throw new Error(`could not discard ${path}: ${resp.status}`);
  }
  const dirty = await fetch(`${baseURL}/forge/map`).then((r) => r.text());
  if (dirty.includes("save-footer--dirty")) throw new Error("the session is still unsaved");
});

// The canvas box and the drawn size of one cell, which is all paint.js is
// allowed to know and all a test needs to aim at a cell.
async function geometry(page) {
  const canvas = byTestId(page, "map-canvas");
  const box = await canvas.boundingBox();
  const w = Number(await canvas.getAttribute("data-cell-w"));
  const h = Number(await canvas.getAttribute("data-cell-h"));
  return { box, w, h };
}

// The centre of a cell, in page coordinates.
function centre(g, x, y) {
  return { x: g.box.x + (x + 0.5) * g.w, y: g.box.y + (y + 0.5) * g.h };
}

async function dragCells(page, from, to) {
  const g = await geometry(page);
  const a = centre(g, from[0], from[1]);
  const b = centre(g, to[0], to[1]);
  await page.mouse.move(a.x, a.y);
  await page.mouse.down();
  // Seven, not ten: at ten the samples of a clean diagonal land exactly on cell
  // boundaries, and which side of one a float lands on is not something a test
  // should depend on.
  await page.mouse.move(b.x, b.y, { steps: 7 });
  await page.mouse.up();
}

async function clickCell(page, x, y) {
  const g = await geometry(page);
  const at = centre(g, x, y);
  await page.mouse.move(at.x, at.y);
  await page.mouse.down();
  await page.mouse.up();
}

// How many cells a layer draws. The template emits an element per non-empty
// cell, so this is the map's own contents read back through the page — which is
// what makes erasing visible without saving anything to disk.
function drawnCells(page, layer) {
  return byTestId(page, `map-layer-${layer}`).locator('[data-testid="map-cell"]');
}

function paintRequests(page) {
  const seen = [];
  page.on("request", (r) => {
    if (r.method() === "POST" && r.url().includes("/forge/map/paint")) seen.push(r.url());
  });
  return seen;
}

// The arithmetic, without a pointer driving it. Playwright is then left to
// prove the wiring rather than the sums, the way 13-canvas-editing does for
// dragResult.
test("what a stroke covers is a pure function", async ({ page }) => {
  const cases = await page.evaluate(async () => {
    const { cellAt, lineCells, trail, strokeResult } = await import("/static/js/paint.js");
    const box = { left: 10, top: 20, width: 96, height: 64 };
    const cell = { w: 16, h: 16 };
    return {
      middle: cellAt({ x: 10 + 40, y: 20 + 40 }, box, cell),
      origin: cellAt({ x: 10, y: 20 }, box, cell),
      left: cellAt({ x: 9, y: 30 }, box, cell),
      above: cellAt({ x: 30, y: 19 }, box, cell),
      past: cellAt({ x: 10 + 96, y: 30 }, box, cell),
      below: cellAt({ x: 30, y: 20 + 64 }, box, cell),
      // A browser reports 287.99998 for a box that is 288 wide, and flooring
      // that loses the last column of every map at every zoom.
      lastColumn: cellAt({ x: 10 + 95.5, y: 30 }, { ...box, width: 95.99998 }, cell),
      noZoomYet: cellAt({ x: 30, y: 30 }, box, { w: 0, h: 0 }),

      diagonal: lineCells({ x: 0, y: 0 }, { x: 3, y: 3 }),
      // A pointer that jumps five cells in one frame, which is an ordinary
      // flick: without joining them the stroke is four cells of dotted line.
      jump: lineCells({ x: 0, y: 0 }, { x: 5, y: 0 }),
      backwards: lineCells({ x: 2, y: 2 }, { x: 0, y: 2 }),

      // Joined first, then deduplicated. The other order would draw a line
      // between two cells the pointer never travelled between.
      revisited: trail([
        { x: 0, y: 0 },
        { x: 2, y: 0 },
        { x: 0, y: 0 },
      ]),
      reentered: trail([{ x: 0, y: 0 }, null, { x: 4, y: 0 }]),

      click: strokeResult({ points: [{ x: 2, y: 1 }] }, { cell: { x: 2, y: 1 } }),
      abandoned: strokeResult({ points: [{ x: 0, y: 0 }] }, { cell: null }),
      nothing: strokeResult({ points: [] }, { cell: { x: 0, y: 0 } }),
      nonsense: strokeResult(null, { cell: { x: 0, y: 0 } }),
    };
  });

  expect(cases.middle).toEqual({ x: 2, y: 2 });
  expect(cases.origin).toEqual({ x: 0, y: 0 });
  expect(cases.left).toBeNull();
  expect(cases.above).toBeNull();
  expect(cases.past).toBeNull();
  expect(cases.below).toBeNull();
  expect(cases.lastColumn).toEqual({ x: 5, y: 0 });
  expect(cases.noZoomYet).toBeNull();

  expect(cases.diagonal).toEqual([
    { x: 0, y: 0 },
    { x: 1, y: 1 },
    { x: 2, y: 2 },
    { x: 3, y: 3 },
  ]);
  expect(cases.jump).toEqual([
    { x: 0, y: 0 },
    { x: 1, y: 0 },
    { x: 2, y: 0 },
    { x: 3, y: 0 },
    { x: 4, y: 0 },
    { x: 5, y: 0 },
  ]);
  expect(cases.backwards).toEqual([
    { x: 2, y: 2 },
    { x: 1, y: 2 },
    { x: 0, y: 2 },
  ]);

  expect(cases.revisited).toEqual([
    { x: 0, y: 0 },
    { x: 1, y: 0 },
    { x: 2, y: 0 },
  ]);
  expect(cases.reentered).toEqual([{ x: 0, y: 0 }, { x: 4, y: 0 }]);

  expect(cases.click).toEqual({ x: 2, y: 1, x2: 2, y2: 1, cells: "2,1" });
  expect(cases.abandoned).toBeNull();
  expect(cases.nothing).toBeNull();
  expect(cases.nonsense).toBeNull();
});

test("a click paints one cell and asks once", async ({ page }) => {
  const posts = paintRequests(page);
  await byTestId(page, "tool-erase").click();

  const before = await drawnCells(page, 0).count();
  await clickCell(page, 2, 2);
  await expect(drawnCells(page, 0)).toHaveCount(before - 1);
  expect(posts).toHaveLength(1);
  // A click is a trail of one cell, and goes out as one: the same request a
  // drag that never left its cell produces.
  expect(decodeURIComponent(posts[0])).toContain("cells=2,2");
});

test("a click that does not patch the canvas keeps the ghost under the pointer", async ({ page }) => {
  await byTestId(page, "tool-erase").click();
  await byTestId(page, "layer-select-props").click();
  // An already-empty props cell: no map change and therefore no stream patch.
  await clickCell(page, 5, 3);
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  await expect(byTestId(page, "map-ghost")).toBeVisible();
});

test("a canvas replaced mid-drag does not strand the next gesture", async ({ page }) => {
  const g = await geometry(page);
  const at = centre(g, 1, 1);
  await page.mouse.move(at.x, at.y);
  await page.mouse.down();
  // A stream patch replaces this node while the pointer is down. The old
  // pointer capture may be lost without delivering pointerup to the document.
  await page.evaluate(({ x, y }) => {
    const canvas = document.querySelector('[data-testid="map-canvas"]');
    const replacement = canvas.cloneNode(true);
    replacement.classList.remove("map-canvas--dragging", "map-canvas--hovering");
    canvas.replaceWith(replacement);
    // Simulate another pointer starting a new gesture without a pointerup from
    // the detached canvas (which may have lost its capture off-window).
    replacement.dispatchEvent(new PointerEvent("pointerdown", {
      bubbles: true, button: 0, pointerId: 99, clientX: x, clientY: y,
    }));
  }, at);
  await expect(byTestId(page, "map-canvas")).toHaveClass(/map-canvas--dragging/);
  await page.mouse.up();
  await page.evaluate(() => document.dispatchEvent(new PointerEvent("pointerup", { pointerId: 99 })));
});

// A drag that never left the cell it started in is the gesture a click is, and
// has to go out as the same request. Asserted with a pointer rather than as a
// second call to the pure function: the sampled path of such a drag is a list of
// one either way, so a unit case for it is the click case written twice.
test("a drag that never leaves its cell is a click", async ({ page }) => {
  const posts = paintRequests(page);
  await byTestId(page, "tool-erase").click();

  const g = await geometry(page);
  const at = centre(g, 1, 2);
  await page.mouse.move(at.x, at.y);
  await page.mouse.down();
  // A quarter of a cell in each direction, which is a hand that did not stay
  // still rather than a drag to anywhere.
  await page.mouse.move(at.x + g.w / 4, at.y + g.h / 4, { steps: 4 });
  await page.mouse.up();
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);

  expect(posts).toHaveLength(1);
  // The trail is the last parameter, so anchoring to the end is what says the
  // stroke named one cell and not one cell plus its neighbours.
  expect(decodeURIComponent(posts[0]), "a wobble spanned more than its own cell")
    .toMatch(/&cells=1,2$/);
});

// The bug the trail exists for. Before this story a stamp drag was the
// rectangle its two ends span, so a diagonal across four cells painted sixteen
// — and the fill tool beside it did exactly the same thing.
test("dragging across four cells paints four and issues exactly one request", async ({ page }) => {
  const posts = paintRequests(page);
  await byTestId(page, "tool-erase").click();

  const before = await drawnCells(page, 0).count();
  await dragCells(page, [1, 0], [4, 3]);
  await expect(drawnCells(page, 0)).toHaveCount(before - 4);
  expect(posts, "one gesture, one request").toHaveLength(1);
});

test("leaving and reentering does not paint the cells between", async ({ page }) => {
  await byTestId(page, "tool-erase").click();
  const g = await geometry(page);
  const a = centre(g, 0, 0);
  const b = centre(g, 4, 0);
  const outside = await byTestId(page, "layer-heading").boundingBox();
  const posts = paintRequests(page);
  const before = await drawnCells(page, 0).count();
  await page.mouse.move(a.x, a.y);
  await page.mouse.down();
  await page.mouse.move(outside.x + outside.width / 2, outside.y + outside.height / 2);
  await page.mouse.move(b.x, b.y);
  await page.mouse.up();
  await expect(drawnCells(page, 0)).toHaveCount(before - 2);
  expect(posts).toHaveLength(1);
});

test("another pointer cannot finish this pointer's stroke", async ({ page }) => {
  await byTestId(page, "tool-erase").click();
  const g = await geometry(page);
  const a = centre(g, 0, 0);
  const posts = paintRequests(page);
  await page.mouse.move(a.x, a.y);
  await page.mouse.down();
  await page.evaluate(() => {
    const canvas = document.querySelector('[data-testid="map-canvas"]');
    canvas.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, button: 0, pointerId: 99 }));
    canvas.dispatchEvent(new PointerEvent("pointerup", { bubbles: true, pointerId: 99 }));
  });
  expect(posts, "the other pointer released this stroke").toHaveLength(0);
  await page.mouse.up();
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
  expect(posts).toHaveLength(1);
});

// And the tool beside it means the rectangle, which is the whole difference
// between the two buttons.
test("rect fill previews its rectangle while dragging and commits on release", async ({ page }) => {
  await byTestId(page, "palette-tile-20").click();
  await byTestId(page, "tool-fill").click();

  const g = await geometry(page);
  const marquee = byTestId(page, "map-marquee");
  await expect(marquee).toBeHidden();

  const a = centre(g, 1, 0);
  const b = centre(g, 4, 3);
  await page.mouse.move(a.x, a.y);
  await page.mouse.down();
  await page.mouse.move(b.x, b.y, { steps: 10 });
  await expect(marquee).toBeVisible();

  const box = await marquee.boundingBox();
  expect(Math.round(box.width / g.w)).toBe(4);
  expect(Math.round(box.height / g.h)).toBe(4);

  await page.mouse.up();
  await expect(marquee).toBeHidden();
  // The rectangle, not the diagonal stroke, is what the map now draws.
  const firstPainted = page.locator('[data-cell="ground:1,0"]');
  await expect(firstPainted).toHaveAttribute("style", /background-position:/);
  const paintedStyle = await firstPainted.getAttribute("style");
  const paintedImage = /background-position:[^;]+/.exec(paintedStyle)?.[0];
  expect(paintedImage).toBeTruthy();
  for (let y = 0; y < 4; y++) {
    for (let x = 1; x <= 4; x++) {
      const style = await page.locator(`[data-cell="ground:${x},${y}"]`).getAttribute("style");
      expect(style).toContain(paintedImage);
    }
  }
  expect(await page.locator('[data-cell="ground:0,0"]').getAttribute("style")).not.toContain(paintedImage);
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
});

// The stamp and the eraser follow the pointer, so a rectangle drawn round them
// would promise cells the stroke is not going to paint.
test("the stamp shows no rectangle while dragging", async ({ page }) => {
  await byTestId(page, "palette-tile-1").click();
  const g = await geometry(page);
  const a = centre(g, 0, 0);
  const b = centre(g, 3, 3);
  await page.mouse.move(a.x, a.y);
  await page.mouse.down();
  await page.mouse.move(b.x, b.y, { steps: 8 });
  await expect(byTestId(page, "map-marquee")).toBeHidden();
  await page.mouse.up();
  // The stroke has to land before this test ends. Everything after it — the
  // cleanup, and the next test — shares one editing session, and a request
  // still in flight arrives after the discard that was meant to undo it.
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
});

// A right-click is the context menu's, and a middle click is a scroll gesture.
// Neither is a stroke, and painting on one is how a menu you meant to open
// leaves a tile behind it.
test("only the left button paints", async ({ page }) => {
  const posts = paintRequests(page);
  await byTestId(page, "tool-erase").click();
  const before = await drawnCells(page, 0).count();

  const g = await geometry(page);
  const at = centre(g, 2, 2);
  for (const button of ["right", "middle"]) {
    await page.mouse.move(at.x, at.y);
    await page.mouse.down({ button });
    await page.mouse.up({ button });
  }

  await expect(drawnCells(page, 0)).toHaveCount(before);
  expect(posts, "a click that is not the left button painted").toHaveLength(0);
});

test("the ghost stamp appears on hover and tracks the pointer", async ({ page }) => {
  const ghost = byTestId(page, "map-ghost");
  await expect(ghost).toBeHidden();

  const g = await geometry(page);
  const first = centre(g, 1, 1);
  await page.mouse.move(first.x, first.y);
  await expect(ghost).toBeVisible();
  const at1 = await ghost.boundingBox();

  const second = centre(g, 3, 2);
  await page.mouse.move(second.x, second.y);
  await expect
    .poll(async () => (await ghost.boundingBox()).x)
    .toBe(at1.x + 2 * g.w);
  expect(Math.round((await ghost.boundingBox()).y - at1.y)).toBe(g.h);

  // And it is one cell big at whatever the zoom is. Sized in CSS from the same
  // custom property the canvas scales by; without the zoom in that calc the
  // ghost is a 16px box over a 64px cell and says nothing useful.
  const size = await ghost.boundingBox();
  expect(Math.round(size.width)).toBe(Math.round(g.w));
  expect(Math.round(size.height)).toBe(Math.round(g.h));

  // And it says what a click would do, which is the tool as well as the angle.
  await expect(byTestId(page, "map-ghost-note")).toHaveText("stamp upright");
  await byTestId(page, "turn-cw").click();
  await expect(byTestId(page, "map-ghost-note")).toHaveText("stamp 90°");
  await byTestId(page, "tool-erase").click();
  await expect(byTestId(page, "map-ghost-note")).toHaveText("erase");

  // Off the canvas, off the screen. A ghost left behind over the palette is a
  // click that looks like it would land somewhere it would not.
  await byTestId(page, "map-list").hover();
  await expect(ghost).toBeHidden();
});

test("a drag released over the layer panel paints nothing", async ({ page }) => {
  const posts = paintRequests(page);
  await byTestId(page, "tool-erase").click();
  const before = await drawnCells(page, 0).count();

  const g = await geometry(page);
  const from = centre(g, 2, 2);
  const panel = await byTestId(page, "layer-heading").boundingBox();
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.mouse.move(panel.x + panel.width / 2, panel.y + panel.height / 2, { steps: 10 });
  await page.mouse.up();

  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  expect(posts, "a drag abandoned posts nothing").toHaveLength(0);
  await expect(drawnCells(page, 0)).toHaveCount(before);
});

// The debt Story 4 recorded. Its spec posted signals by hand, because Datastar
// keeps no readable signal store; a real click is the first thing that proves
// the page sends the tool, the tile and the orientation it is displaying.
test("a stroke carries the tile and the orientation the toolbar is showing", async ({ page }) => {
  await byTestId(page, "palette-tile-20").click();
  await byTestId(page, "turn-cw").click();
  await expect(byTestId(page, "map-tools-note")).toHaveText("stamp 90°");
  // The props layer, so the stamp lands on an empty cell rather than replacing
  // one of the ground tiles.
  await byTestId(page, "layer-select-props").click();

  await clickCell(page, 5, 3);
  const painted = page.locator('[data-cell="props:5,3"]');
  await expect(painted).toHaveCount(1);

  // The tile it drew with, which is the one the palette had in hand.
  await expect(painted).toHaveAttribute("style", /background-image/);
  // And the matrix it drew it with. An upright tile is matrix(1,0,0,1,…); any
  // turn changes the first four numbers, so this is the orientation arriving.
  const style = await painted.getAttribute("style");
  const matrix = /matrix\(([^)]*)\)/.exec(style)[1].split(",").slice(0, 4).map(Number);
  expect(matrix, "the stamp was drawn upright, so the rotation never left the page")
    .not.toEqual([1, 0, 0, 1]);
});

// The re-attachment bug, asserted rather than hoped for. The canvas is replaced
// by the page stream whenever anything changes, so a module that bound to the
// element instead of the document would have two handlers after the first
// stroke, and send two requests for the second.
test("after a patch arrives on the stream, the next drag still paints once", async ({ page }) => {
  await byTestId(page, "tool-erase").click();

  const first = paintRequests(page);
  await clickCell(page, 1, 1);
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
  expect(first).toHaveLength(1);

  // That stroke patched the canvas region. The next one has to be one request
  // and one cell all over again.
  const before = await drawnCells(page, 0).count();
  const posts = paintRequests(page);
  await clickCell(page, 2, 1);
  await expect(drawnCells(page, 0)).toHaveCount(before - 1);
  expect(posts, "the canvas was rebound when it was patched").toHaveLength(1);
});

test("the tool shortcuts select the tools", async ({ page }) => {
  for (const [key, tool] of [
    ["r", "fill"],
    ["e", "erase"],
    ["b", "stamp"],
  ]) {
    await page.keyboard.press(key);
    await expect(byTestId(page, `tool-${tool}`)).toHaveAttribute("data-current", "true");
  }

  // Shift, or caps lock, is not a different key. Comparing the key as it
  // arrives leaves every shortcut silently dead in half the world's keyboard
  // states.
  await page.keyboard.press("E");
  await expect(byTestId(page, "tool-erase")).toHaveAttribute("data-current", "true");
  await page.keyboard.press("b");

  // Q turns the stamp and X mirrors it, by the same tables the buttons use.
  await page.keyboard.press("q");
  await expect(byTestId(page, "map-tools-note")).toHaveText("stamp 90°");
  await page.keyboard.press("x");
  await expect(byTestId(page, "map-tools-note")).toHaveText("stamp mirrored 270°");

  // G is the grid, which is view state and never reaches the file. The lines
  // themselves, not only the button: a toggle that lights up and changes
  // nothing on the canvas is the failure worth catching.
  const lines = page.locator(".map-canvas__grid");
  await expect(byTestId(page, "grid-toggle")).toHaveAttribute("data-current", "true");
  await expect(lines).toBeVisible();
  await page.keyboard.press("g");
  await expect(byTestId(page, "grid-toggle")).toHaveAttribute("data-current", "false");
  await expect(lines).toBeHidden();
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  await page.keyboard.press("g");
  await expect(byTestId(page, "grid-toggle")).toHaveAttribute("data-current", "true");
  await expect(lines).toBeVisible();
});

// A shortcut that fires while someone is typing eats the letter and silently
// changes the tool. MAP has no text field of its own yet — the dialogs that
// bring them are Epic 17 — so this puts one on the page and focuses it. The
// handler under test is the real one, bound to the window by the real toolbar.
test("the shortcuts do not fire while a field has the focus", async ({ page }) => {
  await expect(byTestId(page, "tool-stamp")).toHaveAttribute("data-current", "true");
  await page.evaluate(() => {
    const field = document.createElement("input");
    field.type = "text";
    field.id = "typing-probe";
    document.body.appendChild(field);
    field.focus();
  });

  await page.keyboard.type("berg");
  await expect(page.locator("#typing-probe")).toHaveValue("berg");
  // "b" is the stamp and "e" is the eraser. Neither may have moved.
  await expect(byTestId(page, "tool-stamp")).toHaveAttribute("data-current", "true");
  await expect(byTestId(page, "tool-erase")).toHaveAttribute("data-current", "false");
  await expect(byTestId(page, "grid-toggle")).toHaveAttribute("data-current", "true");
});

test("painting works after switching maps and back", async ({ page }) => {
  await byTestId(page, "map-e2e-arena.tmx").click();
  await expect(byTestId(page, "map-title")).toHaveText("e2e-arena.tmx");
  await byTestId(page, "map-e2e-level.tmx").click();
  await expect(byTestId(page, "map-title")).toHaveText("e2e-level.tmx");
  await expect(byTestId(page, "map-canvas")).toHaveAttribute("data-cell-w", /^[1-9]/);

  const posts = paintRequests(page);
  await byTestId(page, "tool-erase").click();
  const before = await drawnCells(page, 0).count();
  await clickCell(page, 3, 1);
  await expect(drawnCells(page, 0)).toHaveCount(before - 1);
  expect(posts).toHaveLength(1);
  expect(posts[0], "the stroke went to the map that was showing").toContain("e2e-level.tmx");
});

// Whether the ghost is *drawn* is not something the DOM can answer. Every
// overlay on this canvas is positioned with z-index auto, so tree order decides
// what covers what — and the ghost has to come before the scaled box in the
// markup to escape its transform. It rendered perfectly and was invisible under
// the tile it was over, and nothing that reads attributes or bounding boxes
// could tell: `toBeVisible` is about display and size, not about what is on top.
//
// So these compare pixels. The canvas is static between the two shots — the map
// does not animate and the cursor is not captured — so any difference is the
// thing being turned on.
test.describe("what is actually drawn", () => {
  const canvasOf = (page) => byTestId(page, "map-canvas");

  test("the ghost is drawn over the tile it is over, not under it", async ({ page }) => {
    // A cell with a tile on it. The fixture's ground layer is fully tiled, so
    // every cell is one — which is exactly why this went unnoticed.
    const plain = await canvasOf(page).screenshot();
    const g = await geometry(page);
    const at = centre(g, 2, 1);
    await page.mouse.move(at.x, at.y);
    await expect(byTestId(page, "map-ghost")).toBeVisible();

    const hovered = await canvasOf(page).screenshot();
    expect(Buffer.compare(plain, hovered), "the ghost changed no pixel on the canvas").not.toBe(0);
  });

  test("the marquee is drawn over the tiles", async ({ page }) => {
    // A tile in hand, so releasing the drag is an ordinary fill rather than a
    // refusal — the refusal is legal and tested elsewhere, but it leaves a
    // banner on a session every later test shares.
    await byTestId(page, "palette-tile-1").click();
    await byTestId(page, "tool-fill").click();
    const g = await geometry(page);
    const a = centre(g, 1, 1);
    const b = centre(g, 3, 2);
    // The ghost must be over the same cell in both shots; otherwise its move
    // would change pixels even with the marquee hidden under the tiles.
    await page.mouse.move(b.x, b.y);
    await expect(byTestId(page, "map-ghost")).toBeVisible();
    const plain = await canvasOf(page).screenshot();
    await page.mouse.move(a.x, a.y);
    await page.mouse.down();
    await page.mouse.move(b.x, b.y, { steps: 5 });
    await expect(byTestId(page, "map-marquee")).toBeVisible();
    const dragging = await canvasOf(page).screenshot();
    await page.mouse.up();
    await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);

    expect(Buffer.compare(plain, dragging), "the marquee changed no pixel").not.toBe(0);
  });

  test("the grid is drawn over the tiles, so turning it off is visible", async ({ page }) => {
    const on = await canvasOf(page).screenshot();
    await byTestId(page, "grid-toggle").click();
    await expect(byTestId(page, "grid-toggle")).toHaveAttribute("data-current", "false");
    const off = await canvasOf(page).screenshot();

    expect(Buffer.compare(on, off), "the grid was already invisible under the tiles").not.toBe(0);
  });
});

test.describe("accessibility", () => {
  // The ghost and the marquee are pointer feedback: they say nothing a screen
  // reader has not already been told by the toolbar and the status line, and
  // announcing a box that moves with the mouse is noise.
  test("the pointer overlays are not announced", async ({ page }) => {
    for (const id of ["map-ghost", "map-marquee"]) {
      await expect(byTestId(page, id)).toHaveAttribute("aria-hidden", "true");
    }
  });

  test("the grid toggle says what it is and what state it is in", async ({ page }) => {
    const grid = byTestId(page, "grid-toggle");
    await expect(grid).toHaveAttribute("aria-pressed", "true");
    await expect(grid).toHaveAttribute("title", "hide the grid");
    await grid.click();
    await expect(grid).toHaveAttribute("aria-pressed", "false");
    await expect(grid).toHaveAttribute("title", "show the grid");
  });
});
