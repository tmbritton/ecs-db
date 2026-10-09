const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const root = path.resolve(__dirname, "../fixtures/project");
const toml = path.join(root, "assets/animations.toml");
const source = path.join(root, "e2e-import-source.png");
const invalid = path.join(root, "e2e-import-tall.png");
const destination = path.join(root, "assets/sprites/e2e-import-source.png");
const invalidDestination = path.join(root, "assets/sprites/e2e-import-tall.png");
let original;

async function makePNG(page, width, height) {
  const base64 = await page.evaluate(({ width, height }) => {
    const canvas = document.createElement("canvas");
    canvas.width = width;
    canvas.height = height;
    const ctx = canvas.getContext("2d");
    ctx.fillStyle = "#e90";
    ctx.fillRect(0, 0, width / 2, height);
    ctx.fillStyle = "#05b";
    ctx.fillRect(width / 2, 0, width / 2, height);
    return canvas.toDataURL("image/png").split(",")[1];
  }, { width, height });
  return Buffer.from(base64, "base64");
}

test.beforeEach(async ({ page }) => {
  original = fs.readFileSync(toml, "utf8");
  await page.goto("/forge/schema");
  fs.writeFileSync(source, await makePNG(page, 32, 16));
});

test.afterEach(async ({ baseURL }) => {
  fs.writeFileSync(toml, original);
  fs.rmSync(source, { force: true });
  fs.rmSync(invalid, { force: true });
  fs.rmSync(destination, { force: true });
  fs.rmSync(invalidDestination, { force: true });
  const response = await fetch(`${baseURL}/forge/sprites/reload?file=${encodeURIComponent(toml)}`, { method: "POST" });
  if (response.status !== 204) throw new Error(`cannot restore import fixture: ${response.status}`);
});

test("import copies a project PNG into the active mod and drafts playable frames and a binding", async ({ page }) => {
  await page.goto("/forge/sprites");
  await byTestId(page, "sprite-import-open").click();
  await expect(byTestId(page, "sprite-import-dialog").locator('[role="dialog"]')).toHaveAttribute("aria-label", "Import sprite sheet");
  await byTestId(page, "sprite-import-source").selectOption(source);
  await byTestId(page, "sprite-import-name").fill("e2e-imported");
  await byTestId(page, "sprite-import-entity-type").fill("TestDummy");
  const posted = page.waitForRequest((r) => r.url().includes("/forge/sprites/import") && r.method() === "POST");
  await byTestId(page, "sprite-import-submit").click();
  await posted;
  await expect(page).toHaveURL(/animation=e2e-imported/);
  await expect(byTestId(page, "sprite-frames")).toContainText("column 1");
  await expect(byTestId(page, "entity-sheet-TestDummy")).toBeVisible();
  expect(fs.readFileSync(destination)).toEqual(fs.readFileSync(source));
  expect(fs.readFileSync(toml, "utf8")).toBe(original);
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
  const art = await byTestId(page, "sprite-strip").getAttribute("src");
  expect((await page.request.get(new URL(art, page.url()).toString())).status()).toBe(200);
  const saved = page.waitForResponse((r) => r.url().includes("/forge/sprites/save?") && r.request().method() === "POST");
  await byTestId(page, "save-footer").getByText("Save", { exact: true }).click();
  expect((await saved).status()).toBe(204);
  const disk = fs.readFileSync(toml, "utf8");
  expect(disk).toContain("frames = [0, 1]");
  expect(disk).toContain('entity_type = "TestDummy"');
  expect(disk).toContain("# The e2e project's first assets mod");
  await page.reload();
  await expect(byTestId(page, "sprite-strip")).toBeVisible();
});

test("wrong dimensions and a claimed destination refuse without a copy or draft", async ({ page }) => {
  fs.writeFileSync(invalid, await makePNG(page, 16, 32));
  await page.goto("/forge/sprites?import=1");
  await byTestId(page, "sprite-import-source").selectOption(invalid);
  await byTestId(page, "sprite-import-name").fill("e2e-bad");
  await byTestId(page, "sprite-import-submit").click();
  await expect(byTestId(page, "sprite-import-problem")).toContainText("one row");
  expect(fs.existsSync(invalidDestination)).toBe(false);
  expect(fs.readFileSync(toml, "utf8")).toBe(original);

  fs.mkdirSync(path.dirname(destination), { recursive: true });
  fs.writeFileSync(destination, "claimed art");
  await byTestId(page, "sprite-import-source").selectOption(source);
  await byTestId(page, "sprite-import-name").fill("e2e-claimed");
  await byTestId(page, "sprite-import-submit").click();
  await expect(byTestId(page, "sprite-import-problem")).toContainText("already exists");
  expect(fs.readFileSync(destination, "utf8")).toBe("claimed art");
  expect(fs.readFileSync(toml, "utf8")).toBe(original);
});

test("a guessed file outside the project is refused by the import action", async ({ page }) => {
  await page.goto("/forge/sprites?import=1");
  const response = await page.request.post(`/forge/sprites/import?file=${encodeURIComponent(toml)}`, {
    form: { source: "/etc/passwd", animation: "e2e-guessed" },
  });
  expect(response.status()).toBe(200); // follows the redirect back to the dialog
  await page.goto(response.url());
  await expect(byTestId(page, "sprite-import-problem")).toContainText("not a project-local PNG");
  expect(fs.existsSync(path.join(root, "assets/sprites/passwd"))).toBe(false);
  expect(fs.readFileSync(toml, "utf8")).toBe(original);
});

test("failed import retains the animation that was selected when the dialog opened", async ({ page }) => {
  fs.writeFileSync(invalid, await makePNG(page, 16, 32));
  await page.goto("/forge/sprites");
  await byTestId(page, "sprite-create-name").fill("e2e-second");
  await byTestId(page, "sprite-create-sheet").fill("sprites/not-imported-yet.png");
  await byTestId(page, "sprite-create-submit").click();
  await byTestId(page, "animation-e2e-second").click();
  await expect(page).toHaveURL(/animation=e2e-second/);
  await byTestId(page, "sprite-import-open").click();
  await byTestId(page, "sprite-import-source").selectOption(invalid);
  await byTestId(page, "sprite-import-name").fill("bad-import");
  await byTestId(page, "sprite-import-submit").click();
  await expect(byTestId(page, "sprite-import-problem")).toContainText("one row");
  await expect(page).toHaveURL(/animation=e2e-second/);
  await page.keyboard.press("Escape");
  await expect(byTestId(page, "sprites-selected-animation")).toContainText("e2e-second");
});

test.describe("accessibility", () => {
  test("the import dialog has named controls and Escape closes it", async ({ page }) => {
    await page.goto("/forge/sprites");
    await byTestId(page, "sprite-import-open").focus();
    await page.keyboard.press("Enter");
    await expect(byTestId(page, "sprite-import-dialog").locator('[role="dialog"]')).toHaveAttribute("aria-modal", "true");
    await expect(byTestId(page, "sprite-import-source")).toBeVisible();
    await expect(page.locator('label[for="sprite-import-entity-type"]')).toContainText("optional");
    await page.keyboard.press("Escape");
    await expect(byTestId(page, "sprite-import-dialog")).toHaveCount(0);
  });
});
