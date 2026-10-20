// Package cancellation applies Workflow cancellation requests to Workflow status.
//
// The Tink Server and the Workflow controller can each observe a request first, so both
// use this package to make the same state transitions. See
// docs/technical/designs/WORKFLOW_CANCELLATION.md.
package cancellation

import (
	"fmt"
	"time"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Requested reports whether cancellation of wf has been requested.
func Requested(wf *tinkerbell.Workflow) bool {
	return wf.Spec.Cancelled != nil && *wf.Spec.Cancelled
}

// IsFinal reports whether state is a terminal Workflow result.
// Cancellation never replaces a final result.
func IsFinal(state tinkerbell.WorkflowState) bool {
	switch state {
	case tinkerbell.WorkflowStateSuccess,
		tinkerbell.WorkflowStateFailed,
		tinkerbell.WorkflowStateTimeout,
		tinkerbell.WorkflowStateCancelled:
		return true
	default:
		return false
	}
}

// Outstanding returns the Action that is assigned to an Agent and whose result has not been
// reported, or nil if there is none. An assignment whose Agent never reported RUNNING is
// still outstanding, because the Agent may have received it.
func Outstanding(wf *tinkerbell.Workflow) *tinkerbell.CurrentState {
	cs := wf.Status.CurrentState
	if cs == nil {
		return nil
	}
	if cs.State == tinkerbell.WorkflowStatePending || cs.State == tinkerbell.WorkflowStateRunning {
		return cs
	}
	return nil
}

// Apply moves a Workflow whose cancellation was requested to CANCELLING, if an Action is
// outstanding, or to CANCELLED otherwise. It reports false, leaving wf unchanged, when
// cancellation was not requested or wf already has a final result.
// Applying it again to an unchanged Workflow changes nothing.
func Apply(wf *tinkerbell.Workflow, now time.Time) bool {
	if !Requested(wf) || IsFinal(wf.Status.State) {
		return false
	}
	markRequested(wf, now)

	if a := Outstanding(wf); a != nil {
		wf.Status.State = tinkerbell.WorkflowStateCancelling
		wf.Status.SetConditionIfDifferent(tinkerbell.WorkflowCondition{
			Type:   tinkerbell.Cancellation,
			Status: metav1.ConditionFalse,
			Reason: tinkerbell.CancellationReasonLegacyAgent,
			Message: fmt.Sprintf("waiting for Agent %q to report the result of Action %q in Task %q; the Agent cannot stop an Action that is running",
				a.AgentID, actionName(a), taskName(a)),
			Time: &metav1.Time{Time: now},
		})
		return true
	}

	finish(wf, now, tinkerbell.CancellationReasonNoOutstandingAction, "cancelled with no Action outstanding; no further Actions will run")
	return true
}

// Complete moves a cancelled Workflow to CANCELLED once the outstanding Action's result is
// known. Status.CurrentState must already hold that result.
func Complete(wf *tinkerbell.Workflow, now time.Time) {
	markRequested(wf, now)
	msg := "cancelled; no further Actions will run"
	if cs := wf.Status.CurrentState; cs != nil {
		msg = fmt.Sprintf("Agent %q reported Action %q in Task %q as %s after cancellation was requested; no further Actions will run",
			cs.AgentID, actionName(cs), taskName(cs), cs.State)
	}
	finish(wf, now, tinkerbell.CancellationReasonLegacyActionFinished, msg)
}

func finish(wf *tinkerbell.Workflow, now time.Time, reason, msg string) {
	wf.Status.State = tinkerbell.WorkflowStateCancelled
	if wf.Status.Cancellation.CompletedAt == nil {
		wf.Status.Cancellation.CompletedAt = &metav1.Time{Time: now}
	}
	wf.Status.SetConditionIfDifferent(tinkerbell.WorkflowCondition{
		Type:    tinkerbell.Cancellation,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: msg,
		Time:    &metav1.Time{Time: now},
	})
}

func markRequested(wf *tinkerbell.Workflow, now time.Time) {
	if wf.Status.Cancellation == nil {
		wf.Status.Cancellation = &tinkerbell.CancellationStatus{}
	}
	if wf.Status.Cancellation.RequestedAt == nil {
		wf.Status.Cancellation.RequestedAt = &metav1.Time{Time: now}
	}
}

func actionName(cs *tinkerbell.CurrentState) string {
	if cs.ActionName != "" {
		return cs.ActionName
	}
	return cs.ActionID
}

func taskName(cs *tinkerbell.CurrentState) string {
	if cs.TaskName != "" {
		return cs.TaskName
	}
	return cs.TaskID
}
