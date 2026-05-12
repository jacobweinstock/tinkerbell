package runner

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// runExport runs the export phase for every kind. Per-kind state is
// recorded in PhaseState.Export. Re-running export skips kinds that
// are already Done.
func (r *Runner) runExport(ctx context.Context, state *State) error {
	for _, k := range SourceKinds {
		if state.Phases.Export[k.Name] == PhaseDone {
			continue
		}
		state.SetExport(k.Name, PhaseInProgress)
		if err := state.Save(r.layout); err != nil {
			return err
		}
		if err := r.exportKind(ctx, state, k); err != nil {
			state.SetExport(k.Name, PhaseFailed)
			_ = state.Save(r.layout)
			return fmt.Errorf("export %s: %w", k.Name, err)
		}
		state.SetExport(k.Name, PhaseDone)
		if err := state.Save(r.layout); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) exportKind(ctx context.Context, state *State, k SourceKind) error {
	dir := r.layout.SourceKindDir(k.Name)
	if err := mkdirAll(dir); err != nil {
		return err
	}
	counts := state.Count(k.Name)
	return r.client.List(ctx, k.GVR, "", func(u *unstructured.Unstructured) error {
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
		return nil
	})
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
