// Epic 13 Story 2 — the machine editing session.
//
// Many files rather than one, and they can be created, renamed and deleted
// while Forge is running. What only a browser can show here is that the
// controls are actually wired, that a save writes the file the page says it
// wrote, and that a machine created in AGENTS is bindable in ENTS without a
// restart — the staleness Epic 12 Story 7 documented and deferred to here.

const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const BEHAVIORS = path.resolve(__dirname, "../fixtures/project/behaviors");
// Machines resolve sorted by id, so e2e-guard is what an unqualified
// /forge/agents selects. Every test that means e2e-wander says so.
const WANDER = "e2e-wander";
const GUARD = "e2e-guard";
const WANDER_FILE = path.join(BEHAVIORS, "e2e-wander.json");
const GUARD_FILE = path.join(BEHAVIORS, "e2e-guard.json");
const SCHEMA = path.resolve(__dirname, "../fixtures/project/schema.json");

test.describe.configure({ mode: "serial" });

let original;
let originalSchema;
test.beforeAll(() => {
  original = {};
  for (const name of fs.readdirSync(BEHAVIORS)) {
    original[name] = fs.readFileSync(path.join(BEHAVIORS, name), "utf8");
  }
  originalSchema = fs.readFileSync(SCHEMA, "utf8");
});

function restoreFiles() {
  for (const name of fs.readdirSync(BEHAVIORS)) {
    if (!(name in original)) fs.unlinkSync(path.join(BEHAVIORS, name));
  }
  for (const [name, body] of Object.entries(original)) {
    fs.writeFileSync(path.join(BEHAVIORS, name), body);
  }
  fs.writeFileSync(SCHEMA, originalSchema);
}

test.afterAll(restoreFiles);

test.afterEach(async ({ baseURL }) => {
  restoreFiles();
  // Re-read the directory rather than discarding: the files have just been
  // put back by hand, so what Forge holds is stale in both directions — an
  // edit it has not saved, and a set that may name a file that no longer
  // exists. Discard restores each file's own snapshot, which is what Forge last
  // read, not what is there now.
  const resp = await fetch(`${baseURL}/forge/agents/reload`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`could not reset the machines: ${resp.status}`);
  await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
});

const footer = (page) => byTestId(page, "save-footer");
const saveButton = (page) =>
  page.locator('[data-testid="save-footer"] button', { hasText: /^save$/i });

// Selects by clicking the list, not by building the ?machine= path by hand.
// The path is absolute and belongs to the server's view of the project, so
// reconstructing it here is both fragile and not what a user does — the first
// version of this silently selected the wrong machine and the assertions were
// about a file nobody had edited.
async function openAgents(page, id) {
  await page.goto("/forge/agents");
  await expect(byTestId(page, "agents-mode")).toBeVisible();
  if (!id) return;
  await byTestId(page, `machine-${id}`).click();
  await expect(byTestId(page, "machine-editor")).toHaveAttribute("data-machine", id, {
    timeout: 10_000,
  });
}

test("the machine list shows every machine the project resolved", async ({ page }) => {
  await openAgents(page);
  await expect(byTestId(page, "machine-e2e-wander")).toBeVisible();
  await expect(byTestId(page, "machine-e2e-guard")).toBeVisible();
});

test("editing a machine marks the session dirty and names the file", async ({ page }) => {
  await openAgents(page, WANDER);
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/);

  const id = byTestId(page, "machine-rename-id");
  await id.fill("e2e-roam");
  await id.press("Enter");

  await expect(footer(page)).toHaveClass(/save-footer--dirty/, { timeout: 10_000 });
  await expect(byTestId(page, "save-footer-file")).toContainText("e2e-wander.json");
  // And the list marks which one.
  await expect(byTestId(page, "machine-e2e-roam")).toHaveAttribute("data-dirty", "true");
});

test("saving writes only the machine that changed", async ({ page }) => {
  const otherPath = GUARD_FILE;
  const before = fs.statSync(otherPath);
  await new Promise((r) => setTimeout(r, 20));

  await openAgents(page, WANDER);
  const id = byTestId(page, "machine-rename-id");
  await id.fill("e2e-roam");
  await id.press("Enter");
  await expect(footer(page)).toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  await saveButton(page).click();
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  expect(fs.readFileSync(WANDER_FILE, "utf8")).toContain('"id": "e2e-roam"');
  // Byte-stable emission means an identical rewrite would pass a bytes check,
  // so the untouched machine is checked by mtime as well.
  const after = fs.statSync(otherPath);
  expect(after.mtimeMs).toBe(before.mtimeMs);
});

test("discard restores the working value without touching disk", async ({ page }) => {
  const filePath = WANDER_FILE;
  const before = fs.readFileSync(filePath, "utf8");

  await openAgents(page, WANDER);
  const id = byTestId(page, "machine-rename-id");
  await id.fill("e2e-roam");
  await id.press("Enter");
  await expect(footer(page)).toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  page.once("dialog", (d) => d.accept());
  await page.locator('[data-testid="save-footer"] button', { hasText: /discard/i }).click();

  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/, { timeout: 10_000 });
  expect(fs.readFileSync(filePath, "utf8")).toBe(before);
});

test("creating a machine writes a file and lists it", async ({ page }) => {
  await openAgents(page);
  await byTestId(page, "add-machine").click();

  await expect(byTestId(page, "machine-NewMachine")).toBeVisible({ timeout: 10_000 });
  expect(fs.existsSync(path.join(BEHAVIORS, "NewMachine.json"))).toBe(true);
  // It arrives clean: a file that is unsaved the moment it exists would show as
  // work nobody did.
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/);
});

test("a machine created in AGENTS is bindable in ENTS at once", async ({ page }) => {
  await openAgents(page);
  await byTestId(page, "add-machine").click();
  await expect(byTestId(page, "machine-NewMachine")).toBeVisible({ timeout: 10_000 });

  await page.goto("/forge/ents?type=TestGoblin");
  const select = page.locator('[data-testid="type-editor"] select').first();
  await expect(select.locator('option[value="NewMachine"]')).toHaveCount(1);
});

test("the two renames are separate controls with separate results", async ({ page }) => {
  await openAgents(page, WANDER);

  // The id lives inside the file and is what bindings resolve through.
  const id = byTestId(page, "machine-rename-id");
  await id.fill("e2e-roam");
  await id.press("Enter");
  await expect(byTestId(page, "machine-e2e-roam")).toBeVisible({ timeout: 10_000 });
  await saveButton(page).click();
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  // The file has not moved: nothing resolves through a filename.
  expect(fs.existsSync(WANDER_FILE)).toBe(true);
  expect(fs.readFileSync(WANDER_FILE, "utf8")).toContain('"id": "e2e-roam"');

  // Renaming the file moves it and leaves the id alone.
  const file = byTestId(page, "machine-rename-file");
  await file.fill("e2e-roam");
  await file.press("Enter");
  await expect(byTestId(page, "machine-editor")).toHaveAttribute("data-machine", "e2e-roam", {
    timeout: 10_000,
  });
  expect(fs.existsSync(path.join(BEHAVIORS, "e2e-roam.json"))).toBe(true);
  expect(fs.existsSync(WANDER_FILE)).toBe(false);
  expect(fs.readFileSync(path.join(BEHAVIORS, "e2e-roam.json"), "utf8")).toContain('"id": "e2e-roam"');
});

test("renaming an id says what it will break before it breaks", async ({ page }) => {
  // TestGoblin binds e2e-wander in the fixture schema.
  await openAgents(page, WANDER);
  await expect(byTestId(page, "rename-id-note")).toContainText("TestGoblin");

  // And a machine nothing binds says so instead.
  await byTestId(page, "machine-e2e-guard").click();
  await expect(byTestId(page, "rename-id-note")).toContainText(/breaks nothing/i);
});

test("deleting asks first, and cancelling leaves the file", async ({ page }) => {
  await openAgents(page, GUARD);

  page.once("dialog", async (d) => {
    expect(d.message()).toContain("e2e-guard");
    await d.dismiss();
  });
  await byTestId(page, "delete-machine").click();
  await page.waitForTimeout(300);
  expect(fs.existsSync(GUARD_FILE)).toBe(true);

  page.once("dialog", (d) => d.accept());
  await byTestId(page, "delete-machine").click();
  await expect(byTestId(page, "machine-e2e-guard")).toHaveCount(0, { timeout: 10_000 });
  expect(fs.existsSync(GUARD_FILE)).toBe(false);
});

// Every machine gets its own verdict, so a save over several reports several
// times rather than once for the set.
//
// The *refusal* half of that — a machine that will not validate being turned
// down while its neighbours are written — has no browser test, and deliberately
// so: with the canvas still to come in Stories 4 and 5, there is no control in
// AGENTS that can put a machine into an invalid state, and a test that reached
// past the UI to manufacture one would be testing the Go code through a browser.
// internal/forge/machines covers it directly.
test("a save over several machines reports each of them", async ({ page }) => {
  await openAgents(page);
  await byTestId(page, "add-machine").click();
  await expect(byTestId(page, "machine-NewMachine")).toBeVisible({ timeout: 10_000 });

  for (const id of ["e2e-wander", "NewMachine"]) {
    await byTestId(page, `machine-${id}`).click();
    await expect(byTestId(page, "machine-editor")).toHaveAttribute("data-machine", /.+/);
    const initial = byTestId(page, "machine-rename-id");
    await initial.fill(`${id}-renamed`);
    await initial.press("Enter");
    await expect(byTestId(page, `machine-${id}-renamed`)).toBeVisible({ timeout: 10_000 });
  }
  await expect(byTestId(page, "save-footer-file")).toContainText("2 unsaved machines");

  await saveButton(page).click();
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  // Both files, both renamed — one Save, two writes.
  expect(fs.readFileSync(WANDER_FILE, "utf8")).toContain('"id": "e2e-wander-renamed"');
  expect(fs.readFileSync(path.join(BEHAVIORS, "NewMachine.json"), "utf8")).toContain(
    '"id": "NewMachine-renamed"'
  );
});
