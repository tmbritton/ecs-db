#!/usr/bin/env bash
# Run the Forge end-to-end suite.
#
#   scripts/e2e.sh              # all specs, headless
#   scripts/e2e.sh --ui         # interactive runner
#   scripts/e2e.sh 05-shell     # one spec by filename fragment
#
# mise installs each npm package into its own prefix, which is not on Node's
# module search path, so `require("@playwright/test")` from a spec file cannot
# resolve on its own. NODE_PATH is built here from mise's own answer rather than
# hard-coded, so this works on any machine that has run `mise install`.
set -euo pipefail

cd "$(dirname "$0")/.."

need() {
  if ! mise where "$1" >/dev/null 2>&1; then
    echo "Missing $1. Run: mise install" >&2
    exit 1
  fi
}
need npm:@playwright/test

# Everything below runs out of the @playwright/test install and nothing else.
# Both npm:playwright and npm:@playwright/test ship a binary called
# "playwright", and mise resolves the name to the former — which loads a second
# copy of the runner. The specs then require the other copy, and Playwright
# rejects it with "did not expect test.describe() to be called here", an error
# that says nothing about two installs being on the path.
PW_TEST="$(mise where npm:@playwright/test)"
export NODE_PATH="$PW_TEST/lib/node_modules${NODE_PATH:+:$NODE_PATH}"

# The suite drives a real browser, so the browser has to be there, and the
# failure without it is opaque. Run unconditionally: the cache is version-keyed
# (chromium-1234/), and @playwright/mcp is pinned to "latest" in .mise.toml and
# writes to the same cache — so "the cache directory exists" does not mean *this*
# version's browser is in it, and the guard would skip the install exactly on the
# clean-machine path it exists for. Already-installed costs ~2s and no download.
"$PW_TEST/bin/playwright" install chromium >/dev/null

exec "$PW_TEST/bin/playwright" test --config e2e/playwright.config.js "$@"
