const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const toml = path.resolve(__dirname, "../fixtures/project/assets/animations.toml");
const image = path.resolve(__dirname, "../fixtures/project/assets/sprites/e2e-player.png");
const alternate = path.resolve(__dirname, "../fixtures/project/assets/sprites/e2e-alternate.png");
let original;

test.beforeEach(async ({ page }) => {
  original = fs.readFileSync(toml, "utf8");
  await page.goto("/forge/schema");
  const pixels = await page.evaluate(() => {
    const canvas = document.createElement("canvas");
    canvas.width = 32;
    canvas.height = 16;
    const ctx = canvas.getContext("2d");
    ctx.fillStyle = "#f00";
    ctx.fillRect(0, 0, 16, 16);
    ctx.fillStyle = "#00f";
    ctx.fillRect(16, 0, 16, 16);
    return canvas.toDataURL("image/png").split(",")[1];
  });
  fs.mkdirSync(path.dirname(image), { recursive: true });
  fs.writeFileSync(image, Buffer.from(pixels, "base64"));
});

test.afterEach(async ({ baseURL }) => {
  fs.writeFileSync(toml, original);
  fs.rmSync(image, { force: true });
  fs.rmSync(alternate, { force: true });
  const response = await fetch(`${baseURL}/forge/sprites/reload?file=${encodeURIComponent(toml)}`, { method: "POST" });
  if (response.status !== 204) throw new Error(`cannot restore animation fixture: ${response.status}`);
});

test("the selected strip serves real art and plays its column sequence", async ({ page }) => {
  await page.goto(`/forge/sprites?file=${encodeURIComponent(toml)}&animation=e2e-idle`);
  const art = byTestId(page, "sprite-strip");
  await expect(art).toBeVisible();
  const response = await page.request.get(new URL(await art.getAttribute("src"), page.url()).toString());
  expect(response.status()).toBe(200);
  expect(response.headers()["content-type"]).toContain("image/png");
  await expect(byTestId(page, "sprite-frames")).toContainText("column 0");
  await expect(byTestId(page, "sprite-playback")).toBeVisible();
  const selected = byTestId(page, "animation-e2e-idle");
  await expect(selected).toHaveAttribute("aria-current", "true");
  await byTestId(page, "animation-file").click();
  await page.goBack();
  await expect(byTestId(page, "sprites-selected-animation")).toContainText("e2e-idle");
});

test("editing ordered columns, rate and loop stays in the draft until Save", async ({ page }) => {
  await page.goto("/forge/sprites");
  const input = byTestId(page, "sprite-frame-input");
  const posted = page.waitForResponse((r) => r.url().includes("/forge/sprites/edit/frames") && r.request().method() === "POST");
  await input.fill("1,0,1");
  await input.dispatchEvent("change");
  expect((await posted).status()).toBe(204);
  await expect(byTestId(page, "sprite-frames")).toContainText("column 1");
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
  expect(fs.readFileSync(toml, "utf8")).toBe(original);
  await byTestId(page, "sprite-fps").fill("2");
  await byTestId(page, "sprite-fps").dispatchEvent("change");
  await byTestId(page, "sprite-loop").selectOption("false");
  const saved = page.waitForResponse((r) => r.url().includes("/forge/sprites/save?") && r.request().method() === "POST");
  await byTestId(page, "save-footer").getByText("Save", { exact: true }).click();
  expect((await saved).status()).toBe(204);
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  const disk = fs.readFileSync(toml, "utf8");
  expect(disk).toContain("frames = [1, 0, 1]");
  expect(disk).toContain("fps = 2");
  expect(disk).toContain("loop = false");
  expect(disk).toContain("# The e2e project's first assets mod");
});

test("the preview advances through columns and stops on the last when loop is off", async ({ page }) => {
  await page.goto("/forge/sprites");
  await byTestId(page, "sprite-fps").fill("2");
  await byTestId(page, "sprite-fps").dispatchEvent("change");
  await byTestId(page, "sprite-frame-input").fill("0,1");
  await byTestId(page, "sprite-frame-input").dispatchEvent("change");
  await byTestId(page, "sprite-loop").selectOption("false");
  const player = byTestId(page, "sprite-playback");
  await expect(player).toHaveCSS("background-position-x", "0px");
  await expect(player).toHaveCSS("background-position-x", "-64px", { timeout: 1500 });
  await page.waitForTimeout(650);
  await expect(player).toHaveCSS("background-position-x", "-64px");
  expect(fs.readFileSync(toml, "utf8")).toBe(original);
  page.once("dialog", (dialog) => dialog.accept());
  await byTestId(page, "save-footer-reload").click();
});

test("changing playback after it finishes starts the new sequence at its first frame", async ({ page }) => {
  await page.goto("/forge/sprites");
  await byTestId(page, "sprite-fps").fill("10");
  await byTestId(page, "sprite-fps").dispatchEvent("change");
  await byTestId(page, "sprite-frame-input").fill("0,1");
  await byTestId(page, "sprite-frame-input").dispatchEvent("change");
  await byTestId(page, "sprite-loop").selectOption("false");
  const player = byTestId(page, "sprite-playback");
  await expect(player).toHaveCSS("background-position-x", "-64px");
  await page.waitForTimeout(400);
  await byTestId(page, "sprite-frame-input").fill("1,0");
  await byTestId(page, "sprite-frame-input").dispatchEvent("change");
  await expect(player).toHaveAttribute("data-frames", "1, 0");
  await expect(player).toHaveCSS("background-position-x", "-64px");
  await expect(player).toHaveCSS("background-position-x", "0px");
  page.once("dialog", (dialog) => dialog.accept());
  await byTestId(page, "save-footer-reload").click();
});

test("changing the sheet restarts playback on its first frame", async ({ page }) => {
  fs.copyFileSync(image, alternate);
  await page.goto("/forge/sprites");
  await byTestId(page, "sprite-fps").fill("10");
  await byTestId(page, "sprite-fps").dispatchEvent("change");
  await byTestId(page, "sprite-frame-input").fill("0,1");
  await byTestId(page, "sprite-frame-input").dispatchEvent("change");
  await byTestId(page, "sprite-loop").selectOption("false");
  const player = byTestId(page, "sprite-playback");
  await expect(player).toHaveCSS("background-position-x", "-64px");
  await page.waitForTimeout(350);
  await byTestId(page, "sprite-sheet").fill("sprites/e2e-alternate.png");
  await byTestId(page, "sprite-sheet").dispatchEvent("change");
  await expect(player).toHaveAttribute("data-sheet", "sprites/e2e-alternate.png");
  await expect(player).toHaveCSS("background-position-x", "0px");
  page.once("dialog", (dialog) => dialog.accept());
  await byTestId(page, "save-footer-reload").click();
});

test("creating an animation and an entity binding changes the working file only", async ({ page }) => {
  await page.goto("/forge/sprites");
  await byTestId(page, "sprite-create-name").fill("e2e-walk");
  await byTestId(page, "sprite-create-sheet").fill("sprites/e2e-player.png");
  await byTestId(page, "sprite-create-frames").fill("1,0");
  const posted = page.waitForResponse((r) => r.url().includes("/forge/sprites/create/animation") && r.request().method() === "POST");
  await byTestId(page, "sprite-create-submit").click();
  expect((await posted).status()).toBe(204);
  await expect(byTestId(page, "animation-e2e-walk")).toBeVisible();
  await byTestId(page, "sprite-binding-name").fill("TestDummy");
  await byTestId(page, "sprite-binding-sheet").fill("sprites/e2e-player.png");
  const bound = page.waitForResponse((r) => r.url().includes("/forge/sprites/create/binding") && r.request().method() === "POST");
  await byTestId(page, "sprite-binding-create-submit").click();
  expect((await bound).status()).toBe(204);
  await expect(byTestId(page, "entity-sheet-TestDummy")).toBeVisible();
  expect(fs.readFileSync(toml, "utf8")).toBe(original);
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
  page.once("dialog", (dialog) => dialog.accept());
  await byTestId(page, "save-footer-reload").click();
});

test("a later-mod sheet path is refused without changing the working animation", async ({ page }) => {
  await page.goto("/forge/sprites");
  const outside = path.resolve(__dirname, "../fixtures/project/overlay/assets/sprites/guessed.png");
  const field = byTestId(page, "sprite-sheet");
  const posted = page.waitForResponse((r) => r.url().includes("/forge/sprites/edit/sheet") && r.request().method() === "POST");
  await field.fill(outside);
  await field.dispatchEvent("change");
  expect((await posted).status()).toBe(204);
  await expect(byTestId(page, "sprites-edit-problem")).toContainText("outside the selected sprites directory");
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  expect(fs.readFileSync(toml, "utf8")).toBe(original);
});

test.describe("accessibility", () => {
  test("sprite edit controls are labelled, keyboard reachable, and problems have text", async ({ page }) => {
    await page.goto("/forge/sprites");
    await expect(byTestId(page, "sprite-frame-input")).toHaveAttribute("type", "text");
    await byTestId(page, "sprite-fps").focus();
    await expect(byTestId(page, "sprite-fps")).toBeFocused();
    const fpsLabel = await page.locator('label[for="sprite-fps"]').textContent();
    expect(fpsLabel).toContain("Frames per second");
    await byTestId(page, "sprite-frame-input").fill("99");
    await byTestId(page, "sprite-frame-input").dispatchEvent("change");
    await expect(byTestId(page, "sprites-edit-problem")).toContainText("column 99");
    await expect(byTestId(page, "sprites-edit-problem")).toHaveAttribute("role", "alert");
    await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  });
});
