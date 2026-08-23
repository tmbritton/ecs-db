// Epic 13 Story 5 — the statechart canvas you can edit.
//
// This is the story whose failures are invisible everywhere else: a drag
// handler that never binds renders perfectly and does nothing. So every
// assertion here is on the file the session would save, or on a counted
// request — never on a CSS transform, which is what the JS did rather than what
// the machine is.

const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const PROJECT = path.resolve(__dirname, "../fixtures/project");
const BEHAVIORS = path.join(PROJECT, "behaviors");
const NESTED = "e2e-nested";

test.describe.configure({ mode: "serial" });

let original;
test.beforeAll(() => {
  original = {};
  for (const name of fs.readdirSync(BEHAVIORS)) {
    original[name] = fs.readFileSync(path.join(BEHAVIORS, name), "utf8");
  }
});

function restoreFiles() {
  for (const name of fs.readdirSync(BEHAVIORS)) {
    if (!(name in original)) fs.unlinkSync(path.join(BEHAVIORS, name));
  }
  for (const [name, body] of Object.entries(original)) {
    fs.writeFileSync(path.join(BEHAVIORS, name), body);
  }
}

test.afterAll(restoreFiles);

test.afterEach(async ({ baseURL }) => {
  restoreFiles();
  // The menu too. It is per-server, and its backdrop covers the viewport so
  // that clicking anywhere closes it — so one left open by a test that
  // cancelled a confirmation would block the next test's first click. A page
  // load clears it as well; this is belt and braces for tests that never load
  // one.
  await fetch(`${baseURL}/forge/agents/menu?close=1`, { method: "POST" });
  const resp = await fetch(`${baseURL}/forge/agents/reload`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`could not reset the machines: ${resp.status}`);
});

async function openCanvas(page) {
  await page.goto("/forge/agents");
  await byTestId(page, `machine-${NESTED}`).click();
  await expect(byTestId(page, "statechart")).toBeVisible();
}

// saved reads what the session holds, by saving it and reading the file. The
// canvas never writes, so this is the only way to see what it actually did —
// and asserting on the file rather than on the DOM is the whole point of this
// spec.
async function saved(page, baseURL) {
  const resp = await fetch(`${baseURL}/forge/agents/save`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`save: ${resp.status}`);
  // The save is synchronous in the handler, so the file is on disk by the time
  // it answers.
  return JSON.parse(fs.readFileSync(path.join(BEHAVIORS, `${NESTED}.json`), "utf8"));
}

function forgeMeta(machine, state) {
  const node = state.split(".").reduce((acc, part) => acc.states[part], machine);
  return node.meta && node.meta.forge;
}

// The arithmetic, without a browser driving it. Playwright is then left to
// prove the wiring rather than the sums.
test("what a drag means is a pure function", async ({ page }) => {
  await page.goto("/forge/agents");
  const cases = await page.evaluate(async () => {
    const { dragResult } = await import("/static/js/canvas.js");
    const move = { kind: "move", path: "idle", startX: 100, startY: 100 };
    const connect = { kind: "connect", path: "idle", startX: 0, startY: 0 };
    return {
      moved: dragResult(move, { x: 130, y: 80, overPath: null }),
      // A drag that ended where it started is a click. Posting it would write
      // the position a node already had and mark the machine unsaved.
      still: dragResult(move, { x: 100, y: 100, overPath: null }),
      connected: dragResult(connect, { x: 5, y: 5, overPath: "combat" }),
      // Both ways of abandoning a connection.
      onNothing: dragResult(connect, { x: 5, y: 5, overPath: null }),
      onSelf: dragResult(connect, { x: 5, y: 5, overPath: "idle" }),
      nonsense: dragResult({}, { x: 0, y: 0, overPath: null }),
    };
  });

  expect(cases.moved).toEqual({ action: "move", path: "idle", dx: 30, dy: -20 });
  expect(cases.still).toBeNull();
  expect(cases.connected).toEqual({ action: "connect", from: "idle", to: "combat" });
  expect(cases.onNothing).toBeNull();
  expect(cases.onSelf).toBeNull();
  expect(cases.nonsense).toBeNull();
});

test("dragging a node changes its position in the file", async ({ page, baseURL }) => {
  await openCanvas(page);
  const before = forgeMeta(await saved(page, baseURL), "idle");

  const box = await byTestId(page, "state-idle").boundingBox();
  await page.mouse.move(box.x + 60, box.y + 10);
  await page.mouse.down();
  await page.mouse.move(box.x + 60 + 90, box.y + 10 + 40, { steps: 8 });
  await page.mouse.up();

  await expect
    .poll(async () => forgeMeta(await saved(page, baseURL), "idle").x, { timeout: 10_000 })
    .toBe(before.x + 90);
  expect(forgeMeta(await saved(page, baseURL), "idle").y).toBe(before.y + 40);
});

test("dragging and pressing Escape changes nothing", async ({ page, baseURL }) => {
  await openCanvas(page);
  const before = JSON.stringify(await saved(page, baseURL));
  const box = await byTestId(page, "state-idle").boundingBox();

  // The positive control first: the same gesture without Escape does change
  // the file. Without it this test passes if canvas.js never loads at all.
  await page.mouse.move(box.x + 60, box.y + 10);
  await page.mouse.down();
  await page.mouse.move(box.x + 120, box.y + 50, { steps: 6 });
  await page.mouse.up();
  await expect
    .poll(async () => JSON.stringify(await saved(page, baseURL)), { timeout: 10_000 })
    .not.toBe(before);

  const moved = JSON.stringify(await saved(page, baseURL));
  const now = await byTestId(page, "state-idle").boundingBox();
  await page.mouse.move(now.x + 60, now.y + 10);
  await page.mouse.down();
  await page.mouse.move(now.x + 200, now.y + 200, { steps: 6 });
  await page.keyboard.press("Escape");
  await page.mouse.up();
  await page.waitForTimeout(3000);

  expect(JSON.stringify(await saved(page, baseURL))).toBe(moved);
});

// The AC says a drag released outside the canvas leaves the machine untouched,
// on the same terms as Escape. It used to post: a node dropped on the mode rail
// took a large negative coordinate, which then shifts every other node on the
// canvas to bring it back on screen.
test("a drag released outside the canvas changes nothing", async ({ page, baseURL }) => {
  await openCanvas(page);
  const before = JSON.stringify(await saved(page, baseURL));

  const posts = [];
  page.on("request", (r) => r.method() === "POST" && posts.push(r.url()));

  const box = await byTestId(page, "state-idle").boundingBox();
  const rail = await byTestId(page, "machine-list").boundingBox();
  await page.mouse.move(box.x + 60, box.y + 10);
  await page.mouse.down();
  await page.mouse.move(rail.x + rail.width / 2, rail.y + 60, { steps: 8 });
  await page.mouse.up();
  await page.waitForTimeout(1500);

  expect(posts, "a drag off the canvas still asked the server").toHaveLength(0);
  expect(JSON.stringify(await saved(page, baseURL))).toBe(before);
});

test("dragging a port onto another state creates a transition", async ({ page, baseURL }) => {
  await openCanvas(page);
  expect((await saved(page, baseURL)).states.resting.on).toBeUndefined();

  const port = await byTestId(page, "port-resting").boundingBox();
  const target = await byTestId(page, "state-combat.fleeing").boundingBox();
  await page.mouse.move(port.x + 6, port.y + 6);
  await page.mouse.down();
  await page.mouse.move(target.x + 80, target.y + 18, { steps: 10 });
  await page.mouse.up();

  await expect
    .poll(async () => Object.keys((await saved(page, baseURL)).states.resting.on || {}), {
      timeout: 10_000,
    })
    .toHaveLength(1);

  const on = (await saved(page, baseURL)).states.resting.on;
  const [event] = Object.keys(on);
  // The server named it, and it did not reuse one the machine already means
  // something by.
  expect(["SPOTTED", "POKED", "NUDGED", "LOST", "RECOVER"]).not.toContain(event);
  const target0 = Array.isArray(on[event]) ? on[event][0] : on[event];
  expect(JSON.stringify(target0)).toContain("fleeing");
});

test("a connection dropped on empty space creates nothing and posts nothing", async ({
  page,
  baseURL,
}) => {
  await openCanvas(page);
  const before = JSON.stringify(await saved(page, baseURL));

  const posts = [];
  page.on("request", (r) => r.method() === "POST" && posts.push(r.url()));

  const port = await byTestId(page, "port-resting").boundingBox();
  const space = await byTestId(page, "canvas-ground").boundingBox();
  await page.mouse.move(port.x + 6, port.y + 6);
  await page.mouse.down();
  await page.mouse.move(space.x + space.width - 40, space.y + space.height - 30, { steps: 8 });
  await page.mouse.up();
  await page.waitForTimeout(1500);

  expect(posts, "a connection dropped on nothing still asked the server").toHaveLength(0);
  expect(JSON.stringify(await saved(page, baseURL))).toBe(before);

  // The positive control: the same gesture onto a state does post, so this test
  // is not passing because nothing is wired up at all.
  const target = await byTestId(page, "state-combat.fleeing").boundingBox();
  const again = await byTestId(page, "port-resting").boundingBox();
  await page.mouse.move(again.x + 6, again.y + 6);
  await page.mouse.down();
  await page.mouse.move(target.x + 80, target.y + 18, { steps: 8 });
  await page.mouse.up();
  await expect.poll(() => posts.length, { timeout: 10_000 }).toBe(1);
});

// A connection may be dropped anywhere on a state, not only on its title bar.
// The node layer lets clicks through everywhere else, so a hit test on elements
// alone reported "nothing" for most of every box — and for the whole interior
// of a compound state, which could not be connected to at all.
test("a connection can be dropped anywhere on a state", async ({ page, baseURL }) => {
  await openCanvas(page);

  const port = await byTestId(page, "port-resting").boundingBox();
  const target = await byTestId(page, "state-combat.fleeing").boundingBox();
  // The bottom of the box, well below the header that used to be the only
  // place this worked.
  await page.mouse.move(port.x + 6, port.y + 6);
  await page.mouse.down();
  await page.mouse.move(target.x + 80, target.y + target.height - 4, { steps: 10 });
  await page.mouse.up();

  await expect
    .poll(async () => Object.keys((await saved(page, baseURL)).states.resting.on || {}).length, {
      timeout: 10_000,
    })
    .toBe(1);
});


test("double-clicking empty space adds a state", async ({ page, baseURL }) => {
  await openCanvas(page);
  const before = Object.keys((await saved(page, baseURL)).states);

  const space = await byTestId(page, "canvas-ground").boundingBox();
  await page.mouse.dblclick(space.x + 620, space.y + 240);

  await expect
    .poll(async () => Object.keys((await saved(page, baseURL)).states).length, { timeout: 10_000 })
    .toBe(before.length + 1);

  const machine = await saved(page, baseURL);
  const added = Object.keys(machine.states).find((name) => !before.includes(name));
  // Placed where it was dropped, or every new state lands in the same spot.
  expect(forgeMeta(machine, added)).toBeTruthy();
  expect(forgeMeta(machine, added).x).toBeGreaterThan(400);
});

test("right-clicking a state opens its menu, and set-as-initial moves the tag", async ({
  page,
  baseURL,
}) => {
  await openCanvas(page);
  expect((await saved(page, baseURL)).initial).toBe("idle");

  await byTestId(page, "select-state-resting").click({ button: "right" });
  await expect(byTestId(page, "canvas-menu")).toBeVisible({ timeout: 10_000 });
  await expect(byTestId(page, "canvas-menu")).toContainText("resting");

  await page.getByRole("menuitem", { name: "Set as initial" }).click();

  await expect
    .poll(async () => (await saved(page, baseURL)).initial, { timeout: 10_000 })
    .toBe("resting");
  // The tag on the canvas follows, without a reload.
  await expect(byTestId(page, "state-resting")).toHaveAttribute("data-initial", "true");
  // And the menu closed: one left open over a machine that has just changed
  // describes something that may no longer be there.
  await expect(byTestId(page, "canvas-menu")).toHaveCount(0);
});

test("deleting a state that is a target warns, and cancelling keeps it", async ({
  page,
  baseURL,
}) => {
  await openCanvas(page);

  await byTestId(page, "select-state-combat.attacking").click({ button: "right" });
  await expect(byTestId(page, "canvas-menu")).toBeVisible({ timeout: 10_000 });

  let asked = "";
  page.once("dialog", (d) => {
    asked = d.message();
    d.dismiss();
  });
  await page.getByRole("menuitem", { name: "Delete state" }).click();
  await page.waitForTimeout(1500);

  // It names what would break rather than counting it.
  expect(asked).toContain("POKED");
  expect(asked).toContain("idle");
  // Cancelling kept the state.
  expect((await saved(page, baseURL)).states.combat.states.attacking).toBeDefined();
});

// A handler bound twice is indistinguishable from one bound once until you
// count — and the mode content is replaced by the page stream on every change,
// so a module that re-attached on each patch would double up as you worked.
test("ten drags leave ten consistent positions, one request each", async ({ page, baseURL }) => {
  await openCanvas(page);
  const start = forgeMeta(await saved(page, baseURL), "idle");

  const moves = [];
  page.on("request", (r) => {
    if (r.method() === "POST" && r.url().includes("move=")) moves.push(r.url());
  });

  for (let i = 0; i < 10; i++) {
    const box = await byTestId(page, "state-idle").boundingBox();
    await page.mouse.move(box.x + 60, box.y + 10);
    await page.mouse.down();
    await page.mouse.move(box.x + 70, box.y + 15, { steps: 3 });
    await page.mouse.up();
    // Wait for the canvas to come back before the next one, so each drag starts
    // from where the server put the box rather than from a stale rectangle.
    await expect
      .poll(async () => forgeMeta(await saved(page, baseURL), "idle").x, { timeout: 10_000 })
      .toBe(start.x + 10 * (i + 1));
  }

  expect(moves, `${moves.length} requests for ten drags`).toHaveLength(10);
  const end = forgeMeta(await saved(page, baseURL), "idle");
  expect(end.x).toBe(start.x + 100);
  expect(end.y).toBe(start.y + 50);
});

test("accessibility", async ({ page }) => {
  await openCanvas(page);

  // The menu is a menu, with menu items, and it is named after what it is about.
  await byTestId(page, "select-state-resting").click({ button: "right" });
  await expect(byTestId(page, "canvas-menu")).toBeVisible({ timeout: 10_000 });
  await expect(page.getByRole("menu")).toHaveAttribute("aria-label", /resting/i);
  await expect(page.getByRole("menuitem", { name: "Set as initial" })).toBeVisible();

  // Focus moves into the menu when it opens, so it is operable rather than
  // merely reachable — without it a keyboard user tabs past every remaining
  // node head and edge label to get there.
  const items = page.getByRole("menuitem");
  await expect(items.first()).toBeFocused();
  for (const item of await items.all()) {
    await expect(item).toHaveJSProperty("tagName", "BUTTON");
  }

  // And Escape backs out of it. There was no keyboard way out at all.
  await page.keyboard.press("Escape");
  await expect(byTestId(page, "canvas-menu")).toHaveCount(0, { timeout: 10_000 });

  // The menu key opens it in the first place, which is what makes rename,
  // set-as-initial and delete reachable without a pointer at all — they live
  // nowhere else until Story 6.
  await byTestId(page, "select-state-resting").focus();
  await page.keyboard.press("ContextMenu");
  await expect(byTestId(page, "canvas-menu")).toBeVisible({ timeout: 10_000 });

  // The port is decoration with a title: dragging is a pointer gesture, and the
  // thing it achieves — a transition — is reachable another way in Story 7.
  await expect(byTestId(page, "port-resting")).toHaveAttribute("aria-hidden", "true");
  await expect(byTestId(page, "port-resting")).toHaveAttribute("title", /connect/i);
});
