// Epic 12 Story 6 — the usage panel.
//
// Two kinds of fact: which entity types declare a component (from the file,
// always available) and how many entities exist right now (from the database,
// and different in a second). The sharpest assertion here is that removing the
// database changes the count without a reload — which is what proves the number
// is live rather than rendered once.

const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const DB = path.resolve(__dirname, "../fixtures/project/e2e.db");
const STASH = `${DB}.stash`;

test.describe.configure({ mode: "serial" });

test.afterEach(async ({ baseURL }) => {
  restoreDatabase();
  // Two tests add a component. Discarding in afterEach rather than inline
  // means a timed-out assertion cannot leave it in the shared session for the
  // rest of the run.
  const resp = await fetch(`${baseURL}/forge/schema/discard`, { method: "POST" });
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

test("a component lists the entity types that declare it", async ({ page }) => {
  await page.goto("/forge/schema?component=Position");

  // Both fixture types require Position.
  await expect(byTestId(page, "used-by-TestGoblin")).toBeVisible();
  await expect(byTestId(page, "used-by-TestDummy")).toBeVisible();
});

test("a component used by one type lists only that one", async ({ page }) => {
  await page.goto("/forge/schema?component=E2EProbe");

  await expect(byTestId(page, "used-by-TestDummy")).toBeVisible();
  await expect(byTestId(page, "used-by-TestGoblin")).toHaveCount(0);
});

test("live counts come from the seeded population", async ({ page }) => {
  await page.goto("/forge/ents?type=TestDummy");
  // Three dummies and one goblin: counts chosen so neither can be confused
  // with an off-by-one — and matched exactly, because toContainText("3") is a
  // substring test that 13, 30 and 36 all satisfy.
  await expect(byTestId(page, "live-TestDummy").locator(".live__count")).toHaveText("3");

  await page.goto("/forge/ents?type=TestGoblin");
  await expect(byTestId(page, "live-TestGoblin").locator(".live__count")).toHaveText("1");
  // And the singular reads as a singular.
  await expect(byTestId(page, "live-TestGoblin")).toContainText(/entity of this type/);
});

// A component is carried by entities, which is not the same as the population
// of the types that declare it. Health is optional on TestDummy and required
// on TestGoblin: three dummies exist without it and one goblin with it, so
// counting the declaring types answers 4 and the truth is 1.
test("a component counts its own rows, not its declaring types", async ({ page }) => {
  await page.goto("/forge/schema?component=Health");

  const count = byTestId(page, "live-rows-Health").locator(".live__count");
  await expect(count).toHaveText("1");
  await expect(count).not.toHaveText("4");
});

// A component added in the editor has no table until the engine migrates.
// "0 rows" would say the data is gone rather than not yet made.
test("a component with no table says so rather than showing zero", async ({ page }) => {
  await page.goto("/forge/schema");
  await byTestId(page, "add-component").click();
  await expect(byTestId(page, "component-NewComponent")).toBeVisible({ timeout: 10_000 });

  await page.goto("/forge/schema?component=NewComponent");
  await expect(byTestId(page, "live-not-built")).toBeVisible();
  await expect(byTestId(page, "live-rows-NewComponent")).toHaveCount(0);
});

test("removing the database changes the count without a reload", async ({ page }) => {
  await page.goto("/forge/ents?type=TestDummy");
  await expect(byTestId(page, "live-TestDummy")).toContainText("3");

  hideDatabase();

  // On the stream the page already holds — no navigation, no reload. This is
  // what separates a live reading from one rendered once at page load.
  await expect(byTestId(page, "live-unavailable")).toBeVisible({ timeout: 15_000 });
  await expect(byTestId(page, "live-TestDummy")).toHaveCount(0);
  await expect(byTestId(page, "live-unavailable")).toContainText(/not a count of zero/);

  restoreDatabase();

  await expect(byTestId(page, "live-TestDummy")).toContainText("3", { timeout: 15_000 });
});

test("the file-derived usage survives losing the database", async ({ page }) => {
  await page.goto("/forge/schema?component=Position");
  hideDatabase();

  // Which types declare a component is the fact you need when deciding whether
  // a deletion is safe, and it comes from the file — so it must not go with
  // the database.
  await expect(byTestId(page, "live-unavailable")).toBeVisible({ timeout: 15_000 });
  await expect(byTestId(page, "used-by-TestGoblin")).toBeVisible();
});

test("a component used by nothing says so", async ({ page }) => {
  // Add a component: nothing declares it yet.
  await page.goto("/forge/schema");
  await byTestId(page, "add-component").click();
  await expect(byTestId(page, "component-NewComponent")).toBeVisible({ timeout: 10_000 });

  await page.goto("/forge/schema?component=NewComponent");
  await expect(byTestId(page, "used-by-none")).toBeVisible();
});

test("nothing anywhere shows a spawn count", async ({ page }) => {
  for (const url of ["/forge/schema?component=Position", "/forge/ents?type=TestGoblin"]) {
    await page.goto(url);
    const note = byTestId(page, "spawn-note");
    await expect(note).toBeVisible();
    await expect(note).toContainText(/Epic 14/);
    await expect(note).toContainText(/not shown/);
    // The panel as a whole, not the note: the note is static prose whose only
    // digits are "14", so asserting it has no zero in it cannot fail. What
    // must hold is that no element anywhere is labelled as a spawn count.
    await expect(page.locator('[data-testid^="spawn-count"]')).toHaveCount(0);
  }
});
