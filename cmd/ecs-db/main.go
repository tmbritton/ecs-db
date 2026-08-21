package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/tmbritton/ecs-db/internal/config"
)

var cfgPath string

// rootCmd carries no RunE: running the game is the `run` subcommand, which is
// registered by run.go or run_stub.go depending on the build tag.
var rootCmd = &cobra.Command{
	Use:   "ecs-db",
	Short: "ECS-in-SQLite game engine and content tools",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Silence usage from here on, not via the struct field: cobra parses
		// flags before PersistentPreRunE, so a flag typo still gets its usage
		// block, while a runtime failure (bad config, missing build tag) gets
		// just the error. Setting SilenceUsage on the struct would suppress
		// both, leaving flag typos with no recovery hint at all.
		cmd.SilenceUsage = true
		return config.Init(cfgPath)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "./game.toml", "path to TOML config file")
	rootCmd.AddCommand(schemaCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
