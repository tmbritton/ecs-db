# Story 1: Round-trip fidelity

**Epic:** 13 — Forge: AGENTS mode  
**Status:** ✅ Complete  
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

- [x] `ParseMachine` retains fields it does not model, at machine level and at
      every state node, and `EmitMachine` writes them back
- [x] **No data loss:** every key in the input is present in the output with a
      JSON-equal value, asserted by comparing decoded values rather than by
      grepping for field names
- [x] **Idempotence:** `emit(parse(emit(parse(x)))) == emit(parse(x))`, which is
      what `editable.Dirty` needs — it compares marshalled bytes against the
      file on disk, so a machine that does not settle is permanently dirty for
      no visible reason
- [x] **No regression:** the two machine files Forge will actually edit
      (`behaviors/goblin.json`, the e2e fixture) stay byte-identical
- [x] ~~A machine file that Forge has never edited is byte-identical after a
      parse → emit round trip~~ — **not achievable and not the right goal**; the
      emitter has one canonical style by design. Replaced by the three above,
      and the normalizations that remain are inventoried. See the plan
- [x] Retained fields keep their order relative to each other and follow the
      known fields. (~~authored position~~ — `writeStateNode` already emits
      *known* fields in a fixed order, so interleaving unknowns by authored
      index produces a jumble)
- [x] The remaining normalizations are pinned by golden files, so a new one
      cannot appear without changing a checked-in file
- [x] A retained field that collides with one the parser *does* model is the
      parser's, not the passenger's — round-tripping must never produce a file
      with two `initial` keys
- [x] `invoke` is still rejected at every nesting level. It is not an unknown
      field; it is a known one the engine refuses, and preserving it would
      produce a file Forge accepts and the engine will not load
- [x] Where canvas layout is stored is decided and written down, with the reason
- [x] If layout goes in a sidecar, it is not a `*.json` file inside a behaviours
      directory — `ScanDir` loads those as machines
- [x] Table tests over real Stately exports, not hand-written approximations of
      them
- [x] `go test ./...` passes

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

## As Implemented

`ParseMachine` keeps every field it does not model, verbatim and in document
order, and `EmitMachine` writes them back — at four levels: the machine, each
state node, each transition, and each action and guard.

Coverage: `agent` 88.1%.

**That sentence originally said "at machine level and at every state node", and
claimed a Forge save no longer deletes anything. Both were wrong**, and the
review found them by looking for inputs the corpus did not contain. See *What
the review caught*.

### It was worse than unknown fields

The story was written about `description`, `tags` and `meta`. Running the
current round trip over every machine file in the repo first — before designing
anything — turned up two more problems and settled the shape of the work:

| File | Before | After |
|---|---|---|
| `behaviors/goblin.json` | byte-identical | byte-identical |
| `e2e/.../e2e-wander.json` | byte-identical | byte-identical |
| `testdata/behaviors/burning.json` | rewritten | byte-identical |
| `testdata/behaviors/wandering_goblin.json` | rewritten | 3 lines, all formatting |

The extra problem was **shorthand expansion**. XState accepts several spellings
of one thing — `"on": {"E": "next"}`, `{"E": {"target": "next"}}` and
`{"E": [{"target": "next"}]}` are one transition three ways, and `entry`, `exit`
and `cond` are the same — and the emitter wrote its own preferred spelling
regardless of what the file said. That is not data loss, but it is a rewrite of
lines nobody edited, which is the thing `internal/jsonorder` exists to prevent.

It also mattered more than it looked: **Stately Studio writes the object form.**
So the epic's headline claim was failing on the one input it names. `Form`,
`Transition.Bare`, `ActionSpec.Bare` and `CondSpec.Bare` now record which
spelling was authored, and the emitter reproduces it.

The zero value of all four is the canonical form, so a machine built in code —
a state added on the canvas in Story 5 — emits the way the live files are
written without anything having to say so.

**A spelling is preserved only while it can still say what the value says.**
Adding a guard to a bare transition, or a second action to an unwrapped one,
expands it. That guard is the difference between preserving formatting and
writing a file that has lost information, and it has a test per case — none of
which a round trip can reach, because parsing never produces a bare transition
that also has a guard.

### Two acceptance criteria of my own were wrong

Both were written before the measurement above and both assumed something untrue
about the emitter. They are struck through in place rather than quietly
rewritten:

- *"byte-identical after a parse → emit round trip"* — the emitter has one
  canonical whitespace style by design, so a file authored in another one is
  reformatted no matter what. Replaced by three properties that are actually
  the goal: **no data loss**, **idempotence**, and **no regression on the live
  files**.
- *"Retained fields keep their authored position"* — `writeStateNode` emits
  *known* fields in a fixed order, so slotting unknown ones back into their
  original slots among already-reordered known ones would produce a jumble.
  Replaced by: relative order among the extras is kept, and they follow the
  known fields.

### The known-key set is derived, not typed

`machineKeys` and `stateKeys` come from reflection over the `json` tags of
`rawMachine` and `rawStateNode`. A hand-written list would go stale the first
time someone added a field to the parser, and it fails badly rather than
harmlessly: the new field would be parsed normally *and* captured as an extra,
so the emitter would write it twice and produce a file with two `initial` keys.
Deriving it means adding a field to the parser automatically stops it being an
extra.

The lookup is lowercased because `encoding/json`'s field matching is
case-insensitive — a machine authored with `"Initial"` decodes into `Initial`,
so an extra by that name has already been consumed. `invoke` is in those tags
too, so it is excluded by construction, which is the right answer: it is a known
field the engine refuses, not an unknown one, and preserving it would produce a
file Forge accepts and the engine will not load.

### `json.Indent` was the obvious choice and the wrong one

Extras have to be written at the depth they are being written at, and
`json.Indent` does that — while imposing its own layout. `["enemy",
"stationary"]` came back as three lines and `{ "x": 320, "y": 200 }` as four, so
every Stately file was reformatted by the change that was supposed to stop
reformatting it.

Since `json.RawMessage` keeps the source bytes whitespace and all, the fix is to
shift the value's indentation rather than re-render it: re-base every line after
the first from the value's own shallowest indent to the target. For a plain
round trip the depth is unchanged, so the text comes back byte for byte — and an
extra keeps the author's own spacing, including a four-space file's.

### What still changes, and why that is written down

Three formatting normalizations remain, pinned by golden files under
`testdata/golden/` so a fourth cannot appear without changing a checked-in file:

1. **Extras move to the end of their object** — deliberate, per the corrected
   criterion above.
2. **A context value's number formatting is canonicalised**: `"speed": 2.0`
   becomes `2`. `Context` decodes to `map[string]any`, so the authored text is
   gone before anything could preserve it. An *extra's* formatting survives,
   because extras keep their bytes.
3. **Array wrapping is canonicalised**: a single-element `entry` written inline
   is expanded, a single transition written across lines is inlined. *Which*
   spelling was used is preserved; how it was wrapped is not.

Only (2) and (3) can make a file dirty the moment it is opened, and neither
touches the two files Forge actually edits. **Story 2 has to say "reformatting"
rather than a bare "unsaved changes" when it happens.**

One of those is more than formatting at the extreme. A context value decodes to
`float64`, so an integer past that type's exact range comes back *changed*, not
merely reformatted — `12345678901234567890` becomes `12345678901234567000`. That
is real loss, and it is pinned by
`TestRoundTrip_ContextNumbersAreLimitedToFloat64` rather than left to be
discovered. The corpus deliberately holds no such file, because the property
test would rightly fail on one. Extras are unaffected: they keep their bytes, so
an unmodelled field carries a number of any size exactly.

The property test compares numbers as **exact rationals**, which puts the line in
the right place: `2.0` emitted as `2` is the same value and passes, while a
precision loss is a different value and fails. A string comparison would reject
the first; a `float64` comparison would accept the second, because both sides
would already have been rounded to the same wrong number before anything looked.
The first version of that test did the latter, behind a `normalise` helper that
recursively rebuilt maps and slices and returned everything unchanged — nineteen
lines that read as careful and did nothing, in a test whose whole job is to be
careful.

### Where layout lives

Not `behaviors/<id>.layout.json`. `Loader.ScanDir` loads every `*.json` in a
behaviours directory, and the sidecar does not fail — it *succeeds*, registering
a machine with an empty id, so two sidecars would collide on `""` and the
machine list would carry a nameless row.

**Canvas coordinates go in each state's own `meta`, under a `forge` key.**

- `meta` is XState's sanctioned place for arbitrary per-state data, and it now
  survives a round trip.
- It is colocated: deleting a state deletes its layout with it, so there is no
  second file to fall out of step and no orphan to clean up.
- One file means one conflict path, one atomic write, one dirty flag.
- The engine ignores it. `rawStateNode` has no `meta` field, so it reaches
  `Extra` and nothing in the interpreter or the validator reads it —
  `TestExtras_ChangeNothingTheEngineReads` asserts that rather than claiming it,
  by validating a decorated machine against a plain one and comparing the
  manifest, the entry actions, the initial state and the context.

The cost is that Forge must merge into `meta` rather than replace it, since a
user may have their own keys there.
`TestExtras_CanCarryLayoutBesideSomeoneElsesMeta` proves that works. The
accessor itself belongs in Story 4, where the canvas's needs are known.

### What the review caught

Three defects that falsified the story's central claim, all of them invisible to
a corpus made of the files this repo happens to have.

**Root-level `on`, `entry`, `exit` and `after` were still being deleted — and
this change is what guaranteed it.** `rawMachine` declared all four with `json`
tags and *nothing ever read them*: the machine root is a state node in XState,
this package does not implement root-level transitions, and those fields existed
only to be swallowed by the decoder. Harmless dead weight until unknown fields
started being preserved, at which point `knownKeys` turned them into a deletion
allowlist — excluded from `Extra` "by construction", exactly like `invoke`, but
unlike `invoke` not rejected, just silently dropped. A machine with a global
`"on": {"RESET": "idle"}` lost it on save. They are gone from `rawMachine` now,
so they reach `Extra` and survive; the engine still does not act on them, and
preserving what the engine ignores beats deleting it.

**Unknown fields inside transitions, actions and guards were dropped.** Extras
were captured at exactly two levels. XState v4 puts `description`, `id`, `meta`
and `internal` on a transition and `description` on an action — and `internal`
is not decoration: it decides whether entry and exit actions re-fire on a
self-transition, so dropping it changes what the machine does. `Transition`,
`ActionSpec` and `CondSpec` now carry the same `Extra`/`ExtraOrder` pair, written
inline by `inlineExtras`.

**`writeExtras` silently dropped any extra not listed in `ExtraOrder`** — which
is precisely the path Story 4 takes. Writing canvas layout into a state whose
file had no `meta` means adding a key nothing recorded an order for, and it went
nowhere, layout and all. It now uses `jsonorder.Apply`, like every other
order-preserving site in the file; that also made the duplicate-key guard in
`extraFields` redundant on the emit side, so it is now tested where it still
acts — keeping the exported `ExtraOrder` clean.

Three smaller ones taken in the same pass: the emitter would happily write a
second `initial` key if one were put into `Extra` (the derived key set guarded
the parse side only); `actionObject` and `condObject` checked `len(Params) == 0`
where `specObject` deliberately checks `== nil`, so an action edited to have an
empty-but-present params map collapsed back to a bare string and lost it; and
`shiftIndent` gave up entirely on a CRLF file, because a `\r` on a blank line
measures as depth zero and collapsed the computed base.

### What the tests caught

Thirty-one deliberate defects; thirty fail the suite and one is provably
equivalent. Six survived the first run and all six were real gaps:

- **Three spelling guards had no test at all**, because a round trip cannot
  reach them: nothing that parses produces a bare transition carrying a guard,
  an unwrapped action list holding two actions, or a bare action with params.
  They needed the editing path, which is what `TestForms_ExpandWhenTheValueOutgrowsThem`
  drives.
- **Case-insensitive key matching was untested** until a fixture used a spelling
  that collides with a modelled field. Without it, `"Initial"` would have been
  emitted twice.
- **Indentation shifting was untested** because every fixture was already
  two-space, so shifting and splicing verbatim gave the same answer. A
  four-space file separates them.
- **R17** — trimming the first line in `shiftIndent` — is an *equivalent*
  mutation, not a gap: a `json.RawMessage` decoded from an object never starts
  with whitespace, which was checked rather than assumed.

### One fix introduced its own bug, and the test for it caught that too

`knownKeys` moved to `reflect.VisibleFields` so that untagged fields,
`json:",omitempty"` tags and embedded structs would all be counted — the review
showed the original tag-only version missed all three, which would have meant a
field parsed *and* captured, and emitted twice.

`VisibleFields` also reports **unexported** fields, which `encoding/json` never
binds. Claiming them would mean the opposite failure: a future unexported field
named `notes` would make a real `"notes"` key in someone's file count as
modelled, and it would be deleted on save. `TestKnownKeys_CoversEveryFieldEncodingJSONBinds`
failed on exactly that the first time it ran — it establishes what
`encoding/json` binds by round-tripping a probe struct rather than by reading
the documentation, so it is checking the real rule rather than my memory of it.

### The harness bug, again

One "SURVIVED" was the harness lying. `str.replace` with a pattern that does not
match returns the string unchanged and raises nothing, so a mutation that was
never applied is indistinguishable from one that survived — the pattern in
question had been written without the comment that sits inside the block.

The script now hashes the files before and after patching and reports `NO-OP`
when nothing changed. That is the third time this project's mutation harness has
produced a confident wrong answer: aliased `cp`, unquoted `$FILES` under zsh,
and now a silent no-op patch. Each was found by suspecting a result rather than
by the harness saying anything.

### Left for later

- **Root-level `on`/`entry`/`exit`/`after` are preserved but not executed.** The
  engine has never implemented root-level transitions; before this story it
  deleted them, and now it keeps them. Neither is the same as running them, and
  a machine relying on a global `RESET` still will not get one. Worth either
  implementing or reporting as an unsupported field — but not silently dropping,
  which is what it used to do.
- **Full authored field order** would make a Stately file round-trip *byte*-
  identically rather than nearly. It needs `writeStateNode` to emit known fields
  in the authored order with the fixed order as a fallback, which is a bigger
  change than this story's goal justified. Worth doing if Story 2 finds the
  dirty-on-open problem actually bites.
- **Context number formatting** would need raw bytes kept beside the decoded
  values, and a rule for when an edit invalidates them. It belongs with whatever
  story lets you edit a context value.
