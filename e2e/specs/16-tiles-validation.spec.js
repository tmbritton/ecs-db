const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const maps = path.resolve(__dirname, "../fixtures/project/maps");
const arena = fs.readFileSync(path.join(maps, "e2e-arena.tmx"), "utf8");

function projectTileset(name, body) {
  const tsx = path.join(maps, `e2e-story8-${name}.tsx`);
  const map = path.join(maps, `e2e-story8-${name}.tmx`);
  fs.writeFileSync(tsx, body);
  fs.writeFileSync(map, arena.replace("e2e-tiles.tsx", path.basename(tsx)));
  return { tsx, map, cleanup: () => { fs.rmSync(map, { force: true }); fs.rmSync(tsx, { force: true }); } };
}

test("missing art and an out-of-bounds sheet rectangle mark their actual tiles", async ({ page }) => {
  const missing = projectTileset("missing", `<?xml version="1.0"?><tileset name="missing" tilewidth="16" tileheight="16" tilecount="2" columns="2"><properties><property name="entityType" value="TestGoblin"/></properties><image source="e2e-story8-no-image.png" width="32" height="16"/></tileset>`);
  const short = projectTileset("short", `<?xml version="1.0"?><tileset name="short" tilewidth="16" tileheight="16" tilecount="2" columns="2"><properties><property name="entityType" value="TestGoblin"/></properties><image source="tiny16-basic.png" width="16" height="16"/></tileset>`);
  const unknown = projectTileset("unknown-size", `<?xml version="1.0"?><tileset name="unknown-size" tilewidth="16" tileheight="16" tilecount="1" columns="1"><properties><property name="entityType" value="TestGoblin"/></properties><image source="tiny16-basic.png" width="0" height="16"/></tileset>`);
  try {
    await page.goto(`/forge/tiles?file=${encodeURIComponent(missing.tsx)}&tile=0`);
    await expect(byTestId(page, "tileset-validation")).toContainText("e2e-story8-no-image.png");
    await expect(byTestId(page, "tile-inspector-art-0")).toContainText("art unavailable");
    await expect(byTestId(page, "tile-grid-invalid-0")).toBeVisible();
    await page.goto(`/forge/tiles?file=${encodeURIComponent(short.tsx)}&tile=0`);
    await expect(byTestId(page, "tile-grid-art-0")).toHaveCSS("background-image", /forge\/asset/);
    await expect(byTestId(page, "tile-validation-0")).not.toContainText("outside the declared sheet image");
    await byTestId(page, "tile-1").click();
    await expect(byTestId(page, "tile-inspector-art-1")).toContainText("art unavailable");
    await expect(byTestId(page, "tile-validation-1")).toContainText("outside the declared sheet image");
    await expect(byTestId(page, "tile-grid-invalid-1")).toBeVisible();
    await page.goto(`/forge/tiles?file=${encodeURIComponent(unknown.tsx)}&tile=0`);
    await expect(byTestId(page, "tile-inspector-art-0")).toContainText("art unavailable");
    await expect(byTestId(page, "tile-validation-0")).toContainText("dimensions");
  } finally {
    missing.cleanup();
    short.cleanup();
    unknown.cleanup();
  }
});

test("bad metadata points to one tile, a repair clears its error, and Tiled-only XML survives Save", async ({ page, baseURL }) => {
  const source = `<?xml version="1.0"?><tileset name="rich" tilewidth="16" tileheight="16" tilecount="2" columns="2"><image source="tiny16-basic.png" width="32" height="16"/><tile id="0" type="wall"><properties><property name="entityType" value="UnknownEnemy"/><property name="passable" type="bool" value="true"/><property name="weight" type="int" value="many"/><property name="Health.hp" type="string" value="lots"/></properties><objectgroup id="1"><object id="2" x="0" y="0"><polygon points="0,0 8,0 8,8"/></object></objectgroup><animation><frame tileid="1" duration="80"/></animation></tile><wangsets><wangset name="seam"/></wangsets><terraintypes><terrain name="grass"/></terraintypes><!-- unknown artist XML --> </tileset>`;
  const fixture = projectTileset("rich", source);
  try {
    await page.goto(`/forge/tiles?file=${encodeURIComponent(fixture.tsx)}&tile=0`);
    await expect(byTestId(page, "tile-validation-0")).toContainText("UnknownEnemy");
    await expect(byTestId(page, "tile-validation-0")).toContainText("passable");
    await expect(byTestId(page, "tile-validation-0")).toContainText("whole number");
    await expect(byTestId(page, "tile-validation-0")).toContainText("Health.hp");
    await expect(byTestId(page, "tiled-only-metadata")).toContainText("collision objects");
    await expect(byTestId(page, "tiled-only-metadata")).toContainText("animation");
    await expect(byTestId(page, "tiled-only-metadata")).toContainText("wangsets");
    await expect(byTestId(page, "tiled-only-metadata")).toContainText("terrains");
    await expect(byTestId(page, "tiled-feature-boundary")).toContainText("not game mechanics");
    await byTestId(page, "tile-property-name").fill("entityType");
    await byTestId(page, "tile-property-value").fill("TestDummy");
    const repaired = page.waitForResponse((r) => r.url().includes("/forge/tiles/tile/property") && r.request().method() === "POST");
    await byTestId(page, "tile-property-submit").click();
    expect((await repaired).status()).toBe(204);
    await expect(byTestId(page, "tile-validation-0")).not.toContainText("UnknownEnemy");
    await expect(byTestId(page, "tile-validation-0")).toContainText("passable");
    await byTestId(page, "tile-property-name").fill("Health.hp");
    await byTestId(page, "tile-property-value").fill("5");
    const fixedHealth = page.waitForResponse((r) => r.url().includes("/forge/tiles/tile/property") && r.request().method() === "POST");
    await byTestId(page, "tile-property-submit").click();
    expect((await fixedHealth).status()).toBe(204);
    await expect(byTestId(page, "tile-validation-0")).not.toContainText("Health.hp");
    await byTestId(page, "tile-class-input").fill("gate");
    const changed = page.waitForResponse((r) => r.url().includes("/forge/tiles/tile/class") && r.request().method() === "POST");
    await byTestId(page, "tile-class-input").press("Tab");
    expect((await changed).status()).toBe(204);
    await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
    const saved = page.waitForResponse((r) => r.url().includes("/forge/tiles/save?") && r.request().method() === "POST");
    await byTestId(page, "save-footer").locator("button", { hasText: "Save" }).click();
    expect((await saved).status()).toBe(204);
    const disk = fs.readFileSync(fixture.tsx, "utf8");
    for (const keep of ["<objectgroup", "<animation", "<wangsets", "<terraintypes", "unknown artist XML"]) {
      expect(disk).toContain(keep);
    }
  } finally {
    await fetch(`${baseURL}/forge/tiles/discard?file=${encodeURIComponent(fixture.tsx)}`, { method: "POST" });
    fixture.cleanup();
  }
});

test("malformed or unsupported project tilesets stay listed with reasons and no editor", async ({ page }) => {
  const malformed = projectTileset("broken", `<tileset name="broken">`);
  const json = path.join(maps, "e2e-story8-invalid.tsj");
  const jsonMap = path.join(maps, "e2e-story8-invalid.tmx");
  const unsupported = path.join(maps, "e2e-story8-unsupported.txt");
  const unsupportedMap = path.join(maps, "e2e-story8-unsupported.tmx");
  try {
    fs.writeFileSync(json, `{ not JSON`);
    fs.writeFileSync(jsonMap, arena.replace("e2e-tiles.tsx", path.basename(json)));
    fs.writeFileSync(unsupported, `<tileset name="unsupported" tilewidth="16" tileheight="16" tilecount="1" columns="1"/>`);
    fs.writeFileSync(unsupportedMap, arena.replace("e2e-tiles.tsx", path.basename(unsupported)));
    await page.goto(`/forge/tiles?file=${encodeURIComponent(malformed.tsx)}`);
    await expect(byTestId(page, "tileset-problem")).toContainText("parsing");
    await expect(byTestId(page, "tileset-maps")).toContainText(path.basename(malformed.map));
    await expect(byTestId(page, "tile-class-input")).toHaveCount(0);
    await expect(byTestId(page, "save-footer").locator("button")).toHaveCount(0);
    await page.goto(`/forge/tiles?file=${encodeURIComponent(json)}`);
    await expect(byTestId(page, "tileset-open-problem")).toContainText(path.basename(json));
    await expect(byTestId(page, "tile-class-input")).toHaveCount(0);
    await page.goto(`/forge/tiles?file=${encodeURIComponent(unsupported)}`);
    await expect(byTestId(page, "tileset-problem")).toContainText(".tsx");
    await expect(byTestId(page, "tileset-maps")).toContainText(path.basename(unsupportedMap));
    await expect(byTestId(page, "tile-class-input")).toHaveCount(0);
    await page.goto(`/forge/tiles?file=${encodeURIComponent(path.join(maps, "e2e-tiles.tsx"))}`);
    await expect(byTestId(page, "tile-0")).toBeVisible();
  } finally {
    fs.rmSync(unsupportedMap, { force: true });
    fs.rmSync(unsupported, { force: true });
    fs.rmSync(jsonMap, { force: true });
    fs.rmSync(json, { force: true });
    malformed.cleanup();
  }
});

test("an authored tile with both class spellings is marked and its edit refused", async ({ page }) => {
  const fixture = projectTileset("ambiguous", `<?xml version="1.0"?><tileset name="ambiguous" tilewidth="16" tileheight="16" tilecount="1" columns="1"><image source="tiny16-basic.png" width="16" height="16"/><tile id="0" type="wall" class="door"/></tileset>`);
  try {
    await page.goto(`/forge/tiles?file=${encodeURIComponent(fixture.tsx)}&tile=0`);
    await expect(byTestId(page, "tile-validation-0")).toContainText("both type and class");
    const refused = page.waitForResponse((r) => r.url().includes("/forge/tiles/tile/class") && r.request().method() === "POST");
    await byTestId(page, "tile-class-input").fill("gate");
    await byTestId(page, "tile-class-input").press("Tab");
    expect((await refused).status()).toBe(204);
    await expect(byTestId(page, "tile-class-problem")).toContainText("both type and class");
    await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  } finally {
    fixture.cleanup();
  }
});

test.describe("accessibility", () => {
  test("validation reasons and edit refusals are text, named by affected tile and input", async ({ page }) => {
    const fixture = projectTileset("a11y", `<?xml version="1.0"?><tileset name="a11y" tilewidth="16" tileheight="16" tilecount="2" columns="2"><image source="tiny16-basic.png" width="32" height="16"/><tile id="0"><properties><property name="passable" type="bool" value="false"/></properties></tile></tileset>`);
    try {
      await page.goto(`/forge/tiles?file=${encodeURIComponent(fixture.tsx)}&tile=0`);
      await expect(byTestId(page, "tile-validation-0")).toHaveAttribute("role", "alert");
      await expect(byTestId(page, "tile-validation-0")).toContainText("MAP");
      await byTestId(page, "tile-property-name").fill("weight");
      await byTestId(page, "tile-property-type").selectOption("int");
      await byTestId(page, "tile-property-value").fill("many");
      const refusal = page.waitForResponse((r) => r.url().includes("/forge/tiles/tile/property") && r.request().method() === "POST");
      await byTestId(page, "tile-property-submit").click();
      expect((await refusal).status()).toBe(204);
      await expect(byTestId(page, "tile-property-problem")).toHaveAttribute("role", "alert");
      await expect(byTestId(page, "tile-property-problem")).toContainText("whole number");
      await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
      await byTestId(page, "tile-1").click();
      await expect(byTestId(page, "tile-property-problem")).toHaveCount(0);
      await expect(byTestId(page, "tileset-edit-problem")).toHaveCount(0);
    } finally {
      fixture.cleanup();
    }
  });
});
