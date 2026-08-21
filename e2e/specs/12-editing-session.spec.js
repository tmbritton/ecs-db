// Epic 12 Story 1 — the editing session both schema modes share.
//
// The session lives on the server, not in the page, because mode switching is a
// full page load. The last test here is the one that proves it.

const fs = require("fs");
const path = require("path");
const { test, expect, byTestId, expectSettled, watchEventStreams } = require("../fixtures");

const SCHEMA = path.resolve(__dirname, "../fixtures/project/schema.json");

// These tests edit and save the fixture project's schema, so they run one at a
// time and restore it afterwards.
test.describe.configure({ mode: "serial" });

let original;
test.beforeAll(() => {
  original = fs.readFileSync(SCHEMA, "utf8");
});

// schema.json is a tracked file. afterEach restores it between tests, but an
// interrupted run — Ctrl-C, a worker crash — would leave it modified and ready
// to be committed by accident.
test.afterAll(() => {
  if (original !== undefined) fs.writeFileSync(SCHEMA, original);
});

// Restoring the file is not enough. The session is server-side state that
// outlives a request — which is the whole point of the story — so it keeps an
// unsaved bump from a previous test unless it is told to re-read. Reload is the
// real endpoint for exactly that, so the cleanup exercises production code
// rather than reaching around it.
test.afterEach(async ({ baseURL }) => {
  fs.writeFileSync(SCHEMA, original);
  const resp = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
  if (resp.status !== 204) {
    throw new Error(`could not reset the session: ${resp.status}`);
  }
});

const footer = (page) => byTestId(page, "save-footer");
const saveButton = (page) => page.locator('[data-testid="save-footer"] button', { hasText: /save/i });
const discardButton = (page) => page.locator('[data-testid="save-footer"] button', { hasText: /discard/i });

// Drives a real edit through the same action the UI will use once Story 2 puts
// controls on the page. Until then this is how the session is exercised end to
// end without inventing a fake trigger.
async function bumpVersion(page) {
  return page.evaluate(() => fetch("/dev/schema/bump", { method: "POST" }).then((r) => r.status));
}

test("the footer is clean on load and names the file", async ({ page }) => {
  const resp = await page.goto("/forge/schema");
  const html = await resp.text();
  // Asserted on the served HTML: the footer must be right on first paint, not
  // after the first stream tick.
  expect(html).toContain("schema.json");
  expect(html).not.toContain("save-footer--dirty");
});

test("both buttons are disabled when there is nothing to save", async ({ page }) => {
  await page.goto("/forge/schema");
  await expect(saveButton(page)).toBeDisabled();
  await expect(discardButton(page)).toBeDisabled();
});

test("an edit marks the footer dirty without a page reload", async ({ page }) => {
  await page.goto("/forge/schema");
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/);

  expect(await bumpVersion(page)).toBe(204);

  // The change arrives on the stream the page already holds.
  await expect(footer(page)).toHaveClass(/save-footer--dirty/, { timeout: 10_000 });
  await expect(saveButton(page)).toBeEnabled();
});

test("saving writes the file and returns the footer to clean", async ({ page }) => {
  await page.goto("/forge/schema");
  expect(await bumpVersion(page)).toBe(204);
  await expect(footer(page)).toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  await saveButton(page).click();
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  // The file, not the UI.
  const onDisk = JSON.parse(fs.readFileSync(SCHEMA, "utf8"));
  expect(onDisk.schemaVersion).toBe(8);
});

test("discarding asks first, and cancelling changes nothing", async ({ page }) => {
  await page.goto("/forge/schema");
  expect(await bumpVersion(page)).toBe(204);
  await expect(footer(page)).toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  // Discard throws away work with no undo, so it confirms. Dismissing must
  // leave the edit alone.
  let asked = 0;
  page.once("dialog", (d) => {
    asked++;
    d.dismiss();
  });
  await discardButton(page).click();
  await page.waitForTimeout(500);
  expect(asked, "discard did not ask before throwing work away").toBe(1);
  await expect(footer(page)).toHaveClass(/save-footer--dirty/);
});

test("discarding restores the last saved state", async ({ page }) => {
  await page.goto("/forge/schema");
  expect(await bumpVersion(page)).toBe(204);
  await expect(footer(page)).toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  page.once("dialog", (d) => d.accept());
  await discardButton(page).click();
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  const onDisk = JSON.parse(fs.readFileSync(SCHEMA, "utf8"));
  expect(onDisk.schemaVersion).toBe(7);
});

// A conflict must not be a dead end. The endpoints existed with no control on
// the page, so the only button on offer was Discard — which destroys the edit
// and then reports the file as saved.
test("a conflict offers both ways out", async ({ page }) => {
  await page.goto("/forge/schema");
  expect(await bumpVersion(page)).toBe(204);

  // Something else edits the file underneath us.
  const theirs = original.replace('"schemaVersion": 7', '"schemaVersion": 11');
  fs.writeFileSync(SCHEMA, theirs);

  await saveButton(page).click();
  const report = byTestId(page, "save-report-summary");
  await expect(report).toContainText("changed on disk", { timeout: 10_000 });

  await expect(byTestId(page, "conflict-reload")).toBeVisible();
  await expect(byTestId(page, "conflict-overwrite")).toBeVisible();

  // Take theirs, and the session is usable again.
  await byTestId(page, "conflict-reload").click();
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/, { timeout: 10_000 });
  const onDisk = JSON.parse(fs.readFileSync(SCHEMA, "utf8"));
  expect(onDisk.schemaVersion).toBe(11);
});

test("a save reports its outcome on the page stream", async ({ page }) => {
  await page.goto("/forge/schema");
  expect(await bumpVersion(page)).toBe(204);
  await saveButton(page).click();

  const report = byTestId(page, "save-report-summary");
  await expect(report).toBeVisible({ timeout: 10_000 });

  // Not "hot-reload live". The edit bumped schemaVersion, so the saved schema
  // no longer matches the database and nothing will pick it up — which the
  // readout correctly notices.
  //
  // The wording is imprecise, though: a stale database reports as "no game
  // running" because savereport folds StateMismatch in with StateOffline. The
  // consequence is the same but the reason is not, and a stale database is
  // exactly the case worth naming. Recorded in the story; not fixed here,
  // because it belongs to Epic 11 Story 6's outcome set.
  await expect(report).toContainText("saved");
  await expect(report).not.toContainText("not saved");
});

test("saving does not open a second stream", async ({ page }) => {
  const streams = watchEventStreams(page);
  await page.goto("/forge/schema");
  expect(await bumpVersion(page)).toBe(204);
  await saveButton(page).click();
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  await expectSettled(page, () => streams, ["/forge/schema/events"], {
    message: "editing opened a stream of its own",
  });
});

// The point of the story. Mode switching is a full page load, so an unsaved
// edit only survives it if the session lives on the server. A per-mode copy
// passes every other test in this file and fails this one.
test("an unsaved edit survives switching modes", async ({ page }) => {
  await page.goto("/forge/schema");
  expect(await bumpVersion(page)).toBe(204);
  await expect(footer(page)).toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  await byTestId(page, "rail-ents").click();
  await expect(page).toHaveURL("/forge/ents");
  // Still dirty in the other mode: it is one file and one session.
  await expect(footer(page)).toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  await byTestId(page, "rail-schema").click();
  await expect(page).toHaveURL("/forge/schema");
  await expect(footer(page)).toHaveClass(/save-footer--dirty/, { timeout: 10_000 });

  // And it is still the same edit, not merely some dirtiness.
  await saveButton(page).click();
  await expect(footer(page)).not.toHaveClass(/save-footer--dirty/, { timeout: 10_000 });
  const onDisk = JSON.parse(fs.readFileSync(SCHEMA, "utf8"));
  expect(onDisk.schemaVersion).toBe(8);
});
