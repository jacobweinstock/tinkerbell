package controller

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/backend/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type fakeRenders struct {
	st kube.RenderStatus
	ok bool
}

func (f fakeRenders) RenderStatus(types.NamespacedName) (kube.RenderStatus, bool) { return f.st, f.ok }
func (f fakeRenders) OnRender(func(types.NamespacedName))                         {}

func TestRenderedCondition(t *testing.T) {
	tests := map[string]struct {
		st         kube.RenderStatus
		wantStatus metav1.ConditionStatus
		wantReason string
		wantMsg    string
	}{
		"rendered": {
			st:         kube.RenderStatus{Generation: 3, Templated: true},
			wantStatus: metav1.ConditionTrue, wantReason: "Rendered",
		},
		"no templates": {
			st:         kube.RenderStatus{Generation: 3},
			wantStatus: metav1.ConditionTrue, wantReason: "NoTemplates",
		},
		"reference denied": {
			st:         kube.RenderStatus{Generation: 3, Templated: true, Err: errors.Join(kube.ErrReferenceDenied, errors.New("missing key")), ServingPrevious: true},
			wantStatus: metav1.ConditionFalse, wantReason: "ReferenceDenied",
			wantMsg: "reference denied\nmissing key; the previous rendering is served",
		},
		"reference not found": {
			st:         kube.RenderStatus{Generation: 3, Templated: true, Err: fmt.Errorf("get: %w", apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "net"))},
			wantStatus: metav1.ConditionFalse, wantReason: "ReferenceNotFound",
			wantMsg: `get: configmaps "net" not found; nothing is served`,
		},
		"template error": {
			st:         kube.RenderStatus{Generation: 3, Templated: true, Err: errors.New("bad template")},
			wantStatus: metav1.ConditionFalse, wantReason: "TemplateError",
			wantMsg: "bad template; nothing is served",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := renderedCondition(tt.st)
			if c.Type != ConditionRendered || c.Status != tt.wantStatus || c.Reason != tt.wantReason || c.Message != tt.wantMsg || c.ObservedGeneration != 3 {
				t.Errorf("got %+v", c)
			}
		})
	}
}

func TestReconcileAppliesOnlyChanges(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tinkerbell.AddToScheme(scheme)
	hw := &tinkerbell.Hardware{ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "tink"}}
	var applies int
	c := interceptor.NewClient(fake.NewClientBuilder().WithScheme(scheme).WithObjects(hw).WithStatusSubresource(hw).Build(), interceptor.Funcs{
		SubResourceApply: func(ctx context.Context, c client.Client, sub string, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
			applies++
			return c.SubResource(sub).Apply(ctx, obj, opts...)
		},
	})
	req := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(hw)}
	renderedAt := metav1.NewTime(time.Unix(100, 0))
	r := NewRenderedReconciler(c, fakeRenders{st: kube.RenderStatus{Generation: 1, Templated: true, LastRenderTime: renderedAt}, ok: true})

	for range 2 {
		if _, err := r.Reconcile(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if applies != 1 {
		t.Fatalf("applied %d times, want 1: an unchanged condition must not be written", applies)
	}
	got := &tinkerbell.Hardware{}
	if err := c.Get(context.Background(), req.NamespacedName, got); err != nil {
		t.Fatal(err)
	}
	if cond := meta.FindStatusCondition(got.Status.Conditions, ConditionRendered); cond == nil || cond.Reason != "Rendered" {
		t.Fatalf("conditions = %+v", got.Status.Conditions)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, ConditionRendered).DeepCopy()
	if got.Status.LastRenderTime == nil || !got.Status.LastRenderTime.Equal(&renderedAt) {
		t.Fatalf("LastRenderTime = %v, want %v", got.Status.LastRenderTime, renderedAt)
	}

	renderedAt = metav1.NewTime(time.Unix(101, 0))
	r.renders = fakeRenders{st: kube.RenderStatus{Generation: 1, Templated: true, LastRenderTime: renderedAt}, ok: true}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if applies != 2 {
		t.Fatalf("applied %d times after a completed rerender, want 2", applies)
	}
	if err := c.Get(context.Background(), req.NamespacedName, got); err != nil {
		t.Fatal(err)
	}
	if got.Status.LastRenderTime == nil || !got.Status.LastRenderTime.Equal(&renderedAt) {
		t.Fatalf("LastRenderTime = %v, want %v", got.Status.LastRenderTime, renderedAt)
	}
	if updated := meta.FindStatusCondition(got.Status.Conditions, ConditionRendered); updated == nil || !updated.LastTransitionTime.Equal(&cond.LastTransitionTime) {
		t.Fatalf("condition transition time changed on a successful rerender: %+v", updated)
	}

	r.renders = fakeRenders{}
	if _, err := r.Reconcile(context.Background(), req); err != nil || applies != 2 {
		t.Fatalf("an unrendered Hardware must be left alone: err = %v, applies = %d", err, applies)
	}
}
