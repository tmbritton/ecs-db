// Shared fixtures for the Forge e2e suite.
//
// The point of this file is the `page` override below. Forge's characteristic
// failure is silent: a Datastar attribute that names an unregistered plugin is
// skipped with no error and no visual difference, a stylesheet 404 leaves the
// page merely ugly, and an exception inside a handler is swallowed by the
// framework. None of it fails a test that only asserts on markup.
//
// So every test in this suite watches the browser's own error channels for its
// whole lifetime and fails at the end if anything was reported. Individual
// tests opt out for one navigation with `expectPageErrors`.

const base = require("@playwright/test");
const { expect } = base;

const test = base.test.extend({
  page: async ({ page }, use) => {
    const problems = [];
    // A pattern, not a boolean. A blanket mute would drop every unexpected
    // console error, exception and 5xx that happened to occur during the same
    // navigation as the one deliberate failure.
    let allowed = [];
    const record = (msg) => {
      if (allowed.some((re) => re.test(msg))) return;
      problems.push(msg);
    };

    page.on("console", (m) => m.type() === "error" && record(`console error: ${m.text()}`));
    page.on("pageerror", (e) => record(`uncaught exception: ${e}`));
    page.on("requestfailed", (r) => {
      // An abort is the client deciding to stop, not a failure to serve: the
      // page-level SSE stream is cancelled on every navigation away, Datastar
      // cancels a superseded action by default, and teardown cancels whatever
      // is in flight. None of that is what this watch is for.
      //
      // Scoped to the two paths that legitimately get cancelled rather than
      // waived everywhere. A server that hangs up mid-body reports
      // ERR_INCOMPLETE_CHUNKED_ENCODING or ERR_CONNECTION_RESET, not
      // ERR_ABORTED, so real transport failures still bite either way — but a
      // narrow exemption is one fewer place for a future failure to hide.
      const err = r.failure()?.errorText || "";
      const cancellable = isEventStream(r.url()) || r.method() !== "GET";
      if (err.includes("ERR_ABORTED") && cancellable) return;
      record(`request failed: ${r.method()} ${r.url()} — ${err}`);
    });
    // requestfailed covers transport errors only. A 404 on a stylesheet is a
    // successful response with a bad status, and matters just as much for a
    // tool that must work offline and self-contained.
    page.on("response", (r) => r.status() >= 400 && record(`HTTP ${r.status()}: ${r.url()}`));

    // Lets a test assert on a deliberate failure — a 404 probe, say — without
    // the surrounding watch turning it into a suite failure.
    //
    // Takes patterns rather than a bare on/off flag: a blanket mute would drop
    // every unexpected console error, exception and 5xx that happened to occur
    // during the same navigation. One deliberate failure usually needs two
    // patterns, because it arrives on two channels with different wording —
    // the response channel knows the URL, the browser's console message does
    // not. Anything not matched still fails the test.
    page.expectPageErrors = async (patterns, fn) => {
      allowed = Array.isArray(patterns) ? patterns : [patterns];
      try {
        return await fn();
      } finally {
        allowed = [];
      }
    };

    await use(page);

    if (problems.length) {
      throw new Error(`the page reported ${problems.length} problem(s):\n  ${problems.join("\n  ")}`);
    }
  },
});

// Datastar appends the page's signal state to an action's URL, so a stream
// request arrives as "/forge/map/events?datastar=%7B%7D" rather than a bare
// path. Matching on a bare suffix finds nothing — which reads exactly like a
// page that never subscribed, and cost an afternoon once already.
function isEventStream(url) {
  return new URL(url).pathname.endsWith("/events");
}

// Test IDs are the selector of record for this suite: they survive the copy
// and markup churn that the modes will go through, and they say plainly that
// an element is depended on. `byTestId` keeps the attribute name in one place.
const byTestId = (page, id) => page.locator(`[data-testid="${id}"]`);

// expect.poll stops the moment its assertion first passes, so anything that
// happens *after* that is invisible: a second SSE subscription opened on a
// timer, a duplicate POST from a double-bound handler. Reaching the expected
// value is necessary but not sufficient — it has to still hold once the page
// has settled.
//
// Verified: with a page that opened a second stream 500ms after load, the
// plain expect.poll form passed 20/20.
async function expectSettled(page, read, expected, { settleMs = 900, message } = {}) {
  await expect.poll(read, { message }).toEqual(expected);
  await page.waitForTimeout(settleMs);
  expect(read(), message).toEqual(expected);
}

// Collects the paths of every SSE subscription a page opens, so "exactly one
// stream per page" can be measured rather than assumed.
function watchEventStreams(page) {
  const seen = [];
  page.on("request", (r) => {
    if (isEventStream(r.url())) seen.push(new URL(r.url()).pathname);
  });
  return seen;
}

// The six modes, in rail order. Mirrors internal/forge/mode/mode.go; the specs
// that walk the rail check this list against what the page actually renders,
// so the two cannot drift silently.
const MODES = [
  { slug: "map", caption: "MAP", title: "Map" },
  { slug: "tiles", caption: "TILES", title: "Tiles" },
  { slug: "ents", caption: "ENTS", title: "Entity Types" },
  { slug: "schema", caption: "SCHEMA", title: "Schema" },
  { slug: "agents", caption: "AGENTS", title: "Agents" },
  { slug: "sprites", caption: "SPRT", title: "Sprites" },
];

module.exports = {
  test,
  expect: base.expect,
  byTestId,
  expectSettled,
  watchEventStreams,
  isEventStream,
  MODES,
};
