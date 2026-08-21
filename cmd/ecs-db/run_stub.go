//go:build !ebitengine

package main

import (
	"errors"

	"github.com/spf13/cobra"
)

// errNoEbitengine explains why `run` cannot work in a headless build. The
// command is still registered so the CLI surface matches the tagged build and
// the user gets an explanation rather than "unknown command".
var errNoEbitengine = errors.New(
	"this binary was built without the \"ebitengine\" build tag; rebuild with `make build` to run the game")

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the game (unavailable in this build)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return errNoEbitengine
	},
}

func init() { rootCmd.AddCommand(runCmd) }
