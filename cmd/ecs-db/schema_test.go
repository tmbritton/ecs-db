package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// execute runs the root command with args, capturing stdout/stderr. It exists
// so tests exercise the wiring a user actually hits rather than calling RunE
// directly.
func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	err := rootCmd.Execute()
	return buf.String(), err
}

// writeProject lays down a game.toml + schema.json pair and returns the
// config path.
func writeProject(t *testing.T, schemaJSON string) string {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(schemaJSON), 0o644); err != nil {
		t.Fatalf("writing schema: %v", err)
	}
	cfgPath := filepath.Join(dir, "game.toml")
	cfg := "[database]\npath = \"" + filepath.Join(dir, "test.db") + "\"\n" +
		"[schema]\npath = \"" + schemaPath + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return cfgPath
}

const validSchema = `{
  "schemaVersion": 3,
  "components": {
    "Position": {"type": "object", "properties": {"x": {"type": "number"}, "y": {"type": "number"}}}
  },
  "entityTypes": {
    "Player": {"requiredComponents": ["Position"], "optionalComponents": [],
               "allowExtraComponents": false, "validationLevel": "strict"}
  }
}`

func TestSchemaValidate(t *testing.T) {
	tests := []struct {
		name       string
		schema     string
		wantErr    bool
		wantOutput string
	}{
		{
			name:       "valid schema reports its contents",
			schema:     validSchema,
			wantOutput: "schema.json OK (version 3, 1 components, 1 entity types)",
		},
		{
			name:    "malformed json is rejected",
			schema:  `{"schemaVersion": 3, `,
			wantErr: true,
		},
		{
			name: "component named Behavior is reserved",
			schema: `{"schemaVersion": 1,
				"components": {"Behavior": {"type": "entity-ref"}},
				"entityTypes": {"E": {"requiredComponents": ["Behavior"]}}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := execute(t, "schema", "validate", "--config", writeProject(t, tt.schema))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got none (output %q)", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out, tt.wantOutput) {
				t.Errorf("output %q does not contain %q", out, tt.wantOutput)
			}
		})
	}
}

// Running the game is `ecs-db run`, not the bare root command. If a future
// story wires RunE onto the root, `ecs-db` with no args would launch a game
// window instead of printing help.
func TestRootCmd_HasNoRunE(t *testing.T) {
	if rootCmd.RunE != nil || rootCmd.Run != nil {
		t.Error("rootCmd must have no Run/RunE — running the game is the `run` subcommand")
	}
}
