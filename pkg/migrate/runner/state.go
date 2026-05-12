package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Phase describes the state of a phase or per-kind sub-step. A
// not-present entry in the maps is treated identically to Pending.
type Phase string

const (
	PhasePending    Phase = "pending"
	PhaseInProgress Phase = "in_progress"
	PhaseDone       Phase = "done"
	PhaseFailed     Phase = "failed"
)

// State is the persisted on-disk record of a migration's progress.
// It is loaded at the start of every Run and written back atomically
// after each meaningful step.
type State struct {
	Version int       `json:"version"`
	Workdir string    `json:"workdir"`
	Started time.Time `json:"started_at,omitempty"`
	Updated time.Time `json:"updated_at,omitempty"`

	Phases PhaseState `json:"phases"`
	Counts KindCounts `json:"counts,omitempty"`
}

// PhaseState tracks per-phase progress. Per-kind sub-step maps are
// keyed by the kind name used in the workdir layout (e.g. "hardware",
// "template", "bmcmachine").
type PhaseState struct {
	Export            map[string]Phase `json:"export,omitempty"`
	Transform         map[string]Phase `json:"transform,omitempty"`
	ApplyCRDsAdditive Phase            `json:"apply_crds_additive,omitempty"`
	ApplyObjects      map[string]Phase `json:"apply_objects,omitempty"`
	DeleteOldCRDs     Phase            `json:"delete_old_crds,omitempty"`
	ApplyCRDsFinal    Phase            `json:"apply_crds_final,omitempty"`
}

// KindCounts records per-kind tallies used in the final report.
type KindCounts map[string]*Counts

// Counts is one kind's tally.
type Counts struct {
	Exported      int `json:"exported,omitempty"`
	Transformed   int `json:"transformed,omitempty"`
	Applied       int `json:"applied,omitempty"`
	SkippedResume int `json:"skipped_resume,omitempty"`
	Failed        int `json:"failed,omitempty"`
	Discarded     int `json:"discarded,omitempty"`
}

// stateVersion is the on-disk schema version. Bumping this and adding
// a migration step here is the supported way to evolve the file.
const stateVersion = 1

// LoadState reads state from layout.StateFile(). If the file does not
// exist it returns a freshly initialised State.
func LoadState(layout Layout) (*State, error) {
	data, err := os.ReadFile(layout.StateFile())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &State{
				Version: stateVersion,
				Workdir: layout.Workdir,
				Started: time.Now().UTC(),
				Phases:  PhaseState{},
				Counts:  KindCounts{},
			}, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	if s.Version != stateVersion {
		return nil, fmt.Errorf("state.json version %d is not supported (expected %d)", s.Version, stateVersion)
	}
	if s.Workdir != "" && s.Workdir != layout.Workdir {
		return nil, fmt.Errorf("state.json workdir mismatch: file says %q, current run says %q", s.Workdir, layout.Workdir)
	}
	if s.Counts == nil {
		s.Counts = KindCounts{}
	}
	return &s, nil
}

// Save writes the state atomically: write a temp file in the same
// directory, fsync, then rename over the target. The rename is the
// atomic commit point on POSIX filesystems.
func (s *State) Save(layout Layout) error {
	s.Updated = time.Now().UTC()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	target := layout.StateFile()
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create state tmp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write state tmp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("fsync state tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close state tmp: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}

// SetExport records a phase value for a single kind in the export
// stage. The map is created if necessary.
func (s *State) SetExport(kind string, p Phase) {
	if s.Phases.Export == nil {
		s.Phases.Export = map[string]Phase{}
	}
	s.Phases.Export[kind] = p
}

// SetTransform records a phase value for a single kind in the
// transform stage.
func (s *State) SetTransform(kind string, p Phase) {
	if s.Phases.Transform == nil {
		s.Phases.Transform = map[string]Phase{}
	}
	s.Phases.Transform[kind] = p
}

// SetApplyObjects records a phase value for a single kind in the
// apply-objects stage.
func (s *State) SetApplyObjects(kind string, p Phase) {
	if s.Phases.ApplyObjects == nil {
		s.Phases.ApplyObjects = map[string]Phase{}
	}
	s.Phases.ApplyObjects[kind] = p
}

// Count returns the Counts entry for kind, creating it lazily.
func (s *State) Count(kind string) *Counts {
	c, ok := s.Counts[kind]
	if !ok {
		c = &Counts{}
		s.Counts[kind] = c
	}
	return c
}
