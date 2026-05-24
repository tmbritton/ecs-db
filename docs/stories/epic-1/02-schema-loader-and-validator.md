# Story 2: schema.json Loader & Validator

**Epic:** 1 — Schema-driven data foundation  
**Status:** ⚠️ Partially done — basic loader exists; needs comprehensive validation  
**Priority:** Critical — must reject malformed input before DDL generation runs

## Context

`internal/schema/validate.go` already provides `InitSchema`, `LoadSchema`, `ValidateSchema`, `ValidateEntities`, and `ValidateReferenceComponents`. `schema_test.go` covers basic cases. However, the current validator misses several checks required by the architecture doc and the plan.

## Current Implementation

| Check | Status |
|-------|--------|
| Reject malformed JSON | **Done** — `json.Unmarshal` errors propagated, but no line/column info |
| Component type must be known | **Done** — `ComponentMap` lookup fails for unknown types |
| Entity references exist in `components` | **Done** — `ValidateEntities` |
| Reference components point to valid entity types | **Done** — `ValidateReferenceComponents` |
| Required fields present (`version`, `components`, `entities`) | **Done** — `ValidateSchema` basic checks |
| **Duplicate component names** | ❌ Not checked (JSON unmarshal doesn't catch duplicates) |
| **Duplicate entity type names** | ❌ Not checked |
| **Component types supported by SQL generator** | ❌ No validation that declared types can be turned into SQL |
| **Line/column in JSON parse errors** | ❌ Not reported |
| **Cross-validation of entity type constraints** | ❌ Not implemented (entity type fields don't exist yet in current format) |

## Tasks

- [ ] **Improve JSON error reporting** — When `json.Unmarshal` fails, wrap the error to include file path. Use `json.Decoder` with `UseNumber()` to ensure numeric precision for `schemaVersion` (integer).
- [ ] **Add duplicate detection** — Detect and reject when `schema.json` contains duplicate keys for components or entity types. (JSON unmarshal silently takes the last value; use `json.Decoder` with raw token parsing or a two-pass approach.)
- [ ] **Validate component types are SQL-generatable** — After the document shape is locked (Story 1), add a check that every declared component type has a corresponding SQL column generator. Unknown types should fail with a clear message: "component X uses type 'Y' which has no SQL mapping."
- [ ] **Validate entity type constraints** — For each entity type: all components in `requiredComponents` and `optionalComponents` must reference a declared component. No duplicates across the two lists. If `allowExtraComponents: false`, the union of required + optional is the allowed set.
- [ ] **Validate `validationLevel` values** — Only `"strict"` and `"warning"` are accepted; anything else is an error.
- [ ] **Validate `schemaVersion` is a positive integer** — Reject `0`, negative numbers, or non-integer values.
- [ ] **Refactor validation into distinct phases** — (1) structural (JSON shape), (2) cross-reference (components ↔ entity types), (3) SQL compatibility (types → columns). Each phase returns its own error messages, enabling targeted fixes.
- [ ] **Update test coverage** to exercise all new validation paths, including edge cases:
  - Duplicate component/entity names
  - Entity type referencing unknown component
  - Entity type with overlapping `requiredComponents` and `optionalComponents`
  - Invalid `validationLevel`
  - `schemaVersion` as `0`, `-1`, or non-integer (string/float)
  - Component using a type with no SQL mapping

## Acceptance Criteria

- [ ] A `schema.json` with duplicate component names is rejected with an error message identifying the duplicate name.
- [ ] A `schema.json` with duplicate entity type names is rejected with an error message identifying the duplicate name.
- [ ] Malformed JSON is rejected with an error that includes the file path and the `json.ParseError` line/column (if available from the decoder).
- [ ] An entity type referencing a component not declared in `components` is rejected with an error naming the entity type and missing component.
- [ ] A component using a type with no SQL column generator is rejected with an error naming the component and unsupported type.
- [ ] A `schemaVersion` of `0` or negative is rejected with a clear error message.
- [ ] A `validationLevel` value other than `"strict"` or `"warning"` is rejected.
- [ ] An entity type with the same component in both `requiredComponents` and `optionalComponents` is rejected.
- [ ] `schema_test.go` has at least one test case per validation rule above, all passing.
- [ ] `InitSchema("./schema.json")` on a valid `schema.json` returns no error and a fully populated `DatabaseSchema`.
