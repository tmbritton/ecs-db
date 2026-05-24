# Story 2: Schema Loader & Validator — Implementation Plan

**Epic:** 1 — Schema-driven data foundation  
**Status:** ✅ Implemented  
**Priority:** Critical

## Goal

Extend Story 1's baseline loader/validator with comprehensive validation that rejects malformed input before DDL generation runs. Covers duplicate key detection, SQL compatibility checking, phased validation, and improved error reporting.

## Tasks & Status

| Phase | Task | Tests | File(s) |
|-------|------|-------|---------|
| **1** | `detectDuplicateKeys` — Token-stream duplicate detection for `components` and `entityTypes` | ✅ | `validate.go`, `load_validate_test.go` |
| **2** | `validateSQLCompatibility` — Reject component types with no SQL column generator | ✅ | `validate.go`, `load_validate_test.go` |
| **3** | Refactor `ValidateSchema` into three phases: `validateStructure`, `validateCrossReference`, `validateSQLCompatibility` | ✅ | `validate.go` |
| **4** | File path context in `InitSchema` errors | ✅ | `validate.go` |
| **5** | `allowExtraComponents` semantics: document that it's a schema-only guard, not enforced at DB level | ✅ | `types.go` (comment) |
| **6** | Comprehensive test coverage for all new paths | ✅ | `load_validate_test.go` |

## Design Decisions

- **Duplicate key detection:** Uses `json.NewDecoder` with `Token()` streaming to walk the top-level `components` and `entityTypes` objects. Go's `json.Unmarshal` silently takes the last value for duplicate keys, so we pre-validate with raw tokens before unmarshalling.
- **SQL compatibility validation:** Uses a whitelist of known component types (already declared in `component.go`). This is a schema-layer concern — the storage layer's `componentTableSQL` must handle every component type, so rejecting unsupported types early gives clearer error messages.
- **Phased validation:** Three distinct internal validation functions called sequentially by `ValidateSchema`, short-circuiting on first error. Enables independent testing and targeted error messages.
- **`allowExtraComponents`:** Purely a schema-level guard. The DB does not enforce it because every component has its own table keyed by `entity_id`. Document this in the code.

## Architecture Doc Cross-Reference

The arch doc's schema.json format is fully validated by the combination of `LoadSchema` (structural) + `ValidateSchema` (semantic):

| Requirement | Validation | Phase |
|---|---|---|
| `schemaVersion` ≥ 1 integer | `LoadSchema` | structure |
| `components` non-empty | `validateStructure` | structure |
| `entityTypes` non-empty | `validateStructure` | structure |
| Component `type` field unknown | `Component.UnmarshalJSON` | structure |
| Object component has no properties | `Component.UnmarshalJSON` | structure |
| Array component has no items | `Component.UnmarshalJSON` | structure |
| Duplicate component/entityType keys | `detectDuplicateKeys` | structure |
| Entity type references undeclared component | `validateCrossReference` | cross-reference |
| Required ∩ optional ≠ ∅ | `validateCrossReference` | cross-reference |
| `validationLevel` invalid | `validateCrossReference` | cross-reference |
| Component type has no SQL mapping | `validateSQLCompatibility` | SQL compatibility |

## Acceptance Criteria

- [x] A `schema.json` with duplicate component names is rejected with an error identifying the duplicate name.
- [x] A `schema.json` with duplicate entity type names is rejected with an error identifying the duplicate name.
- [x] Malformed JSON is rejected with an error that includes the file path.
- [x] An entity type referencing an undeclared component is rejected with an error naming the entity type and missing component.
- [x] A component using a type with no SQL column generator is rejected with an error naming the component and unsupported type.
- [x] A `schemaVersion` of `0` or negative is rejected with a clear error message.
- [x] A `validationLevel` other than `"strict"` or `"warning"` is rejected.
- [x] An entity type with the same component in both `requiredComponents` and `optionalComponents` is rejected.
- [x] `schema_test.go` has at least one test case per validation rule above, all passing.
- [x] `InitSchema("./schema.json")` on a valid `schema.json` returns no error and a fully populated `DatabaseSchema`.
