// Epic 13 Story 6 — the state inspector.
//
// The interesting part is the action catalogue. `agent.Registry` has carried a
// description and a parameter schema for every built-in since Epic 2 and had no
// caller; this is it. What makes that worth testing in a browser rather than in
// Go is the claim underneath: the list offered is the engine's own vocabulary
// for *this* project, so an action name is chosen and never typed, and a name
// the engine would refuse is unreachable rather than merely reported.

const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const PROJECT = path.resolve(__dirname, "../fixtures/project");
const BEHAVIORS = path.join(PROJECT, "behaviors");
const NESTED = "e2e-nested";

test.describe.configure({ mode: "serial" });

let original;
test.beforeAll(() => {
  original = {};
  for (const name of fs.readdirSync(BEHAVIORS)) {
    original[name] = fs.readFileSync(path.join(BEHAVIORS, name), "utf8");
  }
});

function restoreFiles() {
  for (const name of fs.readdirSync(BEHAVIORS)) {
    if (!(name in original)) fs.unlinkSync(path.join(BEHAVIORS, name));
  }
  for (const [name, body] of Object.entries(original)) {
    fs.writeFileSync(path.join(BEHAVIORS, name), body);
  }
}

test.afterAll(restoreFiles);

test.afterEach(async ({ baseURL }) => {
  restoreFiles();
  await fetch(`${baseURL}/forge/agents/menu?close=1`, { method: "POST" });
  const resp = await fetch(`${baseURL}/forge/agents/reload`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`could not reset the machines: ${resp.status}`);
});

// entry and exit are arrays in the file — XState accepts a bare value there and
// the emitter writes the array form, so a test reading `.entry.params` reads
// undefined and passes for the wrong reason.
function firstAction(machine, state, kind = "entry") {
  const list = machine.states[state][kind];
  if (!list) return undefined;
  const spec = Array.isArray(list) ? list[0] : list;
  return typeof spec === "string" ? { type: spec } : spec;
}

async function selectState(page, statePath) {
  await page.goto("/forge/agents");
  await byTestId(page, `machine-${NESTED}`).click();
  await byTestId(page, `select-state-${statePath}`).click();
  await expect(byTestId(page, "state-inspector")).toBeVisible();
}

// What the session would save, which is the only place an edit is real.
async function saved(page, baseURL) {
  const resp = await fetch(`${baseURL}/forge/agents/save`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`save: ${resp.status}`);
  return JSON.parse(fs.readFileSync(path.join(BEHAVIORS, `${NESTED}.json`), "utf8"));
}

test("selecting a node fills the inspector with that node's values", async ({ page }) => {
  await page.goto("/forge/agents");
  await byTestId(page, `machine-${NESTED}`).click();
  // Nothing selected: it says what to do rather than showing an empty form.
  await expect(byTestId(page, "inspector-empty")).toBeVisible();

  await byTestId(page, "select-state-idle").click();
  await expect(byTestId(page, "state-name")).toHaveValue("idle");
  // The fixture's idle carries one entry action.
  await expect(byTestId(page, "entry-action-0")).toHaveAttribute("data-action", "pickRandomTarget");

  await byTestId(page, "select-state-resting").click();
  await expect(byTestId(page, "state-name")).toHaveValue("resting");
  await expect(byTestId(page, "entry-action-0")).toHaveCount(0);
});

// The claim this story rests on: the list is the engine's vocabulary for this
// project, not a list someone typed. The gate is buildRegistry's — the engine
// registers the pathfinding builtins only when a map is configured — and this
// fixture gained a [map] in Epic 15 Story 2, so the assertion is that the gate
// *opens*, not that it is shut. The closed side is covered in Go, four times
// over (project_test.go, inspector_test.go, canvasedit_test.go,
// transitionedit_test.go), where a project can be built without one.
test("the action catalogue is the registry's, gated as the engine gates it", async ({ page }) => {
  await selectState(page, "idle");
  const options = byTestId(page, "add-entry-action").locator("option");

  // Selection patches the inspector over SSE. evaluateAll on an empty locator
  // returns [] immediately; wait for the catalogue before reading its values.
  await expect(options.filter({ hasText: /^dealDamage/ })).toHaveCount(1);
  const values = await options.evaluateAll((els) => els.map((e) => e.value));
  expect(values).toContain("dealDamage");
  expect(values).toContain("setAnimation");
  expect(values, "this project has a map, so pathfinding registers").toContain("computePath");
  expect(values, "and so does the action that walks the path it found").toContain("stepAlongPath");

  // Each with its description, so what an action does is there while you are
  // choosing it.
  const labels = await options.evaluateAll((els) => els.map((e) => e.textContent));
  expect(labels.find((l) => l.startsWith("dealDamage"))).toContain("Decrement Health.hp");
});

test("adding dealDamage generates its form from the registry", async ({ page, baseURL }) => {
  await selectState(page, "resting");
  await byTestId(page, "add-entry-action").locator("select").selectOption("dealDamage");
  await expect(byTestId(page, "entry-action-0")).toBeVisible({ timeout: 10_000 });

  // amount is a number and required; target is text, optional, and shows its
  // registered default as a hint rather than as a value.
  //
  // A text input with inputmode=decimal rather than type=number: the value of a
  // number input is empty for anything it cannot parse, and empty is how a
  // parameter is cleared, so half-typing an exponent deleted what was there.
  await expect(byTestId(page, "input-entry-0-amount")).toHaveAttribute("data-param-type", "number");
  await expect(byTestId(page, "input-entry-0-amount")).toHaveAttribute("inputmode", "decimal");
  await expect(byTestId(page, "input-entry-0-target")).toHaveAttribute("placeholder", "$player");
  await expect(byTestId(page, "input-entry-0-target")).toHaveValue("");
  // The description, from the registry.
  await expect(byTestId(page, "entry-desc-0")).toContainText("Decrement Health.hp");

  // Required and empty is reported, and it is a warning: the engine loads the
  // machine and the action fails when it runs, so blocking the save would be a
  // rule the engine does not have.
  await expect(byTestId(page, "problem-entry-0-amount")).toContainText("amount is required");

  await byTestId(page, "input-entry-0-amount").fill("5");
  await byTestId(page, "input-entry-0-amount").blur();
  await expect
    .poll(async () => firstAction(await saved(page, baseURL), "resting"), { timeout: 10_000 })
    .toBeTruthy();

  // A number, not the string "5": the engine reads it as its declared type.
  expect(firstAction(await saved(page, baseURL), "resting").params.amount).toBe(5);
  // And the message goes when the value arrives.
  await expect(byTestId(page, "problem-entry-0-amount")).toHaveCount(0);
});

// The round trip an object parameter has to survive: file → textarea → post →
// file. It did not. A Go map printed as "map[hp:3 max:10]", which has never
// been JSON, so the textarea showed something the field would refuse the moment
// it was touched — and nothing on screen said the text was not the value.
test("an object parameter can be edited twice", async ({ page, baseURL }) => {
  await selectState(page, "resting");
  await byTestId(page, "add-entry-action").locator("select").selectOption("attachComponent");
  await expect(byTestId(page, "entry-action-0")).toBeVisible({ timeout: 10_000 });

  const data = byTestId(page, "input-entry-0-data");
  await data.fill('{"hp": 3, "max": 10}');
  await data.blur();
  await expect
    .poll(async () => firstAction(await saved(page, baseURL), "resting")?.params?.data?.hp, {
      timeout: 10_000,
    })
    .toBe(3);

  // What the field now holds is what the file holds, so a second edit works.
  await expect(data).toHaveValue('{"hp":3,"max":10}');
  await data.fill('{"hp": 4, "max": 10}');
  await data.blur();
  await expect
    .poll(async () => firstAction(await saved(page, baseURL), "resting")?.params?.data?.hp, {
      timeout: 10_000,
    })
    .toBe(4);
  // And no refusal was reported along the way.
  await expect(byTestId(page, "edit-problem")).toHaveCount(0);
});

// An input type=number reports an empty value for anything it cannot parse, and
// empty is how a parameter is cleared — so typing an exponent on the way to a
// number deleted the value that was already there.
test("half-typing a number does not delete the value that was there", async ({
  page,
  baseURL,
}) => {
  await selectState(page, "resting");
  await byTestId(page, "add-entry-action").locator("select").selectOption("dealDamage");
  await expect(byTestId(page, "entry-action-0")).toBeVisible({ timeout: 10_000 });

  const amount = byTestId(page, "input-entry-0-amount");
  await amount.fill("5");
  await amount.blur();
  await expect
    .poll(async () => firstAction(await saved(page, baseURL), "resting")?.params?.amount, {
      timeout: 10_000,
    })
    .toBe(5);

  // Mid-way through typing 1e5.
  await amount.fill("1e");
  await amount.blur();

  // The refusal is asserted before the file, and the order is load-bearing:
  // saved() is not a read. It POSTs /forge/agents/save, and a save that
  // succeeds clears the refusal — so reading the file first destroys the thing
  // the next line is looking for.
  //
  // That used to be invisible. The clearing reached the browser on the next
  // two-second poll, which was after this assertion had already run, so the
  // test passed against a DOM that was two seconds out of date. Now that an
  // edit is pushed the moment it happens, a stale DOM is no longer available to
  // hide behind.
  await expect(byTestId(page, "edit-problem")).toContainText("amount takes a number");

  // And the value is untouched, rather than the parameter quietly disappearing.
  expect(firstAction(await saved(page, baseURL), "resting").params.amount).toBe(5);

  // And finishing the number works.
  await amount.fill("1e5");
  await amount.blur();
  await expect
    .poll(async () => firstAction(await saved(page, baseURL), "resting")?.params?.amount, {
      timeout: 10_000,
    })
    .toBe(100000);
});

test("removing an action removes it from the file", async ({ page, baseURL }) => {
  await selectState(page, "idle");
  expect((await saved(page, baseURL)).states.idle.entry).toBeTruthy();

  await byTestId(page, "remove-entry-0").click();
  await expect
    .poll(async () => (await saved(page, baseURL)).states.idle.entry, { timeout: 10_000 })
    .toBeUndefined();
});

test("renaming a state updates the canvas and the transitions that target it", async ({
  page,
  baseURL,
}) => {
  await selectState(page, "resting");
  // The fixture's idle has an after transition targeting resting.
  expect(JSON.stringify((await saved(page, baseURL)).states.idle.after)).toContain("resting");

  await byTestId(page, "state-name").fill("napping");
  await byTestId(page, "state-name").blur();

  await expect(byTestId(page, "state-napping")).toBeVisible({ timeout: 10_000 });
  const machine = await saved(page, baseURL);
  expect(machine.states.napping).toBeDefined();
  expect(machine.states.resting).toBeUndefined();
  // The transition follows, or a rename silently dangles it.
  expect(JSON.stringify(machine.states.idle.after)).toContain("napping");
});

test("set as initial moves the tag on the canvas, and is absent where it would do nothing", async ({
  page,
  baseURL,
}) => {
  await selectState(page, "idle");
  // idle is already the initial state, so the control is absent rather than
  // disabled: an action that does nothing is worse than one that is not there.
  await expect(byTestId(page, "already-initial")).toBeVisible();
  await expect(byTestId(page, "set-initial")).toHaveCount(0);

  await byTestId(page, "select-state-resting").click();
  await expect(byTestId(page, "set-initial")).toContainText("the machine starts in");
  await byTestId(page, "set-initial").click();

  await expect(byTestId(page, "state-resting")).toHaveAttribute("data-initial", "true", {
    timeout: 10_000,
  });
  expect((await saved(page, baseURL)).initial).toBe("resting");
});

// A nested state's initial belongs to the compound state that holds it, and the
// machine's is a different field.
test("a compound state's initial is its own", async ({ page, baseURL }) => {
  await selectState(page, "combat.fleeing");
  await expect(byTestId(page, "set-initial")).toContainText("the state combat enters");
  await byTestId(page, "set-initial").click();

  await expect
    .poll(async () => (await saved(page, baseURL)).states.combat.initial, { timeout: 10_000 })
    .toBe("fleeing");
  // The machine's is untouched.
  expect((await saved(page, baseURL)).initial).toBe("idle");
});

test("accessibility", async ({ page }) => {
  await selectState(page, "resting");
  await expect(page.getByRole("complementary", { name: /state inspector/i })).toBeVisible();

  await byTestId(page, "add-entry-action").locator("select").selectOption("dealDamage");
  await expect(byTestId(page, "entry-action-0")).toBeVisible({ timeout: 10_000 });

  // The generated inputs are labelled: each sits inside its own <label> with
  // the parameter's name, so a screen reader announces which field it is.
  const amount = byTestId(page, "input-entry-0-amount");
  await expect(amount).toHaveAccessibleName(/amount/i);

  // And the required-parameter message is associated with its field rather than
  // merely near it — the pattern Epic 12 Story 7 established.
  const described = await amount.getAttribute("aria-describedby");
  expect(described, "the input points at no message").toBeTruthy();
  await expect(page.locator(`#${described}`)).toContainText("amount is required");
  // And not marked invalid: this is a warning, the file is one the engine
  // accepts, and the save is not blocked — aria-invalid would say otherwise.
  await expect(amount).not.toHaveAttribute("aria-invalid", "true");

  // The remove control is a named button, not a bare glyph.
  await expect(byTestId(page, "remove-entry-0")).toHaveAccessibleName(/remove dealDamage/i);

  // The list says it is closed, rather than looking extensible: a modder cannot
  // add to it without Go and a rebuild.
  await expect(byTestId(page, "catalogue-is-closed")).toContainText(/rebuild/i);
});
