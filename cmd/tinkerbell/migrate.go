package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/peterbourgon/ff/v4"
	"github.com/tinkerbell/tinkerbell/crd"
	"github.com/tinkerbell/tinkerbell/pkg/backend/kube"
	"github.com/tinkerbell/tinkerbell/pkg/migrate/report"
	"github.com/tinkerbell/tinkerbell/pkg/migrate/runner"
	"k8s.io/client-go/dynamic"
)

// migrateConfig is the parsed CLI configuration for the migrate
// subcommand. Field defaults are applied in newMigrateCommand.
type migrateConfig struct {
	Workdir    string
	KubeConfig string
	DryRun     bool
	Report     string
	LogLevel   int
}

// newMigrateCommand builds the `tinkerbell migrate` ff subcommand.
// The returned command owns its own flag set so it does not inherit
// the long list of stack flags from the root command.
func newMigrateCommand() *ff.Command {
	cfg := &migrateConfig{
		KubeConfig: kubeConfig(),
		Report:     "tui",
	}

	fs := ff.NewFlagSet("tinkerbell migrate")
	fs.StringVar(&cfg.Workdir, 'w', "workdir", "", "Required: directory used for the migration. Holds source-v1alpha1/, target-v1alpha2/, state.json, logs/. Must be persisted between resumes.")
	fs.StringVar(&cfg.KubeConfig, 'k', "kubeconfig", cfg.KubeConfig, "Path to a kubeconfig file. Defaults to $HOME/.kube/config when present, otherwise in-cluster config.")
	fs.BoolVar(&cfg.DryRun, 'n', "dry-run", "Stop after the transform phase. The cluster is not modified.")
	fs.StringVar(&cfg.Report, 'r', "report", cfg.Report, "Final report renderer: tui (text table) or json.")
	fs.IntVar(&cfg.LogLevel, 'v', "log-level", 0, "Log verbosity level (0=info, 1=debug).")

	return &ff.Command{
		Name:     "migrate",
		Usage:    "tinkerbell migrate -w DIR [flags]",
		LongHelp: "Migrate a Tinkerbell installation from v1alpha1 to v1alpha2.\n\nThe migration is resumable: re-running with the same --workdir picks up where a previous run left off. The workdir holds an exported snapshot of v1alpha1 objects, the transformed v1alpha2 equivalents, and a state.json file recording per-phase progress.",
		Flags:    fs,
		Exec: func(ctx context.Context, _ []string) error {
			return runMigrate(ctx, cfg, os.Stdout, os.Stderr)
		},
	}
}

// runMigrate executes the migration with cfg already populated by
// flag parsing. It is split out from the ff.Command Exec closure so
// tests can drive it with a fixed migrateConfig.
func runMigrate(ctx context.Context, cfg *migrateConfig, stdout, stderr io.Writer) error {
	if cfg.Workdir == "" {
		return errors.New("--workdir is required")
	}
	if cfg.Report != "tui" && cfg.Report != "json" {
		return fmt.Errorf("--report must be tui or json (got %q)", cfg.Report)
	}

	log := getLogger(cfg.LogLevel).WithName("migrate")

	// invocationStart bounds the wall-clock interval reported in the
	// header. We deliberately do not use state.Started for this: that
	// timestamp is persisted in the workdir and survives resumes, so
	// reusing it would inflate the elapsed time of a quick resume to
	// "minutes since the first run".
	invocationStart := time.Now().UTC()

	rcfg := runner.Config{
		Workdir: cfg.Workdir,
		Logger:  log,
		DryRun:  cfg.DryRun,
	}
	var tui *report.TUIProgress
	if cfg.Report == "tui" {
		tui = report.NewTUIProgress(stderr)
		rcfg.Progress = tui
	}

	restCfg, err := kube.NewFileRestConfig(cfg.KubeConfig, "")
	if err != nil {
		return fmt.Errorf("load kubeconfig: %w", err)
	}
	// Defaults (5/10) cause a ~12s client-side throttle floor for a
	// run with ~60 per-object writes. Migrate is a one-shot batch tool
	// against a single apiserver, so we raise these aggressively. The
	// runner's Concurrency knob caps in-flight requests independently.
	restCfg.QPS = 50
	restCfg.Burst = 100
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("build dynamic client: %w", err)
	}
	rcfg.Client = runner.NewDynamicClusterClient(dyn)

	if !cfg.DryRun {
		tb, err := crd.NewTinkerbell(crd.WithLogger(log), crd.WithRestConfig(restCfg))
		if err != nil {
			return fmt.Errorf("build CRD client: %w", err)
		}
		rcfg.CRDInstaller = runner.NewTinkerbellCRDInstaller(&tb)
	}

	r, err := runner.New(rcfg)
	if err != nil {
		return err
	}

	state, runErr := r.Run(ctx)
	if tui != nil {
		tui.Stop()
	}
	if state != nil {
		rep := report.Build(state, invocationStart, time.Now().UTC())
		if writeErr := writeReport(stdout, rep, cfg.Report); writeErr != nil {
			fmt.Fprintf(stderr, "warning: failed to render report: %v\n", writeErr)
		}
	}
	return runErr
}

func writeReport(w io.Writer, r report.Report, format string) error {
	switch format {
	case "json":
		return report.WriteJSON(w, r)
	case "tui":
		return report.WriteText(w, r)
	default:
		return fmt.Errorf("unknown report format %q", format)
	}
}
