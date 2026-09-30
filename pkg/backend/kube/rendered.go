package kube

import (
	"context"
	"fmt"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	"k8s.io/apimachinery/pkg/types"
)

// Consumers of rendered Hardware, as named in reference rules.
const (
	ConsumerSmee    = "smee"
	ConsumerTootles = "tootles"
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

// RenderedReader serves Hardware rendered for one consumer. It only reads, so a
// rendered Hardware can never be written back over its templates.
type RenderedReader struct {
	stored   hardwareFilterer
	store    *renderStore
	consumer string
}

type hardwareFilterer interface {
	FilterHardware(ctx context.Context, opts data.HardwareFilter) (*tinkerbell.Hardware, error)
}

// RenderedReader returns a reader of Hardware rendered for consumer, which must
// be ConsumerSmee or ConsumerTootles. It requires Hardware templating.
func (b *Backend) RenderedReader(consumer string) *RenderedReader {
	return &RenderedReader{stored: b, store: b.store, consumer: consumer}
}

// FilterHardware is Backend.FilterHardware, returning the rendered Hardware. A
// Hardware whose templates have never rendered successfully is not found.
func (r *RenderedReader) FilterHardware(ctx context.Context, opts data.HardwareFilter) (*tinkerbell.Hardware, error) {
	hw, err := r.stored.FilterHardware(ctx, opts)
	if err != nil {
		return nil, err
	}
	rendered, ok := r.store.rendered(r.consumer, hw)
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

// RenderStatus returns the outcome of the latest render of the Hardware key, and
// false if it has not been rendered or templating is disabled.
func (b *Backend) RenderStatus(key types.NamespacedName) (RenderStatus, bool) {
	if b.store == nil {
		return RenderStatus{}, false
	}
	return b.store.status(key)
}

// OnRender makes fn be called with the key of every Hardware rendered, starting
// with those already rendered. fn must not block. It does nothing when
// templating is disabled.
func (b *Backend) OnRender(fn func(types.NamespacedName)) {
	if b.store != nil {
		b.store.onRender(fn)
	}
}

func (b *Backend) newRenderStore() *renderStore {
	get := func(ctx context.Context, key types.NamespacedName) (*tinkerbell.Hardware, error) {
		hw := &tinkerbell.Hardware{}
		return hw, b.cluster.GetClient().Get(ctx, key, hw)
	}
	return newRenderStore(b.Logger.WithName("hardware-render"), b.cluster.GetCache(), b.cluster.GetRESTMapper(),
		get, b.ResolveReferences, ConsumerSmee, ConsumerTootles)
}
