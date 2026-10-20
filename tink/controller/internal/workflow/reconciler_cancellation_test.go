package workflow

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1alpha1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// reconcileCancelled reconciles wf once with a client that fails the test on any write other
// than a Workflow status patch. Cancellation must not render templates, toggle Hardware,
// create BMC jobs, or run any other side effect. It returns the stored Workflow and the
// number of status patches.
func reconcileCancelled(t *testing.T, wf *v1alpha1.Workflow) (*v1alpha1.Workflow, int) {
	t.Helper()

	statusPatches := 0
	base := GetFakeClientBuilder().WithObjects(wf).Build()
	cc := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.CreateOption) error {
			t.Fatalf("unexpected create of %T %s", obj, obj.GetName())
			return nil
		},
		Update: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.UpdateOption) error {
			t.Fatalf("unexpected update of %T %s", obj, obj.GetName())
			return nil
		},
		Patch: func(_ context.Context, _ client.WithWatch, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
			t.Fatalf("unexpected patch of %T %s", obj, obj.GetName())
			return nil
		},
		Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
			t.Fatalf("unexpected delete of %T %s", obj, obj.GetName())
			return nil
		},
		SubResourcePatch: func(ctx context.Context, c client.Client, subResource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			statusPatches++
			return c.SubResource(subResource).Patch(ctx, obj, patch, opts...)
		},
	})
	r := &Reconciler{client: cc, nowFunc: TestTime.Now, references: fakeReferences{}}

	result, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(wf)})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if diff := cmp.Diff(reconcile.Result{}, result); diff != "" {
		t.Fatalf("Reconcile() result mismatch (-want +got):\n%s", diff)
	}

	got := &v1alpha1.Workflow{}
	if err := cc.Get(context.Background(), client.ObjectKeyFromObject(wf), got); err != nil {
		t.Fatalf("get Workflow: %v", err)
	}
	return got, statusPatches
}

func cancelledWorkflow(state v1alpha1.WorkflowState) *v1alpha1.Workflow {
	cancelled := true
	return &v1alpha1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "debian", Namespace: "default"},
		Spec: v1alpha1.WorkflowSpec{
			Cancelled:   &cancelled,
			TemplateRef: "debian",
			HardwareRef: "machine1",
		},
		Status: v1alpha1.WorkflowStatus{
			State:   state,
			AgentID: "agent-1",
			Tasks: []v1alpha1.Task{
				{
					ID:      "task-1",
					Name:    "first",
					AgentID: "agent-1",
					Actions: []v1alpha1.Action{{ID: "action-1", Name: "stream", State: v1alpha1.WorkflowStatePending}},
				},
				{
					ID:      "task-2",
					Name:    "second",
					AgentID: "agent-2",
					Actions: []v1alpha1.Action{{ID: "action-2", Name: "reboot", State: v1alpha1.WorkflowStatePending}},
				},
			},
		},
	}
}

func assertCancellation(t *testing.T, got *v1alpha1.Workflow, wantState v1alpha1.WorkflowState, wantCondition metav1.ConditionStatus, wantReason string) {
	t.Helper()

	if got.Status.State != wantState {
		t.Fatalf("state = %s, want %s", got.Status.State, wantState)
	}
	now := metav1.NewTime(TestTime.Now())
	want := &v1alpha1.CancellationStatus{RequestedAt: &now}
	if wantState == v1alpha1.WorkflowStateCancelled {
		want.CompletedAt = &now
	}
	if diff := cmp.Diff(want, got.Status.Cancellation); diff != "" {
		t.Fatalf("cancellation status mismatch (-want +got):\n%s", diff)
	}
	for _, c := range got.Status.Conditions {
		if c.Type == v1alpha1.Cancellation {
			if c.Status != wantCondition || c.Reason != wantReason {
				t.Fatalf("condition = %s/%s, want %s/%s", c.Status, c.Reason, wantCondition, wantReason)
			}
			return
		}
	}
	t.Fatalf("missing %s condition in %+v", v1alpha1.Cancellation, got.Status.Conditions)
}

func TestReconcileCancelledWithoutOutstandingAction(t *testing.T) {
	tests := map[string]func() *v1alpha1.Workflow{
		"new Workflow is not rendered": func() *v1alpha1.Workflow {
			wf := cancelledWorkflow("")
			wf.Status = v1alpha1.WorkflowStatus{}
			return wf
		},
		"disabled new Workflow": func() *v1alpha1.Workflow {
			wf := cancelledWorkflow("")
			wf.Status = v1alpha1.WorkflowStatus{}
			disabled := true
			wf.Spec.Disabled = &disabled
			return wf
		},
		"preparing Workflow does not run boot operations": func() *v1alpha1.Workflow {
			wf := cancelledWorkflow(v1alpha1.WorkflowStatePreparing)
			wf.Spec.BootOptions = v1alpha1.BootOptions{BootMode: v1alpha1.BootModeIsoboot, ISOURL: "http://example.com/hook.iso", ToggleAllowNetboot: true}
			return wf
		},
		"pending Workflow": func() *v1alpha1.Workflow { return cancelledWorkflow(v1alpha1.WorkflowStatePending) },
		"between Tasks does not move to the next Agent": func() *v1alpha1.Workflow {
			wf := cancelledWorkflow(v1alpha1.WorkflowStateRunning)
			wf.Status.Tasks[0].Actions[0].State = v1alpha1.WorkflowStateSuccess
			wf.Status.CurrentState = &v1alpha1.CurrentState{AgentID: "agent-1", TaskID: "task-1", ActionID: "action-1", State: v1alpha1.WorkflowStateSuccess}
			return wf
		},
		"post-processing does not toggle Hardware": func() *v1alpha1.Workflow {
			wf := cancelledWorkflow(v1alpha1.WorkflowStatePost)
			wf.Spec.BootOptions = v1alpha1.BootOptions{ToggleAllowNetboot: true}
			wf.Status.Tasks[0].Actions[0].State = v1alpha1.WorkflowStateSuccess
			wf.Status.Tasks[1].Actions[0].State = v1alpha1.WorkflowStateSuccess
			wf.Status.CurrentState = &v1alpha1.CurrentState{AgentID: "agent-2", TaskID: "task-2", ActionID: "action-2", State: v1alpha1.WorkflowStateSuccess}
			return wf
		},
	}
	for name, workflow := range tests {
		t.Run(name, func(t *testing.T) {
			wf := workflow()
			got, patches := reconcileCancelled(t, wf)
			if patches != 1 {
				t.Fatalf("expected one status patch, got %d", patches)
			}
			assertCancellation(t, got, v1alpha1.WorkflowStateCancelled, metav1.ConditionTrue, v1alpha1.CancellationReasonNoOutstandingAction)
			if got.Status.AgentID != wf.Status.AgentID {
				t.Fatalf("AgentID = %q, want %q", got.Status.AgentID, wf.Status.AgentID)
			}
			if diff := cmp.Diff(wf.Status.Tasks, got.Status.Tasks); diff != "" {
				t.Fatalf("Tasks changed (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReconcileCancelledWithOutstandingAction(t *testing.T) {
	for _, actionState := range []v1alpha1.WorkflowState{v1alpha1.WorkflowStatePending, v1alpha1.WorkflowStateRunning} {
		t.Run(string(actionState), func(t *testing.T) {
			wf := cancelledWorkflow(v1alpha1.WorkflowStateRunning)
			wf.Status.Tasks[0].Actions[0].State = actionState
			wf.Status.CurrentState = &v1alpha1.CurrentState{AgentID: "agent-1", TaskID: "task-1", ActionID: "action-1", State: actionState}
			// The global timeout has passed, but CANCELLING must not become TIMEOUT while the
			// Action might still be running.
			expired := metav1.NewTime(TestTime.BeforeSec(60))
			wf.Status.GlobalExecutionStop = &expired

			got, patches := reconcileCancelled(t, wf)
			if patches != 1 {
				t.Fatalf("expected one status patch, got %d", patches)
			}
			assertCancellation(t, got, v1alpha1.WorkflowStateCancelling, metav1.ConditionFalse, v1alpha1.CancellationReasonLegacyAgent)
			if diff := cmp.Diff(wf.Status.CurrentState, got.Status.CurrentState); diff != "" {
				t.Fatalf("current state changed (-want +got):\n%s", diff)
			}

			// Reconciling the CANCELLING Workflow again changes nothing.
			again, patches := reconcileCancelled(t, got)
			if patches != 0 {
				t.Fatalf("expected no status patch, got %d", patches)
			}
			if diff := cmp.Diff(got.Status, again.Status); diff != "" {
				t.Fatalf("status changed (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReconcileCancelledAfterFinalResult(t *testing.T) {
	for _, state := range []v1alpha1.WorkflowState{
		v1alpha1.WorkflowStateSuccess,
		v1alpha1.WorkflowStateFailed,
		v1alpha1.WorkflowStateTimeout,
		v1alpha1.WorkflowStateCancelled,
	} {
		t.Run(string(state), func(t *testing.T) {
			wf := cancelledWorkflow(state)
			wf.Status.CurrentState = &v1alpha1.CurrentState{AgentID: "agent-1", TaskID: "task-1", ActionID: "action-1", State: v1alpha1.WorkflowStateRunning}

			got, patches := reconcileCancelled(t, wf)
			if patches != 0 {
				t.Fatalf("expected no status patch, got %d", patches)
			}
			if diff := cmp.Diff(wf.Status, got.Status); diff != "" {
				t.Fatalf("status changed (-want +got):\n%s", diff)
			}
		})
	}
}
