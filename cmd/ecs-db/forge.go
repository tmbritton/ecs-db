package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/forge/server"
	"github.com/tmbritton/ecs-db/internal/forge/web"
)

// shutdownGrace bounds how long in-flight requests get to finish. Forge holds
// long-lived SSE connections, so this is a drain deadline, not a guess at
// request duration.
const shutdownGrace = 5 * time.Second

var forgeCmd = &cobra.Command{
	Use:   "forge",
	Short: "Run the Forge content editor",
	Long: "Serves the Forge content editor, a web UI for authoring schema.json, " +
		"behaviour machines, maps, tilesets and sprite animations.",
	RunE: runForge,
}

func init() { rootCmd.AddCommand(forgeCmd) }

func runForge(cmd *cobra.Command, _ []string) error {
	cfg := config.Get()
	srv := server.New(server.Config{Addr: cfg.Forge.Addr}, web.Static)

	// Bind before announcing anything: otherwise a bind failure prints
	// "listening on ..." and only then returns the error.
	if err := srv.Listen(); err != nil {
		return fmt.Errorf("forge: %w", err)
	}

	// cobra sets a context in ExecuteC, but leaves it nil when RunE is called
	// directly — as a test does — and signal.NotifyContext(nil, ...) panics.
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve() }()

	// srv.Addr() is the bound address, so a configured port of 0 prints the
	// real one rather than ":0".
	fmt.Fprintf(cmd.OutOrStdout(), "⚒ Forge listening on http://%s\n", srv.Addr())

	select {
	case err := <-errCh:
		// Failed to bind, or died on its own.
		return err
	case <-ctx.Done():
		// Interrupted: drain on a fresh context, since ctx is already cancelled.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
