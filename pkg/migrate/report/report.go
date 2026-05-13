// Package report builds and renders the final summary emitted by the
// migrate command. The Report struct is the single source of truth
// consumed by both --report json and --report tui (text).
//
// Outcome is derived from State (in pkg/migrate/runner). The package
// is intentionally read-only over the runner's state: it never
// touches the workdir or the cluster.
package report

import (
	"fmt"
	"sort"
	"time"

	"github.com/tinkerbell/tinkerbell/pkg/migrate/runner"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Outcome is the high-level result shown at the top of the report.
type Outcome string

const (
	// OutcomeSuccess: every phase done with no failures.
	OutcomeSuccess Outcome = "success"
	// OutcomePartial: at least one phase or kind is not yet done but
	// nothing has failed (typical for resume / dry-run).
	OutcomePartial Outcome = "partial"
	// OutcomeFailed: at least one kind recorded a failure.
	OutcomeFailed Outcome = "failed"
)

// Report is the final summary of a migrate run. The JSON shape is
// stable and documented in docs/technical/V1ALPHA1_TO_V1ALPHA2_MIGRATION.md.
type Report struct {
	Workdir     string            `json:"workdir"`
	StartedAt   time.Time         `json:"started_at,omitempty"`
	CompletedAt time.Time         `json:"completed_at,omitempty"`
	Outcome     Outcome           `json:"outcome"`
	Phases      runner.PhaseState `json:"phases"`
	Kinds       []KindReport      `json:"kinds"`
	Discarded   []DiscardedReport `json:"discarded,omitempty"`
}

// KindReport is one row of the per-kind table. Applied is zero for
// archive and drop handlings; renderers display "—" in those cases.
type KindReport struct {
	Name          string          `json:"-"`
	Source        string          `json:"source"`
	Target        string          `json:"target,omitempty"`
	Handling      runner.Handling `json:"handling"`
	Exported      int             `json:"exported"`
	Transformed   int             `json:"transformed"`
	Applied       int             `json:"applied"`
	SkippedResume int             `json:"skipped_resume"`
	Failed        int             `json:"failed"`
	Errors        []string        `json:"errors,omitempty"`
}

// DiscardedReport summarises a HandlingDrop kind: how many objects
// were seen and why they were discarded.
type DiscardedReport struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// targetVersion is the API version every apply/archive kind is being
// promoted to. Kept here (rather than derived from a v2 GVR) because
// the source GVR catalog already encodes the v1alpha1 group/resource
// and the target group is the same for every kind in scope.
const targetVersion = "v1alpha2"

// discardReason maps a HandlingDrop source kind name to the
// human-readable reason emitted in the report. Only bmctask is
// currently dropped; the map exists so future additions stay obvious.
var discardReason = map[string]string{
	"bmctask": "no v1alpha2 successor",
}

// Build assembles a Report from a finished (or partially finished)
// runner state. The completedAt argument is the wall-clock instant
// the run ended; pass time.Now() at call sites unless overriding for
// tests.
func Build(state *runner.State, completedAt time.Time) Report {
	r := Report{
		Workdir:     state.Workdir,
		StartedAt:   state.Started,
		CompletedAt: completedAt,
		Phases:      state.Phases,
	}

	for _, k := range runner.SourceKinds {
		counts := state.Counts[k.Name]
		if counts == nil {
			counts = &runner.Counts{}
		}
		if k.Handling == runner.HandlingDrop {
			r.Discarded = append(r.Discarded, DiscardedReport{
				Source: gvrString(k.GVR),
				Reason: discardReasonFor(k.Name),
				Count:  counts.Discarded,
			})
			continue
		}
		row := KindReport{
			Name:          k.Name,
			Source:        gvrString(k.GVR),
			Handling:      k.Handling,
			Exported:      counts.Exported,
			Transformed:   counts.Transformed,
			Applied:       counts.Applied,
			SkippedResume: counts.SkippedResume,
			Failed:        counts.Failed,
		}
		if k.Handling == runner.HandlingApply {
			row.Target = fmt.Sprintf("%s.%s/%s", k.TargetName, k.GVR.Group, targetVersion)
		}
		r.Kinds = append(r.Kinds, row)
	}

	sort.SliceStable(r.Kinds, func(i, j int) bool { return r.Kinds[i].Name < r.Kinds[j].Name })
	sort.SliceStable(r.Discarded, func(i, j int) bool { return r.Discarded[i].Source < r.Discarded[j].Source })

	r.Outcome = computeOutcome(state, r)
	return r
}

func discardReasonFor(name string) string {
	if r, ok := discardReason[name]; ok {
		return r
	}
	return "discarded"
}

func gvrString(gvr schema.GroupVersionResource) string {
	// Friendly form: "<resource>.<group>/<version>".
	return fmt.Sprintf("%s.%s/%s", gvr.Resource, gvr.Group, gvr.Version)
}

// computeOutcome inspects the persisted phase data and per-kind
// failure counts to derive the report Outcome.
func computeOutcome(state *runner.State, r Report) Outcome {
	for _, k := range r.Kinds {
		if k.Failed > 0 {
			return OutcomeFailed
		}
	}
	for _, d := range r.Discarded {
		_ = d // discards never contribute to failed
	}
	if !allPhasesDone(state) {
		return OutcomePartial
	}
	return OutcomeSuccess
}

func allPhasesDone(s *runner.State) bool {
	if s.Phases.ApplyCRDsAdditive != runner.PhaseDone ||
		s.Phases.DeleteOldCRDs != runner.PhaseDone ||
		s.Phases.ApplyCRDsFinal != runner.PhaseDone {
		return false
	}
	for _, k := range runner.SourceKinds {
		if s.Phases.Export[k.Name] != runner.PhaseDone {
			return false
		}
		if s.Phases.Transform[k.Name] != runner.PhaseDone {
			return false
		}
	}
	for _, t := range runner.ApplyKinds() {
		if s.Phases.ApplyObjects[t] != runner.PhaseDone {
			return false
		}
	}
	return true
}
