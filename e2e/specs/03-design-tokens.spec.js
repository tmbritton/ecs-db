// Story 3 — design tokens.
//
// The token layer is checked in Go by grepping the stylesheets for hues outside
// the palette. What Go cannot check is that the tokens actually resolve in a
// browser: a typo'd custom property name is not an error, it is an empty value,
// and the element simply inherits something else.

const { test, expect } = require("../fixtures");

// Every token, with the value tokens.css declares. Duplicated from
// internal/forge/web/palette.go on purpose — this side asserts what the browser
// computes, and a copy that agrees with the Go side is the point.
const PALETTE = {
  "--void": "rgb(26, 23, 20)",
  "--ink": "rgb(32, 29, 26)",
  "--inset": "rgb(28, 25, 22)",
  "--panel": "rgb(37, 34, 32)",
  "--raised": "rgb(40, 36, 32)",
  "--canvas": "rgb(36, 32, 25)",
  "--active": "rgb(51, 46, 40)",
  "--hover": "rgb(42, 38, 34)",
  "--border": "rgb(61, 55, 47)",
  "--border-dashed": "rgb(74, 67, 55)",
  "--border-hi": "rgb(90, 81, 66)",
  "--text-hi": "rgb(242, 234, 217)",
  "--text": "rgb(216, 210, 200)",
  "--text-secondary": "rgb(184, 174, 158)",
  "--text-dim": "rgb(154, 143, 125)",
  "--text-faint": "rgb(110, 103, 92)",
  "--amber": "rgb(255, 180, 84)",
  "--amber-hi": "rgb(255, 198, 120)",
  "--green": "rgb(158, 206, 106)",
  "--cyan": "rgb(86, 197, 208)",
  "--violet": "rgb(200, 164, 255)",
  "--red": "rgb(224, 108, 96)",
};

test("every design token resolves to its declared colour", async ({ page }) => {
  await page.goto("/dev/tokens");

  const resolved = await page.evaluate((names) => {
    const probe = document.createElement("div");
    document.body.append(probe);
    const out = {};
    for (const name of names) {
      probe.style.color = "";
      probe.style.color = `var(${name})`;
      out[name] = getComputedStyle(probe).color;
    }
    probe.remove();
    return out;
  }, Object.keys(PALETTE));

  for (const [name, want] of Object.entries(PALETTE)) {
    // An undefined custom property leaves the declaration invalid and the
    // element inherits — which looks like a styling opinion, not a bug.
    expect(resolved[name], `${name} does not resolve`).toBe(want);
  }
});

test("the shape language holds: 2px borders, square corners, no gradients", async ({ page }) => {
  await page.goto("/forge/map");

  const shape = await page.evaluate(() => {
    const el = document.querySelector('[data-testid="rail-map"]');
    const cs = getComputedStyle(el);
    return {
      borderWidth: cs.borderTopWidth,
      radius: cs.borderTopLeftRadius,
      backgroundImage: cs.backgroundImage,
    };
  });
  expect(shape.borderWidth).toBe("2px");
  expect(shape.radius).toBe("0px");
  expect(shape.backgroundImage).toBe("none");
});

test("the reference page renders the whole palette", async ({ page }) => {
  await page.goto("/dev/tokens");
  const body = await page.locator("body").innerText();
  for (const name of Object.keys(PALETTE)) {
    expect(body, `/dev/tokens does not document ${name}`).toContain(name);
  }
});
