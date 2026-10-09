const fs = require("fs");
const os = require("os");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const maps = path.resolve(__dirname, "../fixtures/project/maps");
const shared = path.join(maps, "e2e-tiles.tsx");
const level = fs.readFileSync(path.join(maps, "e2e-arena.tmx"), "utf8");

test("shared TSX is one selectable file with both maps, and survives navigation", async ({ page }) => {
  await page.goto("/forge/tiles");
  const row = byTestId(page, "tileset-e2e-tiles.tsx");
  await expect(row).toHaveCount(1);
  await row.click();
  await expect(page).toHaveURL(/\/forge\/tiles\?file=.*e2e-tiles\.tsx/);
  await expect(byTestId(page, "tileset-title")).toHaveText("e2e-tiles.tsx");
  await expect(byTestId(page, "tileset-maps")).toContainText("e2e-level.tmx");
  await expect(byTestId(page, "tileset-maps")).toContainText("e2e-arena.tmx");
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  await expect(byTestId(page, "tileset-panel")).toContainText("the next ecs-db run");
  await byTestId(page, "rail-map").click();
  await byTestId(page, "rail-tiles").click();
  await byTestId(page, "tileset-e2e-tiles.tsx").click();
  await expect(byTestId(page, "tileset-maps")).toContainText("e2e-arena.tmx");
  await expect(byTestId(page, "tileset-e2e-tiles.tsx")).toHaveCount(1);
});

test("listed JSON tileset is read-only; guessed file never becomes selectable", async ({ page }) => {
  const tsj = path.join(maps, "e2e-story5-legacy.tsj");
  const map = path.join(maps, "e2e-story5-legacy.tmx");
  const embeddedMap = path.join(maps, "e2e-story5-embedded.tmx");
  try {
    fs.writeFileSync(tsj, JSON.stringify({
      name: "legacy", tilewidth: 16, tileheight: 16, tilecount: 120, columns: 8,
      image: "tiny16-basic.png", imagewidth: 128, imageheight: 240,
    }));
    fs.writeFileSync(map, level.replace("e2e-tiles.tsx", path.basename(tsj)));
    fs.writeFileSync(embeddedMap, level.replace('<tileset firstgid="1" source="e2e-tiles.tsx"/>',
      '<tileset firstgid="1" name="embedded" tilewidth="16" tileheight="16" tilecount="1" columns="1"><image source="tiny16-basic.png" width="16" height="16"/></tileset>'));
    await page.goto("/forge/tiles");
    await expect(byTestId(page, "tileset-list").locator('a[data-testid^="tileset-"]')).toHaveCount(3);
    await byTestId(page, `tileset-${path.basename(tsj)}`).click();
    await expect(page).toHaveURL(/file=.*e2e-story5-legacy\.tsj/);
    await expect(byTestId(page, "tileset-readonly")).toBeVisible();
    await expect(byTestId(page, "tileset-summary")).toContainText("legacy");
    await expect(byTestId(page, "save-footer")).toContainText("read-only");
    await expect(byTestId(page, "save-footer").locator("button")).toHaveCount(0);
    await expect(byTestId(page, "save-footer-reload")).toHaveCount(0);
    const guessed = path.join(maps, "e2e-things.tsx") + ".unlisted";
    await page.goto(`/forge/tiles?file=${encodeURIComponent(guessed)}`);
    await expect(byTestId(page, "tileset-title")).not.toHaveText(path.basename(guessed));
    const resp = await page.request.post(`/forge/tiles/save?file=${encodeURIComponent(guessed)}`);
    expect(resp.status()).toBe(204);
    await expect(byTestId(page, "tileset-edit-problem")).toContainText("not a writable project tileset");
  } finally {
    fs.rmSync(embeddedMap, { force: true });
    fs.rmSync(map, { force: true });
    fs.rmSync(tsj, { force: true });
  }
});

test("a map-referenced symlink outside the project is listed with a refusal, never opened", async ({ page }) => {
  const external = fs.mkdtempSync(path.join(os.tmpdir(), "forge-story5-"));
  const secret = path.join(external, "secret.tsx");
  const alias = path.join(maps, "e2e-story5-escape.tsx");
  const map = path.join(maps, "e2e-story5-escape.tmx");
  try {
    fs.writeFileSync(secret, fs.readFileSync(shared));
    fs.symlinkSync(secret, alias);
    fs.writeFileSync(map, level.replace("e2e-tiles.tsx", path.basename(alias)));
    await page.goto("/forge/tiles");
    await byTestId(page, `tileset-${path.basename(alias)}`).click();
    await expect(byTestId(page, "tileset-readonly")).toContainText("Unavailable for editing");
    await expect(byTestId(page, "tileset-problem")).toContainText("outside the project root");
    await expect(byTestId(page, "tileset-summary")).toHaveCount(0);
    await expect(byTestId(page, "save-footer")).toHaveCount(1);
    await expect(byTestId(page, "save-footer").locator("button")).toHaveCount(0);
    const resp = await page.request.post(`/forge/tiles/save?file=${encodeURIComponent(alias)}`);
    expect(resp.status()).toBe(204);
    await expect(byTestId(page, "tileset-edit-problem")).toContainText("not a writable project tileset");
  } finally {
    fs.rmSync(map, { force: true });
    fs.rmSync(alias, { force: true });
    fs.rmSync(external, { recursive: true, force: true });
  }
});

test("Reload from disk picks up a changed TSX without navigating or saving", async ({ page }) => {
  const tsx = path.join(maps, "e2e-story5-reload.tsx");
  const map = path.join(maps, "e2e-story5-reload.tmx");
  try {
    const original = fs.readFileSync(shared, "utf8");
    fs.writeFileSync(tsx, original);
    fs.writeFileSync(map, level.replace("e2e-tiles.tsx", path.basename(tsx)));
    await page.goto("/forge/tiles");
    await byTestId(page, `tileset-${path.basename(tsx)}`).click();
    const before = await byTestId(page, "tileset-summary").innerText();
    fs.writeFileSync(tsx, original.replace('name="tiny16-basic"', 'name="reloaded-story5"'));
    page.once("dialog", (dialog) => dialog.accept());
    const posted = page.waitForResponse((r) => r.url().includes("/forge/tiles/reload") && r.request().method() === "POST");
    await byTestId(page, "save-footer-reload").click();
    expect((await posted).status()).toBe(204);
    await expect(byTestId(page, "tileset-summary")).not.toHaveText(before);
    await expect(byTestId(page, "tileset-summary")).toContainText("reloaded-story5");
    await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  } finally {
    fs.rmSync(map, { force: true });
    fs.rmSync(tsx, { force: true });
  }
});

test.describe("accessibility", () => {
  test("tileset file selection is keyboard navigable and read-only status is explicit", async ({ page }) => {
    await page.goto("/forge/tiles");
    await expect(byTestId(page, "tileset-list")).toHaveAttribute("aria-label", "External tilesets");
    const row = byTestId(page, "tileset-e2e-tiles.tsx");
    await row.focus();
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/file=.*e2e-tiles\.tsx/);
    await expect(byTestId(page, "tileset-e2e-tiles.tsx")).toHaveAttribute("aria-current", "true");
    await expect(byTestId(page, "save-footer-reload")).toBeEnabled();
    await expect(byTestId(page, "save-footer").locator("button", { hasText: "Save" })).toBeDisabled();
  });
});
