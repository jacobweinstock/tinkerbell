package runner

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// runExport runs the export phase for every kind. Per-kind state is
// recorded in PhaseState.Export. Re-running export skips kinds that
// are already Done.
//
// Per-kind failures do not abort the phase: each kind's outcome is
// recorded independently in state.Phases.Export and the per-kind
// Counts.Failed counter, mirrored into the final report's FAILED
// column. The phase as a whole returns the joined error so the
// runner can decide whether to continue with later phases.
func (r *Runner) runExport(ctx context.Context, state *State) (rerr error) {
	r.progress.PhaseStart("export")
	defer func() { r.progress.PhaseEnd("export", rerr) }()
	var errs []error
	for _, k := range SourceKinds {
		if err := ctx.Err(); err != nil {
			return err
		}
		if state.Phases.Export[k.Name] == PhaseDone {
			continue
		}
		state.SetExport(k.Name, PhaseInProgress)
		if err := state.Save(r.layout); err != nil {
			return err
		}
		if err := r.exportKind(ctx, state, k); err != nil {
			state.Count(k.Name).Failed++
			state.SetExport(k.Name, PhaseFailed)
			_ = state.Save(r.layout)
			r.progress.KindEnd("export", k.Name, err)
			errs = append(errs, fmt.Errorf("export %s: %w", k.Name, err))
			continue
		}
		state.SetExport(k.Name, PhaseDone)
		if err := state.Save(r.layout); err != nil {
			return err
		}
		r.progress.KindEnd("export", k.Name, nil)
	}
	return errors.Join(errs...)
}

func (r *Runner) exportKind(ctx context.Context, state *State, k SourceKind) error {
	dir := r.layout.SourceKindDir(k.Name)
	if err := mkdirAll(dir); err != nil {
		return err
	}
	// Total is not known up front (paged List); pass 0 so the
	// reporter renders an indeterminate counter.
	r.progress.KindStart("export", k.Name, 0)
	counts := state.Count(k.Name)
	err := r.client.List(ctx, k.GVR, "", func(u *unstructured.Unstructured) error {
		// Strip server-populated fields so the on-disk copy is idempotent
		// for re-runs and clean enough to re-apply if a human ever does
		// it manually.
		stripServerFields(u)
		data, err := yaml.Marshal(u.Object)
		if err != nil {
			return fmt.Errorf("encode %s/%s: %w", u.GetNamespace(), u.GetName(), err)
		}
		path := dir + "/" + objectFilename(u.GetNamespace(), u.GetName())
		if err := writeFileAtomic(path, data); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		counts.Exported++
		r.progress.KindItem("export", k.Name)
		return nil
	})
	// A NotFound here means the v1alpha1 GVR is not served by this
	// cluster — typically because a previous migration already
	// dropped v1alpha1 from the CRD's spec.versions. Treat that as
	// "nothing to export" so re-runs against an already-migrated
	// cluster succeed cleanly.
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list %s: %w", k.GVR, err)
	}
	return nil
}

// stripServerFields removes fields that the apiserver assigns at write
// time. Keeping them in the export would make state.json's idempotency
// checks more fragile and would prevent a re-apply.
func stripServerFields(u *unstructured.Unstructured) {
	u.SetResourceVersion("")
	u.SetUID("")
	u.SetGeneration(0)
	u.SetCreationTimestamp(metav1Zero())
	u.SetDeletionGracePeriodSeconds(nil)
	u.SetSelfLink("")
	unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(u.Object, "status")
}
