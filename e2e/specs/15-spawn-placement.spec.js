// Epic 15 Story 6: a map object is a spawn, and its id is the engine's identity.
const { test, expect, byTestId } = require("../fixtures");
const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");

const level = path.resolve(__dirname, "../fixtures/project/maps/e2e-level.tmx");
const schema = path.resolve(__dirname, "../fixtures/project/schema.json");

test.beforeEach(async ({ page }) => {
  await page.goto("/forge/map");
  await expect(byTestId(page, "map-canvas")).toHaveAttribute("data-cell-w", /^[1-9]/);
});

test.afterEach(async ({ baseURL }) => {
  for (const name of ["e2e-level.tmx", "e2e-arena.tmx"]) {
    const path = `e2e/fixtures/project/maps/${name}`;
    const resp = await fetch(`${baseURL}/forge/map/discard?map=${encodeURIComponent(path)}`, { method: "POST" });
    if (resp.status !== 204) throw new Error(`could not discard ${path}: ${resp.status}`);
  }
});

async function dropOn(page, source, x, y) {
  const box = await byTestId(page, "map-canvas").boundingBox();
  const w = Number(await byTestId(page, "map-canvas").getAttribute("data-cell-w"));
  const h = Number(await byTestId(page, "map-canvas").getAttribute("data-cell-h"));
  await source.dragTo(byTestId(page, "map-canvas"), {
    targetPosition: { x: (x + 0.5) * w, y: (y + 0.5) * h },
  });
  return { box, w, h };
}

test("dragging a type into an explicit object group places and selects a spawn", async ({ page }) => {
  await expect(byTestId(page, "object-group-spawns")).toHaveAttribute("data-current", "true");
  await dropOn(page, byTestId(page, "spawn-type-TestGoblin"), 2, 1);
  await expect(byTestId(page, "spawn-1")).toBeVisible();
  await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
  await byTestId(page, "spawn-1").click();
  await expect(page).toHaveURL(/spawn=1/);
  await expect(byTestId(page, "spawn-selected")).toContainText("TestGoblin");
  await page.reload();
  await expect(byTestId(page, "spawn-selected")).toContainText("TestGoblin");
});

test("saving writes a spawn the map still contains after reload", async ({ page, baseURL }) => {
  const original = fs.readFileSync(level, "utf8");
  const held = await byTestId(page, "map-path").textContent();
  let temp;
  try {
    await dropOn(page, byTestId(page, "spawn-type-TestGoblin"), 2, 1);
    await expect(byTestId(page, "spawn-1")).toBeVisible();
    const result = await page.request.post(`/forge/map/save?map=${encodeURIComponent(held)}`);
    expect(result.status()).toBe(204);
    expect(fs.readFileSync(level, "utf8")).toContain('type="TestGoblin" x="32" y="16"');
    await page.reload();
    await expect(byTestId(page, "spawn-1")).toBeVisible();
    // The configured fixture has no Tile entity type, so the full tile loader
    // cannot run here. Exercise its real spawn-import path against the *saved*
    // TMX in a fresh engine database, not a separately constructed document.
    temp = fs.mkdtempSync(path.join("/tmp/opencode", "forge-spawn-"));
    const imported = execFileSync("go", ["run", "./e2e/fixtures/seed",
      "-out", path.join(temp, "world.sqlite"), "-map", level], {
      cwd: path.resolve(__dirname, "../.."), encoding: "utf8",
    });
    expect(imported).toContain("imported spawn object 1: TestGoblin at 2,1");
  } finally {
    if (temp) fs.rmSync(temp, { recursive: true, force: true });
    fs.writeFileSync(level, original);
    const response = await fetch(`${baseURL}/forge/map/reload?map=${encodeURIComponent(held)}`, { method: "POST" });
    if (response.status !== 204) throw new Error(`map reload: ${response.status}`);
  }
});

test("when a map has several object groups the author chooses the destination", async ({ page, baseURL }) => {
  const original = fs.readFileSync(level, "utf8");
  const held = await byTestId(page, "map-path").textContent();
  const twoGroups = original.replace('<objectgroup id="2" name="spawns"/>',
    '<objectgroup id="2" name="spawns"/>\n  <objectgroup id="4" name="reinforcements"/>');
  expect(twoGroups).not.toBe(original);
  try {
    fs.writeFileSync(level, twoGroups);
    expect((await fetch(`${baseURL}/forge/map/reload?map=${encodeURIComponent(held)}`, { method: "POST" })).status).toBe(204);
    await page.reload();
    await expect(byTestId(page, "object-group-spawns")).toHaveClass(/segmented__on/);
    await byTestId(page, "object-group-reinforcements").click();
    await expect(byTestId(page, "object-group-reinforcements")).toHaveAttribute("aria-pressed", "true");
    await expect(byTestId(page, "object-group-reinforcements")).toHaveClass(/segmented__on/);
    await expect(byTestId(page, "object-group-spawns")).not.toHaveClass(/segmented__on/);
    await dropOn(page, byTestId(page, "spawn-type-TestGoblin"), 2, 2);
    await expect(byTestId(page, "spawn-1")).toBeVisible();
    expect((await page.request.post(`/forge/map/save?map=${encodeURIComponent(held)}`)).status()).toBe(204);
    expect(fs.readFileSync(level, "utf8")).toMatch(/<objectgroup id="4" name="reinforcements">\s*<object id="1"/);
  } finally {
    fs.writeFileSync(level, original);
    const resp = await fetch(`${baseURL}/forge/map/reload?map=${encodeURIComponent(held)}`, { method: "POST" });
    if (resp.status !== 204) throw new Error(`map reload: ${resp.status}`);
  }
});

test("moving keeps the id; deleting and placing again does not reuse it", async ({ page }) => {
  await dropOn(page, byTestId(page, "spawn-type-TestGoblin"), 1, 0);
  await expect(byTestId(page, "spawn-1")).toBeVisible();
  await dropOn(page, byTestId(page, "spawn-1"), 3, 2);
  await expect(byTestId(page, "spawn-1")).toHaveAttribute("data-cell", "3,2");
  await byTestId(page, "spawn-1").click();
  await byTestId(page, "spawn-delete").click();
  await expect(byTestId(page, "spawn-1")).toHaveCount(0);
  await expect(byTestId(page, "spawn-missing")).toContainText("no longer in this map");
  await byTestId(page, "spawn-clear-selection").click();
  await expect(page).not.toHaveURL(/spawn=/);
  await dropOn(page, byTestId(page, "spawn-type-TestGoblin"), 1, 1);
  await expect(byTestId(page, "spawn-2")).toBeVisible();
});

test("a type without Position stays visible with its reason but cannot be dragged", async ({ page, baseURL }) => {
  const original = fs.readFileSync(schema, "utf8");
  const edited = JSON.parse(original);
  edited.entityTypes.E2ENoPosition = {
    requiredComponents: ["Health"], optionalComponents: [],
    allowExtraComponents: false, validationLevel: "strict",
  };
  try {
    fs.writeFileSync(schema, JSON.stringify(edited));
    const reloaded = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
    expect(reloaded.status).toBe(204);
    await page.reload();
    const row = byTestId(page, "spawn-type-E2ENoPosition");
    await expect(row).toContainText("does not declare Position");
    await expect(row).not.toHaveAttribute("draggable", "true");
  } finally {
    fs.writeFileSync(schema, original);
    const reloaded = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
    if (reloaded.status !== 204) throw new Error(`schema reload: ${reloaded.status}`);
  }
});

test("a type name with quotes and Datastar action text still drags", async ({ page, baseURL }) => {
  const original = fs.readFileSync(schema, "utf8");
  const edited = JSON.parse(original);
  const name = "O'Brien@post(x)";
  edited.entityTypes[name] = {
    requiredComponents: ["Position"], optionalComponents: [],
    allowExtraComponents: false, validationLevel: "strict",
  };
  try {
    fs.writeFileSync(schema, JSON.stringify(edited));
    expect((await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" })).status).toBe(204);
    await page.reload();
    await dropOn(page, byTestId(page, `spawn-type-${name}`), 2, 1);
    await expect(byTestId(page, "spawn-1")).toHaveAttribute("aria-label", /O'Brien@post\(x\)/);
  } finally {
    fs.writeFileSync(schema, original);
    const resp = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
    if (resp.status !== 204) throw new Error(`schema reload: ${resp.status}`);
  }
});

test.describe("accessibility", () => {
  test("spawn controls identify their purpose", async ({ page }) => {
    await expect(byTestId(page, "spawn-type-TestGoblin")).toHaveAttribute("aria-label", /TestGoblin/);
    await expect(byTestId(page, "object-group-spawns")).toHaveAttribute("aria-pressed", "true");
    await dropOn(page, byTestId(page, "spawn-type-TestGoblin"), 1, 1);
    await byTestId(page, "spawn-1").click();
    await expect(byTestId(page, "spawn-delete")).toHaveAttribute("aria-label", /delete/i);
  });
});
