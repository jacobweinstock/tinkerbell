package runner

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
)

// Config configures a Runner.
type Config struct {
	// Workdir is the root of the migration directory layout. Required.
	Workdir string

	// Client provides cluster operations (List for export, ApplyPatch
	// for apply_objects). Required for any phase that talks to the
	// cluster; may be nil when DryRun is true.
	Client ClusterClient

	// CRDInstaller drives the apply_crds_additive, delete_old_crds and
	// apply_crds_final phases. When nil the runner treats those
	// phases as no-ops (useful in tests and for partial dry-runs that
	// only exercise object I/O).
	CRDInstaller CRDInstaller

	// Logger is used for human-readable progress and error reporting.
	// A discard logger is used when zero-valued.
	Logger logr.Logger

	// Progress receives live phase / per-kind notifications as Run
	// makes progress. Optional: a nil value is treated as
	// NopProgress.
	Progress Progress

	// DryRun stops the runner after the transform phase. The cluster
	// is never modified.
	DryRun bool

	// Concurrency caps the number of in-flight per-object requests
	// inside a single phase (apply_objects, delete_archived_objects).
	// Zero or negative selects a sensible default.
	Concurrency int
}

// Runner orchestrates the migration phases described in
// docs/technical/V1ALPHA1_TO_V1ALPHA2_MIGRATION.md. It is safe to
// re-instantiate and Run a Runner against the same workdir; resume is
// handled automatically via state.json.
type Runner struct {
	cfg         Config
	layout      Layout
	client      ClusterClient
	crds        CRDInstaller
	log         logr.Logger
	progress    Progress
	concurrency int
}

// New builds a Runner from cfg. It validates the configuration but
// does not touch the filesystem or cluster.
func New(cfg Config) (*Runner, error) {
	if cfg.Workdir == "" {
		return nil, errors.New("runner.New: Config.Workdir is required")
	}
	if !cfg.DryRun && cfg.Client == nil {
		return nil, errors.New("runner.New: Config.Client is required when DryRun is false")
	}
	prog := cfg.Progress
	if prog == nil {
		prog = NopProgress{}
	}
	conc := cfg.Concurrency
	if conc <= 0 {
		conc = 8
	}
	return &Runner{
		cfg:         cfg,
		layout:      NewLayout(cfg.Workdir),
		client:      cfg.Client,
		crds:        cfg.CRDInstaller,
		log:         cfg.Logger,
		progress:    prog,
		concurrency: conc,
	}, nil
}

// Layout returns the path layout the runner operates on.
func (r *Runner) Layout() Layout { return r.layout }

// Run executes every phase in order, persisting state to disk before
// and after each meaningful step. Phases already marked Done are
// skipped.
//
// Phase order: export, transform, apply_crds_additive, apply_objects,
// delete_archived_objects, delete_old_crds, apply_crds_final. The
// three CRD phases are no-ops when Config.CRDInstaller is nil.
func (r *Runner) Run(ctx context.Context) (*State, error) {
	if err := r.layout.Init(); err != nil {
		return nil, fmt.Errorf("init layout: %w", err)
	}
	state, err := LoadState(r.layout)
	if err != nil {
		return nil, err
	}
	if err := state.Save(r.layout); err != nil {
		return nil, err
	}

	if err := r.runExport(ctx, state); err != nil {
		return state, err
	}
	if err := r.runTransform(ctx, state); err != nil {
		return state, err
	}
	if r.cfg.DryRun {
		return state, nil
	}

	if err := r.runCRDPhase(ctx, state, &state.Phases.ApplyCRDsAdditive, "apply_crds_additive", func(c context.Context) error {
		if r.crds == nil {
			return nil
		}
		return r.crds.ApplyAdditive(c)
	}); err != nil {
		return state, err
	}

	if err := r.runApplyObjects(ctx, state); err != nil {
		return state, err
	}

	if err := r.runDeleteArchivedObjects(ctx, state); err != nil {
		return state, err
	}

	if err := r.runCRDPhase(ctx, state, &state.Phases.DeleteOldCRDs, "delete_old_crds", func(c context.Context) error {
		if r.crds == nil {
			return nil
		}
		return r.crds.DeleteOld(c)
	}); err != nil {
		return state, err
	}

	if err := r.runCRDPhase(ctx, state, &state.Phases.ApplyCRDsFinal, "apply_crds_final", func(c context.Context) error {
		if r.crds == nil {
			return nil
		}
		return r.crds.ApplyFinal(c)
	}); err != nil {
		return state, err
	}
	return state, nil
}

// runCRDPhase is the shared scaffolding for the three CRD phases.
// It marks the phase in-progress, runs fn, then marks done; on
// failure it records "failed" and returns the wrapped error.
func (r *Runner) runCRDPhase(ctx context.Context, state *State, slot *Phase, name string, fn func(context.Context) error) error {
	if *slot == PhaseDone {
		return nil
	}
	r.progress.PhaseStart(name)
	*slot = PhaseInProgress
	if err := state.Save(r.layout); err != nil {
		r.progress.PhaseEnd(name, err)
		return err
	}
	if err := fn(ctx); err != nil {
		*slot = PhaseFailed
		_ = state.Save(r.layout)
		r.progress.PhaseEnd(name, err)
		return fmt.Errorf("%s: %w", name, err)
	}
	*slot = PhaseDone
	if err := state.Save(r.layout); err != nil {
		r.progress.PhaseEnd(name, err)
		return err
	}
	r.progress.PhaseEnd(name, nil)
	return nil
}
