package report

import (
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/tinkerbell/tinkerbell/pkg/migrate/runner"
)

// WriteText renders r as a human-readable, terminal-friendly report.
// The layout matches the description in
// docs/technical/V1ALPHA1_TO_V1ALPHA2_MIGRATION.md: header, per-kind
// table, archived panel, discarded panel, next-steps panel.
//
// Output is plain text with tabwriter-aligned columns; ANSI styling
// can be layered on by callers if a TTY is detected.
func WriteText(w io.Writer, r Report) error {
	if _, err := fmt.Fprintln(w, "Tinkerbell v1alpha1 -> v1alpha2 migration"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  workdir:   %s\n", r.Workdir); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  wall time: %s\n", durationStr(r.StartedAt, r.CompletedAt)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  outcome:   %s\n\n", r.Outcome); err != nil {
		return err
	}

	if err := writeKindTable(w, r.Kinds); err != nil {
		return err
	}

	if archived := filterByHandling(r.Kinds, runner.HandlingArchive, runner.HandlingArchiveVerbatim); len(archived) > 0 {
		if _, err := fmt.Fprintln(w, "\nArchived (transformed, not applied):"); err != nil {
			return err
		}
		for _, k := range archived {
			if _, err := fmt.Fprintf(w, "  %-50s %d objects\n", k.Source, k.Transformed); err != nil {
				return err
			}
		}
	}

	if len(r.Discarded) > 0 {
		if _, err := fmt.Fprintln(w, "\nDiscarded:"); err != nil {
			return err
		}
		for _, d := range r.Discarded {
			if _, err := fmt.Fprintf(w, "  %-50s %d objects  (%s)\n", d.Source, d.Count, d.Reason); err != nil {
				return err
			}
		}
	}

	if _, err := fmt.Fprintln(w, "\nNext steps:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  %s\n", nextStep(r)); err != nil {
		return err
	}
	return nil
}

func writeKindTable(w io.Writer, kinds []KindReport) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "SOURCE\tTARGET\tHANDLING\tEXPORTED\tTRANSFORMED\tAPPLIED\tSKIPPED\tFAILED"); err != nil {
		return err
	}
	for _, k := range kinds {
		applied := strconv.Itoa(k.Applied)
		if k.Handling != runner.HandlingApply {
			applied = "—"
		}
		target := k.Target
		if target == "" {
			target = "—"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\t%d\t%d\n",
			k.Source, target, k.Handling,
			k.Exported, k.Transformed, applied,
			k.SkippedResume, k.Failed,
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func filterByHandling(kinds []KindReport, hs ...runner.Handling) []KindReport {
	set := map[runner.Handling]struct{}{}
	for _, h := range hs {
		set[h] = struct{}{}
	}
	out := []KindReport{}
	for _, k := range kinds {
		if _, ok := set[k.Handling]; ok {
			out = append(out, k)
		}
	}
	return out
}

func durationStr(start, end time.Time) string {
	if start.IsZero() || end.IsZero() {
		return "n/a"
	}
	d := end.Sub(start).Round(time.Second)
	return d.String()
}

func nextStep(r Report) string {
	switch r.Outcome {
	case OutcomeSuccess:
		return "Migration complete. The workdir under " + r.Workdir + " can be deleted once you have confirmed cluster state."
	case OutcomeFailed:
		return "Inspect the kinds with FAILED > 0, fix the source objects, and re-run the migrate subcommand. Per-kind state is preserved in state.json so completed work will not be repeated."
	default:
		return "Re-run the migrate subcommand with the same --workdir to resume from where it stopped."
	}
}
