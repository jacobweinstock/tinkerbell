package crd

import (
	"context"
	"slices"
	"testing"

	apiv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

func TestV1Alpha1OnlyCRDNames(t *testing.T) {
	names := V1Alpha1OnlyCRDNames()
	want := []string{
		"templates.tinkerbell.org",
		"workflowrulesets.tinkerbell.org",
		"machines.bmc.tinkerbell.org",
		"tasks.bmc.tinkerbell.org",
	}
	for _, w := range want {
		if !slices.Contains(names, w) {
			t.Errorf("missing %q in v1alpha1-only set: %v", w, names)
		}
	}
	if len(names) != len(want) {
		t.Errorf("got %d names %v, want %d %v", len(names), names, len(want), want)
	}
}

func TestCRDsForModeAdditiveSharedNames(t *testing.T) {
	crds, err := CRDsForMode(ModeAdditive)
	if err != nil {
		t.Fatalf("CRDsForMode: %v", err)
	}

	// Every v1alpha2 CRD should be present.
	for name := range TinkerbellV1Alpha2 {
		if _, ok := crds[name]; !ok {
			t.Errorf("additive map missing %q", name)
		}
	}

	// Shared-name CRDs should declare both versions, with v1alpha2
	// as storage and v1alpha1 not stored.
	shared := []string{
		"hardware.tinkerbell.org",
		"workflows.tinkerbell.org",
		"jobs.bmc.tinkerbell.org",
	}
	for _, name := range shared {
		raw, ok := crds[name]
		if !ok {
			t.Errorf("missing shared CRD %q", name)
			continue
		}
		var crd apiv1.CustomResourceDefinition
		if err := yaml.Unmarshal(raw, &crd); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		seen := map[string]apiv1.CustomResourceDefinitionVersion{}
		for _, v := range crd.Spec.Versions {
			seen[v.Name] = v
		}
		v1, hasv1 := seen["v1alpha1"]
		v2, hasv2 := seen["v1alpha2"]
		if !hasv1 || !hasv2 {
			keys := make([]string, 0, len(seen))
			for k := range seen {
				keys = append(keys, k)
			}
			t.Errorf("%s: want both versions, got %v", name, keys)
			continue
		}
		if !v1.Served || v1.Storage {
			t.Errorf("%s v1alpha1: want served=true,storage=false; got served=%v,storage=%v", name, v1.Served, v1.Storage)
		}
		if !v2.Served || !v2.Storage {
			t.Errorf("%s v1alpha2: want served=true,storage=true; got served=%v,storage=%v", name, v2.Served, v2.Storage)
		}
	}

	// Renamed CRDs (v1alpha2-only names) should be byte-equal to the
	// embedded v1alpha2 source, since no merge takes place.
	for _, name := range []string{"tasks.tinkerbell.org", "bmcs.tinkerbell.org", "policies.tinkerbell.org"} {
		if string(crds[name]) != string(TinkerbellV1Alpha2[name]) {
			t.Errorf("%s: additive map should be byte-equal to v1alpha2 source", name)
		}
	}
}

func TestCRDsForModeFinal(t *testing.T) {
	crds, err := CRDsForMode(ModeFinal)
	if err != nil {
		t.Fatalf("CRDsForMode: %v", err)
	}
	if len(crds) != len(TinkerbellV1Alpha2) {
		t.Errorf("final map size %d, want %d", len(crds), len(TinkerbellV1Alpha2))
	}
	for name := range TinkerbellV1Alpha2 {
		if string(crds[name]) != string(TinkerbellV1Alpha2[name]) {
			t.Errorf("final mode altered %s", name)
		}
	}
}

func TestDeleteCRDsIdempotent(t *testing.T) {
	client := fake.NewSimpleClientset()
	tb, err := NewTinkerbell(func(t *Tinkerbell) { t.Client = client })
	if err != nil {
		t.Fatalf("NewTinkerbell: %v", err)
	}

	// Pre-create one of the names we will delete.
	existing := &apiv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "templates.tinkerbell.org"},
	}
	if _, err := client.ApiextensionsV1().CustomResourceDefinitions().
		Create(context.Background(), existing, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Two calls: first deletes, second hits NotFound and must succeed.
	for i := 0; i < 2; i++ {
		if err := tb.DeleteCRDs(context.Background(), []string{
			"templates.tinkerbell.org",
			"never-existed.tinkerbell.org",
		}); err != nil {
			t.Fatalf("DeleteCRDs (call %d): %v", i+1, err)
		}
	}

	// Confirm the seeded CRD is gone.
	_, getErr := client.ApiextensionsV1().CustomResourceDefinitions().
		Get(context.Background(), "templates.tinkerbell.org", metav1.GetOptions{})
	if !apierrors.IsNotFound(getErr) {
		t.Errorf("expected NotFound, got %v", getErr)
	}
	_ = schema.GroupVersionResource{} // keep schema import live for future tests
}
