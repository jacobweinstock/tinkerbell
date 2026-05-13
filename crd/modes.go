package crd

import (
	"context"
	"errors"
	"fmt"

	apiv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// Mode selects which CRD set Migrate applies. The migrate command
// uses Additive between the export and apply_objects phases (so the
// cluster serves both versions while objects are rewritten) and Final
// after delete_old_crds to drop v1alpha1 from spec.versions on
// shared-name CRDs.
type Mode int

const (
	// ModeV1Alpha1 is the historical default: apply only the
	// v1alpha1 CRDs. Used by NewTinkerbell with no overrides.
	ModeV1Alpha1 Mode = iota
	// ModeAdditive applies the v1alpha2 CRD set, but for CRDs whose
	// name is shared between v1alpha1 and v1alpha2 (hardware,
	// workflows, bmc.jobs) merges both versions into spec.versions
	// with v1alpha1 served=true,storage=false and v1alpha2
	// served=true,storage=true. Renamed CRDs (tasks, bmcs, policies)
	// are applied as-is.
	ModeAdditive
	// ModeFinal applies the v1alpha2 CRD set as-is. For shared-name
	// CRDs this drops v1alpha1 from spec.versions.
	ModeFinal
)

// CRDsForMode returns the CRD source map appropriate for mode.
// Errors only arise from ModeAdditive when the embedded CRD YAML
// cannot be re-encoded after merging spec.versions.
func CRDsForMode(mode Mode) (map[string][]byte, error) {
	switch mode {
	case ModeV1Alpha1:
		return TinkerbellDefaults, nil
	case ModeFinal:
		return TinkerbellV1Alpha2, nil
	case ModeAdditive:
		return buildAdditiveCRDs()
	default:
		return nil, fmt.Errorf("unknown CRD Mode %d", mode)
	}
}

// buildAdditiveCRDs computes the additive CRD map by merging
// spec.versions on shared-name CRDs. v1alpha2 is the base (its schema
// and resource conversion settings win); v1alpha1's lone version
// block is appended with served=true, storage=false. v1alpha2 is
// forced to served=true, storage=true to be unambiguous.
func buildAdditiveCRDs() (map[string][]byte, error) {
	out := make(map[string][]byte, len(TinkerbellV1Alpha2))
	for name, v2Raw := range TinkerbellV1Alpha2 {
		v1Raw, shared := TinkerbellDefaults[name]
		if !shared {
			out[name] = v2Raw
			continue
		}
		merged, err := mergeVersions(v1Raw, v2Raw)
		if err != nil {
			return nil, fmt.Errorf("merge spec.versions for %s: %w", name, err)
		}
		out[name] = merged
	}
	return out, nil
}

func mergeVersions(v1Raw, v2Raw []byte) ([]byte, error) {
	var v1CRD, v2CRD apiv1.CustomResourceDefinition
	if err := yaml.Unmarshal(v1Raw, &v1CRD); err != nil {
		return nil, fmt.Errorf("decode v1alpha1 CRD: %w", err)
	}
	if err := yaml.Unmarshal(v2Raw, &v2CRD); err != nil {
		return nil, fmt.Errorf("decode v1alpha2 CRD: %w", err)
	}
	// Force the storage flag on every existing v1alpha2 entry to false
	// before re-marking v1alpha2 as storage=true. Defensive: today the
	// embedded files have a single version block each but this keeps
	// the merge correct if that ever changes.
	for i := range v2CRD.Spec.Versions {
		v2CRD.Spec.Versions[i].Served = true
		v2CRD.Spec.Versions[i].Storage = (v2CRD.Spec.Versions[i].Name == "v1alpha2")
	}
	for _, vv := range v1CRD.Spec.Versions {
		vv.Served = true
		vv.Storage = false
		v2CRD.Spec.Versions = append(v2CRD.Spec.Versions, vv)
	}
	return yaml.Marshal(&v2CRD)
}

// MigrateMode applies the CRD set selected by mode using the same
// apply / update / create fallback chain as Migrate. The receiver's
// CRDs field is updated to the selected map so a subsequent Ready
// call checks the right names.
func (t *Tinkerbell) MigrateMode(ctx context.Context, mode Mode) error {
	crds, err := CRDsForMode(mode)
	if err != nil {
		return err
	}
	t.CRDs = crds
	return t.Migrate(ctx)
}

// DeleteCRDs deletes the named CustomResourceDefinitions. Names that
// are already absent (404) are not an error: deletion is idempotent.
// All other errors from the apiserver are joined and returned so a
// single bad name does not hide problems with the others.
//
// Deleting a CRD asks the apiserver to garbage-collect every CR of
// that kind. There is no recovery short of restoring from the migrate
// workdir, so callers (the migrate runner) must persist their state
// before invoking this.
func (t Tinkerbell) DeleteCRDs(ctx context.Context, names []string) error {
	var joined error
	for _, name := range names {
		err := t.Client.ApiextensionsV1().CustomResourceDefinitions().
			Delete(ctx, name, metav1.DeleteOptions{})
		if err == nil || apierrors.IsNotFound(err) {
			continue
		}
		joined = errors.Join(joined, fmt.Errorf("delete CRD %s: %w", name, err))
	}
	return joined
}

// V1Alpha1OnlyCRDNames returns the names of the v1alpha1 CRDs that no
// longer exist in v1alpha2 and must be deleted in the
// delete_old_crds phase.
func V1Alpha1OnlyCRDNames() []string {
	out := []string{}
	for name := range TinkerbellDefaults {
		if _, kept := TinkerbellV1Alpha2[name]; !kept {
			out = append(out, name)
		}
	}
	return out
}
