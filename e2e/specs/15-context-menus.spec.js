// Epic 15 Story 8 — menu actions are edits, not decorative rows.
const { test, expect, byTestId } = require("../fixtures");

test.beforeEach(async ({ page }) => {
  await page.goto("/forge/map");
  await expect(byTestId(page, "map-canvas")).toHaveAttribute("data-cell-w", /^[1-9]/);
});

test.afterEach(async ({ baseURL }) => {
  await fetch(`${baseURL}/forge/map/menu?close=1`, { method: "POST" });
  for (const name of ["e2e-level.tmx", "e2e-arena.tmx"]) {
    const path = `e2e/fixtures/project/maps/${name}`;
    const response = await fetch(`${baseURL}/forge/map/discard?map=${encodeURIComponent(path)}`, { method: "POST" });
    if (response.status !== 204) throw new Error(`cannot discard ${name}: ${response.status}`);
  }
});

function menu(page) { return byTestId(page, "map-context-menu"); }
function items(page) { return menu(page).locator(".ctx-menu__item"); }

test("right-clicking a layer opens one menu; Escape and outside click close it", async ({ page }) => {
  await byTestId(page, "layer-props").click({ button: "right" });
  await expect(menu(page)).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(menu(page)).toHaveCount(0);

  await byTestId(page, "layer-props").click({ button: "right" });
  await byTestId(page, "layer-ground").click({ button: "right" });
  await expect(menu(page)).toHaveCount(1);
  await byTestId(page, "map-title").click();
  await expect(menu(page)).toHaveCount(0);
});

test("renaming through the layer menu changes the working map", async ({ page }) => {
  await byTestId(page, "layer-props").click({ button: "right" });
  page.once("dialog", (dialog) => dialog.accept("decor"));
  await items(page).first().click();
  await expect(byTestId(page, "layer-decor")).toBeVisible();
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
  await expect(menu(page)).toHaveCount(0);
});

test("moving a tile layer changes the topmost image at an overlapping cell", async ({ page }) => {
  // The browser's hit test answers which layer actually paints on top. Merely
  // reordering the list would leave that answer wrong and must fail this spec.
  const props = page.locator('[data-cell="props:1,1"]');
  const box = await props.boundingBox();
  const topCell = () => page.evaluate(({ x, y }) =>
    document.elementFromPoint(x, y)?.closest(".map-cell")?.dataset.cell,
    { x: box.x + box.width / 2, y: box.y + box.height / 2 });
  expect(await topCell()).toBe("props:1,1");
  await byTestId(page, "layer-props").click({ button: "right" });
  await items(page).nth(2).click();
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
  await expect.poll(topCell).toBe("ground:1,1");
  const names = await byTestId(page, "map-list").locator(".layer-row__name").allTextContents();
  expect(names).toEqual(["props", "ground"]);
});

test("the menu eye changes only the view and the picker respects it", async ({ page }) => {
  await byTestId(page, "layer-props").click({ button: "right" });
  await items(page).nth(1).click();
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-hidden", "true");
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  const canvas = byTestId(page, "map-canvas");
  const box = await canvas.boundingBox();
  const w = Number(await canvas.getAttribute("data-cell-w"));
  const h = Number(await canvas.getAttribute("data-cell-h"));
  await page.mouse.click(box.x + w * 1.5, box.y + h * 1.5, { button: "right" });
  await items(page).first().click();
  await expect(byTestId(page, "map-selected-tile")).toHaveText("tile 12");
});

test("a moved layer keeps its eye and paint selection", async ({ page }) => {
  await byTestId(page, "layer-select-props").click();
  await byTestId(page, "layer-props").click({ button: "right" });
  await items(page).nth(1).click();
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-hidden", "true");
  await byTestId(page, "layer-props").click({ button: "right" });
  await items(page).nth(2).click();
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-hidden", "true");
  await expect(byTestId(page, "layer-ground")).toHaveAttribute("data-hidden", "false");
  await expect(byTestId(page, "map-active-layer")).toHaveText("painting props");
  // Unhide without selecting another layer; the next stroke must still land
  // in props, now at index 0, rather than in ground at its old index.
  await byTestId(page, "layer-props").click({ button: "right" });
  await items(page).nth(1).click();
  await byTestId(page, "palette-tile-20").click();
  const canvas = byTestId(page, "map-canvas");
  const box = await canvas.boundingBox();
  const w = Number(await canvas.getAttribute("data-cell-w"));
  const h = Number(await canvas.getAttribute("data-cell-h"));
  await page.mouse.click(box.x + w / 2, box.y + h / 2);
  await expect(byTestId(page, "map-layer-0").locator('[data-cell="props:0,0"]')).toHaveCount(1);
  await expect(byTestId(page, "map-layer-1").locator('[data-cell="ground:0,0"]')).toHaveCount(1);
});

test("a layer deletion announces its next-load cost before changing anything", async ({ page }) => {
  await byTestId(page, "layer-props").click({ button: "right" });
  let warning = "";
  page.once("dialog", async (dialog) => {
    warning = dialog.message();
    await dialog.dismiss();
  });
  await items(page).last().click();
  expect(warning).toContain("tiles");
  await expect(byTestId(page, "layer-props")).toBeVisible();
  page.once("dialog", (dialog) => dialog.accept());
  await items(page).last().click();
  await expect(byTestId(page, "layer-props")).toHaveCount(0);
  await expect(byTestId(page, "map-layer-count")).toHaveText("1 layer");
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
});

test("deleting the selected layer never redirects its next stroke into another layer", async ({ page }) => {
  await byTestId(page, "layer-select-props").click();
  await byTestId(page, "layer-props").click({ button: "right" });
  page.once("dialog", (dialog) => dialog.accept());
  await items(page).last().click();
  await expect(byTestId(page, "map-active-layer")).toHaveText("painting nothing");
  await byTestId(page, "palette-tile-20").click();
  const canvas = byTestId(page, "map-canvas");
  const box = await canvas.boundingBox();
  const w = Number(await canvas.getAttribute("data-cell-w"));
  const h = Number(await canvas.getAttribute("data-cell-h"));
  await page.mouse.click(box.x + w / 2, box.y + h / 2);
  await expect(byTestId(page, "edit-problem")).toContainText("choose another layer");
  await expect(byTestId(page, "map-layer-0").locator('[data-cell="ground:0,0"]')).toHaveCount(1);
});

test("a canvas cell offers an eyedropper, not another stamp button", async ({ page }) => {
  await byTestId(page, "palette-tile-20").click();
  const box = await byTestId(page, "map-canvas").boundingBox();
  const w = Number(await byTestId(page, "map-canvas").getAttribute("data-cell-w"));
  const h = Number(await byTestId(page, "map-canvas").getAttribute("data-cell-h"));
  await page.mouse.click(box.x + w / 2, box.y + h / 2, { button: "right" });
  await expect(menu(page)).toBeVisible();
  await items(page).first().click();
  await expect(byTestId(page, "map-selected-tile")).toHaveText("tile 1");
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
});

test("the spawn menu duplicates with a fresh id and deletes its target", async ({ page }) => {
  const canvas = byTestId(page, "map-canvas");
  const w = Number(await canvas.getAttribute("data-cell-w"));
  const h = Number(await canvas.getAttribute("data-cell-h"));
  await byTestId(page, "spawn-type-TestGoblin").dragTo(canvas, {
    targetPosition: { x: w / 2, y: h / 2 },
  });
  await expect(byTestId(page, "spawn-1")).toBeVisible();
  await byTestId(page, "spawn-1").click({ button: "right" });
  await expect(menu(page)).toBeVisible();
  await items(page).first().click();
  await expect(byTestId(page, "spawn-2")).toBeVisible();
  await expect(menu(page)).toHaveCount(0);
  await byTestId(page, "spawn-2").click({ button: "right" });
  await items(page).last().click();
  await expect(byTestId(page, "spawn-2")).toHaveCount(0);
  await expect(byTestId(page, "spawn-1")).toBeVisible();
});

test.describe("accessibility", () => {
  test("menus have an accessible name and keyboard dismissal", async ({ page }) => {
    await byTestId(page, "layer-ground").click({ button: "right" });
    await expect(menu(page).locator('[role="menu"]')).toHaveAttribute("aria-label", /layer/i);
    await expect(menu(page).locator("[autofocus]")).toHaveCount(1);
    await page.keyboard.press("Escape");
    await expect(menu(page)).toHaveCount(0);
  });
});
