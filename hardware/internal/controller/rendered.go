// Package controller contains the Hardware controller's reconcilers.
package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/backend/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ConditionRendered reports whether a Hardware's templates render for every consumer.
const ConditionRendered = "Rendered"

const fieldOwner = "hardware-controller"

// RenderReporter reports the outcome of Hardware renders.
type RenderReporter interface {
	RenderStatus(key types.NamespacedName) (kube.RenderStatus, bool)
	OnRender(fn func(types.NamespacedName))
}

// RenderedReconciler keeps each Hardware's Rendered condition in line with its
// latest render. It does not render: it reports what Smee and Tootles are served.
type RenderedReconciler struct {
	client  client.Client
	renders RenderReporter
}

// NewRenderedReconciler returns a RenderedReconciler.
func NewRenderedReconciler(c client.Client, renders RenderReporter) *RenderedReconciler {
	return &RenderedReconciler{client: c, renders: renders}
}

// SetupWithManager reconciles every Hardware the render store reports on.
func (r *RenderedReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Only started once this replica leads, so only the leader writes status.
	renders := source.Func(func(_ context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
		r.renders.OnRender(func(key types.NamespacedName) {
			q.Add(reconcile.Request{NamespacedName: key})
		})
		return nil
	})
	return ctrl.NewControllerManagedBy(mgr).Named("hardware-rendered").WatchesRawSource(renders).Complete(r)
}

// Reconcile applies the Rendered condition when it has changed.
func (r *RenderedReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	st, ok := r.renders.RenderStatus(req.NamespacedName)
	if !ok {
		return reconcile.Result{}, nil
	}
	hw := &tinkerbell.Hardware{}
	if err := r.client.Get(ctx, req.NamespacedName, hw); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	want := renderedCondition(st)
	if have := meta.FindStatusCondition(hw.Status.Conditions, ConditionRendered); have != nil {
		if have.Status == want.Status && have.Reason == want.Reason && have.Message == want.Message && have.ObservedGeneration == want.ObservedGeneration {
			return reconcile.Result{}, nil
		}
		if have.Status == want.Status {
			want.LastTransitionTime = have.LastTransitionTime
		}
	}

	apiVersion, kind := tinkerbell.GroupVersion.String(), "Hardware"
	apply := &tinkerbell.HardwareApplyConfiguration{
		Kind:       &kind,
		APIVersion: &apiVersion,
		Metadata:   tinkerbell.HardwareApplyMetadata{Name: &hw.Name, Namespace: &hw.Namespace},
		Status:     &tinkerbell.HardwareStatusApplyConfiguration{Conditions: []metav1.Condition{want}},
	}
	if err := r.client.Status().Apply(ctx, apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return reconcile.Result{}, fmt.Errorf("apply %s condition to hardware %s: %w", ConditionRendered, req.NamespacedName, err)
	}
	return reconcile.Result{}, nil
}

// renderedCondition turns a render outcome into the Rendered condition.
func renderedCondition(st kube.RenderStatus) metav1.Condition {
	c := metav1.Condition{
		Type:               ConditionRendered,
		Status:             metav1.ConditionTrue,
		Reason:             "Rendered",
		ObservedGeneration: st.Generation,
		LastTransitionTime: metav1.Now(),
	}
	if !st.Templated {
		c.Reason = "NoTemplates"
	}
	if len(st.Failures) == 0 {
		return c
	}

	c.Status = metav1.ConditionFalse
	c.Reason = failureReason(st.Failures[0].Err)
	msgs := make([]string, 0, len(st.Failures))
	for _, f := range st.Failures {
		served := "nothing is served"
		if f.ServingPrevious {
			served = "the previous rendering is served"
		}
		msgs = append(msgs, fmt.Sprintf("%s: %v; %s", f.Consumer, f.Err, served))
	}
	c.Message = strings.Join(msgs, "\n")
	return c
}

func failureReason(err error) string {
	switch {
	case errors.Is(err, kube.ErrReferenceDenied):
		return "ReferenceDenied"
	case apierrors.IsNotFound(err):
		return "ReferenceNotFound"
	default:
		return "TemplateError"
	}
}
