package runner

import (
	"context"

	"github.com/tinkerbell/tinkerbell/crd"
)

// NewTinkerbellCRDInstaller wraps a crd.Tinkerbell as a CRDInstaller.
// The wrapped value is mutated by ApplyAdditive / ApplyFinal because
// crd.Tinkerbell.MigrateMode resets the receiver's CRDs map; callers
// must not share a single crd.Tinkerbell across goroutines while a
// migrate run is in progress.
func NewTinkerbellCRDInstaller(t *crd.Tinkerbell) CRDInstaller {
	return &tinkerbellCRDInstaller{t: t}
}

type tinkerbellCRDInstaller struct {
	t *crd.Tinkerbell
}

func (i *tinkerbellCRDInstaller) ApplyAdditive(ctx context.Context) error {
	if err := i.t.MigrateMode(ctx, crd.ModeAdditive); err != nil {
		return err
	}
	return i.t.Ready(ctx)
}

func (i *tinkerbellCRDInstaller) ApplyFinal(ctx context.Context) error {
	// Drop v1alpha1 from status.storedVersions on shared-name CRDs
	// before mutating spec.versions; otherwise the apiserver rejects
	// the v1alpha2-only CRD with "v1alpha1 missing from spec.versions".
	// Safe here because apply_objects has already re-persisted every
	// surviving object as v1alpha2.
	if err := i.t.FinalizeStoredVersions(ctx); err != nil {
		return err
	}
	if err := i.t.MigrateMode(ctx, crd.ModeFinal); err != nil {
		return err
	}
	return i.t.Ready(ctx)
}

func (i *tinkerbellCRDInstaller) DeleteOld(ctx context.Context) error {
	return i.t.DeleteCRDs(ctx, crd.V1Alpha1OnlyCRDNames())
}
