// Epic 15 Story 3 — the map renders.
//
// What only a browser can show: that the tileset image actually loads through
// the asset route, that the cells are where the map says, and that the canvas
// does not flicker under the page stream. Painting is Story 4's; nothing here
// changes a file.
const { test, expect, byTestId } = require("../fixtures");

test.beforeEach(async ({ page }) => {
  await page.goto("/forge/map");
});

// "the view survives an unrelated push" dirties the schema session to get a
// push it can watch. Discarding here rather than inline means a timed-out
// assertion cannot leave it dirty for the rest of the run — every later test
// would then be looking at an unsaved footer it did not cause. Two other specs
// clean up for exactly this reason.
test.afterEach(async ({ baseURL }) => {
  const resp = await fetch(`${baseURL}/forge/schema/discard`, { method: "POST" });
  if (resp.status !== 204) throw new Error(`could not reset the session: ${resp.status}`);
});

test("the fixture map draws a cell per tile, from the tileset image", async ({ page }) => {
  await expect(byTestId(page, "map-canvas")).toBeVisible();
  // 6x4 of ground, every cell filled, plus the two props on top of it.
  await expect(byTestId(page, "map-cell")).toHaveCount(26);

  // The image is fetched, and it is served — not a broken picture with a box
  // where a tile should be.
  const src = await byTestId(page, "map-cell")
    .first()
    .evaluate((el) => getComputedStyle(el).backgroundImage);
  expect(src).toContain("/forge/asset");

  const url = src.match(/url\("([^"]+)"\)/)[1];
  const res = await page.request.get(url);
  expect(res.status(), "the tileset image did not load").toBe(200);
  expect(res.headers()["content-type"]).toBe("image/png");
});

test("walls and floors draw different slices of the sheet", async ({ page }) => {
  const positions = await byTestId(page, "map-cell").evaluateAll((els) =>
    els.map((e) => getComputedStyle(e).backgroundPosition),
  );
  const distinct = new Set(positions);
  expect(distinct.size, "every cell drew the same tile").toBeGreaterThan(1);
});

test("the status line gives the map's size and its tile size", async ({ page }) => {
  await expect(byTestId(page, "map-size")).toHaveText("6×4 cells");
  await expect(byTestId(page, "map-tile-size")).toHaveText("16×16 px");
  await expect(byTestId(page, "map-layer-count")).toHaveText("2 layers");
  // 6x4 at 16px fits comfortably, so it opens at the largest step FitScale
  // offers, with nobody having asked.
  await expect(byTestId(page, "map-zoom")).toHaveText("4× · 64px cells");
  await expect(byTestId(page, "map-selected-tile")).toHaveText("no tile selected");
});

test("hiding a layer stops it drawing and leaves the file alone", async ({ page }) => {
  // Visible cells, not cells. The server draws every layer and CSS hides a
  // group, so the eye costs no request — which means the elements are all still
  // there and counting them would report 26 whatever the eye is set to.
  // filter({visible:true}), not a ":visible" selector. Playwright discourages
  // the pseudo-class and its assertions do not handle it: toHaveCount against
  // it never resolves, reporting "Received: undefined" until the test times
  // out, on a page that is rendering perfectly.
  const drawn = byTestId(page, "map-cell").filter({ visible: true });
  const requests = await countRequests(page);

  await expect(drawn).toHaveCount(26);

  // The props layer, so what disappears is a handful of tiles rather than the
  // whole picture — which is what makes this a layer test and not a blank one.
  await byTestId(page, "layer-eye-props").click();
  await expect(drawn).toHaveCount(24);
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-hidden", "true");
  await expect(byTestId(page, "layer-ground")).toHaveAttribute("data-hidden", "false");
  // An eye is a view, not an edit.
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);

  await byTestId(page, "layer-eye-props").click();
  await expect(drawn).toHaveCount(26);

  await byTestId(page, "layer-eye-ground").click();
  await expect(drawn).toHaveCount(2);

  expect(await requests.read(), "the eye talked to the server").toBe(0);
  expect(page.url(), "the hidden layers leaked into the URL").not.toContain("hide=");
});

test("selecting a tile marks it, and costs no request at all", async ({ page }) => {
  // The tile in hand is the browser's: it is a signal, not a query parameter,
  // so it does not survive a reload and does not need to. What it does instead
  // is cost nothing — and, more importantly, survive a patch, which the URL
  // version could not: see "the view survives an unrelated push" below.
  const requests = await countRequests(page);

  await byTestId(page, "palette-tile-2").click();
  await expect(byTestId(page, "palette-tile-2")).toHaveAttribute("data-selected", "true");
  await expect(byTestId(page, "palette-tile-2")).toHaveAttribute("aria-pressed", "true");
  await expect(byTestId(page, "map-selected-tile")).toHaveText("tile 2");

  // Clicking the selected tile again puts it down, which is how you paint
  // nothing without reaching for the eraser.
  await byTestId(page, "palette-tile-2").click();
  await expect(byTestId(page, "map-selected-tile")).toHaveText("no tile selected");

  expect(await requests.read(), "choosing a tile talked to the server").toBe(0);
  expect(page.url(), "the tile leaked into the URL").not.toContain("tile=");
});

test("a tile selection survives looking at a different layer", async ({ page }) => {
  await byTestId(page, "palette-tile-1").click();
  await byTestId(page, "layer-eye-props").click();
  await expect(byTestId(page, "map-selected-tile")).toHaveText("tile 1");
});

// Tiled addresses tilesets by range, picking the highest first gid at or below
// an id. A project with one tileset never exercises that, and this map's props
// come from a second sheet starting at 121.
test("the palette lists both tilesets, in first-gid order", async ({ page }) => {
  await expect(byTestId(page, "palette-tiny16-basic")).toBeVisible();
  await expect(byTestId(page, "palette-tiny16-things")).toBeVisible();

  const names = await page
    .locator("[data-testid^='palette-tiny16-']")
    .evaluateAll((els) => els.map((e) => e.getAttribute("data-testid")));
  expect(names).toEqual(["palette-tiny16-basic", "palette-tiny16-things"]);

  // 120 tiles in the first sheet, so the second starts at 121 — and a tile from
  // it is drawn on the map.
  await expect(byTestId(page, "palette-tile-121")).toBeVisible();
});

test("LIVE and REPLAY are present, disabled, and say which epic owns them", async ({ page }) => {
  await expect(byTestId(page, "lens-authored")).toBeVisible();
  await expect(byTestId(page, "lens-live")).toBeDisabled();
  await expect(byTestId(page, "lens-replay")).toBeDisabled();
  await expect(byTestId(page, "lens-live")).toHaveAttribute("title", /Epic 18/);
  await expect(byTestId(page, "lens-replay")).toHaveAttribute("title", /Epic 19/);
});

test("the canvas is not re-patched every tick", async ({ page }) => {
  // The canvas is the biggest thing on the page stream. If any part of building
  // it ranges a map, two renders of an unchanged file differ and the server
  // sends the whole canvas twice a second — which both flickers and defeats the
  // identical-patch suppression the rest of the mode relies on.
  //
  // Counted from a second, raw subscription, the way 06-engine-status does it:
  // Datastar consumes its own stream, so the frames cannot be counted from
  // outside it.
  await expect(byTestId(page, "map-cell")).toHaveCount(26);

  // The page's own subscription, not a default one: the stream carries the map,
  // the hidden layers, the active layer and the tile, and a test that dropped
  // them would be measuring a view nobody is looking at.
  const stream = await page.evaluate(() =>
    document.querySelector("[data-init]").getAttribute("data-init").match(/@get\('([^']+)'\)/)[1],
  );
  expect(stream, "the stream does not carry the view").toContain("map=");

  const content = await page.evaluate(async (url) => {
    const resp = await fetch(url);
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
    // The canvas is its own patch target now, so this counts the canvas rather
    // than the whole mode — which is a sharper question than it was: before the
    // split, "the mode was sent twice" and "the canvas was sent twice" were the
    // same measurement, and only the second one matters.
    //
    // Anchored on the leading space: `data-testid="map-canvas"` ends with the
    // substring `id="map-canvas"`, and an unanchored match counts it too.
    return (text.match(/ id="map-canvas-region"/g) || []).length;
  }, stream);
  expect(content, `the canvas was patched ${content} times in ~6s`).toBe(1);
});

test("the asset route refuses a path no tileset names", async ({ page }) => {
  for (const target of ["/etc/passwd", "../../etc/passwd", "e2e/fixtures/project/schema.json"]) {
    const res = await page.request.get(`/forge/asset?path=${encodeURIComponent(target)}`);
    expect(res.status(), `${target} was served`).toBe(404);
  }
});

test("the layer row is two controls: what is drawn, and what is painted", async ({ page }) => {
  // Conflating them means you cannot look at a layer without also painting into
  // it, which is how you lose an hour in a tile editor.
  await expect(byTestId(page, "layer-ground")).toHaveAttribute("data-active", "true");
  await expect(byTestId(page, "map-active-layer")).toHaveText("painting ground");

  await byTestId(page, "layer-select-props").click();
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-active", "true");
  await expect(byTestId(page, "layer-ground")).toHaveAttribute("data-active", "false");
  await expect(byTestId(page, "map-active-layer")).toHaveText("painting props");

  // Hiding the layer you are painting into does not stop it being the one you
  // are painting into.
  await byTestId(page, "layer-eye-props").click();
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-hidden", "true");
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-active", "true");
});

test("choosing a layer to paint into does not disturb the tile in hand", async ({ page }) => {
  await byTestId(page, "layer-select-props").click();
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-active", "true");

  await byTestId(page, "palette-tile-1").click();
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-active", "true");
  await expect(byTestId(page, "map-selected-tile")).toHaveText("tile 1");
  expect(page.url(), "the layer leaked into the URL").not.toContain("layer=");
});

test("zoom is a control the browser owns outright", async ({ page }) => {
  const requests = await countRequests(page);

  // The fixture is 6x4 at 16px, so it opens at the largest step that fits.
  await expect(byTestId(page, "zoom-4")).toHaveAttribute("data-current", "true");

  await byTestId(page, "zoom-1").click();
  await expect(byTestId(page, "zoom-1")).toHaveAttribute("data-current", "true");
  await expect(byTestId(page, "zoom-4")).toHaveAttribute("data-current", "false");
  await expect(byTestId(page, "map-zoom")).toHaveText("1× · 16px cells");

  // The map really is drawn smaller. The exact number, because "less than 200"
  // is also true at 2x.
  expect((await byTestId(page, "map-canvas").boundingBox()).width, "6 cells of 16px at 1x").toBe(96);
  await expect(byTestId(page, "map-canvas")).toHaveAttribute("data-cell-w", "16");

  await byTestId(page, "zoom-8").click();
  await expect(byTestId(page, "map-zoom")).toHaveText("8× · 128px cells");
  expect((await byTestId(page, "map-canvas").boundingBox()).width, "6 cells of 16px at 8x").toBe(768);

  expect(await requests.read(), "changing zoom talked to the server").toBe(0);
  expect(page.url(), "the zoom leaked into the URL").not.toContain("zoom=");
});

// The reason all four of these had to leave the URL, rather than becoming
// @get requests that patch regions.
//
// A page's SSE subscription is built when it loads and cannot change
// afterwards. So anything the server renders from it is frozen at that moment,
// and the next unrelated event — a save, another tab's edit, the engine coming
// up — re-renders the view as it was and undoes everything since. What the
// server does not render, a re-render cannot clobber.
test("the view survives an unrelated push from the server", async ({ page }) => {
  await byTestId(page, "zoom-8").click();
  await byTestId(page, "palette-tile-2").click();
  await byTestId(page, "layer-eye-props").click();
  await expect(byTestId(page, "map-zoom")).toHaveText("8× · 128px cells");

  // Something else entirely changes, and the stream pushes it to this page.
  await page.evaluate(() => fetch("/forge/schema/version", { method: "POST" }));
  await expect(byTestId(page, "save-footer")).toContainText("unsaved", { timeout: 10_000 });

  await expect(byTestId(page, "map-zoom")).toHaveText("8× · 128px cells");
  await expect(byTestId(page, "map-selected-tile")).toHaveText("tile 2");
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-hidden", "true");
});

// It used to be "zoom travels with the other links", when every one of these
// was a navigation and each had to carry the others' state in its href. They
// are independent signals now, so what this asks is the weaker and more
// obvious thing: that changing one does not disturb another.
test("the four view controls do not disturb one another", async ({ page }) => {
  await byTestId(page, "zoom-2").click();
  await byTestId(page, "palette-tile-1").click();
  await expect(byTestId(page, "zoom-2")).toHaveAttribute("data-current", "true");
  await expect(byTestId(page, "map-selected-tile")).toHaveText("tile 1");

  await byTestId(page, "layer-eye-props").click();
  await expect(byTestId(page, "zoom-2")).toHaveAttribute("data-current", "true");
});

// The canvas panel scrolls exactly as far as the map, and no further.
//
// Zoom is a CSS transform, and a transform does not resize the box it is on —
// so the box that takes up space and the box that is scaled have to be two
// different elements with two different sizes. Giving the inner one `inset: 0`
// makes it the size of the outer, which is *already* multiplied by the zoom,
// and scaling that again gives a visual box of map x zoom squared.
//
// Nothing about the map looks wrong when that happens: every cell is placed by
// its own matrix, so the picture is correct and only the scroll range is not.
// At 8x it was eight times too long in each axis, with the map occupying 1.6%
// of what you could scroll to and a thumb an eighth of its proper size.
test("the canvas scrolls as far as the map and no further", async ({ page }) => {
  const extent = () =>
    byTestId(page, "map-viewport").evaluate((el) => ({
      w: el.scrollWidth,
      h: el.scrollHeight,
      clientW: el.clientWidth,
    }));

  await byTestId(page, "zoom-1").click();
  await expect(byTestId(page, "map-canvas")).toHaveAttribute("data-cell-w", "16");
  const at1 = await extent();

  await byTestId(page, "zoom-8").click();
  await expect(byTestId(page, "map-canvas")).toHaveAttribute("data-cell-w", "128");
  const at8 = await extent();

  // 6x4 cells of 16px: 96x64 at 1x, 768x512 at 8x.
  //
  // Against the viewport's own width, not the map's: a map narrower than the
  // panel does not overflow it, so scrollWidth is the panel. What this rules
  // out is the map being scrolled to as though it were far bigger than it is —
  // 6144px at 8x, if the scaled box were sized from the already-scaled one.
  const room = (n) => Math.max(n, at8.clientW) + 24; // 24 for the panel's padding
  expect(at1.w).toBe(at1.clientW);
  expect(at8.w, `scrollWidth is ${at8.w} for a 768px map — the scaled box is sized twice`)
    .toBeLessThanOrEqual(room(768));
  expect(at8.h, `scrollHeight is ${at8.h} for a 512px map`).toBeLessThanOrEqual(room(512));
});

test("the palette does not follow the canvas zoom", async ({ page }) => {
  // Zoom is a canvas concern. The rail is 212px wide and cannot follow it: at 8x
  // a 32px tile became a 256px swatch, flex shrank its width to fit while its
  // height and background-size did not, and every tile became a distorted crop.
  const swatch = byTestId(page, "palette-tile-1");
  const before = await swatch.boundingBox();

  await byTestId(page, "zoom-8").click();
  await expect(byTestId(page, "zoom-8")).toHaveAttribute("data-current", "true");

  const after = await swatch.boundingBox();
  expect(after.width).toBe(before.width);
  expect(after.height).toBe(before.height);
});

// countRequests watches from inside the page, and counts only what happens
// after it is called.
//
// A PerformanceObserver rather than page.on("request"), which is the obvious
// way and does not work: attaching one made every later assertion in the same
// test hang until the 30-second timeout, reporting "Received: undefined" on a
// page that the failure screenshot shows rendering perfectly. Removing it, the
// same test passed in 14 seconds.
//
// Nor waitForLoadState("networkidle") to settle first: the SSE stream is a
// request that deliberately never completes, so the page is never idle by that
// definition and the wait hangs until the test times out too.
//
// Resource timing covers fetch, XHR and every subresource, which is everything
// a view control could cause — and because the observer starts here, the stream
// already open at page load is not counted while a *reconnect* would be, which
// is exactly the split these tests want.
async function countRequests(page) {
  await page.evaluate(() => {
    window.__requests = 0;
    new PerformanceObserver((list) => {
      window.__requests += list.getEntries().length;
    }).observe({ type: "resource", buffered: false });
  });
  return { read: () => page.evaluate(() => window.__requests) };
}
