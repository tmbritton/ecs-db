// Epic 12 Story 5 — ENTS mode.
//
// The fixture project has TestDummy and TestGoblin, and TestGoblin binds
// e2e-wander with an `hp` context key — so the seeds panel has real data rather
// than a stub to render.

const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const SCHEMA = path.resolve(__dirname, "../fixtures/project/schema.json");

test.describe.configure({ mode: "serial" });

let original;
test.beforeAll(() => {
  original = fs.readFileSync(SCHEMA, "utf8");
});

test.afterAll(() => {
  if (original !== undefined) fs.writeFileSync(SCHEMA, original);
});

test.afterEach(async ({ baseURL }) => {
  fs.writeFileSync(SCHEMA, original);
  const resp = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`could not reset the session: ${resp.status}`);
});

const footer = (page) => byTestId(page, "save-footer");
const saveButton = (page) =>
  page.locator('[data-testid="save-footer"] button', { hasText: /save/i });

async function openType(page, type) {
  await page.goto(`/forge/ents?type=${type}`);
  await expect(byTestId(page, "type-editor")).toBeVisible();
}

test("the type list is in authored order, not alphabetical", async ({ page }) => {
  await page.goto("/forge/ents");

  const names = await page.locator('[data-testid^="type-"] .list-row__label').allTextContents();
  // The fixture lists TestGoblin first, which is deliberately not alphabetical:
  // with the two the other way round, sorting the list and preserving it look
  // identical and this assertion proves nothing.
  expect(names).toEqual(["TestGoblin", "TestDummy"]);
  expect([...names].sort()).not.toEqual(names);
});

test("selecting a type is a URL that survives a reload", async ({ page }) => {
  await openType(page, "TestGoblin");
  await expect(byTestId(page, "type-editor")).toHaveAttribute("data-type", "TestGoblin");

  await page.reload();
  await expect(byTestId(page, "type-editor")).toHaveAttribute("data-type", "TestGoblin");
});

test("a bound machine is shown as the type's behaviour", async ({ page }) => {
  await openType(page, "TestGoblin");

  // The dropdown is populated from the project's resolved machines. It was
  // empty in the running app until this story wired them through, which no Go
  // test could see because they all pass their own fixture.
  // Identified by an option it must contain, not by position: `.first()` is
  // whichever select happens to come first, and would silently retarget if a
  // control were added above it.
  const behaviour = page
    .locator('[data-testid="type-editor"] select')
    .filter({ has: page.locator('option[value="e2e-wander"]') });
  await expect(behaviour).toHaveValue("e2e-wander");

  // Asserted as a *resolved* machine, not merely as an option with that value.
  // A binding whose machine did not resolve is still offered — deliberately,
  // so the dropdown does not silently retarget — and it is labelled "missing".
  // Without that distinction this passes with no machines wired at all, which
  // is exactly the state the app shipped in.
  const labels = await behaviour.locator("option").allTextContents();
  expect(labels).toContain("e2e-wander");
  expect(labels.join("|")).not.toContain("missing");
  expect(labels).toContain("none");
});

test("the context seeds panel shows the machine's context, sourced and read-only", async ({ page }) => {
  await openType(page, "TestGoblin");

  const seeds = byTestId(page, "context-seeds");
  await expect(seeds).toBeVisible();
  await expect(byTestId(page, "seed-hp")).toBeVisible();
  await expect(seeds).toContainText("edit in AGENTS");

  // Absent, not merely unstyled: "looks read-only" and "is read-only" differ,
  // and only one of them survives a stylesheet change.
  expect(await seeds.locator("input, select, button, textarea").count()).toBe(0);
});

test("a type with no machine says so rather than rendering an empty panel", async ({ page }) => {
  await openType(page, "TestDummy");

  await expect(byTestId(page, "seeds-none")).toBeVisible();
  await expect(byTestId(page, "seeds-table")).toHaveCount(0);
});

test("adding an optional component marks the session dirty and shows the chip", async ({ page }) => {
  await openType(page, "TestGoblin");
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/);

  // TestGoblin requires Position and Health; E2EProbe is the one left.
  await byTestId(page, "add-optional").locator("select").selectOption("E2EProbe");

  await expect(byTestId(page, "optional-E2EProbe")).toBeVisible({ timeout: 10_000 });
  await expect(footer(page)).toHaveClass(/save-footer--dirty/, { timeout: 10_000 });
});

test("a component already on the type is not offered again", async ({ page }) => {
  await openType(page, "TestGoblin");

  const options = await byTestId(page, "add-required").locator("option").allTextContents();
  expect(options).not.toContain("Position"); // already required
  expect(options).not.toContain("Health"); // already required
  expect(options).toContain("E2EProbe");
});

test("a required chip cannot be detached — the lock is not decorative", async ({ page }) => {
  await openType(page, "TestGoblin");

  const required = byTestId(page, "required-Position");
  await expect(required).toBeVisible();
  expect(await required.locator(".chip__remove").count()).toBe(0);
  await expect(required.locator(".chip__lock")).toBeVisible();

  // The way out is demotion, or the contract is a dead end.
  await expect(byTestId(page, "demote-Position")).toBeVisible();
});

test("demoting a required component moves it to optional", async ({ page }) => {
  await openType(page, "TestGoblin");

  await byTestId(page, "demote-Position").click();

  await expect(byTestId(page, "optional-Position")).toBeVisible({ timeout: 10_000 });
  await expect(byTestId(page, "required-Position")).toHaveCount(0);
});

test("the validation level states its consequence and can be changed", async ({ page }) => {
  await openType(page, "TestGoblin");
  await expect(byTestId(page, "validation-consequence")).toContainText(/refused/);

  await page
    .locator('[data-testid="type-editor"] select')
    .filter({ has: page.locator('option[value="warning"]') })
    .selectOption("warning");

  await expect(byTestId(page, "validation-consequence")).toContainText(/logged/, { timeout: 10_000 });
});

test("saving writes the change and the file still loads", async ({ page }) => {
  await openType(page, "TestGoblin");
  await byTestId(page, "add-optional").locator("select").selectOption("E2EProbe");
  await expect(footer(page)).toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  await saveButton(page).click();
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  const raw = fs.readFileSync(SCHEMA, "utf8");
  expect(JSON.parse(raw).entityTypes.TestGoblin.optionalComponents).toContain("E2EProbe");
  // The engine's format uses [] rather than null for an empty list. Asserted
  // on the bytes, because every type in the file has a non-empty list by the
  // time this test has run — so a per-type check could not see the regression.
  expect(raw).not.toContain("null");
});

test("a new type serialises with empty arrays, not nulls", async ({ page }) => {
  await page.goto("/forge/ents");
  await byTestId(page, "add-type").click();
  await expect(byTestId(page, "type-NewType")).toBeVisible({ timeout: 10_000 });

  await saveButton(page).click();
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  const raw = fs.readFileSync(SCHEMA, "utf8");
  const onDisk = JSON.parse(raw);
  expect(onDisk.entityTypes.NewType.requiredComponents).toEqual([]);
  expect(onDisk.entityTypes.NewType.optionalComponents).toEqual([]);
  expect(raw).not.toContain("null");
});

test("accessibility", async ({ page }) => {
  await openType(page, "TestGoblin");

  // The rail's landmark plus this mode's own list.
  await expect(page.getByRole("navigation", { name: /entity types/i })).toBeVisible();

  // A required chip announces that it is required rather than showing a glyph
  // with no name.
  await expect(byTestId(page, "required-Position").locator('[role="img"]')).toHaveAttribute(
    "aria-label",
    /required/i,
  );

  // The move controls are named, not bare arrows.
  await expect(byTestId(page, "demote-Position")).toHaveAttribute("aria-label", /optional/i);
});
