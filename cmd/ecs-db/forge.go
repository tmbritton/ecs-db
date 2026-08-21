package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/forge/server"
	"github.com/tmbritton/ecs-db/internal/forge/session"
	"github.com/tmbritton/ecs-db/internal/forge/status"
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

	// The readout compares the database's recorded schema_version against the
	// schema.json this project declares. Both are paths rather than values:
	// Forge exists to edit that file, so a version read once here would go
	// stale the first time someone bumps it, and a schema that fails to load is
	// reported as such rather than stopping the editor — fixing it is exactly
	// what Forge is for.
	// Resolve the project so its problems are visible at startup. A project
	// that will not open must not stop the editor: fixing a broken schema or a
	// bad machine file is exactly what Forge is for, and refusing to start
	// would leave no way to do it. Epic 12 renders this; for now it is logged.
	//
	// config.Load resolves every path against the config file that declared
	// them, so these are already absolute and mean the same thing the engine
	// and the project model mean. That was not always true: the engine read
	// them verbatim while internal/forge/project resolved them, and the two
	// halves of one process looked for the same schema.json in different
	// places. Taking them from cfg is now correct even when the project fails
	// to open — which is exactly the case Forge exists to fix.
	engine := status.Config{DBPath: cfg.Database.Path, SchemaPath: cfg.Schema.Path}
	if len(cfg.Mods) > 0 {
		engine.ModName = cfg.Mods[0].Name
	}
	var editing *session.Session
	if proj, err := project.Open(cfgPath); err != nil {
		slog.Warn("opening project", "config", cfgPath, "err", err)
	} else {
		slog.Info("project opened",
			"schema", proj.SchemaPath, "mods", len(proj.Mods), "machines", len(proj.Machines))
		for _, p := range proj.Problems {
			slog.Warn("project problem", "path", p.Path, "err", p.Err)
		}

		// The one editable schema.json both modes work on. A failure here is
		// logged rather than fatal, for the same reason opening the project is:
		// the modes stay readable and the reason stays visible, and fixing a
		// broken schema is exactly what Forge is for.
		if editing, err = session.Open(proj.SchemaPath); err != nil {
			slog.Warn("starting an editing session", "schema", proj.SchemaPath, "err", err)
		}
	}

	srv := server.New(server.Config{
		Addr:         cfg.Forge.Addr,
		Session:      editing,
		Engine:       engine,
		PollInterval: cfg.Forge.PollInterval(),
	}, web.Static)

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
