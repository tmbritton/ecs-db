// Epic 15 Story 2 — the map editing session.
//
// What a map *is* to Forge: which maps the project has, which one the engine
// actually loads, and what happens to one that will not open. The canvas is
// Story 3's; painting is Story 4's, and so is the browser proof that an edit
// flips the footer — this story ships no edit route, and dirty, discard and
// reload are proven in Go where an edit is one call.
const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const MAPS = path.resolve(__dirname, "../fixtures/project/maps");

// Files a test drops into the fixture's maps directory, removed afterwards so
// the next spec sees the project it expects. The Epic 12 and 13 specs seed and
// restore schema.json and behaviors/ the same way.
const seeded = [];

function seedFile(name, body) {
  const at = path.join(MAPS, name);
  fs.writeFileSync(at, body);
  seeded.push(at);
}

test.afterEach(() => {
  while (seeded.length) {
    const at = seeded.pop();
    if (fs.existsSync(at)) fs.rmSync(at);
  }
});

test.beforeEach(async ({ page }) => {
  await page.goto("/forge/map");
});

test("the project's maps are listed, configured one first and marked", async ({ page }) => {
  await expect(byTestId(page, "map-mode")).toBeVisible();

  const rows = byTestId(page, "map-list").locator("[data-testid^='map-e2e-']");
  await expect(rows).toHaveCount(2);

  // game.toml names e2e-level.tmx and not e2e-arena.tmx. Only the first is the
  // map `ecs-db run` reads, and the list says so.
  await expect(byTestId(page, "map-configured-e2e-level.tmx")).toBeVisible();
  await expect(byTestId(page, "map-configured-e2e-arena.tmx")).toHaveCount(0);
});

test("the configured map is the one that opens", async ({ page }) => {
  await expect(byTestId(page, "map-title")).toHaveText("e2e-level.tmx");
  await expect(byTestId(page, "map-path")).toContainText("maps/e2e-level.tmx");
});

test("another map opens from the list and the URL follows", async ({ page }) => {
  await byTestId(page, "map-e2e-arena.tmx").click();
  await expect(byTestId(page, "map-title")).toHaveText("e2e-arena.tmx");
  expect(page.url()).toContain("e2e-arena.tmx");

  await page.reload();
  await expect(byTestId(page, "map-title")).toHaveText("e2e-arena.tmx");
});

test("a freshly opened map is saved", async ({ page }) => {
  const footer = byTestId(page, "save-footer");
  await expect(footer).toBeVisible();
  await expect(footer).not.toHaveClass(/save-footer--dirty/);
  await expect(footer).toContainText("e2e-level.tmx");
});

test("a map that will not parse is listed with its reason, not hidden", async ({ page }) => {
  seedFile("e2e-broken.tmx", "<map>this is not a map</map>");
  await page.reload();

  const problems = byTestId(page, "map-project-problems");
  await expect(problems).toBeVisible();
  await expect(problems).toContainText("e2e-broken.tmx");
  // And it is not offered as something to edit, because there is nothing there
  // to paint on.
  await expect(byTestId(page, "map-e2e-broken.tmx")).toHaveCount(0);
});

test("a .tmj is listed with what to do about it", async ({ page }) => {
  seedFile("e2e-json.tmj", '{"width":2,"height":2}');
  await page.reload();

  const problems = byTestId(page, "map-project-problems");
  await expect(problems).toContainText("e2e-json.tmj");
  await expect(problems).toContainText(".tmx");
});

test("the mode says a saved map is not hot-reloaded", async ({ page }) => {
  // The engine watches behaviour files and sprite assets and does not watch
  // maps. The design prototype's caption says the opposite, and a UI that
  // repeated it would have people waiting for something that never happens.
  await expect(byTestId(page, "map-editor")).toContainText("next");
  await expect(byTestId(page, "map-editor")).toContainText("ecs-db run");
});

test("a map changed on disk reports the conflict and offers both ways out", async ({ page }) => {
  // The path the server means, read off the page rather than rebuilt here.
  // config.Load resolves [map].path against the config file, so what Forge
  // holds is relative to the process's working directory — and a spec that
  // guessed an absolute path would have its request refused as naming a map the
  // project does not have.
  const held = await byTestId(page, "map-path").textContent();
  const level = path.join(MAPS, "e2e-level.tmx");
  const original = fs.readFileSync(level, "utf8");
  // The object counter: always present in a Tiled map, never part of the
  // picture, and safe to bump — the map still parses, so Reload can take it.
  // Pinning a row of tile ids instead made this spec depend on what the fixture
  // happened to look like, and it broke the moment the art changed.
  const touched = original.replace('nextobjectid="1"', 'nextobjectid="2"');
  expect(touched, "the fixture map has no object counter to bump").not.toBe(original);

  try {
    // No edit of Forge's own: this story ships no paint route, and Save checks
    // for an outside write before it checks whether it has anything to write.
    fs.writeFileSync(level, touched);
    const res = await page.request.post(`/forge/map/save?map=${encodeURIComponent(held)}`);
    expect(res.status()).toBe(204);
    await page.reload();

    const report = byTestId(page, "save-report");
    await expect(report).toContainText("changed on disk");
    // The buttons have to act on the map, not on schema.json — they posted to
    // /forge/schema/ for every file until Story 2's review.
    await expect(byTestId(page, "conflict-reload")).toHaveAttribute(
      "data-on:click",
      /\/forge\/map\/reload/,
    );
    await expect(byTestId(page, "conflict-overwrite")).toHaveAttribute(
      "data-on:click",
      /\/forge\/map\/save\/overwrite/,
    );

    await byTestId(page, "conflict-reload").click();
    await expect(byTestId(page, "save-report")).toHaveCount(0);
  } finally {
    fs.writeFileSync(level, original);
    await page.request.post(`/forge/map/reload?map=${encodeURIComponent(held)}`);
  }
});
