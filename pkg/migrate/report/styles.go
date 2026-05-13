package report

import "github.com/charmbracelet/lipgloss"

// Shared styles used by both the live TUI (progress.go) and the
// final text report (text.go). Keeping these in one file makes the
// two outputs visually consistent.
var (
	styleTitle    = lipgloss.NewStyle().Bold(true)
	styleSubdued  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleSection  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	styleKey      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleSuccess  = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	styleRunning  = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	styleFailed   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	stylePartial  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	stylePending  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleArchive  = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	styleDiscard  = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	styleZero     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleEmDash   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleArrow    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleAccent   = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	styleHeaderHL = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
)

// statusSymbol returns the lipgloss-styled glyph used to indicate
// status across both the TUI and the final report.
func statusSymbol(s phaseStatus) string {
	switch s {
	case statusDone:
		return styleSuccess.Render("✓")
	case statusRunning:
		return styleRunning.Render("●")
	case statusFailed:
		return styleFailed.Render("✗")
	default:
		return stylePending.Render("·")
	}
}

// outcomeStyle returns the style used to render the given outcome.
func outcomeStyle(o Outcome) lipgloss.Style {
	switch o {
	case OutcomeSuccess:
		return styleSuccess
	case OutcomeFailed:
		return styleFailed
	default:
		return stylePartial
	}
}
