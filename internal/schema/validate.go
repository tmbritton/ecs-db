package schema

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadSchema parses schema.json bytes into a DatabaseSchema.
// It performs structural unmarshalling (type dispatch for components,
// default application for entity types) but does NOT run semantic
// validation. Call ValidateSchema on the result.
func LoadSchema(jsonData []byte) (DatabaseSchema, error) {
	var raw struct {
		SchemaVersion json.RawMessage     `json:"schemaVersion"`
		Components    map[string]Component `json:"components"`
		EntityTypes   map[string]EntityType `json:"entityTypes"`
	}
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		return DatabaseSchema{}, fmt.Errorf("failed to parse schema.json: %w", err)
	}

	// ── schemaVersion: must be a JSON integer ──
	var version float64
	if err := json.Unmarshal(raw.SchemaVersion, &version); err != nil {
		return DatabaseSchema{}, fmt.Errorf("schemaVersion: expected integer, got %s", string(raw.SchemaVersion))
	}
	if version != float64(int(version)) {
		return DatabaseSchema{}, fmt.Errorf("schemaVersion: expected integer, got %g", version)
	}
	if int(version) < 1 {
		return DatabaseSchema{}, fmt.Errorf("schemaVersion: must be >= 1, got %d", int(version))
	}

	// ── Entity type defaults ──
	for i, et := range raw.EntityTypes {
		et.ApplyDefaults()
		raw.EntityTypes[i] = et
	}

	return DatabaseSchema{
		SchemaVersion: int(version),
		Components:    raw.Components,
		EntityTypes:   raw.EntityTypes,
	}, nil
}

// ValidateSchema performs semantic validation on a loaded schema.
func ValidateSchema(s DatabaseSchema) error {
	// Redundant with LoadSchema's integer check — kept as a safety net
	// for callers that construct DatabaseSchema directly without LoadSchema.
	if s.SchemaVersion < 1 {
		return fmt.Errorf("schemaVersion: must be >= 1, got %d", s.SchemaVersion)
	}

	if len(s.Components) == 0 {
		return fmt.Errorf("components: at least one component must be declared")
	}

	if len(s.EntityTypes) == 0 {
		return fmt.Errorf("entityTypes: at least one entity type must be declared")
	}

	// Component name → existence already guaranteed by the map.
	// Entity type component references must resolve.
	for typeName, et := range s.EntityTypes {
		allComponents := append(et.RequiredComponents, et.OptionalComponents...)
		for _, compName := range allComponents {
			if _, ok := s.Components[compName]; !ok {
				return fmt.Errorf("entityType %q references undeclared component %q",
					typeName, compName)
			}
		}

		// No overlap between required and optional.
		for _, req := range et.RequiredComponents {
			for _, opt := range et.OptionalComponents {
				if req == opt {
					return fmt.Errorf("entityType %q: component %q is in both requiredComponents and optionalComponents",
						typeName, req)
				}
			}
		}

		// validationLevel must be valid.
		switch et.ValidationLevel {
		case ValidationStrict, ValidationWarning:
			// ok
		default:
			return fmt.Errorf("entityType %q: invalid validationLevel %q (must be %q or %q)",
				typeName, et.ValidationLevel, ValidationStrict, ValidationWarning)
		}
	}

	return nil
}

// InitSchema reads schema.json from disk, unmarshals, and validates it.
func InitSchema(path string) (DatabaseSchema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DatabaseSchema{}, fmt.Errorf("reading schema file: %w", err)
	}
	s, err := LoadSchema(data)
	if err != nil {
		return DatabaseSchema{}, err
	}
	if err := ValidateSchema(s); err != nil {
		return DatabaseSchema{}, err
	}
	return s, nil
}
