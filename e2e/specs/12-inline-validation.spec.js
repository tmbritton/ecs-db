// Epic 12 Story 7 — inline validation.
//
// The claim under test is that Forge says what is wrong *while* it is being
// typed, using the engine's own checks. Two halves of that are invisible to Go:
// whether the message actually appears without a reload, and whether the Save
// button is genuinely unclickable rather than merely styled as though it were.
//
// The fixture's e2e-wander machine seeds the context key `hp`, and only Health
// declares an `hp` field — so a second `hp` anywhere is the ambiguity the
// warning exists to predict, and it is two clicks away.

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

const saveButton = (page) =>
  page.locator('[data-testid="save-footer"] button', { hasText: /^save$/i });

// takeFromDisk writes a schema.json and makes Forge adopt it, which is how a
// state the editor's own guards prevent is reached: someone edited the file in
// another window. It is also the suite's existing reset path, so it is not a
// mechanism invented for this spec.
async function takeFromDisk(page, baseURL, mutate) {
  const doc = JSON.parse(original);
  mutate(doc);
  fs.writeFileSync(SCHEMA, JSON.stringify(doc, null, 2) + "\n");
  const resp = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
  expect(resp.status).toBe(204);
}

test("a component cannot be named Behavior, and the editor says so as you type", async ({
  page,
}) => {
  await page.goto("/forge/schema?component=Position");
  const rename = byTestId(page, "component-rename");
  await rename.fill("Behavior");
  await rename.press("Enter");

  // Refused at the edit boundary, so the reserved name never reaches the file.
  // The save was never pressed, and the message is already on screen.
  await expect(byTestId(page, "edit-problem")).toContainText(/reserved/i, { timeout: 10_000 });
  await expect(byTestId(page, "component-Position")).toBeVisible();
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
});

test("an entity type requiring a component that does not exist is reported", async ({
  page,
  baseURL,
}) => {
  await takeFromDisk(page, baseURL, (doc) => {
    doc.entityTypes.TestDummy.requiredComponents.push("Nonexistent");
  });

  await page.goto("/forge/ents?type=TestDummy");
  const problems = byTestId(page, "type-problems");
  await expect(problems).toBeVisible({ timeout: 10_000 });
  await expect(problems).toContainText("Nonexistent");
  await expect(problems).toContainText("undeclared component");
  // Named as an error, not only coloured as one. Scoped to the first item
  // rather than the list: two problems on one owner is reachable, and a
  // multi-element locator fails as a strict-mode violation rather than as
  // anything to do with validation.
  await expect(problems.locator("li").first()).toHaveAttribute("data-blocking", "true");
});

test("a behaviour binding that names no machine is reported against that field", async ({
  page,
  baseURL,
}) => {
  await takeFromDisk(page, baseURL, (doc) => {
    doc.entityTypes.TestGoblin.behavior = "no-such-machine";
  });

  await page.goto("/forge/ents?type=TestGoblin");
  const select = page.locator('[data-testid="type-editor"] select').first();

  // The message is associated with the control, not merely next to it — which
  // is the difference between a sighted reader seeing it and everyone else.
  await expect(select).toHaveAttribute("aria-describedby", /.+/, { timeout: 10_000 });
  const describedBy = await select.getAttribute("aria-describedby");
  const message = page.locator(`#${describedBy}`);
  await expect(message).toContainText("no-such-machine");

  // A warning, not an error: nothing in the engine reads this field yet, so the
  // file still saves and the control's value is not invalid.
  await expect(message.locator("li").first()).toHaveAttribute("data-blocking", "false");
  await expect(select).not.toHaveAttribute("aria-invalid", "true");
  await expect(byTestId(page, "save-footer")).toHaveAttribute("data-blocked", "false");

  // And the seeds panel below defers to it rather than sending the reader to a log.
  await expect(byTestId(page, "seeds-missing")).toContainText("Behavior field");
});

// A problem on a row nobody has clicked is a problem nobody can find: the
// footer says the save is refused and the page shows nothing.
test("a list row carrying a problem is marked, even when it is not the open one", async ({
  page,
  baseURL,
}) => {
  await takeFromDisk(page, baseURL, (doc) => {
    doc.entityTypes.TestDummy.requiredComponents.push("Nonexistent");
  });

  // Open the *other* type, so the broken one is only reachable through the list.
  await page.goto("/forge/ents?type=TestGoblin");
  await expect(byTestId(page, "type-editor")).toHaveAttribute("data-type", "TestGoblin");

  await expect(byTestId(page, "type-TestDummy")).toHaveAttribute("data-problem", "true", {
    timeout: 10_000,
  });
  await expect(byTestId(page, "row-problem-TestDummy")).toHaveClass(/badge-problem--error/);
  await expect(byTestId(page, "type-TestGoblin")).toHaveAttribute("data-problem", "false");
  await expect(byTestId(page, "row-problem-TestGoblin")).toHaveCount(0);

  // And following the marker leads to the message.
  await byTestId(page, "type-TestDummy").click();
  await expect(byTestId(page, "type-problems")).toContainText("Nonexistent");
});

test("adding a second hp field warns about the ambiguity and names both components", async ({
  page,
}) => {
  await page.goto("/forge/schema?component=Position");
  await byTestId(page, "add-field").click();
  await expect(byTestId(page, "field-newField")).toBeVisible({ timeout: 10_000 });

  const input = page.locator('[data-testid="field-newField"] input[type="text"]');
  await input.fill("hp");
  await input.press("Enter");

  const row = byTestId(page, "field-hp");
  await expect(row).toHaveAttribute("data-problem", "true", { timeout: 10_000 });
  const warning = row.locator("li").first();
  await expect(warning).toHaveAttribute("data-blocking", "false");
  for (const named of ["Health", "Position", "e2e-wander", "hp"]) {
    await expect(warning).toContainText(named);
  }
});

test("a warning does not disable save; an error does", async ({ page, baseURL }) => {
  // The warning first: the same ambiguity as above, through the UI.
  await page.goto("/forge/schema?component=Position");
  await byTestId(page, "add-field").click();
  await expect(byTestId(page, "field-newField")).toBeVisible({ timeout: 10_000 });
  const input = page.locator('[data-testid="field-newField"] input[type="text"]');
  await input.fill("hp");
  await input.press("Enter");
  await expect(byTestId(page, "field-hp")).toHaveAttribute("data-problem", "true", {
    timeout: 10_000,
  });

  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
  await expect(byTestId(page, "save-footer")).toHaveAttribute("data-blocked", "false");
  await expect(saveButton(page)).toBeEnabled();

  // Now an error. Taking the schema from disk makes the session clean, and a
  // clean session disables Save on its own — so the assertion below would pass
  // against a footer that ignored validation entirely. One further edit through
  // the UI makes it dirty again, which is what makes "disabled" mean "blocked".
  await takeFromDisk(page, baseURL, (doc) => {
    doc.entityTypes.TestDummy.requiredComponents.push("Nonexistent");
  });
  await expect(byTestId(page, "save-footer")).toHaveAttribute("data-blocked", "true", {
    timeout: 10_000,
  });
  await byTestId(page, "add-field").click();
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/, {
    timeout: 10_000,
  });

  await expect(saveButton(page)).toBeDisabled();
  await expect(byTestId(page, "save-blocked")).toContainText("1 problem to fix first");
});

test("fixing the problem clears the message without a reload", async ({ page }) => {
  await page.goto("/forge/schema?component=Position");
  await byTestId(page, "add-field").click();
  await expect(byTestId(page, "field-newField")).toBeVisible({ timeout: 10_000 });

  const input = page.locator('[data-testid="field-newField"] input[type="text"]');
  await input.fill("hp");
  await input.press("Enter");
  await expect(byTestId(page, "field-hp")).toHaveAttribute("data-problem", "true", {
    timeout: 10_000,
  });

  // Rename it to something no machine seeds. No navigation, no reload — the
  // cleared state arrives on the page stream like everything else.
  const renamed = page.locator('[data-testid="field-hp"] input[type="text"]');
  await renamed.fill("stamina");
  await renamed.press("Enter");

  await expect(byTestId(page, "field-stamina")).toHaveAttribute("data-problem", "false", {
    timeout: 10_000,
  });
  await expect(byTestId(page, "field-stamina").locator("li")).toHaveCount(0);
});

test("a message points at an element that exists, on both halves of the file", async ({
  page,
  baseURL,
}) => {
  await takeFromDisk(page, baseURL, (doc) => {
    doc.components.Health.behavior = "missing-machine";
    doc.entityTypes.TestGoblin.behavior = "also-missing";
  });

  for (const [url, editor] of [
    ["/forge/schema?component=Health", "component-editor"],
    ["/forge/ents?type=TestGoblin", "type-editor"],
  ]) {
    await page.goto(url);
    await expect(byTestId(page, editor)).toBeVisible({ timeout: 10_000 });

    const described = page.locator(`[data-testid="${editor}"] select[aria-describedby]`);
    await expect(described).toHaveCount(1);
    const id = await described.getAttribute("aria-describedby");
    // An aria-describedby pointing at nothing reads as an association that
    // exists, which is worse than no attribute at all.
    await expect(page.locator(`#${id}`)).toHaveCount(1);
    await expect(page.locator(`#${id}`)).toBeVisible();
  }
});

test("the report says it may not be complete", async ({ page, baseURL }) => {
  await takeFromDisk(page, baseURL, (doc) => {
    doc.entityTypes.TestDummy.requiredComponents.push("Nonexistent");
  });

  await page.goto("/forge/ents?type=TestDummy");
  await expect(byTestId(page, "problems-partial")).toContainText(
    /stops at the first failure/i,
    { timeout: 10_000 },
  );
});

test("a valid schema says nothing at all", async ({ page }) => {
  await page.goto("/forge/schema?component=Health");
  await expect(byTestId(page, "component-editor")).toBeVisible();

  await expect(page.locator(".problems")).toHaveCount(0);
  await expect(page.locator("[aria-invalid]")).toHaveCount(0);
  await expect(byTestId(page, "save-footer")).toHaveAttribute("data-blocked", "false");
});
