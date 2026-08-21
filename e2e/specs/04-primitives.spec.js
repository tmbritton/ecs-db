// Story 4 — templ primitives, exercised through the /dev/tokens gallery.
//
// This is the spec that would have caught the defect that shipped: every
// Datastar handler in the package was inert, because v1.0.2 separates a plugin
// from its key with a colon and the whole package used `data-on-click`, which
// parses as a plugin named "on-click" and is skipped in silence. The markup
// was correct, the tests passed, the screenshots looked right, and nothing
// worked. So these tests assert on *effects* — a request on the wire, a class
// that changed — never on the presence of an attribute.

const { test, expect, byTestId, expectSettled } = require("../fixtures");

const demo = (page, name) => byTestId(page, `demo-${name}`);

test.beforeEach(async ({ page }) => {
  await page.goto("/dev/tokens");
});

test.describe("actions reach the server", () => {
  // The gallery wires real actions against a real endpoint rather than
  // decorative ones, precisely so this can be checked.
  const clickable = [
    ["listrow", ".list-row"],
    ["chip", ".chip__remove"],
    // Not just any segment: the card holds three groups and each has one
    // already selected, and re-clicking a checked radio fires no change event.
    ["segmented", ".segment:not(.segment--selected)"],
    ["iconbutton", ".icon-btn:not(:disabled)"],
    ["ctxmenu", ".ctx-menu__item"],
    ["savefooter", ".save-footer button:not(:disabled)"],
  ];

  for (const [name, selector] of clickable) {
    test(`${name} fires its action`, async ({ page }) => {
      const posts = [];
      page.on("request", (r) => {
        if (new URL(r.url()).pathname === "/dev/noop") posts.push(r.method());
      });

      await demo(page, name).locator(selector).first().click();
      // Settled, not merely reached: a handler bound twice fires a duplicate a
      // moment later, which a plain poll would never see.
      await expectSettled(page, () => posts, ["POST"], {
        message: `${name} should reach the server exactly once`,
      });
    });
  }

  test("a disabled icon button fires nothing", async ({ page }) => {
    const posts = [];
    page.on("request", (r) => {
      if (new URL(r.url()).pathname === "/dev/noop") posts.push(r.method());
    });

    const disabled = demo(page, "iconbutton").locator(".icon-btn:disabled").first();
    await expect(disabled).toBeDisabled();
    await disabled.click({ force: true });
    await page.waitForTimeout(300);
    expect(posts).toEqual([]);
  });
});

test.describe("controls carry state, not just appearance", () => {
  // A control with an action but no signal binding fires a request the server
  // cannot interpret: an action posts the page's *signals*, and a plain HTML
  // `name` attribute contributes nothing outside a form submission, which this
  // architecture does not have.
  test("the segmented control puts its value in the signal store", async ({ page }) => {
    // Scoped to one group: the card demonstrates three, each bound to its own
    // signal, so a card-wide query would legitimately see three names.
    const group = demo(page, "segmented").locator(".segmented").first();
    const inputs = group.locator(".segment__input");
    await expect(inputs.first()).toHaveAttribute("data-bind", /.+/);

    // Radios in one group share a name, which is what makes them mutually
    // exclusive and arrow-navigable. Datastar derives it from the bound signal
    // path — there is no `name` attribute in the template to do it by hand.
    const names = await inputs.evaluateAll((els) => [...new Set(els.map((e) => e.name))]);
    expect(names).toHaveLength(1);
    expect(names[0]).not.toBe("");
  });

  test("selecting a segment moves the selection", async ({ page }) => {
    const segments = demo(page, "segmented").locator(".segmented").first().locator(".segment");
    const second = segments.nth(1);

    await second.click();
    await expect(second.locator(".segment__input")).toBeChecked();
    await expect(segments.nth(0).locator(".segment__input")).not.toBeChecked();
  });

  test("the dropdown is signal-bound", async ({ page }) => {
    await expect(demo(page, "inputs").locator("select")).toHaveAttribute("data-bind", /.+/);
  });
});

test.describe("the modal dismisses only from the scrim", () => {
  // Three behaviours that took three attempts to get right. A bare backdrop
  // handler fires for clicks bubbling out of the dialog; the `__outside`
  // modifier binds at the *document*, so it fires for clicks anywhere else on
  // the page. Only an explicit `evt.target === el` test means "the scrim".
  const dismissals = () => [];

  test("clicking the scrim dismisses", async ({ page }) => {
    const posts = dismissals();
    page.on("request", (r) => {
      if (new URL(r.url()).pathname === "/dev/noop") posts.push(r.url());
    });

    // Click the backdrop's top-left corner, which the dialog does not cover.
    // locator.click scrolls the element into view first; page.mouse.click takes
    // viewport coordinates, and /dev/tokens is long enough that a boundingBox
    // from below the fold lands somewhere else entirely — which reads as
    // "dismissal is broken" and has already cost an afternoon once.
    await demo(page, "modal").locator(".modal-backdrop").click({ position: { x: 4, y: 4 } });

    await expectSettled(page, () => posts.length, 1, {
      message: "the scrim should dismiss exactly once",
    });
  });

  test("clicking inside the dialog does not dismiss", async ({ page }) => {
    const posts = dismissals();
    page.on("request", (r) => {
      if (new URL(r.url()).pathname === "/dev/noop") posts.push(r.url());
    });

    await demo(page, "modal").locator(".modal__body").click();
    await page.waitForTimeout(300);
    expect(posts, "a click inside the dialog closed it").toEqual([]);
  });

  test("clicking elsewhere on the page does not dismiss", async ({ page }) => {
    const posts = dismissals();
    page.on("request", (r) => {
      if (new URL(r.url()).pathname === "/dev/noop") posts.push(r.url());
    });

    // The failure mode of the `__outside` modifier: it binds at the document,
    // so this click would have closed the dialog.
    await page.locator("body").click({ position: { x: 5, y: 5 } });
    await page.waitForTimeout(300);
    expect(posts, "a click elsewhere on the page closed the dialog").toEqual([]);
  });
});

test.describe("accessibility of the primitives", () => {
  // Selection here is by test id, so these say out loud what role-based
  // selectors would otherwise have checked by accident. Each corresponds to a
  // defect found in review.
  test("an activatable row is a real button, not a div with a handler", async ({ page }) => {
    const row = demo(page, "listrow").locator(".list-row").first();
    expect(await row.evaluate((el) => el.tagName)).toBe("BUTTON");
    // Which means it is keyboard-operable with no code of ours.
    await row.focus();
    await expect(row).toBeFocused();
  });

  test("the chip's remove control is not announced as a glyph", async ({ page }) => {
    const remove = demo(page, "chip").locator(".chip__remove").first();
    const name = await remove.getAttribute("aria-label");
    expect(name, "the remove button announces its glyph").not.toBe("✕");
    expect(name).toBeTruthy();
  });

  test("clipped radio and checkbox inputs stay in the focus order", async ({ page }) => {
    // display:none would remove them from the focus order entirely; the design
    // needs them invisible but still reachable.
    for (const [name, selector] of [
      ["segmented", ".segment__input"],
      ["inputs", ".checkbox__input"],
    ]) {
      const input = demo(page, name).locator(selector).first();
      await input.focus();
      await expect(input).toBeFocused();
    }
  });

  test("the dialog is a labelled, modal dialog", async ({ page }) => {
    const dialog = demo(page, "modal").locator('[role="dialog"]');
    await expect(dialog).toHaveAttribute("aria-modal", "true");
    await expect(dialog).toHaveAccessibleName(/.+/);
  });
});

test.describe("the shape language", () => {
  test("selection reads as the active fill plus an amber edge", async ({ page }) => {
    const active = demo(page, "listrow").locator(".list-row--active").first();
    const style = await active.evaluate((el) => {
      const cs = getComputedStyle(el);
      return { background: cs.backgroundColor, borderLeft: cs.borderLeftColor };
    });
    expect(style.background).toBe("rgb(51, 46, 40)"); // --active
    expect(style.borderLeft).toBe("rgb(255, 180, 84)"); // --amber
  });

  test("hover does not erase the selected row's fill", async ({ page }) => {
    const active = demo(page, "listrow").locator(".list-row--active").first();
    await active.hover();
    await expect(active).toHaveCSS("background-color", "rgb(51, 46, 40)");
  });
});
