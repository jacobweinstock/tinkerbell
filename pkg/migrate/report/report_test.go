package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tinkerbell/tinkerbell/pkg/migrate/runner"
)

// fullyDoneState constructs a State where every phase is Done. Test
// helpers seed counts on top of this.
func fullyDoneState() *runner.State {
	s := &runner.State{
		Workdir: "/tmp/migrate",
		Started: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		Counts:  runner.KindCounts{},
		Phases: runner.PhaseState{
			Export:            map[string]runner.Phase{},
			Transform:         map[string]runner.Phase{},
			ApplyObjects:      map[string]runner.Phase{},
			ApplyCRDsAdditive: runner.PhaseDone,
			DeleteOldCRDs:     runner.PhaseDone,
			ApplyCRDsFinal:    runner.PhaseDone,
		},
	}
	for _, k := range runner.SourceKinds {
		s.Phases.Export[k.Name] = runner.PhaseDone
		s.Phases.Transform[k.Name] = runner.PhaseDone
	}
	for _, t := range runner.ApplyKinds() {
		s.Phases.ApplyObjects[t] = runner.PhaseDone
	}
	return s
}

func TestBuildSuccess(t *testing.T) {
	s := fullyDoneState()
	s.Count("hardware").Exported = 10
	s.Count("hardware").Transformed = 10
	s.Count("hardware").Applied = 10
	s.Count("bmctask").Discarded = 7
	end := s.Started.Add(2 * time.Minute)

	r := Build(s, end)
	if r.Outcome != OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", r.Outcome)
	}
	if r.Workdir != "/tmp/migrate" {
		t.Fatalf("workdir = %q", r.Workdir)
	}
	if r.CompletedAt != end {
		t.Fatalf("completedAt = %v", r.CompletedAt)
	}

	// One kind row per non-drop kind.
	wantRows := len(runner.SourceKinds) - 1
	if len(r.Kinds) != wantRows {
		t.Fatalf("kinds rows = %d, want %d (%+v)", len(r.Kinds), wantRows, r.Kinds)
	}
	// Discarded only contains bmctask.
	if len(r.Discarded) != 1 || r.Discarded[0].Count != 7 {
		t.Fatalf("discarded = %+v", r.Discarded)
	}
	// Hardware row carries the source/target strings we expect.
	var hw *KindReport
	for i := range r.Kinds {
		if r.Kinds[i].Name == "hardware" {
			hw = &r.Kinds[i]
		}
	}
	if hw == nil {
		t.Fatal("hardware row missing")
	}
	if hw.Source != "hardware.tinkerbell.org/v1alpha1" {
		t.Errorf("hw.Source = %q", hw.Source)
	}
	if hw.Target != "hardware.tinkerbell.org/v1alpha2" {
		t.Errorf("hw.Target = %q", hw.Target)
	}
}

func TestBuildPartial(t *testing.T) {
	s := fullyDoneState()
	s.Phases.Transform["hardware"] = runner.PhaseInProgress
	r := Build(s, time.Now())
	if r.Outcome != OutcomePartial {
		t.Fatalf("outcome = %q, want partial", r.Outcome)
	}
}

func TestBuildFailed(t *testing.T) {
	s := fullyDoneState()
	s.Count("hardware").Failed = 3
	r := Build(s, time.Now())
	if r.Outcome != OutcomeFailed {
		t.Fatalf("outcome = %q, want failed", r.Outcome)
	}
}

func TestWriteJSON(t *testing.T) {
	s := fullyDoneState()
	s.Count("hardware").Exported = 1
	s.Count("hardware").Transformed = 1
	s.Count("hardware").Applied = 1
	r := Build(s, s.Started.Add(time.Second))

	var buf bytes.Buffer
	if err := WriteJSON(&buf, r); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	// Round-trip back through json.Decode to ensure validity.
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if got["outcome"] != "success" {
		t.Errorf("outcome in JSON = %v", got["outcome"])
	}
	if got["workdir"] != "/tmp/migrate" {
		t.Errorf("workdir in JSON = %v", got["workdir"])
	}
}

func TestWriteText(t *testing.T) {
	s := fullyDoneState()
	s.Count("hardware").Exported = 4
	s.Count("hardware").Transformed = 4
	s.Count("hardware").Applied = 4
	s.Count("workflow").Exported = 2
	s.Count("workflow").Transformed = 2
	s.Count("bmctask").Discarded = 1
	r := Build(s, s.Started.Add(30*time.Second))

	var buf bytes.Buffer
	if err := WriteText(&buf, r); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"outcome:   success",
		"hardware.tinkerbell.org/v1alpha1",
		"hardware.tinkerbell.org/v1alpha2",
		"Archived",
		"Discarded:",
		"no v1alpha2 successor",
		"Next steps:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n--- output ---\n%s", want, out)
		}
	}
	// Applied column shows em-dash for archive rows.
	if !strings.Contains(out, "archive") {
		t.Errorf("expected archive handling label\n%s", out)
	}
}
