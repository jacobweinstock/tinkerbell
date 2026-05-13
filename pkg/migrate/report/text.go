package report

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/tinkerbell/tinkerbell/pkg/migrate/runner"
)

// WriteText renders r as a human-readable, terminal-friendly report.
// Styling is provided by lipgloss and intentionally mirrors the live
// progress view (see progress.go) so the two outputs feel like one
// continuous experience.
//
// The layout matches the description in
// docs/technical/V1ALPHA1_TO_V1ALPHA2_MIGRATION.md: header, per-kind
// table, archived panel, discarded panel, next-steps panel.
func WriteText(w io.Writer, r Report) error {
	var b strings.Builder

	writeHeader(&b, r)
	b.WriteString("\n")
	writeKindTable(&b, r.Kinds)
	writeArchived(&b, r.Kinds)
	writeDiscarded(&b, r.Discarded)
	writeNextSteps(&b, r)

	_, err := io.WriteString(w, b.String())
	return err
}

func writeHeader(b *strings.Builder, r Report) {
	title := styleTitle.Render("Tinkerbell ") +
		styleAccent.Render("v1alpha1") +
		styleArrow.Render(" → ") +
		styleAccent.Render("v1alpha2") +
		styleTitle.Render(" migration")
	fmt.Fprintln(b, title)

	wall := durationStr(r.StartedAt, r.CompletedAt)
	outcome := outcomeStyle(r.Outcome).Render(string(r.Outcome))
	sep := styleSubdued.Render("  ·  ")
	meta := styleKey.Render("workdir") + " " + r.Workdir + sep +
		styleKey.Render("wall time") + " " + wall + sep +
		styleKey.Render("outcome") + " " + outcome
	fmt.Fprintln(b, "  "+meta)
}

func writeKindTable(b *strings.Builder, kinds []KindReport) {
	headers := []string{"SOURCE", "TARGET", "HANDLING", "EXPORTED", "TRANSFORMED", "APPLIED", "SKIPPED", "FAILED"}

	rows := make([][]string, 0, len(kinds))
	for _, k := range kinds {
		applied := strconv.Itoa(k.Applied)
		if k.Handling != runner.HandlingApply {
			applied = styleEmDash.Render("—")
		}
		target := k.Target
		if target == "" {
			target = styleEmDash.Render("—")
		}
		rows = append(rows, []string{
			k.Source,
			target,
			handlingLabel(k.Handling),
			numCell(k.Exported),
			numCell(k.Transformed),
			applied,
			numCell(k.SkippedResume),
			failedCell(k.Failed),
		})
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8"))).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			base := lipgloss.NewStyle().Padding(0, 1)
			if row == table.HeaderRow {
				return base.Bold(true).Foreground(lipgloss.Color("15"))
			}
			// Right-align numeric columns (EXPORTED..FAILED).
			if col >= 3 {
				return base.Align(lipgloss.Right)
			}
			return base
		})

	fmt.Fprintln(b, t.Render())
}

func writeArchived(b *strings.Builder, kinds []KindReport) {
	archived := filterByHandling(kinds, runner.HandlingArchive, runner.HandlingArchiveVerbatim)
	if len(archived) == 0 {
		return
	}
	fmt.Fprintln(b)
	fmt.Fprintln(b, styleSection.Render("Archived")+" "+styleSubdued.Render("(transformed, not applied)"))
	for _, k := range archived {
		fmt.Fprintf(b, "  %s %s  %s\n",
			styleArchive.Render("⤓"),
			padRight(k.Source, 50),
			styleSubdued.Render(fmt.Sprintf("%d objects", k.Transformed)),
		)
	}
}

func writeDiscarded(b *strings.Builder, discarded []DiscardedReport) {
	if len(discarded) == 0 {
		return
	}
	fmt.Fprintln(b)
	fmt.Fprintln(b, styleSection.Render("Discarded"))
	for _, d := range discarded {
		fmt.Fprintf(b, "  %s %s  %s  %s\n",
			styleDiscard.Render("✗"),
			padRight(d.Source, 50),
			styleSubdued.Render(fmt.Sprintf("%d objects", d.Count)),
			styleDiscard.Render("("+d.Reason+")"),
		)
	}
}

func writeNextSteps(b *strings.Builder, r Report) {
	fmt.Fprintln(b)
	fmt.Fprintln(b, styleSection.Render("Next steps"))
	fmt.Fprintf(b, "  %s\n", nextStep(r))
}

func handlingLabel(h runner.Handling) string {
	switch h {
	case runner.HandlingApply:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(string(h))
	case runner.HandlingArchive, runner.HandlingArchiveVerbatim:
		return styleArchive.Render(string(h))
	case runner.HandlingDrop:
		return styleDiscard.Render(string(h))
	default:
		return string(h)
	}
}

func numCell(n int) string {
	if n == 0 {
		return styleZero.Render("0")
	}
	return strconv.Itoa(n)
}

func failedCell(n int) string {
	if n == 0 {
		return styleZero.Render("0")
	}
	return styleFailed.Render(strconv.Itoa(n))
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
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
