const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const active = path.resolve(__dirname, "../fixtures/project/assets/animations.toml");
const later = path.resolve(__dirname, "../fixtures/project/overlay/assets/animations.toml");

test("SPRT names only the first assets mod as game-loaded", async ({ page }) => {
  await page.goto("/forge/sprites");
  await expect(byTestId(page, "sprites-active-mod")).toContainText("e2e-core");
  await expect(byTestId(page, "sprites-later-e2e-overlay")).toContainText("not loaded");
  await expect(byTestId(page, "animation-e2e-idle")).toBeVisible();
  await expect(byTestId(page, "sprites-panel")).not.toContainText("not-loaded");
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  await byTestId(page, "animation-file").click();
  await expect(page).toHaveURL(/\/forge\/sprites\?file=.*animations\.toml/);
  await expect(byTestId(page, "sprites-file-path")).toHaveText(active);
  await byTestId(page, "rail-map").click();
  await byTestId(page, "rail-sprites").click();
  await expect(byTestId(page, "sprites-active-mod")).toContainText("e2e-core");
});

test("Reload takes an external TOML edit without pretending the draft hot-reloads", async ({ page, baseURL }) => {
  const original = fs.readFileSync(active, "utf8");
  try {
    await page.goto(`/forge/sprites?file=${encodeURIComponent(active)}`);
    await expect(byTestId(page, "animation-e2e-idle")).toBeVisible();
    fs.writeFileSync(active, original.replace('name = "e2e-idle"', 'name = "e2e-rest"'));
    page.once("dialog", (dialog) => dialog.accept());
    const posted = page.waitForResponse((r) => r.url().includes("/forge/sprites/reload") && r.request().method() === "POST");
    await byTestId(page, "save-footer-reload").click();
    expect((await posted).status()).toBe(204);
    await expect(byTestId(page, "animation-e2e-rest")).toBeVisible();
    await expect(byTestId(page, "animation-e2e-idle")).toHaveCount(0);
    await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
    await expect(byTestId(page, "sprites-panel")).toContainText("hot-reloads after Save");
  } finally {
    fs.writeFileSync(active, original);
    const response = await fetch(`${baseURL}/forge/sprites/reload?file=${encodeURIComponent(active)}`, { method: "POST" });
    if (response.status !== 204) throw new Error(`could not restore active TOML: ${response.status}`);
  }
});

test("a malformed active file and a guessed later-mod path are refused honestly", async ({ page, baseURL }) => {
  const original = fs.readFileSync(active, "utf8");
  const laterOriginal = fs.readFileSync(later, "utf8");
  try {
    await page.goto("/forge/sprites");
    const guessed = await page.request.post(`/forge/sprites/save?file=${encodeURIComponent(later)}`);
    expect(guessed.status()).toBe(204);
    await expect(byTestId(page, "sprites-edit-problem")).toContainText("not the active animation file");
    expect(fs.readFileSync(later, "utf8")).toBe(laterOriginal);
    fs.writeFileSync(active, "[[animation]\n");
    const reloaded = await page.request.post(`/forge/sprites/reload?file=${encodeURIComponent(active)}`);
    expect(reloaded.status()).toBe(204);
    await expect(byTestId(page, "sprites-file-problem")).toContainText("animations.toml");
    await expect(byTestId(page, "save-footer-reload")).toBeEnabled();
    await expect(byTestId(page, "save-footer").locator("button", { hasText: "Save" })).toHaveCount(0);
    fs.writeFileSync(active, original);
    page.once("dialog", (dialog) => dialog.accept());
    const restored = page.waitForResponse((r) => r.url().includes("/forge/sprites/reload") && r.request().method() === "POST");
    await byTestId(page, "save-footer-reload").click();
    expect((await restored).status()).toBe(204);
    await expect(byTestId(page, "animation-e2e-idle")).toBeVisible();
  } finally {
    fs.writeFileSync(active, original);
    const response = await fetch(`${baseURL}/forge/sprites/reload?file=${encodeURIComponent(active)}`, { method: "POST" });
    if (response.status !== 204) throw new Error(`could not restore active TOML: ${response.status}`);
  }
});

test("a deleted active file can be restored from the held working copy", async ({ page, baseURL }) => {
  const original = fs.readFileSync(active, "utf8");
  try {
    await page.goto("/forge/sprites");
    fs.rmSync(active);
    page.once("dialog", (dialog) => dialog.accept());
    const tried = page.waitForResponse((r) => r.url().includes("/forge/sprites/reload") && r.request().method() === "POST");
    await byTestId(page, "save-footer-reload").click();
    expect((await tried).status()).toBe(204);
    await expect(byTestId(page, "save-footer-restore")).toBeVisible();
    const restored = page.waitForResponse((r) => r.url().includes("/forge/sprites/save/overwrite") && r.request().method() === "POST");
    await byTestId(page, "save-footer-restore").click();
    expect((await restored).status()).toBe(204);
    expect(fs.readFileSync(active, "utf8")).toBe(original);
    await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  } finally {
    if (!fs.existsSync(active)) fs.writeFileSync(active, original);
    const response = await fetch(`${baseURL}/forge/sprites/reload?file=${encodeURIComponent(active)}`, { method: "POST" });
    if (response.status !== 204) throw new Error(`could not restore TOML: ${response.status}`);
  }
});

test.describe("accessibility", () => {
  test("the active file link and source list work by keyboard; later mods have no write action", async ({ page }) => {
    await page.goto("/forge/sprites");
    await expect(byTestId(page, "sprites-list")).toHaveAttribute("aria-label", "Animation asset sources");
    await byTestId(page, "animation-file").focus();
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/file=.*animations\.toml/);
    await expect(byTestId(page, "animation-file")).toHaveAttribute("aria-current", "true");
    await expect(byTestId(page, "save-footer-reload")).toBeEnabled();
    await expect(byTestId(page, "sprites-later-e2e-overlay").locator("button")).toHaveCount(0);
  });
});
