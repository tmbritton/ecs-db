package schema

// ValidationLevel controls how strictly entity-type rules are enforced.
type ValidationLevel string

const (
	ValidationStrict  ValidationLevel = "strict"
	ValidationWarning ValidationLevel = "warning"
)

// DatabaseSchema is the root representation of schema.json.
// The JSON shape is flat — no nested "schema" wrapper.
type DatabaseSchema struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Components    map[string]Component  `json:"components"`
	EntityTypes   map[string]EntityType `json:"entityTypes"`
}

// EntityType is a named template declaring which components an entity of
// that type must have, may have, and whether additional components are
// permitted after creation.
type EntityType struct {
	Behavior             string          `json:"behavior,omitempty"`
	RequiredComponents   []string        `json:"requiredComponents"`
	OptionalComponents   []string        `json:"optionalComponents"`
	AllowExtraComponents bool            `json:"allowExtraComponents"`
	ValidationLevel      ValidationLevel `json:"validationLevel"`
}

// ApplyDefaults populates fields whose defaults depend on omitted JSON keys.
func (et *EntityType) ApplyDefaults() {
	if et.ValidationLevel == "" {
		et.ValidationLevel = ValidationStrict
	}
}

// IsComponentRequired reports whether the named component must be present
// at entity creation time.
func (et *EntityType) IsComponentRequired(name string) bool {
	for _, c := range et.RequiredComponents {
		if c == name {
			return true
		}
	}
	return false
}

// IsComponentOptional reports whether the named component is permitted
// but not required at creation time.
func (et *EntityType) IsComponentOptional(name string) bool {
	for _, c := range et.OptionalComponents {
		if c == name {
			return true
		}
	}
	return false
}

// IsComponentAllowed reports whether the named component may be attached
// to an entity of this type (required, optional, or allowExtraComponents).
func (et *EntityType) IsComponentAllowed(name string) bool {
	return et.IsComponentRequired(name) ||
		et.IsComponentOptional(name) ||
		et.AllowExtraComponents
}
