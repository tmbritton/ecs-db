// Epic 12 Story 4 — migration framing.
//
// The panel says what the engine will do to the database on its next start,
// and a save that would destroy data has to be confirmed first. The assertion
// that matters most is not on the dialog: it is that cancelling leaves
// schema.json byte-identical on disk.

const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const SCHEMA = path.resolve(__dirname, "../fixtures/project/schema.json");
const DB = path.resolve(__dirname, "../fixtures/project/e2e.db");
const STASH = `${DB}.stash`;

// These edit the fixture schema and move the fixture database, so they run one
// at a time and always put both back.
test.describe.configure({ mode: "serial" });

let original;
test.beforeAll(() => {
  original = fs.readFileSync(SCHEMA, "utf8");
});

test.afterAll(() => {
  if (original !== undefined) fs.writeFileSync(SCHEMA, original);
});

test.afterEach(async ({ baseURL }) => {
  restoreDatabase();
  fs.writeFileSync(SCHEMA, original);
  const resp = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`could not reset the session: ${resp.status}`);
});

function hideDatabase() {
  fs.renameSync(DB, STASH);
  for (const p of [`${DB}-wal`, `${DB}-shm`]) {
    if (fs.existsSync(p)) fs.renameSync(p, `${p}.stash`);
  }
}

function restoreDatabase() {
  if (fs.existsSync(STASH)) fs.renameSync(STASH, DB);
  for (const p of [`${DB}-wal`, `${DB}-shm`]) {
    if (fs.existsSync(`${p}.stash`)) fs.renameSync(`${p}.stash`, p);
  }
}

const panel = (page) => byTestId(page, "migration-panel");
const statements = (page) => page.locator('[data-testid="migration-statement"]');
const confirm = (page) => byTestId(page, "migration-confirm");
const saveButton = (page) =>
  page.locator('[data-testid="save-footer"] button', { hasText: /save/i });

// Opens SCHEMA mode on a component and waits for the panel, which is rendered
// server-side rather than arriving on the stream.
async function openSchema(page, component = "Health") {
  await page.goto(`/forge/schema?component=${component}`);
  await expect(panel(page)).toBeVisible();
}

test("with the database in step, the panel says nothing is pending", async ({ page }) => {
  await openSchema(page);

  await expect(byTestId(page, "migration-none")).toBeVisible();
  await expect(byTestId(page, "migration-unavailable")).toHaveCount(0);
  await expect(statements(page)).toHaveCount(0);
});

test("adding a component shows an additive change and no destructive mark", async ({ page }) => {
  await openSchema(page);
  await byTestId(page, "add-component").click();

  await expect(statements(page).first()).toBeVisible({ timeout: 10_000 });
  await expect(page.locator('[data-testid="migration-statement"][data-destructive="true"]')).toHaveCount(0);
  await expect(byTestId(page, "destructive-badge")).toHaveCount(0);
  await expect(statements(page).first()).toContainText(/CREATE TABLE/i);
});

test("deleting a field shows a destructive change, marked", async ({ page }) => {
  await openSchema(page);
  page.once("dialog", (d) => d.accept());
  await byTestId(page, "delete-field-maxHp").click();

  const destructive = page.locator('[data-testid="migration-statement"][data-destructive="true"]');
  await expect(destructive.first()).toBeVisible({ timeout: 10_000 });
  await expect(byTestId(page, "destructive-badge").first()).toBeVisible();
  // The statements themselves, not a count.
  await expect(destructive.first()).toContainText(/comp_health/i);
});

test("saving a destructive change asks first, and writes nothing until answered", async ({ page }) => {
  await openSchema(page);
  const before = fs.readFileSync(SCHEMA, "utf8");

  page.once("dialog", (d) => d.accept());
  await byTestId(page, "delete-field-maxHp").click();
  await expect(saveButton(page)).toBeEnabled({ timeout: 10_000 });

  await saveButton(page).click();
  await expect(confirm(page)).toBeVisible({ timeout: 10_000 });

  // It names what is being lost.
  await expect(confirm(page)).toContainText(/comp_health/i);
  await expect(page.locator('[data-testid="confirm-statement"]').first()).toBeVisible();

  // The file, not the UI: nothing has been written while the question stands.
  expect(fs.readFileSync(SCHEMA, "utf8")).toBe(before);
});

test("cancelling writes nothing and keeps the edit", async ({ page }) => {
  await openSchema(page);
  const before = fs.readFileSync(SCHEMA, "utf8");

  page.once("dialog", (d) => d.accept());
  await byTestId(page, "delete-field-maxHp").click();
  await expect(saveButton(page)).toBeEnabled({ timeout: 10_000 });
  await saveButton(page).click();
  await expect(confirm(page)).toBeVisible({ timeout: 10_000 });

  await byTestId(page, "confirm-cancel").click();
  await expect(confirm(page)).toHaveCount(0, { timeout: 10_000 });

  // This is the assertion the story is written for.
  expect(fs.readFileSync(SCHEMA, "utf8")).toBe(before);
  // Cancelling declined the save, not the work.
  await expect(byTestId(page, "field-maxHp")).toHaveCount(0);
});

// The save footer is in the shell, so Save can be pressed from any mode. A
// held save that renders nowhere is a button that silently does nothing.
test("a save held from another mode still shows its confirmation there", async ({ page }) => {
  await openSchema(page);
  page.once("dialog", (d) => d.accept());
  await byTestId(page, "delete-field-maxHp").click();
  await expect(saveButton(page)).toBeEnabled({ timeout: 10_000 });

  await page.goto("/forge/ents");
  await expect(saveButton(page)).toBeEnabled({ timeout: 10_000 });
  await saveButton(page).click();

  await expect(confirm(page)).toBeVisible({ timeout: 10_000 });
  await expect(confirm(page)).toContainText(/comp_health/i);
});

test("the dialog is a labelled modal, dismissable by keyboard", async ({ page }) => {
  await openSchema(page);
  const before = fs.readFileSync(SCHEMA, "utf8");

  page.once("dialog", (d) => d.accept());
  await byTestId(page, "delete-field-maxHp").click();
  await expect(saveButton(page)).toBeEnabled({ timeout: 10_000 });
  await saveButton(page).click();
  await expect(confirm(page)).toBeVisible({ timeout: 10_000 });

  const dialog = page.locator('[role="dialog"][aria-modal="true"]');
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveAttribute("aria-label", /destroys data/i);

  await page.keyboard.press("Escape");
  await expect(confirm(page)).toHaveCount(0, { timeout: 10_000 });
  expect(fs.readFileSync(SCHEMA, "utf8")).toBe(before);
});

test("confirming saves", async ({ page }) => {
  await openSchema(page);

  page.once("dialog", (d) => d.accept());
  await byTestId(page, "delete-field-maxHp").click();
  await expect(saveButton(page)).toBeEnabled({ timeout: 10_000 });
  await saveButton(page).click();
  await expect(confirm(page)).toBeVisible({ timeout: 10_000 });

  await byTestId(page, "confirm-save").click();
  await expect(confirm(page)).toHaveCount(0, { timeout: 10_000 });

  await expect
    .poll(() => JSON.parse(fs.readFileSync(SCHEMA, "utf8")).components.Health.properties.maxHp, {
      timeout: 10_000,
    })
    .toBeUndefined();
});

test("with no database the panel says so rather than showing an empty list", async ({ page }) => {
  hideDatabase();
  await openSchema(page);

  await expect(byTestId(page, "migration-unavailable")).toBeVisible({ timeout: 10_000 });
  await expect(byTestId(page, "migration-none")).toHaveCount(0);
  await expect(statements(page)).toHaveCount(0);
});

// The engine migrates on structure, not only on a version bump — Story 11
// opened that gate. The panel used to carry a warning that these statements
// would be skipped until schemaVersion moved, and to stop listing them as
// pending; both are gone, so the list reads the same at either version.
//
// Deliberately not "the migration-inert warning is absent": that testid is not
// in any template any more, so an assertion about it would pass whatever the
// panel did. What is checked is the list that is still there.
test("the panel lists pending statements with or without a version bump", async ({ page }) => {
  await openSchema(page);
  await byTestId(page, "add-component").click();
  await expect(statements(page).first()).toBeVisible({ timeout: 10_000 });

  const before = await statements(page).count();
  await expect(byTestId(page, "migration-none")).toHaveCount(0);

  await byTestId(page, "schema-version").click();
  await expect(statements(page).first()).toBeVisible({ timeout: 10_000 });
  await expect(statements(page)).toHaveCount(before);
  await expect(byTestId(page, "migration-none")).toHaveCount(0);
});
