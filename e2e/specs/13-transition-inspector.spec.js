// Epic 13 Story 7 — the transition inspector.
//
// The guard half is Story 6's argument pointed at the other catalogue:
// `Registry.Guards()` has carried a description and a parameter schema since
// Epic 2 and had no caller, and offering exactly what the engine registers makes
// "guard is not registered" unreachable rather than merely reported.
//
// The order half is what only a browser can check. A transition's position among
// the transitions on its event is the machine's logic — XState takes the first
// whose guard passes — and the chart's edge id is positional, so reordering
// moves the thing that is selected. Following it takes a server-computed
// redirect, and a selection that did not follow would make the button refuse to
// repeat: the second click would undo the first.

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

// What the session would save, which is the only place an edit is real.
async function saved(baseURL) {
  const resp = await fetch(`${baseURL}/forge/agents/save`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`save: ${resp.status}`);
  return JSON.parse(fs.readFileSync(path.join(BEHAVIORS, `${NESTED}.json`), "utf8"));
}

// A transition's key holds an array in the file, and XState also accepts a bare
// object there — so a test reading `.on.GO.cond` reads undefined and passes for
// the wrong reason.
function transitions(machine, state, event, kind = "on") {
  const at = machine.states[state][kind][event];
  return Array.isArray(at) ? at : [at];
}

async function selectEdge(page, id) {
  await page.goto("/forge/agents");
  await byTestId(page, `machine-${NESTED}`).click();
  await byTestId(page, `edge-${id}`).click();
  await expect(byTestId(page, "transition-inspector")).toBeVisible();
}

test("selecting an edge fills the panel with that transition's values", async ({ page }) => {
  await page.goto("/forge/agents");
  await byTestId(page, `machine-${NESTED}`).click();
  await expect(byTestId(page, "inspector-empty")).toBeVisible();

  await byTestId(page, "edge-idle|on|POKED|0").click();
  await expect(byTestId(page, "transition-event")).toHaveValue("POKED");
  // Source and target, both readable — not only visible as two boxes and a line.
  await expect(byTestId(page, "transition-route")).toContainText("idle → combat.attacking");
  await expect(byTestId(page, "transition-guard").locator("select")).toHaveValue("inRange");

  // And the state panel is not also on screen: one rail, one selection.
  await expect(byTestId(page, "state-inspector")).toHaveCount(0);
});

// The same claim Story 6 makes about actions, against the other catalogue. The
// fixture has no map, so the engine would not register inLineOfSight — and a
// machine using it would not load.
test("the guard dropdown is the registry's, plus an explicit none", async ({ page }) => {
  await selectEdge(page, "idle|on|SPOTTED|0");
  const options = byTestId(page, "transition-guard").locator("option");

  const values = await options.evaluateAll((els) => els.map((e) => e.value));
  expect(values, "there is no way to say a transition has no guard").toContain("");
  expect(values).toContain("inRange");
  expect(values).toContain("healthAbove");
  expect(values, "inLineOfSight needs a map this project has not got").not.toContain("inLineOfSight");
  expect(values, "and so does pathfinding's guard").not.toContain("pathComplete");

  // Each with its description, so what a guard does is there while you are
  // choosing it.
  const labels = await options.evaluateAll((els) => els.map((e) => e.textContent));
  expect(labels.find((l) => l.startsWith("inRange"))).toContain("distance");
});

test("choosing a guard generates its parameters from the registered schema", async ({ page, baseURL }) => {
  await selectEdge(page, "idle|on|SPOTTED|0");
  await byTestId(page, "transition-guard").locator("select").selectOption("inRange");

  // inRange takes target (string, required) and distance (number, required).
  // The generated form is Story 6's, which is what the shared id shape says:
  // input-<scope>-<param>, with the scope naming the form rather than a second
  // component naming itself.
  await expect(byTestId(page, "input-guard-distance")).toBeVisible({ timeout: 10_000 });
  await expect(byTestId(page, "input-guard-distance")).toHaveAttribute("data-param-type", "number");
  await expect(byTestId(page, "input-guard-distance")).toHaveAttribute("inputmode", "decimal");
  await expect(byTestId(page, "input-guard-target")).toHaveAttribute("data-param-type", "string");

  await byTestId(page, "input-guard-distance").fill("4");
  await byTestId(page, "input-guard-distance").blur();
  await expect(byTestId(page, "input-guard-distance")).toHaveValue("4", { timeout: 10_000 });

  const machine = await saved(baseURL);
  const cond = transitions(machine, "idle", "SPOTTED")[0].cond;
  expect(cond.type).toBe("inRange");
  // The number, not the string: the engine reads a param as its declared type.
  expect(cond.params.distance).toBe(4);
});

// A guard with nothing on it comes back as the shorthand it would have been
// authored as, rather than expanded into an object — or every save rewrites
// files nobody edited.
test("clearing the guard removes cond entirely rather than writing an empty one", async ({ page, baseURL }) => {
  await selectEdge(page, "idle|on|POKED|0");
  await expect(byTestId(page, "transition-guard").locator("select")).toHaveValue("inRange");

  await byTestId(page, "transition-guard").locator("select").selectOption("");
  await expect(byTestId(page, "transition-guard").locator("select")).toHaveValue("", { timeout: 10_000 });

  const machine = await saved(baseURL);
  const t = transitions(machine, "idle", "POKED")[0];
  expect(t.cond, "an empty cond was written where there should be none").toBeUndefined();
  expect("cond" in t).toBe(false);

  // And the canvas draws it as an unguarded transition now.
  await expect(byTestId(page, "edge-idle|on|POKED|0")).toHaveAttribute("data-guard", "", { timeout: 10_000 });
});

test("the target dropdown offers only this machine's states", async ({ page, baseURL }) => {
  await selectEdge(page, "idle|on|SPOTTED|0");
  const values = await byTestId(page, "transition-target")
    .locator("option")
    .evaluateAll((els) => els.map((e) => e.value));

  expect(values).toEqual(["", "idle", "combat", "combat.attacking", "combat.fleeing", "combat.back", "resting"]);
  expect(values, "a state from another machine leaked in").not.toContain("watching");

  // Changing it moves the edge on the canvas, with no reload.
  await expect(byTestId(page, "edge-idle|on|SPOTTED|0")).toHaveAttribute("data-to", "combat");
  await byTestId(page, "transition-target").locator("select").selectOption("resting");
  await expect(byTestId(page, "edge-idle|on|SPOTTED|0")).toHaveAttribute("data-to", "resting", { timeout: 10_000 });

  const machine = await saved(baseURL);
  expect(transitions(machine, "idle", "SPOTTED")[0].target).toBe("resting");
});

// An after transition's key is a duration, and the parser is the engine's own —
// so what it accepts here is exactly what the engine will accept at load.
test("an after transition shows a duration, and refuses one the engine cannot read", async ({ page, baseURL }) => {
  await selectEdge(page, "idle|after|500|0");
  await expect(byTestId(page, "transition-event")).toHaveValue("500");
  await expect(byTestId(page, "duration-hint")).toContainText("1s");

  await byTestId(page, "transition-event").fill("1 second");
  await byTestId(page, "transition-event").blur();
  // Reported against the field, with a spelling that works — nothing else on
  // screen says what a duration looks like.
  await expect(byTestId(page, "problem-transition-event")).toContainText("1s", { timeout: 10_000 });
  // The machine keeps its duration — the edge is still keyed on 500 — while the
  // field keeps what was typed. Snapping the text back would throw away the
  // attempt with no more explanation than the message already gives.
  await expect(byTestId(page, "edge-idle|after|500|0")).toBeVisible();
  await expect(byTestId(page, "transition-event")).toHaveValue("1 second");

  await byTestId(page, "transition-event").fill("1s");
  await byTestId(page, "transition-event").blur();
  // The key moved, so the selection had to move with it.
  await page.waitForURL(/sel=edge%3Aidle%7Cafter%7C1s%7C0/, { timeout: 10_000 });
  await expect(byTestId(page, "transition-event")).toHaveValue("1s");

  const machine = await saved(baseURL);
  expect(machine.states.idle.after["1s"]).toBeTruthy();
  expect(machine.states.idle.after["500"]).toBeUndefined();
});

// The order is the logic: XState takes the first transition on an event whose
// guard passes, so reordering changes what the machine does.
//
// The fork is built here rather than baked into the fixture, and that is
// deliberate twice over. It exercises the merge — renaming an event onto one
// that already exists appends, which is the only sane default because appended
// means lowest priority — and it leaves e2e-nested's layout alone, which four
// other specs click on by position.
async function forkSpotted(page) {
  await selectEdge(page, "idle|on|POKED|0");
  await byTestId(page, "transition-event").fill("SPOTTED");
  await byTestId(page, "transition-event").blur();
  // Appended, so it is index 1 — and only the server knew that, which is why
  // the answer is a redirect and not a 204.
  await page.waitForURL(/sel=edge%3Aidle%7Con%7CSPOTTED%7C1/, { timeout: 10_000 });
}

test("two transitions on one event keep file order, and reordering changes the file", async ({
  page,
  baseURL,
}) => {
  await forkSpotted(page);

  const labels = () => byTestId(page, "transition-order").locator("li .order-row__label");
  // Each row says where it goes as well as what fires it: on this list the
  // event is the one thing every row shares.
  await expect(labels()).toHaveText([
    "SPOTTED → combat",
    "SPOTTED [inRange] → combat.attacking",
  ]);
  await expect(byTestId(page, "order-1")).toHaveAttribute("data-current", "true");
  // Nothing below an unconditional transition can ever fire. A warning, not an
  // error: ValidateMachine does not check it.
  await expect(byTestId(page, "unreachable-warning")).toBeVisible();
  await expect(byTestId(page, "move-down-1")).toBeDisabled();

  await byTestId(page, "move-up-1").click();
  // The selection follows the transition, or the button would not repeat: the
  // second click would move whatever took the old index back again.
  await page.waitForURL(/sel=edge%3Aidle%7Con%7CSPOTTED%7C0/, { timeout: 10_000 });
  await expect(labels()).toHaveText([
    "SPOTTED [inRange] → combat.attacking",
    "SPOTTED → combat",
  ]);
  await expect(byTestId(page, "order-0")).toHaveAttribute("data-current", "true");
  await expect(byTestId(page, "unreachable-warning")).toHaveCount(0);

  const machine = await saved(baseURL);
  expect(transitions(machine, "idle", "SPOTTED").map((t) => t.target)).toEqual([
    "combat.attacking",
    "combat",
  ]);
  expect(machine.states.idle.on.POKED, "the emptied event key was left behind").toBeUndefined();
});

test("the transition's own actions use the same list and the same generated form", async ({ page, baseURL }) => {
  await selectEdge(page, "idle|on|NUDGED|0");
  // The fixture's NUDGED is internal: actions and no target.
  await expect(byTestId(page, "taction-action-0")).toHaveAttribute("data-action", "log");
  await expect(byTestId(page, "transition-target").locator("select")).toHaveValue("");

  await byTestId(page, "add-taction-action").locator("select").selectOption("dealDamage");
  await expect(byTestId(page, "taction-action-1")).toBeVisible({ timeout: 10_000 });
  // The same id shape as a state's action parameters — one component, not two.
  await expect(byTestId(page, "input-taction-1-amount")).toHaveAttribute("data-param-type", "number");

  await byTestId(page, "input-taction-1-amount").fill("6");
  await byTestId(page, "input-taction-1-amount").blur();
  await expect(byTestId(page, "input-taction-1-amount")).toHaveValue("6", { timeout: 10_000 });

  const machine = await saved(baseURL);
  const actions = transitions(machine, "idle", "NUDGED")[0].actions;
  expect(actions[1]).toEqual({ type: "dealDamage", params: { amount: 6 } });
});

test("deleting the transition is offered here as well as from the canvas menu", async ({ page, baseURL }) => {
  await selectEdge(page, "idle|on|NUDGED|0");
  page.once("dialog", (d) => {
    expect(d.message()).toContain("idle");
    d.accept();
  });
  await byTestId(page, "delete-transition").click();
  await expect(byTestId(page, "edge-idle|on|NUDGED|0")).toHaveCount(0, { timeout: 10_000 });

  const machine = await saved(baseURL);
  expect(machine.states.idle.on.NUDGED, "the event key was left behind with nothing on it").toBeUndefined();
});

test("accessibility", async ({ page }) => {
  await forkSpotted(page);
  await expect(page.getByRole("complementary", { name: /transition inspector/i })).toBeVisible();

  // The event field is a text input with suggestions, not a dropdown: an event
  // name is authored rather than registered, so a dropdown could not express a
  // new one.
  await expect(byTestId(page, "transition-event")).toHaveAccessibleName(/event/i);
  await expect(byTestId(page, "transition-guard").locator("select")).toHaveAccessibleName(/guard/i);
  await expect(byTestId(page, "transition-target").locator("select")).toHaveAccessibleName(/target/i);

  // The reorder controls are named buttons, not bare glyphs — ↑ and ↓ announce
  // as nothing useful.
  await expect(byTestId(page, "move-down-0")).toHaveAccessibleName(/move .* later/i);
  await expect(byTestId(page, "move-up-1")).toHaveAccessibleName(/move .* earlier/i);

  // A guard parameter's required message is associated with its field rather
  // than merely near it — but not marked invalid, because it is a warning and
  // the save is not blocked. The forked transition is POKED's, so it already
  // carries inRange.
  const distance = byTestId(page, "input-guard-distance");
  await expect(distance).toBeVisible({ timeout: 10_000 });
  await expect(distance).toHaveAccessibleName(/distance/i);
  const described = await distance.getAttribute("aria-describedby");
  expect(described, "the input points at no message").toBeTruthy();
  await expect(page.locator(`#${described}`)).toContainText("distance is required");
  await expect(distance).not.toHaveAttribute("aria-invalid", "true");
});
