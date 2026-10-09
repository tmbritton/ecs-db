package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/forge/animations"
	"github.com/tmbritton/ecs-db/internal/forge/eventbus"
	"github.com/tmbritton/ecs-db/internal/forge/machines"
	"github.com/tmbritton/ecs-db/internal/forge/maps"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/forge/server"
	"github.com/tmbritton/ecs-db/internal/forge/session"
	"github.com/tmbritton/ecs-db/internal/forge/status"
	"github.com/tmbritton/ecs-db/internal/forge/tilesets"
	"github.com/tmbritton/ecs-db/internal/forge/web"
	"github.com/tmbritton/ecs-db/internal/schema"
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
	var machineSession *machines.Session
	var mapSession *maps.Session
	var tilesetSession *tilesets.Session
	var animationSession *animations.Session
	var resolved []project.Machine
	var behaviorDirs []string
	var problems []project.Problem
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
		resolved = proj.Machines
		problems = proj.Problems
		// In load order, and only the mods that contribute one. Inline
		// validation runs the engine's own behaviour-reference check against
		// each in turn, because that check takes a single directory — the
		// interpreter calls it with one — while a project has as many as it has
		// mods and the loader accepts a machine from any of them.
		for _, mod := range proj.Mods {
			if mod.Behaviors != "" {
				behaviorDirs = append(behaviorDirs, mod.Behaviors)
			}
		}

		// The editing session for the behaviour machines. Its schema is read
		// through a function rather than captured, because machine validation
		// depends on the schema — a context key has to match exactly one
		// component field — and the user may have unsaved schema edits.
		// Validating against the file on disk would report problems the
		// editor's own state says are already fixed.
		currentSchema := func() schema.DatabaseSchema {
			if editing == nil {
				return proj.Schema
			}
			var out schema.DatabaseSchema
			editing.Read(func(d schema.DatabaseSchema) { out = d })
			return out
		}
		machineSession, err = machines.Open(machines.Config{
			Mods:   proj.Mods,
			HasMap: cfg.Map.Path != "",
			Schema: currentSchema,
		})
		if err != nil {
			slog.Warn("starting the machine editing session", "err", err)
		}

		// The editing session for the project's maps. A project with no [map]
		// opens one holding nothing, which is a state MAP mode renders rather
		// than a failure — the map is the one thing a project can legitimately
		// not have and still be worth editing.
		mapSession = maps.Open(maps.Config{
			// The config file's directory is the project, and is what bounds
			// what MAP mode will serve to the browser.
			Root:    filepath.Dir(proj.ConfigPath),
			MapPath: proj.MapPath,
		})
		tilesetSession = tilesets.Open(filepath.Dir(proj.ConfigPath), mapSession)
		mapSession.SetTilesetOpener(tilesetSession.WorkingBytes)
		assetMods := make([]animations.AssetMod, 0, len(cfg.Mods))
		for _, mod := range cfg.Mods {
			assetMods = append(assetMods, animations.AssetMod{Name: mod.Name, Assets: mod.Assets})
		}
		animationSession = animations.Open(animations.Config{Root: filepath.Dir(proj.ConfigPath), Mods: assetMods})
	}

	srv := server.New(server.Config{
		Addr: cfg.Forge.Addr,
		// Built here rather than left to the server to default, because this is
		// the composition root and later epics have a second consumer: LIVE
		// publishes world_version ticks from outside the HTTP handlers, and it
		// has to reach the same bus the streams are listening on.
		Bus:              eventbus.New(slog.Default()),
		Session:          editing,
		Engine:           engine,
		PollInterval:     cfg.Forge.PollInterval(),
		Machines:         resolved,
		MachineSession:   machineSession,
		MapSession:       mapSession,
		TilesetSession:   tilesetSession,
		AnimationSession: animationSession,
		TileSize:         cfg.Window.TileSize,
		BehaviorDirs:     behaviorDirs,
		Problems:         problems,
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
