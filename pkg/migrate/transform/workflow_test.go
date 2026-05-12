package transform

import (
	"testing"

	v1bmc "github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	v1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	v2 "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell"
	v2bmc "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell/bmc"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWorkflow_basic(t *testing.T) {
	src := &v1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "wf1", Namespace: "ns"},
		Spec: v1.WorkflowSpec{
			TemplateRef: "ubuntu",
			HardwareRef: "node-1",
		},
	}
	refs := TemplateRefs{
		"ubuntu": {{Name: "ubuntu", Namespace: "ns"}},
	}
	got, err := Workflow(src, refs)
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}
	want := &v2.Workflow{
		TypeMeta:   metav1.TypeMeta{APIVersion: "tinkerbell.org/v1alpha2", Kind: "Workflow"},
		ObjectMeta: metav1.ObjectMeta{Name: "wf1", Namespace: "ns"},
		Spec: v2.WorkflowSpec{
			Tasks: []v2.WorkflowTask{{
				TaskRef: v2.SimpleReference{Name: "ubuntu", Namespace: "ns"},
				Hardware: &v2.WorkflowHardware{
					HardwareRef: &v2.SimpleReference{Name: "node-1", Namespace: "ns"},
				},
			}},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Workflow mismatch (-want +got):\n%s", diff)
	}
}

func TestWorkflow_multiTaskFanout(t *testing.T) {
	src := &v1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "wf", Namespace: "ns"},
		Spec: v1.WorkflowSpec{
			TemplateRef: "tpl",
			HardwareRef: "h1",
			Disabled:    ptr(true),
		},
	}
	refs := TemplateRefs{
		"tpl": {
			{Name: "tpl-first", Namespace: "ns"},
			{Name: "tpl-second", Namespace: "ns"},
		},
	}
	got, err := Workflow(src, refs)
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}
	if len(got.Spec.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(got.Spec.Tasks))
	}
	if got.Annotations["tinkerbell.org/disabled"] != "true" {
		t.Errorf("missing disabled annotation: %v", got.Annotations)
	}
}

func TestWorkflow_bootOptionsAndCustomboot(t *testing.T) {
	pa := v1bmc.PowerOn
	src := &v1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "wf", Namespace: "ns"},
		Spec: v1.WorkflowSpec{
			TemplateRef: "t",
			HardwareRef: "h",
			BootOptions: v1.BootOptions{
				ToggleAllowNetboot: true,
				BootMode:           "iso",
				ISOURL:             "http://iso/u.iso",
				CustombootConfig: v1.CustombootConfig{
					PreparingActions: []v1bmc.Action{{PowerAction: &pa}},
				},
			},
		},
	}
	got, err := Workflow(src, nil)
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}
	if len(got.Spec.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(got.Spec.Tasks))
	}
	wh := got.Spec.Tasks[0].Hardware
	if wh == nil {
		t.Fatal("expected Hardware on WorkflowTask")
	}
	if !wh.BootOptions.ToggleNetboot {
		t.Errorf("ToggleNetboot not set")
	}
	if wh.BootOptions.BootMode != v2.BootModeIsoboot {
		t.Errorf("BootMode = %q, want %q (mapped from 'iso')", wh.BootOptions.BootMode, v2.BootModeIsoboot)
	}
	if wh.BootOptions.Customboot == nil || len(wh.BootOptions.Customboot.PreOperations) != 1 {
		t.Fatalf("expected one PreOperation, got %+v", wh.BootOptions.Customboot)
	}
	op := wh.BootOptions.Customboot.PreOperations[0]
	if op.PowerAction == nil || *op.PowerAction != v2bmc.PowerActionOn {
		t.Errorf("PowerAction = %v, want On", op.PowerAction)
	}
}

func TestWorkflow_missingTemplateRefFallback(t *testing.T) {
	src := &v1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "wf", Namespace: "ns"},
		Spec:       v1.WorkflowSpec{TemplateRef: "unknown"},
	}
	got, err := Workflow(src, TemplateRefs{})
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}
	if len(got.Spec.Tasks) != 1 {
		t.Fatalf("expected 1 fallback task, got %d", len(got.Spec.Tasks))
	}
	if got.Spec.Tasks[0].TaskRef.Name != "unknown" {
		t.Errorf("fallback TaskRef.Name = %q, want %q", got.Spec.Tasks[0].TaskRef.Name, "unknown")
	}
}
