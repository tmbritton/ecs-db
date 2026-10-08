// Epic 15 Story 9 — validation is live, attributed and never blocks Save.
const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const MAPS = path.resolve(__dirname, "../fixtures/project/maps");
const level = fs.readFileSync(path.join(MAPS, "e2e-level.tmx"), "utf8");
const seeded = [];

function seed(name, body) {
  const at = path.join(MAPS, name);
  fs.writeFileSync(at, body);
  seeded.push(name);
}

async function openSeeded(page, name) {
  await page.reload();
  await byTestId(page, `map-${name}`).click();
  await expect(byTestId(page, "map-title")).toHaveText(name);
}

test.beforeEach(async ({ page }) => { await page.goto("/forge/map"); });
test.afterEach(async ({ page }) => {
  while (seeded.length) {
    const name = seeded.pop();
    if (name.endsWith(".tmx") || name.endsWith(".tmj")) {
      await page.request.post(`/forge/map/discard?map=${encodeURIComponent(`e2e/fixtures/project/maps/${name}`)}`);
    }
    fs.rmSync(path.join(MAPS, name), { force: true });
  }
});

test("missing mapId warns, and adding one clears the warning without navigation", async ({ page }) => {
  seed("e2e-noid.tmx", level.replace('<property name="mapId" value="e2e-level"/>', ""));
  await openSeeded(page, "e2e-noid.tmx");
  await expect(byTestId(page, "map-id-warning")).toContainText("spawns are filed under its path");
  await byTestId(page, "map-id").fill("stable-noid");
  await byTestId(page, "map-id").press("Tab");
  await expect(byTestId(page, "map-id-warning")).toHaveCount(0);
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
  await expect(byTestId(page, "map-id")).toHaveValue("stable-noid");
});

test("two maps with one mapId name both files", async ({ page }) => {
  seed("e2e-duplicate-id.tmx", level);
  await page.reload();
  await expect(byTestId(page, "map-identity-conflict")).toContainText("e2e-level.tmx");
  await expect(byTestId(page, "map-identity-conflict")).toContainText("e2e-duplicate-id.tmx");
});

test("an idless tile layer shows why its reorder and delete are unavailable", async ({ page }) => {
  const src = level.replace('value="e2e-level"', 'value="idless-layer"')
    .replace('<layer id="1" name="ground"', '<layer name="ground"');
  seed("e2e-idless-layer.tmx", src);
  await openSeeded(page, "e2e-idless-layer.tmx");
  await expect(byTestId(page, "layer-ground")).toHaveAttribute("data-invalid", "true");
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-invalid", "false");
  await expect(byTestId(page, "map-validation")).toContainText("positive Tiled ID");
  await expect(byTestId(page, "map-validation")).toContainText("cannot import");
  const opened = page.waitForRequest((req) => req.url().includes("/forge/map/menu?") && req.method() === "POST");
  await byTestId(page, "layer-ground").click({ button: "right" });
  await opened;
  await expect(byTestId(page, "map-context-menu")).toBeVisible();
  await expect(byTestId(page, "map-context-menu").locator(".ctx-menu__item")).toHaveCount(2);
});

test("both layers with a duplicate ID get the collision, not only the second", async ({ page }) => {
  const src = level.replace('value="e2e-level"', 'value="duplicate-layer-id"')
    .replace('<layer id="3" name="props"', '<layer id="1" name="props"');
  seed("e2e-duplicate-layer-id.tmx", src);
  await openSeeded(page, "e2e-duplicate-layer-id.tmx");
  for (const name of ["ground", "props"]) {
    await expect(byTestId(page, `layer-${name}`)).toHaveAttribute("data-invalid", "true");
  }
  await expect(byTestId(page, "map-validation")).toContainText("ground");
  await expect(byTestId(page, "map-validation")).toContainText("props");
  await expect(byTestId(page, "map-validation")).toContainText("cannot import");
  await byTestId(page, "layer-props").click({ button: "right" });
  await expect(byTestId(page, "map-context-menu").locator(".ctx-menu__item")).toHaveCount(2);
});

test("an unknown spawn class is flagged on its own marker and clears on reload", async ({ page }) => {
  const withSpawn = level.replace('value="e2e-level"', 'value="bad-class"')
    .replace('<objectgroup id="2" name="spawns"/>',
      '<objectgroup id="2" name="spawns"><object id="1" type="NotAnEntity" x="0" y="0"/></objectgroup>');
  seed("e2e-bad-class.tmx", withSpawn);
  await openSeeded(page, "e2e-bad-class.tmx");
  await expect(byTestId(page, "spawn-1")).toHaveAttribute("data-invalid", "true");
  await expect(byTestId(page, "map-validation")).toContainText('unknown entity type "NotAnEntity"');
  const fixed = withSpawn.replace('type="NotAnEntity"', 'type="TestGoblin"')
    .replace('x="0" y="0"/>', 'x="0" y="0"><properties><property name="Health.hp" type="int" value="5"/></properties></object>');
  fs.writeFileSync(path.join(MAPS, "e2e-bad-class.tmx"), fixed);
  await page.request.post(`/forge/map/reload?map=${encodeURIComponent("e2e/fixtures/project/maps/e2e-bad-class.tmx")}`);
  await expect(byTestId(page, "spawn-1")).toHaveAttribute("data-invalid", "false");
  await expect(byTestId(page, "map-validation")).toHaveCount(0);
});

test("a missing required component marks the spawn until it is repaired", async ({ page }) => {
  seed("e2e-missing-health.tmx", level.replace('value="e2e-level"', 'value="missing-health"')
    .replace('<objectgroup id="2" name="spawns"/>',
      '<objectgroup id="2" name="spawns"><object id="1" type="TestGoblin" x="0" y="0"/></objectgroup>'));
  await openSeeded(page, "e2e-missing-health.tmx");
  await expect(byTestId(page, "spawn-1")).toHaveAttribute("data-invalid", "true");
  await expect(byTestId(page, "map-validation")).toContainText('missing required component "Health"');
  await byTestId(page, "spawn-1").click();
  await byTestId(page, "spawn-repair-required").click();
  await expect(byTestId(page, "spawn-1")).toHaveAttribute("data-invalid", "false");
  await expect(byTestId(page, "map-validation")).toHaveCount(0);
});

test("duplicate object IDs mark both objects rather than only the second", async ({ page }) => {
  const props = '<properties><property name="Health.hp" type="int" value="5"/></properties>';
  const body = level.replace('value="e2e-level"', 'value="duplicate-objects"')
    .replace('<objectgroup id="2" name="spawns"/>',
      `<objectgroup id="2" name="spawns"><object id="1" name="first" type="TestGoblin" x="0" y="0">${props}</object><object id="1" name="second" type="TestGoblin" x="16" y="0">${props}</object></objectgroup>`);
  seed("e2e-duplicate-objects.tmx", body);
  await openSeeded(page, "e2e-duplicate-objects.tmx");
  await expect(byTestId(page, "spawn-1-g0-i0")).toHaveAttribute("data-invalid", "true");
  await expect(byTestId(page, "spawn-1-g0-i1")).toHaveAttribute("data-invalid", "true");
  await expect(byTestId(page, "map-validation")).toContainText("object id 1 belongs to another object");
  await expect(byTestId(page, "map-validation")).toContainText("first");
  await expect(byTestId(page, "map-validation")).toContainText("second");
  const urlBefore = page.url();
  for (const id of ["spawn-1-g0-i0", "spawn-1-g0-i1"]) {
    await byTestId(page, id).click();
    expect(page.url()).toBe(urlBefore);
    await expect(byTestId(page, "spawn-selected")).toHaveCount(0);
  }
  await page.goto(`${urlBefore}&spawn=1`);
  await expect(byTestId(page, "spawn-ambiguous")).toBeVisible();
  await expect(byTestId(page, "spawn-delete")).toHaveCount(0);
});

test("an unresolved gid marks its cell and several independent problems stay visible", async ({ page }) => {
  const broken = level.replace('value="e2e-level"', 'value="bad-tiles"')
    .replace("1,1,1,1,1,1,", "99999,1,1,1,1,1,")
    .replace('<objectgroup id="2" name="spawns"/>',
      '<objectgroup id="2" name="spawns"><object id="1" type="NotAnEntity" x="0" y="0"/></objectgroup>');
  seed("e2e-bad-tiles.tmx", broken);
  await openSeeded(page, "e2e-bad-tiles.tmx");
  const cell = byTestId(page, "map-layer-0").locator('[data-cell="ground:0,0"]');
  await expect(cell).toHaveAttribute("data-invalid", "true");
  await expect(byTestId(page, "spawn-1")).toHaveAttribute("data-invalid", "true");
  await expect(byTestId(page, "map-validation-count")).toContainText("2");
  await expect(byTestId(page, "map-validation")).toContainText("does not hold it");
  await expect(byTestId(page, "map-validation")).toContainText("unknown entity type");
  await expect(byTestId(page, "save-footer")).not.toHaveAttribute("data-blocked", "true");
});

test("a covered tile on a hidden lower layer still reports its import refusal", async ({ page }) => {
  const broken = level.replace('value="e2e-level"', 'value="buried-bad-tile"')
    .replace('<layer id="1" name="ground"', '<layer id="1" name="ground" visible="0"')
    .replace('1,12,15,15,14,1,', '1,99999,15,15,14,1,');
  seed("e2e-buried-bad-tile.tmx", broken);
  await openSeeded(page, "e2e-buried-bad-tile.tmx");
  await expect(byTestId(page, "map-validation-count")).toContainText("1");
  await expect(byTestId(page, "map-validation")).toContainText("does not hold it");
  await expect(byTestId(page, "map-layer-0").locator('[data-cell="ground:1,1"]'))
    .toHaveAttribute("data-invalid", "true");
});

test("a tile whose image rectangle is outside its sheet reports the load refusal", async ({ page }) => {
  const sheet = fs.readFileSync(path.join(MAPS, "e2e-tiles.tsx"), "utf8")
    .replace('width="128" height="240"', 'width="16" height="240"');
  seed("e2e-shrunken-tiles.tsx", sheet);
  seed("e2e-bad-art.tmx", level.replace('value="e2e-level"', 'value="bad-art"')
    .replace('source="e2e-tiles.tsx"', 'source="e2e-shrunken-tiles.tsx"'));
  await openSeeded(page, "e2e-bad-art.tmx");
  await expect(byTestId(page, "map-validation")).toContainText("tiny16-basic.png");
  await expect(byTestId(page, "map-layer-0").locator('[data-cell="ground:1,1"]'))
    .toHaveAttribute("data-invalid", "true");
});

test("an unloadable working map can still be saved with its validation visible", async ({ page }) => {
  seed("e2e-save-broken.tmx", level.replace('value="e2e-level"', 'value="save-broken"')
    .replace("1,1,1,1,1,1,", "99999,1,1,1,1,1,"));
  await openSeeded(page, "e2e-save-broken.tmx");
  await expect(byTestId(page, "map-validation")).toContainText("does not hold it");
  await byTestId(page, "map-id").fill("saved-even-broken");
  await byTestId(page, "map-id").press("Tab");
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
  await byTestId(page, "save-footer").locator(".save-footer__save").click();
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);
  const onDisk = fs.readFileSync(path.join(MAPS, "e2e-save-broken.tmx"), "utf8");
  expect(onDisk).toContain('value="saved-even-broken"');
  expect(onDisk).toContain("99999,1,1,1,1,1,");
  await expect(byTestId(page, "map-validation")).toContainText("does not hold it");
});

test.describe("accessibility", () => {
  test("map identity is labelled and owner problems have text as well as colour", async ({ page }) => {
    await expect(byTestId(page, "map-id")).toHaveAttribute("aria-label", "mapId");
    seed("e2e-a11y-issue.tmx", level.replace('value="e2e-level"', 'value="a11y-issue"')
      .replace("1,1,1,1,1,1,", "99999,1,1,1,1,1,"));
    await openSeeded(page, "e2e-a11y-issue.tmx");
    await expect(byTestId(page, "map-validation")).toHaveAttribute("aria-label", "Map validation");
    await expect(byTestId(page, "map-layer-0").locator('[data-cell="ground:0,0"]'))
      .toHaveAttribute("title", /does not hold it/);
  });
});
