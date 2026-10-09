const fs = require("fs");
const os = require("os");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const maps = path.resolve(__dirname, "../fixtures/project/maps");
const arena = fs.readFileSync(path.join(maps, "e2e-arena.tmx"), "utf8");
const sheet = path.join(maps, "e2e-tiles.tsx");

test("sheet local IDs crop different pixels, select by URL and restore with history", async ({ page }) => {
  let writes = 0;
  page.on("request", (r) => { if (r.method() === "POST") writes++; });
  await page.goto(`/forge/tiles?file=${encodeURIComponent(sheet)}`);
  await expect(byTestId(page, "tile-grid").locator('a[data-testid^="tile-"]')).toHaveCount(120);
  await expect(byTestId(page, "tile-0")).toHaveAttribute("aria-current", "true");
  await expect(byTestId(page, "tile-properties")).toContainText("entityType (string)");
  await expect(byTestId(page, "tileset-panel")).toContainText("Movement and sight come from referenced entities in MAP");
  const art = byTestId(page, "tile-inspector-art-0");
  const first = await art.screenshot();
  const image = await art.evaluate((node) => getComputedStyle(node).backgroundImage);
  const served = await page.request.get(image.match(/url\(["']?([^"')]+)["']?\)/)[1]);
  expect(served.status()).toBe(200);
  expect(served.headers()["content-type"]).toContain("image/png");
  await byTestId(page, "tile-13").click();
  await expect(page).toHaveURL(/tile=13/);
  await expect(byTestId(page, "tile-class")).toContainText("water");
  await expect(byTestId(page, "tile-13")).toHaveAttribute("aria-current", "true");
  const second = await byTestId(page, "tile-inspector-art-13").screenshot();
  expect(Buffer.compare(first, second), "two different sheet crops rendered the same pixels").not.toBe(0);
  await page.goBack();
  await expect(byTestId(page, "tile-0")).toHaveAttribute("aria-current", "true");
  await page.goForward();
  await expect(byTestId(page, "tile-13")).toHaveAttribute("aria-current", "true");
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  expect(writes, "read-only selection sent a mutation").toBe(0);
});

test("a collection lists only its sparse IDs and uses each tile's image", async ({ page }) => {
  const tsx = path.join(maps, "e2e-story6-collection.tsx");
  const map = path.join(maps, "e2e-story6-collection.tmx");
  try {
    fs.writeFileSync(tsx, `<?xml version="1.0"?><tileset name="sparse" tilewidth="16" tileheight="16" tilecount="2" columns="0"><tile id="1" type="door"><image source="tiny16-basic.png" width="128" height="240"/></tile><tile id="21" type="portal"><image source="tiny16-things.png" width="192" height="128"/></tile></tileset>`);
    fs.writeFileSync(map, arena.replace("e2e-tiles.tsx", path.basename(tsx)));
    await page.goto(`/forge/tiles?file=${encodeURIComponent(tsx)}`);
    const rows = byTestId(page, "tile-grid").locator('a[data-testid^="tile-"]');
    await expect(rows).toHaveCount(2);
    await expect(byTestId(page, "tile-1")).toBeVisible();
    await expect(byTestId(page, "tile-21")).toBeVisible();
    const first = await byTestId(page, "tile-inspector-art-1").screenshot();
    await byTestId(page, "tile-21").click();
    await expect(byTestId(page, "tile-inspector-art-21")).toBeVisible();
    await expect(byTestId(page, "tile-class")).toContainText("portal");
    const image = await byTestId(page, "tile-inspector-art-21").evaluate((node) => getComputedStyle(node).backgroundImage);
    expect(image).toContain("tiny16-things.png");
    const served = await page.request.get(image.match(/url\(["']?([^"')]+)["']?\)/)[1]);
    expect(served.status()).toBe(200);
    expect(served.headers()["content-type"]).toContain("image/png");
    const second = await byTestId(page, "tile-inspector-art-21").screenshot();
    expect(Buffer.compare(first, second), "the collection used one image for two different IDs").not.toBe(0);
  } finally {
    fs.rmSync(map, { force: true });
    fs.rmSync(tsx, { force: true });
  }
});

test("an unknown local ID reports a problem rather than choosing another tile", async ({ page }) => {
  await page.goto(`/forge/tiles?file=${encodeURIComponent(sheet)}&tile=9999`);
  await expect(byTestId(page, "tile-problem")).toContainText("not in this tileset");
  await expect(byTestId(page, "tile-selected")).toHaveCount(0);
  await page.goto(`/forge/tiles?file=${encodeURIComponent(sheet)}&tile=13`);
  await expect(byTestId(page, "tile-selected")).toContainText("Tile 13");
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
});

test("a large sheet pages its grid rather than rendering every tile at once", async ({ page }) => {
  const tsx = path.join(maps, "e2e-story6-long-sheet.tsx");
  const map = path.join(maps, "e2e-story6-long-sheet.tmx");
  try {
    fs.writeFileSync(tsx, `<?xml version="1.0"?><tileset name="long" tilewidth="16" tileheight="16" tilecount="264" columns="8"><image source="tiny16-basic.png" width="128" height="528"/></tileset>`);
    fs.writeFileSync(map, arena.replace("e2e-tiles.tsx", path.basename(tsx)));
    await page.goto(`/forge/tiles?file=${encodeURIComponent(tsx)}`);
    await expect(byTestId(page, "tile-grid").locator('a[data-testid^="tile-"]:not([data-testid^="tile-page-"])')).toHaveCount(256);
    await byTestId(page, "tile-page-next").click();
    await expect(page).toHaveURL(/tilepage=1/);
    await expect(byTestId(page, "tile-grid").locator('a[data-testid^="tile-"]:not([data-testid^="tile-page-"])')).toHaveCount(8);
    await expect(byTestId(page, "tile-256")).toHaveAttribute("aria-current", "true");
    await byTestId(page, "tile-page-previous").click();
    await expect(byTestId(page, "tile-0")).toHaveAttribute("aria-current", "true");
  } finally {
    fs.rmSync(map, { force: true });
    fs.rmSync(tsx, { force: true });
  }
});

test("unavailable art shows a fallback and the project asset route refuses it", async ({ page }) => {
  const outside = fs.mkdtempSync(path.join(os.tmpdir(), "story6-art-"));
  const image = path.join(outside, "outside.png");
  const alias = path.join(maps, "e2e-story6-unsafe.png");
  const tsx = path.join(maps, "e2e-story6-unsafe.tsx");
  const map = path.join(maps, "e2e-story6-unsafe.tmx");
  try {
    fs.copyFileSync(path.join(maps, "tiny16-basic.png"), image);
    fs.symlinkSync(image, alias);
    fs.writeFileSync(tsx, `<?xml version="1.0"?><tileset name="unsafe" tilewidth="16" tileheight="16" tilecount="1" columns="0"><tile id="5"><image source="e2e-story6-unsafe.png" width="16" height="16"/></tile></tileset>`);
    fs.writeFileSync(map, arena.replace("e2e-tiles.tsx", path.basename(tsx)));
    await page.goto(`/forge/tiles?file=${encodeURIComponent(tsx)}`);
    await expect(byTestId(page, "tile-inspector-art-5")).toContainText("art unavailable");
    await page.expectPageErrors([/HTTP 404: .*\/forge\/asset/, /console error: Failed to load resource.*404/], async () => {
      const response = await page.request.get(`/forge/asset?path=${encodeURIComponent(alias)}`);
      expect(response.status()).toBe(404);
    });
  } finally {
    fs.rmSync(map, { force: true });
    fs.rmSync(tsx, { force: true });
    fs.rmSync(alias, { force: true });
    fs.rmSync(outside, { recursive: true, force: true });
  }
});

test.describe("accessibility", () => {
  test("tileset grid and inspector are named, tile links work by keyboard and images have alternatives", async ({ page }) => {
    await page.goto(`/forge/tiles?file=${encodeURIComponent(sheet)}`);
    await expect(byTestId(page, "tile-grid")).toHaveAttribute("aria-label", "Tiles in the selected tileset");
    await expect(byTestId(page, "tile-inspector")).toHaveAttribute("aria-label", "Selected tile");
    await byTestId(page, "tile-13").focus();
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/tile=13/);
    await expect(byTestId(page, "tile-13")).toHaveAttribute("aria-current", "true");
    await expect(byTestId(page, "tile-inspector-art-13")).toHaveAttribute("aria-label", "Tile 13 image");
  });
});
