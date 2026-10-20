package crd

import (
	"context"
	"math"
	"testing"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"
)

// TestWorkflowCancelledIsSticky evaluates the generated v1alpha1 Workflow CRD's CEL rules, so
// it fails if the rule is missing from the CRD as well as if it is wrong.
func TestWorkflowCancelledIsSticky(t *testing.T) {
	spec := workflowSpecSchema(t)
	// Matches the API server's per-call limit (k8s.io/apiserver/pkg/apis/cel.PerCallLimit).
	validator := cel.NewValidator(spec, false, 1_000_000)
	if validator == nil {
		t.Fatal("v1alpha1 Workflow spec has no CEL validation rules")
	}

	tests := map[string]struct {
		old, new map[string]any
		wantErr  bool
	}{
		"unset to true":                           {old: map[string]any{}, new: map[string]any{"cancelled": true}},
		"false to true":                           {old: map[string]any{"cancelled": false}, new: map[string]any{"cancelled": true}},
		"true to true":                            {old: map[string]any{"cancelled": true}, new: map[string]any{"cancelled": true}},
		"unset to false":                          {old: map[string]any{}, new: map[string]any{"cancelled": false}},
		"false to unset":                          {old: map[string]any{"cancelled": false}, new: map[string]any{}},
		"unrelated change while unset":            {old: map[string]any{"templateRef": "a"}, new: map[string]any{"templateRef": "b"}},
		"unrelated change while cancelled":        {old: map[string]any{"cancelled": true, "templateRef": "a"}, new: map[string]any{"cancelled": true, "templateRef": "b"}},
		"true to false is rejected":               {old: map[string]any{"cancelled": true}, new: map[string]any{"cancelled": false}, wantErr: true},
		"true to unset is rejected":               {old: map[string]any{"cancelled": true}, new: map[string]any{}, wantErr: true},
		"unset during another change is rejected": {old: map[string]any{"cancelled": true, "templateRef": "a"}, new: map[string]any{"templateRef": "a"}, wantErr: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			errs, _ := validator.Validate(context.Background(), field.NewPath("spec"), spec, tc.new, tc.old, math.MaxInt64)
			if gotErr := len(errs) > 0; gotErr != tc.wantErr {
				t.Fatalf("Validate() errors = %v, want error: %v", errs, tc.wantErr)
			}
		})
	}
}

func workflowSpecSchema(t *testing.T) *structuralschema.Structural {
	t.Helper()

	raw, err := crdFS.ReadFile("bases/v1alpha1/tinkerbell.org_workflows.yaml")
	if err != nil {
		t.Fatalf("read CRD: %v", err)
	}
	crd := &apiv1.CustomResourceDefinition{}
	if err := yaml.Unmarshal(raw, crd); err != nil {
		t.Fatalf("decode CRD: %v", err)
	}
	var props *apiv1.JSONSchemaProps
	for _, v := range crd.Spec.Versions {
		if v.Name == "v1alpha1" && v.Schema != nil && v.Schema.OpenAPIV3Schema != nil {
			if p, ok := v.Schema.OpenAPIV3Schema.Properties["spec"]; ok {
				props = &p
			}
		}
	}
	if props == nil {
		t.Fatal("v1alpha1 Workflow CRD has no spec schema")
	}

	internal := &apiextensions.JSONSchemaProps{}
	if err := apiv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(props, internal, nil); err != nil {
		t.Fatalf("convert schema: %v", err)
	}
	s, err := structuralschema.NewStructural(internal)
	if err != nil {
		t.Fatalf("structural schema: %v", err)
	}
	return s
}
