// Story 5 — app shell: menu bar, mode rail, routed modes, one SSE stream.
//
// What is worth testing in a browser here is not the markup — Go tests cover
// that — but the things only a browser knows: that a link actually navigates,
// that history works, that the page really opens its subscription, and that
// the viewport is filled exactly once.

const { test, expect, byTestId, expectSettled, watchEventStreams, MODES } = require("../fixtures");

test.describe("app shell", () => {
  test("/ lands in the default mode", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveURL("/forge/map");
    await expect(byTestId(page, "rail-map")).toHaveAttribute("data-active", "true");
  });

  test("the frame renders on every mode", async ({ page }) => {
    for (const m of MODES) {
      await page.goto(`/forge/${m.slug}`);
      await expect(byTestId(page, "menubar")).toBeVisible();
      await expect(byTestId(page, "mode-rail")).toBeVisible();
      await expect(byTestId(page, "mode-content")).toBeVisible();
      await expect(byTestId(page, "wordmark")).toHaveText("⚒ FORGE");
      await expect(page).toHaveTitle(`Forge — ${m.title}`);
    }
  });

  test("exactly one rail button is active, and it is this mode's", async ({ page }) => {
    for (const active of MODES) {
      await page.goto(`/forge/${active.slug}`);
      for (const m of MODES) {
        await expect(byTestId(page, `rail-${m.slug}`)).toHaveAttribute(
          "data-active",
          m.slug === active.slug ? "true" : "false"
        );
      }
      await expect(page.locator('[data-active="true"]')).toHaveCount(1);
    }
  });

  test("each mode renders a stub naming itself and the epic that fills it", async ({ page }) => {
    for (const m of MODES) {
      await page.goto(`/forge/${m.slug}`);
      await expect(byTestId(page, "mode-stub-caption")).toHaveText(m.caption);
      await expect(byTestId(page, "mode-stub-epic")).toContainText("Epic");
    }
  });
});

test.describe("navigation is navigation", () => {
  // Mode switching is a full page load. These are the behaviours that come for
  // free from anchors and would all have to be reimplemented — badly — if the
  // rail were rewritten into click handlers.
  test("clicking a rail button navigates and moves the active state", async ({ page }) => {
    await page.goto("/forge/map");
    for (const m of MODES.slice(1)) {
      await byTestId(page, `rail-${m.slug}`).click();
      await expect(page).toHaveURL(`/forge/${m.slug}`);
      await expect(byTestId(page, `rail-${m.slug}`)).toHaveAttribute("data-active", "true");
    }
  });

  test("back and forward move between modes", async ({ page }) => {
    await page.goto("/forge/map");
    await byTestId(page, "rail-schema").click();
    await expect(page).toHaveURL("/forge/schema");

    await page.goBack();
    await expect(page).toHaveURL("/forge/map");
    await expect(byTestId(page, "rail-map")).toHaveAttribute("data-active", "true");

    await page.goForward();
    await expect(page).toHaveURL("/forge/schema");
    await expect(byTestId(page, "rail-schema")).toHaveAttribute("data-active", "true");
  });

  test("a mode can be deep-linked", async ({ page }) => {
    await page.goto("/forge/agents");
    await expect(byTestId(page, "rail-agents")).toHaveAttribute("data-active", "true");
    await expect(byTestId(page, "mode-stub-caption")).toHaveText("AGENTS");
  });

  test("an unknown mode is a 404", async ({ page }) => {
    // Two patterns for one failure: the response channel names the URL, the
    // browser's console message does not. Neither would waive a 500, a failed
    // stylesheet, or an uncaught exception.
    const resp = await page.expectPageErrors(
      [/HTTP 404: .*\/forge\/nope/, /console error: Failed to load resource.*404/],
      () => page.goto("/forge/nope")
    );
    expect(resp.status()).toBe(404);
  });
});

test.describe("the page-level SSE stream", () => {
  // The check a dead Datastar attribute fails. `data-on-load` — which the
  // implementation plan called for — names no registered plugin, so the page
  // would render perfectly and never connect. Nothing but a browser sees that.
  test("each page opens exactly one subscription, to its own mode", async ({ page }) => {
    for (const m of MODES) {
      const streams = watchEventStreams(page);
      await page.goto(`/forge/${m.slug}`);
      await expectSettled(page, () => streams, [`/forge/${m.slug}/events`], {
        message: `${m.slug} should open exactly one stream, its own`,
      });
    }
  });

  test("the stream is a live text/event-stream, not a one-shot response", async ({ page }) => {
    const [request] = await Promise.all([
      page.waitForRequest((r) => new URL(r.url()).pathname === "/forge/map/events"),
      page.goto("/forge/map"),
    ]);
    const resp = await request.response();
    expect(resp.status()).toBe(200);
    expect(resp.headers()["content-type"]).toBe("text/event-stream");

    // The headers prove nothing on their own: the SDK writes and flushes them
    // before the handler body runs, so a handler that returns immediately
    // still answers 200 with text/event-stream. Verified by mutation — the
    // assertions above passed against a server that hung up at once, and
    // Datastar does not reconnect after a clean end-of-stream, so the page
    // would have been permanently and silently unsubscribed.
    const outcome = await Promise.race([
      resp.finished().then(() => "closed"),
      new Promise((r) => setTimeout(() => r("still open"), 1500)),
    ]);
    expect(outcome, "the server closed the event stream instead of holding it open").toBe("still open");
  });
});

test.describe("layout", () => {
  test("the shell fills the viewport and the page never scrolls", async ({ page }) => {
    await page.goto("/forge/map");
    const box = await page.evaluate(() => {
      const e = document.scrollingElement;
      return {
        scrollH: e.scrollHeight,
        clientH: e.clientHeight,
        scrollW: e.scrollWidth,
        clientW: e.clientWidth,
      };
    });
    expect(box.scrollH).toBeLessThanOrEqual(box.clientH);
    expect(box.scrollW).toBeLessThanOrEqual(box.clientW);

    // "Does not overflow" is only half of it, and the cheap half: a shell
    // collapsed to 40px satisfies it perfectly. Verified — with
    // `.shell { height: 40px }` the app rendered as a strip with the mode
    // content clipped to about 6px, and the whole spec still passed.
    const shell = await byTestId(page, "shell").boundingBox();
    expect(shell.height, "the shell does not fill the viewport").toBeCloseTo(box.clientH, 0);
    expect(shell.width).toBeCloseTo(box.clientW, 0);

    // And the content region has to have somewhere to render into.
    const content = await byTestId(page, "mode-content").boundingBox();
    expect(content.height).toBeGreaterThan(box.clientH - 100);
  });

  // Measured, not eyeballed: the rail shipped at 46x52 against the design's
  // 46x46 because the glyph had been enlarged for legibility, and at a glance
  // it looked right.
  test("the chrome matches the design's dimensions", async ({ page }) => {
    await page.goto("/forge/map");

    const menubar = await byTestId(page, "menubar").boundingBox();
    expect(menubar.height).toBeGreaterThanOrEqual(32);
    expect(menubar.height).toBeLessThanOrEqual(36);

    const rail = await byTestId(page, "mode-rail").boundingBox();
    expect(rail.width).toBe(62);

    // Bounded on both sides. The regression this test was written for was a
    // rail that grew to 46x52; a rail that *shrinks* — losing its padding, say
    // — breaks the same "column of squares" and an upper bound alone waves it
    // through. Verified: with `.rail__btn { padding: 0 }` the buttons
    // collapsed to ~32px and the spec passed.
    for (const m of MODES) {
      const btn = await byTestId(page, `rail-${m.slug}`).boundingBox();
      expect(btn.width, `${m.slug} button width`).toBe(46);
      expect(btn.height, `${m.slug} button height`).toBeGreaterThanOrEqual(44);
      expect(btn.height, `${m.slug} button height`).toBeLessThanOrEqual(48);
    }
  });

  test("the settings cog is pinned to the foot of the rail", async ({ page }) => {
    await page.goto("/forge/map");
    const last = await byTestId(page, "rail-sprites").boundingBox();
    const cog = await byTestId(page, "rail-cog").boundingBox();
    const rail = await byTestId(page, "mode-rail").boundingBox();

    expect(cog.y).toBeGreaterThan(last.y + last.height);
    // The point is that it is *pinned*, not merely last. Sitting after the
    // mode buttons is what flex column order gives you for free, so asserting
    // only that tests nothing: verified by deleting `margin-top: auto`, which
    // parks the cog directly under Sprites and still passed.
    const gapBelow = rail.y + rail.height - (cog.y + cog.height);
    expect(gapBelow, "the cog is not pinned to the bottom of the rail").toBeLessThanOrEqual(14);
    // And it is genuinely separated from the mode group above it.
    expect(cog.y - (last.y + last.height)).toBeGreaterThan(100);
  });
});

test.describe("affordances that are not ready yet", () => {
  // Epic 17 wires these. Until then they must not look or behave like working
  // controls — and the cog must not link to /forge/settings, which 404s.
  test("menu items are disabled, not dead links", async ({ page }) => {
    await page.goto("/forge/map");
    for (const label of ["file", "edit", "view", "map", "engine", "help"]) {
      await expect(byTestId(page, `menu-item-${label}`)).toBeDisabled();
    }
  });

  test("the settings cog is disabled and navigates nowhere", async ({ page }) => {
    await page.goto("/forge/map");
    const cog = byTestId(page, "rail-cog");
    await expect(cog).toBeDisabled();
    await cog.click({ force: true });
    await expect(page).toHaveURL("/forge/map");
  });

  // Every link the shell renders has to resolve. A 404 sitting in the primary
  // navigation is the kind of thing that survives review because nobody clicks
  // the one button that is not finished.
  test("every link in the shell resolves", async ({ page, request }) => {
    await page.goto("/forge/map");
    const hrefs = await page.locator("a[href^='/']").evaluateAll((as) => as.map((a) => a.getAttribute("href")));
    expect(hrefs.length).toBe(MODES.length);
    for (const href of hrefs) {
      const resp = await request.get(href);
      expect(resp.status(), `${href} is linked from the shell`).toBe(200);
    }
  });
});

test.describe("accessibility", () => {
  // Selection is by test id throughout this suite, which means nothing here
  // notices an accessibility regression on its own. These assertions make that
  // coverage explicit — each one corresponds to a defect already found once.
  test("the rail is a labelled navigation landmark", async ({ page }) => {
    await page.goto("/forge/map");
    await expect(byTestId(page, "mode-rail")).toHaveRole("navigation");
    await expect(byTestId(page, "mode-rail")).toHaveAccessibleName("Editor modes");
  });

  test("the active mode is announced, not just coloured", async ({ page }) => {
    await page.goto("/forge/schema");
    await expect(byTestId(page, "rail-schema")).toHaveAttribute("aria-current", "page");
    await expect(page.locator('[aria-current="page"]')).toHaveCount(1);
  });

  // The visible caption is an abbreviation — SPRT, ENTS. Left alone, the app's
  // primary navigation announces "SPRT, link" for Sprites and "ENTS, link" for
  // Entity Types. mode.Mode already carries the spelled-out Title.
  test("rail buttons announce the mode's full name, not its abbreviation", async ({ page }) => {
    await page.goto("/forge/map");
    for (const m of MODES) {
      await expect(byTestId(page, `rail-${m.slug}`)).toHaveAccessibleName(m.title);
      await expect(byTestId(page, `rail-caption-${m.slug}`)).toHaveText(m.caption);
    }
    // The cog is glyph-only, which is how an icon button ends up announced as
    // "⚙" — or as nothing at all.
    await expect(byTestId(page, "rail-cog")).toHaveAccessibleName("Preferences");
  });

  test("the rail is reachable and operable by keyboard", async ({ page }) => {
    await page.goto("/forge/map");
    await byTestId(page, "rail-tiles").focus();
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL("/forge/tiles");
  });
});
