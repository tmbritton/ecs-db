# Story 1: Database Introspection

**Epic:** 2 — Schema versioning & auto-migrations  
**Status:** ✅ Done  
**Priority:** Critical blocker — introspection is the foundation for everything auto-migrate needs

## Context

Epic 1's `NewSQLiteStore` detects version mismatches via `checkSchemaVersion` in `meta`, but has no idea what tables, columns, or indexes actually exist in the database. To auto-migrate from `schema.json` changes, we need to **reconstruct the current database schema from the live SQLite database** — effectively reverse-engineering what `bootstrapDatabase` did.

This is storage-layer code. The result is a representation of what physically exists in the database (component tables, columns, types) that we can compare against the `schema.json` via schema diff (Story 2).

**Key constraint:** Entity types are stored only as `TEXT` in the `entities` table. We cannot introspect `requiredComponents`, `optionalComponents`, or `validationLevel` from the database — those are always "ground truth from schema.json". Introspection can only recover component structure (which have physical tables). Entity type changes are purely in-memory validation rules with no DDL impact.

## Acceptance Criteria

- [ ] Function to list all `comp_*` tables in the database
- [ ] Introspection produces a representation of each component's columns and SQL types
- [ ] Round-trip fidelity: a component → DDL → introspect produces an equivalent representation
- [ ] All component types are correctly recovered: object (multi-column), string, integer, number, boolean, entity-ref, array
- [ ] Full-schema introspection returns all components plus the stored schema_version from `meta`
- [ ] Fixed tables (`meta`, `world`, `entities`, `event_queue`, `input_events`, `transitions`) are not returned as components
- [ ] 100% test coverage on the introspection code

## Notes

- Component table naming is `comp_<lowercase(name)>` with one table per declared component
- The mapping from semantic types to SQL types is one-way (lossy). `boolean` maps to `INTEGER`, `number` maps to `REAL`, `entity-ref` maps to `INTEGER`. The diff layer handles this by comparing SQL types directly.
- Object components with no properties produce a table with only `entity_id` — still a valid, introspectable table.
