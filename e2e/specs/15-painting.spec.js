// Epic 15 Story 4 — painting, server side.
//
// The pointer surface is Story 5's, so nothing here clicks the canvas. What
// only a browser can show at this stage is the toolbar: the tool and the
// stamp's orientation are signals, and a signal control renders perfectly and
// does nothing at all if a Datastar attribute names a plugin that is not
// registered. No Go test can see that.
//
// The strokes below post their signals explicitly rather than letting the page
// send them, because Datastar keeps no readable signal store — data-signals on
// #mode-content is the seed, not the live values. That the page sends what it
// is showing is asserted in Story 5, where a click is what starts a stroke.
const { test, expect, byTestId } = require("../fixtures");

test.beforeEach(async ({ page }) => {
  await page.goto("/forge/map");
  await expect(byTestId(page, "tool-stamp")).toBeVisible();
});

// Painting writes to the shared editing session, so a failure here would leave
// every later test looking at an unsaved footer it did not cause.
//
// Discard names its map deliberately — throwing work away is not something to
// resolve by default — so this has to name it too. It is checked rather than
// assumed: a refusal also answers 204, so an un-named discard is silently a
// no-op, which is exactly how this cleanup was wrong the first time.
test.afterEach(async ({ page, baseURL }) => {
  const held = await page.evaluate(() =>
    [...document.querySelectorAll('.list-row[data-dirty="true"]')].map((el) =>
      new URL(el.href).searchParams.get("map"),
    ),
  );
  for (const path of held.filter(Boolean)) {
    const resp = await fetch(`${baseURL}/forge/map/discard?map=${encodeURIComponent(path)}`, {
      method: "POST",
    });
    if (resp.status !== 204) throw new Error(`could not discard ${path}: ${resp.status}`);
  }
  const resp = await fetch(`${baseURL}/forge/schema/discard`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`could not reset the schema session: ${resp.status}`);

  // The session is shared, so leaving anything unsaved here breaks the next
  // test rather than this one. Prove it is clean before handing over.
  const dirty = await fetch(`${baseURL}/forge/map`).then((r) => r.text());
  if (dirty.includes("save-footer--dirty")) throw new Error("the session is still unsaved");
});

// paint posts one stroke the way the pointer surface will, carrying the page's
// own id so a refusal reaches the page that asked.
async function paint(page, query, signals) {
  return page.evaluate(
    async ({ query, signals }) => {
      const seed = JSON.parse(document.querySelector("#mode-content").dataset.signals || "{}");
      const res = await fetch(`/forge/map/paint?${query}`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ page: seed.page, ...signals }),
      });
      return res.status;
    },
    { query, signals },
  );
}

test("the toolbar marks the chosen tool", async ({ page }) => {
  await expect(byTestId(page, "tool-stamp")).toHaveAttribute("data-current", "true");

  await byTestId(page, "tool-fill").click();
  await expect(byTestId(page, "tool-fill")).toHaveAttribute("data-current", "true");
  await expect(byTestId(page, "tool-stamp")).toHaveAttribute("data-current", "false");

  await byTestId(page, "tool-erase").click();
  await expect(byTestId(page, "tool-erase")).toHaveAttribute("data-current", "true");
  await expect(byTestId(page, "tool-fill")).toHaveAttribute("data-current", "false");
});

test("rotating the stamp walks the four quarter turns and comes back", async ({ page }) => {
  const note = byTestId(page, "map-tools-note");
  await expect(note).toHaveText("stamp upright");

  for (const want of ["stamp 90°", "stamp 180°", "stamp 270°", "stamp upright"]) {
    await byTestId(page, "turn-cw").click();
    await expect(note).toHaveText(want);
  }
});

test("mirroring is its own orientation, not a turn", async ({ page }) => {
  const note = byTestId(page, "map-tools-note");
  await byTestId(page, "flip-h").click();
  await expect(note).toHaveText("stamp mirrored");
  // And it is its own inverse.
  await byTestId(page, "flip-h").click();
  await expect(note).toHaveText("stamp upright");

  // A top-to-bottom mirror is a left-to-right one turned half way round, which
  // is what the labels say. There is no naming that avoids this: each reflected
  // orientation is reachable by either button, from a different angle.
  await byTestId(page, "flip-v").click();
  await expect(note).toHaveText("stamp mirrored 180°");
});

// The bug this exists for: turning a mirrored stamp used to throw the mirror
// away and land back on upright, silently. Four turns from a mirrored stamp has
// to walk the mirrored orientations and come back mirrored.
test("turning a mirrored stamp keeps it mirrored", async ({ page }) => {
  const note = byTestId(page, "map-tools-note");
  await byTestId(page, "flip-h").click();
  await expect(note).toHaveText("stamp mirrored");

  for (const want of ["stamp mirrored 90°", "stamp mirrored 180°", "stamp mirrored 270°", "stamp mirrored"]) {
    await byTestId(page, "turn-cw").click();
    await expect(note).toHaveText(want);
  }
});

test("a stroke changes the map and the page hears about it", async ({ page }) => {
  // The fixture's props layer is empty at (0,0), so this puts something where
  // there was nothing and the canvas has to grow a cell.
  const before = await byTestId(page, "map-cell").count();

  expect(await paint(page, "x=0&y=0", { tool: "stamp", tile: 123, layer: 1 })).toBe(204);

  // No reload: the change arrives on the stream the page already holds.
  await expect(byTestId(page, "map-cell")).toHaveCount(before + 1, { timeout: 10_000 });
  await expect(byTestId(page, "save-footer")).toContainText("unsaved");
});

test("a drag is one request and fills the rectangle it spans", async ({ page }) => {
  const before = await byTestId(page, "map-cell").count();

  // Four empty props cells in one stroke, corners given in either order.
  expect(await paint(page, "x=4&y=1&x2=5&y2=2", { tool: "fill", tile: 123, layer: 1 })).toBe(204);

  await expect(byTestId(page, "map-cell")).toHaveCount(before + 4, { timeout: 10_000 });
});

test("painting the tile that is already there leaves the map saved", async ({ page }) => {
  // e2e-level.tmx puts gid 1 at the top-left of ground.
  expect(await paint(page, "x=0&y=0", { tool: "stamp", tile: 1, layer: 0 })).toBe(204);

  await expect(byTestId(page, "save-footer")).not.toContainText("unsaved");
  await expect(byTestId(page, "edit-problem")).toHaveCount(0);
});

test("a refused stroke says why, on the page that asked for it", async ({ page }) => {
  expect(await paint(page, "x=99&y=99", { tool: "stamp", tile: 1, layer: 0 })).toBe(204);

  await expect(byTestId(page, "edit-problem")).toContainText("outside", { timeout: 10_000 });
  await expect(byTestId(page, "save-footer")).not.toContainText("unsaved");
});

// AGENTS asks for this deliberately: test-id selectors cannot get it by
// accident, and a toolbar of unlabelled glyph buttons is unusable without it.
test.describe("accessibility", () => {
  test("each group of tools is named", async ({ page }) => {
    await expect(byTestId(page, "tool-control")).toHaveAttribute("aria-label", "Tool");
    await expect(byTestId(page, "turn-control")).toHaveAttribute("aria-label", "Stamp orientation");
    for (const id of ["tool-control", "turn-control"]) {
      await expect(byTestId(page, id)).toHaveAttribute("role", "group");
    }
  });

  test("every tool says what it does, not just which glyph it is", async ({ page }) => {
    for (const id of ["tool-stamp", "tool-fill", "tool-erase", "turn-cw", "flip-h", "flip-v"]) {
      const title = await byTestId(page, id).getAttribute("title");
      expect(title, `${id} has no title`).toBeTruthy();
      // The glyphs are the only text these carry, and a glyph is not a name.
      expect(title.length, `${id}'s title is "${title}"`).toBeGreaterThan(4);
    }
  });

  test("the chosen tool is announced, not just coloured", async ({ page }) => {
    await expect(byTestId(page, "tool-stamp")).toHaveAttribute("aria-current", "true");
    await byTestId(page, "tool-erase").click();
    await expect(byTestId(page, "tool-erase")).toHaveAttribute("aria-current", "true");
    await expect(byTestId(page, "tool-stamp")).toHaveAttribute("aria-current", "false");
  });

  test("the toolbar is reachable and operable by keyboard", async ({ page }) => {
    await byTestId(page, "tool-stamp").focus();
    await page.keyboard.press("Tab");
    await page.keyboard.press("Enter");
    await expect(byTestId(page, "tool-fill")).toHaveAttribute("aria-current", "true");

    await byTestId(page, "turn-cw").focus();
    await page.keyboard.press("Enter");
    await expect(byTestId(page, "map-tools-note")).toHaveText("stamp 90°");
  });
});
