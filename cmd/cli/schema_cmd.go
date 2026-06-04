package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/schema"
)

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Schema utilities",
}

var schemaValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate schema.json and report its contents",
	RunE:  runSchemaValidate,
}

func init() {
	schemaCmd.AddCommand(schemaValidateCmd)
}

func runSchemaValidate(cmd *cobra.Command, args []string) error {
	s, err := schema.InitSchema(config.Get().Schema.Path)
	if err != nil {
		return fmt.Errorf("schema invalid: %w", err)
	}
	fmt.Printf("schema.json OK (version %d, %d components, %d entity types)\n",
		s.SchemaVersion, len(s.Components), len(s.EntityTypes))
	return nil
}
