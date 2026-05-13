package transform

import (
	"fmt"

	v1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	v2 "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkflowRuleSet converts a v1alpha1 WorkflowRuleSet into a v1alpha2
// Policy.
//
// Fields that have no v1alpha2 equivalent are dropped:
//
//   - WorkflowRuleSetSpec.Rules ([]string) — v1's free-form Quamina
//     pattern strings have no clean mapping to v2's structured
//     AgentAttributes. The migrated Policy has an empty WorkflowRule
//     entry containing only the Workflow Config; the operator is
//     expected to translate rule patterns by hand. The original v1
//     rules are preserved as the annotation
//     "tinkerbell.org/migrated-rules" (one rule per line) so they are
//     not lost.
//   - WorkflowRuleSetWorkflow.Template.AgentValue — not used in v2.
func WorkflowRuleSet(src *v1.WorkflowRuleSet) (*v2.Policy, error) {
	if src == nil {
		return nil, fmt.Errorf("WorkflowRuleSet: nil source")
	}

	meta := cleanObjectMeta(src.ObjectMeta)
	if len(src.Spec.Rules) > 0 {
		if meta.Annotations == nil {
			meta.Annotations = map[string]string{}
		}
		joined := ""
		for i, r := range src.Spec.Rules {
			if i > 0 {
				joined += "\n"
			}
			joined += r
		}
		meta.Annotations["tinkerbell.org/migrated-rules"] = joined
	}

	cfg := v2.WorkflowConfig{
		Namespace:     src.Spec.Workflow.Namespace,
		AddAttributes: src.Spec.Workflow.AddAttributes,
		Disabled:      src.Spec.Workflow.Disabled,
	}

	// v1.WorkflowRuleSetWorkflow.Template.KVs becomes Globals.TemplateMap.
	if len(src.Spec.Workflow.Template.KVs) > 0 {
		cfg.Globals = &v2.Extra{
			TemplateMap: copyMap(src.Spec.Workflow.Template.KVs),
		}
	}

	// v1.Template.Ref is a Template name. The Template will have been
	// split into one or more Tasks during the Template transform phase
	// with a deterministic naming scheme; here we cannot know the
	// resulting Task names so we record a single TaskRef using the
	// original Template name. The runner can rewrite this once the
	// TemplateRefs map is available, but for the common case (one Task
	// per Template) the name will already be correct because the
	// Template transform reuses the Template's name for a single-task
	// fan-out.
	if ref := src.Spec.Workflow.Template.Ref; ref != "" {
		cfg.Tasks = []v2.WorkflowTaskConfig{{
			TaskRef: v2.SimpleReference{Name: ref, Namespace: src.Spec.Workflow.Namespace},
		}}
	}

	out := &v2.Policy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v2.GroupVersion.String(),
			Kind:       "Policy",
		},
		ObjectMeta: meta,
		Spec: v2.PolicySpec{
			Rules: v2.Rules{
				WorkflowAutoCreation: []v2.WorkflowRule{{
					Config: cfg,
				}},
			},
		},
	}
	return out, nil
}
