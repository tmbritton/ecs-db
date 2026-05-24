# Story 1: Define & Lock schema.json Document Shape

**Epic:** 1 — Schema-driven data foundation  
**Status:** ✅ Done  
**Priority:** Critical blocker — nothing else builds correctly until this is locked

## Context

The architecture doc (`docs/game-engine-arch.md`) defines a fully specified `schema.json` format. The current `schema.json` and `internal/schema/types.go` implement a different, simpler shape. These must be reconciled before the loader, validator, DDL generator, and entity creation logic can be finalized.

## Decision

Aligned to the architecture document (Option A). Components are multi-column (`object` type with typed `properties`), entity types have `requiredComponents`, `optionalComponents`, `allowExtraComponents`, and `validationLevel`. The `schemaVersion` is an integer for migration comparison.

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

- [x] **Decide the component shape** — multi-column per architecture doc.
- [x] **Lock the top-level JSON shape** — Done. `DatabaseSchema{SchemaVersion int, Components, EntityTypes}`.
- [x] **Define entity type fields** — `EntityType{RequiredComponents, OptionalComponents, AllowExtraComponents, ValidationLevel}` with `ApplyDefaults()`.
- [x] **Define the component property system** — `Property{Type, Properties, Items}` with recursive `Validate()`. New file: `internal/schema/property.go`.
- [x] **Write Go validation** — `LoadSchema` + `ValidateSchema` enforce structural and semantic rules.
- [x] **Update `schema.json`** — Goblin + Player with Position, Health, Sprite.
- [x] **Update all existing tests** — old `schema_test.go` replaced with comprehensive coverage (67 tests, 98.2%).

## Acceptance Criteria

- [x] `schema.json` at the repo root matches the locked document shape exactly, with a `schemaVersion` integer field and at least two entity types and three components declared.
- [x] `internal/schema/types.go` exports Go structs that exactly round-trip the locked `schema.json` shape.
- [x] The Go structs use `schemaVersion int` (not string "version") and the correct top-level key names.
- [x] Entity type struct includes `RequiredComponents`, `OptionalComponents`, `AllowExtraComponents`, and `ValidationLevel` fields with appropriate defaults.
- [x] Component struct supports at least `object` (with nested typed properties) and `array` types in addition to primitives.
- [x] A `LoadSchema` call on a structurally-invalid `schema.json` (e.g., wrong top-level keys, missing `schemaVersion`) returns a descriptive error naming the structural issue.
- [x] All Go tests pass after the format change.
- [x] The architecture doc's example `schema.json` can be loaded by the new types.

## Files Changed

| File | Change |
|------|--------|
| `internal/schema/types.go` | Rewritten: `DatabaseSchema`, `EntityType`, `ValidationLevel` with helper methods |
| `internal/schema/property.go` | **New**: `Property` type system with recursive validation |
| `internal/schema/component.go` | Rewritten: polymorphic `Component` with `UnmarshalJSON` dispatch |
| `internal/schema/entity.go` | Deleted — moved into `types.go` |
| `internal/schema/validate.go` | Rewritten: `LoadSchema`, `ValidateSchema`, `InitSchema` |
| `internal/schema/schema_test.go` | Deleted — replaced by new test files |
| `internal/schema/property_test.go` | **New**: property type tests |
| `internal/schema/component_test.go` | **New**: component unmarshalling tests |
| `internal/schema/entitytype_test.go` | **New**: EntityType helper method tests |
| `internal/schema/load_validate_test.go` | **New**: loader + validator integration tests |
| `schema.json` | Migrated to locked format (Goblin + Player) |
| `internal/schema/testdata/*` | Updated to new format |
