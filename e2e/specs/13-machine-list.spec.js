// Epic 13 Story 3 — the machine list and the context manifest.
//
// Two things here can only be shown in a browser. The first is that the
// manifest's two absences are actually different on screen: "this machine seeds
// nothing" and "nobody has worked out what this machine seeds" are the same
// empty map in Go, and the whole story is about not letting them render as the
// same sentence. The second is that the readout follows the *session* rather
// than the page — breaking a machine from another tab has to reach this one on
// the stream it already holds, with nobody reloading anything.

const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const PROJECT = path.resolve(__dirname, "../fixtures/project");
const BEHAVIORS = path.join(PROJECT, "behaviors");
const OVERLAY = path.join(PROJECT, "overlay/behaviors");
const SCHEMA = path.join(PROJECT, "schema.json");

const WANDER = "e2e-wander";
const GUARD = "e2e-guard";
const SHADOWED = "e2e-shadowed";

test.describe.configure({ mode: "serial" });

let original;
let originalSchema;
test.beforeAll(() => {
  original = {};
  for (const dir of [BEHAVIORS, OVERLAY]) {
    original[dir] = {};
    for (const name of fs.readdirSync(dir)) {
      original[dir][name] = fs.readFileSync(path.join(dir, name), "utf8");
    }
  }
  originalSchema = fs.readFileSync(SCHEMA, "utf8");
});

function restoreFiles() {
  for (const dir of [BEHAVIORS, OVERLAY]) {
    for (const name of fs.readdirSync(dir)) {
      if (!(name in original[dir])) fs.unlinkSync(path.join(dir, name));
    }
    for (const [name, body] of Object.entries(original[dir])) {
      fs.writeFileSync(path.join(dir, name), body);
    }
  }
  fs.writeFileSync(SCHEMA, originalSchema);
}

test.afterAll(restoreFiles);

test.afterEach(async ({ baseURL }) => {
  restoreFiles();
  // Both sessions: this spec edits the schema to break a machine, and a
  // discarded schema edit would otherwise leave every later test looking at a
  // machine that does not validate.
  await fetch(`${baseURL}/forge/schema/discard`, { method: "POST" });
  const resp = await fetch(`${baseURL}/forge/agents/reload`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`could not reset the machines: ${resp.status}`);
});

// Selects by clicking the list rather than building the ?machine= path: the
// path is absolute and belongs to the server's view of the project.
async function openAgents(page, id) {
  await page.goto("/forge/agents");
  await expect(byTestId(page, "agents-mode")).toBeVisible();
  if (!id) return;
  await byTestId(page, `machine-${id}`).click();
  await expect(byTestId(page, "machine-editor")).toHaveAttribute("data-machine", id, {
    timeout: 10_000,
  });
}

test("every machine is listed with the mod it came from", async ({ page }) => {
  await openAgents(page);

  await expect(byTestId(page, `machine-${WANDER}`)).toBeVisible();
  await expect(byTestId(page, `machine-mod-${WANDER}`)).toHaveText("e2e-core");
  await expect(byTestId(page, `machine-mod-${GUARD}`)).toHaveText("e2e-core");
});

// The rule comes out of what the loader actually did, and the only way to see
// it is a project where two mods declare one id.
test("a machine shadowing an earlier mod's is tagged, and names the mod that won", async ({
  page,
}) => {
  await openAgents(page);

  await expect(byTestId(page, `machine-override-${SHADOWED}`)).toBeVisible();
  await expect(byTestId(page, `machine-mod-${SHADOWED}`)).toHaveText("e2e-overlay");
  // And only that one: a machine no later mod touches is not tagged.
  await expect(byTestId(page, `machine-override-${WANDER}`)).toHaveCount(0);

  // The row that resolved is the overlay's, so the editor is showing the file
  // the *game* would run — which is the whole reason the tag is worth having.
  await byTestId(page, `machine-${SHADOWED}`).click();
  await expect(byTestId(page, "machine-source")).toHaveText(`e2e-overlay/${SHADOWED}.json`);
});

test("selecting a machine is a URL that survives a reload", async ({ page }) => {
  await openAgents(page, WANDER);
  const url = page.url();
  expect(url).toContain("machine=");

  await page.reload();
  await expect(byTestId(page, "machine-editor")).toHaveAttribute("data-machine", WANDER);
  expect(page.url()).toBe(url);
});

test("the manifest maps a context key to the component that declares it", async ({ page }) => {
  await openAgents(page, WANDER);

  const row = byTestId(page, "manifest-hp");
  await expect(row).toBeVisible();
  await expect(row).toContainText("Health");
  await expect(byTestId(page, "machine-validity")).toHaveAttribute("data-valid", "true");
});

// The distinction the story exists for. Asserted as two different strings read
// out of the page, because a refactor that collapsed them into one would
// otherwise pass every test that only checks a machine "shows something".
test("a machine that seeds nothing reads differently from one that could not be computed", async ({
  page,
}) => {
  // e2e-guard declares no context at all.
  await openAgents(page, GUARD);
  await expect(byTestId(page, "manifest-empty")).toBeVisible();
  await expect(byTestId(page, "manifest-unavailable")).toHaveCount(0);
  const seedsNothing = (await byTestId(page, "manifest-empty").textContent()).trim();

  // Break e2e-wander by taking away the component field its context maps to.
  // Through the schema editor, because that is how it happens: a machine is
  // rarely broken on its own, it stops validating because the schema moved.
  await page.goto("/forge/schema?component=Health");
  page.once("dialog", (d) => d.accept());
  await byTestId(page, "delete-field-hp").click();
  await expect(byTestId(page, "field-hp")).toHaveCount(0, { timeout: 10_000 });

  await openAgents(page, WANDER);
  await expect(byTestId(page, "manifest-unavailable")).toBeVisible();
  await expect(byTestId(page, "manifest-empty")).toHaveCount(0);
  const notComputed = (await byTestId(page, "manifest-unavailable").textContent()).trim();

  expect(notComputed).not.toBe(seedsNothing);
  // And it says why, rather than leaving an empty box with a heading.
  expect(notComputed).toMatch(/does not validate/i);
  // The reason is on the page too, naming the key that stopped resolving.
  await expect(byTestId(page, "machine-problems")).toContainText("hp");
});

// The readout follows the session, not the page it was rendered on. Two tabs,
// because that is the only way to change the schema without reloading AGENTS —
// and a readout that only updated on navigation would pass every single-tab
// test while being wrong in the way that matters.
test("the validity readout flips both ways without a reload", async ({ page, context }) => {
  await openAgents(page, WANDER);
  const validity = byTestId(page, "machine-validity");
  await expect(validity).toHaveAttribute("data-valid", "true");

  const schemaTab = await context.newPage();
  await schemaTab.goto("/forge/schema?component=Health");
  schemaTab.once("dialog", (d) => d.accept());
  await byTestId(schemaTab, "delete-field-hp").click();
  await expect(byTestId(schemaTab, "field-hp")).toHaveCount(0, { timeout: 10_000 });

  // No reload here: the patch arrives on the stream this page already holds.
  await expect(validity).toHaveAttribute("data-valid", "false", { timeout: 10_000 });
  await expect(validity).toContainText("1 problem");

  // And back: putting the field back makes the machine loadable again.
  await schemaTab.locator('[data-testid="add-field"]').click();
  await expect(byTestId(schemaTab, "field-newField")).toBeVisible({ timeout: 10_000 });
  const name = byTestId(schemaTab, "field-newField").locator("input").first();
  await name.fill("hp");
  await name.press("Enter");
  await expect(byTestId(schemaTab, "field-hp")).toBeVisible({ timeout: 10_000 });

  await expect(validity).toHaveAttribute("data-valid", "true", { timeout: 10_000 });
  await schemaTab.close();
});

test("a file that did not load is reported by name, and is in no list", async ({ page }) => {
  fs.writeFileSync(path.join(BEHAVIORS, "e2e-broken.json"), "{ not json");
  // Nothing watches behaviors/, so a file changed outside Forge needs an
  // explicit re-read. There is no control for it in AGENTS yet — the save
  // report offers one only for a conflict — so the test asks the same endpoint
  // the suite's own reset uses.
  const resp = await page.request.post("/forge/agents/reload");
  expect(resp.status()).toBe(204);

  await page.goto("/forge/agents");
  await expect(byTestId(page, "machine-project-problems")).toContainText("e2e-broken.json");
  // Reported, and not listed as though it were a machine.
  await expect(byTestId(page, "machine-e2e-broken")).toHaveCount(0);
});

// The mod-choice create control. Which mod a machine belongs to decides which
// one can override it later, so the control asks — and until this fixture grew
// a second mod, that branch had never run in a browser at all.
test("creating asks which mod, and the file lands in the one chosen", async ({ page }) => {
  await openAgents(page);

  await expect(byTestId(page, "add-machine-mod")).toBeVisible();
  await expect(byTestId(page, "add-machine")).toHaveCount(0);

  await byTestId(page, "add-machine-mod").locator("select").selectOption("e2e-overlay");
  await expect(byTestId(page, "machine-NewMachine")).toBeVisible({ timeout: 10_000 });

  expect(fs.existsSync(path.join(OVERLAY, "NewMachine.json"))).toBe(true);
  expect(fs.existsSync(path.join(BEHAVIORS, "NewMachine.json"))).toBe(false);
  await expect(byTestId(page, "machine-mod-NewMachine")).toHaveText("e2e-overlay");
});

// Unsaved work on a machine that stops resolving is kept, and this is the only
// place it can be reached from — it is in no list, and every other control works
// from the resolved set.
//
// Stranded through the UI rather than by asking the server to re-read: POST
// /forge/agents/reload is "take theirs" and would discard the very edit this
// test is about. Creating a machine re-resolves while keeping working values,
// which is how this actually happens — you are mid-edit, a later mod turns out
// to declare the same id, and the next thing you do re-resolves.
test("work stranded by a re-resolve can be found and given up", async ({ page }) => {
  await openAgents(page, WANDER);
  const id = byTestId(page, "machine-rename-id");
  await id.fill("e2e-roam");
  await id.press("Enter");
  await expect(byTestId(page, "machine-e2e-roam")).toBeVisible({ timeout: 10_000 });

  // The overlay mod supplies its own e2e-wander, which wins — so the core file
  // the unsaved rename belongs to is no longer the machine.
  fs.writeFileSync(
    path.join(OVERLAY, "e2e-wander.json"),
    fs.readFileSync(path.join(BEHAVIORS, "e2e-wander.json"), "utf8"),
  );
  await byTestId(page, "add-machine-mod").locator("select").selectOption("e2e-core");
  await expect(byTestId(page, "machine-NewMachine")).toBeVisible({ timeout: 10_000 });

  const problems = byTestId(page, "machine-project-problems");
  await expect(problems).toContainText("e2e-wander.json", { timeout: 10_000 });
  await expect(problems).toContainText(/unsaved changes/i);
  // And it is not in the list under the name it was given, because it is not a
  // machine any more.
  await expect(byTestId(page, "machine-e2e-roam")).toHaveCount(0);

  page.once("dialog", (d) => d.accept());
  await byTestId(page, "discard-stranded-e2e-wander.json").click();
  await expect(byTestId(page, "discard-stranded-e2e-wander.json")).toHaveCount(0, {
    timeout: 10_000,
  });
});

test("accessibility", async ({ page }) => {
  await openAgents(page, WANDER);

  // The rail's landmark plus this mode's own list.
  await expect(page.getByRole("navigation", { name: /machines/i })).toBeVisible();
  // The selected row is marked current, not only coloured.
  await expect(byTestId(page, `machine-${WANDER}`)).toHaveAttribute("aria-current", "true");

  // The override tag is a word, so it survives being read out; the mod that won
  // is on the row as text rather than only in a title attribute.
  await expect(byTestId(page, `machine-override-${SHADOWED}`)).toHaveText("override");
  await expect(byTestId(page, `machine-mod-${SHADOWED}`)).toHaveText("e2e-overlay");

  // A manifest row reads as a sentence rather than three unrelated words: the
  // component is the one part whose role is not obvious from its value.
  await expect(byTestId(page, "manifest-hp")).toContainText("from Health");
  // And the panel says the values are not editable here.
  await expect(byTestId(page, "manifest-source")).toContainText(/read-only/i);

  // The validity readout says what it means in words as well as in hue.
  await expect(byTestId(page, "machine-validity")).toContainText(/valid/i);
});
