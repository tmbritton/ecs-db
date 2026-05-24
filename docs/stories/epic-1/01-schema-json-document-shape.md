# Story 1: Define & Lock schema.json Document Shape

**Epic:** 1 — Schema-driven data foundation  
**Status:** ⚠️ Partially done — format diverges from architecture doc  
**Priority:** Critical blocker — nothing else builds correctly until this is locked

## Context

The architecture doc (`docs/game-engine-arch.md`) defines a fully specified `schema.json` format. The current `schema.json` and `internal/schema/types.go` implement a different, simpler shape. These must be reconciled before the loader, validator, DDL generator, and entity creation logic can be finalized.

## Current State vs Architecture

| Aspect | Current (`schema.json`) | Architecture Doc |
|--------|------------------------|------------------|
| Version key | `"version"` (string) | `"schemaVersion"` (integer) |
| Components location | `"schema" → "components"` | Top-level `"components"` |
| Entity types location | `"schema" → "entities"` | Top-level `"entityTypes"` |
| Entity definition | `"components": [...]` (flat list) | `requiredComponents`, `optionalComponents`, `allowExtraComponents`, `validationLevel` |
| Component property type | Polymorphic Go types via `ComponentMap` | Type-driven properties: `object` (with nested `properties`), `array` (with `items`), `entity-ref`, plus primitives (`number`, `integer`, `string`, etc.) |
| Supported component types | `text`, `integer`, `reference`, `datetime`, `url`, `email`, `boolean` | `object` (with nested typed properties), `array` (of entity-refs), `entity-ref` (shorthand), `string`, `integer`, `number` |

**Note:** The current design uses one table per component with a single `value` column (plus constraint metadata like `min`/`max`). The architecture doc uses one table per component with **multiple typed columns** matching the component's `properties` (e.g., `comp_position` has `x REAL`, `y REAL`). This fundamentally changes the DDL generation story.

## Tasks

- [ ] **Decide the component shape** — single `value` column (current) vs. multi-column per component property (architecture doc). The architecture doc's approach is more expressive and aligns with SQL normalization, but requires more complex DDL generation. Record the decision in this story.
- [ ] **Lock the top-level JSON shape** — `schemaVersion` (int), `components` (object keyed by name), `entityTypes` (object keyed by name). Update `internal/schema/types.go` to match.
- [ ] **Define entity type fields** — `requiredComponents` (list), `optionalComponents` (list), `allowExtraComponents` (bool, default `false`), `validationLevel` (`"strict"` or `"warning"`, default `"strict"`).
- [ ] **Define the component property system** — at minimum: `object` (with `properties` map supporting `string`, `integer`, `number`, `entity-ref`, `boolean`), `array` (with `items` specifying the item type), and `entity-ref` shorthand. Update `internal/schema/types.go` accordingly.
- [ ] **Write a JSON Schema (or Go validation equivalent)** for `schema.json` itself — self-validate the document shape before running semantic validations.
- [ ] **Update `schema.json`** to the locked format using one concrete example entity type from the architecture doc (e.g., `Goblin` or `Player`).
- [ ] **Update all existing tests** to use the new format.

## Acceptance Criteria

- [ ] `schema.json` at the repo root matches the locked document shape exactly, with a `schemaVersion` integer field and at least two entity types and three components declared.
- [ ] `internal/schema/types.go` exports Go structs that exactly round-trip the locked `schema.json` shape.
- [ ] The Go structs use `schemaVersion int` (not string "version") and the correct top-level key names.
- [ ] Entity type struct includes `RequiredComponents`, `OptionalComponents`, `AllowExtraComponents`, and `ValidationLevel` fields with appropriate defaults.
- [ ] Component struct supports at least `object` (with nested typed properties) and `array` types in addition to primitives.
- [ ] A `LoadSchema` call on a structurally-invalid `schema.json` (e.g., wrong top-level keys, missing `schemaVersion`) returns a descriptive error naming the structural issue.
- [ ] All Go tests pass after the format change.
- [ ] The architecture doc's example `schema.json` can be loaded by the new types.
