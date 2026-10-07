// Epic 15 Story 0, phase 5 — the navigations that remain do not read as hard
// refreshes.
//
// Selecting a component, an entity type, a machine or a map is still a real
// page load, as is switching mode. Selecting inside the statechart is not. At ~50ms each the browser's default is a
// flash: the window blanks and repaints, which reads as something breaking.
//
// What only a browser can show: that the transition actually runs, that it does
// *not* run on the SSE patches, and that no view-transition-name collides —
// a collision throws, and a thrown transition drops silently back to the
// instant swap this exists to replace.
const { test, expect, byTestId } = require("../fixtures");

// Counts both kinds. A cross-document transition never goes through
// startViewTransition — it arrives on the new document as a pagereveal event
// carrying one — so watching only the method would report zero for the very
// thing this phase adds.
async function watchTransitions(page) {
  await page.addInitScript(() => {
    window.__crossDoc = 0;
    window.__sameDoc = 0;
    addEventListener("pagereveal", (e) => {
      if (e.viewTransition) window.__crossDoc++;
    });
    const start = document.startViewTransition?.bind(document);
    if (start) {
      document.startViewTransition = (cb) => {
        window.__sameDoc++;
        return start(cb);
      };
    }
  });
  return {
    crossDoc: () => page.evaluate(() => window.__crossDoc || 0),
    sameDoc: () => page.evaluate(() => window.__sameDoc || 0),
  };
}

// One test below dirties the schema session to get a push it can watch, and
// this project runs one worker against one server — so a spec that left it
// dirty would have every later test looking at an unsaved footer it did not
// cause. Every other stateful spec resets for the same reason.
test.afterEach(async ({ baseURL }) => {
  const resp = await fetch(`${baseURL}/forge/schema/discard`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`could not reset the session: ${resp.status}`);
});

test("switching mode is a transition rather than a blank and repaint", async ({ page }) => {
  const vt = await watchTransitions(page);
  await page.goto("/forge/map");

  // Nothing to transition from on the first load.
  expect(await vt.crossDoc(), "the first load animated from nothing").toBe(0);

  await byTestId(page, "rail-schema").click();
  await expect(byTestId(page, "rail-schema")).toHaveAttribute("data-active", "true");
  expect(await vt.crossDoc(), "switching mode did not run a view transition").toBe(1);
});

test("selecting a component is a transition too", async ({ page }) => {
  const vt = await watchTransitions(page);
  await page.goto("/forge/schema");

  await byTestId(page, "component-Health").click();
  await expect(byTestId(page, "component-name")).toHaveText("Health");
  expect(await vt.crossDoc(), "selecting a component did not run a view transition").toBe(1);
});

test("only the browser's skipped-transition rejection is treated as cancellation", async ({ page }) => {
  await page.goto("/forge/map");
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.evaluate(() => {
    setTimeout(() => { Promise.reject(new DOMException("Transition was skipped", "AbortError")); }, 0);
  });
  // Let the browser deliver the unhandled-rejection event before asserting.
  await page.waitForTimeout(100);
  expect(errors, "a skipped view transition escaped as a page error").toEqual([]);

  // A different rejection must still reach the page's error channel. The
  // fixture permits only this deliberate probe, not unrelated failures.
  await page.expectPageErrors(/uncaught exception: Error: other failure/, async () => {
    await page.evaluate(() => {
      setTimeout(() => { Promise.reject(new Error("other failure")); }, 0);
    });
    await expect.poll(() => errors.some((e) => e.includes("other failure"))).toBe(true);
  });
});

// The other half, and the more important one. An edit reaches the screen in
// ~20ms; wrapping that in an animation with a duration would put back the
// latency the rest of this work removed. A transition is for a change of view,
// not for a keystroke landing.
//
// Asserted on the wire rather than by counting startViewTransition calls, which
// is what this did first and could not fail: Datastar wraps a patch in a
// transition only when the SSE response says `useViewTransition true`, and
// nothing in Forge writes that line. Counting calls therefore measured a thing
// no change to this repo could make non-zero. Reading the stream measures the
// decision itself.
test("an edit pushed down the stream is not animated", async ({ page }) => {
  const vt = await watchTransitions(page);
  await page.goto("/forge/schema");
  await expect(byTestId(page, "save-footer")).toBeVisible();

  const stream = await page.evaluate(
    () => document.querySelector("[data-init]").getAttribute("data-init").match(/@get\('([^']+)'\)/)[1],
  );

  const sent = await page.evaluate(async (url) => {
    const bump = fetch("/forge/schema/version", { method: "POST" });
    const resp = await fetch(url);
    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let text = "";
    const deadline = Date.now() + 4000;
    while (Date.now() < deadline) {
      const chunk = await Promise.race([
        reader.read(),
        new Promise((r) => setTimeout(() => r({ done: true }), deadline - Date.now())),
      ]);
      if (chunk.done) break;
      text += decoder.decode(chunk.value, { stream: true });
      if (text.includes("useViewTransition")) break;
    }
    await reader.cancel();
    await bump;
    return text;
  }, stream);

  expect(sent, "a patch asked the browser to animate it").not.toContain("useViewTransition");
  await expect(byTestId(page, "save-footer")).toContainText("unsaved", { timeout: 10_000 });
  expect(await vt.sameDoc(), "a pushed edit was animated").toBe(0);
});

// A name has to be unique in the document or the transition throws — and the
// failure is silent, because a thrown transition just swaps instantly, which
// looks exactly like this feature not being switched on.
test("every view-transition-name is a singleton", async ({ page }) => {
  await page.goto("/forge/map");

  const names = await page.evaluate(() => {
    const seen = {};
    for (const el of document.querySelectorAll("*")) {
      const n = getComputedStyle(el).viewTransitionName;
      if (n && n !== "none") seen[n] = (seen[n] || 0) + 1;
    }
    return seen;
  });

  const duplicated = Object.entries(names).filter(([, n]) => n > 1);
  expect(duplicated, `these names are on more than one element: ${JSON.stringify(duplicated)}`)
    .toEqual([]);
  // And the chrome that should hold still across a navigation is named at all.
  expect(Object.keys(names).sort()).toEqual(["menubar", "rail", "root"]);
});

// Asserted by running one, not by reading the stylesheet back.
//
// The first version of this walked document.styleSheets for a
// prefers-reduced-motion rule and checked its animation-name — which passes
// identically with reduced motion *off*, because it asserts that the CSS was
// authored rather than that anything is instant. It also named the variable
// `duration` while holding an animation name.
//
// page.emulateMedia rather than test.use({ reducedMotion }), which did not
// reach the page here: the first honest version of this test asserted
// matchMedia up front and reported false, so every assertion under it would
// have been about nothing.
test("with reduced motion, a navigation animates nothing", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/forge/map");

  // Guard the guard. If reduced motion is not actually in effect, everything
  // below passes or fails for reasons that have nothing to do with it.
  expect(
    await page.evaluate(() => matchMedia("(prefers-reduced-motion: reduce)").matches),
    "reduced motion is not in effect, so this test is not about it",
  ).toBe(true);

  await byTestId(page, "rail-schema").click();
  await expect(byTestId(page, "rail-schema")).toHaveAttribute("data-active", "true");

  // Every view-transition animation the browser created. Reduced motion should
  // leave none of them to run.
  const running = await page.evaluate(() =>
    document
      .getAnimations()
      .filter((a) => String(a.effect?.pseudoElement || "").includes("view-transition"))
      .map((a) => `${a.animationName}@${a.effect.getComputedTiming().duration}`),
  );
  expect(running, `the transition animated under reduced motion: ${running}`).toEqual([]);
});
