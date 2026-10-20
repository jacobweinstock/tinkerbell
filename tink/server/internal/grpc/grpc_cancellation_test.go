package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var cancelTime = time.Unix(1_700_000_100, 0).UTC()

func cancelHandler(backend *mockBackendReadWriter) *Handler {
	return &Handler{
		Logger:  logr.Discard(),
		Backend: backend,
		NowFunc: func() time.Time { return cancelTime },
	}
}

func cancelled(wf tinkerbell.Workflow) tinkerbell.Workflow {
	wf.Spec.Cancelled = toPtr(true)
	return wf
}

// cancelledWithOutstandingAction returns the cancelled Workflow machine0, whose Action was assigned to
// machine-mac-1 and has not reported a result.
func cancelledWithOutstandingAction(workflowState tinkerbell.WorkflowState) tinkerbell.Workflow {
	wf := cancelled(activeWorkflowFixture("machine0"))
	wf.Status.State = workflowState
	wf.Status.Tasks[0].ID = "provision"
	wf.Status.Tasks[0].Actions[0].ID = "stream"
	wf.Status.Tasks[0].Actions[0].State = tinkerbell.WorkflowStateRunning
	wf.Status.CurrentState = &tinkerbell.CurrentState{
		AgentID:  "machine-mac-1",
		TaskID:   "provision",
		ActionID: "stream",
		State:    tinkerbell.WorkflowStateRunning,
	}
	return wf
}

func TestGetActionCancelledWorkflow(t *testing.T) {
	tests := map[string]struct {
		workflows    []tinkerbell.Workflow
		wantCode     codes.Code
		wantWorkflow string
	}{
		"cancelled Workflow is not served": {
			workflows: []tinkerbell.Workflow{cancelled(activeWorkflowFixture("machine1"))},
			wantCode:  codes.FailedPrecondition,
		},
		"cancelled Workflow does not shadow an active Workflow": {
			workflows:    []tinkerbell.Workflow{cancelled(activeWorkflowFixture("machine0")), activeWorkflowFixture("machine1")},
			wantWorkflow: "default/machine1",
		},
		"outstanding Action of a cancelled Workflow blocks an active Workflow listed after it": {
			workflows: []tinkerbell.Workflow{cancelledWithOutstandingAction(tinkerbell.WorkflowStateCancelling), activeWorkflowFixture("machine1")},
			wantCode:  codes.FailedPrecondition,
		},
		"outstanding Action of a cancelled Workflow blocks an active Workflow listed before it": {
			workflows: []tinkerbell.Workflow{activeWorkflowFixture("machine1"), cancelledWithOutstandingAction(tinkerbell.WorkflowStateRunning)},
			wantCode:  codes.FailedPrecondition,
		},
		"outstanding Action of a cancelled Workflow that already FAILED does not block": {
			workflows:    []tinkerbell.Workflow{cancelledWithOutstandingAction(tinkerbell.WorkflowStateFailed), activeWorkflowFixture("machine1")},
			wantWorkflow: "default/machine1",
		},
		"outstanding Action assigned to another Agent does not block": {
			workflows: func() []tinkerbell.Workflow {
				other := cancelledWithOutstandingAction(tinkerbell.WorkflowStateCancelling)
				other.Status.CurrentState.AgentID = "machine-mac-2"
				return []tinkerbell.Workflow{other, activeWorkflowFixture("machine1")}
			}(),
			wantWorkflow: "default/machine1",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			backend := &mockBackendReadWriter{}
			backend.listWorkflowsFunc = func() ([]tinkerbell.Workflow, error) { return tc.workflows, nil }

			resp, err := cancelHandler(backend).GetAction(context.Background(), &proto.ActionRequest{AgentId: toPtr("machine-mac-1")})
			if status.Code(err) != tc.wantCode {
				t.Fatalf("unexpected error code: got %s, want %s (error: %v)", status.Code(err), tc.wantCode, err)
			}
			if tc.wantCode != codes.OK {
				if resp != nil {
					t.Fatalf("expected no Action, got %+v", resp)
				}
				if backend.workflowUpdates != 0 {
					t.Fatalf("expected no Workflow updates, got %d", backend.workflowUpdates)
				}
				return
			}
			if resp.GetWorkflowId() != tc.wantWorkflow {
				t.Fatalf("served Workflow %q, want %q", resp.GetWorkflowId(), tc.wantWorkflow)
			}
		})
	}
}

// TestGetActionCancellationWinsUpdateConflict verifies that an Action is not assigned when
// cancellation is committed after the Workflow was read from the cache.
func TestGetActionCancellationWinsUpdateConflict(t *testing.T) {
	stale := activeWorkflowFixture("machine1")
	committed := cancelled(activeWorkflowFixture("machine1"))

	backend := &mockBackendReadWriter{workflow: stale.DeepCopy()}
	backend.listWorkflowsFunc = func() ([]tinkerbell.Workflow, error) {
		return []tinkerbell.Workflow{*backend.workflow.DeepCopy()}, nil
	}
	backend.updateWorkflowFunc = func(_ *tinkerbell.Workflow) error {
		backend.workflow = committed.DeepCopy()
		return kerrors.NewConflict(schema.GroupResource{Group: "tinkerbell.org", Resource: "workflows"}, stale.Name, errors.New("cancellation won"))
	}
	handler := cancelHandler(backend)

	if _, err := handler.GetAction(context.Background(), &proto.ActionRequest{AgentId: toPtr("machine-mac-1")}); status.Code(err) != codes.Aborted {
		t.Fatalf("unexpected error code: got %s, want %s (error: %v)", status.Code(err), codes.Aborted, err)
	}
	if _, err := handler.GetAction(context.Background(), &proto.ActionRequest{AgentId: toPtr("machine-mac-1")}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("unexpected error code on retry: got %s, want %s (error: %v)", status.Code(err), codes.FailedPrecondition, err)
	}
	if backend.workflowUpdates != 1 {
		t.Fatalf("expected no update after cancellation was read, got %d updates", backend.workflowUpdates)
	}
	if diff := cmp.Diff(&committed, backend.workflow); diff != "" {
		t.Fatalf("committed cancellation changed (-want +got):\n%s", diff)
	}
}

// cancelledReportFixture returns a cancelled Workflow, not yet observed by the controller,
// whose only Action was assigned to agent-1 and has the given state.
func cancelledReportFixture(actionState tinkerbell.WorkflowState) *tinkerbell.Workflow {
	wf := failedWorkflowFixture()
	wf.Spec.Cancelled = toPtr(true)
	wf.Status.State = tinkerbell.WorkflowStateRunning
	wf.Status.CurrentState.State = actionState
	wf.Status.Tasks[0].Actions[0].State = actionState
	return wf
}

func TestReportActionStatusCancelledRunning(t *testing.T) {
	for _, assigned := range []tinkerbell.WorkflowState{tinkerbell.WorkflowStatePending, tinkerbell.WorkflowStateRunning} {
		t.Run("assignment "+string(assigned), func(t *testing.T) {
			wf := cancelledReportFixture(assigned)
			backend := &mockBackendReadWriter{workflow: wf}

			if _, err := cancelHandler(backend).ReportActionStatus(context.Background(), actionStatusRequest(proto.ActionStatusRequest_RUNNING)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if backend.workflowUpdates != 1 {
				t.Fatalf("expected one Workflow update, got %d", backend.workflowUpdates)
			}
			if wf.Status.State != tinkerbell.WorkflowStateCancelling {
				t.Fatalf("Workflow state = %s, want %s", wf.Status.State, tinkerbell.WorkflowStateCancelling)
			}
			if got := wf.Status.Tasks[0].Actions[0].State; got != tinkerbell.WorkflowStateRunning {
				t.Fatalf("Action state = %s, want %s", got, tinkerbell.WorkflowStateRunning)
			}
			if got := wf.Status.CurrentState.State; got != tinkerbell.WorkflowStateRunning {
				t.Fatalf("current state = %s, want %s", got, tinkerbell.WorkflowStateRunning)
			}
			want := &tinkerbell.CancellationStatus{RequestedAt: &metav1.Time{Time: cancelTime}}
			if diff := cmp.Diff(want, wf.Status.Cancellation); diff != "" {
				t.Fatalf("cancellation status mismatch (-want +got):\n%s", diff)
			}
			if !wf.Status.HasCondition(tinkerbell.Cancellation, metav1.ConditionFalse) {
				t.Fatalf("expected a False %s condition, got %+v", tinkerbell.Cancellation, wf.Status.Conditions)
			}
		})
	}
}

func TestReportActionStatusCancelledResult(t *testing.T) {
	for _, result := range []proto.ActionStatusRequest_StateType{
		proto.ActionStatusRequest_SUCCESS,
		proto.ActionStatusRequest_FAILED,
		proto.ActionStatusRequest_TIMEOUT,
	} {
		for _, workflowState := range []tinkerbell.WorkflowState{tinkerbell.WorkflowStateRunning, tinkerbell.WorkflowStateCancelling} {
			t.Run(result.String()+" while "+string(workflowState), func(t *testing.T) {
				wf := cancelledReportFixture(tinkerbell.WorkflowStateRunning)
				wf.Status.State = workflowState
				backend := &mockBackendReadWriter{workflow: wf}

				if _, err := cancelHandler(backend).ReportActionStatus(context.Background(), actionStatusRequest(result)); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if backend.workflowUpdates != 1 {
					t.Fatalf("expected one Workflow update, got %d", backend.workflowUpdates)
				}
				// The last Action's SUCCESS would normally move the Workflow to POST.
				if wf.Status.State != tinkerbell.WorkflowStateCancelled {
					t.Fatalf("Workflow state = %s, want %s", wf.Status.State, tinkerbell.WorkflowStateCancelled)
				}
				wantState := tinkerbell.WorkflowState(result.String())
				if got := wf.Status.Tasks[0].Actions[0].State; got != wantState {
					t.Fatalf("Action state = %s, want %s", got, wantState)
				}
				if got := wf.Status.CurrentState.State; got != wantState {
					t.Fatalf("current state = %s, want %s", got, wantState)
				}
				want := &tinkerbell.CancellationStatus{
					RequestedAt: &metav1.Time{Time: cancelTime},
					CompletedAt: &metav1.Time{Time: cancelTime},
				}
				if diff := cmp.Diff(want, wf.Status.Cancellation); diff != "" {
					t.Fatalf("cancellation status mismatch (-want +got):\n%s", diff)
				}
				if !wf.Status.HasCondition(tinkerbell.Cancellation, metav1.ConditionTrue) {
					t.Fatalf("expected a True %s condition, got %+v", tinkerbell.Cancellation, wf.Status.Conditions)
				}
			})
		}
	}
}

func TestReportActionStatusCancelledRejected(t *testing.T) {
	staleAction := failedWorkflowFixture().Status.Tasks[0].Actions[0]
	staleAction.ID = "action-0"
	staleAction.State = tinkerbell.WorkflowStateSuccess

	tests := map[string]struct {
		workflow func() *tinkerbell.Workflow
		request  *proto.ActionStatusRequest
		wantCode codes.Code
	}{
		"report for an earlier Action": {
			workflow: func() *tinkerbell.Workflow {
				wf := cancelledReportFixture(tinkerbell.WorkflowStateRunning)
				wf.Status.Tasks[0].Actions = append([]tinkerbell.Action{staleAction}, wf.Status.Tasks[0].Actions...)
				return wf
			},
			request: func() *proto.ActionStatusRequest {
				r := actionStatusRequest(proto.ActionStatusRequest_SUCCESS)
				r.ActionId = toPtr("action-0")
				return r
			}(),
			wantCode: codes.FailedPrecondition,
		},
		"report with no outstanding Action": {
			workflow: func() *tinkerbell.Workflow { return cancelledReportFixture(tinkerbell.WorkflowStateSuccess) },
			request:  actionStatusRequest(proto.ActionStatusRequest_SUCCESS),
			wantCode: codes.FailedPrecondition,
		},
		"report for another Task": {
			workflow: func() *tinkerbell.Workflow { return cancelledReportFixture(tinkerbell.WorkflowStateRunning) },
			request: func() *proto.ActionStatusRequest {
				r := actionStatusRequest(proto.ActionStatusRequest_SUCCESS)
				r.TaskId = toPtr("task-2")
				return r
			}(),
			wantCode: codes.FailedPrecondition,
		},
		"report with an unspecified state": {
			workflow: func() *tinkerbell.Workflow { return cancelledReportFixture(tinkerbell.WorkflowStateRunning) },
			request:  actionStatusRequest(proto.ActionStatusRequest_UNSPECIFIED),
			wantCode: codes.InvalidArgument,
		},
		"report with a PENDING state": {
			workflow: func() *tinkerbell.Workflow { return cancelledReportFixture(tinkerbell.WorkflowStateRunning) },
			request:  actionStatusRequest(proto.ActionStatusRequest_PENDING),
			wantCode: codes.InvalidArgument,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			wf := tc.workflow()
			before := wf.DeepCopy()
			backend := &mockBackendReadWriter{workflow: wf}

			resp, err := cancelHandler(backend).ReportActionStatus(context.Background(), tc.request)
			if status.Code(err) != tc.wantCode {
				t.Fatalf("unexpected error code: got %s, want %s (error: %v)", status.Code(err), tc.wantCode, err)
			}
			if resp != nil {
				t.Fatalf("expected no response, got %+v", resp)
			}
			if backend.workflowUpdates != 0 {
				t.Fatalf("expected no Workflow updates, got %d", backend.workflowUpdates)
			}
			if diff := cmp.Diff(before, wf); diff != "" {
				t.Fatalf("Workflow changed (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReportActionStatusCancelledWriteConflict(t *testing.T) {
	backend := &mockBackendReadWriter{
		workflow: cancelledReportFixture(tinkerbell.WorkflowStateRunning),
		writeErr: kerrors.NewConflict(schema.GroupResource{Group: "tinkerbell.org", Resource: "workflows"}, "workflow-1", errors.New("changed")),
	}

	_, err := cancelHandler(backend).ReportActionStatus(context.Background(), actionStatusRequest(proto.ActionStatusRequest_SUCCESS))
	if status.Code(err) != codes.Aborted {
		t.Fatalf("unexpected error code: got %s, want %s (error: %v)", status.Code(err), codes.Aborted, err)
	}
}
