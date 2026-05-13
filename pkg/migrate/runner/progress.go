package runner

// Progress receives live notifications as the runner moves through
// phases and per-kind sub-steps. Implementations must be safe to
// call from a single goroutine; the runner does not invoke Progress
// concurrently. A zero-value (nil) Progress is treated as the
// no-op reporter defined below.
//
// Lifecycle:
//   - PhaseStart is called once when a phase begins.
//   - For phases that have per-kind sub-steps (export, transform,
//     apply_objects, delete_archived_objects), KindStart is called
//     with total = expected number of items (or 0 if unknown), then
//     KindItem is called once per item processed, then KindEnd is
//     called with the final error (nil on success).
//   - PhaseEnd is called once when the phase finishes, with the
//     final error (nil on success).
//
// Phases without per-kind sub-steps (the three CRD phases) emit
// only PhaseStart / PhaseEnd.
type Progress interface {
	PhaseStart(phase string)
	PhaseEnd(phase string, err error)
	KindStart(phase, kind string, total int)
	KindItem(phase, kind string)
	KindEnd(phase, kind string, err error)
}

// NopProgress is the do-nothing Progress used when Config.Progress
// is nil.
type NopProgress struct{}

func (NopProgress) PhaseStart(string)             {}
func (NopProgress) PhaseEnd(string, error)        {}
func (NopProgress) KindStart(string, string, int) {}
func (NopProgress) KindItem(string, string)       {}
func (NopProgress) KindEnd(string, string, error) {}
