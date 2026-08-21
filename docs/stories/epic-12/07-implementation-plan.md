# Epic 12 Story 7: Inline validation — Implementation Plan

**Goal:** say what is wrong while it is being typed, using the engine's own checks.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/validation/validation.go` | Collect errors and warnings |
| Create | `internal/forge/validation/validation_test.go` | |
| Create | `internal/forge/templates/components/fieldproblem.templ` | Inline message |

---

## Task 1: Collect, do not reimplement

```go
type Problem struct {
	Field    string // what it attaches to, for aria-describedby
	Message  string
	Blocking bool   // an error stops a save; a warning does not
}

func Check(s schema.DatabaseSchema, behaviorsDir string) []Problem
```

Sources, in order:

1. `schema.ValidateSchema(s)` — structure, reserved names, cross-references. **Blocking.**
2. `schema.ValidateBehaviorRefs(s, behaviorsDir)` — a `behavior` naming a machine file that does not exist. **Blocking.** This gives it its first caller in the codebase.
3. The ambiguity check below. **Warning.**

No rule is restated here. If Forge and the engine disagree about validity, the
engine wins and Forge was wrong to say otherwise.

## Task 2: The ambiguity warning

`ValidateMachine` errors when a context key matches two components' fields,
because it cannot choose which to seed from. That surfaces at machine-load
time — far from the schema edit that caused it.

`buildFieldIndex` in `validator.go` already builds `field → []component`. It is
unexported; either export it or build the same two-line index here and say why.
Prefer exporting, so the check and the error it predicts share one
implementation.

A warning, not an error: the schema is legal. Blocking the save would be Forge
inventing a rule the engine does not have.

## Task 3: The short-circuit, stated

`ValidateSchema` returns the first phase's failures and stops
(`validate.go:234`). So the list is not necessarily complete, and the UI must
not imply it is — "first problems found" rather than a bare list.

Fixing the engine to collect all phases is real work with its own risk, and it
belongs in its own change rather than smuggled into a UI story. Note it in the
As Implemented for whoever picks it up.

## Task 4: Attachment

A problem renders next to what caused it and is associated with it:
`aria-describedby` on the input, `aria-invalid` when blocking. Positioning
alone is not association — that is the difference between a sighted user seeing
the message and everyone else getting it.

---

## Verification

Table tests per problem class, each asserting the field it attaches to and
whether it blocks.

The e2e spec's key assertions: an error disables save and a warning does not;
and fixing the problem clears the message without a reload.

Mutation-check the ambiguity warning specifically — it is the one rule here that
is not simply delegated, so it is the one that can be quietly wrong.
