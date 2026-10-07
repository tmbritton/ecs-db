// Epic 15 Story 7: editing a spawn must write TMX the engine can import.
const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");
const { test, expect, byTestId } = require("../fixtures");

const level = path.resolve(__dirname, "../fixtures/project/maps/e2e-level.tmx");
const schema = path.resolve(__dirname, "../fixtures/project/schema.json");

test.beforeEach(async ({ page }) => {
  await page.goto("/forge/map");
  await expect(byTestId(page, "map-canvas")).toHaveAttribute("data-cell-w", /^[1-9]/);
  const canvas = byTestId(page, "map-canvas");
  const w = Number(await canvas.getAttribute("data-cell-w"));
  const h = Number(await canvas.getAttribute("data-cell-h"));
  await byTestId(page, "spawn-type-TestGoblin").dragTo(canvas, {
    targetPosition: { x: 2.5 * w, y: 1.5 * h },
  });
  await expect(byTestId(page, "spawn-1")).toBeVisible();
  await byTestId(page, "spawn-1").click();
  await expect(page).toHaveURL(/spawn=1/);
});

test.afterEach(async ({ baseURL }) => {
  const held = "e2e/fixtures/project/maps/e2e-level.tmx";
  const response = await fetch(`${baseURL}/forge/map/discard?map=${encodeURIComponent(held)}`, { method: "POST" });
  if (response.status !== 204) throw new Error(`could not discard map: ${response.status}`);
});

test("the selected spawn shows locked components, its position, and its type's machine", async ({ page }) => {
  await expect(byTestId(page, "spawn-selected")).toContainText("TestGoblin · object 1");
  await expect(byTestId(page, "spawn-component-Position")).toContainText("2,1");
  await expect(byTestId(page, "spawn-component-Position")).not.toContainText("✕");
  await expect(byTestId(page, "spawn-component-Health")).toContainText("🔒");
  await expect(byTestId(page, "spawn-component-Health")).toContainText("ƒ ctx");
  await expect(byTestId(page, "spawn-field-Health.hp")).toHaveValue("0");
  const behavior = byTestId(page, "spawn-behavior");
  await expect(behavior).toContainText("e2e-wander");
  await expect(behavior.locator("a")).toHaveAttribute("href", /\/forge\/ents\?type=TestGoblin/);
  await expect(behavior.locator("select")).toHaveCount(0);
  await expect(byTestId(page, "context-seeds")).toContainText("hp");
});

test("the inspector sits beside a usable canvas", async ({ page }) => {
  const canvas = await byTestId(page, "map-viewport").boundingBox();
  const inspector = await byTestId(page, "spawn-selected").boundingBox();
  expect(canvas.height, "the inspector squeezed the map out of view").toBeGreaterThan(150);
  expect(inspector.x, "the inspector landed below or over the map").toBeGreaterThan(canvas.x + canvas.width - 4);
  expect(inspector.y).toBeLessThan(canvas.y + canvas.height);
});

test("editing Health.hp survives save and the engine imports its value", async ({ page, baseURL }) => {
  const original = fs.readFileSync(level, "utf8");
  const held = await byTestId(page, "map-path").textContent();
  let temp;
  try {
    const hp = byTestId(page, "spawn-field-Health.hp");
    const edit = page.waitForResponse((r) => r.url().includes("/forge/map/spawn/property") && r.request().method() === "POST");
    await hp.fill("7");
    await hp.blur();
    await edit;
    await expect(byTestId(page, "spawn-field-Health.hp")).toHaveValue("7");
    await expect(byTestId(page, "save-footer")).toHaveClass(/save-footer--dirty/);
    expect((await page.request.post(`/forge/map/save?map=${encodeURIComponent(held)}`)).status()).toBe(204);
    expect(fs.readFileSync(level, "utf8")).toContain('name="Health.hp" type="int" value="7"');
    temp = fs.mkdtempSync(path.join("/tmp/opencode", "forge-inspector-"));
    const imported = execFileSync("go", ["run", "./e2e/fixtures/seed", "-out",
      path.join(temp, "world.sqlite"), "-map", level], {
      cwd: path.resolve(__dirname, "../.."), encoding: "utf8",
    });
    expect(imported).toContain("imported spawn object 1: TestGoblin at 2,1");
    expect(imported).toContain("with hp 7");
  } finally {
    if (temp) fs.rmSync(temp, { recursive: true, force: true });
    fs.writeFileSync(level, original);
    const resp = await fetch(`${baseURL}/forge/map/reload?map=${encodeURIComponent(held)}`, { method: "POST" });
    if (resp.status !== 204) throw new Error(`map reload: ${resp.status}`);
  }
});

test("an optional component may be attached and detached, required ones may not", async ({ page, baseURL }) => {
  const original = fs.readFileSync(schema, "utf8");
  const edited = JSON.parse(original);
  edited.entityTypes.TestGoblin.optionalComponents = ["E2EProbe"];
  try {
    fs.writeFileSync(schema, JSON.stringify(edited));
    expect((await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" })).status).toBe(204);
    await page.reload();
    await expect(byTestId(page, "spawn-add-component")).toBeVisible();
    await byTestId(page, "spawn-add-component").selectOption("E2EProbe");
    await expect(byTestId(page, "spawn-component-E2EProbe")).toBeVisible();
    await expect(byTestId(page, "spawn-component-Health").locator("button")).toHaveCount(0);
    await byTestId(page, "spawn-component-E2EProbe").locator("button.chip__remove").click();
    await expect(byTestId(page, "spawn-component-E2EProbe")).toHaveCount(0);
  } finally {
    fs.writeFileSync(schema, original);
    const resp = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
    if (resp.status !== 204) throw new Error(`schema reload: ${resp.status}`);
  }
});

test("warning-level contracts show the engine warning and keep the edit", async ({ page, baseURL }) => {
  const original = fs.readFileSync(schema, "utf8");
  const edited = JSON.parse(original);
  edited.entityTypes.TestGoblin.validationLevel = "warning";
  try {
    fs.writeFileSync(schema, JSON.stringify(edited));
    expect((await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" })).status).toBe(204);
    await page.reload();
    // The type did not declare E2EProbe, but warning level tells the engine
    // to keep an extra component and report it, not refuse the whole spawn.
    const held = await byTestId(page, "map-path").textContent();
    const result = await page.request.post(`/forge/map/spawn/component?map=${encodeURIComponent(held)}&id=1&add=E2EProbe`);
    expect(result.status()).toBe(204);
    await expect(byTestId(page, "spawn-component-E2EProbe")).toBeVisible();
    await expect(byTestId(page, "spawn-contract-warning")).toContainText("not allowed");
  } finally {
    fs.writeFileSync(schema, original);
    const resp = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
    if (resp.status !== 204) throw new Error(`schema reload: ${resp.status}`);
  }
});

test("an optional entity reference asks for its target before attaching", async ({ page, baseURL }) => {
  const original = fs.readFileSync(schema, "utf8");
  const edited = JSON.parse(original);
  edited.components.Target = { type: "entity-ref" };
  edited.entityTypes.TestGoblin.optionalComponents = ["Target"];
  try {
    fs.writeFileSync(schema, JSON.stringify(edited));
    expect((await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" })).status).toBe(204);
    await page.reload();
    await expect(byTestId(page, "spawn-attach-Target")).toBeVisible();
    await byTestId(page, "spawn-target-Target").fill("1");
    await byTestId(page, "spawn-attach-Target").click();
    await expect(byTestId(page, "spawn-component-Target")).toBeVisible();
    await expect(byTestId(page, "spawn-field-Target.target_entity_id")).toHaveValue("1");
  } finally {
    fs.writeFileSync(schema, original);
    const resp = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
    if (resp.status !== 204) throw new Error(`schema reload: ${resp.status}`);
  }
});

test("an object component asks for every reference target before attaching", async ({ page, baseURL }) => {
  const original = fs.readFileSync(schema, "utf8");
  const edited = JSON.parse(original);
  edited.components.Owner = { type: "object", properties: {
    first: { type: "entity-ref" }, second: { type: "entity-ref" },
  } };
  edited.entityTypes.TestGoblin.optionalComponents = ["Owner"];
  try {
    fs.writeFileSync(schema, JSON.stringify(edited));
    expect((await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" })).status).toBe(204);
    await page.reload();
    await byTestId(page, "spawn-target-Owner.first").fill("1");
    await byTestId(page, "spawn-target-Owner.second").fill("2");
    await byTestId(page, "spawn-attach-Owner").click();
    await expect(byTestId(page, "spawn-field-Owner.first")).toHaveValue("1");
    await expect(byTestId(page, "spawn-field-Owner.second")).toHaveValue("2");
  } finally {
    fs.writeFileSync(schema, original);
    const resp = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
    if (resp.status !== 204) throw new Error(`schema reload: ${resp.status}`);
  }
});

test("a hand-authored spawn missing required components can be repaired", async ({ page, baseURL }) => {
  const original = fs.readFileSync(level, "utf8");
  const held = await byTestId(page, "map-path").textContent();
  const incomplete = original
    .replace('nextobjectid="1"', 'nextobjectid="2"')
    .replace('<objectgroup id="2" name="spawns"/>',
      '<objectgroup id="2" name="spawns"><object id="1" type="TestGoblin" x="16" y="16"/></objectgroup>');
  expect(incomplete).not.toBe(original);
  try {
    // Give up the in-memory spawn from beforeEach, then open this authored
    // object from disk. It names no Health properties at all.
    expect((await page.request.post(`/forge/map/discard?map=${encodeURIComponent(held)}`)).status()).toBe(204);
    fs.writeFileSync(level, incomplete);
    expect((await fetch(`${baseURL}/forge/map/reload?map=${encodeURIComponent(held)}`, { method: "POST" })).status).toBe(204);
    await page.goto(`/forge/map?map=${encodeURIComponent(held)}&spawn=1`);
    await expect(byTestId(page, "spawn-component-Health")).toContainText("Missing required component");
    await byTestId(page, "spawn-repair-required").click();
    await expect(byTestId(page, "spawn-field-Health.hp")).toHaveValue("0");
    await expect(byTestId(page, "spawn-repair-required")).toHaveCount(0);
  } finally {
    fs.writeFileSync(level, original);
    const resp = await fetch(`${baseURL}/forge/map/reload?map=${encodeURIComponent(held)}`, { method: "POST" });
    if (resp.status !== 204) throw new Error(`map reload: ${resp.status}`);
  }
});

test("a forbidden machine seed respects the type's validation level", async ({ page, baseURL }) => {
  const original = fs.readFileSync(schema, "utf8");
  const edited = JSON.parse(original);
  edited.entityTypes.TestGoblin.requiredComponents = ["Position"];
  try {
    fs.writeFileSync(schema, JSON.stringify(edited));
    expect((await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" })).status).toBe(204);
    await page.reload();
    await expect(byTestId(page, "spawn-context-warning")).toContainText("engine will refuse");
    edited.entityTypes.TestGoblin.validationLevel = "warning";
    fs.writeFileSync(schema, JSON.stringify(edited));
    expect((await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" })).status).toBe(204);
    await page.reload();
    await expect(byTestId(page, "spawn-context-warning")).toContainText("proceeds with a warning");
    await expect(byTestId(page, "spawn-context-warning")).not.toContainText("will refuse");
  } finally {
    fs.writeFileSync(schema, original);
    const resp = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
    if (resp.status !== 204) throw new Error(`schema reload: ${resp.status}`);
  }
});

test("a field the engine rejects leaves the map unchanged and says why", async ({ page }) => {
  const field = byTestId(page, "spawn-field-Health.hp");
  const response = page.waitForResponse((r) => r.url().includes("/forge/map/spawn/property") && r.request().method() === "POST");
  await field.fill("plenty");
  await field.blur();
  await response;
  await expect(byTestId(page, "edit-problem")).toContainText("Health.hp");
  await page.reload();
  await expect(byTestId(page, "spawn-field-Health.hp")).toHaveValue("0");
});

test.describe("accessibility", () => {
  test("a value, a locked component and navigation are labelled", async ({ page }) => {
    await expect(byTestId(page, "spawn-field-Health.hp")).toHaveAttribute("aria-label", "Health.hp");
    await expect(byTestId(page, "spawn-component-Health").locator('[aria-label="required by type"]')).toHaveCount(1);
    await expect(byTestId(page, "spawn-behavior").locator("a")).toHaveAttribute("href", /ents/);
  });
});
