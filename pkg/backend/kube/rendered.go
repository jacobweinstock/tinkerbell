package kube

import (
	"context"
	"fmt"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	"k8s.io/apimachinery/pkg/types"
)

// renderWorkers is the number of Hardware rendered concurrently.
const renderWorkers = 4

// RenderHardware returns hw with its templates rendered against references when
// Hardware templating is enabled, and hw unchanged when it is not.
func (b *Backend) RenderHardware(hw *tinkerbell.Hardware, references map[string]any) (*tinkerbell.Hardware, error) {
	if !b.HardwareTemplating {
		return hw, nil
	}
	return renderHardware(hw, references)
}

// RenderedReader serves Hardware rendered under Hardware-wide reference policy.
// It only reads, so rendered Hardware cannot be written back over its templates.
type RenderedReader struct {
	stored hardwareFilterer
	store  *renderStore
}

type hardwareFilterer interface {
	FilterHardware(ctx context.Context, opts data.HardwareFilter) (*tinkerbell.Hardware, error)
}

// RenderedReader returns a reader of Hardware rendered under Hardware-wide reference policy.
func (b *Backend) RenderedReader() *RenderedReader {
	return &RenderedReader{stored: b, store: b.store}
}

// FilterHardware is Backend.FilterHardware, returning the rendered Hardware. A
// Hardware whose templates have never rendered successfully is not found.
func (r *RenderedReader) FilterHardware(ctx context.Context, opts data.HardwareFilter) (*tinkerbell.Hardware, error) {
	hw, err := r.stored.FilterHardware(ctx, opts)
	if err != nil {
		return nil, err
	}
	rendered, ok := r.store.rendered(hw)
	if !ok {
		return nil, hardwareNotRenderedError{hardwareNotFoundError{name: hw.Name, namespace: hw.Namespace}}
	}
	return rendered, nil
}

// hardwareNotRenderedError is not found to consumers, which cannot use a Hardware
// before its templates render, but says why.
type hardwareNotRenderedError struct {
	hardwareNotFoundError
}

func (h hardwareNotRenderedError) Error() string {
	return fmt.Sprintf("hardware %s/%s has templates that have not rendered successfully", h.namespace, h.name)
}

func (b *Backend) newRenderStore() *renderStore {
	get := func(ctx context.Context, key types.NamespacedName) (*tinkerbell.Hardware, error) {
		hw := &tinkerbell.Hardware{}
		return hw, b.cluster.GetClient().Get(ctx, key, hw)
	}
	return newRenderStore(b.Logger.WithName("hardware-render"), b.cluster.GetCache(), b.cluster.GetRESTMapper(),
		get, b.ResolveReferences)
}
