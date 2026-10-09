const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const maps = path.resolve(__dirname, "../fixtures/project/maps");
const tsx = path.join(maps, "e2e-tiles.tsx");
const tileURL = `/forge/tiles?file=${encodeURIComponent(tsx)}&tile=14`;
const discard = `/forge/tiles/discard?file=${encodeURIComponent(tsx)}`;
const reload = `/forge/tiles/reload?file=${encodeURIComponent(tsx)}`;

test("class edits patch the selected inspector and grid once, without writing disk", async ({ page, baseURL }) => {
  const original = fs.readFileSync(tsx, "utf8");
  try {
    await page.goto(tileURL);
    let writes = 0;
    page.on("request", (r) => { if (r.url().includes("/forge/tiles/tile/class")) writes++; });
    const sent = page.waitForResponse((r) => r.url().includes("/forge/tiles/tile/class") && r.request().method() === "POST");
    await byTestId(page, "tile-class-input").fill("hallway");
    await byTestId(page, "tile-class-input").press("Tab");
    expect((await sent).status()).toBe(204);
    await expect(byTestId(page, "tile-class")).toContainText("hallway");
    await expect(byTestId(page, "tile-grid-class-14")).toHaveText("hallway");
    await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
    expect(fs.readFileSync(tsx, "utf8")).toBe(original);
    expect(writes).toBe(1);
    for (const map of ["e2e-level.tmx", "e2e-arena.tmx"]) {
      await page.goto(`/forge/map?map=${encodeURIComponent(path.join(maps, map))}&layer=1&x=2&y=1`);
      await expect(byTestId(page, "tile-artwork-class")).toContainText("hallway");
    }
    await page.goto(tileURL);
    await byTestId(page, "rail-schema").click();
    await expect(byTestId(page, "save-elsewhere")).toContainText("1 unsaved tileset in TILES");
    await page.goBack();
    await expect(byTestId(page, "tile-grid-class-14")).toHaveText("hallway");
    const response = page.waitForResponse((r) => r.url().includes("/forge/tiles/discard") && r.request().method() === "POST");
    page.once("dialog", (dialog) => dialog.accept());
    await byTestId(page, "save-footer").locator("button", { hasText: "Discard" }).click();
    expect((await response).status()).toBe(204);
    await expect(byTestId(page, "tile-class")).toContainText("path");
    await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  } finally {
    fs.writeFileSync(tsx, original);
    await fetch(`${baseURL}${reload}`, { method: "POST" });
  }
});

test("typed properties update both MAP previews and create an implicit sheet tile", async ({ page, baseURL }) => {
  const original = fs.readFileSync(tsx, "utf8");
  try {
    await page.goto(tileURL);
    await byTestId(page, "tile-property-name").fill("entityType");
    await byTestId(page, "tile-property-type").selectOption("string");
    await byTestId(page, "tile-property-value").fill("Wall");
    const sent = page.waitForResponse((r) => r.url().includes("/forge/tiles/tile/property") && r.request().method() === "POST");
    await byTestId(page, "tile-property-submit").click();
    expect((await sent).status()).toBe(204);
    await expect(byTestId(page, "tile-properties")).toContainText("entityType (string)");
    await expect(byTestId(page, "tile-properties")).toContainText("Wall");
    await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
    expect(fs.readFileSync(tsx, "utf8")).toBe(original);

    for (const map of ["e2e-level.tmx", "e2e-arena.tmx"]) {
      const selectedMap = path.join(maps, map);
      await page.goto(`/forge/map?map=${encodeURIComponent(selectedMap)}&layer=1&x=2&y=1`);
      await expect(byTestId(page, "tile-template")).toContainText("referenced Wall");
    }
    await page.goto(`/forge/tiles?file=${encodeURIComponent(tsx)}&tile=1`);
    await expect(byTestId(page, "tile-selected")).toContainText("Implicit sheet tile");
    await byTestId(page, "tile-property-name").fill("story7Implicit");
    await byTestId(page, "tile-property-type").selectOption("int");
    await byTestId(page, "tile-property-value").fill("4");
    const added = page.waitForResponse((r) => r.url().includes("/forge/tiles/tile/property") && r.request().method() === "POST");
    await byTestId(page, "tile-property-submit").click();
    expect((await added).status()).toBe(204);
    await expect(byTestId(page, "tile-properties")).toContainText("story7Implicit (int)");
    await page.goto(tileURL);
    const saving = page.waitForResponse((r) => r.url().includes("/forge/tiles/save") && r.request().method() === "POST");
    await byTestId(page, "save-footer").locator("button", { hasText: "Save" }).click();
    expect((await saving).status()).toBe(204);
    await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
    expect(fs.readFileSync(tsx, "utf8")).toContain('name="entityType" value="Wall"');
    expect(fs.readFileSync(tsx, "utf8")).toContain('<tile id="1"');
  } finally {
    fs.writeFileSync(tsx, original);
    await fetch(`${baseURL}${reload}`, { method: "POST" });
  }
});

test("conflicting TSX save offers file-specific Reload and Overwrite", async ({ page, baseURL }) => {
  const original = fs.readFileSync(tsx, "utf8");
  try {
    await page.goto(tileURL);
    const changeClass = async (value) => {
      const sent = page.waitForResponse((r) => r.url().includes("/forge/tiles/tile/class") && r.request().method() === "POST");
      await byTestId(page, "tile-class-input").fill(value);
      await byTestId(page, "tile-class-input").press("Tab");
      expect((await sent).status()).toBe(204);
      await expect(byTestId(page, "tile-class")).toContainText(value);
    };
    const save = async () => {
      const sent = page.waitForResponse((r) => r.url().includes("/forge/tiles/save?") && r.request().method() === "POST");
      await byTestId(page, "save-footer").locator("button", { hasText: "Save" }).click();
      expect((await sent).status()).toBe(204);
      await expect(byTestId(page, "conflict-reload")).toBeVisible();
      await expect(byTestId(page, "conflict-overwrite")).toBeVisible();
    };
    await changeClass("mine-first");
    fs.writeFileSync(tsx, original.replace('name="tiny16-basic"', 'name="theirs-first"'));
    await save();
    const reloading = page.waitForResponse((r) => r.url().includes("/forge/tiles/reload?") && r.request().method() === "POST");
    await byTestId(page, "conflict-reload").click();
    expect((await reloading).status()).toBe(204);
    await expect(byTestId(page, "tileset-summary")).toContainText("theirs-first");
    await expect(byTestId(page, "tile-class")).toContainText("path");
    await changeClass("mine-second");
    fs.writeFileSync(tsx, original.replace('name="tiny16-basic"', 'name="theirs-second"'));
    await save();
    const overwriting = page.waitForResponse((r) => r.url().includes("/forge/tiles/save/overwrite?") && r.request().method() === "POST");
    await byTestId(page, "conflict-overwrite").click();
    expect((await overwriting).status()).toBe(204);
    await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
    expect(fs.readFileSync(tsx, "utf8")).toContain('type="mine-second"');
  } finally {
    fs.writeFileSync(tsx, original);
    await fetch(`${baseURL}${reload}`, { method: "POST" });
  }
});

test("invalid typed input explains the refusal and leaves the TSX clean", async ({ page, baseURL }) => {
  try {
    await page.goto(tileURL);
    await byTestId(page, "tile-property-name").fill("height");
    await byTestId(page, "tile-property-type").selectOption("int");
    await byTestId(page, "tile-property-value").fill("plenty");
    const sent = page.waitForResponse((r) => r.url().includes("/forge/tiles/tile/property") && r.request().method() === "POST");
    await byTestId(page, "tile-property-submit").click();
    expect((await sent).status()).toBe(204);
    await expect(byTestId(page, "tileset-edit-problem")).toContainText("whole number");
    await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
    await expect(byTestId(page, "tile-properties")).toHaveCount(0);
  } finally {
    await fetch(`${baseURL}${discard}`, { method: "POST" });
  }
});

test("read-only TSJ and a guessed TSX path cannot acquire edit controls or write", async ({ page }) => {
  const legacy = path.join(maps, "e2e-story7-legacy.tsj");
  const map = path.join(maps, "e2e-story7-legacy.tmx");
  const content = JSON.stringify({ name: "legacy", tilewidth: 16, tileheight: 16, tilecount: 1,
    columns: 1, image: "tiny16-basic.png", imagewidth: 16, imageheight: 16 });
  try {
    fs.writeFileSync(legacy, content);
    fs.writeFileSync(map, fs.readFileSync(path.join(maps, "e2e-arena.tmx"), "utf8").replace("e2e-tiles.tsx", path.basename(legacy)));
    await page.goto(`/forge/tiles?file=${encodeURIComponent(legacy)}&tile=0`);
    await expect(byTestId(page, "tile-class-input")).toHaveCount(0);
    await expect(byTestId(page, "tile-property-submit")).toHaveCount(0);
    const refused = await page.request.post(`/forge/tiles/tile/class?file=${encodeURIComponent(legacy)}&tile=0&value=hacked`);
    expect(refused.status()).toBe(204);
    await expect(byTestId(page, "tileset-edit-problem")).toContainText("not a writable project tileset");
    expect(fs.readFileSync(legacy, "utf8")).toBe(content);
    const guessed = path.join(maps, "e2e-story7-not-referenced.tsx");
    fs.writeFileSync(guessed, fs.readFileSync(tsx));
    try {
      const unknown = await page.request.post(`/forge/tiles/tile/class?file=${encodeURIComponent(guessed)}&tile=0&value=hacked`);
      expect(unknown.status()).toBe(204);
      expect(fs.readFileSync(guessed, "utf8")).not.toContain('type="hacked"');
    } finally {
      fs.rmSync(guessed, { force: true });
    }
  } finally {
    fs.rmSync(map, { force: true });
    fs.rmSync(legacy, { force: true });
  }
});

test.describe("accessibility", () => {
  test("class and typed-property controls have labels and keyboard submission", async ({ page, baseURL }) => {
    try {
      await page.goto(tileURL);
      await expect(byTestId(page, "tile-class-input")).toHaveAttribute("id", "tile-class-input");
      await expect(byTestId(page, "tile-property-name")).toHaveAttribute("id", "tile-property-name");
      await expect(byTestId(page, "tile-property-type")).toHaveAttribute("id", "tile-property-type");
      await expect(byTestId(page, "tile-property-value")).toHaveAttribute("id", "tile-property-value");
      await expect(byTestId(page, "tile-class-input")).toHaveAccessibleName("Tile class/type");
      await expect(byTestId(page, "tile-property-name")).toHaveAccessibleName("Property name");
      await expect(byTestId(page, "tile-property-type")).toHaveAccessibleName("Property type");
      await expect(byTestId(page, "tile-property-value")).toHaveAccessibleName("Property value");
      await byTestId(page, "tile-property-name").fill("story7Keyboard");
      await byTestId(page, "tile-property-value").fill("yes");
      const sent = page.waitForResponse((r) => r.url().includes("/forge/tiles/tile/property") && r.request().method() === "POST");
      await byTestId(page, "tile-property-submit").focus();
      await page.keyboard.press("Enter");
      expect((await sent).status()).toBe(204);
      await expect(byTestId(page, "tile-properties")).toContainText("story7Keyboard");
    } finally {
      await fetch(`${baseURL}${discard}`, { method: "POST" });
    }
  });
});
