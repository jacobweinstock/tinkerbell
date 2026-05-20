package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v5"
	v1alpha1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// valueToPointer converts a value of any type to a pointer of that type.
func valueToPointer[T any](v T) *T {
	return &v
}

func defaultBackoff() func() time.Duration {
	backoffPolicy := backoff.NewExponentialBackOff()
	backoffPolicy.MaxInterval = time.Second
	return backoffPolicy.NextBackOff
}

type backoffConfig struct {
	maxRetries int
	duration   func() time.Duration
}

type backoffOpts func(*backoffConfig)

// setAllowPXE sets the allowPXE field on the hardware network interfaces.
// If hardware is nil then it will be retrieved using the client.
// The hardware object will be updated in the cluster.
func setAllowPXE(ctx context.Context, cc client.Client, w *v1alpha1.Workflow, h *v1alpha1.Hardware, allowPXE bool, opts ...backoffOpts) error {
	if h == nil && w == nil {
		return fmt.Errorf("both workflow and hardware cannot be nil")
	}
	bc := &backoffConfig{
		maxRetries: 4,
		duration:   defaultBackoff(),
	}
	for _, opt := range opts {
		opt(bc)
	}
	for attempt := 1; attempt <= bc.maxRetries; attempt++ {
		if h == nil {
			h = &v1alpha1.Hardware{}
			if err := cc.Get(ctx, client.ObjectKey{Name: w.Spec.HardwareRef, Namespace: w.Namespace}, h); err != nil {
				return fmt.Errorf("hardware not found: name=%v; namespace=%v, error: %w", w.Spec.HardwareRef, w.Namespace, err)
			}
		}

		for idx := range h.Spec.Interfaces {
			if h.Spec.Interfaces[idx].Netboot != nil {
				h.Spec.Interfaces[idx].Netboot.AllowPXE = valueToPointer(allowPXE)
			} else {
				h.Spec.Interfaces[idx].Netboot = &v1alpha1.Netboot{
					AllowPXE: valueToPointer(allowPXE),
				}
			}
		}

		if err := cc.Update(ctx, h); err != nil {
			if apierrors.IsConflict(err) {
				if attempt >= bc.maxRetries {
					return fmt.Errorf("error updating allow pxe after %d retries: %w", attempt, err)
				}
				h = nil // reset h to nil to retry fetching the hardware
				// This is a conflict error, which means the hardware object was updated by another process
				// We will retry fetching the hardware object and updating it again.
				time.Sleep(bc.duration())
				continue
			}
			return fmt.Errorf("error updating allow pxe: %w", err)
		}
		return nil
	}

	return nil
}

// hardwareFrom retrieves the in cluster hardware object defined in the given workflow.
func hardwareFrom(ctx context.Context, cc client.Client, w *v1alpha1.Workflow) (*v1alpha1.Hardware, error) {
	if w == nil {
		return nil, fmt.Errorf("workflow is nil")
	}

	h := &v1alpha1.Hardware{}
	if err := cc.Get(ctx, client.ObjectKey{Name: w.Spec.HardwareRef, Namespace: w.Namespace}, h); err != nil {
		return nil, fmt.Errorf("hardware not found: name=%v; namespace=%v, error: %w", w.Spec.HardwareRef, w.Namespace, err)
	}

	return h, nil
}

// ensureHardwareTraceparent makes sure the Hardware referenced by w carries
// the workflow's root traceparent under v1alpha1.AnnotationTraceparent. This
// is what lets smee (DHCP/TFTP/iPXE) and tootles (metadata) re-parent their
// spans into the workflow's trace whenever they handle a request for this
// machine, regardless of which phase the workflow is in.
//
// The annotation tracks the workflow's root traceparent (not a phase
// traceparent) so a single stable value covers the full lifecycle. It is
// re-stamped on every reconcile pass that finds it missing or stale, which
// makes the operation self-healing if smee/tootles ever observe a Hardware
// without it.
//
// Missing hardware, missing HardwareRef, and apply conflicts are non-fatal:
// the reconciler will try again on the next pass.
func ensureHardwareTraceparent(ctx context.Context, cc client.Client, w *v1alpha1.Workflow, traceparent string) error {
	if w == nil || w.Spec.HardwareRef == "" || traceparent == "" {
		return nil
	}
	h := &v1alpha1.Hardware{}
	if err := cc.Get(ctx, client.ObjectKey{Name: w.Spec.HardwareRef, Namespace: w.Namespace}, h); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get hardware for traceparent stamping: %w", err)
	}
	if h.Annotations[v1alpha1.AnnotationTraceparent] == traceparent {
		return nil
	}
	patch := client.MergeFrom(h.DeepCopy())
	if h.Annotations == nil {
		h.Annotations = map[string]string{}
	}
	h.Annotations[v1alpha1.AnnotationTraceparent] = traceparent
	if err := cc.Patch(ctx, h, patch); err != nil {
		return fmt.Errorf("patch hardware traceparent annotation: %w", err)
	}
	return nil
}

// toggleHardware toggles the allowPXE field on the hardware network interfaces.
// It is idempotent and uses the Workflow.Status.BootOptionsStatus.AllowNetboot fields for idempotent checks.
// This function will update the Workflow status.
func (s *state) toggleHardware(ctx context.Context, allowPXE bool) error {
	// 1. check if we've already set the allowPXE field to the desired value
	// 2. if not, set the allowPXE field to the desired value
	// 3. return a WorkflowCondition with the result of the operation

	hw, err := hardwareFrom(ctx, s.client, s.workflow)
	if err != nil {
		s.workflow.Status.SetConditionIfDifferent(v1alpha1.WorkflowCondition{
			Type:    v1alpha1.ToggleAllowNetbootTrue,
			Status:  metav1.ConditionFalse,
			Reason:  "Error",
			Message: fmt.Sprintf("error getting hardware: %v", err),
			Time:    &metav1.Time{Time: metav1.Now().UTC()},
		})

		return err
	}

	if allowPXE {
		if s.workflow.Status.BootOptions.AllowNetboot.ToggledTrue {
			return nil
		}
		if err := setAllowPXE(ctx, s.client, s.workflow, hw, allowPXE); err != nil {
			s.workflow.Status.SetConditionIfDifferent(v1alpha1.WorkflowCondition{
				Type:    v1alpha1.ToggleAllowNetbootTrue,
				Status:  metav1.ConditionFalse,
				Reason:  "Error",
				Message: fmt.Sprintf("error setting allowPXE to %v: %v", allowPXE, err),
				Time:    &metav1.Time{Time: metav1.Now().UTC()},
			})
			return err
		}
		s.workflow.Status.BootOptions.AllowNetboot.ToggledTrue = true
		s.workflow.Status.SetCondition(v1alpha1.WorkflowCondition{
			Type:    v1alpha1.ToggleAllowNetbootTrue,
			Status:  metav1.ConditionTrue,
			Reason:  "Complete",
			Message: fmt.Sprintf("set allowPXE to %v", allowPXE),
			Time:    &metav1.Time{Time: metav1.Now().UTC()},
		})
		return nil
	}

	if s.workflow.Status.BootOptions.AllowNetboot.ToggledFalse {
		return nil
	}
	if err := setAllowPXE(ctx, s.client, s.workflow, hw, allowPXE); err != nil {
		s.workflow.Status.SetConditionIfDifferent(v1alpha1.WorkflowCondition{
			Type:    v1alpha1.ToggleAllowNetbootFalse,
			Status:  metav1.ConditionFalse,
			Reason:  "Error",
			Message: fmt.Sprintf("error setting allowPXE to %v: %v", allowPXE, err),
			Time:    &metav1.Time{Time: metav1.Now().UTC()},
		})
		return err
	}
	s.workflow.Status.BootOptions.AllowNetboot.ToggledFalse = true
	s.workflow.Status.SetCondition(v1alpha1.WorkflowCondition{
		Type:    v1alpha1.ToggleAllowNetbootFalse,
		Status:  metav1.ConditionTrue,
		Reason:  "Complete",
		Message: fmt.Sprintf("set allowPXE to %v", allowPXE),
		Time:    &metav1.Time{Time: metav1.Now().UTC()},
	})
	return nil
}
