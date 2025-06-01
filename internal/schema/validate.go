package schema

import (
	"encoding/json"
	"fmt"
	"os"
)

// Temporary struct for initial parsing
type tempSchema struct {
	Version string `json:"version"`
	Schema  struct {
		Components map[string]json.RawMessage `json:"components"`
		Entities   map[string]Entity          `json:"entities"`
	} `json:"schema"`
}

func LoadSchema(jsonData []byte) (DatabaseSchema, error) {
	var schema DatabaseSchema
	var temp tempSchema

	if err := json.Unmarshal(jsonData, &temp); err != nil {
		return DatabaseSchema{}, err
	}

	schema.Schema.Components = make(map[string]Component)

	for key, val := range temp.Schema.Components {
		var typeInfo struct {
			Type string `json:"type"`
		}

		if err := json.Unmarshal(val, &typeInfo); err != nil {
			return DatabaseSchema{}, err
		}
		componentFactory, ok := ComponentMap[typeInfo.Type]
		if ok {
			component := componentFactory()

			if err := json.Unmarshal(val, component); err != nil {
				return DatabaseSchema{}, err
			}
			schema.Schema.Components[key] = component
		} else {
			return DatabaseSchema{}, fmt.Errorf("%s is not a known component type", typeInfo.Type)
		}
	}

	schema.Version = temp.Version

	schema.Schema.Entities = temp.Schema.Entities

	return schema, nil
}

// Ensure all components referenced by entities actually exist
func ValidateEntities(schema DatabaseSchema) error {
	for entityName, entity := range schema.Schema.Entities {
		for _, componentName := range entity.Components {
			_, exists := schema.Schema.Components[componentName]
			if !exists {
				return fmt.Errorf("entity %s references non-existent component %s", entityName, componentName)
			}
		}
	}
	return nil
}

// Ensure all reference components point to valid entity types
func ValidateReferenceComponents(schema DatabaseSchema) error {
	for _, component := range schema.Schema.Components {
		if refComponent, ok := component.(*ReferenceComponent); ok {
			entityType := refComponent.EntityType
			_, exists := schema.Schema.Entities[entityType]
			if !exists {
				// return false
				return fmt.Errorf("entityType %s refers to a non-existent entity type", entityType)
			}
		}
	}
	return nil
}

func ValidateSchema(schema DatabaseSchema) error {
	if schema.Version == "" {
		return fmt.Errorf("version field is required")
	}

	if schema.Schema.Components == nil || len(schema.Schema.Components) == 0 {
		return fmt.Errorf("components field is required")
	}

	if schema.Schema.Entities == nil || len(schema.Schema.Entities) == 0 {
		return fmt.Errorf("entities field is required")
	}

	if err := ValidateEntities(schema); err != nil {
		return err
	}

	if err := ValidateReferenceComponents(schema); err != nil {
		return err
	}

	return nil
}

func InitSchema(path string) (DatabaseSchema, error) {
	jsonData, err := os.ReadFile(path)
	if err != nil {
		return DatabaseSchema{}, fmt.Errorf("Error reading file at %s", path)
	}
	schema, err := LoadSchema(jsonData)
	if err != nil {
		return DatabaseSchema{}, err
	}
	if err := ValidateSchema(schema); err != nil {
		return DatabaseSchema{}, err
	}
	return schema, nil
}
