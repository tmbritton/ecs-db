package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// knownSQLComponentTypes is the set of component types that the storage
// layer can generate SQL tables for. If a component uses a type outside
// this set, the schema is considered invalid.
// Keep in sync with storage.componentTableSQL's component type switch.
var knownSQLComponentTypes = map[string]bool{
	ComponentTypeObject:    true,
	ComponentTypeArray:     true,
	ComponentTypeEntityRef: true,
	ComponentTypeString:    true,
	ComponentTypeInteger:   true,
	ComponentTypeNumber:    true,
	ComponentTypeBoolean:   true,
}

// LoadSchema parses schema.json bytes into a DatabaseSchema.
// It performs structural unmarshalling (type dispatch for components,
// default application for entity types, duplicate key detection) but
// does NOT run semantic validation. Call ValidateSchema on the result.
func LoadSchema(jsonData []byte) (DatabaseSchema, error) {
	// ── Phase 0: detect duplicate keys in top-level maps ──
	if err := detectDuplicateKeys(jsonData); err != nil {
		return DatabaseSchema{}, err
	}

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

// detectDuplicateKeys uses a token stream to detect when the top-level
// "components" or "entityTypes" maps contain the same key more than once.
// Go's json.Unmarshal silently takes the last value for duplicate keys, so
// we must pre-validate with raw tokens before unmarshalling.
func detectDuplicateKeys(jsonData []byte) error {
	dec := json.NewDecoder(strings.NewReader(string(jsonData)))

	// Consume the opening brace of the top-level object.
	_, err := dec.Token()
	if err != nil {
		return fmt.Errorf("detectDuplicateKeys: %w", err)
	}

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("detectDuplicateKeys: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			continue
		}
		if key != "components" && key != "entityTypes" {
			// Not a map we care about — skip its value entirely.
			if err := skipValue(dec); err != nil {
				return fmt.Errorf("detectDuplicateKeys: %w", err)
			}
			continue
		}

		// Consume the opening brace of the nested object.
		delim, err := dec.Token()
		if err != nil {
			return fmt.Errorf("detectDuplicateKeys: %w", err)
		}
		if delim != json.Delim('{') {
			// Not an object — skip anyway.
			if err := skipValue(dec); err != nil {
				return fmt.Errorf("detectDuplicateKeys: %w", err)
			}
			continue
		}

		seen := make(map[string]bool)
		for dec.More() {
			innerKeyTok, err := dec.Token()
			if err != nil {
				return fmt.Errorf("detectDuplicateKeys: %w", err)
			}
			innerKey, ok := innerKeyTok.(string)
			if !ok {
				continue
			}
			if seen[innerKey] {
				return fmt.Errorf("duplicate %s key %q", key, innerKey)
			}
			seen[innerKey] = true
			// Skip the value (could be a complex nested object).
			if err := skipValue(dec); err != nil {
				return fmt.Errorf("detectDuplicateKeys: %w", err)
			}
		}

		// Consume closing brace.
		_, _ = dec.Token()
	}

	// Consume closing brace of top-level object.
	_, _ = dec.Token()
	return nil
}

// skipValue consumes one complete JSON value from the decoder, recursing
// into nested objects and arrays as needed. This is used after reading a
// key token to advance past its corresponding value.
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if delim, ok := tok.(json.Delim); ok {
		switch delim {
		case '{', '[':
			// We need to read until we find the matching closing delimiter.
			// dec.More() + dec.Token() handles this naturally — but we
			// already consumed the opening delimiter, so we just need to
			// consume key+value pairs (for objects) or values (for arrays)
			// until we hit the closing delimiter.
			return skipContainer(dec)
		}
	}
	// Primitive value — already consumed.
	return nil
}

// skipContainer consumes the rest of a JSON object or array after the
// opening delimiter has been read. It stops when it reads the matching
// closing delimiter.
func skipContainer(dec *json.Decoder) error {
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{', '[':
				if err := skipContainer(dec); err != nil {
					return err
				}
			}
		}
	}
	// Consume the closing delimiter.
	_, err := dec.Token()
	return err
}

// ValidateSchema performs semantic validation on a loaded schema.
// It runs three validation phases in order, short-circuiting on the
// first error encountered.
func ValidateSchema(s DatabaseSchema) error {
	if err := validateStructure(s); err != nil {
		return fmt.Errorf("structure: %w", err)
	}
	if err := validateCrossReference(s); err != nil {
		return fmt.Errorf("cross-reference: %w", err)
	}
	if err := validateSQLCompatibility(s); err != nil {
		return fmt.Errorf("SQL compatibility: %w", err)
	}
	return nil
}

// validateStructure checks required-top-level fields and their basic
// constraints (non-empty, positive version, etc.).
func validateStructure(s DatabaseSchema) error {
	if s.SchemaVersion < 1 {
		return fmt.Errorf("schemaVersion: must be >= 1, got %d", s.SchemaVersion)
	}
	if len(s.Components) == 0 {
		return fmt.Errorf("components: at least one component must be declared")
	}
	if len(s.EntityTypes) == 0 {
		return fmt.Errorf("entityTypes: at least one entity type must be declared")
	}
	return nil
}

// validateCrossReference checks that entity type references resolve to
// declared components, that required ∩ optional is empty, and that
// validationLevel values are valid.
func validateCrossReference(s DatabaseSchema) error {
	for typeName, et := range s.EntityTypes {
		allComponents := append(et.RequiredComponents, et.OptionalComponents...)
		for _, compName := range allComponents {
			if _, ok := s.Components[compName]; !ok {
				return fmt.Errorf("entityType %q references undeclared component %q",
					typeName, compName)
			}
		}

		for _, req := range et.RequiredComponents {
			for _, opt := range et.OptionalComponents {
				if req == opt {
					return fmt.Errorf("entityType %q: component %q is in both requiredComponents and optionalComponents",
						typeName, req)
				}
			}
		}

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

// validateSQLCompatibility checks that every component's type can be
// mapped to a SQL column by the storage layer. This catches cases where
// a component declares a type that has no corresponding CREATE TABLE
// generator.
func validateSQLCompatibility(s DatabaseSchema) error {
	for name, comp := range s.Components {
		if !knownSQLComponentTypes[comp.Type] {
			return fmt.Errorf("component %q uses type %q which has no SQL mapping",
				name, comp.Type)
		}
	}
	return nil
}

// InitSchema reads schema.json from disk, unmarshals, and validates it.
func InitSchema(path string) (DatabaseSchema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DatabaseSchema{}, fmt.Errorf("reading schema file %q: %w", path, err)
	}
	s, err := LoadSchema(data)
	if err != nil {
		return DatabaseSchema{}, fmt.Errorf("loading schema from %q: %w", path, err)
	}
	if err := ValidateSchema(s); err != nil {
		return DatabaseSchema{}, fmt.Errorf("validating schema from %q: %w", path, err)
	}
	return s, nil
}
