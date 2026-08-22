# Story 1: Round-trip fidelity

**Epic:** 13 — Forge: AGENTS mode  
**Status:** 🔲 Not started  
**Priority:** High — every story after this one ships a save button

**Depends on:** nothing (engine-side work in `internal/agent`)

## Context

The design prototype's AGENTS panel says `XState v4 · round-trips with Stately`.
That is the promise the whole mode rests on: author a machine in Forge or in
Stately Studio, move it between them, and the file survives.

It does not survive today. `ParseMachine` ignores fields it does not model —
its own doc comment says so — and `EmitMachine` writes only what it knows, so a
load-edit-save cycle deletes everything else in the file. A Stately export
carrying `description`, `tags` and `meta` comes back with all three gone, no
error and no warning. That is a silent destructive edit to someone's work, and
it gets worse the longer it ships, because the loss happens on a save the user
asked for and would never think to check.

So this is the first story of the epic rather than the last one, and it is
engine work: the fix belongs in `internal/agent` beside the parser and the
emitter, not in Forge.

It also settles a decision the canvas needs. `meta` is XState's sanctioned place
for arbitrary per-state data. Once unknown fields survive, layout coordinates
have a home inside the machine file — which has to be weighed against the
plan's `behaviors/<id>.layout.json` sidecar, and that sidecar has a problem of
its own: `Loader.ScanDir` loads every `*.json` in a behaviours directory, so a
sidecar there registers itself as a machine with an empty id, silently and
successfully.

## Acceptance Criteria

- [ ] `ParseMachine` retains fields it does not model, at machine level and at
      every state node, and `EmitMachine` writes them back
- [ ] A machine file that Forge has never edited is byte-identical after a
      parse → emit round trip, including files carrying `description`, `tags`,
      `meta` and anything else
- [ ] Retained fields keep their authored position, on the same terms as
      `StateOrder` and `ContextOrder` — a save must not reorder a file it did
      not otherwise change
- [ ] A retained field that collides with one the parser *does* model is the
      parser's, not the passenger's — round-tripping must never produce a file
      with two `initial` keys
- [ ] `invoke` is still rejected at every nesting level. It is not an unknown
      field; it is a known one the engine refuses, and preserving it would
      produce a file Forge accepts and the engine will not load
- [ ] Where canvas layout is stored is decided and written down, with the reason
- [ ] If layout goes in a sidecar, it is not a `*.json` file inside a behaviours
      directory — `ScanDir` loads those as machines
- [ ] Table tests over real Stately exports, not hand-written approximations of
      them
- [ ] `go test ./...` passes

## Playwright steps

None. This story has no UI — it is `internal/agent`, reached from Forge only in
Story 2. Resisting the urge to add a browser test for it is the point: the
suite exists for failures Go cannot see, and every failure here is one Go sees
perfectly well.

## Notes

- **The round trip is the test, not the field list.** Asserting that
  `description` survives is a test about `description`. Asserting that arbitrary
  input bytes come back unchanged is a test about the property, and it catches
  the field nobody thought of. Prefer the second, and drive it from files.
- Watch the interaction with `Dirty`. `editable.File` compares marshalled bytes
  against the file on disk, so a machine that does not round-trip byte-stably is
  permanently dirty for no visible reason — `editable`'s package doc already
  calls this a real dependency in both directions. A fidelity bug here surfaces
  in Story 2 as a save footer that will not go clean.
- **Against the sidecar, briefly:** it is a second file to keep in step with the
  first, it needs its own conflict handling, it is invisible to Stately, and the
  obvious place to put it is a directory that would load it as a machine. In
  favour: it keeps layout out of files the engine reads, and `meta` is a field
  users may want for their own purposes. Decide it here, in writing.
- If layout does go in the file, note that the engine ignores it — the loader
  parses `meta` into nothing and the interpreter never reads it — so a layout
  written by Forge cannot change how a machine runs. Say so in the story's
  *As Implemented*, because "the editor writes to the file the game loads" is a
  claim that deserves the evidence.
