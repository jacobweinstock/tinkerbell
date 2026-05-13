package transform

import (
	"testing"

	v1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	v2 "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWorkflowRuleSet(t *testing.T) {
	src := &v1.WorkflowRuleSet{
		ObjectMeta: metav1.ObjectMeta{Name: "rs", Namespace: "ns"},
		Spec: v1.WorkflowRuleSetSpec{
			Rules: []string{`{"cpu": {"vendor": ["intel"]}}`, `{"memory": {"total": [">", 0]}}`},
			Workflow: v1.WorkflowRuleSetWorkflow{
				Namespace:     "tinkerbell",
				AddAttributes: true,
				Disabled:      ptr(true),
				Template: v1.TemplateConfig{
					Ref:        "ubuntu",
					AgentValue: "device_1",
					KVs:        map[string]string{"image": "ubuntu-22.04"},
				},
			},
		},
	}
	got, err := WorkflowRuleSet(src)
	if err != nil {
		t.Fatalf("WorkflowRuleSet: %v", err)
	}
	want := &v2.Policy{
		TypeMeta: metav1.TypeMeta{APIVersion: "tinkerbell.org/v1alpha2", Kind: "Policy"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rs",
			Namespace: "ns",
			Annotations: map[string]string{
				"tinkerbell.org/migrated-rules": "{\"cpu\": {\"vendor\": [\"intel\"]}}\n{\"memory\": {\"total\": [\">\", 0]}}",
			},
		},
		Spec: v2.PolicySpec{
			Rules: v2.Rules{
				WorkflowAutoCreation: []v2.WorkflowRule{{
					Config: v2.WorkflowConfig{
						Namespace:     "tinkerbell",
						AddAttributes: true,
						Disabled:      ptr(true),
						Globals: &v2.Extra{
							TemplateMap: map[string]string{"image": "ubuntu-22.04"},
						},
						Tasks: []v2.WorkflowTaskConfig{{
							TaskRef: v2.SimpleReference{Name: "ubuntu", Namespace: "tinkerbell"},
						}},
					},
				}},
			},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("WorkflowRuleSet mismatch (-want +got):\n%s", diff)
	}
}
