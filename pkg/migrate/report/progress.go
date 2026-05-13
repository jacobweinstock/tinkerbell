// Package report's progress.go provides a runner.Progress
// implementation that renders live phase/kind progress to a TTY using
// bubbletea. When the configured writer is not a TTY the
// implementation falls back to plain line-per-event output so logs
// stay readable in CI / piped contexts.
package report

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/tinkerbell/tinkerbell/pkg/migrate/runner"
)

// TUIProgress is a runner.Progress that renders live status to a
// TTY. It must be Stop()'d after the migration finishes (success or
// failure) so the bubbletea program tears down cleanly.
type TUIProgress struct {
	prog   *tea.Program
	done   chan struct{}
	plain  *plainProgress // non-TTY fallback (nil when TTY)
	closed bool
	mu     sync.Mutex
}

// NewTUIProgress builds a TUIProgress writing to w. When w is not a
// TTY (piped output, redirected to a file, CI), bubbletea is not
// started and the returned Progress prints one line per event
// instead.
func NewTUIProgress(w io.Writer) *TUIProgress {
	if !isTTY(w) {
		return &TUIProgress{plain: &plainProgress{w: w, started: time.Now()}}
	}
	m := newProgressModel()
	p := tea.NewProgram(
		m,
		tea.WithOutput(w),
		tea.WithoutSignalHandler(),
		tea.WithInput(nil),
	)
	t := &TUIProgress{prog: p, done: make(chan struct{})}
	go func() {
		// Errors from Run are not actionable here (the migration
		// outcome is reported separately). Closing done unblocks
		// Stop() so the caller can synchronize on teardown.
		_, _ = p.Run()
		close(t.done)
	}()
	return t
}

// Stop quits the bubbletea program and waits for its render goroutine
// to exit. Safe to call multiple times.
func (t *TUIProgress) Stop() {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.mu.Unlock()
	if t.prog == nil {
		return
	}
	t.prog.Quit()
	<-t.done
}

func (t *TUIProgress) PhaseStart(phase string) {
	if t.plain != nil {
		t.plain.PhaseStart(phase)
		return
	}
	t.send(phaseStartMsg{phase: phase})
}

func (t *TUIProgress) PhaseEnd(phase string, err error) {
	if t.plain != nil {
		t.plain.PhaseEnd(phase, err)
		return
	}
	t.send(phaseEndMsg{phase: phase, err: err})
}

func (t *TUIProgress) KindStart(phase, kind string, total int) {
	if t.plain != nil {
		t.plain.KindStart(phase, kind, total)
		return
	}
	t.send(kindStartMsg{phase: phase, kind: kind, total: total})
}

func (t *TUIProgress) KindItem(phase, kind string) {
	if t.plain != nil {
		t.plain.KindItem(phase, kind)
		return
	}
	t.send(kindItemMsg{phase: phase, kind: kind})
}

func (t *TUIProgress) KindEnd(phase, kind string, err error) {
	if t.plain != nil {
		t.plain.KindEnd(phase, kind, err)
		return
	}
	t.send(kindEndMsg{phase: phase, kind: kind, err: err})
}

func (t *TUIProgress) send(msg tea.Msg) {
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed || t.prog == nil {
		return
	}
	t.prog.Send(msg)
}

// runner.Progress compile-time assertion.
var _ runner.Progress = (*TUIProgress)(nil)

// --- bubbletea model ----------------------------------------------

// phaseOrder is the canonical order shown in the TUI. Phases that
// the runner does not emit (because the user passed --dry-run) are
// drawn but stay in "pending" state.
var phaseOrder = []string{
	"export",
	"transform",
	"apply_crds_additive",
	"apply_objects",
	"delete_archived_objects",
	"delete_old_crds",
	"apply_crds_final",
}

type phaseStatus int

const (
	statusPending phaseStatus = iota
	statusRunning
	statusDone
	statusFailed
)

type kindEntry struct {
	name   string
	status phaseStatus
	done   int
	total  int
}

type phaseEntry struct {
	name   string
	status phaseStatus
	kinds  []*kindEntry
	err    error
}

type progressModel struct {
	phases  []*phaseEntry
	byName  map[string]*phaseEntry
	started time.Time
	now     time.Time
}

func newProgressModel() *progressModel {
	m := &progressModel{
		started: time.Now(),
		now:     time.Now(),
		byName:  map[string]*phaseEntry{},
	}
	for _, name := range phaseOrder {
		p := &phaseEntry{name: name}
		m.phases = append(m.phases, p)
		m.byName[name] = p
	}
	return m
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *progressModel) Init() tea.Cmd { return tick() }

func (m *progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		m.now = time.Time(msg)
		return m, tick()
	case phaseStartMsg:
		if p, ok := m.byName[msg.phase]; ok {
			p.status = statusRunning
		}
	case phaseEndMsg:
		if p, ok := m.byName[msg.phase]; ok {
			if msg.err != nil {
				p.status = statusFailed
				p.err = msg.err
			} else {
				p.status = statusDone
			}
		}
	case kindStartMsg:
		if p, ok := m.byName[msg.phase]; ok {
			p.upsertKind(msg.kind, msg.total).status = statusRunning
		}
	case kindItemMsg:
		if p, ok := m.byName[msg.phase]; ok {
			p.upsertKind(msg.kind, 0).done++
		}
	case kindEndMsg:
		if p, ok := m.byName[msg.phase]; ok {
			ke := p.upsertKind(msg.kind, 0)
			if msg.err != nil {
				ke.status = statusFailed
			} else {
				ke.status = statusDone
			}
		}
	}
	return m, nil
}

func (p *phaseEntry) upsertKind(name string, total int) *kindEntry {
	for _, k := range p.kinds {
		if k.name == name {
			if total > k.total {
				k.total = total
			}
			return k
		}
	}
	k := &kindEntry{name: name, total: total}
	p.kinds = append(p.kinds, k)
	return k
}

func (m *progressModel) View() string {
	var b strings.Builder
	elapsed := m.now.Sub(m.started).Round(time.Second)
	fmt.Fprintf(&b, "%s  %s\n",
		styleTitle.Render("Tinkerbell v1alpha1 → v1alpha2 migration"),
		styleSubdued.Render("elapsed "+elapsed.String()),
	)
	for _, p := range m.phases {
		fmt.Fprintf(&b, "  %s %s\n", statusSymbol(p.status), phaseLabel(p))
		for _, k := range p.kinds {
			fmt.Fprintf(&b, "      %s %s %s\n",
				statusSymbol(k.status),
				k.name,
				kindCounts(k),
			)
		}
		if p.err != nil {
			fmt.Fprintf(&b, "      %s %s\n",
				styleFailed.Render("!"),
				styleFailed.Render(p.err.Error()),
			)
		}
	}
	return b.String()
}

func phaseLabel(p *phaseEntry) string {
	switch p.status {
	case statusDone:
		return styleSuccess.Render(p.name)
	case statusRunning:
		return styleRunning.Render(p.name)
	case statusFailed:
		return styleFailed.Render(p.name)
	default:
		return stylePending.Render(p.name)
	}
}

func kindCounts(k *kindEntry) string {
	if k.total > 0 {
		return styleSubdued.Render(fmt.Sprintf("%d/%d", k.done, k.total))
	}
	return styleSubdued.Render(fmt.Sprintf("%d", k.done))
}

// --- messages ------------------------------------------------------

type phaseStartMsg struct{ phase string }
type phaseEndMsg struct {
	phase string
	err   error
}
type kindStartMsg struct {
	phase, kind string
	total       int
}
type kindItemMsg struct{ phase, kind string }
type kindEndMsg struct {
	phase, kind string
	err         error
}

// --- non-TTY fallback ---------------------------------------------

type plainProgress struct {
	w       io.Writer
	started time.Time
	mu      sync.Mutex
}

func (p *plainProgress) line(format string, a ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	elapsed := time.Since(p.started).Round(time.Second)
	fmt.Fprintf(p.w, "[%s] ", elapsed)
	fmt.Fprintf(p.w, format, a...)
	fmt.Fprintln(p.w)
}

func (p *plainProgress) PhaseStart(phase string)             { p.line("%s ...", phase) }
func (p *plainProgress) KindStart(_, kind string, total int) { p.line("  %s start (%d)", kind, total) }
func (p *plainProgress) KindItem(_, _ string)                {}
func (p *plainProgress) KindEnd(_, kind string, err error) {
	if err != nil {
		p.line("  %s FAIL: %v", kind, err)
		return
	}
	p.line("  %s done", kind)
}
func (p *plainProgress) PhaseEnd(phase string, err error) {
	if err != nil {
		p.line("%s FAIL: %v", phase, err)
		return
	}
	p.line("%s done", phase)
}

// --- TTY detection -------------------------------------------------

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(f.Fd())
}
