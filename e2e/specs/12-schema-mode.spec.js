// Epic 12 Story 2 — SCHEMA mode.
//
// The end-to-end assertion that matters here is the last one: after a save, the
// file on disk contains the change and *only* the change. That is Epic 11
// Story 2's diff-stability property, exercised through the UI for the first
// time.

const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const SCHEMA = path.resolve(__dirname, "../fixtures/project/schema.json");

test.describe.configure({ mode: "serial" });

let original;
test.beforeAll(() => {
  original = fs.readFileSync(SCHEMA, "utf8");
});
test.afterEach(async ({ baseURL }) => {
  fs.writeFileSync(SCHEMA, original);
  const resp = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`could not reset the session: ${resp.status}`);
});
test.afterAll(() => {
  if (original !== undefined) fs.writeFileSync(SCHEMA, original);
});

// The fixture authors Position, Health, E2EProbe — not alphabetical, which is
// the point.
test("the component list is in authored order, not alphabetical", async ({ page }) => {
  await page.goto("/forge/schema");
  const names = await page
    .locator('[data-testid="component-list"] .list-row__label')
    .allTextContents();
  expect(names).toEqual(["Position", "Health", "E2EProbe"]);
  // Alphabetical would put E2EProbe first.
  expect(names[0]).not.toBe("E2EProbe");
});

test("selecting a component is a URL that survives a reload", async ({ page }) => {
  await page.goto("/forge/schema");
  await byTestId(page, "component-Health").click();
  await expect(page).toHaveURL("/forge/schema?component=Health");
  await expect(byTestId(page, "component-name")).toHaveText("Health");

  await page.reload();
  await expect(byTestId(page, "component-name")).toHaveText("Health");
});

test("a non-object component has no fields table", async ({ page }) => {
  await page.goto("/forge/schema?component=Position");
  await expect(byTestId(page, "fields-table")).toBeVisible();

  // Switch the shape and the table goes.
  await page.locator('[data-testid="component-editor"] select').first().selectOption("string");
  await expect(byTestId(page, "no-fields")).toBeVisible({ timeout: 10_000 });
  await expect(page.locator('[data-testid="fields-table"]')).toHaveCount(0);
});

test("adding a field marks the session dirty and shows the field", async ({ page }) => {
  await page.goto("/forge/schema?component=Position");
  await byTestId(page, "add-field").click();

  await expect(byTestId(page, "field-newField")).toBeVisible({ timeout: 10_000 });
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
});

test("a component cannot be named Behavior", async ({ page }) => {
  await page.goto("/forge/schema?component=Position");
  const rename = byTestId(page, "component-rename");
  await rename.fill("Behavior");
  await rename.press("Enter");

  // Refused, and the reason is on the page rather than only in a log — a
  // control that silently does nothing teaches you it is broken.
  await expect(byTestId(page, "edit-problem")).toContainText("reserved", { timeout: 10_000 });
  await expect(page.locator('[data-testid="component-Behavior"]')).toHaveCount(0);
  await expect(byTestId(page, "component-name")).toHaveText("Position");

  // And a legal rename clears it.
  await rename.fill("Placement");
  await rename.press("Enter");
  await expect(byTestId(page, "component-name")).toHaveText("Placement", { timeout: 10_000 });
  await expect(page.locator('[data-testid="edit-problem"]')).toHaveCount(0);
});

test("deleting a component asks first, and cancelling changes nothing", async ({ page }) => {
  await page.goto("/forge/schema?component=Health");

  let asked = 0;
  page.once("dialog", (d) => {
    asked++;
    d.dismiss();
  });
  await byTestId(page, "delete-component").click();
  await page.waitForTimeout(400);

  expect(asked, "delete did not ask before dropping a table").toBe(1);
  await expect(byTestId(page, "component-Health")).toBeVisible();
});

test("the used-by panel lists the types that declare a component", async ({ page }) => {
  await page.goto("/forge/schema?component=Position");
  // The fixture requires Position on both entity types.
  await expect(byTestId(page, "used-by-TestDummy")).toBeVisible();
  await expect(byTestId(page, "used-by-TestGoblin")).toBeVisible();
  await expect(page.locator('[data-testid="used-by-none"]')).toHaveCount(0);
});

// The state that makes deletion safe is worth stating rather than leaving
// blank. A freshly added component is used by nothing.
test("the used-by panel says when a component is used by nothing", async ({ page }) => {
  await page.goto("/forge/schema");
  await byTestId(page, "add-component").click();
  await expect(byTestId(page, "component-NewComponent")).toBeVisible({ timeout: 10_000 });

  await byTestId(page, "component-NewComponent").click();
  await expect(byTestId(page, "used-by-none")).toBeVisible();
});

// Renaming a component that is not the first one must keep the editor on it.
// The page subscribes to its stream naming the component it shows, and that URL
// cannot change — so before the fix the editor silently swapped to whichever
// component sorted first, and the next edit hit that one instead.
test("renaming a non-first component keeps the editor on it", async ({ page }) => {
  await page.goto("/forge/schema?component=Health");
  await expect(byTestId(page, "component-name")).toHaveText("Health");

  const rename = byTestId(page, "component-rename");
  await rename.fill("Vitality");
  await rename.press("Enter");

  await expect(byTestId(page, "component-name")).toHaveText("Vitality", { timeout: 10_000 });
  // Not Position, which is what the fallback would have selected.
  await expect(byTestId(page, "component-name")).not.toHaveText("Position");
});

// The property Epic 11 Story 2 exists for, exercised through the UI: a save
// touches the lines that changed and nothing else.
test("saving writes only the change", async ({ page }) => {
  await page.goto("/forge/schema?component=Position");
  await byTestId(page, "add-field").click();
  await expect(byTestId(page, "field-newField")).toBeVisible({ timeout: 10_000 });

  await page.locator('[data-testid="save-footer"] button', { hasText: /save/i }).click();
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/, {
    timeout: 10_000,
  });

  const after = fs.readFileSync(SCHEMA, "utf8");

  // The field arrived.
  expect(after).toContain('"newField"');

  // And the change is confined to the component that was edited. Adding a
  // longer key re-pads its own properties block — the alignment rule doing
  // exactly what a person aligning by hand would do — but nothing outside
  // Position may move, which is the property Epic 11 Story 2 exists for.
  const outside = (text) =>
    text
      .split("\n")
      .filter((l) => !/"(x|y|newField)":/.test(l))
      .join("\n");
  expect(outside(after)).toBe(outside(original));

  // Components are still in authored order, not alphabetised by the save.
  const order = [...after.matchAll(/^    "(\w+)": \{$/gm)].map((m) => m[1]);
  expect(order).toEqual(["Position", "Health", "E2EProbe", "TestDummy", "TestGoblin"]);

  // And it still loads.
  expect(() => JSON.parse(after)).not.toThrow();
});

test("accessibility", async ({ page }) => {
  await page.goto("/forge/schema?component=Position");

  // The list is a labelled navigation region of real links.
  await expect(byTestId(page, "component-list")).toHaveRole("navigation");
  await expect(byTestId(page, "component-list")).toHaveAccessibleName("Components");

  // The fields table is a table, so its columns are announced.
  await expect(byTestId(page, "fields-table")).toHaveRole("table");

  // Every control in a field row names the field it acts on, or a screen
  // reader hears a column of identical "delete" buttons.
  await expect(byTestId(page, "delete-field-x")).toHaveAccessibleName("Delete field x");
  await expect(page.locator('[data-testid="field-x"] input')).toHaveAccessibleName(
    "Name of field x"
  );
});
