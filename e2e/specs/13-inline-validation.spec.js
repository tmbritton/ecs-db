// Epic 13 Story 8 — inline validation.
//
// `agent.ValidateMachine` returns every error rather than the first, and its own
// doc comment says why: a first-failure-only report "sends someone round the
// edit-save loop once per mistake". This is what makes that pay off — every
// reason the engine would refuse the machine, shown at once, against the thing
// that caused it.
//
// The part only a browser can check is placement. A message about a node
// scrolled out of view is a message nobody reads, so the claim under test is not
// "the text is on the page" but "the thing to look at is marked, and selecting
// it says why".

const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const PROJECT = path.resolve(__dirname, "../fixtures/project");
const BEHAVIORS = path.join(PROJECT, "behaviors");
const SCHEMA = path.join(PROJECT, "schema.json");
const NESTED = "e2e-nested";
const WANDER = "e2e-wander";

test.describe.configure({ mode: "serial" });

let original;
let originalSchema;
test.beforeAll(() => {
  original = {};
  for (const name of fs.readdirSync(BEHAVIORS)) {
    original[name] = fs.readFileSync(path.join(BEHAVIORS, name), "utf8");
  }
  originalSchema = fs.readFileSync(SCHEMA, "utf8");
});

function restoreFiles() {
  for (const name of fs.readdirSync(BEHAVIORS)) {
    if (!(name in original)) fs.unlinkSync(path.join(BEHAVIORS, name));
  }
  for (const [name, body] of Object.entries(original)) {
    fs.writeFileSync(path.join(BEHAVIORS, name), body);
  }
  fs.writeFileSync(SCHEMA, originalSchema);
}

test.afterAll(restoreFiles);

test.afterEach(async ({ baseURL }) => {
  restoreFiles();
  await fetch(`${baseURL}/forge/agents/menu?close=1`, { method: "POST" });
  for (const route of ["/forge/agents/reload", "/forge/schema/reload"]) {
    const resp = await fetch(`${baseURL}${route}`, { method: "POST" });
    if (resp.status !== 204) throw new Error(`${route}: ${resp.status}`);
  }
});

async function openMachine(page, id) {
  await page.goto("/forge/agents");
  await byTestId(page, `machine-${id}`).click();
  await expect(byTestId(page, "statechart")).toBeVisible();
}

// Deleting a state leaves the transitions that targeted it pointing at nothing.
// Story 5 does that deliberately rather than taking their guards and actions
// with them, which makes it the way a machine becomes invalid from the UI.
async function breakByDeleting(page, statePath) {
  page.once("dialog", (d) => d.accept());
  await byTestId(page, `select-state-${statePath}`).click({ button: "right" });
  await byTestId(page, "canvas-menu").waitFor();
  await page.getByRole("menuitem", { name: /delete state/i }).click();
  await expect(byTestId(page, `state-${statePath}`)).toHaveCount(0, { timeout: 10_000 });
}

test("a transition left pointing at nothing is reported against that edge", async ({ page }) => {
  await openMachine(page, NESTED);
  // The fixture's after-500 transition targets resting.
  await expect(byTestId(page, "edge-idle|after|500|0")).toHaveAttribute("data-invalid", "false");

  await breakByDeleting(page, "resting");

  const edge = byTestId(page, "edge-idle|after|500|0");
  await expect(edge).toHaveAttribute("data-invalid", "true", { timeout: 10_000 });
  // Marked without being selected: a problem you have to find before you can
  // see it is the failure this story exists to prevent.
  await expect(edge).toHaveAttribute("title", /not a known state/);

  await edge.click();
  // Under the control whose value failed, not in a list at the top of the
  // panel: the target dropdown is what holds the target that resolves to
  // nothing, and it points at the message.
  await expect(byTestId(page, "transition-target")).toContainText(
    'transition target "resting" is not a known state',
  );
});

test("the count is stated where the canvas can be seen, and the save is refused", async ({ page }) => {
  await openMachine(page, NESTED);
  await expect(byTestId(page, "canvas-problem-summary")).toHaveCount(0);
  await expect(byTestId(page, "save-footer")).toHaveAttribute("data-blocked", "false");

  await breakByDeleting(page, "resting");

  await expect(byTestId(page, "canvas-problem-summary")).toContainText("1 problem", { timeout: 10_000 });
  await expect(byTestId(page, "save-footer")).toHaveAttribute("data-blocked", "true");
  await expect(byTestId(page, "save-footer")).toContainText("1 problem in 1 machine");
  // Disabled, and Discard is not: it is the way out of an invalid state.
  await expect(page.locator(".save-footer__save")).toBeDisabled();
  await expect(page.locator(".save-footer__discard")).toBeEnabled();
});

// The whole point of ValidateMachine returning a list.
test("two problems produce two messages, not one", async ({ page }) => {
  await openMachine(page, NESTED);
  // Two different transitions, two different targets: POKED aims at
  // combat.attacking and the after aims at resting.
  await breakByDeleting(page, "combat.attacking");
  await breakByDeleting(page, "resting");

  await expect(byTestId(page, "canvas-problem-summary")).toContainText("2 problems", { timeout: 10_000 });
  const marked = page.locator('[data-testid^="edge-"][data-invalid="true"]');
  await expect(marked).toHaveCount(2);
});

test("fixing the problem clears the message without a reload", async ({ page }) => {
  await openMachine(page, NESTED);
  await breakByDeleting(page, "resting");
  await expect(byTestId(page, "canvas-problem-summary")).toContainText("1 problem", { timeout: 10_000 });

  // Point the transition somewhere that exists, through the panel that owns it.
  await byTestId(page, "edge-idle|after|500|0").click();
  await byTestId(page, "transition-target").locator("select").selectOption("combat");

  await expect(byTestId(page, "canvas-problem-summary")).toHaveCount(0, { timeout: 10_000 });
  await expect(byTestId(page, "save-footer")).toHaveAttribute("data-blocked", "false");
});

// A context key is about schema.json as much as about the machine, so it belongs
// to no state — and it must not be hung on an arbitrary node to give it a home.
test("a problem belonging to no state is reported against the machine", async ({ page, baseURL }) => {
  await openMachine(page, WANDER);
  await expect(byTestId(page, "machine-problems")).toHaveCount(0);

  // e2e-wander seeds hp, which Health declares. Take the field away.
  const schema = JSON.parse(fs.readFileSync(SCHEMA, "utf8"));
  delete schema.components.Health.properties.hp;
  fs.writeFileSync(SCHEMA, JSON.stringify(schema, null, 2));
  const resp = await fetch(`${baseURL}/forge/schema/reload`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`reload: ${resp.status}`);

  await expect(byTestId(page, "machine-problems")).toContainText("does not match any component field", {
    timeout: 10_000,
  });
  // And on no node, because it belongs to none.
  await expect(page.locator('[data-testid^="state-"][data-invalid="true"]')).toHaveCount(0);
});

test("accessibility", async ({ page }) => {
  await openMachine(page, NESTED);
  await breakByDeleting(page, "resting");
  await expect(byTestId(page, "canvas-problem-summary")).toBeVisible({ timeout: 10_000 });

  // The mark is named as well as coloured: a difference that exists only as a
  // hue is not available to everyone reading.
  await byTestId(page, "edge-idle|after|500|0").click();
  // Associated with the control, not merely near it: the target dropdown points
  // at the message with aria-describedby, and the list it points at is the one
  // that carries that id.
  const target = byTestId(page, "transition-target").locator("select");
  const described = await target.getAttribute("aria-describedby");
  expect(described, "the target control points at no message").toBeTruthy();
  await expect(page.locator(`#${described}`)).toContainText("not a known state");
  // The severity is stated in words rather than only in red.
  await expect(page.locator(`#${described}`)).toContainText(/error/i);

  // A disabled Save says why, rather than being inert for an invisible reason.
  await expect(page.locator(".save-footer__save")).toHaveAttribute("title", /problem/);
});
