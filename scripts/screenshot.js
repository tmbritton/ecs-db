#!/usr/bin/env node
// Screenshot a running Forge page (or any URL) with Playwright.
//
// Usage:
//   node scripts/screenshot.js <url> [preset] [outFile] [--full]
//
//   preset:  desktop (1440x900, default) | wide (1920x1080) | small (1180x720)
//   outFile: defaults to tmp/shots/<derived-name>-<preset>.png
//   --full   capture the whole scrollable page (for /dev/tokens)
//
// Playwright is pinned in .mise.toml. First time on a machine:
//   mise install && mise exec -- playwright install chromium
//
// Exit codes:
//   0  clean
//   1  usage error, or the page could not be loaded at all
//   2  page rendered but reported problems — a console error, a failed request,
//      or any HTTP >= 400. Forge must work offline and self-contained, so a
//      failed request is a real defect rather than noise. The screenshot is
//      still written, so you can look at what went wrong.

const path = require("path");
const fs = require("fs");

// Playwright is pinned as `npm:playwright`, but mise's npm backend installs it
// outside the paths require() searches, so a plain require() genuinely fails
// here. Resolve it explicitly so the script runs with no NODE_PATH wrapper.
function loadPlaywright() {
  const notFound = (err) => err && err.code === "MODULE_NOT_FOUND";

  try {
    return require("playwright");
  } catch (err) {
    // Only a missing module is worth falling back on. A syntax error or a
    // broken transitive dep must surface, not hide behind "run mise install".
    if (!notFound(err)) throw err;
  }

  const { execFileSync } = require("child_process");
  const roots = [];
  for (const [bin, args, toRoot] of [
    ["mise", ["where", "npm:playwright"], (out) => path.join(out, "lib", "node_modules")],
    ["npm", ["root", "-g"], (out) => out],
  ]) {
    try {
      // stdio: pipe — otherwise a missing `mise` dumps subprocess noise ahead
      // of the friendly message below.
      const out = execFileSync(bin, args, {
        encoding: "utf8",
        stdio: ["ignore", "pipe", "pipe"],
      }).trim();
      if (out) roots.push(toRoot(out));
    } catch {
      // bin absent or errored — try the next candidate.
    }
  }

  for (const root of roots) {
    try {
      return require(path.join(root, "playwright"));
    } catch (err) {
      if (!notFound(err)) throw err;
    }
  }

  console.error(
    "Could not load Playwright.\n" +
      "  mise install                              # installs npm:playwright\n" +
      "  mise exec -- playwright install chromium  # downloads the browser\n" +
      `Searched: ${roots.join(", ") || "(nothing resolved)"}`
  );
  process.exit(1);
}

const { chromium } = loadPlaywright();

// Forge is a fixed-viewport desktop tool, not a responsive site. These presets
// are about how much of the six-panel layout fits, not about breakpoints.
const PRESETS = {
  desktop: { width: 1440, height: 900 },
  wide: { width: 1920, height: 1080 },
  small: { width: 1180, height: 720 },
};

function usage(msg) {
  if (msg) console.error(msg);
  console.error("Usage: node scripts/screenshot.js <url> [desktop|wide|small] [outFile] [--full]");
  process.exit(1);
}

function parseArgs(argv) {
  const flags = new Set();
  const positional = [];
  for (const a of argv) {
    if (a.startsWith("--")) flags.add(a);
    else positional.push(a);
  }
  const unknown = [...flags].filter((f) => f !== "--full");
  if (unknown.length) usage(`Unknown flag(s): ${unknown.join(", ")}`);
  return { positional, fullPage: flags.has("--full") };
}

function defaultOutFile(url, preset) {
  const u = new URL(url);
  // decodeURIComponent so "/Forge%20Editor.dc.html" doesn't become a filename
  // with a literal %20 in it.
  const slug =
    decodeURIComponent(u.pathname).replace(/^\/|\/$/g, "").replace(/[/\s]+/g, "-") || "index";
  return path.join("tmp", "shots", `${slug}-${preset}.png`);
}

async function main() {
  const { positional, fullPage } = parseArgs(process.argv.slice(2));
  const [url, preset = "desktop", explicitOut] = positional;

  if (!url) usage();
  const viewport = PRESETS[preset];
  if (!viewport) usage(`Unknown preset "${preset}". Use one of: ${Object.keys(PRESETS).join(", ")}`);

  const outFile = explicitOut || defaultOutFile(url, preset);
  fs.mkdirSync(path.dirname(outFile), { recursive: true });

  const problems = [];
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport });

    page.on("console", (msg) => {
      if (msg.type() === "error") problems.push(`console error: ${msg.text()}`);
    });
    page.on("pageerror", (err) => problems.push(`page error: ${String(err)}`));
    page.on("requestfailed", (req) => {
      problems.push(`request failed: ${req.method()} ${req.url()} — ${req.failure()?.errorText}`);
    });
    // requestfailed covers transport failures only; a 404 on a stylesheet or an
    // SSE endpoint is a successful response with a bad status, and matters just
    // as much for a page that must be self-contained.
    page.on("response", (resp) => {
      if (resp.status() >= 400) problems.push(`HTTP ${resp.status()}: ${resp.url()}`);
    });

    // "load", not "networkidle": every Forge mode page holds an open SSE stream
    // (/forge/{mode}/events), so the network never goes idle and networkidle
    // would always time out.
    await page.goto(url, { waitUntil: "load", timeout: 15000 });

    // Webfonts are central to the design, so wait for them before capturing.
    // Note the `void` — document.fonts.ready resolves to a FontFaceSet, which
    // is not serializable back across the Playwright boundary.
    await page.evaluate(() => document.fonts.ready.then(() => undefined));
    await page.waitForTimeout(300);

    await page.screenshot({ path: outFile, fullPage });
  } finally {
    await browser.close();
  }

  console.log(outFile);
  for (const p of problems) console.error(p);
  if (problems.length) process.exit(2);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
