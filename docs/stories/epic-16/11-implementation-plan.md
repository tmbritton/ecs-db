# Story 11 — project-local sprite import: implementation plan ✅ Complete

1. Tests first for listing safe project-local PNGs and validating source
   dimensions/decoding, names and duplicate destination. Ensure external and
   symlink escapes are refused and temp output is never a visible `.png`.
2. Implement import in `internal/forge/animations` using the active Story 9
   session and a no-replace atomic copy into its sprites root. Preflight the
   working TOML addition without mutating it; after a successful copy, add
   the animation and optional binding to the held document. If a later step
   fails, remove only the file this request created.
3. Add an SPRT Import dialog reached by a URL, with candidates derived from
   the project root and a POST action scoped to the active animation file.
   Preserve the selected animation when closing, and keep source selection
   distinct from the working TOML. Pin browser test IDs in Go.
4. Run Playwright steps above, mutate the POST action to demonstrate the spec
   failing, restore it; run `make test`, both builds/lint tag sets and `make
   e2e`, update the story and roadmap, get staged review, commit and push.
