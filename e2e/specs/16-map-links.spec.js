const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const level = path.resolve(__dirname, "../fixtures/project/maps/e2e-level.tmx");

test("inspect tool selects a positioned Tile without painting or dirtying the map", async ({ page }) => {
  await page.goto("/forge/map");
  const canvas = byTestId(page, "map-canvas");
  await expect(canvas).toHaveAttribute("data-cell-w", /^[1-9]/);
  await byTestId(page, "tool-inspect").click();
  const w = Number(await canvas.getAttribute("data-cell-w"));
  const h = Number(await canvas.getAttribute("data-cell-h"));
  let paintRequests = 0;
  page.on("request", (request) => {
    if (request.url().includes("/forge/map/paint")) paintRequests++;
  });
  await canvas.click({ position: { x: 1.5 * w, y: 1.5 * h } });
  await expect(page).toHaveURL(/layer=1.*x=1.*y=1/);
  await expect(byTestId(page, "tile-template")).toContainText("Floor");
  await expect(byTestId(page, "tile-template")).toContainText("gid 12");
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  expect(paintRequests).toBe(0);
  await byTestId(page, "layer-select-props").click();
  await byTestId(page, "tool-inspect").click();
  await canvas.click({ position: { x: 1.5 * w, y: 1.5 * h } });
  await expect(page).toHaveURL(/layer=3.*x=1.*y=1/);
  await expect(byTestId(page, "tile-selected")).toContainText("layer 3");
  await expect(byTestId(page, "tile-template")).toContainText("gid 20");
  expect(paintRequests).toBe(0);
});

test("inspecting another Tile preserves zoom, hidden layers and the Inspect tool", async ({ page }) => {
  await page.goto("/forge/map");
  await byTestId(page, "zoom-8").click();
  await byTestId(page, "layer-eye-props").click();
  await byTestId(page, "tool-inspect").click();
  const canvas = byTestId(page, "map-canvas");
  const w = Number(await canvas.getAttribute("data-cell-w"));
  const h = Number(await canvas.getAttribute("data-cell-h"));
  await canvas.click({ position: { x: 2.5 * w, y: 1.5 * h } });
  await expect(page).toHaveURL(/layer=1.*x=2.*y=1/);
  await expect(byTestId(page, "zoom-8")).toHaveAttribute("data-current", "true");
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-hidden", "true");
  await expect(byTestId(page, "tool-inspect")).toHaveAttribute("data-current", "true");
  await canvas.click({ position: { x: 3.5 * w, y: 1.5 * h } });
  await expect(page).toHaveURL(/layer=1.*x=3.*y=1/);
  await expect(byTestId(page, "tile-selected")).toContainText("3,1");
});

test.describe("accessibility", () => {
  test("Tile inspection can be chosen by keyboard and has named controls", async ({ page }) => {
    await page.goto("/forge/map");
    const inspect = byTestId(page, "tool-inspect");
    await expect(inspect).toHaveAttribute("aria-label", "Inspect a Tile");
    await inspect.focus();
    await page.keyboard.press("Enter");
    await expect(inspect).toHaveAttribute("data-current", "true");
    const canvas = byTestId(page, "map-canvas");
    const w = Number(await canvas.getAttribute("data-cell-w"));
    const h = Number(await canvas.getAttribute("data-cell-h"));
    await canvas.click({ position: { x: 2.5 * w, y: 2.5 * h } });
    await expect(byTestId(page, "tile-selected")).toHaveAttribute("aria-label", "Selected Tile");
    await expect(byTestId(page, "tile-close")).toHaveAttribute("aria-label", "Close Tile inspector");
  });

  test("arrow keys and Enter inspect a cell without a pointer or paint request", async ({ page }) => {
    await page.goto("/forge/map");
    await byTestId(page, "zoom-8").click();
    await byTestId(page, "layer-eye-props").click();
    await byTestId(page, "tool-inspect").click();
    const canvas = byTestId(page, "map-canvas");
    let paints = 0;
    page.on("request", (r) => { if (r.url().includes("/forge/map/paint")) paints++; });
    await canvas.focus();
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("ArrowDown");
    await expect(byTestId(page, "map-inspect-cursor")).toHaveClass(/map-inspect-cursor--on/);
    const cursor = await byTestId(page, "map-inspect-cursor").boundingBox();
    const bounds = await canvas.boundingBox();
    const cellW = Number(await canvas.getAttribute("data-cell-w"));
    const cellH = Number(await canvas.getAttribute("data-cell-h"));
    expect(cursor.x - bounds.x).toBeCloseTo(cellW, 0);
    expect(cursor.y - bounds.y).toBeCloseTo(cellH, 0);
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/layer=1.*x=1.*y=1/);
    await expect(byTestId(page, "tile-template")).toContainText("gid 12");
    await expect(byTestId(page, "zoom-8")).toHaveAttribute("data-current", "true");
    await expect(byTestId(page, "layer-props")).toHaveAttribute("data-hidden", "true");
    await expect(byTestId(page, "tool-inspect")).toHaveAttribute("data-current", "true");
    expect(paints).toBe(0);
  });

  test("link selection and removal name their target", async ({ page, baseURL }) => {
    const held = "e2e/fixtures/project/maps/e2e-level.tmx";
    try {
      await page.goto("/forge/map");
      const canvas = byTestId(page, "map-canvas");
      const w = Number(await canvas.getAttribute("data-cell-w"));
      const h = Number(await canvas.getAttribute("data-cell-h"));
      await byTestId(page, "spawn-type-TestGoblin").dragTo(canvas, {
        targetPosition: { x: 2.5 * w, y: 1.5 * h },
      });
      await expect(byTestId(page, "spawn-1")).toBeVisible();
      await byTestId(page, "tool-inspect").click();
      await canvas.click({ position: { x: 2.5 * w, y: 1.5 * h } });
      await expect(byTestId(page, "tile-link-target")).toHaveAttribute("aria-label", "Link an object to this Tile");
      const sent = page.waitForResponse((r) => r.url().includes("/forge/map/tile/link") && r.request().method() === "POST");
      await byTestId(page, "tile-link-target").selectOption("1");
      await sent;
      await expect(byTestId(page, "tile-unlink-1")).toHaveAttribute("aria-label", "Unlink object 1 from this Tile");
    } finally {
      const response = await fetch(`${baseURL}/forge/map/discard?map=${encodeURIComponent(held)}`, { method: "POST" });
      if (response.status !== 204) throw new Error(`could not discard map: ${response.status}`);
    }
  });
});

test("two painted cells link the same object and unlinking one preserves the other", async ({ page, baseURL }) => {
  const held = "e2e/fixtures/project/maps/e2e-level.tmx";
  try {
    await page.goto("/forge/map");
    const canvas = byTestId(page, "map-canvas");
    const w = Number(await canvas.getAttribute("data-cell-w"));
    const h = Number(await canvas.getAttribute("data-cell-h"));
    await byTestId(page, "spawn-type-TestGoblin").dragTo(canvas, {
      targetPosition: { x: 2.5 * w, y: 1.5 * h },
    });
    await expect(byTestId(page, "spawn-1")).toBeVisible();
    const inspect = async (x, y) => {
      await byTestId(page, "tool-inspect").click();
      await canvas.click({ position: { x: (x + .5) * w, y: (y + .5) * h } });
      await expect(page).toHaveURL(new RegExp(`layer=1.*x=${x}.*y=${y}`));
    };
    await inspect(2, 1);
    const sent = page.waitForResponse((r) => r.url().includes("/forge/map/tile/link") && r.request().method() === "POST");
    await byTestId(page, "tile-link-target").selectOption("1");
    await sent;
    await expect(byTestId(page, "tile-linked-1")).toBeVisible();
    await inspect(3, 2);
    const second = page.waitForResponse((r) => r.url().includes("/forge/map/tile/link") && r.request().method() === "POST");
    await byTestId(page, "tile-link-target").selectOption("1");
    await second;
    await expect(byTestId(page, "tile-linked-1")).toBeVisible();
    await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
    const unlink = page.waitForResponse((r) => r.url().includes("/forge/map/tile/link") && r.request().method() === "POST");
    await byTestId(page, "tile-unlink-1").click();
    await unlink;
    await expect(byTestId(page, "tile-linked-1")).toHaveCount(0);
    await inspect(2, 1);
    await expect(byTestId(page, "tile-linked-1")).toBeVisible();
    await byTestId(page, "tool-erase").click();
    const erase = page.waitForResponse((r) => r.url().includes("/forge/map/paint") && r.request().method() === "POST");
    await canvas.click({ position: { x: 2.5 * w, y: 1.5 * h } });
    await erase;
    await expect(page.locator('[data-cell="ground:2,1"]')).toHaveCount(0);
    await inspect(2, 1);
    await expect(byTestId(page, "tile-empty")).toBeVisible();
    await expect(byTestId(page, "spawn-1")).toBeVisible();
  } finally {
    const response = await fetch(`${baseURL}/forge/map/discard?map=${encodeURIComponent(held)}`, { method: "POST" });
    if (response.status !== 204) throw new Error(`could not discard map: ${response.status}`);
  }
});

test("saved TileLink reopens, while discard restores the saved footprint", async ({ page, baseURL }) => {
  const original = fs.readFileSync(level, "utf8");
  const held = "e2e/fixtures/project/maps/e2e-level.tmx";
  try {
    await page.goto("/forge/map");
    const canvas = byTestId(page, "map-canvas");
    const w = Number(await canvas.getAttribute("data-cell-w"));
    const h = Number(await canvas.getAttribute("data-cell-h"));
    await byTestId(page, "spawn-type-TestGoblin").dragTo(canvas, {
      targetPosition: { x: 2.5 * w, y: 1.5 * h },
    });
    await expect(byTestId(page, "spawn-1")).toBeVisible();
    await byTestId(page, "tool-inspect").click();
    await canvas.click({ position: { x: 2.5 * w, y: 1.5 * h } });
    await expect(page).toHaveURL(/layer=1.*x=2.*y=1/);
    const sent = page.waitForResponse((r) => r.url().includes("/forge/map/tile/link") && r.request().method() === "POST");
    await byTestId(page, "tile-link-target").selectOption("1");
    await sent;
    await expect(byTestId(page, "tile-linked-1")).toBeVisible();
    expect((await page.request.post(`/forge/map/save?map=${encodeURIComponent(held)}`)).status()).toBe(204);
    expect(fs.readFileSync(level, "utf8")).toContain('name="TileLink.layerID"');
    await page.reload();
    await expect(byTestId(page, "tile-linked-1")).toBeVisible();
    const unlink = page.waitForResponse((r) => r.url().includes("/forge/map/tile/link") && r.request().method() === "POST");
    await byTestId(page, "tile-unlink-1").click();
    await unlink;
    await expect(byTestId(page, "tile-linked-1")).toHaveCount(0);
    expect((await page.request.post(`/forge/map/discard?map=${encodeURIComponent(held)}`)).status()).toBe(204);
    await expect(byTestId(page, "tile-linked-1")).toBeVisible();
  } finally {
    fs.writeFileSync(level, original);
    const response = await fetch(`${baseURL}/forge/map/reload?map=${encodeURIComponent(held)}`, { method: "POST" });
    if (response.status !== 204) throw new Error(`could not reload original map: ${response.status}`);
  }
});

test("a missing object link is visibly refused without changing the map", async ({ page }) => {
  await page.goto("/forge/map?layer=1&x=1&y=1");
  const path = await byTestId(page, "map-path").textContent();
  const response = await page.request.post(`/forge/map/tile/link?map=${encodeURIComponent(path)}&action=link&layer=1&x=1&y=1&id=77`);
  expect(response.status()).toBe(204);
  await expect(byTestId(page, "edit-problem")).toContainText("object ID 77");
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
});

test("an existing broken TileLink marks its object instead of disappearing", async ({ page, baseURL }) => {
  const original = fs.readFileSync(level, "utf8");
  const held = "e2e/fixtures/project/maps/e2e-level.tmx";
  try {
    fs.writeFileSync(level, original.replace('<objectgroup id="2" name="spawns"/>',
      '<objectgroup id="2" name="spawns"><object id="5" type="TestGoblin" x="0" y="0"><properties>' +
      '<property name="TileLink.layerID" type="int" value="3"/>' +
      '</properties></object></objectgroup>'));
    expect((await page.request.post(`/forge/map/reload?map=${encodeURIComponent(held)}`)).status()).toBe(204);
    await page.goto("/forge/map");
    await expect(byTestId(page, "spawn-5")).toHaveAttribute("data-invalid", "true");
    await expect(byTestId(page, "spawn-5")).toHaveAttribute("title", /empty layer 3 cell/);
  } finally {
    fs.writeFileSync(level, original);
    const response = await fetch(`${baseURL}/forge/map/reload?map=${encodeURIComponent(held)}`, { method: "POST" });
    if (response.status !== 204) throw new Error(`could not reload original map: ${response.status}`);
  }
});

test("an untyped TileLink still shows its import refusal on the map", async ({ page, baseURL }) => {
  const original = fs.readFileSync(level, "utf8");
  const held = "e2e/fixtures/project/maps/e2e-level.tmx";
  try {
    fs.writeFileSync(level, original.replace('<objectgroup id="2" name="spawns"/>',
      '<objectgroup id="2" name="spawns"><object id="5" x="0" y="0"><properties>' +
      '<property name="TileLink.layerID" type="int" value="1"/>' +
      '</properties></object></objectgroup>'));
    expect((await page.request.post(`/forge/map/reload?map=${encodeURIComponent(held)}`)).status()).toBe(204);
    await page.goto("/forge/map");
    await expect(byTestId(page, "tile-link-object-5")).toHaveAttribute("data-invalid", "true");
    await expect(byTestId(page, "tile-link-object-5")).toHaveAttribute("title", /no entity type/);
    await expect(byTestId(page, "spawn-5")).toHaveCount(0);
  } finally {
    fs.writeFileSync(level, original);
    const response = await fetch(`${baseURL}/forge/map/reload?map=${encodeURIComponent(held)}`, { method: "POST" });
    if (response.status !== 204) throw new Error(`could not reload original map: ${response.status}`);
  }
});
