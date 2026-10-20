package cancellation

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	t0 = time.Unix(1_700_000_000, 0).UTC()
	t1 = t0.Add(time.Minute)
)

func workflow(cancelled *bool, state tinkerbell.WorkflowState, current *tinkerbell.CurrentState) *tinkerbell.Workflow {
	return &tinkerbell.Workflow{
		Spec: tinkerbell.WorkflowSpec{Cancelled: cancelled},
		Status: tinkerbell.WorkflowStatus{
			State:        state,
			CurrentState: current,
		},
	}
}

func current(state tinkerbell.WorkflowState) *tinkerbell.CurrentState {
	return &tinkerbell.CurrentState{
		AgentID:    "agent-1",
		TaskID:     "task-1",
		ActionID:   "action-1",
		ActionName: "install",
		TaskName:   "provision",
		State:      state,
	}
}

func ptr[T any](v T) *T { return &v }

func TestApplyLeavesWorkflowUnchanged(t *testing.T) {
	tests := map[string]*tinkerbell.Workflow{
		"not requested":            workflow(nil, tinkerbell.WorkflowStateRunning, current(tinkerbell.WorkflowStateRunning)),
		"explicitly not requested": workflow(ptr(false), tinkerbell.WorkflowStateRunning, current(tinkerbell.WorkflowStateRunning)),
		"after SUCCESS":            workflow(ptr(true), tinkerbell.WorkflowStateSuccess, current(tinkerbell.WorkflowStateSuccess)),
		"after FAILED":             workflow(ptr(true), tinkerbell.WorkflowStateFailed, current(tinkerbell.WorkflowStateFailed)),
		// Setting status.state to FAILED by hand leaves the Action RUNNING; the FAILED result still stands.
		"after FAILED with a running Action": workflow(ptr(true), tinkerbell.WorkflowStateFailed, current(tinkerbell.WorkflowStateRunning)),
		"after TIMEOUT":                      workflow(ptr(true), tinkerbell.WorkflowStateTimeout, current(tinkerbell.WorkflowStateTimeout)),
		"after CANCELLED":                    workflow(ptr(true), tinkerbell.WorkflowStateCancelled, current(tinkerbell.WorkflowStateSuccess)),
	}
	for name, wf := range tests {
		t.Run(name, func(t *testing.T) {
			before := wf.DeepCopy()
			if Apply(wf, t0) {
				t.Fatal("Apply() = true, want false")
			}
			if diff := cmp.Diff(before, wf); diff != "" {
				t.Fatalf("Workflow changed (-want +got):\n%s", diff)
			}
		})
	}
}

func TestApplyWithoutOutstandingAction(t *testing.T) {
	tests := map[string]*tinkerbell.Workflow{
		"new":                          workflow(ptr(true), "", nil),
		"preparing":                    workflow(ptr(true), tinkerbell.WorkflowStatePreparing, nil),
		"pending and never assigned":   workflow(ptr(true), tinkerbell.WorkflowStatePending, nil),
		"between Actions":              workflow(ptr(true), tinkerbell.WorkflowStateRunning, current(tinkerbell.WorkflowStateSuccess)),
		"running post-operations":      workflow(ptr(true), tinkerbell.WorkflowStatePost, current(tinkerbell.WorkflowStateSuccess)),
		"cancelling after last report": workflow(ptr(true), tinkerbell.WorkflowStateCancelling, current(tinkerbell.WorkflowStateFailed)),
	}
	for name, wf := range tests {
		t.Run(name, func(t *testing.T) {
			if !Apply(wf, t0) {
				t.Fatal("Apply() = false, want true")
			}
			want := tinkerbell.WorkflowStatus{
				State:        tinkerbell.WorkflowStateCancelled,
				CurrentState: wf.Status.CurrentState,
				Cancellation: &tinkerbell.CancellationStatus{
					RequestedAt: &metav1.Time{Time: t0},
					CompletedAt: &metav1.Time{Time: t0},
				},
				Conditions: []tinkerbell.WorkflowCondition{{
					Type:    tinkerbell.Cancellation,
					Status:  metav1.ConditionTrue,
					Reason:  tinkerbell.CancellationReasonNoOutstandingAction,
					Message: "cancelled with no Action outstanding; no further Actions will run",
					Time:    &metav1.Time{Time: t0},
				}},
			}
			if diff := cmp.Diff(want, wf.Status); diff != "" {
				t.Fatalf("status mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestApplyWithOutstandingAction(t *testing.T) {
	for _, actionState := range []tinkerbell.WorkflowState{tinkerbell.WorkflowStatePending, tinkerbell.WorkflowStateRunning} {
		t.Run(string(actionState), func(t *testing.T) {
			wf := workflow(ptr(true), tinkerbell.WorkflowStateRunning, current(actionState))
			if !Apply(wf, t0) {
				t.Fatal("Apply() = false, want true")
			}
			want := tinkerbell.WorkflowStatus{
				State:        tinkerbell.WorkflowStateCancelling,
				CurrentState: current(actionState),
				Cancellation: &tinkerbell.CancellationStatus{RequestedAt: &metav1.Time{Time: t0}},
				Conditions: []tinkerbell.WorkflowCondition{{
					Type:    tinkerbell.Cancellation,
					Status:  metav1.ConditionFalse,
					Reason:  tinkerbell.CancellationReasonLegacyAgent,
					Message: `waiting for Agent "agent-1" to report the result of Action "install" in Task "provision"; the Agent cannot stop an Action that is running`,
					Time:    &metav1.Time{Time: t0},
				}},
			}
			if diff := cmp.Diff(want, wf.Status); diff != "" {
				t.Fatalf("status mismatch (-want +got):\n%s", diff)
			}

			// Observing the same request again, for example from the other component, changes nothing.
			before := wf.DeepCopy()
			if !Apply(wf, t1) {
				t.Fatal("second Apply() = false, want true")
			}
			if diff := cmp.Diff(before, wf); diff != "" {
				t.Fatalf("second Apply changed the Workflow (-want +got):\n%s", diff)
			}
		})
	}
}

func TestComplete(t *testing.T) {
	wf := workflow(ptr(true), tinkerbell.WorkflowStateRunning, current(tinkerbell.WorkflowStateRunning))
	Apply(wf, t0)

	wf.Status.CurrentState.State = tinkerbell.WorkflowStateSuccess
	Complete(wf, t1)

	want := tinkerbell.WorkflowStatus{
		State:        tinkerbell.WorkflowStateCancelled,
		CurrentState: current(tinkerbell.WorkflowStateSuccess),
		Cancellation: &tinkerbell.CancellationStatus{
			RequestedAt: &metav1.Time{Time: t0},
			CompletedAt: &metav1.Time{Time: t1},
		},
		Conditions: []tinkerbell.WorkflowCondition{{
			Type:    tinkerbell.Cancellation,
			Status:  metav1.ConditionTrue,
			Reason:  tinkerbell.CancellationReasonLegacyActionFinished,
			Message: `Agent "agent-1" reported Action "install" in Task "provision" as SUCCESS after cancellation was requested; no further Actions will run`,
			Time:    &metav1.Time{Time: t1},
		}},
	}
	if diff := cmp.Diff(want, wf.Status); diff != "" {
		t.Fatalf("status mismatch (-want +got):\n%s", diff)
	}
}

func TestCompleteSetsRequestedAtWhenTheServerObservesTheRequestFirst(t *testing.T) {
	wf := workflow(ptr(true), tinkerbell.WorkflowStateRunning, current(tinkerbell.WorkflowStateFailed))
	Complete(wf, t1)

	want := &tinkerbell.CancellationStatus{
		RequestedAt: &metav1.Time{Time: t1},
		CompletedAt: &metav1.Time{Time: t1},
	}
	if diff := cmp.Diff(want, wf.Status.Cancellation); diff != "" {
		t.Fatalf("cancellation status mismatch (-want +got):\n%s", diff)
	}
	if wf.Status.State != tinkerbell.WorkflowStateCancelled {
		t.Fatalf("state = %s, want %s", wf.Status.State, tinkerbell.WorkflowStateCancelled)
	}
}

func TestOutstanding(t *testing.T) {
	tests := map[string]struct {
		current *tinkerbell.CurrentState
		want    bool
	}{
		"none":    {},
		"PENDING": {current: current(tinkerbell.WorkflowStatePending), want: true},
		"RUNNING": {current: current(tinkerbell.WorkflowStateRunning), want: true},
		"SUCCESS": {current: current(tinkerbell.WorkflowStateSuccess)},
		"FAILED":  {current: current(tinkerbell.WorkflowStateFailed)},
		"TIMEOUT": {current: current(tinkerbell.WorkflowStateTimeout)},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := Outstanding(workflow(nil, tinkerbell.WorkflowStateRunning, tc.current)) != nil
			if got != tc.want {
				t.Fatalf("Outstanding() != nil is %v, want %v", got, tc.want)
			}
		})
	}
}
