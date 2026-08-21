// Story 2 — web toolchain: templ, Datastar and embedded assets.
//
// The claim this story makes is that Forge is self-contained: it serves a
// templ-rendered page with its fonts, styles and JS from an embedded FS, and
// needs no network. That is a claim about what the browser fetches, so it can
// only be checked in a browser.

const { test, expect } = require("../fixtures");

test("the page loads every asset it references, all same-origin", async ({ page, baseURL }) => {
  // Derived, not hard-coded: changing the port in playwright.config.js would
  // otherwise fail this test for a reason that has nothing to do with it.
  const ownHost = new URL(baseURL).host;
  const external = [];
  const requested = [];
  page.on("request", (r) => {
    const u = new URL(r.url());
    requested.push(u.pathname);
    // Forge is a local authoring tool for a game that ships no network
    // dependency. A CDN reference would work on the developer's machine and
    // fail on a plane.
    if (u.host !== ownHost) external.push(r.url());
  });

  await page.goto("/forge/map");
  expect(external, "Forge must work offline").toEqual([]);

  for (const asset of [
    "/static/css/fonts.css",
    "/static/css/tokens.css",
    "/static/css/forge.css",
    "/static/js/vendor/datastar.js",
  ]) {
    expect(requested, `${asset} is not being loaded`).toContain(asset);
  }
});

test("Datastar loads and initialises", async ({ page }) => {
  await page.goto("/forge/map");

  // The module has to have executed, not merely 200'd. Datastar processes
  // data-* attributes on load; a bundle that failed to parse leaves them as
  // inert strings and produces no error the page can see.
  const applied = await page.evaluate(async () => {
    const probe = document.createElement("div");
    probe.setAttribute("data-text", "'datastar-is-alive'");
    document.body.append(probe);
    // Give the framework's mutation observer a turn.
    await new Promise((r) => setTimeout(r, 200));
    const text = probe.textContent;
    probe.remove();
    return text;
  });
  expect(applied, "Datastar did not process a data-text attribute").toBe("datastar-is-alive");
});

test("both design fonts are actually loaded, not silently substituted", async ({ page }) => {
  await page.goto("/forge/map");
  await page.evaluate(() => document.fonts.ready.then(() => undefined));

  const families = await page.evaluate(() =>
    [...document.fonts].filter((f) => f.status === "loaded").map((f) => f.family)
  );
  // The typography is load-bearing for the product's identity, and a missing
  // webfont degrades to a system sans that looks approximately right in a
  // screenshot.
  expect(families).toContain("Chakra Petch");
  expect(families).toContain("JetBrains Mono");
});

test("static assets are cached hard and directory listings are not served", async ({ request }) => {
  const asset = await request.get("/static/js/vendor/datastar.js");
  expect(asset.status()).toBe(200);
  expect(asset.headers()["cache-control"]).toContain("immutable");

  for (const dir of ["/static/", "/static/css/", "/static/js/vendor/"]) {
    const resp = await request.get(dir);
    expect(resp.status(), `${dir} should not be browsable`).toBe(404);
  }
});
