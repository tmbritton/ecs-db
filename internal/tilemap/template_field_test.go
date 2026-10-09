package tilemap

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func TestValidateTileTemplateField_UsesTheSchemaRatherThanTiledValueType(t *testing.T) {
	db := &schema.DatabaseSchema{Components: map[string]schema.Component{
		"Health": {Type: "object", Properties: map[string]schema.Property{"hp": {Type: "integer"}}},
		"Label":  {Type: "string"},
	}}
	for _, tt := range []struct {
		name, field, raw, wantErr string
	}{
		{"valid number in string Tiled property", "Health.hp", "5", ""},
		{"schema integer refuses word", "Health.hp", "lots", "whole number"},
		{"unknown field", "Health.power", "5", "does not declare"},
		{"unknown component", "Unknown.hp", "5", "does not declare"},
		{"scalar value", "Label.value", "ready", ""},
		{"scalar wrong field", "Label.hp", "5", "holds its value"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTileTemplateField(db, tt.field, tt.raw)
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Errorf("ValidateTileTemplateField(%q,%q) = %v, want %q", tt.field, tt.raw, err, tt.wantErr)
			}
		})
	}
}

func TestValidateTileTemplateComponents_UsesTheImporterEntityContract(t *testing.T) {
	db := &schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Position": {Type: "object"}, "Health": {Type: "object", Properties: map[string]schema.Property{"hp": {Type: "integer"}}},
			"TileVisual": {Type: "object"}, "TileEntityOwner": {Type: "entityRef"}, "Extra": {Type: "string"},
		},
		EntityTypes: map[string]schema.EntityType{
			"Wall": {RequiredComponents: []string{"Health"}, OptionalComponents: []string{"TileVisual", "TileEntityOwner"}, ValidationLevel: schema.ValidationStrict},
		},
	}
	set := &tiled.Tileset{Properties: tiled.Properties{"entityType": {Value: "Wall"}}, Tiles: map[uint32]tiled.TilesetTile{}}
	for _, tt := range []struct {
		name, want string
		props      tiled.Properties
	}{
		{"missing required", "missing required component", nil},
		{"valid required component", "", tiled.Properties{"Health.hp": {Value: "5"}}},
		{"disallowed extra", "not allowed", tiled.Properties{"Health.hp": {Value: "5"}, "Extra.value": {Value: "wrong"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			set.Tiles[0] = tiled.TilesetTile{ID: 0, Properties: tt.props}
			vr := ValidateTileTemplateComponents(db, set, 0)
			if tt.want == "" && !vr.Valid() || tt.want != "" && (vr.Valid() || !strings.Contains(strings.Join(vr.Errors, " "), tt.want)) {
				t.Errorf("tile contract = %+v, want %q", vr, tt.want)
			}
		})
	}
}
