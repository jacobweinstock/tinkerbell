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

	// Logger is used for human-readable progress and error reporting.
	// A discard logger is used when zero-valued.
	Logger logr.Logger

	// DryRun stops the runner after the transform phase. The cluster
	// is never modified.
	DryRun bool
}

// Runner orchestrates the migration phases described in
// docs/technical/V1ALPHA1_TO_V1ALPHA2_MIGRATION.md. It is safe to
// re-instantiate and Run a Runner against the same workdir; resume is
// handled automatically via state.json.
type Runner struct {
	cfg    Config
	layout Layout
	client ClusterClient
	log    logr.Logger
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
	return &Runner{
		cfg:    cfg,
		layout: NewLayout(cfg.Workdir),
		client: cfg.Client,
		log:    cfg.Logger,
	}, nil
}

// Layout returns the path layout the runner operates on.
func (r *Runner) Layout() Layout { return r.layout }

// Run executes every phase in order, persisting state to disk before
// and after each meaningful step. Phases already marked Done are
// skipped.
//
// Phase coverage in step 2 of the migration plan: export, transform,
// apply_objects. CRD additive / final and delete_old_crds phases are
// stubbed (set Done immediately) and will be filled in by step 4.
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

	// CRD phases are placeholders for step 4.
	if state.Phases.ApplyCRDsAdditive != PhaseDone {
		state.Phases.ApplyCRDsAdditive = PhaseDone
		if err := state.Save(r.layout); err != nil {
			return state, err
		}
	}

	if err := r.runApplyObjects(ctx, state); err != nil {
		return state, err
	}

	if state.Phases.DeleteOldCRDs != PhaseDone {
		state.Phases.DeleteOldCRDs = PhaseDone
		if err := state.Save(r.layout); err != nil {
			return state, err
		}
	}
	if state.Phases.ApplyCRDsFinal != PhaseDone {
		state.Phases.ApplyCRDsFinal = PhaseDone
		if err := state.Save(r.layout); err != nil {
			return state, err
		}
	}
	return state, nil
}
