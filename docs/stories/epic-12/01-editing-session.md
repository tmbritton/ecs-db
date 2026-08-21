# Story 1: Editing session — one editable schema, saved once

**Epic:** 12 — Forge: SCHEMA & ENTS modes  
**Status:** 🔲 Not started  
**Priority:** High — both modes edit the same file through this

**Depends on:** Epic 11 (complete)

## Context

`schema.json` has two halves and Forge shows them in two modes, but it is one file. SCHEMA edits `components`, ENTS edits `entityTypes`, and both save the same document. If each mode owned its own copy, saving one would clobber the other's unsaved work — and the bug would only appear when someone edited both before saving, which is the normal way to add a component and immediately use it.

So there is one editing session per project, and both modes are views onto it. Epic 11 built every piece: `editable.File[schema.DatabaseSchema]` with `schema.Marshal`, `LoadSchema` and `ValidateSchema` as its codec, dirty tracking by byte comparison, atomic writes, conflict detection, and `savereport` for the outcome. This story assembles them and puts a working save footer on the screen.

It is also where Forge does its first mutating request. That shape is worth getting right once: Datastar posts the page's signals to an action endpoint, the server mutates the session, and the response is a patch stream that re-renders whatever changed. Every editing story after this one copies it.

## Acceptance Criteria

- [ ] One `editable.File[schema.DatabaseSchema]` per project, held by the server and shared by every mode
- [ ] Codec is the real one: `schema.Marshal`, `schema.LoadSchema`, `schema.ValidateSchema` — no bespoke serialisation anywhere in this epic
- [ ] `POST /forge/schema/save` and `POST /forge/schema/discard` — actions, not pages; the response is a Datastar patch stream
- [ ] The save footer shows the file name and real dirty state, and both buttons are disabled when clean
- [ ] Saving reports through `savereport`, on the page-level SSE stream, with the outcome wording Epic 11 Story 6 established
- [ ] A conflict offers both resolutions (`Reload` / `SaveOverwriting`); neither is taken silently
- [ ] Discard restores the last saved state and the footer returns to clean
- [ ] A save that fails validation writes nothing and keeps the edit
- [ ] The session is safe for concurrent requests — `editable.File` is documented as not safe for concurrent use, so the thing that owns it provides the lock
- [ ] A project that failed to open leaves the modes readable and says why, rather than 500ing
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/12-editing-session.spec.js`.

- [ ] The footer is clean on load and names `schema.json`
- [ ] Both footer buttons are disabled when clean
- [ ] After an edit the footer becomes dirty **without a page reload** — the patch arrives on the page's existing SSE stream
- [ ] Editing back to the original value returns the footer to clean. A mutation flag passes every other step and fails this one
- [ ] Save writes the file and the footer returns to clean
- [ ] Discard restores the displayed value and returns to clean
- [ ] The save report appears with the wording for a database that is present and matching (`hot-reload live`)
- [ ] The page still holds exactly **one** event-stream request afterwards
- [ ] Switching to ENTS and back does not lose an unsaved SCHEMA edit — the two modes share one session, and a full page load between them must not reset it

That last one is the point of the story and the easiest to get wrong: mode switching is a full page load, so the session has to live on the server, not in the page.

## Notes

- **The whole epic funnels through here.** No mode calls `schema.Marshal` or touches the filesystem itself; see the rule in the epic README.
- Forge is a local single-user tool, so a mutex around the session is the right amount of concurrency control. Do not build a document-versioning scheme.
- Mode switching is a full page load (Epic 10 Story 5). The session outliving the request is what makes unsaved edits survive it.
- `editable.File.Current` is replaced wholesale by `Discard` and `Reload`, so nothing may cache a pointer into it across a request.
- The save endpoint lives under `/forge/schema/` even though ENTS also saves: there is one file, and inventing a second path for the same document would imply otherwise.
