// Playwright configuration for the Forge end-to-end suite.
//
// Run it with scripts/e2e.sh (or `make e2e`), which builds NODE_PATH from
// mise's install prefixes — a bare `playwright test` cannot resolve
// "@playwright/test" from a spec file.
//
// The suite exists because the failures Forge actually ships are invisible to
// `go test`: a Datastar attribute naming an unregistered plugin renders
// perfectly and does nothing, and no Go test, linter or screenshot can see it.
// Everything here therefore drives a real browser and asserts on behaviour —
// requests made, elements changed — rather than on markup.

const { defineConfig, devices } = require("@playwright/test");

// A dedicated port, so a suite run never talks to (or kills) a Forge instance
// the developer is using on the default 7777.
// Must match [forge].addr in e2e/fixtures/project/game.toml.
const PORT = 7788;
const BASE_URL = `http://127.0.0.1:${PORT}`;

// Forge is a fixed-viewport desktop tool, not a responsive site. The viewport
// comes after the device spread, or the preset's 1280x720 wins.
const chrome = { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 } };

module.exports = defineConfig({
  testDir: "./specs",
  // Forge is a local, single-user authoring tool. There is no flaky network to
  // absorb, so a retry would hide a real defect rather than smooth one over.
  retries: 0,
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : [["list"]],

  use: {
    baseURL: BASE_URL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },

  projects: [
    {
      name: "app",
      // Everything that only reads. Runs fully parallel.
      testIgnore: /06-engine-status/,
      use: { ...chrome },
    },
    {
      // The engine-status specs rename the fixture database away and rewrite
      // its schema_version to provoke the offline and mismatch states. There
      // is one server and one database, so they cannot share it with specs
      // reading through the same process — `dependencies` makes them run only
      // after everything else has finished, rather than racing it.
      name: "engine-status",
      testMatch: /06-engine-status/,
      dependencies: ["app"],
      use: { ...chrome },
    },
  ],

  webServer: {
    // Build first: the asset tree is embedded with go:embed, so a stale binary
    // serves stale CSS and templates, and the suite would be testing the last
    // build rather than the working tree.
    // Three steps, in order, and all of them matter:
    //   build  — the asset tree is embedded with go:embed, so a stale binary
    //            serves stale CSS and templates and the suite would be testing
    //            the last build rather than the working tree
    //   seed   — rebuilds the fixture database from scratch, so every run
    //            starts from the same known population
    //   serve  — against the fixture project, never the developer's own
    command: [
      "make build-headless",
      "go run ./e2e/fixtures/seed",
      "./bin/ecs-db-headless forge --config e2e/fixtures/project/game.toml",
    ].join(" && "),
    url: `${BASE_URL}/healthz`,
    // The port is dedicated, so anything already on it is a leftover from an
    // interrupted run rather than something worth reusing.
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: "pipe",
    stderr: "pipe",
    cwd: "..",
  },
});
