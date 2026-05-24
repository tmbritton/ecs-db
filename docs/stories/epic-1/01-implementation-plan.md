# Story 1 Implementation Plan: Align schema.json to Architecture Doc

**Decision:** Option A — rewrite the implementation to match the architecture document.
**Rationale:** The current implementation is a generic document validator. The architecture describes a game engine data model with ECS semantics (entity types, typed component properties, multi-column tables). Downstream stories (2–6) all assume the architecture's shapes, so alignment now unblocks everything cleanly.
**User note:** The `entities` table must have a `type` / `entity_type` field (`TEXT NOT NULL`) even though it departs from pure ECS — readability of "this is a goblin" trumps purity. This is already in the architecture doc's SQL sketch.

---

## Step 1: Rewrite `internal/schema/types.go` — New root shape + entity type struct

Replace the current two-struct setup (`DatabaseSchema` with nested `SchemaDefinition`) with the architecture's flat, top-level JSON shape.

### New structs:

```go
type DatabaseSchema struct {
    SchemaVersion int                    `json:"schemaVersion"`
    Components    map[string]Component   `json:"components"`
    EntityTypes   map[string]EntityType  `json:"entityTypes"`
}

type EntityType struct {
    RequiredComponents   []string        `json:"requiredComponents"`
    OptionalComponents   []string        `json:"optionalComponents"`
    AllowExtraComponents bool            `json:"allowExtraComponents"`
    ValidationLevel      ValidationLevel `json:"validationLevel"`
}

type ValidationLevel string

const (
    ValidationStrict  ValidationLevel = "strict"
    ValidationWarning ValidationLevel = "warning"
)
```

### EntityType.ApplyDefaults()
- If `ValidationLevel` is empty → default to `"strict"`.

### Breaking changes from current code:
- `Version string` → `SchemaVersion int` (integer for migration comparison, not a semver string)
- `"schema" → "components"` → top-level `"components"`
- `"schema" → "entities"` → top-level `"entityTypes"` (renamed because these are *types*, not instances)

---

## Step 2: Create `internal/schema/property.go` — Component property system

The architecture defines components with a type-driven property system, not scalar values. This file isolates the nested property definitions from the component-level interface.

### New structs:

```go
type Property struct {
    Type       string              `json:"type"`
    // Used when Type == "object": keyed map of child properties
    Properties map[string]Property `json:"properties,omitempty"`
    // Used when Type == "array": the type of each array item
    Items      *Property           `json:"items,omitempty"`
}
```

### Supported property types:
- **Primitives:** `"string"`, `"integer"`, `"number"`, `"boolean"`
- **Composite:** `"object"` (with `properties` map of nested `Property`s)
- **Reference:** `"array"` (with `items` specifying item type), `"entity-ref"` (shorthand reference to an entity ID)

### Helper — IsSupportedPropertyType(t string) bool
- Returns true for the supported types above. Used by the validator to reject unknown property types in object components.

---

## Step 3: Rewrite `internal/schema/component.go` — Architecture-aligned components

Remove the current scalar-only approach (`TextComponent`, `IntegerComponent`, `ReferenceComponent`, `DatetimeComponent`, `UrlComponent`, `EmailComponent`, `BoolComponent`) and the `ComponentMap` factory pattern.

### New component interface — single struct with polymorphic unmarshalling:

```go
type Component struct {
    Type       string              `json:"type"`
    // Used when Type == "object": keyed map of nested Property definitions
    Properties map[string]Property `json:"properties,omitempty"`
    // Used when Type == "array": the Property type for array items
    Items      *Property           `json:"items,omitempty"`
}
```

Or equivalently, keep it as an interface (`type Component interface { GetProperties() map[string]Property; ... }`) with concrete implementations (`ObjectComponent`, `ArrayComponent`, `EntityRefComponent`, `PrimitiveComponent`) and a custom `UnmarshalJSON` on the interface that dispatches by `"type"`. The polymorphic dispatch via a factory map pattern from the current code can be preserved if preferred — just the types themselves change.

### Component types to support:
| Type | Go representation | Notes |
|------|-------------------|-------|
| `object` | component with `Properties` map | Generates a multi-column table; each property becomes a typed SQL column |
| `array` | component with `Items` property | Serialized as JSON in a single column (for entity-ref arrays) |
| `entity-ref` | shorthand component | Generates `{entity_id, target_entity_id}` — a foreign key column |
| `string` | primitive | Single typed column |
| `integer` | primitive | Single typed column |
| `number` | primitive (REAL) | Single typed column |
| `boolean` | primitive | Single typed column |

### Types to REMOVE:
- `datetime`, `url`, `email` — not in the architecture doc. Use `integer` (epoch), `string` with validation, or `object` with properties instead. `reference` → replaced by `entity-ref`.

### DDL generation consequence (not done in this story, but drives the design):
Under this model, `comp_position` with type `object` and properties `{x: number, y: number}` generates:
```sql
CREATE TABLE comp_position (
    entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
    x REAL NOT NULL,
    y REAL NOT NULL
);
```
Not the current single-column approach.

---

## Step 4: Rewrite `internal/schema/entity.go` — Entity type with validation fields

Replace the current flat `Entity{Components []string}` with the architecture's four-field entity type template.

### New struct (already defined in Step 1's `types.go`, just documenting here):

```go
type EntityType struct {
    RequiredComponents   []string        `json:"requiredComponents"`
    OptionalComponents   []string        `json:"optionalComponents"`
    AllowExtraComponents bool            `json:"allowExtraComponents"`
    ValidationLevel      ValidationLevel `json:"validationLevel"`
}
```

### Helper methods:
- `IsComponentRequired(name string) bool` — true if name is in `RequiredComponents`
- `IsComponentOptional(name string) bool` — true if name is in `OptionalComponents`
- `IsComponentAllowed(name string) bool` — true if required, optional, or `AllowExtraComponents == true`

These will be consumed by Story 4 (entity creation validation) and Story 5 (attach/detach validation).

---

## Step 5: Rewrite `internal/schema/validate.go` — Updated loader + validator

### LoadSchema changes:
- Parse `schemaVersion` as **integer** — reject if it's a string, zero, or negative.
- Parse flat top-level JSON shape: `schemaVersion`, `components`, `entityTypes` (no `"schema"` wrapper).
- Use the new polymorphic component type (custom `UnmarshalJSON` or factory map with the new types).
- Apply `EntityType.ApplyDefaults()` after unmarshalling.

### ValidateSchema checks:
| Check | Description |
|-------|-------------|
| `schemaVersion` is ≥ 1 | Integer, non-zero |
| `components` map exists & non-empty | At least one component declared |
| `entityTypes` map exists & non-empty | At least one entity type declared |
| Entity type component references resolve | Every name in `requiredComponents` and `optionalComponents` is a declared component |
| No overlap between required and optional | A component cannot be both required and optional |
| `validationLevel` values are valid | Only `"strict"` or `"warning"` |
| Object component property types are supported | Every nested property type in `object` components is in the supported set |
| `entity-ref` / `array` item type is valid | If component uses `entity-ref` or `array` of entity-refs, referenced entity types exist (if cross-referencing) |

### Keep from current code:
- `InitSchema(path string)` — reads file, calls LoadSchema + ValidateSchema, returns result

---

## Step 6: Update root `schema.json` — Migrate to locked format

Replace the current `schema.json` (blog-post-like example with `Post` and `Author` entities, scalar types) with a game-engine shape matching the architecture doc. Use at minimum two entity types and three components.

### Proposed shape (subset of the architecture example):

```json
{
  "schemaVersion": 1,
  "components": {
    "Position": {
      "type": "object",
      "properties": {
        "x": { "type": "number" },
        "y": { "type": "number" }
      }
    },
    "Health": {
      "type": "object",
      "properties": {
        "hp": { "type": "integer" },
        "maxHp": { "type": "integer" }
      }
    },
    "Sprite": {
      "type": "object",
      "properties": {
        "imageId": { "type": "string" },
        "frame": { "type": "integer" }
      }
    }
  },
  "entityTypes": {
    "Goblin": {
      "requiredComponents": ["Position", "Health", "Sprite"],
      "optionalComponents": [],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    },
    "Player": {
      "requiredComponents": ["Position", "Health", "Sprite"],
      "optionalComponents": [],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    }
  }
}
```

We can expand to `Behavior`, `Velocity`, `Inventory` etc. in later stories when those systems come online.

---

## Step 7: Rewrite `internal/schema/schema_test.go` — Full test coverage

### Test categories:

**Load / JSON shape tests:**
- Valid schema (the new `schema.json`) round-trips correctly
- `schemaVersion` as string → error
- `schemaVersion` as zero or negative → error
- Missing `schemaVersion` → error
- Wrong top-level keys (old `"version"` / `"schema"` wrapper) → error
- Invalid JSON → error with useful message

**Entity type validation tests:**
- Entity type references undeclared component → error, names the component
- Component in both `requiredComponents` and `optionalComponents` → error
- Invalid `validationLevel` ("loose", "error", etc.) → error
- Empty `requiredComponents` with `AllowExtraComponents=false` → valid (empty type with no required components is allowed, but nothing can be attached to it)

**Component property tests:**
- Object component with unsupported nested property type → error
- Nested object properties validate recursively
- Array component without `items` → error (or valid? decide)

**Full integration:**
- `InitSchema` on the root `schema.json` succeeds
- The architecture doc's full example `schema.json` (all 7 components, 4 entity types) loads successfully

**Cleanup:**
- Delete `testdata/valid_schema.json` and `testdata/malformed.json` — recreate with new format if `InitSchema` file-based tests are kept

---

## Step 8: Build & verify

```
go build ./...
go test ./internal/schema/ -v
```

Fix any import breaks (e.g., code outside `internal/schema` that references `DatabaseSchema.Schema`, `DatabaseSchema.Version`, `Entity`, `SchemaDefinition`). At time of planning, no such references exist outside `internal/schema`.

---

## File change summary

| File | Action | Reason |
|------|--------|--------|
| `internal/schema/types.go` | Rewrite | New root shape, `EntityType`, `ValidationLevel` |
| `internal/schema/property.go` | **New** | Property type system for `object`/`array` components |
| `internal/schema/component.go` | Rewrite | New component types, remove scalar-only types |
| `internal/schema/entity.go` | Delete or empty | Content moved into `types.go` as `EntityType` |
| `internal/schema/validate.go` | Rewrite | New loader, new validation rules |
| `internal/schema/schema_test.go` | Rewrite | Tests for new shapes and rules |
| `schema.json` | Rewrite | Migrate to locked format |
| `internal/schema/testdata/*` | Recreate | New test fixtures in new format |
| `docs/stories/epic-1/01-schema-json-document-shape.md` | Mark tasks done | Update status when complete |

---

## Dependencies

- None of steps 1–5 depend on each other's output within this epic, but they must be done in roughly this order since each builds on the types defined earlier.
- Steps 1–2 define types → Step 3 uses them → Step 5 wires them up → Step 4 is self-contained helper → Step 6 validates against Step 5 → Step 7 tests everything.
