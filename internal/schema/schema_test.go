package schema

import (
	"testing"
)

func TestValidateEntities(t *testing.T) {
	tests := []struct {
		name    string
		schema  DatabaseSchema
		wantErr bool
	}{
		{
			name: "valid schema",
			schema: DatabaseSchema{
				Schema: SchemaDefinition{
					Components: map[string]Component{
						"title": &TextComponent{BaseComponent: BaseComponent{Type: "text"}},
						"body":  &TextComponent{BaseComponent: BaseComponent{Type: "text"}},
					},
					Entities: map[string]Entity{
						"post": {Components: []string{"title", "body"}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "entity references missing component",
			schema: DatabaseSchema{
				Schema: SchemaDefinition{
					Components: map[string]Component{
						"title": &TextComponent{BaseComponent: BaseComponent{Type: "text"}},
						// "body" is missing!
					},
					Entities: map[string]Entity{
						"post": {Components: []string{"title", "body"}}, // "body" doesn't exist
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEntities(tt.schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEntities() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateEntityReferences(t *testing.T) {
	tests := []struct {
		name    string
		schema  DatabaseSchema
		wantErr bool
	}{
		{
			name: "valid schema",
			schema: DatabaseSchema{
				Schema: SchemaDefinition{
					Components: map[string]Component{
						"title":  &TextComponent{BaseComponent: BaseComponent{Type: "text"}},
						"body":   &TextComponent{BaseComponent: BaseComponent{Type: "text"}},
						"author": &ReferenceComponent{BaseComponent: BaseComponent{Type: "reference"}, EntityType: "user"},
					},
					Entities: map[string]Entity{
						"post": {Components: []string{"title", "body", "author"}},
						"user": {Components: []string{"title"}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "entity references missing entity",
			schema: DatabaseSchema{
				Schema: SchemaDefinition{
					Components: map[string]Component{
						"title":  &TextComponent{BaseComponent: BaseComponent{Type: "text"}},
						"body":   &TextComponent{BaseComponent: BaseComponent{Type: "text"}},
						"author": &ReferenceComponent{BaseComponent: BaseComponent{Type: "reference"}, EntityType: "user"},
					},
					Entities: map[string]Entity{
						"post": {Components: []string{"title", "body", "author"}},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateReferenceComponents(tt.schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateReferenceComponents() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadSchema(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name: "valid schema",
			json: `{
                "version": "1.0",
                "schema": {
                    "components": {
                        "title": {"type": "text"}
                    },
                    "entities": {
                        "post": {"components": ["title"]}
                    }
                }
            }`,
			wantErr: false,
		},
		{
			name:    "empty string",
			json:    ``,
			wantErr: true,
		},
		{
			name: "invalid json",
			json: `{
                "version": "1.0",
                "schema": {
                    "components": {
                        "title": {"type": "text"}
                    },
                    "entities": {
                        "post": {"components": ["title"]}
                    }
                }`, // Missing closing }
			wantErr: true,
		},
		{
			name: "unknown component type",
			json: `{
        "version": "1.0",
        "schema": {
            "components": {
                "title": {"type": "unknown"}
            },
            "entities": {
                "post": {"components": ["title"]}
            }
        }
    }`,
			wantErr: true,
		},
		// Add more test cases...
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadSchema([]byte(tt.json))
			// test logic here
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSchema(t *testing.T) {
	tests := []struct {
		name    string
		schema  DatabaseSchema
		wantErr bool
	}{
		{
			name: "valid schema",
			schema: DatabaseSchema{
				Version: "1.0",
				Schema: SchemaDefinition{
					Components: map[string]Component{
						"title": &TextComponent{BaseComponent: BaseComponent{Type: "text"}},
					},
					Entities: map[string]Entity{
						"post": {Components: []string{"title"}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing version field",
			schema: DatabaseSchema{
				Schema: SchemaDefinition{
					Components: map[string]Component{
						"title": &TextComponent{BaseComponent: BaseComponent{Type: "text"}},
					},
					Entities: map[string]Entity{
						"post": {Components: []string{"title"}},
					},
				},
			},
			wantErr: true,
		},

		{
			name: "missing schema",
			schema: DatabaseSchema{
				Version: "1.0",
			},
			wantErr: true,
		},
		{
			name: "missing components field",
			schema: DatabaseSchema{
				Version: "1.0",
				Schema: SchemaDefinition{
					Entities: map[string]Entity{
						"post": {Components: []string{"title"}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "missing entities field",
			schema: DatabaseSchema{
				Version: "1.0",
				Schema: SchemaDefinition{
					Components: map[string]Component{
						"title": &TextComponent{BaseComponent: BaseComponent{Type: "text"}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "empty components map",
			schema: DatabaseSchema{
				Version: "1.0",
				Schema: SchemaDefinition{
					Components: map[string]Component{},
					Entities: map[string]Entity{
						"post": {Components: []string{"title"}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "empty entities map",
			schema: DatabaseSchema{
				Version: "1.0",
				Schema: SchemaDefinition{
					Components: map[string]Component{
						"title": &TextComponent{BaseComponent: BaseComponent{Type: "text"}},
					},
					Entities: map[string]Entity{},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSchema(tt.schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInitSchema(t *testing.T) {
	tests := []struct {
		name     string
		filepath string
		wantErr  bool
	}{
		{"valid schema", "testdata/valid_schema.json", false},
		{"invalid json", "testdata/malformed.json", true},
		{"missing file", "testdata/nonexistent.json", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := InitSchema(tt.filepath)
			if (err != nil) != tt.wantErr {
				t.Errorf("InitSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
