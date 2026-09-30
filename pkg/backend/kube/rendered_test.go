package kube

import (
	"context"
	"testing"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type storedHardware struct{ hw *tinkerbell.Hardware }

func (s storedHardware) FilterHardware(context.Context, data.HardwareFilter) (*tinkerbell.Hardware, error) {
	return s.hw.DeepCopy(), nil
}

func TestRenderedReader(t *testing.T) {
	ctx := context.Background()
	hws, res := &fakeHardware{}, &fakeResolver{refs: netRefs("example.org")}
	store := newTestStore(hws, res, &fakeInformers{})
	hw := templated("1")
	hws.set(hw)
	r := &RenderedReader{stored: storedHardware{hw}, store: store, consumer: ConsumerSmee}

	_, err := r.FilterHardware(ctx, data.HardwareFilter{})
	if !apierrors.IsNotFound(err) || !hardwareNotFound(err) {
		t.Fatalf("err = %v, want a not found error before the first render", err)
	}

	store.render(ctx, client.ObjectKeyFromObject(hw))
	got, err := r.FilterHardware(ctx, data.HardwareFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if *got.Spec.UserData != "domain=example.org" {
		t.Fatalf("userData = %q", *got.Spec.UserData)
	}
}

func TestRenderHardwareDisabled(t *testing.T) {
	hw := templated("1")

	got, err := (&Backend{}).RenderHardware(hw, nil)
	if err != nil || got != hw {
		t.Fatalf("with templating disabled got %v, %v; want the stored Hardware", got, err)
	}
}

// hardwareNotFound mirrors how Smee and Tootles recognize a missing Hardware.
func hardwareNotFound(err error) bool {
	nf, ok := err.(interface{ NotFound() bool })
	return ok && nf.NotFound()
}
