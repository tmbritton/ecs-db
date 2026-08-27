// Epic 13 Story 4 — the statechart canvas, server-rendered.
//
// Three things here can only be shown in a browser. The first is z-order: an
// edge label that falls over a state has to be the thing that gets clicked, and
// no Go test can tell you which element is on top. The second is that selection
// is a URL — it changes the address bar, it survives a reload, and it comes
// back from a link rather than from client state. The third is that the chart
// is stable on the page stream: two renders of an unchanged machine are
// byte-identical, so the patch is suppressed and nothing on the canvas moves.

const fs = require("fs");
const path = require("path");
const { test, expect, byTestId } = require("../fixtures");

const PROJECT = path.resolve(__dirname, "../fixtures/project");
const BEHAVIORS = path.join(PROJECT, "behaviors");

const NESTED = "e2e-nested";
const WANDER = "e2e-wander";

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
  // reload rather than discard: this spec both edits files on disk and renames
  // a machine in the session, and only "take theirs" undoes both.
  const resp = await fetch(`${baseURL}/forge/agents/reload`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`could not reset the machines: ${resp.status}`);
});

// openMachine goes to AGENTS and selects one machine by its row, so the test
// never has to know the absolute path the session keyed it on.
async function openMachine(page, id) {
  await page.goto("/forge/agents");
  await byTestId(page, `machine-${id}`).click();
  await expect(byTestId(page, "statechart")).toBeVisible();
}

test("draws a node per state and an edge per transition", async ({ page }) => {
  await openMachine(page, NESTED);

  // Every state in the file, nested ones included, and no more than that.
  await expect(page.locator("[data-path]")).toHaveCount(6);
  for (const p of ["idle", "combat", "combat.attacking", "combat.fleeing", "combat.back", "resting"]) {
    await expect(byTestId(page, `state-${p}`)).toBeAttached();
  }

  // The transitions, labelled with what fires them. after carries its duration,
  // because that is what the key is and it has no event name to fall back on.
  await expect(byTestId(page, "edge-idle|on|SPOTTED|0")).toHaveText("SPOTTED");
  await expect(byTestId(page, "edge-idle|after|500|0")).toHaveText("after 500");
  // A guard is the difference between "this happens" and "this might".
  const guarded = byTestId(page, "edge-idle|on|POKED|0");
  await expect(guarded).toHaveText("POKED [inRange]");
  await expect(guarded).toHaveAttribute("data-guard", "inRange");
  await expect(byTestId(page, "edge-idle|on|SPOTTED|0")).toHaveAttribute("data-guard", "");
});

test("compound states hold their children, and history nodes are history", async ({ page }) => {
  await openMachine(page, NESTED);

  // Containment in the DOM, not merely in the picture: the machine is a tree.
  const child = byTestId(page, "state-combat").locator('[data-path="combat.attacking"]');
  await expect(child).toHaveCount(1);
  await expect(byTestId(page, "state-combat")).toHaveAttribute("data-kind", "compound");

  const history = byTestId(page, "state-combat.back");
  await expect(history).toHaveAttribute("data-kind", "history");
  await expect(history).toContainText("H*");

  // And the child is drawn inside its parent's box, not merely inside its
  // markup — the containment has to be true on screen as well.
  const parentBox = await byTestId(page, "state-combat").boundingBox();
  const childBox = await byTestId(page, "state-combat.attacking").boundingBox();
  expect(childBox.x).toBeGreaterThanOrEqual(parentBox.x);
  expect(childBox.y).toBeGreaterThanOrEqual(parentBox.y);
  expect(childBox.x + childBox.width).toBeLessThanOrEqual(parentBox.x + parentBox.width);
});

test("the initial state is marked, and it is the one the file names", async ({ page }) => {
  await openMachine(page, NESTED);

  // Read out of the file rather than typed in here, so this cannot pass by
  // agreeing with a copy of the fixture that has drifted.
  const file = JSON.parse(original["e2e-nested.json"]);
  await expect(byTestId(page, `state-${file.initial}`)).toHaveAttribute("data-initial", "true");
  // Exactly one at the top level, and one inside the compound state.
  await expect(page.locator('[data-path]:not([data-path*="."])[data-initial="true"]')).toHaveCount(1);
  await expect(
    byTestId(page, `state-combat.${file.states.combat.initial}`),
  ).toHaveAttribute("data-initial", "true");
});

test("clicking a node selects it, and a reload keeps it selected", async ({ page }) => {
  await openMachine(page, NESTED);
  await expect(byTestId(page, "canvas-selection")).toHaveText("nothing selected");

  await byTestId(page, "select-state-combat.attacking").click();

  await expect(page).toHaveURL(/sel=state%3Acombat\.attacking/);
  await expect(byTestId(page, "state-combat.attacking")).toHaveAttribute("data-selected", "true");
  await expect(byTestId(page, "canvas-selection")).toHaveText("state combat.attacking");

  await page.reload();
  await expect(byTestId(page, "state-combat.attacking")).toHaveAttribute("data-selected", "true");
});

test("selecting one thing deselects the other", async ({ page }) => {
  await openMachine(page, NESTED);

  await byTestId(page, "select-state-idle").click();
  await expect(page.locator('[data-selected="true"]')).toHaveCount(1);

  await byTestId(page, "edge-idle|on|SPOTTED|0").click();
  await expect(page.locator('[data-selected="true"]')).toHaveCount(1);
  await expect(byTestId(page, "edge-idle|on|SPOTTED|0")).toHaveAttribute("data-selected", "true");
  await expect(byTestId(page, "state-idle")).toHaveAttribute("data-selected", "false");

  // And the ground lets go of both.
  await byTestId(page, "canvas-ground").click();
  await expect(page.locator('[data-selected="true"]')).toHaveCount(0);
  await expect(byTestId(page, "canvas-selection")).toHaveText("nothing selected");
});

// The one thing no Go test can answer: which element is on top. An edge label
// sitting over a state has to be the thing that gets clicked, or every
// transition whose midpoint happens to land on a node is unreachable.
test("clicking an edge over a node selects the edge, not the node", async ({ page, baseURL }) => {
  // Three states, placed so the A→B label lands squarely on C.
  fs.writeFileSync(
    path.join(BEHAVIORS, "e2e-overlap.json"),
    JSON.stringify(
      {
        id: "e2e-overlap",
        initial: "a",
        states: {
          a: { meta: { forge: { x: 0, y: 0 } }, on: { GO: [{ target: "b" }] } },
          b: { meta: { forge: { x: 600, y: 0 } } },
          c: { meta: { forge: { x: 300, y: 0 } } },
        },
      },
      null,
      2,
    ),
  );
  const resp = await fetch(`${baseURL}/forge/agents/reload`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`could not pick up the new machine: ${resp.status}`);

  await openMachine(page, "e2e-overlap");

  const label = byTestId(page, "edge-a|on|GO|0");
  const over = byTestId(page, "state-c");
  const l = await label.boundingBox();
  const c = await over.boundingBox();
  // The premise, stated: if the label were not over the node, clicking it would
  // prove nothing about which is on top.
  expect(l.x).toBeGreaterThanOrEqual(c.x);
  expect(l.x + l.width).toBeLessThanOrEqual(c.x + c.width);
  expect(l.y).toBeGreaterThanOrEqual(c.y);
  expect(l.y + l.height).toBeLessThanOrEqual(c.y + c.height);

  await label.click();
  await expect(page).toHaveURL(/sel=edge%3A/);
  await expect(label).toHaveAttribute("data-selected", "true");
  await expect(over).toHaveAttribute("data-selected", "false");
});

// The reachable route to a dangling edge, and it is not contrived: renaming a
// machine's id rewrites its states' derived ids and does not rewrite the
// transitions that name them, so every fully-qualified target in the file stops
// resolving. That is a defect in Story 2's rename, and until this story there
// was nothing on screen that could show it — which is the whole argument for
// drawing a broken transition rather than dropping it.
test("a transition to a state that does not exist still draws", async ({ page }) => {
  await openMachine(page, NESTED);
  const edge = byTestId(page, "edge-combat.fleeing|on|RECOVER|0");
  await expect(edge).toHaveAttribute("data-dangling", "false");

  await byTestId(page, "machine-rename-id").fill("e2e-renamed");
  await byTestId(page, "machine-rename-id").blur();

  await expect(edge).toHaveAttribute("data-dangling", "true", { timeout: 10_000 });
  // Naming the state that is missing is what makes it fixable.
  // The label's own text, not the anchor's: a dangling edge is also an invalid
  // one, so Story 8 adds a mark beside it.
  await expect(edge.locator(".chart-label__text")).toHaveText("RECOVER → e2e-nested.idle?");
  await expect(edge).toBeVisible();

  // And the machine still draws around it: this is the editor for fixing it.
  await expect(page.locator("[data-path]")).toHaveCount(6);
  await expect(byTestId(page, "machine-validity")).toHaveAttribute("data-valid", "false");
});

test("an edit made elsewhere redraws the canvas on the stream", async ({ page, baseURL }) => {
  await openMachine(page, NESTED);
  await expect(byTestId(page, "state-resting")).toBeAttached();

  // Another tool edits the file. Nothing here reloads the page.
  const edited = JSON.parse(original["e2e-nested.json"]);
  delete edited.states.resting;
  delete edited.states.idle.after;
  fs.writeFileSync(path.join(BEHAVIORS, "e2e-nested.json"), JSON.stringify(edited, null, 2));
  await fetch(`${baseURL}/forge/agents/reload`, { method: "POST" });

  await expect(byTestId(page, "state-resting")).toHaveCount(0, { timeout: 10_000 });
  await expect(byTestId(page, "state-idle")).toBeAttached();
});

// The chart is on a two-second stream that suppresses a patch when the markup
// is unchanged. Any map-iteration order leaking into the output both flickers
// the canvas and defeats the suppression, so the frame count is the assertion.
test("nothing on the canvas moves between two renders", async ({ page }) => {
  await openMachine(page, NESTED);
  await byTestId(page, "select-state-combat.attacking").click();

  // The URL the page's own subscription opens, read out of the attribute that
  // opens it — not rebuilt from the address bar. The two are only the same if
  // streamQuery carries the selection, which is the thing most worth catching
  // here: a stream that dropped it would re-render the mode content with
  // nothing selected on its very first frame.
  const stream = await page.getAttribute("[data-init]", "data-init").then((init) => {
    const m = /@get\('([^']+)'\)/.exec(init) || /['"](\/forge\/[^'"]*events[^'"]*)['"]/.exec(init);
    if (!m) throw new Error(`could not read the subscription out of: ${init}`);
    return m[1];
  });
  expect(stream, "the subscription does not carry the selection").toContain("sel=");

  const frames = await page.evaluate(async (src) => {
    const resp = await fetch(src);
    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let text = "";
    const deadline = Date.now() + 6000;
    while (Date.now() < deadline) {
      const chunk = await Promise.race([
        reader.read(),
        new Promise((r) => setTimeout(() => r({ done: true, timedOut: true }), deadline - Date.now())),
      ]);
      if (chunk.timedOut || chunk.done) break;
      text += decoder.decode(chunk.value, { stream: true });
    }
    await reader.cancel();
    return {
      // Anchored on the leading space: data-testid="mode-content" would
      // otherwise match too and report two patches where there was one.
      // The mode is patched as its regions now, not as one <main>. The chart
      // lives in mode-main, which is the one worth counting here.
      content: (text.match(/ id="mode-main"/g) || []).length,
      chart: (text.match(/data-testid="statechart"/g) || []).length,
    };
  }, stream);

  expect(frames.content, `mode content sent ${frames.content} times in ~6s`).toBe(1);
  expect(frames.chart, `the canvas was redrawn ${frames.chart} times in ~6s`).toBe(1);
});

test("accessibility", async ({ page }) => {
  await openMachine(page, NESTED);

  // Every node is a link, so the canvas can be walked with a keyboard rather
  // than only pointed at.
  const node = byTestId(page, "select-state-idle");
  await expect(node).toHaveRole("link");
  // What kind of state it is, since the borders that say so are only borders.
  await expect(byTestId(page, "select-state-combat")).toHaveAttribute("title", "compound state");
  await expect(byTestId(page, "select-state-combat.back")).toHaveAttribute("title", "deep history");

  await node.click();
  // The selected node is marked current, not only coloured.
  await expect(node).toHaveAttribute("aria-current", "true");
  // And what is selected is said in words, not left to a border colour.
  await expect(byTestId(page, "canvas-selection")).toHaveText("state idle");

  // The ground is a link with no text, so it carries its own name.
  await expect(byTestId(page, "canvas-ground")).toHaveAttribute("aria-label", "Clear selection");

  // The edge layer is decoration around elements that carry the meaning; a
  // screen reader reading out a pile of <path> coordinates helps nobody.
  await expect(page.locator("svg.chart__edges")).toHaveAttribute("aria-hidden", "true");
  // The labels are not — they are the transitions, and they are links.
  await expect(byTestId(page, "edge-idle|on|SPOTTED|0")).toHaveRole("link");
});

// A node's box is sized by the server — chart.headHeight is titleH plus one
// line per entry action, and the SVG edges are routed to those coordinates. So
// the box cannot grow to fit its text, and the stylesheet has to keep the
// one-line-per-entry promise the arithmetic is built on.
//
// It did not. `.chart-node__entry` had no white-space rule, so an action wider
// than the node wrapped: a state with three entry actions drew four lines in a
// box built for three and the last one hung out through the bottom border, over
// the canvas and over whatever edge label was behind it. The head's `gap: 2px`
// was the same bug in miniature — a per-entry 2px the server never budgeted.
//
// Only a browser can see this. The markup is correct either way, every Go test
// passes, and the geometry is right in the style attribute; what is wrong is
// what the text does inside it.
test("a node's entry actions stay inside the node", async ({ page, baseURL }) => {
  // Long enough to have wrapped at the old width, which is the whole point: the
  // fixture's own "pickRandomTarget" fits, and a test that used it would pass
  // against the bug.
  fs.writeFileSync(
    path.join(BEHAVIORS, "e2e-long-entry.json"),
    JSON.stringify(
      {
        id: "e2e-long-entry",
        initial: "wandering",
        states: {
          wandering: {
            // The real shape, and the real names: these render as
            // "setAnimation · goblin_walk", which is what did not fit.
            entry: [
              { type: "setAnimation", params: { animation: "goblin_walk" } },
              { type: "pickRandomTarget", params: { radius: 5 } },
              // log rather than computePath, so this fixture keeps saying the
              // same thing whether or not the project has a map. What is being
              // drawn here is a node with three entry actions, not the gate.
              { type: "log" },
            ],
            meta: { forge: { x: 40, y: 40 } },
          },
        },
      },
      null,
      2,
    ),
  );
  const resp = await fetch(`${baseURL}/forge/agents/reload`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`could not pick up the new machine: ${resp.status}`);
  await openMachine(page, "e2e-long-entry");

  const report = await page.evaluate(() => {
    const out = [];
    for (const node of document.querySelectorAll(".chart-node")) {
      const head = node.querySelector(".chart-node__head");
      const nodeBox = node.getBoundingClientRect();
      out.push({
        node: node.dataset.testid,
        // What the server reserved, against what the browser needed for it.
        reserved: parseFloat(head.style.height),
        used: head.scrollHeight,
        entries: [...head.querySelectorAll(".chart-node__entry")].map((e) => ({
          text: e.textContent.trim(),
          height: Math.round(e.getBoundingClientRect().height),
          escapesBy: Math.round(e.getBoundingClientRect().bottom - nodeBox.bottom),
        })),
      });
    }
    return out;
  });

  expect(report.length).toBeGreaterThan(0);
  for (const node of report) {
    expect(node.entries.length).toBeGreaterThan(0);
    // The box the server sized holds what the browser put in it. This is the
    // assertion the bug fails: 86 used against 68 reserved.
    expect(node.used, `${node.node} overflows the height the server reserved`)
      .toBeLessThanOrEqual(node.reserved);
    for (const entry of node.entries) {
      // One line each. chart.lineH is 14, and an entry that wrapped measured 29.
      expect(entry.height, `"${entry.text}" is not one line`).toBe(14);
      // And inside the border, which is what anyone actually sees.
      expect(entry.escapesBy, `"${entry.text}" hangs out of ${node.node}`)
        .toBeLessThan(0);
    }
  }
});
