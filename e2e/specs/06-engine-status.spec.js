// Story 6 — the engine-status readout.
//
// This is the first real payload down Forge's SSE stream, and its failure mode
// is the one the whole suite exists for: a status that never arrives looks
// exactly like a status that has not changed. So these tests move the world —
// delete the database, put it back — and assert the readout follows.
//
// The fixture database sits inside the fixture project with schema_version 7,
// so the project is self-contained. These tests
// manipulate that file, which is why the suite seeds its own rather than
// pointing at the developer's project.

const fs = require("fs");
const path = require("path");
const { test, expect, byTestId, expectSettled, watchEventStreams } = require("../fixtures");

const DB = path.resolve(__dirname, "../fixtures/project/e2e.db");
const SIDECARS = [DB, `${DB}-wal`, `${DB}-shm`];
const STASH = `${DB}.stash`;

const readout = (page) => byTestId(page, "engine-status-text");

// These specs move a file every other spec's server is reading, so they run one
// at a time and always put it back.
test.describe.configure({ mode: "serial" });

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

test.afterEach(() => restoreDatabase());

// Asserted against the navigation response body, which is the server-rendered
// HTML before any JavaScript runs. Reading the DOM instead proves nothing about
// first paint: `data-init` fires on load, the first patch lands within
// milliseconds, and Playwright's auto-retrying matchers happily wait for it.
// Verified — with the shell rendering a zero Status, the DOM-based version of
// these three tests passed all the same.
test("the readout is correct on first paint, before any script runs", async ({ page }) => {
  const resp = await page.goto("/forge/map");
  const html = await resp.text();
  expect(html, "the menu bar is blank or wrong until the first patch").toContain("hot-reload live");
});

test("it names the fixture project's own values", async ({ page }) => {
  const resp = await page.goto("/forge/map");
  const html = await resp.text();
  // v7 and e2e-core exist nowhere in the repo's real project, so this is also
  // asserting that Forge read the fixture rather than the working copy.
  expect(html).toContain("schema.json v7");
  expect(html).toContain("mods/e2e-core");
});

test("the readout is server-rendered on every mode", async ({ page }) => {
  for (const slug of ["tiles", "ents", "schema", "agents", "sprites"]) {
    const resp = await page.goto(`/forge/${slug}`);
    expect(await resp.text(), `${slug} did not render the readout`).toContain("hot-reload live");
  }
});

test("status travels on the page's existing stream, not a second connection", async ({ page }) => {
  const streams = watchEventStreams(page);
  await page.goto("/forge/map");
  await expect(readout(page)).toContainText("hot-reload live");
  // Story 5's constraint has to survive Story 6 adding traffic to it.
  await expectSettled(page, () => streams, ["/forge/map/events"], {
    message: "the status readout opened its own stream",
  });
});

test("the readout follows the database, both ways", async ({ page }) => {
  await page.goto("/forge/map");
  await expect(readout(page)).toContainText("hot-reload live");

  // The game stops.
  hideDatabase();
  await expect(readout(page)).toContainText("watcher offline", { timeout: 10_000 });
  // It says what follows, not just what happened — the point of the readout is
  // to tell you whether a save will land.
  await expect(readout(page)).toContainText("edits queue until game restarts");

  // And starts again.
  restoreDatabase();
  await expect(readout(page)).toContainText("hot-reload live", { timeout: 10_000 });
});

test("a stale database is reported distinctly from an absent one", async ({ page }) => {
  await page.goto("/forge/map");
  await expect(readout(page)).toContainText("hot-reload live");

  // Rewrite the recorded schema_version so it disagrees with schema.json.
  const { execFileSync } = require("child_process");
  execFileSync("sqlite3", [DB, "UPDATE meta SET value = '99' WHERE key = 'schema_version'"]);

  await expect(readout(page)).toContainText("schema v7", { timeout: 10_000 });
  await expect(readout(page)).toContainText("db v99");
  await expect(readout(page)).not.toContainText("watcher offline");

  execFileSync("sqlite3", [DB, "UPDATE meta SET value = '7' WHERE key = 'schema_version'"]);
  await expect(readout(page)).toContainText("hot-reload live", { timeout: 10_000 });
});

test("an unchanged status is not re-patched every tick", async ({ page }) => {
  // A stream that emits forever makes the browser's EventStream log useless for
  // debugging the much busier traffic Epic 18 puts on this same connection.
  //
  // Datastar consumes its own stream, so the frames cannot be counted from
  // outside it. This opens a second, raw subscription from the page and counts
  // what the server sends — a real measurement rather than a proxy. (The extra
  // stream is this test's own; the "one stream per page" constraint is asserted
  // separately, against a page that is not doing this.)
  await page.goto("/forge/map");
  await expect(readout(page)).toContainText("hot-reload live");

  const frames = await page.evaluate(async () => {
    const resp = await fetch("/forge/map/events");
    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let text = "";
    const deadline = Date.now() + 6000;

    while (Date.now() < deadline) {
      const chunk = await Promise.race([
        reader.read(),
        new Promise((r) => setTimeout(() => r({ done: true, timedOut: true }), deadline - Date.now())),
      ]);
      if (chunk.timedOut) break;
      if (chunk.done) break;
      text += decoder.decode(chunk.value, { stream: true });
    }
    await reader.cancel();

    // Counted per element, not in total. The page has two live regions, so the
    // opening burst is legitimately two frames — but "two frames" is also what
    // you get if one region is sent twice and the other never, which is the
    // regression this is meant to catch.
    // Anchored on the leading space. `data-testid="engine-status"` ends with
    // the substring `id="engine-status"`, so an unanchored match counts the
    // test id as well and reports two patches where there was one. The same
    // collision has now bitten three times in this project — checkbox__box,
    // the engine-status id, and here.
    return {
      status: (text.match(/ id="engine-status"/g) || []).length,
      saves: (text.match(/ id="save-reports"/g) || []).length,
      footer: (text.match(/ id="save-footer"/g) || []).length,
      total: (text.match(/event: datastar-patch-elements/g) || []).length,
    };
  });

  expect(frames.status, `engine status sent ${frames.status} times in ~6s`).toBe(1);
  expect(frames.saves, `save reports sent ${frames.saves} times in ~6s`).toBe(1);
  expect(frames.footer, `save footer sent ${frames.footer} times in ~6s`).toBe(1);
  // Three live regions, each sent once. The total is checked as well so a
  // region added later without a counter here fails loudly.
  expect(frames.total, `${frames.total} patches in total`).toBe(3);
});

test("the readout is announced to assistive tech when it changes", async ({ page }) => {
  await page.goto("/forge/map");
  // It updates without a page load, so a screen reader has to be told. Stated
  // explicitly because test-id selectors notice none of this on their own.
  await expect(byTestId(page, "engine-status")).toHaveRole("status");
});

// The save-report container has to exist on first paint. A patch finds its
// target by id, so a page that rendered nothing until the first save would have
// nowhere to put it.
test("the save-report container is present before anything is saved", async ({ page }) => {
  const resp = await page.goto("/forge/map");
  expect(await resp.text(), "the patch target is missing from the served HTML").toContain(
    'id="save-reports"'
  );
  await expect(byTestId(page, "save-reports")).toBeAttached();
  // Empty, though — nothing has been saved.
  await expect(page.locator('[data-testid="save-report"]')).toHaveCount(0);
});
