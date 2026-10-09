# Story 10 — SPRT animation and binding editor: implementation plan ✅ Complete

1. Test a read model for selected animation/binding, file resolution, image
   dimensions and first-row frame bounds at configured tile size. Build the
   preview from the working document, not from a second TOML decode. Confirm
   that a valid PNG under the first mod resolves and a later-mod/symlink path
   is refused. Keep unavailable art in the list with its reason.
2. Add the `window.tileSize` value to Forge's composition root and build a
   scoped image route backed by `Session.AssetImage`; the selected document
   must name the requested sheet. Reuse the asset route's media type and
   `nosniff`/no-cache behavior. Test successful and refused HTTP requests.
3. Write failing server tests for each edit/create route, invalid input and
   file/name targeting. Mutate `Document` only through `Session.Edit`. Use
   existing `setEditProblem` and save/report lifecycle.
4. Write the Playwright spec from the steps above and pin its test IDs in Go.
   Add Templ controls and strip/sequence preview, a small client-owned
   playback state if needed, and documented one-row limits. Regenerate Templ.
5. Run focused, full, tagged and browser verification, break a browser action
   and watch the spec fail, review the staged diff and update the roadmap.
