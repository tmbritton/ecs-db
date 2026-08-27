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
  await expect(byTestId(page, "map-cell")).toHaveCount(26);

  // The props layer, so what disappears is a handful of tiles rather than the
  // whole picture — which is what makes this a layer test and not a blank one.
  await byTestId(page, "layer-eye-props").click();
  await expect(byTestId(page, "map-cell")).toHaveCount(24);
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-hidden", "true");
  await expect(byTestId(page, "layer-ground")).toHaveAttribute("data-hidden", "false");
  // An eye is a view, not an edit.
  await expect(byTestId(page, "save-footer")).not.toHaveClass(/save-footer--dirty/);

  await byTestId(page, "layer-eye-props").click();
  await expect(byTestId(page, "map-cell")).toHaveCount(26);

  await byTestId(page, "layer-eye-ground").click();
  await expect(byTestId(page, "map-cell")).toHaveCount(2);
});

test("selecting a tile marks it, changes the URL, and survives a reload", async ({ page }) => {
  await byTestId(page, "palette-tile-2").click();
  await expect(byTestId(page, "palette-tile-2")).toHaveAttribute("data-selected", "true");
  await expect(byTestId(page, "map-selected-tile")).toHaveText("tile 2");
  expect(page.url()).toContain("tile=2");

  await page.reload();
  await expect(byTestId(page, "palette-tile-2")).toHaveAttribute("data-selected", "true");

  // Clicking the selected tile again clears it.
  await byTestId(page, "palette-tile-2").click();
  await expect(byTestId(page, "map-selected-tile")).toHaveText("no tile selected");
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

test("the active layer survives a reload and travels with the other links", async ({ page }) => {
  await byTestId(page, "layer-select-props").click();
  expect(page.url()).toContain("layer=1");

  await page.reload();
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-active", "true");

  await byTestId(page, "palette-tile-1").click();
  await expect(byTestId(page, "layer-props")).toHaveAttribute("data-active", "true");
  await expect(byTestId(page, "map-selected-tile")).toHaveText("tile 1");
});

test("zoom is a control, is in the URL, and survives a reload", async ({ page }) => {
  await expect(byTestId(page, "zoom-4")).toHaveAttribute("data-current", "true");

  await byTestId(page, "zoom-1").click();
  await expect(byTestId(page, "zoom-1")).toHaveAttribute("data-current", "true");
  await expect(byTestId(page, "zoom-4")).toHaveAttribute("data-current", "false");
  await expect(byTestId(page, "map-zoom")).toHaveText("1× · 16px cells");
  expect(page.url()).toContain("zoom=1");

  // The map really is drawn smaller: a cell's own box is the tile's size and
  // the matrix is what scales it, so the canvas is what changes. The exact
  // number, because "less than 200" is also true at 2x.
  const box = await byTestId(page, "map-canvas").boundingBox();
  expect(box.width, "6 cells of 16px at 1x").toBe(96);
  await expect(byTestId(page, "map-canvas")).toHaveAttribute("data-cell-w", "16");

  await page.reload();
  await expect(byTestId(page, "zoom-1")).toHaveAttribute("data-current", "true");
});

test("zoom travels with the other links", async ({ page }) => {
  await byTestId(page, "zoom-2").click();
  await byTestId(page, "palette-tile-1").click();
  await expect(byTestId(page, "zoom-2")).toHaveAttribute("data-current", "true");
  await expect(byTestId(page, "map-selected-tile")).toHaveText("tile 1");

  await byTestId(page, "layer-eye-props").click();
  await expect(byTestId(page, "zoom-2")).toHaveAttribute("data-current", "true");
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
